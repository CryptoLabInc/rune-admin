// Package crypto provides metadata key derivation, AES-256-CTR metadata
// encryption, and FHE key lifecycle wrappers around envector-go-sdk.
//
// Wire format for metadata ciphertext:
//
//	base64( IV (16 bytes) || ciphertext (variable) )
//
// AES-256-CTR is unauthenticated; integrity is enforced by upstream JSON
// envelopes and HKDF-derived per-agent keys.
//
// TODO: migrate to AES-256-GCM (AEAD) — keys are issued directly between
// rune and rune-vault so there is no external wire-format compatibility
// constraint. Requires coordinated update of the rune-side encryption path.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	dekLen = 32
	ivLen  = 16
)

var (
	ErrInvalidKey        = errors.New("crypto: AES key must be 32 bytes")
	ErrInvalidCiphertext = errors.New("crypto: ciphertext too short (need >= 16 bytes after base64 decode)")
)

// DeriveAgentKey returns a 32-byte AES-256 DEK derived from the team-wide
// secret and a per-agent identifier via HKDF-SHA256. Mirrors
// vault.vault_core.derive_agent_key (HKDF salt=None, info=agent_id utf-8).
func DeriveAgentKey(teamSecret, agentID string) ([]byte, error) {
	if teamSecret == "" {
		return nil, errors.New("crypto: team_secret is empty")
	}
	r := hkdf.New(sha256.New, []byte(teamSecret), nil, []byte(agentID))
	dek := make([]byte, dekLen)
	if _, err := io.ReadFull(r, dek); err != nil {
		return nil, fmt.Errorf("crypto: hkdf read: %w", err)
	}
	return dek, nil
}

// AgentIDFromToken returns the per-token agent identifier:
// the first 32 hex chars of SHA-256(token).
func AgentIDFromToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:32]
}

// EncryptMetadata produces a base64-encoded AES-256-CTR ciphertext with a
// random 16-byte IV prefixed to the ciphertext.
func EncryptMetadata(plaintext, key []byte) (string, error) {
	if len(key) != dekLen {
		return "", ErrInvalidKey
	}
	iv := make([]byte, ivLen)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("crypto: read iv: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	ct := make([]byte, len(plaintext))
	cipher.NewCTR(block, iv).XORKeyStream(ct, plaintext)
	out := make([]byte, 0, ivLen+len(ct))
	out = append(out, iv...)
	out = append(out, ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// DecryptMetadata reverses EncryptMetadata: base64-decode the input, peel
// off the 16-byte IV, then AES-256-CTR decrypt. Output is raw bytes; the
// caller decides whether to UTF-8/JSON-parse them.
func DecryptMetadata(ctB64 string, key []byte) ([]byte, error) {
	if len(key) != dekLen {
		return nil, ErrInvalidKey
	}
	raw, err := base64.StdEncoding.DecodeString(ctB64)
	if err != nil {
		return nil, fmt.Errorf("crypto: base64 decode: %w", err)
	}
	if len(raw) < ivLen {
		return nil, ErrInvalidCiphertext
	}
	iv := raw[:ivLen]
	ct := raw[ivLen:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pt := make([]byte, len(ct))
	cipher.NewCTR(block, iv).XORKeyStream(pt, ct)
	return pt, nil
}
