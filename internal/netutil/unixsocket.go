package netutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

// UnixSocketTransport creates an http.Transport that connects via Unix domain socket.
func UnixSocketTransport(socketPath string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}
}

// UnixSocketClient creates an http.Client that communicates via Unix domain socket.
func UnixSocketClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: UnixSocketTransport(socketPath),
	}
}

// ListenUnixSocket creates a Unix domain socket listener with proper permissions.
// The socket file is created with 0660 permissions (owner/group read-write).
func ListenUnixSocket(socketPath string) (net.Listener, error) {
	// Ensure the directory exists
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}

	// Remove existing socket file if it exists
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove existing socket: %w", err)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on unix socket: %w", err)
	}

	// Set socket permissions to 0660 (owner/group read-write)
	if err := os.Chmod(socketPath, 0660); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}

	return ln, nil
}

// SocketPaths returns the standard Unix socket paths based on runtime directory.
type SocketPaths struct {
	RuntimeDir string
	AuthSock   string
	FHIRSock   string
	AuditSock  string
}

// NewSocketPaths creates SocketPaths from GOFHIR_RUNTIME_DIR env var.
// Defaults to /run/gofhir if not set.
func NewSocketPaths() SocketPaths {
	runtimeDir := os.Getenv("GOFHIR_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/gofhir"
	}
	return SocketPaths{
		RuntimeDir: runtimeDir,
		AuthSock:   filepath.Join(runtimeDir, "auth.sock"),
		FHIRSock:   filepath.Join(runtimeDir, "fhir.sock"),
		AuditSock:  filepath.Join(runtimeDir, "audit.sock"),
	}
}
