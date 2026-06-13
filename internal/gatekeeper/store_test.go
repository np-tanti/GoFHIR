package gatekeeper

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gk_test.db")
	s, err := OpenStore(path, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndLookupUser(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u := &StoredUser{
		ID:           "user-1",
		Username:     "testuser",
		PasswordHash: hash,
		Role:         "nurse",
		CreatedAt:    time.Now(),
	}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	got, err := s.UserByUsername(ctx, "testuser")
	if err != nil {
		t.Fatalf("lookup by username: %v", err)
	}
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.ID != "user-1" || got.Role != "nurse" {
		t.Errorf("unexpected user: id=%s role=%s", got.ID, got.Role)
	}
	if !CheckPassword("secret123", got.PasswordHash) {
		t.Error("password hash mismatch")
	}

	got2, err := s.UserByID(ctx, "user-1")
	if err != nil {
		t.Fatalf("lookup by id: %v", err)
	}
	if got2 == nil || got2.Username != "testuser" {
		t.Fatal("user by id mismatch")
	}
}

func TestUserByUsernameNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	got, err := s.UserByUsername(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing user")
	}
}

func TestUserByIDNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	got, err := s.UserByID(ctx, "no-such-id")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing id")
	}
}

func TestSessionCRUD(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	ses := &StoredSession{
		ID:        "sess-1",
		UserID:    "user-1",
		Role:      "admin",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := s.CreateSession(ctx, ses); err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := s.SessionByID(ctx, "sess-1")
	if err != nil {
		t.Fatalf("lookup session: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.UserID != "user-1" || got.Role != "admin" {
		t.Errorf("session mismatch: user=%s role=%s", got.UserID, got.Role)
	}

	if err := s.DeleteSession(ctx, "sess-1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	deleted, err := s.SessionByID(ctx, "sess-1")
	if err != nil {
		t.Fatalf("lookup after delete: %v", err)
	}
	if deleted != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestSessionByIDNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	got, err := s.SessionByID(ctx, "no-such-session")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing session")
	}
}

func TestAPIKeyCRUD(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	raw, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if raw == "" || hash == "" {
		t.Fatal("expected non-empty key/hash")
	}

	ak := &StoredAPIKey{
		KeyHash:   hash,
		UserID:    "user-1",
		Role:      "nurse",
		CreatedAt: time.Now(),
	}
	if err := s.CreateAPIKey(ctx, ak); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	got, err := s.APIKeyByHash(ctx, hash)
	if err != nil {
		t.Fatalf("lookup key: %v", err)
	}
	if got == nil {
		t.Fatal("expected key, got nil")
	}
	if got.UserID != "user-1" || got.Role != "nurse" || got.Revoked {
		t.Errorf("key mismatch: user=%s role=%s revoked=%v", got.UserID, got.Role, got.Revoked)
	}

	if SHA256Hash(raw) != hash {
		t.Error("SHA256 hash mismatch")
	}
}

func TestAPIKeyByHashNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	got, err := s.APIKeyByHash(ctx, "nonexistent-hash")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing key hash")
	}
}

func TestExpiredSessionCleanup(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	active := &StoredSession{
		ID:        "active-sess",
		UserID:    "user-1",
		Role:      "nurse",
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(1 * time.Hour),
	}
	if err := s.CreateSession(ctx, active); err != nil {
		t.Fatalf("create active session: %v", err)
	}

	expired := &StoredSession{
		ID:        "expired-sess",
		UserID:    "user-2",
		Role:      "auditor",
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour),
		ExpiresAt: time.Now().UTC().Add(-1 * time.Hour),
	}
	if err := s.CreateSession(ctx, expired); err != nil {
		t.Fatalf("create expired session: %v", err)
	}

	if err := s.DeleteExpiredSessions(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	stillActive, err := s.SessionByID(ctx, "active-sess")
	if err != nil {
		t.Fatalf("lookup active: %v", err)
	}
	if stillActive == nil {
		t.Fatal("active session should not be deleted")
	}

	shouldBeGone, err := s.SessionByID(ctx, "expired-sess")
	if err != nil {
		t.Fatalf("lookup expired: %v", err)
	}
	if shouldBeGone != nil {
		t.Fatal("expired session should have been deleted")
	}
}

func TestDuplicateUser(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	hash, _ := HashPassword("pw")
	u := &StoredUser{ID: "u1", Username: "dup", PasswordHash: hash, Role: "nurse", CreatedAt: time.Now()}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatalf("first create: %v", err)
	}

	u2 := &StoredUser{ID: "u2", Username: "dup", PasswordHash: hash, Role: "admin", CreatedAt: time.Now()}
	if err := s.CreateUser(ctx, u2); err == nil {
		t.Fatal("expected error for duplicate username")
	}
}
