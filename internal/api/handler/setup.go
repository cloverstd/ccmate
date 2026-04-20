package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/settings"
)

type SetupHandler struct {
	mgr        *settings.Manager
	gitProvMgr *gitprovider.Manager
}

func NewSetupHandler(mgr *settings.Manager, gitProvMgr *gitprovider.Manager) *SetupHandler {
	return &SetupHandler{mgr: mgr, gitProvMgr: gitProvMgr}
}

func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"initialized": h.mgr.IsInitialized(r.Context())})
}

func (h *SetupHandler) Setup(w http.ResponseWriter, r *http.Request) {
	if h.mgr.IsInitialized(r.Context()) {
		http.Error(w, `{"error":"system already initialized"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		Token string               `json:"token"`
		Setup settings.SetupRequest `json:"setup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	if !h.mgr.ValidateSetupToken(r.Context(), body.Token) {
		http.Error(w, `{"error":"invalid setup token"}`, http.StatusForbidden)
		return
	}
	if err := h.mgr.ApplySetup(r.Context(), body.Setup); err != nil {
		http.Error(w, `{"error":"setup failed"}`, http.StatusInternalServerError)
		return
	}
	if h.gitProvMgr != nil {
		h.gitProvMgr.Rebuild(r.Context(), h.mgr)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "initialized"})
}

// GetSettings returns all settings with sensitive values masked from backend.
func (h *SetupHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	all := h.mgr.GetAllSettings(r.Context())
	writeJSON(w, http.StatusOK, all)
}

// UpdateSettings batch-updates multiple settings.
func (h *SetupHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	for k, v := range body {
		if k == settings.KeyInitialized || k == settings.KeySetupToken {
			continue
		}
		// Skip masked values (don't overwrite with "***")
		if v == "***" {
			continue
		}
		h.mgr.Set(ctx, k, v)
	}

	// Clear cache to reflect changes
	h.mgr.ClearCache()

	// Rebuild provider holders so settings changes take effect without restart.
	if h.gitProvMgr != nil {
		h.gitProvMgr.Rebuild(ctx, h.mgr)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// CheckGitHubPermissions tests the configured PAT and returns its permissions.
func (h *SetupHandler) CheckGitHubPermissions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := h.mgr.GetWithDefault(ctx, settings.KeyGitHubPersonalToken, "")
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"configured": false,
			"error":      "no personal token configured",
		})
		return
	}

	// Test token by calling GitHub API
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"configured": true, "valid": false, "error": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"configured": true, "valid": false,
			"error":      fmt.Sprintf("GitHub API returned %d", resp.StatusCode),
		})
		return
	}

	var ghUser struct {
		Login string `json:"login"`
	}
	json.NewDecoder(resp.Body).Decode(&ghUser)

	// Check scopes from response headers
	scopes := resp.Header.Get("X-OAuth-Scopes")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"configured": true,
		"valid":      true,
		"user":       ghUser.Login,
		"scopes":     scopes,
	})
}
