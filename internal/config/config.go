package config

import (
	"log"
	"os"
	"strconv"

	"github.com/graphic/gofhir/internal/secrets"
)

type Config struct {
	Port                   string
	DatabasePath           string
	CORSOrigin             string
	SearchMaxCount         int
	DefaultOffset          int
	DefaultCount           int
	AuditHMACKey           string
	JWTSecretKey           string
	TLSCertFile            string
	TLSKeyFile             string
	TLSCAFile              string
	MTLSEnabled            bool
	GatekeeperDBPath       string
	SessionMaxAge          int
	SessionIdleTimeout     int
	SessionAbsoluteTimeout int
	RateLimitUnauth        int
	RateLimitAuth          int
	RateLimitBurst         int
	RuntimeDir             string
	FHIRDBPath             string
	DatabaseEncryptionKey  string
}

func Load() *Config {
	cfg := &Config{}

	secretsMgr := loadSecrets()

	cfg.Port = getConfigValue(secretsMgr, "GOFHIR_PORT", "8080")
	cfg.DatabasePath = getConfigValue(secretsMgr, "GOFHIR_DB_PATH", "data/gofhir.db")
	cfg.CORSOrigin = getConfigValue(secretsMgr, "GOFHIR_CORS_ORIGIN", "*")
	cfg.SearchMaxCount = getConfigIntValue(secretsMgr, "GOFHIR_MAX_COUNT", 100)
	cfg.DefaultOffset = 0
	cfg.DefaultCount = 20
	cfg.AuditHMACKey = getConfigValue(secretsMgr, "GOFHIR_AUDIT_HMAC_KEY", "")
	cfg.JWTSecretKey = getConfigValue(secretsMgr, "GOFHIR_JWT_SECRET", "")
	cfg.TLSCertFile = getConfigValue(secretsMgr, "GOFHIR_TLS_CERT", "")
	cfg.TLSKeyFile = getConfigValue(secretsMgr, "GOFHIR_TLS_KEY", "")
	cfg.TLSCAFile = getConfigValue(secretsMgr, "GOFHIR_TLS_CA", "")
	cfg.MTLSEnabled = getConfigValue(secretsMgr, "GOFHIR_MTLS_ENABLED", "false") == "true"
	cfg.GatekeeperDBPath = getConfigValue(secretsMgr, "GOFHIR_GK_DB_PATH", "data/gatekeeper.db")
	cfg.SessionMaxAge = getConfigIntValue(secretsMgr, "GOFHIR_SESSION_MAX_AGE", 28800)
	cfg.RateLimitUnauth = getConfigIntValue(secretsMgr, "GOFHIR_RL_UNAUTH", 5)
	cfg.RateLimitAuth = getConfigIntValue(secretsMgr, "GOFHIR_RL_AUTH", 50)
	cfg.RateLimitBurst = getConfigIntValue(secretsMgr, "GOFHIR_RL_BURST", 20)
	cfg.RuntimeDir = getConfigValue(secretsMgr, "GOFHIR_RUNTIME_DIR", "/run/gofhir")
	cfg.FHIRDBPath = getConfigValue(secretsMgr, "GOFHIR_FHIR_DB_PATH", "data/gofhir_fhir.db")
	cfg.SessionIdleTimeout = getConfigIntValue(secretsMgr, "GOFHIR_SESSION_IDLE_TIMEOUT", 900)
	cfg.SessionAbsoluteTimeout = getConfigIntValue(secretsMgr, "GOFHIR_SESSION_ABSOLUTE_TIMEOUT", 28800)
	cfg.DatabaseEncryptionKey = getConfigValue(secretsMgr, "GOFHIR_DB_ENCRYPTION_KEY", "")

	return cfg
}

func loadSecrets() *secrets.Manager {
	secretsFile := os.Getenv("GOFHIR_SECRETS_FILE")
	if secretsFile == "" {
		secretsFile = "~/.gofhir/secrets.enc"
	}

	mgr, err := secrets.NewManager(secretsFile)
	if err != nil {
		return nil
	}

	if err := mgr.Load(); err != nil {
		if os.Getenv("GOFHIR_DEBUG_SECRETS") == "true" {
			log.Printf("secrets: %v", err)
		}
		return nil
	}

	return mgr
}

func getConfigValue(mgr *secrets.Manager, key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	if mgr != nil {
		if v, err := mgr.Get(key); err == nil {
			return v
		}
	}

	return fallback
}

func getConfigIntValue(mgr *secrets.Manager, key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}

	if mgr != nil {
		if v, err := mgr.Get(key); err == nil {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
	}

	return fallback
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
