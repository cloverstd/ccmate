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

// PasskeyService handles WebAuthn registration and session management.
type PasskeyService struct {
	webAuthn     *webauthn.WebAuthn
	secureCookie *securecookie.SecureCookie

	mu    sync.RWMutex
	users map[string]*AdminUser // github_login -> AdminUser
}

// AdminUser implements webauthn.User.
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

// NewPasskeyService creates a new Passkey + session service.
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

	sessionKey := []byte(cfg.SessionKey)
	if len(sessionKey) == 0 {
		sessionKey = securecookie.GenerateRandomKey(32)
	}
	sc := securecookie.New(sessionKey, securecookie.GenerateRandomKey(32))

	return &PasskeyService{
		webAuthn:     w,
		secureCookie: sc,
		users:        make(map[string]*AdminUser),
	}, nil
}

// HasPasskey checks if a user has registered a passkey.
func (s *PasskeyService) HasPasskey(username string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	return ok && len(u.Credentials) > 0
}

// BeginRegistration starts passkey registration for a logged-in user.
func (s *PasskeyService) BeginRegistration(ctx context.Context, username string) (*webauthn.SessionData, interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[username]
	if !ok {
		userID := make([]byte, 32)
		if _, err := rand.Read(userID); err != nil {
			return nil, nil, fmt.Errorf("generating user ID: %w", err)
		}
		user = &AdminUser{ID: userID, Name: username, DisplayName: username}
		s.users[username] = user
	}

	options, session, err := s.webAuthn.BeginRegistration(user)
	if err != nil {
		return nil, nil, fmt.Errorf("begin registration: %w", err)
	}

	return session, options, nil
}

// FinishRegistration completes passkey registration.
func (s *PasskeyService) FinishRegistration(ctx context.Context, username string, session *webauthn.SessionData, credential *webauthn.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[username]
	if !ok {
		return fmt.Errorf("user not found")
	}

	user.Credentials = append(user.Credentials, *credential)
	slog.Info("passkey registered", "user", username)
	return nil
}

// BeginLogin starts passkey login for a user.
func (s *PasskeyService) BeginLogin(ctx context.Context, username string) (*webauthn.SessionData, interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[username]
	if !ok || len(user.Credentials) == 0 {
		return nil, nil, fmt.Errorf("no passkey registered for user")
	}

	options, session, err := s.webAuthn.BeginLogin(user)
	if err != nil {
		return nil, nil, fmt.Errorf("begin login: %w", err)
	}

	return session, options, nil
}

// GetUser returns the user for passkey validation.
func (s *PasskeyService) GetUser(username string) *AdminUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.users[username]
}

// RemovePasskey removes all passkeys for a user.
func (s *PasskeyService) RemovePasskey(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, username)
	slog.Info("passkey removed", "user", username)
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

// ResetAdmin clears all passkey registrations.
func (s *PasskeyService) ResetAdmin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = make(map[string]*AdminUser)

	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes)
	slog.Info("all passkey registrations cleared", "token", hex.EncodeToString(tokenBytes))
}
