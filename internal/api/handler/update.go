package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cloverstd/ccmate/internal/updater"
)

type UpdateHandler struct {
	u *updater.Updater
}

func NewUpdateHandler(currentVersion string) *UpdateHandler {
	return &UpdateHandler{u: updater.New(currentVersion)}
}

// Info returns current version, detected platform, and the latest release.
func (h *UpdateHandler) Info(w http.ResponseWriter, r *http.Request) {
	info := h.u.BuildInfo(r.Context())
	writeJSON(w, http.StatusOK, info)
}

// Releases returns the most recent releases (with release notes).
func (h *UpdateHandler) Releases(w http.ResponseWriter, r *http.Request) {
	releases, err := h.u.ListReleases(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, releases)
}

// Apply downloads the chosen release and replaces the running binary, then
// schedules a restart so the supervisor (systemd/docker) starts the new
// binary. Requires a service manager to relaunch — the response signals
// that to the UI.
func (h *UpdateHandler) Apply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	if err := h.u.Apply(r.Context(), body.Tag); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "installed",
		"restart_seconds": 2,
		"tag":             body.Tag,
	})
	// Give the HTTP response a moment to flush before signalling shutdown.
	updater.ScheduleRestart(2 * time.Second)
}
