package gatekeeper

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func SHA256Hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func GenerateJWT(userID, role string, secretKey ed25519.PrivateKey) (string, error) {
	header := map[string]string{"alg": "EdDSA", "typ": "JWT"}
	hdr, _ := json.Marshal(header)
	now := time.Now().Unix()
	payload := map[string]any{
		"sub":  userID,
		"role": role,
		"iat":  now,
		"exp":  now + 900,
	}
	pl, _ := json.Marshal(payload)
	enc := base64.RawURLEncoding.EncodeToString
	signingInput := enc(hdr) + "." + enc(pl)
	sig := ed25519.Sign(secretKey, []byte(signingInput))
	return signingInput + "." + enc(sig), nil
}

func VerifyJWT(token string, publicKey ed25519.PublicKey) (userID, role string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid JWT format")
	}
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", "", fmt.Errorf("invalid JWT signature encoding: %w", err)
	}
	if !ed25519.Verify(publicKey, []byte(signingInput), sig) {
		return "", "", fmt.Errorf("invalid JWT signature")
	}
	pl, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("invalid JWT payload encoding: %w", err)
	}
	var claims struct {
		Sub  string `json:"sub"`
		Role string `json:"role"`
		Exp  int64  `json:"exp"`
	}
	if err := json.Unmarshal(pl, &claims); err != nil {
		return "", "", fmt.Errorf("invalid JWT payload: %w", err)
	}
	if time.Now().Unix() > claims.Exp {
		return "", "", fmt.Errorf("JWT expired")
	}
	return claims.Sub, claims.Role, nil
}

func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func ExtractBearerToken(authHeader string) string {
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	return ""
}

func ExtractAPIKey(apiKeyHeader string) string {
	return apiKeyHeader
}

func GenerateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GenerateAPIKey() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(b)
	hash = SHA256Hash(raw)
	return raw, hash, nil
}
