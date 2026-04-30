package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/CryptoLabInc/rune-admin/vault/internal/crypto"
	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
	pb "github.com/CryptoLabInc/rune-admin/vault/pkg/vaultpb"
)

// MaxMessageSize bounds gRPC frames. EvalKey alone can be tens of MB.
const MaxMessageSize = 256 * 1024 * 1024

// Vault is the runtime container shared by all RPC handlers and the
// admin UDS server. It owns the long-lived token store, FHE key handle,
// and audit logger. Construct via NewVault, tear down via Close.
type Vault struct {
	cfg    *Config
	tokens *tokens.Store
	keys   *crypto.EnvectorKeys
	audit  *AuditLogger

	// Cached bundle pieces from disk. Re-read on demand to pick up
	// rotated keys without restarting; kept here for zero-copy reuse.
	bundleParams crypto.KeysParams

	// restartRequested is set by the admin /restart endpoint so that
	// Serve can return ErrRestartRequested and the process exits with
	// code 1, triggering service-manager (systemd/launchd) restart.
	restartRequested atomic.Bool
}

// NewVault wires all subsystems together. Caller is responsible for Close.
func NewVault(cfg *Config, tokenStore *tokens.Store, keys *crypto.EnvectorKeys, audit *AuditLogger) *Vault {
	return &Vault{
		cfg:    cfg,
		tokens: tokenStore,
		keys:   keys,
		audit:  audit,
		bundleParams: crypto.KeysParams{
			Root:  cfg.Keys.Path,
			KeyID: defaultKeyID(cfg),
			Dim:   cfg.Keys.EmbeddingDim,
		},
	}
}

func defaultKeyID(_ *Config) string {
	// Fixed for Phase 1 — Python pins KEY_ID="vault-key" in vault_core.py:30.
	// Surfaced as a helper so a future config field can override.
	return "vault-key"
}

// Tokens exposes the token store for the admin UDS server.
func (v *Vault) Tokens() *tokens.Store { return v.tokens }

// RequestRestart marks the vault for restart. Serve returns ErrRestartRequested
// after the current shutdown sequence completes.
func (v *Vault) RequestRestart() { v.restartRequested.Store(true) }

// RestartRequested reports whether RequestRestart was called.
func (v *Vault) RestartRequested() bool { return v.restartRequested.Load() }

// Config exposes the resolved config (e.g., for status reporting).
func (v *Vault) Config() *Config { return v.cfg }

// Close releases the FHE key handle. The audit logger and token store
// are owned by the caller (typically the daemon main).
func (v *Vault) Close() error {
	if v.keys != nil {
		_ = v.keys.Close()
	}
	return nil
}

// VaultGRPC is the gRPC service wrapper. Exposed for grpc.RegisterService.
type VaultGRPC struct {
	pb.UnimplementedVaultServiceServer
	v *Vault
}

func NewVaultGRPC(v *Vault) *VaultGRPC { return &VaultGRPC{v: v} }

// ── GetPublicKey ──────────────────────────────────────────────────

func (s *VaultGRPC) GetPublicKey(ctx context.Context, req *pb.GetPublicKeyRequest) (*pb.GetPublicKeyResponse, error) {
	start := time.Now()
	user := s.v.tokens.GetUsername(req.GetToken())
	if user == "" {
		user = "unknown"
	}
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "get_public_key", user, nil, resultCount, statusStr, errDetail, time.Since(start))
	}()

	username, role, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return &pb.GetPublicKeyResponse{Error: msg}, status.Error(st, msg)
	}
	user = username
	if err := role.CheckScope("get_public_key"); err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.GetPublicKeyResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}

	bundle, err := s.v.buildBundle(req.GetToken())
	if err != nil {
		statusStr = "error"
		ed := err.Error()
		errDetail = &ed
		return &pb.GetPublicKeyResponse{Error: err.Error()}, status.Error(codes.Internal, err.Error())
	}
	js, err := json.Marshal(bundle)
	if err != nil {
		statusStr = "error"
		ed := err.Error()
		errDetail = &ed
		return &pb.GetPublicKeyResponse{Error: err.Error()}, status.Error(codes.Internal, err.Error())
	}
	resultCount = 1
	return &pb.GetPublicKeyResponse{KeyBundleJson: string(js)}, nil
}

// buildBundle assembles the per-token JSON bundle returned by GetPublicKey.
// Order of keys is irrelevant — clients parse by name.
func (s *Vault) buildBundle(token string) (map[string]any, error) {
	pub, err := crypto.ReadPublicKeyBundle(s.bundleParams)
	if err != nil {
		return nil, err
	}
	bundle := map[string]any{
		"EncKey.json":  pub.EncKey,
		"EvalKey.json": pub.EvalKey,
		"key_id":       s.bundleParams.KeyID,
	}
	if s.cfg.Keys.IndexName != "" {
		bundle["index_name"] = s.cfg.Keys.IndexName
	}
	agentID := crypto.AgentIDFromToken(token)
	dek, err := crypto.DeriveAgentKey(s.cfg.Tokens.TeamSecret, agentID)
	if err != nil {
		return nil, err
	}
	bundle["agent_id"] = agentID
	bundle["agent_dek"] = base64.StdEncoding.EncodeToString(dek)
	bundle["envector_endpoint"] = s.cfg.Envector.Endpoint
	bundle["envector_api_key"] = s.cfg.Envector.APIKey
	return bundle, nil
}

// ── DecryptScores ─────────────────────────────────────────────────

func (s *VaultGRPC) DecryptScores(ctx context.Context, req *pb.DecryptScoresRequest) (*pb.DecryptScoresResponse, error) {
	start := time.Now()
	topK := req.GetTopK()
	user := s.v.tokens.GetUsername(req.GetToken())
	if user == "" {
		user = "unknown"
	}
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "decrypt_scores", user, &topK, resultCount, statusStr, errDetail, time.Since(start))
	}()

	username, role, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return &pb.DecryptScoresResponse{Error: msg}, status.Error(st, msg)
	}
	user = username
	if err := role.CheckScope("decrypt_scores"); err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.DecryptScoresResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}
	if int(topK) > role.TopK {
		te := tokens.ErrTopKExceeded{Requested: int(topK), MaxTopK: role.TopK, RoleName: role.Name}
		statusStr = "denied"
		msg := te.Error()
		errDetail = &msg
		return &pb.DecryptScoresResponse{Error: msg}, status.Error(codes.InvalidArgument, msg)
	}

	blob, err := base64.StdEncoding.DecodeString(req.GetEncryptedBlobB64())
	if err != nil {
		statusStr = "error"
		msg := fmt.Sprintf("Deserialization failed: %s", err.Error())
		errDetail = &msg
		return &pb.DecryptScoresResponse{Error: msg}, status.Error(codes.InvalidArgument, msg)
	}
	if s.v.keys == nil {
		statusStr = "error"
		msg := "FHE key not loaded"
		errDetail = &msg
		return &pb.DecryptScoresResponse{Error: msg}, status.Error(codes.Internal, msg)
	}
	scores2D, shardIdx, err := s.v.keys.Decrypt(blob)
	if err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		return &pb.DecryptScoresResponse{Error: msg}, status.Error(codes.Internal, msg)
	}
	entries := topK_FromShards(scores2D, shardIdx, int(topK))
	resultCount = len(entries)
	return &pb.DecryptScoresResponse{Results: entries}, nil
}

// topK_FromShards mirrors vault_core._decrypt_scores_impl L276-285:
// flatten 2D scores into (shard_idx, row_idx, score), sort desc by score,
// take top k. Output order matches Python's heapq.nlargest.
func topK_FromShards(scores2D [][]float64, shardIdx []int32, k int) []*pb.ScoreEntry {
	type item struct {
		shard, row int32
		score      float64
	}
	all := make([]item, 0)
	for i, row := range scores2D {
		shard := int32(i)
		if i < len(shardIdx) {
			shard = shardIdx[i]
		}
		for j, v := range row {
			all = append(all, item{shard: shard, row: int32(j), score: v})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	if k > len(all) {
		k = len(all)
	}
	out := make([]*pb.ScoreEntry, k)
	for i := 0; i < k; i++ {
		out[i] = &pb.ScoreEntry{
			ShardIdx: all[i].shard,
			RowIdx:   all[i].row,
			Score:    all[i].score,
		}
	}
	return out
}

// ── DecryptMetadata ───────────────────────────────────────────────

// envelope is the JSON shape of each encrypted_metadata_list element:
// {"a": "<agent_id>", "c": "<base64_ciphertext>"}.
type envelope struct {
	AgentID string `json:"a"`
	Cipher  string `json:"c"`
}

func (s *VaultGRPC) DecryptMetadata(ctx context.Context, req *pb.DecryptMetadataRequest) (*pb.DecryptMetadataResponse, error) {
	start := time.Now()
	user := s.v.tokens.GetUsername(req.GetToken())
	if user == "" {
		user = "unknown"
	}
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "decrypt_metadata", user, nil, resultCount, statusStr, errDetail, time.Since(start))
	}()

	username, role, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return &pb.DecryptMetadataResponse{Error: msg}, status.Error(st, msg)
	}
	user = username
	if err := role.CheckScope("decrypt_metadata"); err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.DecryptMetadataResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}
	if s.v.cfg.Tokens.TeamSecret == "" {
		statusStr = "error"
		msg := "VAULT_TEAM_SECRET not configured"
		errDetail = &msg
		return &pb.DecryptMetadataResponse{Error: msg}, status.Error(codes.Internal, msg)
	}

	out := make([]string, 0, len(req.GetEncryptedMetadataList()))
	for _, blobStr := range req.GetEncryptedMetadataList() {
		var env envelope
		if err := json.Unmarshal([]byte(blobStr), &env); err != nil {
			statusStr = "error"
			msg := fmt.Sprintf("Metadata decryption failed: %s", err.Error())
			errDetail = &msg
			return &pb.DecryptMetadataResponse{Error: msg}, status.Error(codes.InvalidArgument, msg)
		}
		dek, err := crypto.DeriveAgentKey(s.v.cfg.Tokens.TeamSecret, env.AgentID)
		if err != nil {
			statusStr = "error"
			msg := fmt.Sprintf("Metadata decryption failed: %s", err.Error())
			errDetail = &msg
			return &pb.DecryptMetadataResponse{Error: msg}, status.Error(codes.Internal, msg)
		}
		pt, err := crypto.DecryptMetadata(env.Cipher, dek)
		if err != nil {
			statusStr = "error"
			msg := fmt.Sprintf("Metadata decryption failed: %s", err.Error())
			errDetail = &msg
			return &pb.DecryptMetadataResponse{Error: msg}, status.Error(codes.Internal, msg)
		}
		out = append(out, string(pt))
	}
	resultCount = len(out)
	return &pb.DecryptMetadataResponse{DecryptedMetadata: out}, nil
}

// ── error mapping & audit helpers ────────────────────────────────

// mapTokenError maps tokens.ErrXxx → (gRPC code, user-facing message).
// Mirrors vault_grpc_server.py error branches.
func mapTokenError(err error) (codes.Code, string) {
	var nf tokens.ErrTokenNotFound
	if errors.As(err, &nf) {
		return codes.Unauthenticated, err.Error()
	}
	var exp tokens.ErrTokenExpired
	if errors.As(err, &exp) {
		return codes.Unauthenticated, err.Error()
	}
	var rl tokens.ErrRateLimit
	if errors.As(err, &rl) {
		return codes.ResourceExhausted, err.Error()
	}
	var sc tokens.ErrScope
	if errors.As(err, &sc) {
		return codes.PermissionDenied, err.Error()
	}
	var tk tokens.ErrTopKExceeded
	if errors.As(err, &tk) {
		return codes.InvalidArgument, err.Error()
	}
	return codes.Unauthenticated, err.Error()
}

// errStatus tags an error for the audit log: token/scope errors are
// "denied", everything else is "error".
func errStatus(err error) (string, *string) {
	msg := err.Error()
	switch {
	case errors.As(err, new(tokens.ErrTokenNotFound)),
		errors.As(err, new(tokens.ErrTokenExpired)),
		errors.As(err, new(tokens.ErrRateLimit)),
		errors.As(err, new(tokens.ErrScope)),
		errors.As(err, new(tokens.ErrTopKExceeded)):
		return "denied", &msg
	}
	return "error", &msg
}

func (s *VaultGRPC) emit(ctx context.Context, method, user string, topK *int32, resultCount int, statusStr string, errDetail *string, duration time.Duration) {
	if s.v.audit == nil || !s.v.audit.Enabled() {
		return
	}
	p, _ := peer.FromContext(ctx)
	s.v.audit.Log(AuditEntry{
		Timestamp:   nowUTCISO(),
		UserID:      user,
		Method:      method,
		TopK:        topK,
		ResultCount: resultCount,
		Status:      statusStr,
		SourceIP:    ExtractSourceIP(p),
		LatencyMs:   float64(duration.Microseconds()) / 1000.0,
		Error:       errDetail,
	})
}
