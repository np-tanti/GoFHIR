package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port              string
	DatabasePath      string
	CORSOrigin        string
	SearchMaxCount    int
	DefaultOffset     int
	DefaultCount      int
	AuditHMACKey      string
	JWTSecretKey      string
	TLSCertFile       string
	TLSKeyFile        string
	TLSCAFile         string
	MTLSEnabled       bool
	GatekeeperDBPath  string
	SessionMaxAge     int
	RateLimitUnauth   int
	RateLimitAuth     int
	RateLimitBurst    int
}

func Load() *Config {
	return &Config{
		Port:              env("GOFHIR_PORT", "8080"),
		DatabasePath:      env("GOFHIR_DB_PATH", "data/gofhir.db"),
		CORSOrigin:        env("GOFHIR_CORS_ORIGIN", "*"),
		SearchMaxCount:    envInt("GOFHIR_MAX_COUNT", 100),
		DefaultOffset:     0,
		DefaultCount:      20,
		AuditHMACKey:      os.Getenv("GOFHIR_AUDIT_HMAC_KEY"),
		JWTSecretKey:      os.Getenv("GOFHIR_JWT_SECRET"),
		TLSCertFile:       os.Getenv("GOFHIR_TLS_CERT"),
		TLSKeyFile:        os.Getenv("GOFHIR_TLS_KEY"),
		TLSCAFile:         os.Getenv("GOFHIR_TLS_CA"),
		MTLSEnabled:       os.Getenv("GOFHIR_MTLS_ENABLED") == "true",
		GatekeeperDBPath:  env("GOFHIR_GK_DB_PATH", "data/gatekeeper.db"),
		SessionMaxAge:     envInt("GOFHIR_SESSION_MAX_AGE", 28800),
		RateLimitUnauth:   envInt("GOFHIR_RL_UNAUTH", 5),
		RateLimitAuth:     envInt("GOFHIR_RL_AUTH", 50),
		RateLimitBurst:    envInt("GOFHIR_RL_BURST", 20),
	}
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