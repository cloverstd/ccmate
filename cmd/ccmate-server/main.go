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

	"github.com/cloverstd/ccmate/internal/agentprovider"
	mockagent "github.com/cloverstd/ccmate/internal/agentprovider/mock"
	"github.com/cloverstd/ccmate/internal/api"
	"github.com/cloverstd/ccmate/internal/auth"
	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	ghprovider "github.com/cloverstd/ccmate/internal/gitprovider/github"
	"github.com/cloverstd/ccmate/internal/scheduler"
	"github.com/cloverstd/ccmate/internal/sse"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	resetAdmin := flag.Bool("reset-admin", false, "reset admin passkey registration")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Initialize database
	client, err := ent.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	// Run auto migration
	ctx := context.Background()
	if err := client.Schema.Create(ctx); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Initialize Passkey service
	passkeySvc, err := auth.NewPasskeyService(&cfg.Auth)
	if err != nil {
		slog.Error("failed to initialize passkey service", "error", err)
		os.Exit(1)
	}

	if *resetAdmin {
		passkeySvc.ResetAdmin()
		slog.Info("admin passkey reset complete, use the new bootstrap token to re-register")
		return
	}

	// Initialize Git Provider
	gpRegistry := gitprovider.NewRegistry()
	gpRegistry.Register(&ghprovider.Factory{})

	var gitProv gitprovider.GitProvider
	gitProv, err = gpRegistry.Create("github", gitprovider.ProviderConfig{
		AppID:          cfg.GitHub.AppID,
		InstallationID: cfg.GitHub.InstallationID,
		PrivateKeyPath: cfg.GitHub.PrivateKeyPath,
		WebhookSecret:  cfg.GitHub.WebhookSecret,
		PersonalToken:  cfg.GitHub.PersonalToken,
	})
	if err != nil {
		slog.Warn("failed to create github provider, webhook handling will be disabled", "error", err)
	}

	// Initialize Agent Provider
	apRegistry := agentprovider.NewRegistry()
	apRegistry.Register(&mockagent.Factory{})

	var agentProv agentprovider.AgentAdapter
	// Use the first configured provider, fallback to mock
	if len(cfg.Agent.Providers) > 0 {
		p := cfg.Agent.Providers[0]
		factory, ok := apRegistry.Get(p.Name)
		if ok {
			agentProv, err = factory.Create(agentprovider.AgentConfig{
				Binary: p.Binary,
				Extra:  p.Extra,
			})
			if err != nil {
				slog.Warn("failed to create agent provider, falling back to mock", "error", err)
			}
		}
	}
	if agentProv == nil {
		agentProv, _ = (&mockagent.Factory{}).Create(agentprovider.AgentConfig{})
		slog.Info("using mock agent provider")
	}

	// Initialize SSE broker
	broker := sse.NewBroker()

	// Initialize scheduler and wire providers
	sched := scheduler.New(client, cfg, broker)
	sched.SetProviders(gitProv, agentProv)

	// Initialize HTTP router with all dependencies
	router := api.NewRouter(client, cfg, broker, sched, passkeySvc, gitProv)

	srv := &http.Server{
		Addr:         cfg.Server.Addr(),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE needs no write timeout
		IdleTimeout:  60 * time.Second,
	}

	// Start scheduler
	schedCtx, schedCancel := context.WithCancel(ctx)
	go sched.Run(schedCtx)

	// Start cleanup scheduler
	go scheduler.RunCleanup(schedCtx, client, cfg)

	// Start server
	go func() {
		slog.Info("starting server", "addr", cfg.Server.Addr())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	fmt.Println()
	slog.Info("shutting down", "signal", sig.String())

	schedCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	slog.Info("server stopped")
}
