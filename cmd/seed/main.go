package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/graphic/gofhir/internal/auditor"
	"github.com/graphic/gofhir/internal/config"
	fhirstore "github.com/graphic/gofhir/internal/fhir/storage"
	"github.com/graphic/gofhir/internal/gatekeeper"
)

func main() {
	cfg := config.Load()

	auditKey := mustAuditKey(cfg)
	auditStore := mustOpenAudit(cfg)
	defer auditStore.Close()

	gkStore := mustOpenGK(cfg)
	defer gkStore.Close()

	fhirDBPath := strings.TrimSuffix(cfg.DatabasePath, ".db") + "_fhir.db"
	fhirStore := mustOpenFHIR(fhirDBPath)
	defer fhirStore.Close()

	ctx := context.Background()

	seedUsers(ctx, gkStore)
	seedAPIKeys(ctx, gkStore)
	seedFHIR(ctx, fhirStore)
	seedPatientAssignments(ctx, gkStore)
	seedAudit(ctx, auditStore, auditKey)

	fmt.Println("seed complete")
}

func mustAuditKey(cfg *config.Config) []byte {
	if cfg.AuditHMACKey == "" {
		log.Fatal("GOFHIR_AUDIT_HMAC_KEY is required")
	}
	key, err := auditor.HMACKeyFromHex(cfg.AuditHMACKey)
	if err != nil {
		log.Fatalf("invalid GOFFER_AUDIT_HMAC_KEY: %v", err)
	}
	return key
}

func mustOpenAudit(cfg *config.Config) *auditor.Store {
	s, err := auditor.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("auditor open: %v", err)
	}
	return s
}

func mustOpenGK(cfg *config.Config) *gatekeeper.Store {
	key := getEncryptionKey(cfg.DatabaseEncryptionKey)
	s, err := gatekeeper.OpenStore(cfg.GatekeeperDBPath, key)
	if err != nil {
		log.Fatalf("gatekeeper open: %v", err)
	}
	return s
}

func getEncryptionKey(hexKey string) []byte {
	if hexKey == "" {
		return nil
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		log.Printf("invalid encryption key: %v", err)
		return nil
	}
	if len(key) != 32 {
		log.Printf("encryption key must be 32 bytes (64 hex chars)")
		return nil
	}
	return key
}

func mustOpenFHIR(path string) *fhirstore.Store {
	s, err := fhirstore.Open(path)
	if err != nil {
		log.Fatalf("fhir store open: %v", err)
	}
	return s
}

type seedUser struct {
	username string
	password string
	role     string
}

func seedUsers(ctx context.Context, s *gatekeeper.Store) {
	users := []seedUser{
		{username: "nurse-1", password: "nurse123", role: "nurse"},
		{username: "admin-1", password: "admin123", role: "admin"},
		{username: "auditor-1", password: "auditor123", role: "auditor"},
	}
	for _, u := range users {
		existing, err := s.UserByUsername(ctx, u.username)
		if err != nil {
			log.Fatalf("lookup %s: %v", u.username, err)
		}
		if existing != nil {
			fmt.Printf("user %s already exists (role=%s), skipping\n", u.username, existing.Role)
			continue
		}
		hash, err := gatekeeper.HashPassword(u.password)
		if err != nil {
			log.Fatalf("hash password for %s: %v", u.username, err)
		}
		user := &gatekeeper.StoredUser{
			ID:           u.username,
			Username:     u.username,
			PasswordHash: hash,
			Role:         u.role,
			CreatedAt:    time.Now(),
		}
		if err := s.CreateUser(ctx, user); err != nil {
			log.Fatalf("create user %s: %v", u.username, err)
		}
		fmt.Printf("created user %s (role=%s)\n", u.username, u.role)

		// Generate TOTP secret for admin and auditor
		if u.role == "admin" || u.role == "auditor" {
			secret, provisioningURI, err := gatekeeper.GenerateTOTPSecret(u.username)
			if err != nil {
				log.Printf("WARNING: failed to generate TOTP secret for %s: %v", u.username, err)
				continue
			}
			if err := s.EnableTOTP(ctx, u.username, secret); err != nil {
				log.Printf("WARNING: failed to enable TOTP for %s: %v", u.username, err)
				continue
			}
			fmt.Printf("  TOTP enabled for %s\n", u.username)
			fmt.Printf("  Provisioning URI: %s\n", provisioningURI)
			fmt.Printf("  Secret (base32): %s\n", secret)
		}
	}
}

func seedAPIKeys(ctx context.Context, s *gatekeeper.Store) {
	type seedKey struct {
		userID string
		role   string
	}
	keys := []seedKey{
		{userID: "nurse-1", role: "nurse"},
		{userID: "admin-1", role: "admin"},
		{userID: "auditor-1", role: "auditor"},
	}
	for _, k := range keys {
		raw, hash, err := gatekeeper.GenerateAPIKey()
		if err != nil {
			log.Fatalf("generate key for %s: %v", k.userID, err)
		}
		existing, err := s.APIKeyByHash(ctx, hash)
		if err != nil {
			log.Fatalf("lookup key for %s: %v", k.userID, err)
		}
		if existing != nil {
			fmt.Printf("api key for %s already exists, skipping\n", k.userID)
			continue
		}
		ak := &gatekeeper.StoredAPIKey{
			KeyHash:   hash,
			UserID:    k.userID,
			Role:      k.role,
			CreatedAt: time.Now(),
		}
		if err := s.CreateAPIKey(ctx, ak); err != nil {
			log.Fatalf("create api key for %s: %v", k.userID, err)
		}
		fmt.Printf("created api key for %s: %s\n", k.userID, raw)
	}
}

func seedFHIR(ctx context.Context, s *fhirstore.Store) {
	patients := []struct {
		id   string
		data string
	}{
		{
			id:   "pat-001",
			data: `{"resourceType":"Patient","id":"pat-001","active":true,"name":[{"family":"Smith","given":["John"]}],"gender":"male","birthDate":"1980-05-15"}`,
		},
		{
			id:   "pat-002",
			data: `{"resourceType":"Patient","id":"pat-002","active":true,"name":[{"family":"Jones","given":["Alice"]}],"gender":"female","birthDate":"1992-11-03"}`,
		},
	}
	for _, p := range patients {
		_, err := s.Read(ctx, p.id)
		if err == nil {
			fmt.Printf("patient %s already exists, skipping\n", p.id)
			continue
		}
		rec := &fhirstore.Resource{ID: p.id, Data: []byte(p.data)}
		created, err := s.Create(ctx, rec)
		if err != nil {
			log.Fatalf("create patient %s: %v", p.id, err)
		}
		fmt.Printf("created patient %s (version=%d)\n", created.ID, created.Version)
	}

	observations := []struct {
		id   string
		subj string
		data string
	}{
		{
			id:   "obs-001",
			subj: "pat-001",
			data: `{"resourceType":"Observation","id":"obs-001","subject":{"reference":"Patient/pat-001"},"status":"final","code":{"coding":[{"system":"http://loinc.org","code":"8480-6","display":"Systolic blood pressure"}]},"valueQuantity":{"value":120,"unit":"mmHg"}}`,
		},
		{
			id:   "obs-002",
			subj: "pat-002",
			data: `{"resourceType":"Observation","id":"obs-002","subject":{"reference":"Patient/pat-002"},"status":"final","code":{"coding":[{"system":"http://loinc.org","code":"8480-6","display":"Systolic blood pressure"}]},"valueQuantity":{"value":132,"unit":"mmHg"}}`,
		},
	}
	for _, o := range observations {
		_, err := s.Read(ctx, o.id)
		if err == nil {
			fmt.Printf("observation %s already exists, skipping\n", o.id)
			continue
		}
		rec := &fhirstore.Resource{ID: o.id, Data: []byte(o.data)}
		created, err := s.Create(ctx, rec)
		if err != nil {
			log.Fatalf("create observation %s: %v", o.id, err)
		}
		fmt.Printf("created observation %s (version=%d)\n", created.ID, created.Version)
	}
}

func seedAudit(ctx context.Context, s *auditor.Store, key []byte) {
	lastSeq, err := s.LastSeq(ctx)
	if err != nil {
		log.Fatalf("audit last seq: %v", err)
	}
	if lastSeq > 0 {
		fmt.Printf("audit already has %d entries, skipping seed entry\n", lastSeq)
		return
	}
	e := auditor.FirstEntry("seed", "system", "", nil, key)
	if err := s.Append(ctx, &e); err != nil {
		log.Fatalf("append seed entry: %v", err)
	}
	fmt.Printf("appended audit seed entry seq=%d\n", e.Seq)
}

func seedPatientAssignments(ctx context.Context, s *gatekeeper.Store) {
	assignments := []struct {
		patientID string
		userID    string
		role      string
	}{
		{patientID: "pat-001", userID: "nurse-1", role: "viewer"},
		{patientID: "pat-002", userID: "nurse-1", role: "viewer"},
	}
	for _, a := range assignments {
		err := s.AssignPatient(ctx, a.patientID, a.userID, a.role)
		if err != nil {
			log.Printf("assign patient %s to %s: %v", a.patientID, a.userID, err)
			continue
		}
		fmt.Printf("assigned patient %s to %s\n", a.patientID, a.userID)
	}
}
