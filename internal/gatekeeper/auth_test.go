package gatekeeper

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJWTGenerateAndVerify(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	token, err := GenerateJWT("user-1", "admin", priv)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if strings.Count(token, ".") != 2 {
		t.Fatal("expected 3-part JWT")
	}

	userID, role, err := VerifyJWT(token, pub)
	if err != nil {
		t.Fatalf("verify jwt: %v", err)
	}
	if userID != "user-1" || role != "admin" {
		t.Errorf("claims mismatch: user=%s role=%s", userID, role)
	}
}

func TestJWTWrongKey(t *testing.T) {
	_, priv, _ := GenerateKeyPair()
	pub2, _, _ := GenerateKeyPair()

	token, err := GenerateJWT("user-1", "nurse", priv)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}

	_, _, err = VerifyJWT(token, pub2)
	if err == nil {
		t.Fatal("expected error verifying with wrong key")
	}
}

func TestJWTExpired(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	header := map[string]string{"alg": "EdDSA", "typ": "JWT"}
	hdr, _ := json.Marshal(header)
	payload := map[string]any{
		"sub":  "user-1",
		"role": "admin",
		"iat":  time.Now().Unix() - 3600,
		"exp":  time.Now().Unix() - 1800,
	}
	pl, _ := json.Marshal(payload)
	enc := base64.RawURLEncoding.EncodeToString
	signingInput := enc(hdr) + "." + enc(pl)
	sig := ed25519.Sign(priv, []byte(signingInput))
	token := signingInput + "." + enc(sig)

	_, _, err = VerifyJWT(token, pub)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired error, got: %v", err)
	}
}

func TestJWTCorruptFormat(t *testing.T) {
	pub, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"one part", "aaaa"},
		{"two parts", "aaaa.bbbb"},
		{"bad sig encoding", "aaaa.bbbb.cccc"},
		{"garbage", "this.is.not.a.jwt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := VerifyJWT(tt.token, pub)
			if err == nil {
				t.Fatal("expected error for corrupt token")
			}
		})
	}
}

func TestJWTBadPayload(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	enc := base64.RawURLEncoding.EncodeToString
	hdr := enc([]byte(`{"alg":"EdDSA","typ":"JWT"}`))
	pl := enc([]byte(`not-json`))
	sig := ed25519.Sign(priv, []byte(hdr+"."+pl))
	token := hdr + "." + pl + "." + enc(sig)

	_, _, err = VerifyJWT(token, pub)
	if err == nil || !strings.Contains(err.Error(), "invalid JWT payload") {
		t.Fatalf("expected payload error, got: %v", err)
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer abc123", "abc123"},
		{"bearer xyz", ""},
		{"", ""},
		{"Basic dXNlcjpwYXNz", ""},
		{"Bearer ", ""},
	}
	for _, tt := range tests {
		got := ExtractBearerToken(tt.header)
		if got != tt.want {
			t.Errorf("ExtractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestGenerateSessionID(t *testing.T) {
	s1, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s2, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if s1 == s2 {
		t.Fatal("session IDs should be unique")
	}
	if len(s1) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(s1))
	}
}

func TestGenerateAPIKey(t *testing.T) {
	raw1, hash1, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	raw2, hash2, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if raw1 == raw2 {
		t.Fatal("raw keys should be unique")
	}
	if SHA256Hash(raw1) != hash1 {
		t.Fatal("SHA256 hash mismatch")
	}
	if SHA256Hash(raw2) != hash2 {
		t.Fatal("SHA256 hash mismatch")
	}
}

func TestCheckPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !CheckPassword("correct-horse-battery-staple", hash) {
		t.Error("correct password should match")
	}
	if CheckPassword("wrong-password", hash) {
		t.Error("wrong password should not match")
	}
}

func TestGenerateKeyPair(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("pub key size: got %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("priv key size: got %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	msg := []byte("test message")
	sig := ed25519.Sign(priv, msg)
	if !ed25519.Verify(pub, msg, sig) {
		t.Fatal("signature verification failed")
	}
}