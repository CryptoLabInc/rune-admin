package crypto

import (
	"path/filepath"
	"testing"
)

func TestKeysExistFalseForMissingDir(t *testing.T) {
	p := KeysParams{Root: filepath.Join(t.TempDir(), "no-such"), KeyID: "vault-key", Dim: 1024}
	if KeysExist(p) {
		t.Error("KeysExist = true for missing dir")
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
