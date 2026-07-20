package crypto

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeysExistFalseForMissingDir(t *testing.T) {
	p := KeysParams{Root: filepath.Join(t.TempDir(), "no-such"), KeyID: "rune-console-key", Dim: 1024}
	if KeysExist(p) {
		t.Error("KeysExist = true for missing dir")
	}
}

// TestSearchRefusesEmptyScope — the console-side default-deny backstop: an
// empty filterScope means "filtering off" to runespace (org-wide recall),
// so Engine.Search must fail closed before any engine call. The zero
// Engine has a nil client — reaching it would panic, so a clean error
// also proves no client call was attempted.
func TestSearchRefusesEmptyScope(t *testing.T) {
	e := &Engine{}
	hits, err := e.Search(context.Background(), []float32{0.1, 0.2}, 5)
	if err == nil {
		t.Fatal("Search with empty scope must fail closed, got nil error")
	}
	if !strings.Contains(err.Error(), "empty recall scope") {
		t.Errorf("err = %v, want 'empty recall scope'", err)
	}
	if hits != nil {
		t.Errorf("hits = %v, want nil", hits)
	}
}

func TestNilEngineCloseSafe(t *testing.T) {
	var e *Engine
	// A zero Engine value Close is safe (nil client/keys).
	e = &Engine{}
	if err := e.Close(); err != nil {
		t.Errorf("empty Engine Close: %v", err)
	}
}
