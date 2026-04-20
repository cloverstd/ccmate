package gitprovider

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/cloverstd/ccmate/internal/settings"
)

// Manager holds the currently active GitProvider and allows hot-reloading it
// when settings change — consumers call Current() on each use so they always
// see the latest provider without needing to be re-constructed.
type Manager struct {
	registry *Registry
	current  atomic.Pointer[providerHolder]
}

type providerHolder struct {
	p GitProvider
}

// NewManager creates a Manager backed by the given factory Registry.
func NewManager(registry *Registry) *Manager {
	m := &Manager{registry: registry}
	m.current.Store(&providerHolder{})
	return m
}

// Current returns the active provider, or nil if none has been configured.
func (m *Manager) Current() GitProvider {
	h := m.current.Load()
	if h == nil {
		return nil
	}
	return h.p
}

// Rebuild reads the latest github-related settings from the DB, constructs a
// fresh provider, and atomically swaps it in. A failure to build leaves the
// previous provider in place; a fully-unconfigured state clears the provider.
func (m *Manager) Rebuild(ctx context.Context, settingsMgr *settings.Manager) {
	personalToken := settingsMgr.GetWithDefault(ctx, settings.KeyGitHubPersonalToken, "")
	webhookSecret := settingsMgr.GetWithDefault(ctx, settings.KeyGitHubWebhookSecret, "")
	appID := settingsMgr.GetWithDefault(ctx, settings.KeyGitHubAppID, "")

	if personalToken == "" && appID == "" {
		m.current.Store(&providerHolder{})
		slog.Info("git provider cleared (no credentials configured)")
		return
	}

	prov, err := m.registry.Create("github", ProviderConfig{
		WebhookSecret: webhookSecret,
		PersonalToken: personalToken,
	})
	if err != nil {
		slog.Warn("failed to rebuild github provider, keeping previous", "error", err)
		return
	}
	m.current.Store(&providerHolder{p: prov})
	slog.Info("git provider rebuilt from latest settings")
}
