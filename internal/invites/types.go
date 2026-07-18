// Package invites owns the one-time invite wrap store: it seals a freshly
// minted access token behind an opaque single-use handle, hands the caller a
// secret-free clear bundle to deliver, and releases the token exactly once
// on Unwrap. Field names track design-decisions §8.3 Wrap, with the §8.3
// "User" split into MemberID + Email so the store links back to the member
// registry (internal/members) without re-deriving identity from an address.
//
// Storage mirrors internal/members: in-memory indexes behind a sync.RWMutex
// acting as a write-through cache over the unified store database (attached
// via LoadFromDB); reads never touch SQL, and every mutation commits its row
// inside the store lock before the maps change. The old fsync-per-write
// (persist-before-return) contract is carried by the database's
// synchronous(FULL) pragma: a COMMIT is durable on disk before the mutator
// returns, so Issue and Unwrap keep their durable-before-release guarantees.
// A store with no sink attached (NewStore alone) is pure in-memory — how the
// server unit tests use it.
package invites

// Invite is one wrapped token. Handle is the opaque single-use reference that
// travels to the invitee; TokenValue is the sealed plaintext, cleared to ""
// the instant it is consumed. LeaseID is the durable reference used to report
// a post-consumption compromise.
type Invite struct {
	Handle       string `yaml:"handle"`        // opaque single-use: hex(16 random bytes)
	MemberID     string `yaml:"member_id"`     // links to members.Member.ID
	Email        string `yaml:"email"`         // member email at issue time
	TokenValue   string `yaml:"token_value"`   // sealed plaintext; "" once consumed
	Role         string `yaml:"role"`          // token role bound to the invite
	LeaseID      string `yaml:"lease_id"`      // durable ref for ReportCompromise
	CreationPath string `yaml:"creation_path"` // wrap-path binding (§8.3)
	CreatedAt    string `yaml:"created_at"`    // canonical storedb.TimeFormat (RFC3339 UTC ms)
	ExpiresAt    string `yaml:"expires_at"`    // canonical storedb.TimeFormat (RFC3339 UTC ms)
	Status       string `yaml:"status"`        // pending|consumed|compromised|expired|revoked
}

// ClearBundle is what leaves the console — zero secrets. It is safe to put in
// an email body or an HTTP response verbatim; the TokenValue is never here.
type ClearBundle struct {
	Handle       string `json:"handle"`
	LeaseID      string `json:"lease_id"`
	CreationPath string `json:"creation_path"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	ExpiresAt    string `json:"expires_at"`
}

// Invite status enum. "expired" means the invite aged past its TTL;
// "revoked" means an administrator voided it while still pending (cancel,
// re-send, rollback). Both refuse redemption the same way — the distinct
// value exists so listings and the database can tell an admin cancellation
// from a code that simply timed out. Legacy rows written before "revoked"
// existed keep "expired" for both meanings.
const (
	StatusPending     = "pending"
	StatusConsumed    = "consumed"
	StatusCompromised = "compromised"
	StatusExpired     = "expired"
	StatusRevoked     = "revoked"
)

// clearBundle projects the secret-free view of an invite.
func (inv *Invite) clearBundle() *ClearBundle {
	return &ClearBundle{
		Handle:       inv.Handle,
		LeaseID:      inv.LeaseID,
		CreationPath: inv.CreationPath,
		Email:        inv.Email,
		Role:         inv.Role,
		ExpiresAt:    inv.ExpiresAt,
	}
}
