package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/graphic/gofhir/internal/auditor"
	"github.com/graphic/gofhir/internal/ctxutil"
	"github.com/graphic/gofhir/internal/gatekeeper"
)

func parseLogin(r *http.Request) (loginRequest, error) {
	ct := r.Header.Get("Content-Type")
	if ct == "application/x-www-form-urlencoded" {
		if err := r.ParseForm(); err != nil {
			return loginRequest{}, err
		}
		return loginRequest{
			Username: r.FormValue("username"),
			Password: r.FormValue("password"),
		}, nil
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return loginRequest{}, err
	}
	return req, nil
}

type AuthHandler struct {
	gkStore    *gatekeeper.Store
	jwtKey     interface{ Sign([]byte) ([]byte, error) } // placeholder for future
	auditStore *auditor.Store
}

func NewAuth(gkStore *gatekeeper.Store, auditStore *auditor.Store) *AuthHandler {
	return &AuthHandler{gkStore: gkStore, auditStore: auditStore}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Role      string `json:"role"`
	UserID    string `json:"user_id"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	req, err := parseLogin(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}

	userAgent := r.Header.Get("User-Agent")
	remoteAddr := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		remoteAddr = fwd
	}

	user, err := h.gkStore.UserByUsername(r.Context(), req.Username)
	if err != nil || user == nil {
		h.logLoginAudit(user, req.Username, remoteAddr, userAgent, auditor.CredentialPassword, false, "invalid credentials")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if !gatekeeper.CheckPassword(req.Password, user.PasswordHash) {
		h.logLoginAudit(user, req.Username, remoteAddr, userAgent, auditor.CredentialPassword, false, "invalid password")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	sessionID, err := gatekeeper.GenerateSessionID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session creation failed"})
		return
	}
	session := &gatekeeper.StoredSession{
		ID:        sessionID,
		UserID:    user.ID,
		Role:      user.Role,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(8 * time.Hour),
	}
	if err := h.gkStore.CreateSession(r.Context(), session); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session creation failed"})
		return
	}

	h.logLoginAudit(user, req.Username, remoteAddr, userAgent, auditor.CredentialPassword, true, "")

	secure := r.TLS != nil
	http.SetCookie(w, &http.Cookie{
		Name:     "gofhir-session",
		Value:    sessionID,
		Path:     "/",
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   28800,
	})
	writeJSON(w, http.StatusOK, loginResponse{
		SessionID: sessionID,
		Role:      user.Role,
		UserID:    user.ID,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var userID, username, role, sessionID string

	// Get user info from context if available
	if user, ok := ctxutil.UserFrom(r.Context()); ok {
		userID = user.ID
		role = user.Role
		sessionID = user.SessionID
	}

	// Get remote address
	remoteAddr := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		remoteAddr = fwd
	}

	c, err := r.Cookie("gofhir-session")
	if err == nil && c.Value != "" {
		_ = h.gkStore.DeleteSession(r.Context(), c.Value)
	}

	h.logLogoutAudit(userID, username, role, sessionID, remoteAddr)

	secure := r.TLS != nil
	http.SetCookie(w, &http.Cookie{
		Name:     "gofhir-session",
		Value:    "",
		Path:     "/",
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *AuthHandler) logLoginAudit(user *gatekeeper.StoredUser, username, remoteAddr, userAgent string, credType auditor.LoginCredentialType, success bool, failureReason string) {
	if h.auditStore == nil {
		return
	}

	var userID, role, sessionID string
	if user != nil {
		userID = user.ID
		role = user.Role
	}

	auditEvent := auditor.NewLoginAuditEvent(userID, username, role, sessionID, remoteAddr, userAgent, credType, success, failureReason)
	h.appendAuditEntry("login", userID, sessionID, auditEvent)
}

func (h *AuthHandler) logLogoutAudit(userID, username, role, sessionID, remoteAddr string) {
	if h.auditStore == nil {
		return
	}

	auditEvent := auditor.NewLogoutAuditEvent(userID, username, role, sessionID, remoteAddr)
	h.appendAuditEntry("logout", userID, sessionID, auditEvent)
}

func (h *AuthHandler) appendAuditEntry(action, actorID, sessionID string, auditEvent interface{}) {
	payload, err := json.Marshal(auditEvent)
	if err != nil {
		return
	}

	ctx := context.Background()
	lastSeq, err := h.auditStore.LastSeq(ctx)
	if err != nil {
		return
	}

	var prevHash [32]byte
	if lastSeq > 0 {
		prev, err := h.auditStore.EntryBySeq(ctx, lastSeq)
		if err != nil {
			return
		}
		prevHash = auditor.HashOf(prev)
	}

	hmacKey := make([]byte, 32)
	entry := auditor.NewEntry(prevHash, lastSeq+1, action, actorID, sessionID, payload, hmacKey)
	_ = h.auditStore.Append(ctx, &entry)
}

func (h *AuthHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/login", h.Login)
	mux.HandleFunc("POST /auth/logout", h.Logout)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
