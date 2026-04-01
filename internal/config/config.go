package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Server   ServerConfig   `koanf:"server"`
	Database DatabaseConfig `koanf:"database"`
	GitHub   GitHubConfig   `koanf:"github"`
	Agent    AgentConfig    `koanf:"agent"`
	Storage  StorageConfig  `koanf:"storage"`
	Auth     AuthConfig     `koanf:"auth"`
	Limits   LimitsConfig   `koanf:"limits"`
}

type ServerConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

func (s ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

type DatabaseConfig struct {
	Driver string `koanf:"driver"`
	DSN    string `koanf:"dsn"`
}

type GitHubConfig struct {
	AppID          int64  `koanf:"app_id"`
	InstallationID int64  `koanf:"installation_id"`
	PrivateKeyPath string `koanf:"private_key_path"`
	WebhookSecret  string `koanf:"webhook_secret"`
	PersonalToken  string `koanf:"personal_token"`
}

type AgentProviderConfig struct {
	Name   string            `koanf:"name"`
	Binary string            `koanf:"binary"`
	Extra  map[string]string `koanf:"extra"`
}

type AgentConfig struct {
	Providers []AgentProviderConfig `koanf:"providers"`
}

type StorageConfig struct {
	BasePath       string `koanf:"base_path"`
	AttachmentsDir string `koanf:"attachments_dir"`
	LogsDir        string `koanf:"logs_dir"`
	WorkspacesDir  string `koanf:"workspaces_dir"`
}

type AuthConfig struct {
	RPDisplayName string   `koanf:"rp_display_name"`
	RPID          string   `koanf:"rp_id"`
	RPOrigins     []string `koanf:"rp_origins"`
	SessionKey    string   `koanf:"session_key"`
}

type LimitsConfig struct {
	TaskTimeoutMinutes    int `koanf:"task_timeout_minutes"`
	MaxLogSizeMB          int `koanf:"max_log_size_mb"`
	MaxAttachmentSizeMB   int `koanf:"max_attachment_size_mb"`
	MaxTotalAttachmentsMB int `koanf:"max_total_attachments_mb"`
	DefaultMaxConcurrency int `koanf:"default_max_concurrency"`
	RetentionSuccessDays  int `koanf:"retention_success_days"`
	RetentionFailureDays  int `koanf:"retention_failure_days"`
	RetentionAuditDays    int `koanf:"retention_audit_days"`
}

func Load(path string) (*Config, error) {
	k := koanf.New(".")

	// Load YAML config file
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("loading config file: %w", err)
		}
	}

	// Override with environment variables (CCMATE_SERVER_PORT -> server.port)
	if err := k.Load(env.Provider("CCMATE_", ".", func(s string) string {
		return strings.Replace(
			strings.ToLower(strings.TrimPrefix(s, "CCMATE_")),
			"_", ".", -1,
		)
	}), nil); err != nil {
		return nil, fmt.Errorf("loading env vars: %w", err)
	}

	cfg := DefaultConfig()
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	return cfg, nil
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Database: DatabaseConfig{
			Driver: "sqlite3",
			DSN:    "data/ccmate.db?_fk=1&_journal=WAL",
		},
		Storage: StorageConfig{
			BasePath:       "data",
			AttachmentsDir: "attachments",
			LogsDir:        "logs",
			WorkspacesDir:  "workspaces",
		},
		Auth: AuthConfig{
			RPDisplayName: "ccmate",
			RPID:          "localhost",
			RPOrigins:     []string{"http://localhost:8080"},
		},
		Limits: LimitsConfig{
			TaskTimeoutMinutes:    60,
			MaxLogSizeMB:          50,
			MaxAttachmentSizeMB:   10,
			MaxTotalAttachmentsMB: 30,
			DefaultMaxConcurrency: 2,
			RetentionSuccessDays:  7,
			RetentionFailureDays:  30,
			RetentionAuditDays:    180,
		},
	}
}
