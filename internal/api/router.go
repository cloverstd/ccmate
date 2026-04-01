package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/cloverstd/ccmate/internal/api/handler"
	"github.com/cloverstd/ccmate/internal/api/middleware"
	"github.com/cloverstd/ccmate/internal/auth"
	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/scheduler"
	"github.com/cloverstd/ccmate/internal/sse"
	"github.com/cloverstd/ccmate/internal/static"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func NewRouter(
	client *ent.Client,
	cfg *config.Config,
	broker *sse.Broker,
	sched *scheduler.Scheduler,
	passkeySvc *auth.PasskeyService,
	gitProv gitprovider.GitProvider,
) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS)

	// Auth handlers
	authHandler := handler.NewAuthHandler(client, cfg, passkeySvc)
	// Project handlers
	projectHandler := handler.NewProjectHandler(client)
	// Task handlers
	taskHandler := handler.NewTaskHandler(client, cfg, broker, sched)
	// Webhook handler
	webhookHandler := handler.NewWebhookHandler(client, cfg, sched)
	if gitProv != nil {
		webhookHandler.SetGitProvider(gitProv)
	}

	// Public routes
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/passkey/register/start", authHandler.RegisterStart)
		r.Post("/passkey/register/finish", authHandler.RegisterFinish)
		r.Post("/passkey/login/start", authHandler.LoginStart)
		r.Post("/passkey/login/finish", authHandler.LoginFinish)
	})

	// Webhook routes (authenticated via signature)
	r.Post("/webhooks/github", webhookHandler.HandleGitHub)

	// Metrics endpoint (public)
	r.Get("/metrics", handler.MetricsHandler(client))

	// Protected API routes
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.RequireAuth(passkeySvc))

		// Projects
		r.Get("/projects", projectHandler.List)
		r.Post("/projects", projectHandler.Create)
		r.Put("/projects/{id}", projectHandler.Update)
		r.Get("/projects/{id}", projectHandler.Get)

		// Label rules
		r.Get("/projects/{id}/label-rules", projectHandler.ListLabelRules)
		r.Post("/projects/{id}/label-rules", projectHandler.CreateLabelRule)
		r.Delete("/label-rules/{id}", projectHandler.DeleteLabelRule)

		// Prompt templates
		r.Get("/prompt-templates", projectHandler.ListPromptTemplates)
		r.Post("/prompt-templates", projectHandler.CreatePromptTemplate)
		r.Put("/prompt-templates/{id}", projectHandler.UpdatePromptTemplate)
		r.Delete("/prompt-templates/{id}", projectHandler.DeletePromptTemplate)

		// Tasks
		r.Get("/tasks", taskHandler.List)
		r.Post("/tasks", taskHandler.Create)
		r.Get("/tasks/{id}", taskHandler.Get)
		r.Post("/tasks/{id}/pause", taskHandler.Pause)
		r.Post("/tasks/{id}/resume", taskHandler.Resume)
		r.Post("/tasks/{id}/retry", taskHandler.Retry)
		r.Post("/tasks/{id}/cancel", taskHandler.Cancel)
		r.Post("/tasks/{id}/messages", taskHandler.SendMessage)
		r.Post("/tasks/{id}/attachments", taskHandler.UploadAttachment)
		r.Get("/tasks/{id}/events/stream", taskHandler.EventStream)

		// Models
		r.Get("/models", projectHandler.ListModels)
		r.Post("/models", projectHandler.CreateModel)
		r.Put("/models/{id}", projectHandler.UpdateModel)
		r.Delete("/models/{id}", projectHandler.DeleteModel)
	})

	// Serve frontend static files
	fileServer(r)

	return r
}

func fileServer(r chi.Router) {
	distFS, err := fs.Sub(static.FS, "dist")
	if err != nil {
		return
	}
	fsHandler := http.FileServer(http.FS(distFS))

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/webhooks/") || path == "/metrics" {
			http.NotFound(w, r)
			return
		}

		if f, err := distFS.Open(strings.TrimPrefix(path, "/")); err == nil {
			f.Close()
			fsHandler.ServeHTTP(w, r)
			return
		}

		// SPA fallback
		r.URL.Path = "/"
		fsHandler.ServeHTTP(w, r)
	})
}
