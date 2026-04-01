package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cloverstd/ccmate/internal/auth"
	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type AuthHandler struct {
	client     *ent.Client
	cfg        *config.Config
	passkeySvc *auth.PasskeyService

	// In-memory session store for WebAuthn ceremony
	regSession   *webauthn.SessionData
	loginSession *webauthn.SessionData
}

func NewAuthHandler(client *ent.Client, cfg *config.Config, passkeySvc *auth.PasskeyService) *AuthHandler {
	return &AuthHandler{client: client, cfg: cfg, passkeySvc: passkeySvc}
}

func (h *AuthHandler) RegisterStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BootstrapToken string `json:"bootstrap_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if !h.passkeySvc.ValidateBootstrapToken(body.BootstrapToken) {
		http.Error(w, `{"error":"invalid bootstrap token"}`, http.StatusForbidden)
		return
	}

	session, options, err := h.passkeySvc.BeginRegistration(r.Context())
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	h.regSession = session
	writeJSON(w, http.StatusOK, options)
}

func (h *AuthHandler) RegisterFinish(w http.ResponseWriter, r *http.Request) {
	if h.regSession == nil {
		http.Error(w, `{"error":"no pending registration"}`, http.StatusBadRequest)
		return
	}

	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		http.Error(w, `{"error":"invalid credential response"}`, http.StatusBadRequest)
		return
	}

	user := h.passkeySvc.User()
	credential, err := h.passkeySvc.WebAuthn().CreateCredential(user, *h.regSession, parsedResponse)
	if err != nil {
		http.Error(w, `{"error":"credential verification failed"}`, http.StatusBadRequest)
		return
	}

	if err := h.passkeySvc.FinishRegistration(r.Context(), h.regSession, credential); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	h.regSession = nil

	// Issue session cookie
	h.setSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "registered"})
}

func (h *AuthHandler) LoginStart(w http.ResponseWriter, r *http.Request) {
	if !h.passkeySvc.IsRegistered() {
		http.Error(w, `{"error":"admin not registered"}`, http.StatusNotFound)
		return
	}

	session, options, err := h.passkeySvc.BeginLogin(r.Context())
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	h.loginSession = session
	writeJSON(w, http.StatusOK, options)
}

func (h *AuthHandler) LoginFinish(w http.ResponseWriter, r *http.Request) {
	if h.loginSession == nil {
		http.Error(w, `{"error":"no pending login"}`, http.StatusBadRequest)
		return
	}

	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(r.Body)
	if err != nil {
		http.Error(w, `{"error":"invalid credential response"}`, http.StatusBadRequest)
		return
	}

	user := h.passkeySvc.User()
	_, err = h.passkeySvc.WebAuthn().ValidateLogin(user, *h.loginSession, parsedResponse)
	if err != nil {
		http.Error(w, `{"error":"login verification failed"}`, http.StatusUnauthorized)
		return
	}

	h.loginSession = nil

	h.setSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "authenticated"})
}

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter) {
	encoded, err := h.passkeySvc.EncodeSession("ccmate_session", map[string]string{
		"user": "admin",
		"ts":   time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "ccmate_session",
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7, // 7 days
	})
}
