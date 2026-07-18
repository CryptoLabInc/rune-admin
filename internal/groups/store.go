// Package groups owns the group tree, the memberships that bind users
// to it, and the single RBAC judge (plan §6-D3: effective role / scope
// computation lives here and nowhere else).
//
// Storage is an in-memory structure behind a sync.RWMutex over an
// optional SQLite write-through sink (the unified store database):
// reads — including the judge hot paths — are pure map lookups with
// zero SQL, and every mutator commits its rows inside the write-lock
// critical section before the maps change. Groups and memberships form
// one consistency domain: an operation spanning both tables runs in a
// single transaction, with the memberships→groups foreign key as the
// schema-level backstop.
package groups

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

// MaxTreeDepth caps the group tree height (root = depth 1). The load-time
// cycle validation walks at most this many parent hops (plan §6-D2).
const MaxTreeDepth = 8

// Limits carries the judge's configurable knobs (plan §5: top_k caps
// migrate to the 4-role model — read inherits the old member cap,
// write and above inherit the old admin cap).
type Limits struct {
	TopKRead  int
	TopKWrite int
}

// DefaultLimits returns the plan §5 defaults.
func DefaultLimits() Limits { return Limits{TopKRead: 10, TopKWrite: 50} }

// Store is the in-memory group/membership state behind a sync.RWMutex over
// an optional SQLite write-through persistence sink. Reads — including the
// judge hot paths — are pure map lookups with zero SQL; every mutator
// commits its rows to the database before the maps change, so the cache can
// never get ahead of durable state. A store with no sink attached (NewStore
// alone) is a pure in-memory store — how unit tests and the one-time YAML
// importer use it.
type Store struct {
	mu          sync.RWMutex
	groups      map[string]*Group                 // keyed by group ID
	byName      map[string][]*Group               // name -> groups sharing it (unique only among siblings)
	children    map[string][]string               // parent ID -> child IDs
	memberships map[string]map[string]*Membership // user email -> group ID -> membership
	// excluded holds the removed inherited reads: user -> group ID -> exclusion.
	// Kept OUT of memberships on purpose — every judge path iterates that map as
	// "the user's direct grants", so an exclusion living there would read as a
	// grant. Only the inheritance-expanding paths consult this map.
	excluded  map[string]map[string]*ReadExclusion
	orgAdmins map[string]bool // org admin (Owner) emails — grant authority (plan §5, §6-D8)
	limits    Limits

	// validatePerson is the pluggable person-key contract; nil means the
	// default (validateUserEmail). Read without locking — see
	// SetPersonKeyValidator for the boot-time-only contract.
	validatePerson PersonKeyValidator

	// db is the optional write-through persistence sink (the unified store
	// database, attached by LoadFromDB). nil = pure in-memory store.
	db *sql.DB

	now func() time.Time
}

// NewStore returns an empty in-memory group store with default limits and
// the real UTC clock. Persistence is attached separately (LoadFromDB);
// without it every mutation stays in memory only.
func NewStore() *Store {
	return &Store{
		groups:      make(map[string]*Group),
		byName:      make(map[string][]*Group),
		children:    make(map[string][]string),
		memberships: make(map[string]map[string]*Membership),
		excluded:    make(map[string]map[string]*ReadExclusion),
		orgAdmins:   make(map[string]bool),
		limits:      DefaultLimits(),
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// PersonKeyValidator checks a person key (the membership / judge user
// key) before it enters the store. The store treats the key as opaque:
// core deployments keep the default email contract, member deployments
// inject a member-UUID validator instead.
type PersonKeyValidator func(key string) error

// SetPersonKeyValidator replaces the person-key contract; nil restores
// the default (validateUserEmail). It must be called at boot, before
// LoadFromDB and before any Grant/judge traffic — the validator is
// read without locking, so swapping it concurrently with use is unsafe.
func (s *Store) SetPersonKeyValidator(v PersonKeyValidator) {
	s.validatePerson = v
}

// validatePersonKey routes every person-key check (load, Grant,
// CanGrant) through the injected validator, defaulting to the email
// contract.
func (s *Store) validatePersonKey(key string) error {
	if s.validatePerson != nil {
		return s.validatePerson(key)
	}
	return validateUserEmail(key)
}

// SetOrgAdmins declares the organization admin(s) — the Owner identity
// that alone may grant/revoke (plan §5 grant rule, §6-D8). The plan
// mandates exactly one org admin: the console owner bound at first login,
// replayed here by the daemon's OwnerRegistrar hook at boot and on the
// claiming login (config-declared admins were removed). Each call
// replaces the whole set, so the replay is idempotent; the variadic form
// remains for tests. Emails are the person key (plan §0, D2). In this M1
// scope org admins are used for judgment only (CanGrant); they are not
// group memberships.
func (s *Store) SetOrgAdmins(emails ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orgAdmins = make(map[string]bool, len(emails))
	for _, e := range emails {
		if e != "" {
			s.orgAdmins[e] = true
		}
	}
}

// IsOrgAdmin reports whether user is the organization admin (Owner).
func (s *Store) IsOrgAdmin(user string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.orgAdmins[user]
}

// SetLimits overrides the judge knobs. Zero fields keep their defaults.
func (s *Store) SetLimits(l Limits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.TopKRead > 0 {
		s.limits.TopKRead = l.TopKRead
	}
	if l.TopKWrite > 0 {
		s.limits.TopKWrite = l.TopKWrite
	}
}

// LoadFromDB attaches database (the unified store database, opened with
// db.OpenStrict and carrying the storedb schema) as the write-through
// persistence sink and loads the in-memory indexes from its groups and
// memberships tables. Go-side structural validation runs here: an unknown
// parent reference or a cyclic/over-deep tree fails the load (the schema has
// no self-FK on parent_id, so tree shape is Go's job), and every membership
// row is routed through the active person-key validator — call it after
// SetPersonKeyValidator. Group ids are copied byte-verbatim: they are the
// opaque FHE record tags, and non-UUID ids are legal (the schema only
// requires non-empty). There is no fail-soft case: a membership row whose
// group is missing fails the load, because the memberships.group_id foreign
// key makes that state unrepresentable through this binary — reaching it
// means the database was edited externally, and a SQLite file is repairable
// in place.
func (s *Store) LoadFromDB(database *sql.DB) error {
	gRows, err := database.Query(`SELECT id, name, parent_id, created_at FROM groups`)
	if err != nil {
		return fmt.Errorf("groups: load from db: %w", err)
	}
	defer func() { _ = gRows.Close() }()
	var gs []Group
	for gRows.Next() {
		var g Group
		if err := gRows.Scan(&g.ID, &g.Name, &g.ParentID, &g.CreatedAt); err != nil {
			return fmt.Errorf("groups: load from db: %w", err)
		}
		gs = append(gs, g)
	}
	if err := gRows.Err(); err != nil {
		return fmt.Errorf("groups: load from db: %w", err)
	}

	mRows, err := database.Query(`SELECT user, group_id, role, granted_by, granted_at FROM memberships`)
	if err != nil {
		return fmt.Errorf("groups: load from db: %w", err)
	}
	defer func() { _ = mRows.Close() }()
	var ms []Membership
	for mRows.Next() {
		var m Membership
		if err := mRows.Scan(&m.User, &m.GroupID, &m.Role, &m.GrantedBy, &m.GrantedAt); err != nil {
			return fmt.Errorf("groups: load from db: %w", err)
		}
		ms = append(ms, m)
	}
	if err := mRows.Err(); err != nil {
		return fmt.Errorf("groups: load from db: %w", err)
	}

	dRows, err := database.Query(`SELECT user, group_id, removed_by, removed_at FROM read_exclusions`)
	if err != nil {
		return fmt.Errorf("groups: load from db: %w", err)
	}
	defer func() { _ = dRows.Close() }()
	var ds []ReadExclusion
	for dRows.Next() {
		var d ReadExclusion
		if err := dRows.Scan(&d.User, &d.GroupID, &d.RemovedBy, &d.RemovedAt); err != nil {
			return fmt.Errorf("groups: load from db: %w", err)
		}
		ds = append(ds, d)
	}
	if err := dRows.Err(); err != nil {
		return fmt.Errorf("groups: load from db: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups = make(map[string]*Group, len(gs))
	s.byName = make(map[string][]*Group)
	s.children = make(map[string][]string)
	s.memberships = make(map[string]map[string]*Membership)
	s.excluded = make(map[string]map[string]*ReadExclusion)
	for i := range gs {
		// The PRIMARY KEY makes duplicate ids and UNIQUE(parent_id, name)
		// makes sibling-name duplicates unrepresentable, so only the checks
		// the schema cannot express run here.
		cp := gs[i]
		s.groups[cp.ID] = &cp
		s.byName[cp.Name] = append(s.byName[cp.Name], &cp)
	}
	for id, g := range s.groups {
		if g.ParentID == "" {
			continue
		}
		if _, ok := s.groups[g.ParentID]; !ok {
			return fmt.Errorf("groups: load from db: group '%s' references unknown parent '%s'", id, g.ParentID)
		}
		s.children[g.ParentID] = append(s.children[g.ParentID], id)
	}
	// Cycle + depth validation: visited-set walk up every parent chain,
	// capped at MaxTreeDepth hops (plan §6-D2) — same as the YAML load.
	for id := range s.groups {
		if _, err := s.depthLocked(id); err != nil {
			return fmt.Errorf("groups: load from db: %w", err)
		}
	}
	for i := range ms {
		m := ms[i]
		if err := s.validatePersonKey(m.User); err != nil {
			return fmt.Errorf("groups: load from db: membership (user '%s', group '%s'): %w", m.User, m.GroupID, err)
		}
		if !m.Role.Valid() {
			return fmt.Errorf("groups: load from db: membership (user '%s', group '%s') has invalid role %q", m.User, m.GroupID, string(m.Role))
		}
		if _, ok := s.groups[m.GroupID]; !ok {
			return fmt.Errorf("groups: load from db: membership (user '%s', group '%s') references a missing group — unreachable through the console (foreign key); the database was modified externally, repair it before restarting", m.User, m.GroupID)
		}
		cp := m
		if s.memberships[m.User] == nil {
			s.memberships[m.User] = make(map[string]*Membership)
		}
		s.memberships[m.User][m.GroupID] = &cp
	}
	for i := range ds {
		d := ds[i]
		// Same fail-closed posture as memberships: an exclusion only ever
		// REMOVES access, but a row this store did not write means the
		// database was edited externally — validate, don't guess. A missing
		// group is unrepresentable (ON DELETE CASCADE), so only the person
		// key needs checking here.
		if err := s.validatePersonKey(d.User); err != nil {
			return fmt.Errorf("groups: load from db: read exclusion (user '%s', group '%s'): %w", d.User, d.GroupID, err)
		}
		cp := d
		if s.excluded[d.User] == nil {
			s.excluded[d.User] = make(map[string]*ReadExclusion)
		}
		s.excluded[d.User][d.GroupID] = &cp
	}
	s.db = database
	return nil
}

// depthLocked returns the absolute depth of the group (root = 1),
// walking the parent chain with a visited set and a MaxTreeDepth cap.
func (s *Store) depthLocked(id string) (int, error) {
	visited := make(map[string]bool, MaxTreeDepth)
	depth := 0
	cur := id
	for cur != "" {
		if visited[cur] || depth >= MaxTreeDepth {
			return 0, ErrCycle{GroupID: id}
		}
		visited[cur] = true
		depth++
		g, ok := s.groups[cur]
		if !ok {
			return 0, ErrGroupNotFound{Ref: cur}
		}
		cur = g.ParentID
	}
	return depth, nil
}

// resolveLocked maps a group reference (immutable ID first, then display
// name) to the group. Names are unique only among siblings, so a bare name
// can match more than one group across different parents — that is reported
// as ErrAmbiguousName rather than silently picking one.
func (s *Store) resolveLocked(ref string) (*Group, error) {
	if g, ok := s.groups[ref]; ok {
		return g, nil
	}
	switch named := s.byName[ref]; len(named) {
	case 0:
		return nil, ErrGroupNotFound{Ref: ref}
	case 1:
		return named[0], nil
	default:
		return nil, ErrAmbiguousName{Name: ref, Count: len(named)}
	}
}

// ResolveGroup returns the group for an ID-or-name reference.
func (s *Store) ResolveGroup(ref string) (Group, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, err := s.resolveLocked(ref)
	if err != nil {
		return Group{}, err
	}
	return *g, nil
}

// validateUserEmail is the DEFAULT person-key contract: the membership /
// token key is an email (plan §0, D2 — "사람 이름표 = 이메일"). Member
// deployments inject a member-UUID validator via SetPersonKeyValidator
// instead — the store treats the key as opaque either way. The check
// is deliberately lightweight (non-empty, a single interior '@', no
// spaces) — full RFC validation is not the console's job.
func validateUserEmail(user string) error {
	if strings.TrimSpace(user) == "" {
		return fmt.Errorf("user must not be empty")
	}
	at := strings.IndexByte(user, '@')
	if at <= 0 || at != strings.LastIndexByte(user, '@') || at == len(user)-1 {
		return fmt.Errorf("user %q must be an email address (the membership key is an email)", user)
	}
	if strings.ContainsAny(user, " \t\n") {
		return fmt.Errorf("user %q must not contain whitespace", user)
	}
	return nil
}

// groupNameRE mirrors the console client's team-name rule (frontend
// TEAM_NAME_PATTERN): digits, Latin letters, Hangul syllables, and '-' '_'
// only — no spaces, dots, slashes, or other punctuation. The backend is the
// source of truth, so a direct API call is held to the same rule the SPA
// enforces client-side. Names are stored verbatim, so the pattern is matched
// against the raw name (leading/trailing whitespace is therefore rejected).
var groupNameRE = regexp.MustCompile(`^[0-9A-Za-z가-힣_-]+$`)

// maxGroupNameLen caps a team name at the client Input's maxLength (50 runes).
const maxGroupNameLen = 50

func validateGroupName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("group name must not be empty")
	}
	if n := len([]rune(name)); n > maxGroupNameLen {
		return fmt.Errorf("group name is %d characters; the limit is %d", n, maxGroupNameLen)
	}
	if !groupNameRE.MatchString(name) {
		return fmt.Errorf("group name %q may use only letters, digits, Hangul, '-' and '_'", name)
	}
	return nil
}

// nameTakenBySiblingLocked reports whether a group other than exclID, sharing
// parentID, already uses name. Root groups (parentID == "") are siblings of
// one another. Caller holds s.mu.
func (s *Store) nameTakenBySiblingLocked(parentID, name, exclID string) bool {
	if parentID == "" {
		for id, g := range s.groups {
			if id != exclID && g.ParentID == "" && g.Name == name {
				return true
			}
		}
		return false
	}
	for _, cid := range s.children[parentID] {
		if cid == exclID {
			continue
		}
		if g, ok := s.groups[cid]; ok && g.Name == name {
			return true
		}
	}
	return false
}

// removeFromByNameLocked drops the group with id from name's bucket, deleting
// the bucket key when it empties. Caller holds s.mu.
func (s *Store) removeFromByNameLocked(name, id string) {
	bucket := s.byName[name]
	for i, g := range bucket {
		if g.ID == id {
			bucket = append(bucket[:i], bucket[i+1:]...)
			break
		}
	}
	if len(bucket) == 0 {
		delete(s.byName, name)
	} else {
		s.byName[name] = bucket
	}
}

// CreateGroup adds a group under parentRef ("" = root). The ID is a
// freshly generated immutable UUID — this is the opaque tag value.
func (s *Store) CreateGroup(name, parentRef string) (Group, error) {
	if err := validateGroupName(name); err != nil {
		return Group{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Resolve the parent first: the name-uniqueness scope is the sibling set
	// under this parent, so parentID must be known before the duplicate check.
	parentID := ""
	if parentRef != "" {
		parent, err := s.resolveLocked(parentRef)
		if err != nil {
			return Group{}, err
		}
		parentID = parent.ID
		pd, err := s.depthLocked(parentID)
		if err != nil {
			return Group{}, err
		}
		if pd+1 > MaxTreeDepth {
			return Group{}, fmt.Errorf("cannot create group '%s': tree depth would exceed %d", name, MaxTreeDepth)
		}
	}
	if s.nameTakenBySiblingLocked(parentID, name, "") {
		return Group{}, ErrDuplicateName{Name: name}
	}
	id, err := newGroupID()
	if err != nil {
		return Group{}, err
	}
	g := &Group{
		ID:        id,
		Name:      name,
		ParentID:  parentID,
		CreatedAt: storedb.FormatTime(s.now()),
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO groups (id, name, parent_id, created_at) VALUES (?, ?, ?, ?)`,
			g.ID, g.Name, g.ParentID, g.CreatedAt)
		return err
	}); err != nil {
		return Group{}, err
	}
	s.groups[id] = g
	s.byName[name] = append(s.byName[name], g)
	if parentID != "" {
		s.children[parentID] = append(s.children[parentID], id)
	}
	return *g, nil
}

// RenameGroup changes the display name only. The tag (ID) is immutable,
// so stored records are untouched by design (plan §6-D5).
func (s *Store) RenameGroup(ref, newName string) (Group, error) {
	if err := validateGroupName(newName); err != nil {
		return Group{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.resolveLocked(ref)
	if err != nil {
		return Group{}, err
	}
	if newName != g.Name && s.nameTakenBySiblingLocked(g.ParentID, newName, g.ID) {
		return Group{}, ErrDuplicateName{Name: newName}
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE groups SET name = ? WHERE id = ?`, newName, g.ID)
		if err != nil {
			return err
		}
		return expectOneRow(res, "group "+g.ID)
	}); err != nil {
		return Group{}, err
	}
	s.removeFromByNameLocked(g.Name, g.ID)
	g.Name = newName
	s.byName[newName] = append(s.byName[newName], g)
	return *g, nil
}

// DeleteGroup removes a group after the triple guard (plan §6-D7):
// no child groups, no memberships, no sole-tag records. stats may be
// nil — that refuses deletion fail-closed (guard c cannot be verified).
// The no-members guard (ErrHasMembers) is a Go-level refusal; the
// memberships.group_id ON DELETE RESTRICT foreign key backs it at the
// database layer, it does not replace it.
func (s *Store) DeleteGroup(ref string, stats TagStatsProvider) (Group, error) {
	s.mu.RLock()
	g0, err := s.resolveLocked(ref)
	if err != nil {
		s.mu.RUnlock()
		return Group{}, err
	}
	id := g0.ID
	// Local guards first — don't make a remote call for a group that is
	// already blocked by children or members.
	err = s.deleteCheckMembershipTreeLocked(id)
	s.mu.RUnlock()
	if err != nil {
		return Group{}, err
	}

	// Guard (c) is a remote call — never run it under the store lock (and
	// never inside the write transaction below).
	if err := soleTagGuard(id, stats); err != nil {
		return Group{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[id]
	if !ok {
		return Group{}, ErrGroupNotFound{Ref: ref}
	}
	// Re-check the local guards under the write lock (a grant/create may
	// have landed between the read and the write section).
	if err := s.deleteCheckMembershipTreeLocked(id); err != nil {
		return Group{}, err
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, id)
		if err != nil {
			return err
		}
		return expectOneRow(res, "group "+id)
	}); err != nil {
		return Group{}, err
	}
	s.removeFromByNameLocked(g.Name, g.ID)
	delete(s.groups, id)
	delete(s.children, id)
	s.purgeExclusionsForGroupLocked(id)
	if g.ParentID != "" {
		sibs := s.children[g.ParentID]
		for i, c := range sibs {
			if c == id {
				s.children[g.ParentID] = append(sibs[:i], sibs[i+1:]...)
				break
			}
		}
	}
	return *g, nil
}

// purgeExclusionsForGroupLocked drops every user's read exclusion on a group that is
// being deleted. The guards let a group with exclusions (they are not memberships)
// through deletion, so without this the rows would outlive the group they
// refer to, leaving a meaningless exclusion behind.
// Caller must hold s.mu for writing.
func (s *Store) purgeExclusionsForGroupLocked(id string) {
	for user, byGroup := range s.excluded {
		if _, ok := byGroup[id]; !ok {
			continue
		}
		delete(byGroup, id)
		if len(byGroup) == 0 {
			delete(s.excluded, user)
		}
	}
}

// Grant records (user, group, role). An existing membership for the same
// (user, group) is replaced — role changes are re-grants. The judge's
// grant rule (CanGrant) is NOT enforced here: the local admin surface is
// an operator surface with full power + audit (plan §6-D8, layer 1);
// grantedBy carries the audit identity ("local-admin:<actor>").
func (s *Store) Grant(user, groupRef string, role Role, grantedBy string) (Membership, error) {
	if err := s.validatePersonKey(user); err != nil {
		return Membership{}, err
	}
	if !role.Valid() {
		return Membership{}, fmt.Errorf("invalid group role %q (expected read|write|edit)", string(role))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.resolveLocked(groupRef)
	if err != nil {
		return Membership{}, err
	}
	m := &Membership{
		User:      user,
		GroupID:   g.ID,
		Role:      role,
		GrantedBy: grantedBy,
		GrantedAt: storedb.FormatTime(s.now()),
	}
	// A re-grant is a role change, not a new join: keep the original grant
	// time so the console's joinedAt keeps showing when the member actually
	// joined the team (grantedBy still records who last changed the role).
	// The SQL upsert mirrors this — its DO UPDATE never touches granted_at,
	// and the memberships_granted_at_immutable trigger is the second layer.
	if prev, ok := s.memberships[user][g.ID]; ok {
		m.GrantedAt = prev.GrantedAt
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memberships (user, group_id, role, granted_by, granted_at)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(user, group_id) DO UPDATE SET role = excluded.role, granted_by = excluded.granted_by`,
			m.User, m.GroupID, string(m.Role), m.GrantedBy, m.GrantedAt); err != nil {
			return err
		}
		// An explicit grant overrides any removed inherited read on this
		// group — the admin just said yes, so a lingering exclusion (which
		// would still hide the group the moment the direct row is revoked)
		// must not survive. Same transaction as the upsert: the pair can
		// never half-apply.
		_, err := tx.ExecContext(ctx,
			`DELETE FROM read_exclusions WHERE user = ? AND group_id = ?`, m.User, m.GroupID)
		return err
	}); err != nil {
		return Membership{}, err
	}
	if s.memberships[user] == nil {
		s.memberships[user] = make(map[string]*Membership)
	}
	s.memberships[user][g.ID] = m
	delete(s.excluded[user], g.ID)
	if len(s.excluded[user]) == 0 {
		delete(s.excluded, user)
	}
	return *m, nil
}

// RevokeDirectGrant removes ONLY the (user, group) direct membership row and
// deliberately LEAVES any inherited read intact: a user who also belongs to an
// ancestor still reads the group afterwards. This is the rollback/undo
// primitive — it restores the exact state before a Grant, which is why the
// compensating paths (a failed add/invite) use it. To cut a user's ACCESS to a
// group (drop the direct grant AND any surviving inherited read), use Revoke
// instead. Returns false when no such direct membership exists. Effect is
// immediate: scope is recomputed per request (plan §5).
func (s *Store) RevokeDirectGrant(user, groupRef string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.resolveLocked(groupRef)
	if err != nil {
		return false, err
	}
	byGroup, ok := s.memberships[user]
	if !ok {
		return false, nil
	}
	if _, ok := byGroup[g.ID]; !ok {
		return false, nil
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM memberships WHERE user = ? AND group_id = ?`, user, g.ID)
		if err != nil {
			return err
		}
		return expectOneRow(res, fmt.Sprintf("membership (user '%s', group '%s')", user, g.ID))
	}); err != nil {
		return false, err
	}
	delete(byGroup, g.ID)
	if len(byGroup) == 0 {
		delete(s.memberships, user)
	}
	return true, nil
}

// ExcludeRead cancels the read user would otherwise INHERIT on the group,
// removing it from their recall scope (and from the console's inherited list).
// It is the counterpart to Revoke: Revoke deletes a stored grant, ExcludeRead
// subtracts a derived one, which has no row to delete.
//
// Reports false (recording nothing) when there is nothing to remove — the user
// does not inherit read on the group, holds it DIRECTLY (revoke that instead;
// an explicit grant outranks an exclusion), or it is already excluded. That keeps
// the store free of exclusions that remove nothing, and lets callers
// tell "blocked it" from "the user had no access here anyway".
//
// Scope is the group alone, never its subtree — see ReadExclusion.
func (s *Store) ExcludeRead(user, groupRef, removedBy string) (bool, error) {
	if err := s.validatePersonKey(user); err != nil {
		return false, err
	}
	s.mu.Lock()
	g, err := s.resolveLocked(groupRef)
	if err != nil {
		s.mu.Unlock()
		return false, err
	}
	// Only an inherited read can be excluded: a direct member must be revoked,
	// and a group the user cannot reach has nothing to take away.
	if _, direct := s.memberships[user][g.ID]; direct {
		s.mu.Unlock()
		return false, nil
	}
	if !s.inheritsReadLocked(user, g.ID) {
		s.mu.Unlock()
		return false, nil
	}
	d := &ReadExclusion{
		User:      user,
		GroupID:   g.ID,
		RemovedBy: removedBy,
		RemovedAt: storedb.FormatTime(s.now()),
	}
	// An exclusion REMOVES access, so it must be durable before it takes
	// effect: committed first, map second, same as every other mutator — a
	// crash after the admin's removal was acknowledged must not hand the
	// memory read back on restart.
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO read_exclusions (user, group_id, removed_by, removed_at)
			 VALUES (?, ?, ?, ?)`,
			d.User, d.GroupID, d.RemovedBy, d.RemovedAt)
		return err
	}); err != nil {
		s.mu.Unlock()
		return false, err
	}
	if s.excluded[user] == nil {
		s.excluded[user] = make(map[string]*ReadExclusion)
	}
	s.excluded[user][g.ID] = d
	s.mu.Unlock()
	return true, nil
}

// inheritsReadLocked reports whether user currently reaches groupID purely by
// downward inheritance — i.e. it is a descendant of one of their direct groups
// and not already excluded. Caller must hold s.mu.
func (s *Store) inheritsReadLocked(user, groupID string) bool {
	if _, direct := s.memberships[user][groupID]; direct {
		return false
	}
	if s.isExcludedLocked(user, groupID) {
		return false
	}
	return s.reachesByInheritanceLocked(user, groupID, "")
}

// reachesByInheritanceLocked reports whether one of user's direct groups
// other than ignoreDirect has groupID in its subtree. ignoreDirect lets
// Revoke evaluate inheritance as it will stand AFTER the
// direct grant on groupID is deleted, while the map still holds the row
// (the answer must be computed before the transaction that deletes it).
// Caller must hold s.mu.
func (s *Store) reachesByInheritanceLocked(user, groupID, ignoreDirect string) bool {
	for gid := range s.memberships[user] {
		if gid == ignoreDirect {
			continue
		}
		if _, ok := s.groups[gid]; !ok {
			continue
		}
		reach := make(map[string]bool)
		s.collectSubtreeLocked(gid, reach)
		if reach[groupID] {
			return true
		}
	}
	return false
}

// Revoke removes user's ACCESS to the group — the primary revoke used by every
// operator-facing removal. It drops the direct grant (if any) and, when the
// user would still reach the group by downward inheritance afterwards, records
// the read exclusion — BOTH IN ONE TRANSACTION. The console removal actions
// need the pair to be one fact: done as two mutations (RevokeDirectGrant, then
// ExcludeRead), a crash or refusal between them leaves the direct grant gone but
// the exclusion missing, so the group silently returns as inherited read with
// its memory still recallable — the exact hole the exclusion mechanism exists to
// close. To drop ONLY the direct grant and preserve inherited read (rollback /
// undo of a Grant), use RevokeDirectGrant instead.
//
// Returns (revoked, excluded): revoked = a direct grant was deleted,
// excluded = an inherited read was cut. (false, false) means the user had no
// access to the group to begin with (nothing was written — including the
// already-excluded case).
func (s *Store) Revoke(user, groupRef, removedBy string) (revoked, excluded bool, err error) {
	if err := s.validatePersonKey(user); err != nil {
		return false, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.resolveLocked(groupRef)
	if err != nil {
		return false, false, err
	}
	_, hasDirect := s.memberships[user][g.ID]
	// Would the user still inherit read once the direct row is gone? Computed
	// with the row still present, so the direct group itself is ignored in
	// the walk. An existing exclusion means there is nothing left to cut.
	needExclude := !s.isExcludedLocked(user, g.ID) &&
		s.reachesByInheritanceLocked(user, g.ID, g.ID)
	if !hasDirect && !needExclude {
		return false, false, nil
	}
	var d *ReadExclusion
	if needExclude {
		d = &ReadExclusion{
			User:      user,
			GroupID:   g.ID,
			RemovedBy: removedBy,
			RemovedAt: storedb.FormatTime(s.now()),
		}
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		if hasDirect {
			res, derr := tx.ExecContext(ctx,
				`DELETE FROM memberships WHERE user = ? AND group_id = ?`, user, g.ID)
			if derr != nil {
				return derr
			}
			if err := expectOneRow(res, fmt.Sprintf("membership (user '%s', group '%s')", user, g.ID)); err != nil {
				return err
			}
		}
		if d != nil {
			if _, derr := tx.ExecContext(ctx,
				`INSERT INTO read_exclusions (user, group_id, removed_by, removed_at)
				 VALUES (?, ?, ?, ?)`,
				d.User, d.GroupID, d.RemovedBy, d.RemovedAt); derr != nil {
				return derr
			}
		}
		return nil
	}); err != nil {
		return false, false, err
	}
	if hasDirect {
		byGroup := s.memberships[user]
		delete(byGroup, g.ID)
		if len(byGroup) == 0 {
			delete(s.memberships, user)
		}
	}
	if d != nil {
		if s.excluded[user] == nil {
			s.excluded[user] = make(map[string]*ReadExclusion)
		}
		s.excluded[user][g.ID] = d
	}
	return hasDirect, d != nil, nil
}

// isExcludedLocked reports whether user has a removed inherited read on
// groupID. Caller must hold s.mu.
func (s *Store) isExcludedLocked(user, groupID string) bool {
	_, ok := s.excluded[user][groupID]
	return ok
}

// RemoveUser drops every membership of user (token-revocation cleanup
// hook, plan §6-D2 consistency note). Returns the number removed. All of
// the user's rows are deleted in one transaction; on a persist error
// nothing is removed and the error is returned.
func (s *Store) RemoveUser(user string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The user is going away: their exclusions go with them (same
	// transaction), so a later re-invite of the same id starts from a clean
	// inheritance slate. Counted separately from n so a user with exclusions
	// but no memberships is still handled.
	nExcl := len(s.excluded[user])
	n := len(s.memberships[user])
	if n == 0 && nExcl == 0 {
		return 0, nil
	}
	if err := s.persist(func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM memberships WHERE user = ?`, user)
		if err != nil {
			return err
		}
		if err := expectRows(res, int64(n), fmt.Sprintf("memberships of user '%s'", user)); err != nil {
			return err
		}
		res, err = tx.ExecContext(ctx, `DELETE FROM read_exclusions WHERE user = ?`, user)
		if err != nil {
			return err
		}
		return expectRows(res, int64(nExcl), fmt.Sprintf("read exclusions of user '%s'", user))
	}); err != nil {
		return 0, err
	}
	delete(s.memberships, user)
	delete(s.excluded, user)
	return n, nil
}

// ListGroups returns the tree in DFS order (parents before children,
// siblings by name) with absolute depth (root = 1).
func (s *Store) ListGroups() []GroupInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roots := make([]string, 0)
	for id, g := range s.groups {
		if g.ParentID == "" {
			roots = append(roots, id)
		}
	}
	s.sortByNameLocked(roots)

	out := make([]GroupInfo, 0, len(s.groups))
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		g := s.groups[id]
		out = append(out, GroupInfo{
			ID: g.ID, Name: g.Name, ParentID: g.ParentID,
			CreatedAt: g.CreatedAt, Depth: depth,
		})
		kids := append([]string(nil), s.children[id]...)
		s.sortByNameLocked(kids)
		for _, c := range kids {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 1)
	}
	return out
}

func (s *Store) sortByNameLocked(ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		return s.groups[ids[i]].Name < s.groups[ids[j]].Name
	})
}

// ListMemberships returns all memberships sorted by user, then group name.
func (s *Store) ListMemberships() []Membership {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Membership, 0)
	for _, byGroup := range s.memberships {
		for _, m := range byGroup {
			out = append(out, *m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].User != out[j].User {
			return out[i].User < out[j].User
		}
		gi, gj := s.groups[out[i].GroupID], s.groups[out[j].GroupID]
		ni, nj := out[i].GroupID, out[j].GroupID
		if gi != nil {
			ni = gi.Name
		}
		if gj != nil {
			nj = gj.Name
		}
		return ni < nj
	})
	return out
}

// ── persistence (write-through to the unified store database) ───────

// persist runs fn inside one write transaction against the attached sink
// and commits it. It is called with the store write lock held, BEFORE the
// maps are touched: on any error the transaction rolls back and the caller
// returns without mutating, so the in-memory cache never gets ahead of the
// database. Groups and memberships are one consistency domain — an
// operation spanning both tables (DeleteGroupWithMembers) issues both
// statements through a single fn, so the pair commits or rolls back as one
// and the legacy two-file crash class (a membership stranded by its group's
// deletion) is unrepresentable, with the memberships.group_id foreign key
// as the schema-level backstop. With no sink attached (pure in-memory
// store) it is a no-op.
// The mutator API takes no context, so the transaction runs on a background
// context — a caller hanging up mid-request can never cancel a COMMIT and
// desynchronize cache and database.
func (s *Store) persist(fn func(ctx context.Context, tx *sql.Tx) error) error {
	if s.db == nil {
		return nil
	}
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("groups: persist begin: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("groups: persist: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("groups: persist commit: %w", err)
	}
	return nil
}

// expectRows fails when a statement did not touch exactly want rows — the
// cache said what exists, so anything else means cache and database have
// diverged and the mutation must not proceed.
func expectRows(res sql.Result, want int64, ref string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != want {
		return fmt.Errorf("%s: %d rows affected, want %d", ref, n, want)
	}
	return nil
}

// expectOneRow fails when a per-row UPDATE/DELETE did not touch exactly one
// row (see expectRows).
func expectOneRow(res sql.Result, ref string) error {
	return expectRows(res, 1, ref)
}

// Shutdown does nothing.
//
// Deprecated: persistence is write-through to SQLite (every mutation is
// committed before it returns); kept so call sites compile, removed in a
// follow-up release.
func (s *Store) Shutdown() {}

// Flush does nothing.
//
// Deprecated: persistence is write-through to SQLite (every mutation is
// committed before it returns); kept so call sites compile, removed in a
// follow-up release.
func (s *Store) Flush() {}

// newGroupID returns a canonical UUIDv4 string from crypto/rand.
// Hand-rolled to keep the console on stdlib-only direct dependencies.
func newGroupID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
