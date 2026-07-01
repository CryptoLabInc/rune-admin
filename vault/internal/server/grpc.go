package server

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

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

	bundle := map[string]any{
		"key_id":   s.v.bundleParams.KeyID,
		"agent_id": crypto.AgentIDFromToken(req.GetToken()),
		"dim":      s.v.cfg.Keys.EmbeddingDim,
	}
	if s.v.cfg.Keys.IndexName != "" {
		bundle["index_name"] = s.v.cfg.Keys.IndexName
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

	sealed, err := s.sealMeta(req.GetToken(), req.GetMetadata())
	if err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		return &pb.InsertResponse{Error: msg}, status.Error(codes.Internal, msg)
	}

	id, err := s.v.engine.Insert(ctx, req.GetVector(), sealed)
	if err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		return &pb.InsertResponse{Error: msg}, status.Error(codes.Internal, msg)
	}
	resultCount = 1
	return &pb.InsertResponse{Id: id}, nil
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

// sealMeta AES-seals plaintext metadata under the caller's per-agent DEK and
// wraps it in an {a,c} envelope for storage. Empty metadata seals to "".
func (s *VaultGRPC) sealMeta(token, meta string) (string, error) {
	if meta == "" {
		return "", nil
	}
	if s.v.cfg.Tokens.TeamSecret == "" {
		return "", errors.New("VAULT_TEAM_SECRET not configured")
	}
	agentID := crypto.AgentIDFromToken(token)
	dek, err := crypto.DeriveAgentKey(s.v.cfg.Tokens.TeamSecret, agentID)
	if err != nil {
		return "", err
	}
	c, err := crypto.EncryptMetadata([]byte(meta), dek)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(envelope{AgentID: agentID, Cipher: c})
	if err != nil {
		return "", err
	}
	return string(b), nil
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
