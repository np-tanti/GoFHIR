package storage

import (
	"context"
	"testing"
)

func TestSearchPatient(t *testing.T) {
	openTestDB := func(t *testing.T) *Store {
		t.Helper()
		s, err := Open("file::memory:?cache=shared")
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}

	ctx := context.Background()
	s := openTestDB(t)

	// Create two patients
	p1 := &Resource{ID: "pat-001", Data: []byte(`{"resourceType":"Patient","id":"pat-001","name":[{"family":"Test"}]}`)}
	_, err := s.Create(ctx, p1)
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}

	p2 := &Resource{ID: "pat-002", Data: []byte(`{"resourceType":"Patient","id":"pat-002","name":[{"family":"Test"}]}`)}
	_, err = s.Create(ctx, p2)
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}

	// Search for patients
	res, err := s.Search(ctx, "patient", SearchFilters{DefaultCount: 20, DefaultOffset: 0})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if res.Total != 2 {
		t.Fatalf("expected total=2, got %d", res.Total)
	}
	if len(res.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(res.Resources))
	}
}
