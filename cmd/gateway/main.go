package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/graphic/gofhir/internal/auditor"
	"github.com/graphic/gofhir/internal/config"
	"github.com/graphic/gofhir/internal/ctxutil"
	fhirstore "github.com/graphic/gofhir/internal/fhir/storage"
	"github.com/graphic/gofhir/internal/gatekeeper"
	"github.com/graphic/gofhir/internal/handler"
	gkTLS "github.com/graphic/gofhir/internal/tls"
	"github.com/graphic/gofhir/internal/triage"
)

func main() {
	cfg := config.Load()

	if cfg.AuditHMACKey == "" {
		log.Fatal("GOFHIR_AUDIT_HMAC_KEY is required")
	}
	key, err := auditor.HMACKeyFromHex(cfg.AuditHMACKey)
	if err != nil {
		log.Fatalf("invalid GOFFER_AUDIT_HMAC_KEY: %v", err)
	}

	auditStore, err := auditor.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("auditor open: %v", err)
	}
	defer auditStore.Close()

	ctx := context.Background()
	if err := bootstrapAudit(ctx, auditStore, key); err != nil {
		log.Fatalf("audit bootstrap: %v", err)
	}

	fhirDBPath := strings.TrimSuffix(cfg.DatabasePath, ".db") + "_fhir.db"
	fhirStore, err := fhirstore.Open(fhirDBPath)
	if err != nil {
		log.Fatalf("fhir store open: %v", err)
	}
	defer fhirStore.Close()

	gkStore, err := gatekeeper.OpenStore(cfg.GatekeeperDBPath)
	if err != nil {
		log.Fatalf("gatekeeper store open: %v", err)
	}
	defer gkStore.Close()

	jwtPublic := loadOrGenerateJWTKey(cfg)
	auditLogger := &auditLog{store: auditStore, key: key}
	gk := gatekeeper.New(gkStore, jwtPublic)
	fhir := handler.NewFHIR(fhirStore, cfg)
	auth := handler.NewAuth(gkStore, auditStore)
	auditH := handler.NewAudit(auditStore)
	triageStore := triage.NewStore()
	triageHub := triage.NewSSEHub()
	triageH := handler.NewTriageHandler(triageStore, triageHub, fhirStore)

	mux := http.NewServeMux()
	fhir.Register(mux)
	auth.Register(mux)
	auditH.Register(mux)
	triageH.Register(mux)
	mux.HandleFunc("GET /ready", handler.NewReady(auditStore))
	mux.HandleFunc("GET /live", handler.NewHealth())
	mux.Handle("GET /static/", handler.NewStatic())
	mux.HandleFunc("GET /reception", handler.NewReception())
	mux.HandleFunc("GET /reception/", handler.NewReception())
	mux.HandleFunc("GET /er", handler.NewDashboard())
	mux.HandleFunc("GET /er/", handler.NewDashboard())
	mux.HandleFunc("GET /", handler.NewDashboard())

	var h http.Handler = mux
	h = auditMiddleware(auditLogger, h)
	h = gk.Middleware(h)
	h = handler.CORS(cfg)(h)

	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		serveTLS(cfg, h, auditLogger)
	} else {
		servePlaintext(cfg, h)
	}
}

func bootstrapAudit(ctx context.Context, store *auditor.Store, key []byte) error {
	lastSeq, err := store.LastSeq(ctx)
	if err != nil {
		return err
	}
	if lastSeq == 0 {
		e := auditor.FirstEntry("bootstrap", "system", "", nil, key)
		if err := store.Append(ctx, &e); err != nil {
			return err
		}
		log.Printf("audit log initialized with bootstrap entry seq=1")
	}
	return nil
}

func loadOrGenerateJWTKey(cfg *config.Config) ed25519.PublicKey {
	if cfg.JWTSecretKey != "" {
		seed, err := hexDecodeString(cfg.JWTSecretKey)
		if err != nil || len(seed) != 32 {
			log.Fatalf("invalid JWT secret: must be 32 hex bytes")
		}
		privateKey := ed25519.NewKeyFromSeed(seed)
		return privateKey.Public().(ed25519.PublicKey)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("JWT key generation: %v", err)
	}
	log.Printf("WARNING: no GOFFER_JWT_SECRET set, using ephemeral Ed25519 key")
	return pub
}

func hexDecodeString(s string) ([]byte, error) {
	return hex.DecodeString(s)
}

func serveTLS(cfg *config.Config, handler http.Handler, logger *auditLog) {
	certStore, err := gkTLS.NewCertStore(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		log.Fatalf("cert store: %v", err)
	}
	addr := ":" + cfg.Port
	opts := []gkTLS.Option{gkTLS.WithClientCAs(cfg.TLSCAFile)}
	if cfg.MTLSEnabled {
		opts = append(opts, gkTLS.WithMTLS())
	}
	term := gkTLS.New(addr, certStore, handler, opts...)
	go func() {
		log.Printf("TLS listening on %s", addr)
		if err := term.Serve(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("TLS serve: %v", err)
		}
	}()
	waitForShutdown(term.Close)
}

func servePlaintext(cfg *config.Config, handler http.Handler) {
	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()
	waitForShutdown(srv.Close)
}

func waitForShutdown(shutdown func() error) {
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done
	_ = shutdown()
}

type auditLog struct {
	store *auditor.Store
	key   []byte
	mu    sync.Mutex
}

func (a *auditLog) Log(ctx context.Context, action, actorID, sessionID string, payload []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	lastSeq, err := a.store.LastSeq(ctx)
	if err != nil {
		return fmt.Errorf("audit last seq: %w", err)
	}
	var prevHash [32]byte
	if lastSeq > 0 {
		prev, err := a.store.EntryBySeq(ctx, lastSeq)
		if err != nil {
			return fmt.Errorf("audit prev lookup: %w", err)
		}
		prevHash = auditor.HashOf(prev)
	}
	e := auditor.NewEntry(prevHash, lastSeq+1, action, actorID, sessionID, payload, a.key)
	return a.store.Append(ctx, &e)
}

func auditMiddleware(logger *auditLog, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := classifyAction(r.Method, r.URL.Path)
		if action != "" {
			payload, _ := json.Marshal(map[string]string{
				"method": r.Method,
				"path":   r.URL.Path,
				"remote": r.RemoteAddr,
			})
			actorID := "anonymous"
			sessionID := ""
			if u, ok := ctxutil.UserFrom(r.Context()); ok {
				actorID = u.ID
				sessionID = u.SessionID
			}
			if err := logger.Log(r.Context(), action, actorID, sessionID, payload); err != nil {
				log.Printf("audit log error: %v", err)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func classifyAction(method, path string) string {
	if strings.HasPrefix(path, "/events") || strings.HasPrefix(path, "/static") {
		return ""
	}
	if strings.HasPrefix(path, "/triage") {
		switch method {
		case "POST":
			if strings.HasSuffix(path, "/checkin") {
				return "triage.checkin"
			}
			if strings.HasSuffix(path, "/checkout") {
				return "triage.checkout"
			}
			if strings.HasSuffix(path, "/esi") {
				return "triage.esi"
			}
		}
		return "triage.read"
	}
	if strings.HasPrefix(path, "/fhir") {
		switch method {
		case "POST":
			return "fhir.create"
		case "PUT":
			return "fhir.update"
		case "DELETE":
			return "fhir.delete"
		case "GET":
			return "fhir.read"
		}
	}
	if path == "/auth/login" {
		return "login"
	}
	if path == "/auth/logout" {
		return "logout"
	}
	return ""
}
