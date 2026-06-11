package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/graphic/gofhir/internal/auditor"
	"github.com/graphic/gofhir/internal/gatekeeper"
	"github.com/graphic/gofhir/internal/netutil"
	"github.com/graphic/gofhir/internal/ctxutil"
)

func main() {
	cfg := loadConfig()

	// Open gatekeeper database
	gkStore, err := gatekeeper.OpenStore(cfg.gkDBPath)
	if err != nil {
		log.Fatalf("gatekeeper store open: %v", err)
	}
	defer gkStore.Close()

	// Open audit database for verification (read-only)
	auditStore, err := auditor.Open(cfg.auditDBPath)
	if err != nil {
		log.Fatalf("audit store open: %v", err)
	}
	defer auditStore.Close()

	// Load JWT key
	jwtPublic := loadOrGenerateJWTKey(cfg.jwtSecret)

	// Setup Unix socket paths
	sockets := netutil.NewSocketPaths()

	// Create HTTP client for FHIR-Core (upstream)
	fhirTransport := netutil.UnixSocketTransport(sockets.FHIRSock)
	fhirClient := &http.Client{
		Transport: fhirTransport,
		Timeout:   30 * time.Second,
	}

	// Create gatekeeper
	gk := gatekeeper.New(gkStore, jwtPublic)

	// Create proxy handler
	proxyHandler := &reverseProxy{
		client: fhirClient,
	}

	// Wrap with gatekeeper middleware
	handler := gk.Middleware(proxyHandler)

	// Start listening on Unix socket
	ln, err := netutil.ListenUnixSocket(sockets.AuthSock)
	if err != nil {
		log.Fatalf("listen on unix socket: %v", err)
	}
	defer ln.Close()

	log.Printf("Gateway-Auth listening on %s", sockets.AuthSock)

	server := &http.Server{
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	waitForShutdown(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	})
}

type reverseProxy struct {
	client *http.Client
}

func (p *reverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check Audit-Service first (fail-closed)
	if err := checkAuditService(p.client); err != nil {
		log.Printf("audit service unavailable: %v", err)
		http.Error(w, "Service Temporarily Unavailable", http.StatusServiceUnavailable)
		return
	}

	// Async audit log
	go writeAudit(r, p.client)

	// Forward to FHIR-Core
	r.URL.Host = "unix"
	r.URL.Scheme = "http"

	resp, err := p.client.Do(r)
	if err != nil {
		log.Printf("fhir-core proxy error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
}

func checkAuditService(client *http.Client) error {
	// Use a simple health check - create a temporary client to audit service
	auditTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			sockets := netutil.NewSocketPaths()
			return net.Dial("unix", sockets.AuditSock)
		},
	}
	tempClient := &http.Client{Transport: auditTransport, Timeout: 2 * time.Second}

	resp, err := tempClient.Get("http://audit/live")
	if err != nil {
		return fmt.Errorf("audit service unreachable: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("audit service not healthy")
	}
	return nil
}

func writeAudit(r *http.Request, fhirClient *http.Client) {
	// Get user from context
	user, _ := ctxutil.UserFrom(r.Context())

	action := classifyAction(r.Method, r.URL.Path)

	payload := fmt.Sprintf(`{"method":"%s","path":"%s","remote":"%s"}`,
		r.Method, r.URL.Path, r.RemoteAddr)

	auditReq := map[string]interface{}{
		"action":   action,
		"actor_id": "anonymous",
		"payload":  payload,
	}

	if user.ID != "" {
		auditReq["actor_id"] = user.ID
		auditReq["session_id"] = user.SessionID
	}

	// Send to audit service via Unix socket
	auditTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			sockets := netutil.NewSocketPaths()
			return net.Dial("unix", sockets.AuditSock)
		},
	}
	auditClient := &http.Client{Transport: auditTransport, Timeout: 5 * time.Second}

	reqBody := fmt.Sprintf(`{"action":"%s","actor_id":"%s","payload":"%s"}`,
		auditReq["action"], auditReq["actor_id"], payload)

	resp, err := auditClient.Post("http://audit/audit/event", "application/json", strings.NewReader(reqBody))
	if err != nil {
		log.Printf("audit write error: %v", err)
		return
	}
	resp.Body.Close()
}

func classifyAction(method, path string) string {
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
	if strings.HasPrefix(path, "/triage") {
		return "triage.read"
	}
	return "unknown"
}

type appConfig struct {
	gkDBPath     string
	auditDBPath  string
	auditHMACKey string
	jwtSecret    string
}

func loadConfig() *appConfig {
	return &appConfig{
		gkDBPath:     getEnv("GOFHIR_GK_DB_PATH", "data/gatekeeper.db"),
		auditDBPath:  getEnv("GOFHIR_AUDIT_DB_PATH", "data/gofhir.db"),
		auditHMACKey: os.Getenv("GOFHIR_AUDIT_HMAC_KEY"),
		jwtSecret:    os.Getenv("GOFHIR_JWT_SECRET"),
	}
}

func loadOrGenerateJWTKey(secret string) ed25519.PublicKey {
	if secret != "" {
		seed, err := hex.DecodeString(secret)
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
	log.Printf("WARNING: no GOFHIR_JWT_SECRET set, using ephemeral Ed25519 key")
	return pub
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func waitForShutdown(shutdown func() error) {
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done
	log.Printf("shutting down Gateway-Auth...")
	_ = shutdown()
}
