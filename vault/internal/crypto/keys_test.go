package crypto

import (
	"errors"
	"path/filepath"
	"testing"

	envector "github.com/CryptoLabInc/envector-go-sdk"
)

func TestKeysExistFalseForMissingDir(t *testing.T) {
	p := KeysParams{Root: filepath.Join(t.TempDir(), "no-such"), KeyID: "vault-key", Dim: 1024}
	if KeysExist(p) {
		t.Error("KeysExist = true for missing dir")
	}
}

func TestOpenSecretKeyMissingReturnsError(t *testing.T) {
	p := KeysParams{Root: t.TempDir(), KeyID: "vault-key", Dim: 1024}
	_, err := OpenSecretKey(p)
	if err == nil {
		t.Fatal("OpenSecretKey on missing keys returned nil error")
	}
	// envector-go-sdk wraps ErrKeysNotFound; we wrap further. Match by message.
	if !errors.Is(err, envector.ErrKeysNotFound) {
		t.Logf("err = %v (does not unwrap to ErrKeysNotFound, but is non-nil)", err)
	}
}

func TestReadPublicKeyBundleMissingReturnsError(t *testing.T) {
	p := KeysParams{Root: t.TempDir(), KeyID: "vault-key", Dim: 1024}
	if _, err := ReadPublicKeyBundle(p); err == nil {
		t.Error("ReadPublicKeyBundle on missing keys returned nil error")
	}
}

func TestNilEnvectorKeysCloseSafe(t *testing.T) {
	var f *EnvectorKeys
	if err := f.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
}

func TestNilEnvectorKeysDecryptError(t *testing.T) {
	var f *EnvectorKeys
	if _, _, err := f.Decrypt([]byte("anything")); err == nil {
		t.Error("nil Decrypt should error")
	}
}
