package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	runespace "github.com/CryptoLabInc/runespace-sdk"

	"github.com/CryptoLabInc/rune-admin/vault/internal/crypto"
	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
	pb "github.com/CryptoLabInc/rune-admin/vault/pkg/vaultpb"
)

// MaxMessageSize bounds gRPC frames.
const MaxMessageSize = 256 * 1024 * 1024

// Vault is the runtime container shared by all RPC handlers and the admin UDS
// server. It owns the token store, the runespace engine (all FHE keys + the
// sole runespace client), and the audit logger. Construct via NewVault.
type Vault struct {
	cfg    *Config
	tokens *tokens.Store
	engine *crypto.Engine
	audit  *AuditLogger

	bundleParams crypto.KeysParams
}

// NewVault wires all subsystems together. Caller is responsible for Close.
func NewVault(cfg *Config, tokenStore *tokens.Store, engine *crypto.Engine, audit *AuditLogger) *Vault {
	return &Vault{
		cfg:    cfg,
		tokens: tokenStore,
		engine: engine,
		audit:  audit,
		bundleParams: crypto.KeysParams{
			Root:  cfg.Keys.Path,
			KeyID: defaultKeyID(cfg),
			Dim:   cfg.Keys.EmbeddingDim,
		},
	}
}

func defaultKeyID(_ *Config) string { return "vault-key" }

// Tokens exposes the token store for the admin UDS server.
func (v *Vault) Tokens() *tokens.Store { return v.tokens }

// Config exposes the resolved config.
func (v *Vault) Config() *Config { return v.cfg }

// Close releases the runespace engine (gRPC conn + cgo key handles).
func (v *Vault) Close() error {
	if v.engine != nil {
		_ = v.engine.Close()
	}
	return nil
}

// VaultGRPC is the gRPC service wrapper. Exposed for grpc.RegisterService.
type VaultGRPC struct {
	pb.UnimplementedVaultServiceServer
	v *Vault
}

func NewVaultGRPC(v *Vault) *VaultGRPC { return &VaultGRPC{v: v} }

// envelope is the sealed-metadata shape stored verbatim in runespace:
// {"a": "<agent_id>", "c": "<base64 AES-CTR ciphertext>"}. The agent_id lets
// any vault request derive the right per-agent DEK, so team memory captured by
// one agent can be recalled (and metadata-decrypted) by another.
type envelope struct {
	AgentID string `json:"a"`
	Cipher  string `json:"c"`
}

// ── GetAgentManifest (config only — no keys ever leave the vault) ──

func (s *VaultGRPC) GetAgentManifest(ctx context.Context, req *pb.GetAgentManifestRequest) (*pb.GetAgentManifestResponse, error) {
	start := time.Now()
	user := s.v.tokens.GetUsername(req.GetToken())
	if user == "" {
		user = "unknown"
	}
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "get_agent_manifest", user, nil, resultCount, statusStr, errDetail, time.Since(start))
	}()

	username, role, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return &pb.GetAgentManifestResponse{Error: msg}, status.Error(st, msg)
	}
	user = username
	if err := role.CheckScope("get_public_key"); err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.GetAgentManifestResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}

	bundle, err := s.v.buildBundle(ctx, req.GetToken())
	if err != nil {
		statusStr = "error"
		ed := err.Error()
		errDetail = &ed
		return &pb.GetAgentManifestResponse{Error: err.Error()}, status.Error(codes.Internal, err.Error())
	}
	js, err := json.Marshal(bundle)
	if err != nil {
		statusStr = "error"
		ed := err.Error()
		errDetail = &ed
		return &pb.GetAgentManifestResponse{Error: err.Error()}, status.Error(codes.Internal, err.Error())
	}
	resultCount = 1
	return &pb.GetAgentManifestResponse{ManifestJson: string(js)}, nil
}

// buildBundle assembles the agent manifest. The PUBLIC EncKey pair and the
// caller's derived agent_dek leave the vault here — by design: capture-side
// encryption/sealing happens on the developer machine. SecKey/EvalKey/
// team_secret are never included.
func (v *Vault) buildBundle(ctx context.Context, token string) (map[string]any, error) {
	rmpJSON, mmKey, err := crypto.ReadEncKeys(v.bundleParams)
	if err != nil {
		return nil, err
	}
	agentID := crypto.AgentIDFromToken(token)
	dek, err := crypto.DeriveAgentKey(v.cfg.Tokens.TeamSecret, agentID)
	if err != nil {
		return nil, err
	}
	bundle := map[string]any{
		"EncKey.json": string(rmpJSON),
		"mm_enc_key":  base64.StdEncoding.EncodeToString(mmKey),
		"agent_id":    agentID,
		"agent_dek":   base64.StdEncoding.EncodeToString(dek),
		"key_id":      v.bundleParams.KeyID,
		"dim":         v.cfg.Keys.EmbeddingDim,
		// Capability flag: this vault expects client-encrypted inserts.
		"insert": "pre_encrypted",
	}
	if v.cfg.Keys.IndexName != "" {
		bundle["index_name"] = v.cfg.Keys.IndexName
	}
	// Current centroid set version so the client can judge cache freshness
	// before pulling the (large) relay stream. Best-effort: an unreachable
	// engine leaves it empty and the client retries via GetCentroids.
	if v.engine != nil {
		if cs, err := v.engine.Centroids(ctx); err == nil {
			bundle["centroid_set_version"] = cs.Version
		}
	}
	return bundle, nil
}

// ── Insert (capture write) ────────────────────────────────────────

func (s *VaultGRPC) Insert(ctx context.Context, req *pb.InsertRequest) (*pb.InsertResponse, error) {
	start := time.Now()
	user := s.v.tokens.GetUsername(req.GetToken())
	if user == "" {
		user = "unknown"
	}
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "insert", user, nil, resultCount, statusStr, errDetail, time.Since(start))
	}()

	username, role, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return &pb.InsertResponse{Error: msg}, status.Error(st, msg)
	}
	user = username
	if err := role.CheckScope("decrypt_scores"); err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.InsertResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}
	if s.v.engine == nil {
		statusStr = "error"
		msg := "runespace engine not available"
		errDetail = &msg
		return &pb.InsertResponse{Error: msg}, status.Error(codes.Internal, msg)
	}

	it := runespace.PreEncryptedItem{
		ID:                 req.GetId(),
		RMPBlob:            req.GetRmpItem(),
		MMBlob:             req.GetMmItem(),
		ClusterID:          req.GetClusterId(),
		CentroidSetVersion: req.GetCentroidSetVersion(),
		Metadata:           req.GetMetadata(),
	}
	if err := s.v.engine.ForwardInsert(ctx, it); err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		// §9.2 C3: the engine replaced its centroid set. The engine cache was
		// already invalidated (ForwardInsert); relay a stable, typed signal so
		// the client resyncs centroids and retries once with the same id. The
		// WRONG_CENTROID_VERSION prefix is the wire contract rune-mcp matches.
		if errors.Is(err, runespace.ErrCentroidVersionMismatch) {
			wire := "WRONG_CENTROID_VERSION: centroid set was replaced; resync centroids and retry"
			return &pb.InsertResponse{Error: wire}, status.Error(codes.FailedPrecondition, wire)
		}
		return &pb.InsertResponse{Error: msg}, status.Error(codes.Internal, msg)
	}
	resultCount = 1
	return &pb.InsertResponse{Id: it.ID}, nil
}

// ── Search (recall + novelty) ─────────────────────────────────────

func (s *VaultGRPC) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
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
		s.emit(ctx, "search", user, &topK, resultCount, statusStr, errDetail, time.Since(start))
	}()

	username, role, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return &pb.SearchResponse{Error: msg}, status.Error(st, msg)
	}
	user = username
	if err := role.CheckScope("decrypt_scores"); err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.SearchResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}
	if int(topK) > role.TopK {
		te := tokens.ErrTopKExceeded{Requested: int(topK), MaxTopK: role.TopK, RoleName: role.Name}
		statusStr = "denied"
		msg := te.Error()
		errDetail = &msg
		return &pb.SearchResponse{Error: msg}, status.Error(codes.InvalidArgument, msg)
	}
	if s.v.engine == nil {
		statusStr = "error"
		msg := "runespace engine not available"
		errDetail = &msg
		return &pb.SearchResponse{Error: msg}, status.Error(codes.Internal, msg)
	}

	hits, err := s.v.engine.Search(ctx, req.GetVector(), int(topK))
	if err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		return &pb.SearchResponse{Error: msg}, status.Error(codes.Internal, msg)
	}
	out := make([]*pb.SearchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, &pb.SearchHit{Id: h.ID, Score: h.Score, Metadata: s.openMeta(h.Metadata)})
	}
	resultCount = len(out)
	return &pb.SearchResponse{Hits: out}, nil
}

// openMeta best-effort opens a sealed {a,c} envelope to plaintext JSON. On any
// failure it returns the stored string unchanged (plaintext/legacy tolerated).
func (s *VaultGRPC) openMeta(stored string) string {
	if stored == "" {
		return ""
	}
	var env envelope
	if err := json.Unmarshal([]byte(stored), &env); err != nil || env.Cipher == "" {
		return stored
	}
	dek, err := crypto.DeriveAgentKey(s.v.cfg.Tokens.TeamSecret, env.AgentID)
	if err != nil {
		return stored
	}
	pt, err := crypto.DecryptMetadata(env.Cipher, dek)
	if err != nil {
		return stored
	}
	// AES-CTR is unauthenticated: decrypting an envelope sealed under a
	// different team_secret "succeeds" and yields garbage bytes. Invalid
	// UTF-8 here would make the proto3 string unmarshalable and fail the
	// whole Search response at the gRPC layer, so fall back to the sealed
	// envelope instead. Proper fix is AEAD (AES-GCM) — see crypto/metadata.go.
	if !utf8.Valid(pt) {
		slog.Warn("vault: sealed metadata decrypted to non-UTF-8 bytes — returning envelope unopened (different team_secret or corrupted record)",
			"agent_id", env.AgentID)
		return stored
	}
	return string(pt)
}

// ── error mapping & audit helpers ────────────────────────────────

// mapTokenError maps tokens.ErrXxx → (gRPC code, user-facing message).
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

// errStatus tags an error for the audit log.
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

// ── GetCentroids (relay) ─────────────────────────────────────────

// centroidBatchSize bounds one relay frame: 64 centroids × dim 1024 × 4B ≈
// 256KB per message, comfortably under the gRPC frame cap.
const centroidBatchSize = 64

// GetCentroids relays the engine's IVF centroid set to rune-mcp so it can
// push the set down to runed. Same wire shape as runespace's GetCentroids:
// one header frame, then id-ordered batches.
func (s *VaultGRPC) GetCentroids(req *pb.GetCentroidsRequest, stream pb.VaultService_GetCentroidsServer) error {
	ctx := stream.Context()
	start := time.Now()
	user := s.v.tokens.GetUsername(req.GetToken())
	if user == "" {
		user = "unknown"
	}
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "get_centroids", user, nil, resultCount, statusStr, errDetail, time.Since(start))
	}()

	username, role, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return status.Error(st, msg)
	}
	user = username
	if err := role.CheckScope("get_public_key"); err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return status.Error(codes.PermissionDenied, err.Error())
	}
	if s.v.engine == nil {
		statusStr = "error"
		msg := "runespace engine not available"
		errDetail = &msg
		return status.Error(codes.Internal, msg)
	}

	cs, err := s.v.engine.Centroids(ctx)
	if err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		return status.Error(codes.Internal, msg)
	}
	if err := stream.Send(&pb.CentroidChunk{Payload: &pb.CentroidChunk_Header{Header: &pb.CentroidSetHeader{
		Version: cs.Version,
		Dim:     uint32(cs.Dim),
		Nlist:   uint32(len(cs.Vectors)),
		Preset:  cs.Preset,
	}}}); err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		return err
	}
	for lo := 0; lo < len(cs.Vectors); lo += centroidBatchSize {
		hi := min(lo+centroidBatchSize, len(cs.Vectors))
		batch := make([]*pb.Centroid, 0, hi-lo)
		for i := lo; i < hi; i++ {
			batch = append(batch, &pb.Centroid{Id: uint32(i), Vec: cs.Vectors[i]})
		}
		if err := stream.Send(&pb.CentroidChunk{Payload: &pb.CentroidChunk_Batch{Batch: &pb.CentroidBatch{Centroids: batch}}}); err != nil {
			statusStr = "error"
			msg := err.Error()
			errDetail = &msg
			return err
		}
	}
	resultCount = len(cs.Vectors)
	return nil
}
