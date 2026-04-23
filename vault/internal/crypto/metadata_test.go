package crypto

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// HKDF golden vector — derived from Python:
//
//	cryptography.hazmat.primitives.kdf.hkdf.HKDF(
//	    algorithm=SHA256(), length=32, salt=None, info=b"abc123def456")
//	    .derive(b"test-team-secret-32-bytes-please")
const (
	goldenTeamSecret = "test-team-secret-32-bytes-please"
	goldenAgentID    = "abc123def456"
	goldenDEKHex     = "0e4757183d2aa64e384012a494accb6fa18b8ff144c97b78b91bec3b6720767a"

	demoToken        = "evt_0000000000000000000000000000demo"
	demoTokenAgentID = "a84c4af3aac6f4479a6741d9df0cda65"
)

func TestDeriveAgentKeyMatchesPython(t *testing.T) {
	got, err := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString(goldenDEKHex)
	if !bytes.Equal(got, want) {
		t.Errorf("DEK mismatch\n got %x\nwant %s", got, goldenDEKHex)
	}
}

func TestDeriveAgentKeyDeterministic(t *testing.T) {
	d1, err := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d1, d2) {
		t.Error("HKDF output non-deterministic")
	}
}

func TestDeriveAgentKeyDifferentAgents(t *testing.T) {
	a, _ := DeriveAgentKey(goldenTeamSecret, "agent-a")
	b, _ := DeriveAgentKey(goldenTeamSecret, "agent-b")
	if bytes.Equal(a, b) {
		t.Error("different agents produced same DEK")
	}
}

func TestDeriveAgentKeyEmptyTeamSecret(t *testing.T) {
	if _, err := DeriveAgentKey("", "x"); err == nil {
		t.Error("empty team secret should error")
	}
}

func TestAgentIDFromDemoToken(t *testing.T) {
	got := AgentIDFromToken(demoToken)
	if got != demoTokenAgentID {
		t.Errorf("agent_id = %q, want %q", got, demoTokenAgentID)
	}
	if len(got) != 32 {
		t.Errorf("agent_id length = %d, want 32", len(got))
	}
}

// ── round-trip ────────────────────────────────────────────────────

func TestEncryptDecryptRoundTripStr(t *testing.T) {
	key, _ := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	plaintext := []byte("hello world")
	ct, err := EncryptMetadata(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptMetadata(ct, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEncryptDecryptRoundTripBinary(t *testing.T) {
	key, _ := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	plaintext := []byte{0, 1, 2, 3, 'b', 'i', 'n', 'a', 'r', 'y'}
	ct, err := EncryptMetadata(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptMetadata(ct, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round-trip mismatch: got %x, want %x", got, plaintext)
	}
}

func TestEncryptDecryptRoundTripJSON(t *testing.T) {
	key, _ := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	plaintext := []byte(`{"foo":"bar","n":42}`)
	ct, err := EncryptMetadata(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptMetadata(ct, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round-trip mismatch: got %s, want %s", got, plaintext)
	}
}

// IV must change every encryption (random 16 bytes prefixed)
func TestEncryptUsesRandomIV(t *testing.T) {
	key, _ := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	pt := []byte("same plaintext")
	ct1, _ := EncryptMetadata(pt, key)
	ct2, _ := EncryptMetadata(pt, key)
	if ct1 == ct2 {
		t.Error("two encryptions of same plaintext returned identical ciphertext (IV reuse?)")
	}
}

// ── cross-language: decrypt Python-produced ciphertexts ──────────

func TestDecryptPythonGoldenStr(t *testing.T) {
	key, _ := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	// Python: encrypt_metadata("hello world", dek) →
	pythonCT := "OhawM+14dWV/2KJwL0Ud3pqJpP6Mr7XVfCsM"
	got, err := DecryptMetadata(pythonCT, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Errorf("decrypt = %q, want %q", got, "hello world")
	}
}

func TestDecryptPythonGoldenDict(t *testing.T) {
	key, _ := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	// Python: encrypt_metadata({"foo": "bar", "n": 42}, dek) →
	// (the dict is JSON-serialized as {"foo":"bar","n":42} — separators=(",", ":"))
	pythonCT := "x801QtEfmRM9Hg9ncV0p1aHbcPTBGI/63+L7c/TPVoPFRS/p"
	got, err := DecryptMetadata(pythonCT, key)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"foo":"bar","n":42}`
	if string(got) != want {
		t.Errorf("decrypt = %q, want %q", got, want)
	}
}

func TestDecryptPythonGoldenBytes(t *testing.T) {
	key, _ := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	// Python: encrypt_metadata(b"\x00\x01\x02\x03binary", dek)
	pythonCT := "zAoZPxGEAucFdLBQWyahXBFCCwjLL8z2RjA="
	got, err := DecryptMetadata(pythonCT, key)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 1, 2, 3, 'b', 'i', 'n', 'a', 'r', 'y'}
	if !bytes.Equal(got, want) {
		t.Errorf("decrypt = %x, want %x", got, want)
	}
}

// ── error cases ──────────────────────────────────────────────────

func TestDecryptInvalidKey(t *testing.T) {
	short := []byte("short")
	if _, err := DecryptMetadata("anything", short); err != ErrInvalidKey {
		t.Errorf("err = %v, want ErrInvalidKey", err)
	}
}

func TestEncryptInvalidKey(t *testing.T) {
	short := []byte("short")
	if _, err := EncryptMetadata([]byte("x"), short); err != ErrInvalidKey {
		t.Errorf("err = %v, want ErrInvalidKey", err)
	}
}

func TestDecryptInvalidBase64(t *testing.T) {
	key := make([]byte, 32)
	if _, err := DecryptMetadata("!!!not-base64!!!", key); err == nil {
		t.Error("invalid base64 should error")
	}
}

func TestDecryptShortCiphertext(t *testing.T) {
	key := make([]byte, 32)
	short := base64.StdEncoding.EncodeToString([]byte("only12bytes!"))
	if _, err := DecryptMetadata(short, key); err != ErrInvalidCiphertext {
		t.Errorf("err = %v, want ErrInvalidCiphertext", err)
	}
}

// ── secret leakage guard ─────────────────────────────────────────

// Ensure key bytes never appear in error messages.
func TestErrorsDoNotLeakKey(t *testing.T) {
	key, _ := DeriveAgentKey(goldenTeamSecret, goldenAgentID)
	keyHex := hex.EncodeToString(key)
	_, err := DecryptMetadata("!!!", key)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), keyHex[:16]) {
		t.Errorf("error message leaked key prefix: %q", err)
	}
}
