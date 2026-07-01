package server

import (
	"context"
	"math"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/CryptoLabInc/rune-admin/vault/internal/crypto"
	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
	pb "github.com/CryptoLabInc/rune-admin/vault/pkg/vaultpb"
)

// TestVaultServiceL2 brings up the VaultService gRPC server in-process against a
// LIVE runespace and exercises the mcp-facing contract end to end: token auth +
// Insert (encrypt + seal metadata) + Search (decrypt + open metadata). Gated on
// RUNESPACE_ADDR. Reuses the smoke-test key dir so it matches whatever eval key
// runespace already has registered.
//
//	RUNESPACE_ADDR=127.0.0.1:51024 go test ./internal/server -run VaultServiceL2 -v
func TestVaultServiceL2(t *testing.T) {
	addr := os.Getenv("RUNESPACE_ADDR")
	if addr == "" {
		t.Skip("set RUNESPACE_ADDR to run the live VaultService L2 test")
	}
	const dim = 1024

	kp := crypto.KeysParams{Root: filepath.Join(os.TempDir(), "runespace-smoke"), KeyID: "smoke-key", Dim: dim}
	if err := crypto.EnsureKeys(kp); err != nil {
		t.Fatalf("EnsureKeys: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	eng, err := crypto.OpenEngine(ctx, crypto.EngineParams{Keys: kp, Endpoint: addr, Insecure: true})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	defer eng.Close()

	cfg := &Config{
		Tokens: TokensConfig{TeamSecret: "test-secret"},
		Keys:   KeysConfig{Path: kp.Root, EmbeddingDim: dim, IndexName: "test"},
	}
	store := tokens.NewStore()
	store.LoadDefaultsWithDemoToken()
	audit, _ := NewAuditLogger(AuditConfig{Mode: ""})
	v := NewVault(cfg, store, eng, audit)

	// In-process gRPC server on a random port.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer(grpc.MaxRecvMsgSize(MaxMessageSize), grpc.MaxSendMsgSize(MaxMessageSize))
	pb.RegisterVaultServiceServer(gs, NewVaultGRPC(v))
	go gs.Serve(lis)
	defer gs.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MaxMessageSize), grpc.MaxCallSendMsgSize(MaxMessageSize)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewVaultServiceClient(conn)

	// A distinctive vector so the self-query is the clear top hit.
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = float32(math.Cos(float64(i)*0.013 + 0.5))
	}
	const meta = `{"title":"L2 vault test","n":7}`

	ins, err := client.Insert(ctx, &pb.InsertRequest{Token: tokens.DemoToken, Vector: vec, Metadata: meta})
	if err != nil {
		t.Fatalf("Insert RPC: %v", err)
	}
	t.Logf("Insert ok id=%s", ins.GetId())

	sr, err := client.Search(ctx, &pb.SearchRequest{Token: tokens.DemoToken, Vector: vec, TopK: 5})
	if err != nil {
		t.Fatalf("Search RPC: %v", err)
	}
	t.Logf("Search returned %d hits", len(sr.GetHits()))
	for i, h := range sr.GetHits() {
		t.Logf("  hit[%d] id=%s score=%.4f meta=%s", i, h.GetId(), h.GetScore(), h.GetMetadata())
	}
	if len(sr.GetHits()) == 0 {
		t.Fatal("no hits")
	}
	top := sr.GetHits()[0]
	if top.GetScore() < 0.9 {
		t.Fatalf("top score %.4f < 0.9", top.GetScore())
	}
	// The just-inserted item must come back with metadata SEALED then OPENED by
	// the vault → we get the original plaintext JSON, proving the L2 seal/open path.
	if top.GetId() != ins.GetId() {
		t.Fatalf("top id %s != inserted %s", top.GetId(), ins.GetId())
	}
	if top.GetMetadata() != meta {
		t.Fatalf("metadata round-trip mismatch: got %q want %q", top.GetMetadata(), meta)
	}
}
