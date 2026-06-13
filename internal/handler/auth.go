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

type AuthHandler struct {
	gkStore       *gatekeeper.Store
	jwtKey        interface{ Sign([]byte) ([]byte, error) }
	auditStore    *auditor.Store
	hmacKey       []byte
	absTimeoutSec int
}

func NewAuth(gkStore *gatekeeper.Store, auditStore *auditor.Store, hmacKey []byte, absTimeoutSec int) *AuthHandler {
	return &AuthHandler{
		gkStore:       gkStore,
		auditStore:    auditStore,
		hmacKey:       hmacKey,
		absTimeoutSec: absTimeoutSec,
	}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code"`
}

type loginResponse struct {
	Token     string `json:"token,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Role      string `json:"role"`
	UserID    string `json:"user_id"`
	MFAToken  string `json:"mfa_token,omitempty"`
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
		h.logLoginAudit(nil, req.Username, remoteAddr, userAgent, auditor.CredentialPassword, false, "invalid credentials")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if !gatekeeper.CheckPassword(req.Password, user.PasswordHash) {
		h.logLoginAudit(user, req.Username, remoteAddr, userAgent, auditor.CredentialPassword, false, "invalid password")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	// Check if MFA is required
	if user.TOTPSecret != "" {
		if req.TOTPCode == "" {
			// Return MFA required response
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error":     "mfa_required",
				"mfa_token": user.ID, // Simplified: use user ID as temp token
			})
			return
		}
		// Validate TOTP code
		if !gatekeeper.ValidateTOTP(user.TOTPSecret, req.TOTPCode) {
			h.logLoginAudit(user, req.Username, remoteAddr, userAgent, auditor.CredentialPassword, false, "invalid mfa code")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid mfa code"})
			return
		}
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
	if h.absTimeoutSec > 0 {
		session.ExpiresAt = time.Now().Add(time.Duration(h.absTimeoutSec) * time.Second)
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
	if user, ok := ctxutil.UserFrom(r.Context()); ok {
		userID = user.ID
		role = user.Role
		sessionID = user.SessionID
	}
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

// SetupMFA generates a TOTP secret for the authenticated user.
func (h *AuthHandler) SetupMFA(w http.ResponseWriter, r *http.Request) {
	user, ok := ctxutil.UserFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	secret, provisioningURI, err := gatekeeper.GenerateTOTPSecret(user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate secret"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"secret":           secret,
		"provisioning_uri": provisioningURI,
	})
}

// VerifyMFA validates a TOTP code and enables MFA for the user.
func (h *AuthHandler) VerifyMFA(w http.ResponseWriter, r *http.Request) {
	user, ok := ctxutil.UserFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		Code   string `json:"code"`
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code required"})
		return
	}
	// Use the secret from the request (for enrollment) or from the user's stored secret
	secret := req.Secret
	if secret == "" {
		// If no secret in request, try to get from temporary storage
		// For simplicity, require secret in request for enrollment
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "secret required for enrollment"})
		return
	}
	if !gatekeeper.ValidateTOTP(secret, req.Code) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid TOTP code"})
		return
	}
	// Enable TOTP for user
	if err := h.gkStore.EnableTOTP(r.Context(), user.ID, secret); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to enable mfa"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "mfa_enabled"})
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
	h.appendAuditEntry("login", userID, sessionID, auditEvent, nil)
}

func (h *AuthHandler) logLogoutAudit(userID, username, role, sessionID, remoteAddr string) {
	if h.auditStore == nil {
		return
	}
	auditEvent := auditor.NewLogoutAuditEvent(userID, username, role, sessionID, remoteAddr)
	h.appendAuditEntry("logout", userID, sessionID, auditEvent, nil)
}

func (h *AuthHandler) appendAuditEntry(action, actorID, sessionID string, auditEvent interface{}, r *http.Request) {
	payload, err := json.Marshal(auditEvent)
	if err != nil {
		return
	}
	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}
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
	entry := auditor.NewEntry(prevHash, lastSeq+1, action, actorID, sessionID, payload, h.hmacKey)
	_ = h.auditStore.Append(ctx, &entry)
}

func (h *AuthHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/login", h.Login)
	mux.HandleFunc("POST /auth/logout", h.Logout)
	mux.HandleFunc("POST /auth/mfa/setup", h.SetupMFA)
	mux.HandleFunc("POST /auth/mfa/verify", h.VerifyMFA)
}

func parseLogin(r *http.Request) (loginRequest, error) {
	ct := r.Header.Get("Content-Type")
	if ct == "application/x-www-form-urlencoded" {
		if err := r.ParseForm(); err != nil {
			return loginRequest{}, err
		}
		return loginRequest{
			Username: r.FormValue("username"),
			Password: r.FormValue("password"),
			TOTPCode: r.FormValue("totp_code"),
		}, nil
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return loginRequest{}, err
	}
	return req, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
