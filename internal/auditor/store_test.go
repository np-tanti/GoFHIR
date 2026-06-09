package auditor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func testHMACKey(t *testing.T) []byte {
	t.Helper()
	key, err := GenerateHMACKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func TestAppendAndReadRange(t *testing.T) {
	ctx := context.Background()
	s := openTestDB(t)
	key := testHMACKey(t)

	e1 := FirstEntry("login", "alice", "sess-1", []byte(`{"ip":"10.0.0.1"}`), key)
	if err := s.Append(ctx, &e1); err != nil {
		t.Fatalf("append e1: %v", err)
	}

	e2 := NextEntry(e1, "patient.read", "alice", "sess-1", []byte(`{"id":"pat-001"}`), key)
	if err := s.Append(ctx, &e2); err != nil {
		t.Fatalf("append e2: %v", err)
	}

	e3 := NextEntry(e2, "logout", "alice", "sess-1", nil, key)
	if err := s.Append(ctx, &e3); err != nil {
		t.Fatalf("append e3: %v", err)
	}

	entries, err := s.ReadRange(ctx, 1, 3)
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Action != "login" {
		t.Errorf("entry 1 action: got %q", entries[0].Action)
	}
	if entries[2].Action != "logout" {
		t.Errorf("entry 3 action: got %q", entries[2].Action)
	}
}

func TestAppendOnlyNoUpdate(t *testing.T) {
	ctx := context.Background()
	s := openTestDB(t)
	key := testHMACKey(t)

	e1 := FirstEntry("login", "alice", "sess-1", nil, key)
	if err := s.Append(ctx, &e1); err != nil {
		t.Fatalf("append: %v", err)
	}

	_, err := s.db.ExecContext(ctx, `UPDATE audit_log SET action = 'tampered' WHERE seq = 1`)
	if err == nil {
		t.Fatal("expected update to fail on audit_log")
	}
}

func TestLastSeq(t *testing.T) {
	ctx := context.Background()
	s := openTestDB(t)
	key := testHMACKey(t)

	seq, err := s.LastSeq(ctx)
	if err != nil {
		t.Fatalf("last seq empty: %v", err)
	}
	if seq != 0 {
		t.Fatalf("expected 0, got %d", seq)
	}

	e1 := FirstEntry("login", "bob", "sess-2", nil, key)
	if err := s.Append(ctx, &e1); err != nil {
		t.Fatalf("append: %v", err)
	}

	seq, err = s.LastSeq(ctx)
	if err != nil {
		t.Fatalf("last seq: %v", err)
	}
	if seq != 1 {
		t.Fatalf("expected 1, got %d", seq)
	}
}

func TestCount(t *testing.T) {
	ctx := context.Background()
	s := openTestDB(t)
	key := testHMACKey(t)

	n, err := s.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}

	var prev Entry
	for i := 0; i < 5; i++ {
		var e Entry
		if i == 0 {
			e = FirstEntry("test", "user", "sess", []byte{byte(i)}, key)
		} else {
			e = NextEntry(prev, "test", "user", "sess", []byte{byte(i)}, key)
		}
		if err := s.Append(ctx, &e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		prev = e
	}

	n, err = s.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
}

func TestReadAll(t *testing.T) {
	ctx := context.Background()
	s := openTestDB(t)
	key := testHMACKey(t)

	e1 := FirstEntry("login", "carol", "sess-3", nil, key)
	s.Append(ctx, &e1)
	e2 := NextEntry(e1, "logout", "carol", "sess-3", nil, key)
	s.Append(ctx, &e2)

	entries, err := s.ReadAll(ctx)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2, got %d", len(entries))
	}
}

func TestEntryBySeq(t *testing.T) {
	ctx := context.Background()
	s := openTestDB(t)
	key := testHMACKey(t)

	e1 := FirstEntry("login", "dave", "sess-4", nil, key)
	s.Append(ctx, &e1)

	got, err := s.EntryBySeq(ctx, 1)
	if err != nil {
		t.Fatalf("entry by seq: %v", err)
	}
	if got.ActorID != "dave" {
		t.Errorf("actor: got %q", got.ActorID)
	}

	_, err = s.EntryBySeq(ctx, 999)
	if err == nil {
		t.Fatal("expected error for missing seq")
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")
	key := testHMACKey(t)

	ctx := context.Background()
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("open s1: %v", err)
	}
	e1 := FirstEntry("login", "eve", "sess-5", nil, key)
	if err := s1.Append(ctx, &e1); err != nil {
		t.Fatalf("append: %v", err)
	}
	s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("open s2: %v", err)
	}
	defer s2.Close()
	n, err := s2.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 persisted entry, got %d", n)
	}
}

func TestHMACKeyGeneration(t *testing.T) {
	key1, err := GenerateHMACKey()
	if err != nil {
		t.Fatalf("gen key1: %v", err)
	}
	key2, err := GenerateHMACKey()
	if err != nil {
		t.Fatalf("gen key2: %v", err)
	}

	if len(key1) != 32 {
		t.Fatalf("key length: got %d", len(key1))
	}

	hex := HMACKeyHex(key1)
	decoded, err := HMACKeyFromHex(hex)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	if string(key1) != string(decoded) {
		t.Fatal("hex roundtrip mismatch")
	}

	equal := string(key1) == string(key2)
	if equal {
		t.Fatal("two random keys should not match")
	}
}

func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perm.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	mode := info.Mode().Perm()
	t.Logf("DB file mode: %#o", mode)
}