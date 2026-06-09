package auditor

import (
	"crypto/hmac"
	"crypto/sha256"
	"testing"
)

func TestEntryHMAC(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e := FirstEntry("login", "alice", "sess-1", []byte(`{"ip":"10.0.0.1"}`), key)

	if !VerifyEntry(&e, key) {
		t.Fatal("entry HMAC should verify with correct key")
	}

	wrongKey := []byte("wrong-key-32-bytes-long!!!!!!!!")
	if VerifyEntry(&e, wrongKey) {
		t.Fatal("entry HMAC should NOT verify with wrong key")
	}
}

func TestChainIntegrity(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "alice", "sess-1", nil, key)
	e2 := NextEntry(e1, "patient.read", "alice", "sess-1", []byte(`{"id":"pat-001"}`), key)
	e3 := NextEntry(e2, "logout", "alice", "sess-1", nil, key)

	entries := []Entry{e1, e2, e3}
	if broken := VerifyChain(entries, key); broken != -1 {
		t.Fatalf("chain broken at entry %d", broken)
	}
}

func TestChainTamperedEntry(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "alice", "sess-1", nil, key)
	e2 := NextEntry(e1, "patient.read", "alice", "sess-1", []byte(`{"id":"pat-001"}`), key)
	e3 := NextEntry(e2, "logout", "alice", "sess-1", nil, key)

	e2.Action = "patient.delete"

	entries := []Entry{e1, e2, e3}
	if broken := VerifyChain(entries, key); broken != 1 {
		t.Fatalf("expected chain broken at entry 1, got %d", broken)
	}
}

func TestChainTamperedPrevHash(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "alice", "sess-1", nil, key)
	e2 := NextEntry(e1, "patient.read", "alice", "sess-1", []byte(`{"id":"pat-001"}`), key)
	e3 := NextEntry(e2, "logout", "alice", "sess-1", nil, key)

	e3.PrevHash = [32]byte{}

	entries := []Entry{e1, e2, e3}
	if broken := VerifyChain(entries, key); broken != 2 {
		t.Fatalf("expected chain broken at entry 2, got %d", broken)
	}
}

func TestChainTamperedHMAC(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "alice", "sess-1", nil, key)
	e2 := NextEntry(e1, "patient.read", "alice", "sess-1", []byte(`{"id":"pat-001"}`), key)
	e3 := NextEntry(e2, "logout", "alice", "sess-1", nil, key)

	e2.HMAC[0] ^= 0x01

	entries := []Entry{e1, e2, e3}
	if broken := VerifyChain(entries, key); broken != 1 {
		t.Fatalf("expected chain broken at entry 1, got %d", broken)
	}
}

func TestPrevHashCorrectness(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "alice", "sess-1", nil, key)
	e2 := NextEntry(e1, "patient.read", "alice", "sess-1", nil, key)

	expectedPrev := HashOf(&e1)
	if e2.PrevHash != expectedPrev {
		t.Fatal("NextEntry prev_hash does not match HashOf previous entry")
	}
}

func TestHMACDeterministic(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "bob", "sess-2", []byte("payload"), key)

	h1 := e1.ComputeHMAC(key)
	h2 := e1.ComputeHMAC(key)
	if h1 != h2 {
		t.Fatal("HMAC should be deterministic for same inputs")
	}
}

func TestHMACIncludesAllFields(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "alice", "sess-1", []byte("payload-a"), key)
	e2 := FirstEntry("login", "alice", "sess-1", []byte("payload-b"), key)

	if hmac.Equal(e1.HMAC[:], e2.HMAC[:]) {
		t.Fatal("different payloads should produce different HMACs")
	}

	e3 := FirstEntry("login", "bob", "sess-1", []byte("payload-a"), key)
	if hmac.Equal(e1.HMAC[:], e3.HMAC[:]) {
		t.Fatal("different actors should produce different HMACs")
	}
}

func TestNewEntryTimestamp(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	zero := [32]byte{}
	e := NewEntry(zero, 1, "login", "alice", "sess", nil, key)

	if e.Timestamp == 0 {
		t.Fatal("NewEntry should set a non-zero timestamp")
	}
	if e.Seq != 1 {
		t.Fatalf("seq: got %d", e.Seq)
	}
}

func TestHashOfDeterministic(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "alice", "sess-1", nil, key)

	h1 := HashOf(&e1)
	h2 := HashOf(&e1)
	if h1 != h2 {
		t.Fatal("HashOf should be deterministic")
	}
}

func TestMarshalRoundtrip(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "alice", "sess-1", []byte("hello"), key)

	data := e1.Marshal()
	if len(data) == 0 {
		t.Fatal("marshal should produce data")
	}

	if data[0] != 0 || data[1] != 0 || data[2] != 0 || data[3] != 0 || data[4] != 0 || data[5] != 0 || data[6] != 0 || data[7] != 1 {
		t.Fatal("first 8 bytes should be big-endian seq=1")
	}
}

func TestHMACLength(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e := FirstEntry("login", "alice", "sess-1", nil, key)

	if len(e.HMAC) != sha256.Size {
		t.Fatalf("HMAC length: got %d, want %d", len(e.HMAC), sha256.Size)
	}
}

func TestFirstEntryPrevHash(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e := FirstEntry("bootstrap", "system", "init", nil, key)

	var zero [32]byte
	if e.PrevHash != zero {
		t.Fatal("FirstEntry should have zero PrevHash")
	}
}

func TestEmptyPayload(t *testing.T) {
	key := []byte("test-key-32-bytes-long!!!!!!!")
	e1 := FirstEntry("login", "alice", "sess-1", nil, key)
	e2 := NextEntry(e1, "logout", "alice", "sess-1", nil, key)

	if !VerifyEntry(&e1, key) {
		t.Fatal("nil payload entry should verify")
	}
	if !VerifyEntry(&e2, key) {
		t.Fatal("nil payload next entry should verify")
	}
}

func BenchmarkHMAC(b *testing.B) {
	key := []byte("bench-key-32-bytes-long!!!!!!")
	e := FirstEntry("login", "alice", "sess-bench", nil, key)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.ComputeHMAC(key)
	}
}

func BenchmarkVerifyChain(b *testing.B) {
	key := []byte("bench-key-32-bytes-long!!!!!!")
	entries := make([]Entry, 100)
	entries[0] = FirstEntry("start", "sys", "bench", nil, key)
	for i := 1; i < 100; i++ {
		entries[i] = NextEntry(entries[i-1], "event", "sys", "bench", nil, key)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VerifyChain(entries, key)
	}
}