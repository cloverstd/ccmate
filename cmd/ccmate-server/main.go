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

	passkeySvc, err := auth.NewPasskeyService(&cfg.Auth)
	if err != nil {
		slog.Error("failed to initialize passkey service", "error", err)
		os.Exit(1)
	}

	if *resetPasskeys {
		passkeySvc.ResetAdmin()
		slog.Info("all passkey registrations cleared")
		return
	}

	gpRegistry := gitprovider.NewRegistry()
	gpRegistry.Register(&ghprovider.Factory{})

	var gitProv gitprovider.GitProvider
	gitProv, err = gpRegistry.Create("github", gitprovider.ProviderConfig{
		AppID: cfg.GitHub.AppID, InstallationID: cfg.GitHub.InstallationID,
		PrivateKeyPath: cfg.GitHub.PrivateKeyPath, WebhookSecret: cfg.GitHub.WebhookSecret,
		PersonalToken: cfg.GitHub.PersonalToken,
	})
	if err != nil {
		slog.Warn("failed to create github provider", "error", err)
	}

	apRegistry := agentprovider.NewRegistry()
	apRegistry.Register(&mockagent.Factory{})

	var agentProv agentprovider.AgentAdapter
	if len(cfg.Agent.Providers) > 0 {
		p := cfg.Agent.Providers[0]
		if factory, ok := apRegistry.Get(p.Name); ok {
			agentProv, err = factory.Create(agentprovider.AgentConfig{Binary: p.Binary, Extra: p.Extra})
			if err != nil {
				slog.Warn("agent provider fallback to mock", "error", err)
			}
		}
	}
	if agentProv == nil {
		agentProv, _ = (&mockagent.Factory{}).Create(agentprovider.AgentConfig{})
		slog.Info("using mock agent provider")
	}

	broker := sse.NewBroker()
	sched := scheduler.New(client, cfg, broker)
	sched.SetProviders(gitProv, agentProv)

	router := api.NewRouter(client, cfg, broker, sched, passkeySvc, gitProv)

	srv := &http.Server{
		Addr: cfg.Server.Addr(), Handler: router,
		ReadTimeout: 15 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second,
	}

	schedCtx, schedCancel := context.WithCancel(ctx)
	go sched.Run(schedCtx)
	go scheduler.RunCleanup(schedCtx, client, cfg)

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
	slog.Info("shutting down", "signal", sig.String())
	schedCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 30*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	slog.Info("server stopped")
}
