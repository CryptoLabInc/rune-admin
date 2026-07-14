package crypto

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEngineSmoke exercises the full console crypto path against a LIVE runespace
// engine: generate keys → register eval key → Insert (encrypt) → Search
// (decrypt) round-trip. Gated on RUNESPACE_ADDR so normal unit runs skip it.
//
//	RUNESPACE_ADDR=127.0.0.1:51024 go test ./internal/crypto -run EngineSmoke -v
func TestEngineSmoke(t *testing.T) {
	addr := os.Getenv("RUNESPACE_ADDR")
	if addr == "" {
		t.Skip("set RUNESPACE_ADDR to run the live runespace smoke test")
	}
	const dim = 1024

	keyDir := filepath.Join(os.TempDir(), "runespace-smoke")
	kp := KeysParams{Root: keyDir, KeyID: "smoke-key", Dim: dim}
	if err := EnsureKeys(kp); err != nil {
		t.Fatalf("EnsureKeys: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	eng, err := OpenEngine(ctx, EngineParams{Keys: kp, Endpoint: addr, Insecure: true})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	defer eng.Close()
	t.Logf("engine dim=%d", eng.Dim())

	// A deterministic non-trivial vector.
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = float32(math.Sin(float64(i) * 0.01))
	}

	id, err := eng.Insert(ctx, vec, `{"title":"smoke","n":1}`)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	t.Logf("inserted id=%s", id)

	hits, err := eng.Search(ctx, vec, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	t.Logf("search returned %d hits", len(hits))
	for i, h := range hits {
		t.Logf("  hit[%d] id=%s score=%.4f meta=%s", i, h.ID, h.Score, h.Metadata)
	}
	if len(hits) == 0 {
		t.Fatalf("expected >=1 hit for the just-inserted vector, got 0")
	}
	// The inserted vector queried against itself should top the results near 1.0.
	if hits[0].Score < 0.9 {
		t.Fatalf("expected top score ~1.0 for self-query, got %.4f", hits[0].Score)
	}
	found := false
	for _, h := range hits {
		if h.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("inserted id %s not found in results", id)
	}
}
