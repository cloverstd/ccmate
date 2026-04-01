package handler

import (
	"encoding/json"
	"fmt"
	"io"
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

	// In-memory WebAuthn ceremony sessions (keyed by username)
	regSessions   map[string]*webauthn.SessionData
	loginSessions map[string]*webauthn.SessionData
}

func NewAuthHandler(client *ent.Client, cfg *config.Config, passkeySvc *auth.PasskeyService) *AuthHandler {
	return &AuthHandler{
		client: client, cfg: cfg, passkeySvc: passkeySvc,
		regSessions: make(map[string]*webauthn.SessionData),
		loginSessions: make(map[string]*webauthn.SessionData),
	}
}

// --- GitHub OAuth ---

// GitHubOAuthStart redirects the user to GitHub for OAuth authorization.
func (h *AuthHandler) GitHubOAuthStart(w http.ResponseWriter, r *http.Request) {
	clientID := h.cfg.Auth.GitHubClientID
	if clientID == "" {
		http.Error(w, `{"error":"github oauth not configured"}`, http.StatusInternalServerError)
		return
	}

	redirectURI := h.cfg.Auth.RPOrigins[0] + "/api/auth/github/callback"
	url := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user",
		clientID, redirectURI,
	)

	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// GitHubOAuthCallback handles the OAuth callback from GitHub.
func (h *AuthHandler) GitHubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, `{"error":"missing code"}`, http.StatusBadRequest)
		return
	}

	// Exchange code for access token
	tokenResp, err := http.Post(
		fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s",
			h.cfg.Auth.GitHubClientID, h.cfg.Auth.GitHubClientSecret, code),
		"application/json",
		nil,
	)
	if err != nil {
		http.Error(w, `{"error":"token exchange failed"}`, http.StatusInternalServerError)
		return
	}
	defer tokenResp.Body.Close()

	body, _ := io.ReadAll(tokenResp.Body)
	params := parseFormBody(string(body))
	accessToken := params["access_token"]
	if accessToken == "" {
		http.Error(w, `{"error":"no access token received"}`, http.StatusInternalServerError)
		return
	}

	// Get GitHub user info
	userReq, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	userReq.Header.Set("Authorization", "Bearer "+accessToken)
	userReq.Header.Set("Accept", "application/json")
	userResp, err := http.DefaultClient.Do(userReq)
	if err != nil {
		http.Error(w, `{"error":"failed to get user info"}`, http.StatusInternalServerError)
		return
	}
	defer userResp.Body.Close()

	var ghUser struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(userResp.Body).Decode(&ghUser); err != nil || ghUser.Login == "" {
		http.Error(w, `{"error":"invalid github user response"}`, http.StatusInternalServerError)
		return
	}

	// Check whitelist
	if !h.cfg.Auth.IsUserAllowed(ghUser.Login) {
		http.Error(w, fmt.Sprintf(`{"error":"user %s is not in the allowed list"}`, ghUser.Login), http.StatusForbidden)
		return
	}

	// Issue session cookie
	h.setSessionCookie(w, ghUser.Login)

	// Redirect to frontend
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// GetCurrentUser returns the current logged-in user info.
func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("ccmate_session")
	if err != nil {
		http.Error(w, `{"error":"not logged in"}`, http.StatusUnauthorized)
		return
	}

	session, err := h.passkeySvc.DecodeSession("ccmate_session", cookie.Value)
	if err != nil {
		http.Error(w, `{"error":"invalid session"}`, http.StatusUnauthorized)
		return
	}

	username := session["user"]
	hasPasskey := h.passkeySvc.HasPasskey(username)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":        username,
		"has_passkey": hasPasskey,
	})
}

// Logout clears the session cookie.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: "ccmate_session", Value: "", Path: "/",
		HttpOnly: true, MaxAge: -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

// --- Passkey (post-login) ---

// PasskeyRegisterStart begins passkey registration for the logged-in user.
func (h *AuthHandler) PasskeyRegisterStart(w http.ResponseWriter, r *http.Request) {
	username := h.currentUser(r)
	if username == "" {
		http.Error(w, `{"error":"not logged in"}`, http.StatusUnauthorized)
		return
	}

	session, options, err := h.passkeySvc.BeginRegistration(r.Context(), username)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	h.regSessions[username] = session
	writeJSON(w, http.StatusOK, options)
}

// PasskeyRegisterFinish completes passkey registration.
func (h *AuthHandler) PasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	username := h.currentUser(r)
	if username == "" {
		http.Error(w, `{"error":"not logged in"}`, http.StatusUnauthorized)
		return
	}

	session, ok := h.regSessions[username]
	if !ok {
		http.Error(w, `{"error":"no pending registration"}`, http.StatusBadRequest)
		return
	}

	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		http.Error(w, `{"error":"invalid credential response"}`, http.StatusBadRequest)
		return
	}

	user := h.passkeySvc.GetUser(username)
	credential, err := h.passkeySvc.WebAuthn().CreateCredential(user, *session, parsedResponse)
	if err != nil {
		http.Error(w, `{"error":"credential verification failed"}`, http.StatusBadRequest)
		return
	}

	if err := h.passkeySvc.FinishRegistration(r.Context(), username, session, credential); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	delete(h.regSessions, username)
	writeJSON(w, http.StatusOK, map[string]string{"status": "passkey_registered"})
}

// PasskeyLoginStart begins passkey login (alternative to GitHub OAuth).
func (h *AuthHandler) PasskeyLoginStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
		http.Error(w, `{"error":"username required"}`, http.StatusBadRequest)
		return
	}

	if !h.cfg.Auth.IsUserAllowed(body.Username) {
		http.Error(w, `{"error":"user not allowed"}`, http.StatusForbidden)
		return
	}

	session, options, err := h.passkeySvc.BeginLogin(r.Context(), body.Username)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	h.loginSessions[body.Username] = session
	writeJSON(w, http.StatusOK, options)
}

// PasskeyLoginFinish completes passkey login.
func (h *AuthHandler) PasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string          `json:"username"`
		Response json.RawMessage `json:"response_data"`
	}

	// Read body for both username extraction and credential parsing
	rawBody, _ := io.ReadAll(r.Body)
	json.Unmarshal(rawBody, &body)

	if body.Username == "" {
		http.Error(w, `{"error":"username required"}`, http.StatusBadRequest)
		return
	}

	session, ok := h.loginSessions[body.Username]
	if !ok {
		http.Error(w, `{"error":"no pending login"}`, http.StatusBadRequest)
		return
	}

	parsedResponse, err := protocol.ParseCredentialRequestResponse(r)
	if err != nil {
		// Try parsing from raw body
		parsedResponse, err = parseCredentialFromJSON(rawBody)
		if err != nil {
			http.Error(w, `{"error":"invalid credential response"}`, http.StatusBadRequest)
			return
		}
	}

	user := h.passkeySvc.GetUser(body.Username)
	_, err = h.passkeySvc.WebAuthn().ValidateLogin(user, *session, parsedResponse)
	if err != nil {
		http.Error(w, `{"error":"passkey verification failed"}`, http.StatusUnauthorized)
		return
	}

	delete(h.loginSessions, body.Username)
	h.setSessionCookie(w, body.Username)
	writeJSON(w, http.StatusOK, map[string]string{"status": "authenticated"})
}

// PasskeyRemove removes passkey for the logged-in user.
func (h *AuthHandler) PasskeyRemove(w http.ResponseWriter, r *http.Request) {
	username := h.currentUser(r)
	if username == "" {
		http.Error(w, `{"error":"not logged in"}`, http.StatusUnauthorized)
		return
	}

	h.passkeySvc.RemovePasskey(username)
	writeJSON(w, http.StatusOK, map[string]string{"status": "passkey_removed"})
}

// --- helpers ---

func (h *AuthHandler) currentUser(r *http.Request) string {
	cookie, err := r.Cookie("ccmate_session")
	if err != nil {
		return ""
	}
	session, err := h.passkeySvc.DecodeSession("ccmate_session", cookie.Value)
	if err != nil {
		return ""
	}
	return session["user"]
}

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, username string) {
	encoded, err := h.passkeySvc.EncodeSession("ccmate_session", map[string]string{
		"user": username,
		"ts":   time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name: "ccmate_session", Value: encoded, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 86400 * 7,
	})
}

func parseFormBody(body string) map[string]string {
	result := make(map[string]string)
	for _, pair := range splitString(body, "&") {
		kv := splitString(pair, "=")
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}

func splitString(s, sep string) []string {
	var result []string
	for {
		i := indexOf(s, sep)
		if i < 0 {
			result = append(result, s)
			break
		}
		result = append(result, s[:i])
		s = s[i+len(sep):]
	}
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func parseCredentialFromJSON(data []byte) (*protocol.ParsedCredentialAssertionData, error) {
	return nil, fmt.Errorf("credential parsing from JSON body not implemented")
}
