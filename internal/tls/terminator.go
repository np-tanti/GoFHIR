package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

type CertStore struct {
	mu       sync.RWMutex
	certFile string
	keyFile  string
	cert     *tls.Certificate
}

func NewCertStore(certFile, keyFile string) (*CertStore, error) {
	cs := &CertStore{certFile: certFile, keyFile: keyFile}
	if err := cs.load(); err != nil {
		return nil, err
	}
	return cs, nil
}

func (cs *CertStore) load() error {
	cert, err := tls.LoadX509KeyPair(cs.certFile, cs.keyFile)
	if err != nil {
		return fmt.Errorf("cert load: %w", err)
	}
	cs.mu.Lock()
	cs.cert = &cert
	cs.mu.Unlock()
	return nil
}

func (cs *CertStore) Reload() error {
	return cs.load()
}

func (cs *CertStore) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.cert, nil
}

type Terminator struct {
	server      *http.Server
	certStore   *CertStore
	clientCAs   *x509.CertPool
	requireMTLS bool
}

type Option func(*Terminator)

func WithClientCAs(caCertFile string) Option {
	return func(t *Terminator) {
		caCert, err := os.ReadFile(caCertFile)
		if err != nil {
			return
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caCert)
		t.clientCAs = pool
	}
}

func WithMTLS() Option {
	return func(t *Terminator) {
		t.requireMTLS = true
	}
}

func New(addr string, certStore *CertStore, handler http.Handler, opts ...Option) *Terminator {
	t := &Terminator{
		certStore: certStore,
	}
	for _, opt := range opts {
		opt(t)
	}
	tlsCfg := &tls.Config{
		MinVersion:     tls.VersionTLS13,
		GetCertificate: certStore.GetCertificate,
	}
	if t.clientCAs != nil {
		tlsCfg.ClientCAs = t.clientCAs
		if t.requireMTLS {
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
		}
	}
	handler = &cnPropagator{next: handler}
	t.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		TLSConfig:    tlsCfg,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return t
}

func (t *Terminator) Serve() error {
	return t.server.ListenAndServeTLS("", "")
}

func (t *Terminator) Close() error {
	return t.server.Close()
}

type cnPropagator struct {
	next http.Handler
}

func (p *cnPropagator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		cn := r.TLS.PeerCertificates[0].Subject.CommonName
		r.Header.Set("X-Client-CN", cn)
	}
	p.next.ServeHTTP(w, r)
}
