// Package storedb owns the unified store database (runeconsole.db): the
// schema v1 DDL and the idempotent schema bootstrap. It is deliberately a
// LEAF package (imports no store packages) so every store package, and its
// own tests, can depend on the schema without an import cycle. The schema
// carries the branch-decision amendments (invite status 'revoked', relaxed
// groups.id check, persisted tokens.last_used).
package storedb

import (
	"database/sql"
	"fmt"
	"time"
)

// SchemaVersion is the schema version this package installs and EnsureSchema
// stamps into schema_migrations as the baseline.
const SchemaVersion = 1

// schemaBaselineDescription is the description stamped alongside
// SchemaVersion. It names the row for what it is: the version a fresh install
// STARTS at, not a migration that ran.
const schemaBaselineDescription = "initial schema baseline (not an applied migration)"

// SchemaV1 is the complete schema v1 DDL. Every statement is idempotent
// (IF NOT EXISTS) so EnsureSchema can run on every boot. All timestamps are
// TEXT in the canonical form TimeFormat (RFC3339 UTC, fixed three-digit
// milliseconds) unless noted; the fixed width is what keeps lexicographic
// order == time order (see TimeFormat).
const SchemaV1 = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version     INTEGER PRIMARY KEY,
  applied_at  TEXT NOT NULL,
  description TEXT NOT NULL
);
-- EnsureSchema stamps the one baseline row (SchemaVersion,
-- schemaBaselineDescription) here right after installing this DDL, so a fresh
-- install starts from a RECORDED version: the first real migration runner
-- reads a baseline instead of an empty ledger it would have to guess about.
-- Nothing else writes this table — there is no migration to apply yet.

-- members (internal/members/types.go). CreatedAt byte-exact round-trip is
-- test-pinned (store_test.go) => TEXT, never epoch. disabled_from is NULL
-- unless the member is disabled (the CHECK below); Go maps NULL to the
-- store's "" empty-value convention on load.
CREATE TABLE IF NOT EXISTS members (
  id                 TEXT PRIMARY KEY
                       CHECK (length(id) = 36 AND id = lower(id)),  -- full UUIDv4 shape stays in Go ValidateID
  email              TEXT NOT NULL UNIQUE,     -- immutable person key; UNIQUE replaces byEmail map + load-fatal dup check
  display_name       TEXT NOT NULL DEFAULT '',
  status             TEXT NOT NULL
                       CHECK (status IN ('registered','invited','active','disabled')),
  disabled_from      TEXT
                       CHECK (disabled_from IS NULL
                              OR (status = 'disabled'
                                  AND disabled_from IN ('registered','invited','active'))),
  created_at         TEXT NOT NULL,
  session_expired_at TEXT               -- NULL == today's ""; set by session deactivation only
);
-- List() contract: ORDER BY created_at, id (members/store.go List)
CREATE INDEX IF NOT EXISTS idx_members_list_order ON members (created_at, id);

-- Email is the token join key and immutable by design (Update has no email
-- parameter). Hard delete (Remove) stays legal — this guards UPDATE only.
CREATE TRIGGER IF NOT EXISTS members_email_immutable
BEFORE UPDATE OF email ON members
WHEN NEW.email <> OLD.email
BEGIN
  SELECT RAISE(ABORT, 'member email is immutable');
END;

-- invites (internal/invites/types.go). Rows are never deleted (issuance
-- history). NO FK on member_id — user delete revokes only PENDING invites
-- before members.Remove, so consumed/expired history legitimately outlives
-- its member. Soft reference, as today.
-- Status carries 'revoked' (branch decision): RevokePending writes 'revoked'
-- instead of overloading 'expired'.
CREATE TABLE IF NOT EXISTS invites (
  handle        TEXT PRIMARY KEY
                  CHECK (length(handle) = 32 AND handle NOT GLOB '*[^0-9a-f]*'),
  lease_id      TEXT NOT NULL UNIQUE
                  CHECK (length(lease_id) = 32 AND lease_id NOT GLOB '*[^0-9a-f]*'),
  member_id     TEXT NOT NULL,        -- soft ref -> members.id
  email         TEXT NOT NULL,        -- deliberate denormalized snapshot; never join away
  token_value   TEXT
                  CHECK (token_value IS NULL OR status = 'pending'),  -- schema form of the writeSnapshot scrub
  role          TEXT NOT NULL,        -- role-name snapshot; no FK — history survives role deletion
  creation_path TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  expires_at    TEXT,                 -- NULL = never expires; expiry stays lazy in Go against injected clock
  status        TEXT NOT NULL
                  CHECK (status IN ('pending','consumed','compromised','expired','revoked'))
);
CREATE INDEX IF NOT EXISTS idx_invites_member_created
  ON invites (member_id, created_at DESC);          -- ListByMember / LatestByMember
CREATE INDEX IF NOT EXISTS idx_invites_history_order
  ON invites (created_at DESC, handle ASC);         -- List(): CreatedAt desc, handle ASC tiebreak
-- The aged-pending sweep (status='pending' AND expires_at<=?) runs as the
-- FIRST statement of every invite write transaction, inside the store's
-- write mutex — and invites is the one table whose rows are never deleted,
-- so it is the only unbounded scan in the system. This partial index keeps
-- the sweep a near-empty range search regardless of history size (measured:
-- 5.2ms scan -> 0.007ms at 100k rows); it holds only pending rows, and the
-- sweep itself evicts entries as they flip to expired. EnsureSchema re-runs
-- the DDL each boot, so existing databases pick it up without a migration.
CREATE INDEX IF NOT EXISTS idx_invites_pending_expiry
  ON invites (expires_at) WHERE status = 'pending';

-- Forward-only lifecycle: consumed can never revert to pending. Replaces the
-- writeMu whole-snapshot monotonic-ordering argument with a DB guarantee.
-- pending may advance to consumed, expired, or revoked; consumed to
-- compromised; 'revoked' is terminal.
CREATE TRIGGER IF NOT EXISTS invites_status_forward_only
BEFORE UPDATE OF status ON invites
WHEN NOT (
     NEW.status = OLD.status
  OR (OLD.status = 'pending'  AND NEW.status IN ('consumed','expired','revoked'))
  OR (OLD.status = 'consumed' AND NEW.status = 'compromised')
)
BEGIN
  SELECT RAISE(ABORT, 'invalid invite status transition');
END;

-- roles + tokens (internal/tokens). scope = JSON TEXT array (only collection
-- field; linear-scan set semantics in Go — a child table adds review surface
-- for zero queries). rate_limit stays the 'N/Ws' API/file contract string.
-- admin/member are SEED ROWS inserted by tokens.LoadFromDB when absent, so
-- default-role presence is a boot invariant of the database itself;
-- read-time defaults (top_k 0->5, rate_limit ''->'30/60s') materialize at
-- write time.
CREATE TABLE IF NOT EXISTS roles (
  name       TEXT PRIMARY KEY,
  scope      TEXT NOT NULL DEFAULT '[]',
  top_k      INTEGER NOT NULL DEFAULT 5,
  rate_limit TEXT NOT NULL DEFAULT '30/60s'
);
-- Default-role undeletability and DeleteRole's referenced-by-token RESTRICT
-- stay in Go (now inside one transaction). No is_default column:
-- isDefaultRoleName is two constants; a column invites drift from the
-- code-level DefaultRoles().

-- NO FK on tokens.role: a dangling role must keep listing as '?' and real
-- data dirs may contain it. NO FK on tokens.user: email in console flows,
-- freeform for admin/demo tokens. Secrets stay PLAINTEXT this release
-- (hashing is a named follow-up) — the DB inherits the 0600 fail-closed
-- posture instead.
CREATE TABLE IF NOT EXISTS tokens (
  user      TEXT PRIMARY KEY,          -- one token per user (hard invariant, tokensByUser)
  token     TEXT NOT NULL UNIQUE,      -- plaintext 'evt_'+32hex; Validate's lookup key
  role      TEXT NOT NULL,             -- soft ref -> roles.name
  issued_at TEXT NOT NULL,             -- date-only 'YYYY-MM-DD' (NOT RFC3339 — day-granularity expiry is a contract)
  expires   TEXT,                      -- date-only; NULL = never; deliberately NO format CHECK (unparseable == never)
  last_used TEXT                       -- canonical TimeFormat (RFC3339 UTC ms); newly durable, written async + throttled, never inside Validate's lock
);

-- groups + memberships (internal/groups/types.go). parent_id keeps the ''
-- root sentinel (NOT NULL DEFAULT '') precisely so UNIQUE(parent_id,name)
-- enforces root-sibling uniqueness — NULL roots would silently kill it
-- (UNIQUE treats NULLs as distinct). Consequence: no self-FK;
-- unknown-parent/cycle/depth<=8 stay in Go at load and insert, exactly as
-- today. id values are opaque FHE record tags: stored and matched
-- byte-verbatim, never parsed by the schema.
-- id CHECK deliberately relaxed to non-empty (branch decision): the loader
-- accepts non-UUID group ids, so the schema rejects only the empty id; UUID
-- shape enforcement stays in Go where it exists today.
CREATE TABLE IF NOT EXISTS groups (
  id         TEXT PRIMARY KEY
               CHECK (id <> ''),
  name       TEXT NOT NULL CHECK (name <> '' AND instr(name, '/') = 0),
  parent_id  TEXT NOT NULL DEFAULT '',   -- '' = root sentinel
  created_at TEXT NOT NULL,
  UNIQUE (parent_id, name)               -- sibling-scoped uniqueness
);
CREATE INDEX IF NOT EXISTS idx_groups_parent ON groups (parent_id);

-- The ONE cross-table FK, where it eliminates a real bug class: the two-file
-- crash-inconsistency (dangling memberships) becomes unrepresentable.
-- RESTRICT so the admin DeleteGroup guard can refuse (ErrHasMembers); the
-- console DeleteGroupWithMembers path deletes memberships then the group
-- explicitly, in one transaction.
-- NO FK on user — the person key is pluggable (email default vs injected
-- member-UUID validator); the domain is enforced by the injected Go
-- validator, never the schema.
CREATE TABLE IF NOT EXISTS memberships (
  user       TEXT NOT NULL,
  group_id   TEXT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
  role       TEXT NOT NULL CHECK (role IN ('read','write','edit')),
  granted_by TEXT NOT NULL,
  granted_at TEXT NOT NULL,              -- == joinedAt in the console projection
  PRIMARY KEY (user, group_id)
);
CREATE INDEX IF NOT EXISTS idx_memberships_group ON memberships (group_id);

-- A role change is not a new join — granted_at records the original join
-- time. Grant = INSERT ... ON CONFLICT(user, group_id) DO UPDATE SET
-- role=excluded.role, granted_by=excluded.granted_by — never granted_at;
-- this trigger enforces it.
CREATE TRIGGER IF NOT EXISTS memberships_granted_at_immutable
BEFORE UPDATE OF granted_at ON memberships
WHEN NEW.granted_at <> OLD.granted_at
BEGIN
  SELECT RAISE(ABORT, 'granted_at records the original join time and is immutable');
END;

-- Removed inherited reads (groups.ReadExclusion): the subtraction of a
-- DERIVED grant, kept out of memberships on purpose — every judge path reads
-- that table's map as "the user's direct grants", so an exclusion living
-- there would read as a grant. An exclusion only ever REMOVES access, and it
-- is meaningless without its group, so it cascades with the group's deletion
-- (mirroring purgeExclusionsForGroupLocked in the store). NO FK on user —
-- same pluggable person-key rationale as memberships.
CREATE TABLE IF NOT EXISTS read_exclusions (
  user       TEXT NOT NULL,
  group_id   TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  removed_by TEXT NOT NULL,
  removed_at TEXT NOT NULL,
  PRIMARY KEY (user, group_id)
);
CREATE INDEX IF NOT EXISTS idx_read_exclusions_group ON read_exclusions (group_id);
`

// EnsureSchema installs schema v1 into database and records the SchemaVersion
// baseline in schema_migrations. Every DDL statement uses IF NOT EXISTS and
// the stamp is ON CONFLICT DO NOTHING, so calling it on an already-initialized
// database is a no-op: the baseline keeps the applied_at of the boot that
// first wrote it. The two steps need no shared transaction for the same
// reason — a crash between them leaves the next boot to finish the job.
func EnsureSchema(database *sql.DB) error {
	if _, err := database.Exec(SchemaV1); err != nil {
		return fmt.Errorf("storedb: ensure schema: %w", err)
	}
	// The baseline is what a future migration runner reads to learn which
	// version an install started at; without it a fresh database would hand
	// that runner an empty ledger, indistinguishable from "version unknown".
	if _, err := database.Exec(
		`INSERT INTO schema_migrations (version, applied_at, description) VALUES (?, ?, ?)
		 ON CONFLICT(version) DO NOTHING`,
		SchemaVersion, FormatTime(time.Now()), schemaBaselineDescription); err != nil {
		return fmt.Errorf("storedb: stamp schema baseline: %w", err)
	}
	return nil
}
