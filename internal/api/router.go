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
	"github.com/cloverstd/ccmate/internal/settings"
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
	settingsMgr *settings.Manager,
) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS)

	authHandler := handler.NewAuthHandler(client, cfg, passkeySvc, settingsMgr)
	projectHandler := handler.NewProjectHandler(client, gitProv, settingsMgr)
	taskHandler := handler.NewTaskHandler(client, cfg, broker, sched, gitProv, settingsMgr)
	webhookHandler := handler.NewWebhookHandler(client, cfg, sched)
	setupHandler := handler.NewSetupHandler(settingsMgr, gitProv)
	if gitProv != nil {
		webhookHandler.SetGitProvider(gitProv)
	}

	// Webhook
	r.Post("/webhooks/github", webhookHandler.HandleGitHub)

	// Metrics
	r.Get("/metrics", handler.MetricsHandler(client))

	// All /api routes
	r.Route("/api", func(r chi.Router) {
		// --- Public routes ---
		r.Group(func(r chi.Router) {
			// Setup
			r.Get("/setup/status", setupHandler.Status)
			r.Post("/setup", setupHandler.Setup)

			// Auth
			r.Get("/auth/github/start", authHandler.GitHubOAuthStart)
			r.Get("/auth/github/callback", authHandler.GitHubOAuthCallback)
			r.Post("/auth/passkey/login/start", authHandler.PasskeyLoginStart)
			r.Post("/auth/passkey/login/finish", authHandler.PasskeyLoginFinish)
			r.Get("/auth/me", authHandler.GetCurrentUser)
			r.Post("/auth/logout", authHandler.Logout)
		})

		// --- Protected routes ---
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(passkeySvc))

			// Passkey management
			r.Post("/auth/passkey/register/start", authHandler.PasskeyRegisterStart)
			r.Post("/auth/passkey/register/finish", authHandler.PasskeyRegisterFinish)
			r.Delete("/auth/passkey", authHandler.PasskeyRemove)

			// Settings
			r.Get("/settings", setupHandler.GetSettings)
			r.Put("/settings", setupHandler.UpdateSettings)
			r.Get("/github/permissions", setupHandler.CheckGitHubPermissions)

			// GitHub repos
			r.Get("/github/repos", projectHandler.ListGitHubRepos)

			// Projects
			r.Get("/projects", projectHandler.List)
			r.Post("/projects", projectHandler.Create)
			r.Put("/projects/{id}", projectHandler.Update)
			r.Get("/projects/{id}", projectHandler.Get)
			r.Get("/projects/{id}/tasks", projectHandler.ListProjectTasks)
			r.Get("/projects/{id}/issues", projectHandler.ListRepoIssues)
			r.Get("/projects/{id}/pulls", projectHandler.ListRepoPRs)
			r.Get("/projects/{id}/git-info", projectHandler.GetRepoInfo)
			r.Get("/projects/{id}/commits", projectHandler.GetBranchCommits)
			r.Post("/projects/{id}/pull", projectHandler.PullRepo)

			// Global label rules
			r.Get("/label-rules", projectHandler.ListGlobalLabelRules)

			// Prompt templates
			r.Get("/prompt-templates", projectHandler.ListPromptTemplates)
			r.Post("/prompt-templates", projectHandler.CreatePromptTemplate)
			r.Put("/prompt-templates/{id}", projectHandler.UpdatePromptTemplate)
			r.Delete("/prompt-templates/{id}", projectHandler.DeletePromptTemplate)

			// Tasks
			r.Get("/tasks", taskHandler.List)
			r.Post("/tasks", taskHandler.Create)
			r.Post("/tasks/from-prompt", taskHandler.CreateFromPrompt)
			r.Get("/tasks/{id}", taskHandler.Get)
			r.Post("/tasks/{id}/pause", taskHandler.Pause)
			r.Post("/tasks/{id}/resume", taskHandler.Resume)
			r.Post("/tasks/{id}/retry", taskHandler.Retry)
			r.Post("/tasks/{id}/cancel", taskHandler.Cancel)
			r.Post("/tasks/{id}/complete", taskHandler.Complete)
			r.Post("/tasks/{id}/messages", taskHandler.SendMessage)
			r.Post("/tasks/{id}/attachments", taskHandler.UploadAttachment)
			r.Get("/tasks/{id}/events/stream", taskHandler.EventStream)

			// Models
			r.Get("/models", projectHandler.ListModels)
			r.Post("/models", projectHandler.CreateModel)
			r.Put("/models/{id}", projectHandler.UpdateModel)
			r.Delete("/models/{id}", projectHandler.DeleteModel)
		})
	})

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
		r.URL.Path = "/"
		fsHandler.ServeHTTP(w, r)
	})
}
