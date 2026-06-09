package storage

import (
	"context"
	"testing"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	s, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateAndRead(t *testing.T) {
	ctx := context.Background()
	s := openTestDB(t)
	r, err := s.Create(ctx, &Resource{ID: "pat1", Data: []byte(`{"resourceType":"Patient","id":"pat1","active":true}`)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if r.ResourceType != "patient" || r.Version != 1 {
		t.Fatalf("unexpected: %+v", r)
	}
	got, err := s.Read(ctx, "pat1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.ID != "pat1" || got.Version != 1 {
		t.Fatalf("read mismatch: %+v", got)
	}
}
