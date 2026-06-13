package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/graphic/gofhir/internal/auditor"
	"github.com/graphic/gofhir/internal/netutil"
)

func main() {
	cfg := loadConfig()

	if cfg.dbPath == "" {
		log.Fatal("GOFHIR_DB_PATH is required")
	}
	if cfg.hmacKey == "" {
		log.Fatal("GOFHIR_AUDIT_HMAC_KEY is required")
	}

	key, err := auditor.HMACKeyFromHex(cfg.hmacKey)
	if err != nil {
		log.Fatalf("invalid GOFHIR_AUDIT_HMAC_KEY: %v", err)
	}

	// Open audit store
	store, err := auditor.Open(cfg.dbPath)
	if err != nil {
		log.Fatalf("audit store open: %v", err)
	}
	defer store.Close()

	// Bootstrap audit log
	ctx := context.Background()
	if err := bootstrapAudit(ctx, store, key); err != nil {
		log.Fatalf("audit bootstrap: %v", err)
	}

	// Setup socket paths
	sockets := netutil.NewSocketPaths()

	// Create handler
	handler := &auditHandler{
		store: store,
		key:   key,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/live", handler.live)
	mux.HandleFunc("/ready", handler.ready)
	mux.HandleFunc("/audit/event", handler.appendEvent)
	mux.HandleFunc("/audit/entries", handler.getEntries)

	// Start listening on Unix socket
	ln, err := netutil.ListenUnixSocket(sockets.AuditSock)
	if err != nil {
		log.Fatalf("listen on unix socket: %v", err)
	}
	defer ln.Close()

	log.Printf("Audit-Service listening on %s", sockets.AuditSock)

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
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

type auditHandler struct {
	store *auditor.Store
	key   []byte
}

func (h *auditHandler) live(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (h *auditHandler) ready(w http.ResponseWriter, r *http.Request) {
	// Check if we can query the database
	ctx := context.Background()
	_, err := h.store.LastSeq(ctx)
	if err != nil {
		http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (h *auditHandler) appendEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Action    string `json:"action"`
		ActorID   string `json:"actor_id"`
		SessionID string `json:"session_id"`
		Payload   string `json:"payload"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	// Get last sequence for chain integrity
	lastSeq, err := h.store.LastSeq(ctx)
	if err != nil {
		log.Printf("audit last seq error: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Get previous hash
	var prevHash [32]byte
	if lastSeq > 0 {
		prev, err := h.store.EntryBySeq(ctx, lastSeq)
		if err != nil {
			log.Printf("audit prev lookup error: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		prevHash = auditor.HashOf(prev)
	}

	// Decode payload
	payload := []byte(req.Payload)
	if req.Payload != "" && !strings.HasPrefix(req.Payload, "{") {
		payload, _ = hex.DecodeString(req.Payload)
	}

	// Create and append entry
	entry := auditor.NewEntry(prevHash, lastSeq+1, req.Action, req.ActorID, req.SessionID, payload, h.key)
	if err := h.store.Append(ctx, &entry); err != nil {
		log.Printf("audit append error: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"seq":    entry.Seq,
		"status": "ok",
	})
}

func (h *auditHandler) getEntries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Add auditor-only authentication
	// For now, allow read access (in production, this should be restricted)

	ctx := context.Background()
	entries, err := h.store.ReadAll(ctx)
	if err != nil {
		log.Printf("audit query error: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
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

type config struct {
	dbPath  string
	hmacKey string
}

func loadConfig() config {
	return config{
		dbPath:  getEnv("GOFHIR_DB_PATH", "data/gofhir.db"),
		hmacKey: os.Getenv("GOFHIR_AUDIT_HMAC_KEY"),
	}
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
	log.Printf("shutting down Audit-Service...")
	_ = shutdown()
}
