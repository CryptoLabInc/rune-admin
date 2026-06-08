package tests

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/CryptoLabInc/rune-admin/vault/internal/crypto"
	"github.com/CryptoLabInc/rune-admin/vault/internal/server"
	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
	pb "github.com/CryptoLabInc/rune-admin/vault/pkg/vaultpb"
)

type fixtureBundle struct {
	Config    fixtureConfig
	Envelope  []string
	Expected  []any // any: JSON object/array/string per envelope
	ScoresB64 string
	ScoreExp  fixtureScoreExpected
	KeysDir   string
}

type fixtureConfig struct {
	TeamSecret string `json:"team_secret"`
	AgentID    string `json:"agent_id"`
	Token      string `json:"token"`
	Dim        int    `json:"dim"`
}

type fixtureScoreExpected struct {
	Score    [][]float64 `json:"score"`
	ShardIdx []int32     `json:"shard_idx"`
}

func loadFixtures(t *testing.T) *fixtureBundle {
	t.Helper()
	if !FixturesAvailable() {
		t.Skip(SkipReason)
	}
	dir := FixturesDir()

	var fb fixtureBundle
	mustJSON(t, filepath.Join(dir, "config.json"), &fb.Config)
	scoresExp := mustReadFile(t, filepath.Join(dir, "expected_scores.json"))
	if err := json.Unmarshal(scoresExp, &fb.ScoreExp); err != nil {
		t.Fatal(err)
	}
	fb.ScoresB64 = strings.TrimSpace(string(mustReadFile(t, filepath.Join(dir, "ciphertext_score.b64"))))
	mustJSON(t, filepath.Join(dir, "metadata_envelopes.json"), &fb.Envelope)
	mustJSON(t, filepath.Join(dir, "expected_metadata.json"), &fb.Expected)
	fb.KeysDir = filepath.Join(dir, "keys")
	return &fb
}

func mustJSON(t *testing.T, path string, dst any) {
	t.Helper()
	body := mustReadFile(t, path)
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// fixtureVault wires a Vault around the decrypted fixture bundle.
// keys.path points at the temp dir copy of fixture keys so envector-go-sdk
// can stage the bundle without touching the read-only fixture tree.
func fixtureVault(t *testing.T, fb *fixtureBundle) *server.Vault {
	t.Helper()
	keyDir := t.TempDir()
	for _, name := range []string{"EncKey.json", "EvalKey.json", "SecKey.json"} {
		src := filepath.Join(fb.KeysDir, name)
		if _, err := os.Stat(src); err != nil {
			t.Skipf("fixture key %s missing: %v", name, err)
		}
		body, _ := os.ReadFile(src)
		if err := os.WriteFile(filepath.Join(keyDir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// envector.OpenKeysFromFile expects WithKeyPath = directory containing
	// the JSON envelopes; use keyDir directly + KeyID = "vault-key" so the
	// joined path matches.
	keysParams := crypto.KeysParams{Root: filepath.Dir(keyDir), KeyID: filepath.Base(keyDir), Dim: fb.Config.Dim}
	keys, err := crypto.OpenSecretKey(keysParams)
	if err != nil {
		t.Fatalf("OpenSecretKey: %v", err)
	}
	t.Cleanup(func() { keys.Close() })

	cfg := &server.Config{
		Tokens: server.TokensConfig{TeamSecret: fb.Config.TeamSecret},
		Keys:   server.KeysConfig{Path: filepath.Dir(keyDir), EmbeddingDim: fb.Config.Dim},
	}
	store := tokens.NewStore()
	store.LoadDefaultsWithDemoToken()
	// Replace the demo token with the fixture's token so the test can
	// authenticate without re-keying every envelope.
	if fb.Config.Token != "" && fb.Config.Token != tokens.DemoToken {
		// Inject manually via a minimal hack: load defaults then add user.
		_, _ = store.AddRole("fixture", []string{"get_public_key", "decrypt_scores", "decrypt_metadata"}, 1000, "10000/60s")
		// AddToken would generate a new token; we need the fixture's exact
		// token string. The store has no public "InsertToken" — for tests
		// we resort to LoadFromFiles via tempfiles.
		injectFixtureToken(t, store, fb.Config.Token)
	}
	audit, _ := server.NewAuditLogger(server.AuditConfig{Mode: ""})
	return server.NewVault(cfg, store, keys, audit)
}

func injectFixtureToken(t *testing.T, store *tokens.Store, token string) {
	t.Helper()
	dir := t.TempDir()
	rolesPath := filepath.Join(dir, "roles.yml")
	tokensPath := filepath.Join(dir, "tokens.yml")
	if err := os.WriteFile(rolesPath, []byte(`roles:
  fixture:
    scope: [get_public_key, decrypt_scores, decrypt_metadata]
    top_k: 1000
    rate_limit: 10000/60s
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokensPath, []byte(`tokens:
  - user: fixture
    token: `+token+`
    role: fixture
    issued_at: "2026-01-01"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.LoadFromFiles(rolesPath, tokensPath); err != nil {
		t.Fatal(err)
	}
}

// ── decrypt_scores via gRPC handler ───────────────────────────────

func TestDecryptScoresAgainstFixture(t *testing.T) {
	fb := loadFixtures(t)
	v := fixtureVault(t, fb)
	srv := server.NewVaultGRPC(v)

	totalScores := 0
	for _, row := range fb.ScoreExp.Score {
		totalScores += len(row)
	}
	resp, err := srv.DecryptScores(context.Background(), &pb.DecryptScoresRequest{
		Token:            fb.Config.Token,
		EncryptedBlobB64: fb.ScoresB64,
		TopK:             int32(totalScores),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError() != "" {
		t.Fatalf("error: %s", resp.GetError())
	}

	expectedFlat := make([]struct {
		shard int32
		row   int32
		score float64
	}, 0)
	for i, row := range fb.ScoreExp.Score {
		shard := int32(i)
		if i < len(fb.ScoreExp.ShardIdx) {
			shard = fb.ScoreExp.ShardIdx[i]
		}
		for j, s := range row {
			expectedFlat = append(expectedFlat, struct {
				shard int32
				row   int32
				score float64
			}{shard, int32(j), s})
		}
	}
	sort.SliceStable(expectedFlat, func(i, j int) bool {
		return expectedFlat[i].score > expectedFlat[j].score
	})

	if len(resp.Results) != len(expectedFlat) {
		t.Fatalf("len(results) = %d, want %d", len(resp.Results), len(expectedFlat))
	}
	for i, got := range resp.Results {
		want := expectedFlat[i]
		if got.ShardIdx != want.shard || got.RowIdx != want.row {
			t.Errorf("[%d] shard/row = (%d,%d), want (%d,%d)", i, got.ShardIdx, got.RowIdx, want.shard, want.row)
		}
		if math.Abs(got.Score-want.score) > 1e-6 {
			t.Errorf("[%d] score = %v, want %v", i, got.Score, want.score)
		}
	}
}

func TestDecryptScoresTopKAgainstFixture(t *testing.T) {
	fb := loadFixtures(t)
	v := fixtureVault(t, fb)
	srv := server.NewVaultGRPC(v)

	allScores := []float64{}
	for _, row := range fb.ScoreExp.Score {
		allScores = append(allScores, row...)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(allScores)))
	topN := 3
	if len(allScores) < topN {
		topN = len(allScores)
	}
	resp, err := srv.DecryptScores(context.Background(), &pb.DecryptScoresRequest{
		Token:            fb.Config.Token,
		EncryptedBlobB64: fb.ScoresB64,
		TopK:             int32(topN),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != topN {
		t.Fatalf("len = %d, want %d", len(resp.Results), topN)
	}
	for i, got := range resp.Results {
		if math.Abs(got.Score-allScores[i]) > 1e-6 {
			t.Errorf("[%d] score = %v, want %v", i, got.Score, allScores[i])
		}
	}
	for i := 1; i < len(resp.Results); i++ {
		if resp.Results[i].Score > resp.Results[i-1].Score {
			t.Errorf("results not descending at %d", i)
		}
	}
}

// ── decrypt_metadata via gRPC handler ─────────────────────────────

func TestDecryptMetadataSingleAgainstFixture(t *testing.T) {
	fb := loadFixtures(t)
	v := fixtureVault(t, fb)
	srv := server.NewVaultGRPC(v)

	if len(fb.Envelope) == 0 {
		t.Skip("no envelopes in fixture")
	}
	resp, err := srv.DecryptMetadata(context.Background(), &pb.DecryptMetadataRequest{
		Token:                 fb.Config.Token,
		EncryptedMetadataList: []string{fb.Envelope[0]},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError() != "" {
		t.Fatalf("error: %s", resp.GetError())
	}
	if len(resp.DecryptedMetadata) != 1 {
		t.Fatalf("len = %d", len(resp.DecryptedMetadata))
	}
	got := decodeAny(t, resp.DecryptedMetadata[0])
	want := fb.Expected[0]
	if !jsonEq(got, want) {
		t.Errorf("metadata mismatch\n got %#v\nwant %#v", got, want)
	}
}

func TestDecryptMetadataMultipleAgainstFixture(t *testing.T) {
	fb := loadFixtures(t)
	v := fixtureVault(t, fb)
	srv := server.NewVaultGRPC(v)

	resp, err := srv.DecryptMetadata(context.Background(), &pb.DecryptMetadataRequest{
		Token:                 fb.Config.Token,
		EncryptedMetadataList: fb.Envelope,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError() != "" {
		t.Fatalf("error: %s", resp.GetError())
	}
	if len(resp.DecryptedMetadata) != len(fb.Expected) {
		t.Fatalf("len = %d, want %d", len(resp.DecryptedMetadata), len(fb.Expected))
	}
	for i, raw := range resp.DecryptedMetadata {
		got := decodeAny(t, raw)
		if !jsonEq(got, fb.Expected[i]) {
			t.Errorf("[%d] mismatch\n got %#v\nwant %#v", i, got, fb.Expected[i])
		}
	}
}

// decodeAny tries to JSON-parse a string; returns the raw string on failure.
func decodeAny(_ *testing.T, raw string) any {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v
	}
	return raw
}

// jsonEq compares two values by re-serialising to JSON. Handles float vs
// int promotion that comes out of encoding/json's default decoding.
func jsonEq(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
