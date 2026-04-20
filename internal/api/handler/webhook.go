package handler

import (
	"log/slog"
	"net/http"

	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/scheduler"
	"github.com/cloverstd/ccmate/internal/settings"
	"github.com/cloverstd/ccmate/internal/webhook"
)

type WebhookHandler struct {
	client      *ent.Client
	cfg         *config.Config
	sched       *scheduler.Scheduler
	gitProvMgr  *gitprovider.Manager
	settingsMgr *settings.Manager
}

func NewWebhookHandler(client *ent.Client, cfg *config.Config, sched *scheduler.Scheduler, settingsMgr *settings.Manager) *WebhookHandler {
	return &WebhookHandler{client: client, cfg: cfg, sched: sched, settingsMgr: settingsMgr}
}

func (h *WebhookHandler) SetGitProviderManager(mgr *gitprovider.Manager) {
	h.gitProvMgr = mgr
}

func (h *WebhookHandler) HandleGitHub(w http.ResponseWriter, r *http.Request) {
	var gitProv gitprovider.GitProvider
	if h.gitProvMgr != nil {
		gitProv = h.gitProvMgr.Current()
	}
	if gitProv == nil {
		slog.Error("git provider not configured")
		http.Error(w, `{"error":"git provider not configured"}`, http.StatusInternalServerError)
		return
	}

	event, err := gitProv.VerifyWebhook(r)
	if err != nil {
		slog.Warn("webhook verification failed", "error", err)
		http.Error(w, `{"error":"webhook verification failed"}`, http.StatusUnauthorized)
		return
	}

	processor := webhook.NewProcessor(h.client, gitProv, h.settingsMgr)
	if err := processor.ProcessEvent(r.Context(), event); err != nil {
		slog.Error("failed to process webhook event", "error", err)
		http.Error(w, `{"error":"processing failed"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
