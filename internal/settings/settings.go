package settings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/systemsetting"
)

// Keys for system settings stored in DB.
const (
	KeyInitialized       = "initialized"
	KeySetupToken        = "setup_token"
	KeyDefaultAgentProfileID = "default_agent_profile_id"
	KeyGitHubClientID    = "github_client_id"
	KeyGitHubClientSecret = "github_client_secret"
	KeyGitHubCallbackURL = "github_callback_url"
	KeyAllowedUsers      = "allowed_users"       // JSON array
	KeyGitHubWebhookSecret = "github_webhook_secret"
	KeyGitHubPersonalToken = "github_personal_token"
	KeyGitHubAppID       = "github_app_id"
	KeyGitHubInstallationID = "github_installation_id"
	KeyGitHubPrivateKeyPath = "github_private_key_path"
	KeyGitCommitAuthorName  = "git_commit_author_name"
	KeyGitCommitAuthorEmail = "git_commit_author_email"
	KeyAgentProviders    = "agent_providers"      // JSON array
	KeyLabelRules        = "label_rules"          // JSON array
	KeyMaxConcurrency    = "max_concurrency"
	KeyTaskTimeoutMin    = "task_timeout_minutes"
	KeyMaxLogSizeMB      = "max_log_size_mb"
	KeyMaxAttachmentMB   = "max_attachment_size_mb"
	KeyStorageBasePath   = "storage_base_path"
	KeyDefaultPromptTemplateID = "default_prompt_template_id"
	KeyDebugMode               = "debug_mode"
	KeyRPDisplayName     = "rp_display_name"
	KeyRPID              = "rp_id"
	KeyRPOrigins         = "rp_origins"           // JSON array
	KeySessionKey        = "session_key"

	// Notifications
	KeyNotifyEnabledStatuses = "notify_enabled_statuses" // JSON array
	KeyNotifyBaseURL         = "notify_base_url"
	KeyNotifyTelegramEnabled  = "notify_telegram_enabled"
	KeyNotifyTelegramBotToken = "notify_telegram_bot_token"
	KeyNotifyTelegramChatID   = "notify_telegram_chat_id"
)

// Manager manages system settings in the database.
type Manager struct {
	client *ent.Client
	mu     sync.RWMutex
	cache  map[string]string
}

func NewManager(client *ent.Client) *Manager {
	return &Manager{client: client, cache: make(map[string]string)}
}

// ClearCache clears the in-memory cache so fresh values are read from DB.
func (m *Manager) ClearCache() {
	m.mu.Lock()
	m.cache = make(map[string]string)
	m.mu.Unlock()
}

// IsInitialized checks if the system has been set up.
func (m *Manager) IsInitialized(ctx context.Context) bool {
	v, _ := m.Get(ctx, KeyInitialized)
	return v == "true"
}

// EnsureSetupToken generates a setup token if not initialized, returns it.
func (m *Manager) EnsureSetupToken(ctx context.Context) string {
	if m.IsInitialized(ctx) {
		return ""
	}

	existing, _ := m.Get(ctx, KeySetupToken)
	if existing != "" {
		return existing
	}

	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	m.Set(ctx, KeySetupToken, token)
	return token
}

// ValidateSetupToken checks if the provided token matches.
func (m *Manager) ValidateSetupToken(ctx context.Context, token string) bool {
	stored, _ := m.Get(ctx, KeySetupToken)
	return stored != "" && stored == token
}

// MarkInitialized marks the system as initialized and clears the setup token.
func (m *Manager) MarkInitialized(ctx context.Context) {
	m.Set(ctx, KeyInitialized, "true")
	m.Delete(ctx, KeySetupToken)
}

// Get retrieves a setting value.
func (m *Manager) Get(ctx context.Context, key string) (string, error) {
	m.mu.RLock()
	if v, ok := m.cache[key]; ok {
		m.mu.RUnlock()
		return v, nil
	}
	m.mu.RUnlock()

	setting, err := m.client.SystemSetting.Query().
		Where(systemsetting.Key(key)).Only(ctx)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	m.cache[key] = setting.Value
	m.mu.Unlock()

	return setting.Value, nil
}

// GetWithDefault retrieves a setting value with a default fallback.
func (m *Manager) GetWithDefault(ctx context.Context, key, defaultVal string) string {
	v, err := m.Get(ctx, key)
	if err != nil || v == "" {
		return defaultVal
	}
	return v
}

func (m *Manager) GetOptionalInt(ctx context.Context, key string) *int {
	v, err := m.Get(ctx, key)
	if err != nil || v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}

// Set stores a setting value.
func (m *Manager) Set(ctx context.Context, key, value string) error {
	_, err := m.client.SystemSetting.Create().
		SetKey(key).SetValue(value).
		Save(ctx)
	if err != nil {
		// Key exists, update it
		_, err = m.client.SystemSetting.Update().
			Where(systemsetting.Key(key)).
			SetValue(value).
			Save(ctx)
	}

	m.mu.Lock()
	m.cache[key] = value
	m.mu.Unlock()

	return err
}

// Delete removes a setting.
func (m *Manager) Delete(ctx context.Context, key string) {
	m.client.SystemSetting.Delete().Where(systemsetting.Key(key)).Exec(ctx)
	m.mu.Lock()
	delete(m.cache, key)
	m.mu.Unlock()
}

// GetJSONArray retrieves a JSON array setting.
func (m *Manager) GetJSONArray(ctx context.Context, key string) []string {
	v, _ := m.Get(ctx, key)
	if v == "" {
		return nil
	}
	var arr []string
	json.Unmarshal([]byte(v), &arr)
	return arr
}

// SetJSONArray stores a JSON array setting.
func (m *Manager) SetJSONArray(ctx context.Context, key string, arr []string) {
	data, _ := json.Marshal(arr)
	m.Set(ctx, key, string(data))
}

// IsUserAllowed checks if a GitHub username is in the allowed list.
func (m *Manager) IsUserAllowed(ctx context.Context, username string) bool {
	users := m.GetJSONArray(ctx, KeyAllowedUsers)
	if len(users) == 0 {
		return false
	}
	for _, u := range users {
		if u == username {
			return true
		}
	}
	return false
}

// GetGitHubCallbackURL returns the full callback URL.
// If configured, appends the callback path to the base URL.
// Otherwise uses the default origin.
func (m *Manager) GetGitHubCallbackURL(ctx context.Context, defaultOrigin string) string {
	const callbackPath = "/api/auth/github/callback"
	base, _ := m.Get(ctx, KeyGitHubCallbackURL)
	if base != "" {
		// Strip trailing slash
		if base[len(base)-1] == '/' {
			base = base[:len(base)-1]
		}
		return base + callbackPath
	}
	return defaultOrigin + callbackPath
}

// SetupRequest contains all fields for initial system setup.
type SetupRequest struct {
	// GitHub OAuth
	GitHubClientID     string `json:"github_client_id"`
	GitHubClientSecret string `json:"github_client_secret"`
	GitHubCallbackURL  string `json:"github_callback_url"`
	AllowedUsers       []string `json:"allowed_users"`

	// GitHub API (for webhook + repo operations)
	GitHubWebhookSecret  string `json:"github_webhook_secret"`
	GitHubPersonalToken  string `json:"github_personal_token"`

	// Agent
	AgentProvider string `json:"agent_provider"`
	AgentBinary   string `json:"agent_binary"`

	// Label rules
	LabelRules []LabelRule `json:"label_rules"`

	// Limits
	MaxConcurrency     int `json:"max_concurrency"`
	TaskTimeoutMinutes int `json:"task_timeout_minutes"`

	// WebAuthn
	RPDisplayName string `json:"rp_display_name"`
	RPID          string `json:"rp_id"`
	RPOrigin      string `json:"rp_origin"`
}

type LabelRule struct {
	Label       string `json:"label"`
	TriggerMode string `json:"trigger_mode"`
}

// ApplySetup saves all setup fields to the database.
func (m *Manager) ApplySetup(ctx context.Context, req SetupRequest) error {
	sets := map[string]string{
		KeyGitHubClientID:     req.GitHubClientID,
		KeyGitHubClientSecret: req.GitHubClientSecret,
		KeyGitHubCallbackURL:  req.GitHubCallbackURL,
		KeyGitHubWebhookSecret: req.GitHubWebhookSecret,
		KeyGitHubPersonalToken: req.GitHubPersonalToken,
		KeyMaxConcurrency:     fmt.Sprintf("%d", req.MaxConcurrency),
		KeyTaskTimeoutMin:     fmt.Sprintf("%d", req.TaskTimeoutMinutes),
		KeyRPDisplayName:      req.RPDisplayName,
		KeyRPID:               req.RPID,
	}

	if req.MaxConcurrency == 0 {
		sets[KeyMaxConcurrency] = "2"
	}
	if req.TaskTimeoutMinutes == 0 {
		sets[KeyTaskTimeoutMin] = "60"
	}
	if req.RPDisplayName == "" {
		sets[KeyRPDisplayName] = "ccmate"
	}

	for k, v := range sets {
		if err := m.Set(ctx, k, v); err != nil {
			slog.Error("failed to save setting", "key", k, "error", err)
		}
	}

	// JSON arrays
	if len(req.AllowedUsers) > 0 {
		m.SetJSONArray(ctx, KeyAllowedUsers, req.AllowedUsers)
	}
	if req.RPOrigin != "" {
		m.SetJSONArray(ctx, KeyRPOrigins, []string{req.RPOrigin})
	}

	// Agent providers
	if req.AgentProvider != "" {
		providers, _ := json.Marshal([]map[string]string{
			{"name": req.AgentProvider, "binary": req.AgentBinary},
		})
		m.Set(ctx, KeyAgentProviders, string(providers))
	}

	// Label rules
	if len(req.LabelRules) > 0 {
		data, _ := json.Marshal(req.LabelRules)
		m.Set(ctx, KeyLabelRules, string(data))
	}

	m.MarkInitialized(ctx)
	slog.Info("system setup completed")
	return nil
}

// GetLabelRules returns the configured label rules.
func (m *Manager) GetLabelRules(ctx context.Context) []LabelRule {
	v, _ := m.Get(ctx, KeyLabelRules)
	if v == "" {
		return nil
	}
	var rules []LabelRule
	json.Unmarshal([]byte(v), &rules)
	return rules
}

// SensitiveKeys lists keys whose values should be masked in API responses.
var SensitiveKeys = map[string]bool{
	KeyGitHubClientSecret:     true,
	KeyGitHubPersonalToken:    true,
	KeySessionKey:             true,
	KeySetupToken:             true,
	KeyGitHubWebhookSecret:    true,
	KeyNotifyTelegramBotToken: true,
}

// GetAllSettings returns all settings as a map with sensitive values masked.
func (m *Manager) GetAllSettings(ctx context.Context) map[string]string {
	all, err := m.client.SystemSetting.Query().All(ctx)
	if err != nil {
		return nil
	}
	result := make(map[string]string, len(all))
	for _, s := range all {
		if s.Key == KeyInitialized || s.Key == KeySetupToken {
			continue // skip internal keys
		}
		if SensitiveKeys[s.Key] && s.Value != "" {
			result[s.Key] = "***"
		} else {
			result[s.Key] = s.Value
		}
	}
	return result
}
