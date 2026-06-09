package gatekeeper

import (
	"crypto/ed25519"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/graphic/gofhir/internal/ctxutil"
)

type Gatekeeper struct {
	store       *Store
	jwtPublic   ed25519.PublicKey
	rateLimiter *RateLimiter
	unauthRL    *RateLimiter
}

func New(store *Store, jwtPublic ed25519.PublicKey) *Gatekeeper {
	return &Gatekeeper{
		store:       store,
		jwtPublic:   jwtPublic,
		rateLimiter: NewRateLimiter(50, 20),
		unauthRL:    NewRateLimiter(5, 20),
	}
}

func (g *Gatekeeper) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)
		user, authed := g.authenticate(r)
		if authed {
			ctx := ctxutil.WithUser(r.Context(), user)
			r = r.WithContext(ctx)
			if !g.rateLimiter.Allow(ip) {
				http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
				return
			}
		} else {
			if !g.unauthRL.Allow(ip) {
				http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
				return
			}
			if !isPublicPath(r.URL.Path) {
				http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		if err := g.checkAccess(r); err != nil {
			http.Error(w, "403 Forbidden: "+err.Error(), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gatekeeper) authenticate(r *http.Request) (ctxutil.User, bool) {
	if token := ExtractBearerToken(r.Header.Get("Authorization")); token != "" {
		userID, role, err := VerifyJWT(token, g.jwtPublic)
		if err == nil {
			return ctxutil.User{ID: userID, Role: role}, true
		}
	}
	if key := ExtractAPIKey(r.Header.Get("X-API-Key")); key != "" {
		hash := SHA256Hash(key)
		stored, err := g.store.APIKeyByHash(r.Context(), hash)
		if err == nil && stored != nil && !stored.Revoked {
			return ctxutil.User{ID: stored.UserID, Role: stored.Role}, true
		}
	}
	if sessionID := readSessionCookie(r); sessionID != "" {
		session, err := g.store.SessionByID(r.Context(), sessionID)
		if err == nil && session != nil && session.ExpiresAt.After(time.Now()) {
			return ctxutil.User{ID: session.UserID, Role: session.Role, SessionID: session.ID}, true
		}
	}
	if cn := r.Header.Get("X-Client-CN"); cn != "" {
		return ctxutil.User{ID: cn, Role: string(RoleSystem)}, true
	}
	return ctxutil.User{}, false
}

func (g *Gatekeeper) checkAccess(r *http.Request) error {
	user, ok := ctxutil.UserFrom(r.Context())
	if !ok {
		return nil
	}
	path := r.URL.Path
	method := r.Method
	switch {
	case strings.HasPrefix(path, "/fhir"):
		switch method {
		case "GET":
			if !HasPermission(Role(user.Role), PermPatientRead) {
				return fmt.Errorf("role %s lacks patient:read", user.Role)
			}
		case "POST", "PUT":
			if !HasPermission(Role(user.Role), PermPatientWrite) {
				return fmt.Errorf("role %s lacks patient:write", user.Role)
			}
		case "DELETE":
			if !HasPermission(Role(user.Role), PermPatientDelete) {
				return fmt.Errorf("role %s lacks patient:delete", user.Role)
			}
		}
	case strings.HasPrefix(path, "/audit"):
		if !HasPermission(Role(user.Role), PermAuditRead) {
			return fmt.Errorf("role %s lacks audit:read", user.Role)
		}
	case strings.HasPrefix(path, "/admin"):
		if !HasPermission(Role(user.Role), PermAdmin) {
			return fmt.Errorf("role %s lacks admin:all", user.Role)
		}
	}
	return nil
}

var publicPaths = map[string]bool{
	"/auth/login":  true,
	"/auth/logout": true,
	"/live":        true,
	"/ready":       true,
	"/":            true,
	"/reception":   true,
}

func isPublicPath(path string) bool {
	if publicPaths[path] {
		return true
	}
	if strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/reception/") {
		return true
	}
	return false
}

func readSessionCookie(r *http.Request) string {
	c, err := r.Cookie("gofhir-session")
	if err != nil {
		return ""
	}
	return c.Value
}

func extractIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	idx := strings.LastIndex(r.RemoteAddr, ":")
	if idx == -1 {
		return r.RemoteAddr
	}
	return r.RemoteAddr[:idx]
}