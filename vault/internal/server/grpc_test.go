package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
	pb "github.com/CryptoLabInc/rune-admin/vault/pkg/vaultpb"
)

// ── topK_FromShards ───────────────────────────────────────────────

func TestTopKFlatSingleShard(t *testing.T) {
	scores := [][]float64{{0.5, 0.9, 0.1, 0.7}}
	shardIdx := []int32{0}
	got := topK_FromShards(scores, shardIdx, 2)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Score != 0.9 || got[1].Score != 0.7 {
		t.Errorf("scores = [%v %v], want [0.9 0.7]", got[0].Score, got[1].Score)
	}
	if got[0].RowIdx != 1 || got[1].RowIdx != 3 {
		t.Errorf("rows = [%d %d], want [1 3]", got[0].RowIdx, got[1].RowIdx)
	}
}

func TestTopKMultiShard(t *testing.T) {
	scores := [][]float64{
		{0.1, 0.2},
		{0.9, 0.5},
	}
	shardIdx := []int32{10, 20}
	got := topK_FromShards(scores, shardIdx, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	// Top-3 by score desc: 0.9 (shard 20, row 0), 0.5 (shard 20, row 1), 0.2 (shard 10, row 1)
	if got[0].Score != 0.9 || got[0].ShardIdx != 20 || got[0].RowIdx != 0 {
		t.Errorf("[0] = %+v", got[0])
	}
	if got[1].Score != 0.5 || got[1].ShardIdx != 20 || got[1].RowIdx != 1 {
		t.Errorf("[1] = %+v", got[1])
	}
	if got[2].Score != 0.2 || got[2].ShardIdx != 10 || got[2].RowIdx != 1 {
		t.Errorf("[2] = %+v", got[2])
	}
}

func TestTopKKExceedsAvailable(t *testing.T) {
	scores := [][]float64{{0.1, 0.2}}
	got := topK_FromShards(scores, []int32{0}, 10)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (clamped to available)", len(got))
	}
}

func TestTopKEmptyInput(t *testing.T) {
	got := topK_FromShards(nil, nil, 5)
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// ── error mapping ─────────────────────────────────────────────────

func TestMapTokenErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		code codes.Code
	}{
		{tokens.ErrTokenNotFound{}, codes.Unauthenticated},
		{tokens.ErrTokenExpired{User: "x"}, codes.Unauthenticated},
		{tokens.ErrRateLimit{RetryAfter: 5}, codes.ResourceExhausted},
		{tokens.ErrScope{Method: "m", RoleName: "r"}, codes.PermissionDenied},
		{tokens.ErrTopKExceeded{Requested: 50, MaxTopK: 10, RoleName: "member"}, codes.InvalidArgument},
		{errors.New("random"), codes.Unauthenticated},
	}
	for _, c := range cases {
		got, _ := mapTokenError(c.err)
		if got != c.code {
			t.Errorf("mapTokenError(%v) = %v, want %v", c.err, got, c.code)
		}
	}
}

// ── handler — token error paths (no FHE keys needed) ─────────────

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	cfg := &Config{
		Tokens: TokensConfig{TeamSecret: "test-secret"},
		Keys:   KeysConfig{Path: t.TempDir(), EmbeddingDim: 1024},
	}
	store := tokens.NewStore()
	store.LoadDefaultsWithDemoToken()
	audit, _ := NewAuditLogger(AuditConfig{Mode: ""})
	return NewVault(cfg, store, nil, audit)
}

func TestGetPublicKeyInvalidToken(t *testing.T) {
	v := newTestVault(t)
	srv := NewVaultGRPC(v)
	resp, err := srv.GetPublicKey(context.Background(), &pb.GetPublicKeyRequest{
		Token: "evt_ffffffffffffffffffffffffffffffff",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
	if resp.GetError() == "" {
		t.Error("response.error is empty")
	}
}

func TestDecryptScoresInvalidToken(t *testing.T) {
	v := newTestVault(t)
	srv := NewVaultGRPC(v)
	_, err := srv.DecryptScores(context.Background(), &pb.DecryptScoresRequest{
		Token:            "evt_ffffffffffffffffffffffffffffffff",
		EncryptedBlobB64: "AA==",
		TopK:             5,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestDecryptScoresTopKExceeded(t *testing.T) {
	v := newTestVault(t)
	srv := NewVaultGRPC(v)
	// Demo token has admin role with top_k=50; request 51.
	_, err := srv.DecryptScores(context.Background(), &pb.DecryptScoresRequest{
		Token:            tokens.DemoToken,
		EncryptedBlobB64: "AA==",
		TopK:             51,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if !strings.Contains(err.Error(), "exceeds limit 50") {
		t.Errorf("err = %v, want 'exceeds limit 50'", err)
	}
}

func TestDecryptMetadataInvalidToken(t *testing.T) {
	v := newTestVault(t)
	srv := NewVaultGRPC(v)
	_, err := srv.DecryptMetadata(context.Background(), &pb.DecryptMetadataRequest{
		Token:                 "evt_ffffffffffffffffffffffffffffffff",
		EncryptedMetadataList: []string{`{"a":"x","c":"y"}`},
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestDecryptMetadataMalformedEnvelope(t *testing.T) {
	v := newTestVault(t)
	srv := NewVaultGRPC(v)
	resp, err := srv.DecryptMetadata(context.Background(), &pb.DecryptMetadataRequest{
		Token:                 tokens.DemoToken,
		EncryptedMetadataList: []string{"not-json"},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(resp.GetError(), "Metadata decryption failed") {
		t.Errorf("error = %q, want 'Metadata decryption failed'", resp.GetError())
	}
}

// Round-trip: encrypt with crypto helpers, decrypt via gRPC handler.
// This exercises the handler against valid input without needing the FHE
// secret key.
func TestDecryptMetadataRoundTrip(t *testing.T) {
	v := newTestVault(t)
	srv := NewVaultGRPC(v)

	// Encrypt "hello" with an HKDF DEK derived from the team secret.
	agentID := "test-agent"
	plain := "hello"
	dek := mustDEK(t, "test-secret", agentID)
	ct := mustEncrypt(t, []byte(plain), dek)
	envelope := `{"a":"` + agentID + `","c":"` + ct + `"}`

	resp, err := srv.DecryptMetadata(context.Background(), &pb.DecryptMetadataRequest{
		Token:                 tokens.DemoToken,
		EncryptedMetadataList: []string{envelope},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError() != "" {
		t.Fatalf("response error: %s", resp.GetError())
	}
	got := resp.GetDecryptedMetadata()
	if len(got) != 1 || got[0] != plain {
		t.Errorf("decrypted = %v, want [%q]", got, plain)
	}
}
