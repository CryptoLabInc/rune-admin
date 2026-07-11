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

// ── handler — token error paths (no engine needed; auth runs first) ──

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

func TestGetAgentManifestInvalidToken(t *testing.T) {
	srv := NewVaultGRPC(newTestVault(t))
	resp, err := srv.GetAgentManifest(context.Background(), &pb.GetAgentManifestRequest{
		Token: "evt_ffffffffffffffffffffffffffffffff",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
	if resp.GetError() == "" {
		t.Error("response.error is empty")
	}
}

func TestInsertInvalidToken(t *testing.T) {
	srv := NewVaultGRPC(newTestVault(t))
	_, err := srv.Insert(context.Background(), &pb.InsertRequest{
		Token:              "evt_ffffffffffffffffffffffffffffffff",
		Id:                 "test-id-1",
		RmpItem:            []byte{1},
		MmItem:             []byte{2},
		ClusterId:          0,
		CentroidSetVersion: "v1",
		Metadata:           `{"a":"x","c":"y"}`,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestSearchInvalidToken(t *testing.T) {
	srv := NewVaultGRPC(newTestVault(t))
	_, err := srv.Search(context.Background(), &pb.SearchRequest{
		Token:  "evt_ffffffffffffffffffffffffffffffff",
		Vector: []float32{0.1, 0.2},
		TopK:   5,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestSearchTopKExceeded(t *testing.T) {
	srv := NewVaultGRPC(newTestVault(t))
	// Demo token has admin role with top_k=50; request 51 → rejected before engine.
	_, err := srv.Search(context.Background(), &pb.SearchRequest{
		Token:  tokens.DemoToken,
		Vector: []float32{0.1, 0.2},
		TopK:   51,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if !strings.Contains(err.Error(), "exceeds limit 50") {
		t.Errorf("err = %v, want 'exceeds limit 50'", err)
	}
}
