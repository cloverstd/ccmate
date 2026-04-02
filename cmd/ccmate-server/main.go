package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"encoding/json"

	"github.com/cloverstd/ccmate/internal/agentprovider"
	claudeagent "github.com/cloverstd/ccmate/internal/agentprovider/claudecode"
	mockagent "github.com/cloverstd/ccmate/internal/agentprovider/mock"
	"github.com/cloverstd/ccmate/internal/api"
	"github.com/cloverstd/ccmate/internal/auth"
	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	ghprovider "github.com/cloverstd/ccmate/internal/gitprovider/github"
	"github.com/cloverstd/ccmate/internal/scheduler"
	"github.com/cloverstd/ccmate/internal/settings"
	"github.com/cloverstd/ccmate/internal/sse"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	resetPasskeys := flag.Bool("reset-passkeys", false, "clear all passkey registrations")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	client, err := ent.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.Schema.Create(ctx); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Settings manager (DB-backed config)
	settingsMgr := settings.NewManager(client)

	// Check initialization status
	if !settingsMgr.IsInitialized(ctx) {
		token := settingsMgr.EnsureSetupToken(ctx)
		slog.Info("=== SYSTEM NOT INITIALIZED ===")
		slog.Info("Open the web UI and enter this setup token to configure the system")
		slog.Info("setup token", "token", token)
		slog.Info("==============================")
	}

	// Initialize Passkey service with defaults (will be reconfigured after setup)
	rpID := settingsMgr.GetWithDefault(ctx, settings.KeyRPID, "localhost")
	rpOrigins := settingsMgr.GetJSONArray(ctx, settings.KeyRPOrigins)
	if len(rpOrigins) == 0 {
		rpOrigins = []string{fmt.Sprintf("http://%s", cfg.Server.Addr())}
	}
	passkeySvc, err := auth.NewPasskeyService(&auth.PasskeyConfig{
		RPDisplayName: settingsMgr.GetWithDefault(ctx, settings.KeyRPDisplayName, "ccmate"),
		RPID:          rpID,
		RPOrigins:     rpOrigins,
		SessionKey:    settingsMgr.GetWithDefault(ctx, settings.KeySessionKey, ""),
	})
	if err != nil {
		slog.Error("failed to initialize passkey service", "error", err)
		os.Exit(1)
	}

	if *resetPasskeys {
		passkeySvc.ResetAdmin()
		slog.Info("all passkey registrations cleared")
		return
	}

	// Initialize Git Provider (from DB settings)
	gpRegistry := gitprovider.NewRegistry()
	gpRegistry.Register(&ghprovider.Factory{})

	var gitProv gitprovider.GitProvider
	personalToken := settingsMgr.GetWithDefault(ctx, settings.KeyGitHubPersonalToken, "")
	webhookSecret := settingsMgr.GetWithDefault(ctx, settings.KeyGitHubWebhookSecret, "")
	if personalToken != "" || settingsMgr.GetWithDefault(ctx, settings.KeyGitHubAppID, "") != "" {
		gitProv, err = gpRegistry.Create("github", gitprovider.ProviderConfig{
			WebhookSecret: webhookSecret,
			PersonalToken: personalToken,
		})
		if err != nil {
			slog.Warn("failed to create github provider", "error", err)
		}
	}

	// Initialize Agent Provider
	apRegistry := agentprovider.NewRegistry()
	apRegistry.Register(&mockagent.Factory{})
	apRegistry.Register(&claudeagent.Factory{})

	debugMode := settingsMgr.GetWithDefault(ctx, settings.KeyDebugMode, "false")
	var agentProv agentprovider.AgentAdapter

	// Read agent provider config from DB
	provJSON := settingsMgr.GetWithDefault(ctx, settings.KeyAgentProviders, "")
	if provJSON != "" {
		var providers []map[string]string
		if err := json.Unmarshal([]byte(provJSON), &providers); err == nil && len(providers) > 0 {
			p := providers[0]
			if factory, ok := apRegistry.Get(p["name"]); ok {
				agentProv, err = factory.Create(agentprovider.AgentConfig{
					Binary: p["binary"],
					Model:  p["model"],
					Extra:  map[string]string{"debug": debugMode},
				})
				if err != nil {
					slog.Warn("failed to create agent provider, falling back to mock", "name", p["name"], "error", err)
				} else {
					slog.Info("using agent provider", "name", p["name"], "binary", p["binary"], "debug", debugMode)
				}
			}
		}
	}
	if agentProv == nil {
		agentProv, _ = (&mockagent.Factory{}).Create(agentprovider.AgentConfig{
			Extra: map[string]string{"debug": debugMode},
		})
		slog.Info("using mock agent provider (configure agent_providers in Settings to use Claude Code)")
	}

	// SSE + Scheduler
	broker := sse.NewBroker()
	sched := scheduler.New(client, settingsMgr, broker)
	sched.SetProviders(gitProv, agentProv)

	// HTTP router
	router := api.NewRouter(client, cfg, broker, sched, passkeySvc, gitProv, settingsMgr)

	srv := &http.Server{
		Addr: cfg.Server.Addr(), Handler: router,
		ReadTimeout: 15 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second,
	}

	schedCtx, schedCancel := context.WithCancel(ctx)
	go sched.Run(schedCtx)
	go scheduler.RunCleanup(schedCtx, client, settingsMgr)

	go func() {
		slog.Info("starting server", "addr", cfg.Server.Addr())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	fmt.Println()
	slog.Info("shutting down gracefully, waiting for running tasks to pause...", "signal", sig.String())

	// Cancel scheduler (stops accepting new tasks, sends SIGINT to running agents)
	schedCancel()

	// Give agents time to save state (claude saves session automatically)
	slog.Info("waiting for agents to finish (up to 15s)...")
	time.Sleep(3 * time.Second)

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 15*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	slog.Info("server stopped")
}
