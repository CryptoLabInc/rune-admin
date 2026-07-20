package server

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/members"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
	pb "github.com/CryptoLabInc/rune-console/pkg/consolepb"
)

// This file covers the member-UUID person-key contract: group memberships
// are keyed by the immutable member id, tokens keep the email, and the gRPC
// layer resolves email → UUID per request. The shared groups store stays
// byte-identical with core; everything here exercises the branch-only edges.

// newMemberKeyedConsole builds a console wired the way the daemon wires it on
// this branch: a member registry as the dataplane directory and the
// member-UUID person-key contract injected into the groups store.
func newMemberKeyedConsole(t *testing.T) (*Console, *members.Store) {
	t.Helper()
	v := newTestConsole(t)
	reg := members.NewStore()
	v.SetMemberDirectory(reg)
	v.Groups().SetPersonKeyValidator(members.ValidateID)
	return v, reg
}

// seedMembershipGroupID is the group the seeded membership row points at.
// Group ids are opaque record tags, so a short literal is legal here.
const seedMembershipGroupID = "aaaa"

// seedMembershipDB returns a fresh store database holding one group and one
// membership row keyed by user — the on-disk state a daemon boots against.
func seedMembershipDB(t *testing.T, user string) *sql.DB {
	t.Helper()
	const ts = "2026-07-06T00:00:00Z"
	database := openTokensTestDB(t, filepath.Join(t.TempDir(), "runeconsole.db"))
	if _, err := database.Exec(
		`INSERT INTO groups (id, name, parent_id, created_at) VALUES (?, 'eng', '', ?)`,
		seedMembershipGroupID, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO memberships (user, group_id, role, granted_by, granted_at) VALUES (?, ?, 'read', 'local-admin:test', ?)`,
		user, seedMembershipGroupID, ts); err != nil {
		t.Fatal(err)
	}
	return database
}

// TestGroupsLoadFromDBUnderRealMemberIDValidator pins the load boundary under
// the validator the daemon actually injects (members.ValidateID), in both
// directions. internal/groups must not import internal/members, so its own
// validator test can only inject a stand-in — one that would REFUSE a real
// UUID — which leaves the positive direction unproven there: nothing shows a
// canonical member UUID survives the load. This package may import both, so
// it is pinned here. The row must not merely load without error; it must be
// INDEXED, or the daemon boots with every membership silently missing.
func TestGroupsLoadFromDBUnderRealMemberIDValidator(t *testing.T) {
	const memberUUID = "6f1d0e3a-1b2c-4d5e-8f90-a1b2c3d4e5f6"

	// daemon.go order: validator first, then load.
	s := groups.NewStore()
	s.SetPersonKeyValidator(members.ValidateID)
	if err := s.LoadFromDB(seedMembershipDB(t, memberUUID)); err != nil {
		t.Fatalf("load of a member-UUID-keyed row = %v, want nil", err)
	}
	ms := s.ListMemberships()
	if len(ms) != 1 || ms[0].User != memberUUID || ms[0].GroupID != seedMembershipGroupID {
		t.Fatalf("memberships = %+v, want exactly the %s grant on %s", ms, memberUUID, seedMembershipGroupID)
	}
	if ms[0].Role != groups.RoleRead {
		t.Errorf("loaded role = %q, want read", ms[0].Role)
	}

	// Mirror direction: the same contract refuses an email-keyed row.
	s2 := groups.NewStore()
	s2.SetPersonKeyValidator(members.ValidateID)
	err := s2.LoadFromDB(seedMembershipDB(t, "alice@corp.com"))
	if err == nil || !strings.Contains(err.Error(), "member id") {
		t.Errorf("load of an email-keyed row = %v, want a refusal naming the member id contract", err)
	}
}

// TestDataplaneResolvesEmailToMemberUUID pins the whole resolution chain:
// token email → member UUID → membership → the group tag the engine
// receives as the recall scope, plus the capture write gate keyed by the
// UUID (no membership exists under the email).
func TestDataplaneResolvesEmailToMemberUUID(t *testing.T) {
	v, reg := newMemberKeyedConsole(t)
	fake := &fakeEngine{}
	v.engine = fake // same-package test seam, as in grpc_failopen_test.go

	alice, err := reg.Add("alice@corp.com", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	g, err := v.Groups().CreateGroup("eng", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Groups().Grant(alice.ID, g.ID, groups.RoleWrite, "local-admin:test"); err != nil {
		t.Fatal(err)
	}
	tok, err := v.Tokens().AddToken("alice@corp.com", "member", nil)
	if err != nil {
		t.Fatal(err)
	}

	srv := NewConsoleGRPC(v)
	ctx := context.Background()

	if _, err := srv.Search(ctx, &pb.SearchRequest{Token: tok.Token, Vector: []float32{0.1, 0.2}, TopK: 5}); err != nil {
		t.Fatalf("Search = %v, want nil", err)
	}
	if len(fake.gotScope) != 1 || fake.gotScope[0] != g.ID {
		t.Fatalf("recall scope = %v, want [%s] (membership keyed by member UUID must be reached via the token email)",
			fake.gotScope, g.ID)
	}
	if _, err := srv.Insert(ctx, &pb.InsertRequest{Token: tok.Token, RmpItem: []byte{0x01}, MmItem: []byte{0x01}}); err != nil {
		t.Fatalf("Insert = %v, want nil (write grant exists under the member UUID)", err)
	}
}

// TestDataplaneNoMemberRowFailsClosed — a valid token whose user has no
// member row (owner/demo/CLI service tokens) keeps the email as judge key,
// which can hold no memberships on this branch: recall narrows to the
// public-only sentinel and capture is refused. Fail-closed, no special case.
func TestDataplaneNoMemberRowFailsClosed(t *testing.T) {
	v, _ := newMemberKeyedConsole(t)
	fake := &fakeEngine{}
	v.engine = fake

	srv := NewConsoleGRPC(v)
	ctx := context.Background()

	if _, err := srv.Search(ctx, &pb.SearchRequest{Token: tokens.DemoToken, Vector: []float32{0.1, 0.2}, TopK: 5}); err != nil {
		t.Fatalf("Search = %v, want nil", err)
	}
	if len(fake.gotScope) != 1 || fake.gotScope[0] != publicOnlyScopeSentinel {
		t.Errorf("no-member-row recall scope = %v, want [%q]", fake.gotScope, publicOnlyScopeSentinel)
	}
	_, err := srv.Insert(ctx, &pb.InsertRequest{Token: tokens.DemoToken, RmpItem: []byte{0x01}, MmItem: []byte{0x01}})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("no-member-row Insert code = %v, want PermissionDenied (ErrNoWriteGroup)", status.Code(err))
	}
}

// TestDataplaneRejectsMemberIDAsTokenIdentity — the keyspace-collision guard.
// A token User is not constrained to an email at mint, so an admin (or an
// attacker who reads a member UUID off the operator surface) could issue a
// token whose User IS a member's UUID. LookupByEmail would miss (the registry
// is email-keyed), the raw UUID would fall through as the judge key, and
// because memberships are UUID-keyed it would resolve to that member's full
// scope while skipping the disabled gate. The guard refuses a UUID-shaped
// identity with no registry row, so the impersonation is denied, not served.
func TestDataplaneRejectsMemberIDAsTokenIdentity(t *testing.T) {
	v, reg := newMemberKeyedConsole(t)
	fake := &fakeEngine{}
	v.engine = fake

	// Alice is a real member with recall scope over "eng".
	alice, err := reg.Add("alice@corp.com", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	g, err := v.Groups().CreateGroup("eng", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Groups().Grant(alice.ID, g.ID, groups.RoleWrite, "local-admin:test"); err != nil {
		t.Fatal(err)
	}

	// Attacker mints a token whose User is Alice's UUID (no '@', not an email).
	tok, err := v.Tokens().AddToken(alice.ID, "member", nil)
	if err != nil {
		t.Fatal(err)
	}

	srv := NewConsoleGRPC(v)
	ctx := context.Background()

	// Search must be denied — never handed Alice's scope.
	_, err = srv.Search(ctx, &pb.SearchRequest{Token: tok.Token, Vector: []float32{0.1, 0.2}, TopK: 5})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Search with member-UUID token identity code = %v, want PermissionDenied", status.Code(err))
	}
	if len(fake.gotScope) != 0 {
		t.Errorf("engine was reached with scope %v; a rejected impersonation must never call the engine", fake.gotScope)
	}
	// Insert (capture) must be denied too.
	_, err = srv.Insert(ctx, &pb.InsertRequest{Token: tok.Token, RmpItem: []byte{0x01}, MmItem: []byte{0x01}})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("Insert with member-UUID token identity code = %v, want PermissionDenied", status.Code(err))
	}
}

// TestGetPermissionsMemberKeyed — Me stays the token EMAIL while the
// memberships/tree come from the judge keyed by the member UUID; the
// member_roles listing is admin-only AND requires the admin email to be a
// registered member, and its user values are mapped UUID → email for display
// (a key with no member row is shown as-is).
func TestGetPermissionsMemberKeyed(t *testing.T) {
	v, reg := newMemberKeyedConsole(t)
	v.Groups().SetOrgAdmin("admin@corp.com")
	srv := NewConsoleGRPC(v)
	ctx := context.Background()

	alice, err := reg.Add("alice@corp.com", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	g, err := v.Groups().CreateGroup("eng", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Groups().Grant(alice.ID, g.ID, groups.RoleWrite, "local-admin:test"); err != nil {
		t.Fatal(err)
	}
	tokAlice, err := v.Tokens().AddToken("alice@corp.com", "member", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Me == email; the membership reached through the UUID is listed.
	resp, err := srv.GetPermissions(ctx, &pb.GetPermissionsRequest{Token: tokAlice.Token})
	if err != nil {
		t.Fatalf("GetPermissions = %v, want nil", err)
	}
	if resp.GetMe() != "alice@corp.com" {
		t.Errorf("Me = %q, want alice@corp.com (the human identity from the token)", resp.GetMe())
	}
	if len(resp.GetMemberships()) != 1 || resp.GetMemberships()[0].GetGroupId() != g.ID {
		t.Errorf("memberships = %+v, want the UUID-keyed grant on %s", resp.GetMemberships(), g.ID)
	}

	// The org admin need NOT be a registered member: include_member_roles is
	// gated on IsOrgAdmin alone. The admin is a management identity that is not
	// auto-seeded into the member registry, so an unregistered admin email must
	// still get the org-wide listing, with UUIDs mapped back to emails.
	tokAdmin, err := v.Tokens().AddToken("admin@corp.com", "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := srv.GetPermissions(ctx, &pb.GetPermissionsRequest{Token: tokAdmin.Token, IncludeMemberRoles: true})
	if err != nil {
		t.Fatalf("unregistered admin GetPermissions = %v, want nil (IsOrgAdmin is the only gate)", err)
	}
	if len(resp2.GetMemberRoles()) != 1 {
		t.Fatalf("member_roles = %+v, want exactly the eng grant", resp2.GetMemberRoles())
	}
	if got := resp2.GetMemberRoles()[0].GetUser(); got != "alice@corp.com" {
		t.Errorf("member_roles user = %q, want alice@corp.com (UUID mapped back to email)", got)
	}

	// A membership key with no member row stays as-is in the listing.
	ghost, err := reg.Add("ghost@corp.com", "Ghost")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Groups().Grant(ghost.ID, g.ID, groups.RoleRead, "local-admin:test"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Remove(ghost.ID); err != nil { // row gone, membership orphaned
		t.Fatal(err)
	}
	resp3, err := srv.GetPermissions(ctx, &pb.GetPermissionsRequest{Token: tokAdmin.Token, IncludeMemberRoles: true})
	if err != nil {
		t.Fatal(err)
	}
	seenGhost := false
	for _, mr := range resp3.GetMemberRoles() {
		if mr.GetUser() == ghost.ID {
			seenGhost = true
		}
	}
	if !seenGhost {
		t.Errorf("member_roles = %+v, want the orphaned key %s shown as-is", resp3.GetMemberRoles(), ghost.ID)
	}
}
