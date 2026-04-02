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
	"github.com/cloverstd/ccmate/internal/settings"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type AuthHandler struct {
	client     *ent.Client
	cfg        *config.Config
	passkeySvc *auth.PasskeyService
	settingsMgr *settings.Manager

	regSessions   map[string]*webauthn.SessionData
	loginSessions map[string]*webauthn.SessionData
}

func NewAuthHandler(client *ent.Client, cfg *config.Config, passkeySvc *auth.PasskeyService, settingsMgr *settings.Manager) *AuthHandler {
	return &AuthHandler{
		client: client, cfg: cfg, passkeySvc: passkeySvc, settingsMgr: settingsMgr,
		regSessions: make(map[string]*webauthn.SessionData),
		loginSessions: make(map[string]*webauthn.SessionData),
	}
}

// --- GitHub OAuth ---

func (h *AuthHandler) GitHubOAuthStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clientID := h.settingsMgr.GetWithDefault(ctx, settings.KeyGitHubClientID, "")
	if clientID == "" {
		http.Error(w, `{"error":"github oauth not configured"}`, http.StatusInternalServerError)
		return
	}

	origin := fmt.Sprintf("http://%s", h.cfg.Server.Addr())
	redirectURI := h.settingsMgr.GetGitHubCallbackURL(ctx, origin)

	url := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user",
		clientID, redirectURI,
	)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (h *AuthHandler) GitHubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, `{"error":"missing code"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	clientID := h.settingsMgr.GetWithDefault(ctx, settings.KeyGitHubClientID, "")
	clientSecret := h.settingsMgr.GetWithDefault(ctx, settings.KeyGitHubClientSecret, "")

	tokenResp, err := http.Post(
		fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s",
			clientID, clientSecret, code),
		"application/json", nil,
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
	}
	if err := json.NewDecoder(userResp.Body).Decode(&ghUser); err != nil || ghUser.Login == "" {
		http.Error(w, `{"error":"invalid github user response"}`, http.StatusInternalServerError)
		return
	}

	if !h.settingsMgr.IsUserAllowed(ctx, ghUser.Login) {
		http.Error(w, fmt.Sprintf(`{"error":"user %s is not in the allowed list"}`, ghUser.Login), http.StatusForbidden)
		return
	}

	h.setSessionCookie(w, ghUser.Login)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	username := h.currentUser(r)
	if username == "" {
		http.Error(w, `{"error":"not logged in"}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":        username,
		"has_passkey": h.passkeySvc.HasPasskey(username),
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "ccmate_session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

// --- Passkey (post-login) ---

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

func (h *AuthHandler) PasskeyLoginStart(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username string `json:"username"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
		http.Error(w, `{"error":"username required"}`, http.StatusBadRequest)
		return
	}
	if !h.settingsMgr.IsUserAllowed(r.Context(), body.Username) {
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

func (h *AuthHandler) PasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string          `json:"username"`
		Response json.RawMessage `json:"response_data"`
	}
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
	encoded, _ := h.passkeySvc.EncodeSession("ccmate_session", map[string]string{
		"user": username, "ts": time.Now().Format(time.RFC3339),
	})
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
