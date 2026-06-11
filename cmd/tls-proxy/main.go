package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	gkTLS "github.com/graphic/gofhir/internal/tls"
	"github.com/graphic/gofhir/internal/netutil"
)

func main() {
	cfg := loadConfig()

	// Ensure runtime directory exists
	sockets := netutil.NewSocketPaths()
	if err := os.MkdirAll(sockets.RuntimeDir, 0750); err != nil {
		log.Fatalf("create runtime dir: %v", err)
	}

	// Connect to Gateway-Auth via Unix socket
	upstreamTransport := netutil.UnixSocketTransport(sockets.AuthSock)
	upstreamClient := &http.Client{
		Transport: upstreamTransport,
		Timeout:   30 * time.Second,
	}

	// Create reverse proxy handler
	proxyHandler := &reverseProxy{
		client: upstreamClient,
	}

	// TLS configuration
	if cfg.tlsCert == "" || cfg.tlsKey == "" {
		log.Fatal("GOFHIR_TLS_CERT and GOFHIR_TLS_KEY are required")
	}

	certStore, err := gkTLS.NewCertStore(cfg.tlsCert, cfg.tlsKey)
	if err != nil {
		log.Fatalf("cert store: %v", err)
	}

	addr := ":" + cfg.tlsPort
	opts := []gkTLS.Option{gkTLS.WithClientCAs(cfg.tlsCA)}
	if cfg.mtlsEnabled {
		opts = append(opts, gkTLS.WithMTLS())
	}

	terminator := gkTLS.New(addr, certStore, proxyHandler, opts...)

	// Start server
	go func() {
		log.Printf("TLS-Proxy listening on %s, forwarding to %s", addr, sockets.AuthSock)
		if err := terminator.Serve(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("TLS serve: %v", err)
		}
	}()

	// Wait for shutdown
	waitForShutdown(terminator.Close)
}

type reverseProxy struct {
	client *http.Client
}

func (p *reverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Rewrite URL to point to Unix socket backend
	r.URL.Host = "unix"
	r.URL.Scheme = "http"

	// Forward request
	resp, err := p.client.Do(r)
	if err != nil {
		log.Printf("proxy error: %v", err)
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

type config struct {
	tlsPort     string
	tlsCert     string
	tlsKey      string
	tlsCA       string
	mtlsEnabled bool
}

func loadConfig() config {
	return config{
		tlsPort:     getEnv("GOFHIR_TLS_PORT", "443"),
		tlsCert:     os.Getenv("GOFHIR_TLS_CERT"),
		tlsKey:      os.Getenv("GOFHIR_TLS_KEY"),
		tlsCA:       os.Getenv("GOFHIR_TLS_CA"),
		mtlsEnabled: os.Getenv("GOFHIR_MTLS_ENABLED") == "true",
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
	log.Printf("shutting down TLS-Proxy...")
	_ = shutdown()
}
