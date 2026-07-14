package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	runespace "github.com/CryptoLabInc/runespace-sdk"

	"github.com/CryptoLabInc/rune-console/runeconsole/internal/crypto"
	"github.com/CryptoLabInc/rune-console/runeconsole/internal/tokens"
	pb "github.com/CryptoLabInc/rune-console/runeconsole/pkg/consolepb"
)

// TestConsoleServiceL2 brings up the ConsoleService gRPC server in-process against
// a LIVE runespace and plays the rune-mcp role end to end under the
// client-side-crypto contract:
//
//	GetAgentManifest → save EncKey pair → OpenKeys(Enc only)
//	GetCentroids relay → assign cluster
//	EncryptFlat/EncryptClustered + seal metadata (agent_dek)
//	Insert (pre-encrypted forward) → Search → opened-metadata round trip
//
// Gated on RUNESPACE_ADDR. Reuses the smoke-test key dir so it matches
// whatever eval key runespace already has registered.
//
//	RUNESPACE_ADDR=127.0.0.1:51024 go test ./internal/server -run ConsoleServiceL2 -v
func TestConsoleServiceL2(t *testing.T) {
	addr := os.Getenv("RUNESPACE_ADDR")
	if addr == "" {
		t.Skip("set RUNESPACE_ADDR to run the live ConsoleService L2 test")
	}
	const dim = 1024

	// The key set must be the one whose EvalKey the target runespace has
	// registered — decrypting with a mismatched SecKey yields garbage scores.
	// Point RUNESPACE_KEYS_ROOT/RUNESPACE_KEY_ID at that set; default is a
	// throwaway smoke set (fresh engine only).
	kp := crypto.KeysParams{Root: filepath.Join(os.TempDir(), "runespace-smoke"), KeyID: "smoke-key", Dim: dim}
	if r := os.Getenv("RUNESPACE_KEYS_ROOT"); r != "" {
		kp.Root = r
	}
	if id := os.Getenv("RUNESPACE_KEY_ID"); id != "" {
		kp.KeyID = id
	}
	if err := crypto.EnsureKeys(kp); err != nil {
		t.Fatalf("EnsureKeys: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	v := NewConsole(cfg, store, eng, audit)
	v.bundleParams.KeyID = kp.KeyID // manifest must read the smoke key dir

	// In-process gRPC server on a random port.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer(grpc.MaxRecvMsgSize(MaxMessageSize), grpc.MaxSendMsgSize(MaxMessageSize))
	pb.RegisterConsoleServiceServer(gs, NewVaultGRPC(v))
	go gs.Serve(lis)
	defer gs.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MaxMessageSize), grpc.MaxCallSendMsgSize(MaxMessageSize)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewConsoleServiceClient(conn)

	// ── mcp step 1: manifest → EncKey pair + agent_dek ──────────────
	mf, err := client.GetAgentManifest(ctx, &pb.GetAgentManifestRequest{Token: tokens.DemoToken})
	if err != nil {
		t.Fatalf("GetAgentManifest RPC: %v", err)
	}
	var bundle struct {
		EncKeyJSON string `json:"EncKey.json"`
		MMEncKey   string `json:"mm_enc_key"`
		AgentID    string `json:"agent_id"`
		AgentDEK   string `json:"agent_dek"`
		KeyID      string `json:"key_id"`
		Dim        int    `json:"dim"`
		Insert     string `json:"insert"`
	}
	if err := json.Unmarshal([]byte(mf.GetManifestJson()), &bundle); err != nil {
		t.Fatalf("manifest parse: %v", err)
	}
	if bundle.Insert != "pre_encrypted" {
		t.Fatalf("capability = %q, want pre_encrypted", bundle.Insert)
	}
	if bundle.EncKeyJSON == "" || bundle.MMEncKey == "" || bundle.AgentDEK == "" {
		t.Fatal("manifest missing EncKey/mm_enc_key/agent_dek")
	}
	dek, err := base64.StdEncoding.DecodeString(bundle.AgentDEK)
	if err != nil {
		t.Fatalf("agent_dek decode: %v", err)
	}
	mmKey, err := base64.StdEncoding.DecodeString(bundle.MMEncKey)
	if err != nil {
		t.Fatalf("mm_enc_key decode: %v", err)
	}

	// ── mcp step 2: save keys in SDK layout, open Enc-only ──────────
	clientKeyRoot := t.TempDir()
	clientKeyDir := filepath.Join(clientKeyRoot, bundle.KeyID)
	if err := os.MkdirAll(filepath.Join(clientKeyDir, "mm"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clientKeyDir, "EncKey.json"), []byte(bundle.EncKeyJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clientKeyDir, "mm", "EncKey.bin"), mmKey, 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := runespace.OpenKeys(
		runespace.WithKeyPath(clientKeyDir),
		runespace.WithKeyID(bundle.KeyID),
		runespace.WithKeyDim(bundle.Dim),
		runespace.WithKeyParts(runespace.KeyPartEnc),
	)
	if err != nil {
		t.Fatalf("client OpenKeys (Enc only): %v", err)
	}
	defer keys.Close()

	// ── mcp step 3: centroid relay → cluster assignment ─────────────
	cstream, err := client.GetCentroids(ctx, &pb.GetCentroidsRequest{Token: tokens.DemoToken})
	if err != nil {
		t.Fatalf("GetCentroids RPC: %v", err)
	}
	relay := &runespace.CentroidSet{}
	for {
		chunk, err := cstream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("GetCentroids recv: %v", err)
		}
		switch p := chunk.GetPayload().(type) {
		case *pb.CentroidChunk_Header:
			relay.Version = p.Header.GetVersion()
			relay.Dim = int(p.Header.GetDim())
		case *pb.CentroidChunk_Batch:
			for _, c := range p.Batch.GetCentroids() {
				relay.Vectors = append(relay.Vectors, c.GetVec())
			}
		}
	}
	if !relay.Enabled() {
		t.Fatal("centroid relay returned a disabled set")
	}
	t.Logf("centroid relay: version=%s nlist=%d", relay.Version, len(relay.Vectors))

	// ── mcp step 4: encrypt + seal + forward ────────────────────────
	// Unique per run: an identical vector from a previous run would tie at
	// score 1.0 and steal the top slot from this run's insert.
	phase := float64(time.Now().UnixNano()%100000) / 1000.0
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = float32(math.Cos(float64(i)*0.013 + phase))
	}
	var norm float64
	for _, x := range vec {
		norm += float64(x) * float64(x)
	}
	inv := float32(1 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= inv
	}

	rmpBlob, err := keys.EncryptFlat(vec)
	if err != nil {
		t.Fatalf("EncryptFlat: %v", err)
	}
	mmBlob, err := keys.EncryptClustered(vec)
	if err != nil {
		t.Fatalf("EncryptClustered: %v", err)
	}

	const meta = `{"title":"L2 console test","n":7}`
	ct, err := crypto.EncryptMetadata([]byte(meta), dek)
	if err != nil {
		t.Fatalf("client seal: %v", err)
	}
	envJSON, _ := json.Marshal(envelope{AgentID: bundle.AgentID, Cipher: ct})

	id := uuid.NewString()
	ins, err := client.Insert(ctx, &pb.InsertRequest{
		Token:              tokens.DemoToken,
		Id:                 id,
		RmpItem:            rmpBlob,
		MmItem:             mmBlob,
		ClusterId:          relay.Assign(vec),
		CentroidSetVersion: relay.Version,
		Metadata:           string(envJSON),
	})
	if err != nil {
		t.Fatalf("Insert RPC: %v", err)
	}
	if ins.GetId() != id {
		t.Fatalf("Insert echoed id %q, want %q", ins.GetId(), id)
	}
	t.Logf("Insert ok id=%s (client-encrypted forward)", ins.GetId())

	// ── recall: console decrypts scores + opens metadata ──────────────
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
	if top.GetId() != id {
		t.Fatalf("top id %s != inserted %s", top.GetId(), id)
	}
	// Client-sealed metadata must come back OPENED by the console — proving the
	// seal(client)/open(console) split of the target architecture.
	if top.GetMetadata() != meta {
		t.Fatalf("metadata round-trip mismatch: got %q want %q", top.GetMetadata(), meta)
	}
}
