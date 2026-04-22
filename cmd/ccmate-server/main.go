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
	codexagent "github.com/cloverstd/ccmate/internal/agentprovider/codex"
	mockagent "github.com/cloverstd/ccmate/internal/agentprovider/mock"
	"github.com/cloverstd/ccmate/internal/api"
	"github.com/cloverstd/ccmate/internal/auth"
	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	ghprovider "github.com/cloverstd/ccmate/internal/gitprovider/github"
	"github.com/cloverstd/ccmate/internal/notify"
	tgnotify "github.com/cloverstd/ccmate/internal/notify/telegram"
	"github.com/cloverstd/ccmate/internal/scheduler"
	"github.com/cloverstd/ccmate/internal/settings"
	"github.com/cloverstd/ccmate/internal/sse"
	"github.com/cloverstd/ccmate/internal/updater"

	_ "github.com/mattn/go-sqlite3"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	resetPasskeys := flag.Bool("reset-passkeys", false, "clear all passkey registrations")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("ccmate", version)
		return
	}

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

	// Initialize Git Provider (from DB settings). The Manager holds the active
	// provider and can be rebuilt at runtime whenever settings change, so
	// rotating a PAT or webhook secret no longer requires a service restart.
	gpRegistry := gitprovider.NewRegistry()
	gpRegistry.Register(&ghprovider.Factory{})
	gitProvMgr := gitprovider.NewManager(gpRegistry)
	gitProvMgr.Rebuild(ctx, settingsMgr)

	// Initialize Agent Provider
	apRegistry := agentprovider.NewRegistry()
	apRegistry.Register(&mockagent.Factory{})
	apRegistry.Register(&claudeagent.Factory{})
	apRegistry.Register(&codexagent.Factory{})

	debugMode := settingsMgr.GetWithDefault(ctx, settings.KeyDebugMode, "false")
	provJSON := settingsMgr.GetWithDefault(ctx, settings.KeyAgentProviders, "")
	if provJSON != "" {
		var providers []map[string]string
		if err := json.Unmarshal([]byte(provJSON), &providers); err == nil {
			for _, p := range providers {
				slog.Info("configured agent provider", "name", p["name"], "binary", p["binary"], "debug", debugMode)
			}
		}
	}

	// Notifications
	notifyMgr := notify.NewManager(settingsMgr, client)
	tgProvider := tgnotify.New(settingsMgr, client)
	notifyMgr.RegisterProvider(tgProvider)

	// SSE + Scheduler
	broker := sse.NewBroker()
	sched := scheduler.New(client, settingsMgr, broker)
	sched.SetProviders(gitProvMgr, apRegistry)
	sched.SetNotifyManager(notifyMgr)

	// HTTP router
	router := api.NewRouter(client, cfg, broker, sched, passkeySvc, gitProvMgr, settingsMgr, notifyMgr, version)

	srv := &http.Server{
		Addr: cfg.Server.Addr(), Handler: router,
		ReadTimeout: 15 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second,
	}

	schedCtx, schedCancel := context.WithCancel(ctx)
	go sched.Run(schedCtx)
	go scheduler.RunCleanup(schedCtx, client, settingsMgr)
	go scheduler.RunIssueScanner(schedCtx, client, settingsMgr, gitProvMgr)
	go tgProvider.RunPoller(schedCtx, scheduler.NewTelegramDispatcher(sched))

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

	// Cancel scheduler (stops accepting new tasks, sends SIGINT to running agents)
	schedCancel()

	runningCount := sched.RunningCount()
	if runningCount > 0 {
		slog.Info("waiting for running agents to finish...", "signal", sig.String(), "running", runningCount)
		// Poll until agents finish or timeout
		deadline := time.After(15 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
	waitLoop:
		for {
			select {
			case <-deadline:
				slog.Warn("timeout waiting for agents, forcing shutdown", "remaining", sched.RunningCount())
				break waitLoop
			case <-ticker.C:
				if sched.RunningCount() == 0 {
					slog.Info("all agents finished")
					break waitLoop
				}
			}
		}
	} else {
		slog.Info("no running agents, shutting down immediately", "signal", sig.String())
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	slog.Info("server stopped")

	// If an online update replaced the binary, exit non-zero so systemd's
	// Restart= policy (on-failure or always) relaunches with the new build.
	if updater.RestartPending() {
		slog.Info("online update pending, exiting non-zero to trigger systemd restart")
		os.Exit(1)
	}
}
