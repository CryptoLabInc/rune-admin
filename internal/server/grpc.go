package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/CryptoLabInc/rune-console/internal/crypto"
	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
	pb "github.com/CryptoLabInc/rune-console/pkg/consolepb"
)

// MaxMessageSize bounds gRPC frames.
const MaxMessageSize = 256 * 1024 * 1024

// publicOnlyScopeSentinel is the recall scope sent for a valid token that has no
// group memberships. It is a non-UUID string that can never equal a real group
// id (plan §6-D5), so runespace excludes every tagged item and returns only
// empty-tag (public) records. This closes the fail-OPEN gap where an empty scope
// would be read as "filtering off" and leak the whole org for decryption (Search).
const publicOnlyScopeSentinel = "__rune_no_access__"

// memberDirectory is the narrow member-registry surface the dataplane needs:
// one lookup by the token's user key (email) that feeds both the
// member-status gate and the judge-key resolution (email → member UUID), plus
// the reverse id lookup GetPermissions uses to display member-roles entries
// as emails. *members.Store satisfies it; the local interface keeps the hook
// optional and injectable for tests.
type memberDirectory interface {
	LookupByEmail(email string) (id, status string, ok bool)
	Get(id string) (*members.Member, error)
}

// inviteRedemption is the narrow invite-store surface the pre-auth redemption
// RPCs need: pre-validate an invite code (Lookup, read-only) and consume it
// exactly once (Unwrap). *invites.Store satisfies it; the local interface
// keeps the hook optional and injectable for tests, mirroring memberDirectory.
type inviteRedemption interface {
	Lookup(handle, creationPath string) (*invites.ClearBundle, error)
	Unwrap(handle string) (token, memberID string, err error)
}

// memberActivator is the single member-registry hook Unwrap needs: advance the
// redeemed member invited→active. *members.Store satisfies it.
type memberActivator interface {
	Activate(id string) error
}

// Console is the runtime container shared by all RPC handlers and the admin UDS
// server. It owns the token store, the runespace engine (all FHE keys + the
// sole runespace client), and the audit logger. Construct via NewConsole.
type Console struct {
	cfg    *Config
	tokens *tokens.Store
	groups *groups.Store
	audit  *AuditLogger

	// mu guards engine + tagStats: the runespace engine may be connected
	// lazily (after boot, once the access-token flow has provisioned a
	// runespace) while RPC handlers are already serving, so every read of the
	// engine goes through getEngine/engineReady and every write through
	// ConnectEngine under this lock.
	mu     sync.RWMutex
	engine consoleEngine

	// tagStats backs the group-delete sole-tag guard (plan §6-D7).
	// nil until M2b wires the runespace GetTagStats call — the guard is
	// fail-closed, so deletion of ever-captured-into groups stays refused.
	tagStats groups.TagStatsProvider

	// memberDir backs the dataplane member-status gate (member disable H1)
	// and the per-request email → member-UUID judge-key resolution. nil = no
	// gate and no resolution: tests and deployments without the member
	// subsystem keep today's behavior. The daemon wires it via
	// SetMemberDirectory.
	memberDir memberDirectory

	// inviteRedeem + memberAct back the pre-auth invite redemption RPCs
	// (LookupWrap/Unwrap — design-decisions §8.3/§8.4, model P: rune-mcp
	// redeems the code itself). nil = redemption surface off (Unimplemented),
	// so tests and deployments without the member subsystem are unaffected.
	// The daemon wires both via SetInviteRedemption.
	inviteRedeem inviteRedemption
	memberAct    memberActivator

	bundleParams crypto.KeysParams
}

// NewConsole wires all subsystems together. Caller is responsible for Close.
func NewConsole(cfg *Config, tokenStore *tokens.Store, groupStore *groups.Store, engine *crypto.Engine, audit *AuditLogger) *Console {
	v := &Console{
		cfg:    cfg,
		tokens: tokenStore,
		groups: groupStore,
		audit:  audit,
		bundleParams: crypto.KeysParams{
			Root:  cfg.Keys.Path,
			KeyID: defaultKeyID(cfg),
			Dim:   cfg.Keys.EmbeddingDim,
		},
	}
	// Guard the assignment so a nil *crypto.Engine stays a nil interface: a
	// typed-nil stored in the consoleEngine field would defeat the `engine == nil`
	// checks in Insert/Search (Go typed-nil-in-interface trap).
	if engine != nil {
		v.engine = engine
	}
	return v
}

func defaultKeyID(_ *Config) string { return "rune-console-key" }

// getEngine returns the connected runespace engine, or (nil, false) when the
// data plane is not connected yet. Handlers must go through this — the engine
// can be connected/closed concurrently with serving.
func (v *Console) getEngine() (consoleEngine, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.engine, v.engine != nil
}

// engineReady reports whether the runespace engine is connected. The
// interceptor uses it to gate the whole ConsoleService.
func (v *Console) engineReady() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.engine != nil
}

// EngineReady is the exported view of engineReady, used by the console BFF to
// surface data-plane connection status (e.g. in GET /console/session).
func (v *Console) EngineReady() bool { return v.engineReady() }

// DisconnectEngine detaches and closes the runespace engine, returning the
// console to the "runespace not configured" state. The data-plane layer calls
// it after the runespace is deleted so no stale connection or key set lingers.
func (v *Console) DisconnectEngine() { v.ConnectEngine(nil) }

// ConnectEngine attaches (or replaces) the runespace engine and wires the
// delete-guard tag stats to it. A replaced engine is closed. This is the seam
// the data-plane layer calls once the access token is available and the
// runespace has been dialed; a nil engine leaves the console reporting
// "runespace not configured".
func (v *Console) ConnectEngine(engine consoleEngine) {
	v.mu.Lock()
	old := v.engine
	v.engine = engine
	if engine != nil {
		v.tagStats = engineTagStats{eng: engine}
	}
	v.mu.Unlock()
	if old != nil && old != engine {
		_ = old.Close()
	}
}

// ConnectRunespace opens the full FHE key set, dials the runespace engine at
// addr with the data-plane access token, and attaches it (replacing any prior
// engine). It is the data-plane layer's entry point: the credential flow calls
// it once an access JWT has been minted (and again to re-dial before the JWT
// expires).
func (v *Console) ConnectRunespace(ctx context.Context, addr, token string, insecure bool) error {
	eng, err := crypto.OpenEngine(ctx, crypto.EngineParams{
		Keys:     v.bundleParams,
		Endpoint: addr,
		Token:    token,
		Insecure: insecure,
	})
	if err != nil {
		return err
	}
	v.ConnectEngine(eng)
	return nil
}

// Tokens exposes the token store for the admin UDS server.
func (v *Console) Tokens() *tokens.Store { return v.tokens }

// Groups exposes the group RBAC store (tree + memberships + judge).
func (v *Console) Groups() *groups.Store { return v.groups }

// SetTagStats injects the runespace-backed tag statistics provider used
// by the group-delete guard. M2b calls this with the engine adapter.
func (v *Console) SetTagStats(p groups.TagStatsProvider) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.tagStats = p
}

// SetMemberDirectory injects the member registry into the dataplane
// member-status gate and judge-key resolver. The daemon calls this with its
// *members.Store; left nil, no gate runs and the judge stays keyed by the
// token user (there is no member registry to consult).
func (v *Console) SetMemberDirectory(d memberDirectory) { v.memberDir = d }

// SetInviteRedemption injects the invite wrap store and the member-activation
// hook backing the pre-auth redemption RPCs. The daemon calls this with its
// *invites.Store and *members.Store; left nil, LookupWrap/Unwrap answer
// Unimplemented.
func (v *Console) SetInviteRedemption(inv inviteRedemption, act memberActivator) {
	v.inviteRedeem = inv
	v.memberAct = act
}

// resolveMemberAccess is the dataplane member-status gate fused with the
// groups-judge key resolution — ONE registry hit per token-validated request.
//
// Gate half (member disable H1): a token whose user resolves to a DISABLED
// member-registry row is denied. Disabling a member already revokes their
// token on the admin surface; this gate is defense in depth — it also cuts
// tokens that bypassed that path (e.g. issued out-of-band via the CLI for a
// disabled member). Callers wrap the returned message in
// codes.PermissionDenied.
//
// Resolution half: memberships are keyed by the immutable member UUID while
// tokens keep the email (identity as authenticated), so the judge key is the
// member id when a registry row exists. A user with no registry row (the
// owner, demo, CLI service tokens) passes the gate — the registry governs
// members only — and keeps key=user: on this branch such a key can hold no
// memberships (the member-UUID person-key validator refuses email keys at
// load and Grant), so recall falls to the public-only sentinel and capture
// fails with ErrNoWriteGroup. Fail-closed by construction, no special case.
//
// Keyspace guard: a token User is normally an email, but nothing constrains it
// at mint time — an admin may issue a token with any User string. If a token
// carries a member's UUID as its User, LookupByEmail misses (the registry is
// email-keyed) and the raw string would fall through as the judge key; because
// memberships ARE UUID-keyed, that raw UUID would resolve to the victim
// member's full scope while skipping the disabled gate (hasRow=false). Refuse a
// UUID-shaped identity that has no registry row outright: a non-member token has
// no legitimate reason to be named by a member id.
func (v *Console) resolveMemberAccess(user string) (key string, hasRow bool, err error) {
	if v.memberDir == nil {
		return user, false, nil
	}
	id, st, ok := v.memberDir.LookupByEmail(user)
	if !ok {
		if members.ValidateID(user) == nil {
			return "", false, fmt.Errorf("permission denied: token identity must not be a member id")
		}
		return user, false, nil
	}
	if st == members.StatusDisabled {
		return "", true, fmt.Errorf("permission denied: member '%s' is disabled", user)
	}
	return id, true, nil
}

// UseEngineTagStats wires the delete guard to the console's runespace engine
// (plan §6-D7). No-op if no engine is present, leaving the guard fail-closed.
func (v *Console) UseEngineTagStats() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.engine != nil {
		v.tagStats = engineTagStats{eng: v.engine}
	}
}

// engineTagStats adapts crypto.Engine.GetTagStats to groups.TagStatsProvider.
// The interface has no context, so it uses a background context — the call is
// console-internal and short (a manifest scan, plan §6-D7).
type engineTagStats struct{ eng consoleEngine }

func (a engineTagStats) GetTagStats(tags []string) (map[string]groups.TagStat, error) {
	stats, err := a.eng.GetTagStats(context.Background(), tags)
	if err != nil {
		return nil, err
	}
	out := make(map[string]groups.TagStat, len(stats))
	for _, s := range stats {
		out[s.Tag] = groups.TagStat{Total: int(s.Total), Sole: int(s.Sole)}
	}
	return out, nil
}

// TagStats returns the current provider (nil = fail-closed delete guard).
func (v *Console) TagStats() groups.TagStatsProvider {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.tagStats
}

// PurgeGroupTag best-effort sweeps a just-deleted group's tag off the items
// that still carry it next to other tags (crypto.Engine.PurgeTag, the
// cleanup companion of the sole-tag guard). It runs AFTER the group is gone:
// a failure here leaves only a dead UUID on multi-tag items — inert cruft
// that can never match a scope again — so the outcome is audited and
// reported in the returned status string, never propagated as a delete
// failure.
func (v *Console) PurgeGroupTag(ctx context.Context, tag, actor string) string {
	status, detail := "success", ""
	eng, ok := v.getEngine()
	if !ok {
		status, detail = "skipped", "tag cleanup skipped: no runespace engine attached"
	} else if res, err := eng.PurgeTag(ctx, tag); errors.Is(err, crypto.ErrPurgeTagUnsupported) {
		status, detail = "skipped", "tag cleanup skipped: runespace bulk tag removal not yet available (dead tag left as inert cruft)"
	} else if err != nil {
		status, detail = "failed", fmt.Sprintf("tag cleanup failed: %v", err)
	} else if res.SkippedSole > 0 {
		// A capture raced the delete and landed sole-tag items after the
		// guard passed; those records are now unreachable. Surface it —
		// hiding it is how data quietly disappears.
		status, detail = "partial", fmt.Sprintf("tag cleanup: removed from %d items, but %d sole-tag items were skipped and are now unreachable", res.Purged, res.SkippedSole)
	} else {
		detail = fmt.Sprintf("tag cleanup: removed from %d items", res.Purged)
	}
	v.auditTagCleanup(actor, status, detail, tag)
	return detail
}

// RemoveGroupTag strips a group's tag off every record that carries it — the
// console team-delete "purge" memory action. Unlike PurgeGroupTag (best-effort
// post-delete sweep of a dead UUID), this runs as part of the delete
// transaction and its failure MUST propagate so the caller can roll back
// (the doc mandates all-or-nothing memory handling). Returns the number of
// records the tag was removed from. Fails if no engine is attached.
func (v *Console) RemoveGroupTag(ctx context.Context, tag, actor string) (uint64, error) {
	eng, ok := v.getEngine()
	if !ok {
		v.auditTagCleanup(actor, "failed", "no runespace engine attached", tag)
		return 0, fmt.Errorf("no runespace engine attached")
	}
	n, err := eng.RemoveTag(ctx, tag)
	if err != nil {
		v.auditTagCleanup(actor, "failed", fmt.Sprintf("purge tag failed: %v", err), tag)
		return 0, err
	}
	v.auditTagCleanup(actor, "success", fmt.Sprintf("purge: removed tag from %d records", n), tag)
	return n, nil
}

// ReassignGroupTag moves every record tagged `from` to `to` — the console
// team-delete "transfer" memory action (reassign a deleted team's memory to a
// target team). Runs inside the delete transaction; failure propagates for
// rollback. Returns the number of records moved. Fails if no engine is attached.
func (v *Console) ReassignGroupTag(ctx context.Context, from, to, actor string) (uint64, error) {
	eng, ok := v.getEngine()
	if !ok {
		v.auditTagCleanup(actor, "failed", "no runespace engine attached", from)
		return 0, fmt.Errorf("no runespace engine attached")
	}
	n, err := eng.RetagAll(ctx, from, to)
	if err != nil {
		v.auditTagCleanup(actor, "failed", fmt.Sprintf("transfer tag %s→%s failed: %v", from, to, err), from)
		return 0, err
	}
	v.auditTagCleanup(actor, "success", fmt.Sprintf("transfer: moved %d records %s→%s", n, from, to), from)
	return n, nil
}

// auditTagCleanup records the post-delete tag sweep outcome as its own audit
// event so a skipped or failed cleanup stays visible after the fact. target
// is the swept tag — the deleted group's immutable id.
func (v *Console) auditTagCleanup(actor, status, detail, target string) {
	if v.audit == nil || !v.audit.Enabled() {
		return
	}
	entry := AuditEntry{
		Timestamp: nowUTCISO(),
		UserID:    localAdminActor(actor),
		Method:    "admin.group.tag_cleanup",
		Status:    status,
		SourceIP:  "admin-uds",
		Target:    target,
	}
	if status != "success" {
		entry.Error = &detail
	}
	v.audit.Log(entry)
}

// Config exposes the resolved config.
func (v *Console) Config() *Config { return v.cfg }

// Close releases the runespace engine (gRPC conn + cgo key handles).
func (v *Console) Close() error {
	if eng, ok := v.getEngine(); ok {
		_ = eng.Close()
	}
	return nil
}

// ConsoleGRPC is the gRPC service wrapper. Exposed for grpc.RegisterService.
type ConsoleGRPC struct {
	pb.UnimplementedConsoleServiceServer
	v *Console
}

func NewConsoleGRPC(v *Console) *ConsoleGRPC { return &ConsoleGRPC{v: v} }

// envelope is the sealed-metadata shape stored verbatim in runespace:
// {"a": "<agent_id>", "c": "<base64 AES-CTR ciphertext>"}. The agent_id lets
// any console request derive the right per-agent DEK, so team memory captured by
// one agent can be recalled (and metadata-decrypted) by another.
type envelope struct {
	AgentID string `json:"a"`
	Cipher  string `json:"c"`
}

// ── GetAgentManifest (config only — no keys ever leave the console) ──

func (s *ConsoleGRPC) GetAgentManifest(ctx context.Context, req *pb.GetAgentManifestRequest) (*pb.GetAgentManifestResponse, error) {
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
	if _, _, err := s.v.resolveMemberAccess(username); err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.GetAgentManifestResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}
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

// buildBundle assembles the per-token agent manifest returned by
// GetAgentManifest: the PUBLIC EncKey pair (RMP + MM EncKey.json envelopes) and
// the caller's derived agent_dek so rune-mcp can encrypt + seal locally, plus
// the config and the cheap centroid-set version pointer. SecKey never leaves.
func (v *Console) buildBundle(ctx context.Context, token string) (map[string]any, error) {
	rmpEnc, err := crypto.ReadRMPEncKey(v.bundleParams)
	if err != nil {
		return nil, err
	}
	mmEnc, err := crypto.ReadMMEncKey(v.bundleParams)
	if err != nil {
		return nil, err
	}
	agentID := crypto.AgentIDFromToken(token)
	dek, err := crypto.DeriveAgentKey(v.cfg.Tokens.TeamSecret, agentID)
	if err != nil {
		return nil, err
	}
	bundle := map[string]any{
		"key_id":      v.bundleParams.KeyID,
		"agent_id":    agentID,
		"dim":         v.cfg.Keys.EmbeddingDim,
		"rmp_enc_key": rmpEnc, // RMP (flat) EncKey envelope, verbatim JSON
		"mm_enc_key":  mmEnc,  // MM (clustered) EncKey envelope, verbatim JSON
		"agent_dek":   base64.StdEncoding.EncodeToString(dek),
	}
	// Cheap centroid-set version pointer: rune-mcp skips the heavy GetCentroids
	// fetch when its cache already matches. Best-effort — empty ("none loaded
	// yet") when the engine is not connected or has no centroid set.
	if eng, ok := v.getEngine(); ok {
		if cs, cerr := eng.Centroids(ctx); cerr == nil {
			bundle["centroid_set_version"] = cs.Version
		}
	}
	return bundle, nil
}

// ── GetCACert (no-TLS bootstrap: CA distribution) ─────────────────

// GetCACert serves the console's CA certificate (installer-issued ca.pem) so a
// new client can pin+trust it over the initial no-TLS connection, then
// reconnect over TLS. Pre-auth, engine-independent (interceptor exempts it):
// it is a public certificate carrying no secret.
func (s *ConsoleGRPC) GetCACert(ctx context.Context, _ *pb.GetCACertRequest) (*pb.GetCACertResponse, error) {
	start := time.Now()
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "get_ca_cert", "bootstrap", nil, resultCount, statusStr, errDetail, time.Since(start))
	}()

	pem, pin, err := caPEMAndPin(s.v.cfg)
	if err != nil {
		msg := err.Error()
		statusStr, errDetail = "error", &msg
		return &pb.GetCACertResponse{Error: msg}, status.Error(codes.FailedPrecondition, msg)
	}
	resultCount = 1
	return &pb.GetCACertResponse{CaPem: pem, Sha256: pin}, nil
}

// ── Insert (capture write) ────────────────────────────────────────

func (s *ConsoleGRPC) Insert(ctx context.Context, req *pb.InsertRequest) (*pb.InsertResponse, error) {
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

	// Validate still enforces identity + rate limit; the token role no longer
	// gates capture — the group RBAC judge does (plan §6-D3 single judge).
	username, _, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return &pb.InsertResponse{Error: msg}, status.Error(st, msg)
	}
	user = username
	key, _, err := s.v.resolveMemberAccess(username)
	if err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.InsertResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}

	// Capture write gate + tag selection in one judge call (plan §5, §6-D6):
	// tags are the caller's DIRECT write groups. Inherited descendants are never
	// tagged, so a superior's memory stays out of a subordinate group's recall
	// scope (§0 top priority). The judge is keyed by the resolved person key
	// (member UUID when registered). The pre-encrypted path carries no per-item
	// share-group selection, so the caller tags with all direct write groups.
	tags, err := s.v.groups.CaptureTagSet(key, nil)
	if err != nil {
		st := mapGroupsError(err)
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.InsertResponse{Error: err.Error()}, status.Error(st, err.Error())
	}

	eng, ok := s.v.getEngine()
	if !ok {
		statusStr = "error"
		msg := "runespace not configured"
		errDetail = &msg
		return &pb.InsertResponse{Error: msg}, status.Error(codes.FailedPrecondition, msg)
	}

	// The item is already FHE-encrypted and the metadata already sealed by
	// rune-mcp (which holds the PUBLIC EncKey + agent_dek); the console forwards
	// the ciphertext to runespace verbatim and never sees the plaintext.
	if err := eng.InsertPreEncrypted(ctx, crypto.PreEncryptedItem{
		ID:                 req.GetId(),
		RMPBlob:            req.GetRmpItem(),
		MMBlob:             req.GetMmItem(),
		ClusterID:          req.GetClusterId(),
		CentroidSetVersion: req.GetCentroidSetVersion(),
		SealedMetadata:     req.GetMetadata(),
	}, tags...); err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		return &pb.InsertResponse{Error: msg}, status.Error(codes.Internal, msg)
	}
	resultCount = 1
	// The id is caller-generated (the idempotency key); echo it back.
	return &pb.InsertResponse{Id: req.GetId()}, nil
}

// ── GetCentroids (IVF centroid relay) ─────────────────────────────

// centroidBatchSize bounds how many centroid vectors ride in one stream frame.
const centroidBatchSize = 256

// GetCentroids streams the runespace IVF centroid set to the caller: one Header
// frame (version/dim/preset/nlist), then id-ordered Batch frames. rune-mcp
// caches it and assigns each vector's cluster locally before encrypting.
func (s *ConsoleGRPC) GetCentroids(req *pb.GetCentroidsRequest, stream pb.ConsoleService_GetCentroidsServer) error {
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

	username, _, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return status.Error(st, msg)
	}
	user = username

	eng, ok := s.v.getEngine()
	if !ok {
		statusStr = "error"
		msg := "runespace not configured"
		errDetail = &msg
		return status.Error(codes.FailedPrecondition, msg)
	}

	cs, err := eng.Centroids(ctx)
	if err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		return status.Error(codes.Internal, msg)
	}

	if err := stream.Send(&pb.CentroidChunk{Payload: &pb.CentroidChunk_Header{Header: &pb.CentroidSetHeader{
		Version: cs.Version,
		Dim:     int32(cs.Dim),
		Preset:  cs.Preset,
		Nlist:   int32(len(cs.Vectors)),
	}}}); err != nil {
		statusStr = "error"
		msg := err.Error()
		errDetail = &msg
		return err
	}

	for i := 0; i < len(cs.Vectors); i += centroidBatchSize {
		end := i + centroidBatchSize
		if end > len(cs.Vectors) {
			end = len(cs.Vectors)
		}
		batch := make([]*pb.Centroid, 0, end-i)
		for j, v := range cs.Vectors[i:end] {
			batch = append(batch, &pb.Centroid{Id: uint32(i + j), Vec: v})
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

// ── Search (recall + novelty) ─────────────────────────────────────

func (s *ConsoleGRPC) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
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

	// Validate still enforces identity + rate limit; recall itself is open to
	// any valid token (read is the lowest role). What the caller SEES is bounded
	// by their recall scope, computed below (plan §6-D3 single judge).
	username, _, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return &pb.SearchResponse{Error: msg}, status.Error(st, msg)
	}
	user = username
	key, _, err := s.v.resolveMemberAccess(username)
	if err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.SearchResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}

	// top_k cap migrated to the judge (plan §5: read=10, write and above=50).
	if limit := s.v.groups.TopKLimit(key); int(topK) > limit {
		statusStr = "denied"
		msg := fmt.Sprintf("top_k %d exceeds limit %d for your role", topK, limit)
		errDetail = &msg
		return &pb.SearchResponse{Error: msg}, status.Error(codes.InvalidArgument, msg)
	}
	eng, ok := s.v.getEngine()
	if !ok {
		statusStr = "error"
		msg := "runespace not configured"
		errDetail = &msg
		return &pb.SearchResponse{Error: msg}, status.Error(codes.FailedPrecondition, msg)
	}

	// Recall scope = the caller's groups ∪ their recursive descendants,
	// recomputed per request so a revoke takes effect immediately (plan §5).
	// Keyed by the resolved person key (member UUID when registered).
	scope := s.v.groups.RecallScope(key)
	// fail-OPEN fix (plan §0 sustain): an empty scope (a valid token with no
	// memberships) is treated by runespace as "filtering off" — it would return
	// every record for the console to decrypt (org-wide leak). Substitute a
	// sentinel tag that matches no real group, so only empty-tag (public) items
	// pass the filter: default-deny for private memories. The sentinel is
	// non-UUID, so it can never collide with a real group id (plan §6-D5); this
	// closes the gap on the console surface with no runespace API change.
	if len(scope) == 0 {
		scope = []string{publicOnlyScopeSentinel}
	}
	hits, err := eng.Search(ctx, req.GetVector(), int(topK), scope...)
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

// ── GetPermissions (auth query — requirements 4, 8, 12) ───────────

func (s *ConsoleGRPC) GetPermissions(ctx context.Context, req *pb.GetPermissionsRequest) (*pb.GetPermissionsResponse, error) {
	start := time.Now()
	user := s.v.tokens.GetUsername(req.GetToken())
	if user == "" {
		user = "unknown"
	}
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "get_permissions", user, nil, resultCount, statusStr, errDetail, time.Since(start))
	}()

	username, _, err := s.v.tokens.Validate(req.GetToken())
	if err != nil {
		st, msg := mapTokenError(err)
		statusStr, errDetail = errStatus(err)
		return &pb.GetPermissionsResponse{Error: msg}, status.Error(st, msg)
	}
	user = username
	key, hasRow, err := s.v.resolveMemberAccess(username)
	if err != nil {
		statusStr = "denied"
		ed := err.Error()
		errDetail = &ed
		return &pb.GetPermissionsResponse{Error: err.Error()}, status.Error(codes.PermissionDenied, err.Error())
	}

	// The tree/memberships come from the judge keyed by the resolved person
	// key (member UUID when registered); Me below stays the EMAIL — the human
	// identity from the token.
	view, err := s.v.groups.Permissions(key, req.GetRootGroup())
	if err != nil {
		// An unknown root_group is NOT an error — the judge folds it into an
		// empty tree, identical to an unreachable group, so the response can
		// never oracle group existence. Anything surfacing here is an internal
		// tree-invariant failure, so no NotFound-style mapping either.
		statusStr = "error"
		ed := err.Error()
		errDetail = &ed
		return &pb.GetPermissionsResponse{Error: err.Error()}, status.Error(codes.Internal, err.Error())
	}

	resp := &pb.GetPermissionsResponse{Me: user}
	for _, m := range view.Memberships {
		resp.Memberships = append(resp.Memberships, &pb.MembershipEntry{
			GroupId: m.GroupID, GroupName: m.GroupName, Role: string(m.Role),
		})
	}
	for _, n := range view.Tree {
		resp.Tree = append(resp.Tree, &pb.TreeNode{
			GroupId: n.GroupID, Name: n.Name, ParentId: n.ParentID,
			Depth: int32(n.Depth), EffectiveRole: string(n.EffectiveRole),
		})
	}

	// member_roles is the org-wide listing — organization admin only (plan
	// §6-D8, D11): a non-admin request is denied with a reason, never a silent
	// empty list. Admin power is judged at request time as (token email is the
	// org admin — derived from the first-login console owner) AND (that email
	// has a registered member row): an unregistered admin email gets no
	// listing. The member-row conjunct only applies when a registry is wired
	// (always, on this branch).
	if req.GetIncludeMemberRoles() {
		if !s.v.groups.IsOrgAdmin(user) {
			statusStr = "denied"
			msg := fmt.Sprintf("permission denied: include_member_roles requires the organization admin (Owner); '%s' is not", user)
			errDetail = &msg
			return &pb.GetPermissionsResponse{Error: msg}, status.Error(codes.PermissionDenied, msg)
		}
		if s.v.memberDir != nil && !hasRow {
			statusStr = "denied"
			msg := fmt.Sprintf("permission denied: include_member_roles requires the organization admin to be a registered member; '%s' has no member row", user)
			errDetail = &msg
			return &pb.GetPermissionsResponse{Error: msg}, status.Error(codes.PermissionDenied, msg)
		}
		// Membership keys are member UUIDs — map each back to the member
		// email for display; a key with no member row is shown as-is.
		for _, mr := range s.v.groups.MemberRoles() {
			display := mr.User
			if s.v.memberDir != nil {
				if m, err := s.v.memberDir.Get(mr.User); err == nil {
					display = m.Email
				}
			}
			resp.MemberRoles = append(resp.MemberRoles, &pb.MemberRole{
				User: display, GroupId: mr.GroupID, GroupName: mr.GroupName, Role: string(mr.Role),
			})
		}
	}

	resultCount = len(resp.Tree)
	return resp, nil
}

// mapGroupsError maps a groups judge/store error to a gRPC status code
// (plan D11: refusals carry a code + reason, never a silent empty result).
func mapGroupsError(err error) codes.Code {
	switch {
	case errors.As(err, new(groups.ErrGroupNotFound)):
		return codes.NotFound
	case errors.As(err, new(groups.ErrNotAdmin)),
		errors.As(err, new(groups.ErrNoWriteGroup)),
		errors.As(err, new(groups.ErrNotDirectMember)),
		errors.As(err, new(groups.ErrInsufficientRole)):
		return codes.PermissionDenied
	case errors.As(err, new(groups.ErrHasChildren)),
		errors.As(err, new(groups.ErrHasMembers)),
		errors.As(err, new(groups.ErrSoleTagRecords)),
		errors.As(err, new(groups.ErrTagStatsUnavailable)):
		return codes.FailedPrecondition
	default:
		return codes.Internal
	}
}

// sealMeta AES-seals plaintext metadata under the caller's per-agent DEK and
// wraps it in an {a,c} envelope for storage. Empty metadata seals to "".
func (s *ConsoleGRPC) sealMeta(token, meta string) (string, error) {
	if meta == "" {
		return "", nil
	}
	if s.v.cfg.Tokens.TeamSecret == "" {
		return "", errors.New("team secret not configured")
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
func (s *ConsoleGRPC) openMeta(stored string) string {
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

// ── LookupWrap / Unwrap (invite redemption — pre-auth) ────────────
//
// The only two RPCs callable WITHOUT a token: the invite code (handle) is the
// credential (design-decisions §8.3/§8.4, model P — rune-mcp redeems the code
// itself, so the released token only ever travels console → client over TLS).

// LookupWrap pre-validates an invite code and returns who/what it is for.
// Read-only: it never consumes the code and never returns the token.
func (s *ConsoleGRPC) LookupWrap(ctx context.Context, req *pb.LookupWrapRequest) (*pb.LookupWrapResponse, error) {
	start := time.Now()
	user := "unknown"
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "lookup_wrap", user, nil, resultCount, statusStr, errDetail, time.Since(start))
	}()

	if s.v.inviteRedeem == nil {
		msg := "invite redemption is not enabled on this console"
		statusStr = "error"
		errDetail = &msg
		return &pb.LookupWrapResponse{Error: msg}, status.Error(codes.Unimplemented, msg)
	}
	b, err := s.v.inviteRedeem.Lookup(req.GetHandle(), inviteCreationPath)
	if err != nil {
		code, msg := mapInviteError(err)
		statusStr, errDetail = inviteErrStatus(code, msg)
		return &pb.LookupWrapResponse{Error: msg}, status.Error(code, msg)
	}
	user = b.Email
	resultCount = 1
	return &pb.LookupWrapResponse{
		Email:        b.Email,
		Role:         b.Role,
		ExpiresAt:    b.ExpiresAt,
		CreationPath: b.CreationPath,
	}, nil
}

// Unwrap exchanges a pending invite code for its sealed token, exactly once,
// and activates the member in the same call. Order of operations:
//
//  1. Lookup re-runs the pending/TTL/creation-path gates without consuming.
//  2. Member-status pre-check: a disabled member's invite is refused BEFORE
//     the one-time code is burned, so the code stays redeemable after a
//     restore. (The sealed token of a disabled member no longer authenticates
//     anyway — disable revoked it — but burning the invite would force a
//     re-issue for nothing.)
//  3. Store Unwrap: the consumed state is durably on disk before the token is
//     released (invites.Store.Unwrap, persist-before-return).
//  4. Activate (invited→active). A failure here returns Internal WITHOUT the
//     token: the invite is already burned, but "lost invite, re-issue" beats
//     a token handed out for a member the registry refused to activate.
func (s *ConsoleGRPC) Unwrap(ctx context.Context, req *pb.UnwrapRequest) (*pb.UnwrapResponse, error) {
	start := time.Now()
	user := "unknown"
	resultCount := 0
	statusStr := "success"
	var errDetail *string
	defer func() {
		s.emit(ctx, "unwrap", user, nil, resultCount, statusStr, errDetail, time.Since(start))
	}()

	if s.v.inviteRedeem == nil || s.v.memberAct == nil {
		msg := "invite redemption is not enabled on this console"
		statusStr = "error"
		errDetail = &msg
		return &pb.UnwrapResponse{Error: msg}, status.Error(codes.Unimplemented, msg)
	}
	b, err := s.v.inviteRedeem.Lookup(req.GetHandle(), inviteCreationPath)
	if err != nil {
		code, msg := mapInviteError(err)
		statusStr, errDetail = inviteErrStatus(code, msg)
		return &pb.UnwrapResponse{Error: msg}, status.Error(code, msg)
	}
	user = b.Email
	if s.v.memberDir != nil {
		if _, st, ok := s.v.memberDir.LookupByEmail(b.Email); ok && st == members.StatusDisabled {
			msg := fmt.Sprintf("member '%s' is disabled; invite not consumed", b.Email)
			statusStr = "denied"
			errDetail = &msg
			return &pb.UnwrapResponse{Error: msg}, status.Error(codes.PermissionDenied, msg)
		}
	}
	token, memberID, err := s.v.inviteRedeem.Unwrap(req.GetHandle())
	if err != nil {
		code, msg := mapInviteError(err)
		statusStr, errDetail = inviteErrStatus(code, msg)
		return &pb.UnwrapResponse{Error: msg}, status.Error(code, msg)
	}
	if err := s.v.memberAct.Activate(memberID); err != nil {
		msg := fmt.Sprintf("invite consumed but member could not be activated: %v", err)
		statusStr = "error"
		errDetail = &msg
		return &pb.UnwrapResponse{Error: msg}, status.Error(codes.Internal, msg)
	}
	resultCount = 1
	return &pb.UnwrapResponse{Token: token, MemberId: memberID}, nil
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

// mapInviteError maps invites.ErrXxx → (gRPC code, user-facing message) for
// the pre-auth redemption RPCs. "Already used" is FailedPrecondition, not
// NotFound, on purpose: a consumed code presented by someone who never used
// it is the interception alarm (§8.3 — the one-time design cannot stop a
// stolen code being redeemed first, but it must make the theft visible). A
// creation-path mismatch maps to NotFound so this surface does not disclose
// wraps minted by any other path.
func mapInviteError(err error) (codes.Code, string) {
	switch {
	case errors.As(err, new(invites.ErrInviteConsumed)),
		errors.As(err, new(invites.ErrInviteExpired)):
		return codes.FailedPrecondition, err.Error()
	case errors.As(err, new(invites.ErrInviteCompromised)):
		return codes.PermissionDenied, err.Error()
	case errors.As(err, new(invites.ErrInviteNotFound)),
		errors.As(err, new(invites.ErrCreationPathMismatch)):
		return codes.NotFound, err.Error()
	}
	return codes.Internal, err.Error()
}

// inviteErrStatus tags a mapped invite error for the audit log: gate refusals
// are "denied", store/internal failures are "error".
func inviteErrStatus(code codes.Code, msg string) (string, *string) {
	if code == codes.Internal {
		return "error", &msg
	}
	return "denied", &msg
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

func (s *ConsoleGRPC) emit(ctx context.Context, method, user string, topK *int32, resultCount int, statusStr string, errDetail *string, duration time.Duration) {
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
