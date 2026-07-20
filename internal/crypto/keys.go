package crypto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	runespace "github.com/CryptoLabInc/runespace-sdk"
)

// KeysParams names the on-disk key bundle and FHE dimension.
type KeysParams struct {
	// Root is the parent directory containing <KeyID>/. Each tier lives in its
	// own subdirectory: rmp/ (flat/IP0) and mm/ (clustered/IP1). E.g.,
	// "/opt/runeconsole/rune-console-keys" with KeyID "rune-console-key" reads from
	// "/opt/runeconsole/rune-console-keys/rune-console-key/{rmp,mm}/{Enc,Sec,Eval}Key(.json|.bin)".
	Root  string
	KeyID string
	Dim   int
}

func (p KeysParams) keyDir() string { return filepath.Join(p.Root, p.KeyID) }

// ReadRMPEncKey reads the RMP (flat-tier) EncKey envelope (rmp/EncKey.json)
// verbatim — the PUBLIC encryption key rune-mcp uses for EncryptFlat. The
// manifest carries it as a string under "rmp_enc_key".
func ReadRMPEncKey(p KeysParams) (string, error) {
	b, err := os.ReadFile(filepath.Join(p.keyDir(), "rmp", "EncKey.json"))
	if err != nil {
		return "", fmt.Errorf("crypto: read rmp/EncKey.json: %w", err)
	}
	return string(b), nil
}

// ReadMMEncKey reads the MM (clustered-tier) EncKey envelope (mm/EncKey.json)
// verbatim — the PUBLIC key rune-mcp uses for EncryptClustered. The manifest
// carries it as a string under "mm_enc_key".
func ReadMMEncKey(p KeysParams) (string, error) {
	b, err := os.ReadFile(filepath.Join(p.keyDir(), "mm", "EncKey.json"))
	if err != nil {
		return "", fmt.Errorf("crypto: read mm/EncKey.json: %w", err)
	}
	return string(b), nil
}

// KeysExist reports whether the bundle is present under Root/KeyID.
func KeysExist(p KeysParams) bool {
	return runespace.KeysExist(
		runespace.WithKeyPath(p.keyDir()),
		runespace.WithKeyID(p.KeyID),
		runespace.WithKeyDim(p.Dim),
	)
}

// EnsureKeys generates a fresh key set (Enc/Eval/Sec, RMP+MM) if none exists.
// No-op if a bundle is already present (GenerateKeys never overwrites).
func EnsureKeys(p KeysParams) error {
	if KeysExist(p) {
		return nil
	}
	if err := os.MkdirAll(p.keyDir(), 0o700); err != nil {
		return fmt.Errorf("crypto: mkdir key dir: %w", err)
	}
	if err := runespace.GenerateKeys(
		runespace.WithKeyPath(p.keyDir()),
		runespace.WithKeyID(p.KeyID),
		runespace.WithKeyDim(p.Dim),
	); err != nil && !errors.Is(err, runespace.ErrKeysAlreadyExist) {
		return fmt.Errorf("crypto: generate keys: %w", err)
	}
	return nil
}

// EngineParams configures the console's runespace client.
type EngineParams struct {
	Keys     KeysParams
	Endpoint string // runespace gRPC address
	Token    string // optional runespace access token
	Insecure bool   // true for local plaintext dev
}

// Engine is the console's runespace client. It owns the full FHE key set
// (Enc+Eval+Sec) and the gRPC connection, and is the SOLE talker to the
// runespace engine: Insert encrypts locally then appends; Search sends the
// plaintext query, decrypts the score blobs, and returns ranked hits.
type Engine struct {
	client *runespace.Client
	keys   *runespace.Keys
}

// OpenEngine opens the full key set, dials runespace, and makes sure the eval
// key is registered (RegisterKeys on first run, UseKeys thereafter).
func OpenEngine(ctx context.Context, p EngineParams) (*Engine, error) {
	keys, err := runespace.OpenKeys(
		runespace.WithKeyPath(p.Keys.keyDir()),
		runespace.WithKeyID(p.Keys.KeyID),
		runespace.WithKeyDim(p.Keys.Dim),
	) // default parts: Enc (encrypt) + Sec (decrypt); eval streamed by RegisterKeys
	if err != nil {
		return nil, fmt.Errorf("crypto: open keys: %w", err)
	}

	opts := []runespace.ClientOption{}
	if p.Token != "" {
		opts = append(opts, runespace.WithAccessToken(p.Token))
	}
	if p.Insecure {
		opts = append(opts, runespace.WithInsecure())
	}
	client, err := runespace.Dial(p.Endpoint, opts...)
	if err != nil {
		_ = keys.Close()
		return nil, fmt.Errorf("crypto: dial runespace %s: %w", p.Endpoint, err)
	}

	// Register the eval key once; if the engine already serves this key set,
	// just bind locally (UseKeys) so restarts are idempotent.
	if info, ierr := client.Info(ctx); ierr == nil && len(info.RegisteredKeys) > 0 {
		client.UseKeys(keys)
	} else if err := client.RegisterKeys(ctx, keys); err != nil {
		_ = client.Close()
		_ = keys.Close()
		return nil, fmt.Errorf("crypto: register keys: %w", err)
	}

	return &Engine{client: client, keys: keys}, nil
}

// Dim returns the FHE slot dimension the key set was opened with.
func (e *Engine) Dim() int { return e.keys.Dim() }

// Insert encrypts vec (EncKey, RMP+MM) and appends it under a fresh opaque id,
// which it returns. sealedMeta is stored verbatim in the manifest — the caller
// is responsible for sealing it (agent_dek) beforehand. filterTags are the
// opaque group tags (immutable group IDs, plan §6-D5) attached to the item;
// runespace stores them without interpreting them.
func (e *Engine) Insert(ctx context.Context, vec []float32, sealedMeta string, filterTags ...string) (string, error) {
	var opts []runespace.InsertOption
	if len(filterTags) > 0 {
		opts = append(opts, runespace.WithFilterTags(filterTags...))
	}
	return e.client.Insert(ctx, vec, sealedMeta, opts...)
}

// PreEncryptedItem is a client-encrypted capture item the console forwards to
// runespace verbatim: rune-mcp (which holds the PUBLIC EncKey + agent_dek)
// produced the RMP/MM ciphertext, routed the IVF cluster, and sealed the
// metadata, so the console holds no key for this path and does no crypto.
type PreEncryptedItem struct {
	ID                 string
	RMPBlob            []byte // SDK Keys.EncryptFlat output
	MMBlob             []byte // SDK Keys.EncryptClustered output
	ClusterID          uint32 // hard single IVF assignment, 0..nlist-1
	CentroidSetVersion string // set version the assignment was routed against
	SealedMetadata     string // client-sealed {"a","c"} envelope, stored verbatim
}

// InsertPreEncrypted forwards a client-encrypted item to runespace verbatim
// (SDK InsertPreEncrypted, idempotent on item.ID). filterTags are the opaque
// group tags (immutable group IDs, plan §6-D5) the console resolved from the
// caller's token; runespace stores them without interpreting them.
func (e *Engine) InsertPreEncrypted(ctx context.Context, it PreEncryptedItem, filterTags ...string) error {
	var opts []runespace.InsertOption
	if len(filterTags) > 0 {
		opts = append(opts, runespace.WithFilterTags(filterTags...))
	}
	return e.client.InsertPreEncrypted(ctx, runespace.PreEncryptedItem{
		ID:                 it.ID,
		RMPBlob:            it.RMPBlob,
		MMBlob:             it.MMBlob,
		ClusterID:          it.ClusterID,
		CentroidSetVersion: it.CentroidSetVersion,
		Metadata:           it.SealedMetadata,
	}, opts...)
}

// CentroidSet is the runespace IVF centroid set the console relays to rune-mcp,
// which routes each vector's cluster locally before encrypting it.
type CentroidSet struct {
	Version string
	Dim     int
	Preset  string
	Vectors [][]float32
}

// Centroids fetches the runespace IVF centroid set for relay. Version is also
// surfaced (cheaply) in the agent manifest so rune-mcp fetches the full set
// only on a version mismatch.
func (e *Engine) Centroids(ctx context.Context) (*CentroidSet, error) {
	cs, err := e.client.Centroids(ctx)
	if err != nil {
		return nil, err
	}
	return &CentroidSet{Version: cs.Version, Dim: cs.Dim, Preset: cs.Preset, Vectors: cs.Vectors}, nil
}

// SearchHit is one decrypted, ranked result. Metadata is the stored envelope
// (still sealed); the caller opens it with the agent_dek.
type SearchHit struct {
	ID       string
	Score    float64
	Metadata string
}

// Search runs the blind search (plaintext query → decrypted scores → ranked
// hits with metadata resolved). filterScope is the caller's recall scope (the
// group tags they may see, plan §5); runespace folds out any item whose tags
// do not intersect it. An empty filterScope is REFUSED: the team-based-filter
// contract reads no scope as "filtering off" (org-wide visibility), so every
// caller must pass an explicit scope — a zero-membership caller passes the
// public-only sentinel (server.publicOnlyScopeSentinel), which matches no
// real group id and so admits only untagged (public) items.
func (e *Engine) Search(ctx context.Context, vec []float32, topK int, filterScope ...string) ([]SearchHit, error) {
	if len(filterScope) == 0 {
		return nil, errors.New("crypto: empty recall scope: refusing unfiltered search (callers must pass an explicit scope; zero-membership callers get the public-only sentinel)")
	}
	matches, err := e.client.Search(ctx, vec, topK, runespace.WithScope(filterScope...))
	if err != nil {
		return nil, err
	}
	out := make([]SearchHit, 0, len(matches))
	for _, m := range matches {
		out = append(out, SearchHit{ID: m.ID, Score: m.Score, Metadata: m.Metadata})
	}
	return out, nil
}

// TagStat is one per-tag summary from runespace (plan §6-D7): Total counts
// items carrying the tag; Sole counts items for which it is the only tag —
// the ones that would become invisible if the tag's group were deleted.
type TagStat struct {
	Tag   string
	Total int64
	Sole  int64
}

// ErrTagStatsUnsupported reports that the connected runespace/SDK line does
// not expose the GetTagStats RPC. The group-delete guard is fail-closed, so
// callers surface this as "deletion refused: cannot verify sole-tag records".
var ErrTagStatsUnsupported = errors.New("crypto: runespace GetTagStats RPC is not available in this SDK/server version; group deletion stays refused until it ships")

// GetTagStats asks runespace for per-tag item counts (manifest scan). It
// backs the group-delete sole-tag guard; the console is the sole caller
// (plan §6-D7, §12-5 network-isolation premise).
//
// The runespace main line does not (yet) expose this RPC — it existed only on
// the feat/team-filter-mechanism / feat/rbac-proto-sync pair — so this build
// returns ErrTagStatsUnsupported unconditionally and the guard refuses every
// delete of an ever-captured-into group. Swap the body back to
// e.client.GetTagStats(ctx, tags) once the RPC lands upstream.
func (e *Engine) GetTagStats(_ context.Context, _ []string) ([]TagStat, error) {
	return nil, ErrTagStatsUnsupported
}

// PurgeResult summarizes a PurgeTag run: Purged counts items the tag was
// removed from; SkippedSole counts items left untouched because the tag was
// their ONLY tag (stripping it would flip them to public — visible to every
// scope — so the server must never do that).
type PurgeResult struct {
	Purged      int64
	SkippedSole int64
}

// ErrPurgeTagUnsupported reports that the connected runespace/SDK line has no
// bulk tag-removal call yet. Callers treat it as a benign skip: a dead group
// UUID left on multi-tag items can never match any recall scope again (group
// IDs are never reissued), so the leftover tag is invisible cruft, not a leak.
var ErrPurgeTagUnsupported = errors.New("crypto: runespace bulk tag removal is not available in this SDK/server version; the deleted group's tag stays on its remaining multi-tag items as inert cruft")

// PurgeTag removes a deleted group's tag from every live item that carries it
// alongside other tags. It is the post-delete cleanup companion of the
// sole-tag guard (plan §6-D7): the guard refuses deletion while sole-tag
// records exist; PurgeTag then sweeps the dead UUID off the multi-tag items.
//
// Expected upstream contract (bulk tag removal by tag value, announced but
// not yet shipped):
//   - NEVER strips an item's last tag — an untagged item is public, so
//     sole-tag items are skipped and counted, never exposed;
//   - idempotent — re-purging an already-gone tag is a no-op;
//   - reports Purged / SkippedSole counts (SkippedSole > 0 means a capture
//     raced the delete; it must be surfaced, not hidden).
//
// The current SDK exposes only per-item UpdateTags(id, add, remove) and no
// way to enumerate items by tag, so a client-side sweep is impossible; this
// returns ErrPurgeTagUnsupported until the RPC ships, at which point the body
// becomes a one-line client call.
func (e *Engine) PurgeTag(_ context.Context, _ string) (PurgeResult, error) {
	return PurgeResult{}, ErrPurgeTagUnsupported
}

// RemoveTag strips a single tag from every live item that carries it and
// returns the number of items changed. It backs the console team-delete
// "purge" memory action (remove this team's tag — the memory itself is not
// destroyed, and records shared with other teams stay reachable there). Unlike
// the stubbed PurgeTag above, this is a direct, shipped SDK call
// (runespace RemoveTag RPC): the SDK/runespace side owns the sole-tag policy.
func (e *Engine) RemoveTag(ctx context.Context, tag string) (uint64, error) {
	return e.client.RemoveTag(ctx, tag)
}

// RetagAll reassigns every item tagged `from` to `to` and returns the number
// of items moved. It backs the console team-delete "transfer" memory action
// (reassign a deleted team's memory to another team). Direct SDK call
// (runespace RetagAll RPC).
func (e *Engine) RetagAll(ctx context.Context, from, to string) (uint64, error) {
	return e.client.RetagAll(ctx, from, to)
}

// Close releases the gRPC connection and the cgo key handles. Idempotent.
func (e *Engine) Close() error {
	var first error
	if e.client != nil {
		if err := e.client.Close(); err != nil {
			first = err
		}
		e.client = nil
	}
	if e.keys != nil {
		if err := e.keys.Close(); err != nil && first == nil {
			first = err
		}
		e.keys = nil
	}
	return first
}
