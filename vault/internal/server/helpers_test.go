package server

import (
	"testing"

	"github.com/CryptoLabInc/rune-admin/vault/internal/crypto"
)

func mustDEK(t *testing.T, secret, agentID string) []byte {
	t.Helper()
	d, err := crypto.DeriveAgentKey(secret, agentID)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustEncrypt(t *testing.T, plaintext, key []byte) string {
	t.Helper()
	ct, err := crypto.EncryptMetadata(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	return ct
}
