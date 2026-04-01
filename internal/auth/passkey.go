package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"

	"github.com/cloverstd/ccmate/internal/config"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/gorilla/securecookie"
)

// PasskeyService handles WebAuthn registration and authentication.
type PasskeyService struct {
	webAuthn     *webauthn.WebAuthn
	secureCookie *securecookie.SecureCookie

	mu   sync.RWMutex
	user *AdminUser // single admin user

	// Bootstrap token for first-time registration
	bootstrapToken string
	registered     bool
}

// AdminUser implements webauthn.User for the single admin.
type AdminUser struct {
	ID          []byte
	Name        string
	DisplayName string
	Credentials []webauthn.Credential
}

func (u *AdminUser) WebAuthnID() []byte                         { return u.ID }
func (u *AdminUser) WebAuthnName() string                       { return u.Name }
func (u *AdminUser) WebAuthnDisplayName() string                { return u.DisplayName }
func (u *AdminUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

// NewPasskeyService creates a new Passkey authentication service.
func NewPasskeyService(cfg *config.AuthConfig) (*PasskeyService, error) {
	wconfig := &webauthn.Config{
		RPDisplayName: cfg.RPDisplayName,
		RPID:          cfg.RPID,
		RPOrigins:     cfg.RPOrigins,
	}

	w, err := webauthn.New(wconfig)
	if err != nil {
		return nil, fmt.Errorf("creating webauthn: %w", err)
	}

	// Derive session encryption key
	sessionKey := []byte(cfg.SessionKey)
	if len(sessionKey) == 0 {
		sessionKey = securecookie.GenerateRandomKey(32)
	}
	sc := securecookie.New(sessionKey, securecookie.GenerateRandomKey(32))

	// Generate bootstrap token
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generating bootstrap token: %w", err)
	}
	bootstrapToken := hex.EncodeToString(tokenBytes)

	slog.Info("bootstrap token generated (use this for first-time admin registration)",
		"token", bootstrapToken)

	return &PasskeyService{
		webAuthn:       w,
		secureCookie:   sc,
		bootstrapToken: bootstrapToken,
	}, nil
}

// IsRegistered returns whether the admin has been registered.
func (s *PasskeyService) IsRegistered() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.registered
}

// ValidateBootstrapToken checks if the provided token matches.
func (s *PasskeyService) ValidateBootstrapToken(token string) bool {
	return s.bootstrapToken != "" && token == s.bootstrapToken
}

// BeginRegistration starts the WebAuthn registration process.
func (s *PasskeyService) BeginRegistration(ctx context.Context) (*webauthn.SessionData, interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.registered {
		return nil, nil, fmt.Errorf("admin already registered")
	}

	userID := make([]byte, 32)
	if _, err := rand.Read(userID); err != nil {
		return nil, nil, fmt.Errorf("generating user ID: %w", err)
	}

	s.user = &AdminUser{
		ID:          userID,
		Name:        "admin",
		DisplayName: "Admin",
	}

	options, session, err := s.webAuthn.BeginRegistration(s.user)
	if err != nil {
		return nil, nil, fmt.Errorf("begin registration: %w", err)
	}

	return session, options, nil
}

// FinishRegistration completes the WebAuthn registration.
func (s *PasskeyService) FinishRegistration(ctx context.Context, session *webauthn.SessionData, credential *webauthn.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.user == nil {
		return fmt.Errorf("no pending registration")
	}

	s.user.Credentials = append(s.user.Credentials, *credential)
	s.registered = true
	s.bootstrapToken = "" // Invalidate bootstrap token

	slog.Info("admin registered successfully")
	return nil
}

// BeginLogin starts the WebAuthn login process.
func (s *PasskeyService) BeginLogin(ctx context.Context) (*webauthn.SessionData, interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.registered || s.user == nil {
		return nil, nil, fmt.Errorf("admin not registered")
	}

	options, session, err := s.webAuthn.BeginLogin(s.user)
	if err != nil {
		return nil, nil, fmt.Errorf("begin login: %w", err)
	}

	return session, options, nil
}

// EncodeSession creates a signed session cookie value.
func (s *PasskeyService) EncodeSession(name string, value map[string]string) (string, error) {
	return s.secureCookie.Encode(name, value)
}

// DecodeSession validates and decodes a session cookie value.
func (s *PasskeyService) DecodeSession(name, value string) (map[string]string, error) {
	var result map[string]string
	err := s.secureCookie.Decode(name, value, &result)
	return result, err
}

// WebAuthn returns the underlying webauthn instance.
func (s *PasskeyService) WebAuthn() *webauthn.WebAuthn {
	return s.webAuthn
}

// User returns the admin user.
func (s *PasskeyService) User() *AdminUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.user
}

// ResetAdmin clears the admin registration, allowing re-registration.
func (s *PasskeyService) ResetAdmin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.user = nil
	s.registered = false

	// Generate new bootstrap token
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return
	}
	s.bootstrapToken = hex.EncodeToString(tokenBytes)

	slog.Info("admin registration reset, new bootstrap token generated",
		"token", s.bootstrapToken)
}
