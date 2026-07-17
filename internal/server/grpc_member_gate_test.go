package server

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/members"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
	pb "github.com/CryptoLabInc/rune-console/pkg/consolepb"
)

// TestMemberStatusGateOnDataplane covers the defense-in-depth half of member
// disable: even a token that still VALIDATES (e.g. issued out-of-band via the
// CLI after the member was disabled) must be denied on the token-validated
// gRPC paths when its user resolves to a disabled member row. Active members
// and users with no registry row (the demo/owner token) pass unchanged.
func TestMemberStatusGateOnDataplane(t *testing.T) {
	v := newTestConsole(t)
	fake := &fakeEngine{}
	v.engine = fake // same-package test seam, as in grpc_failopen_test.go

	reg := members.NewStore()
	v.SetMemberDirectory(reg)
	// Fixture for the validator injection (assertions below are unchanged):
	// with a member registry wired, memberships are keyed by member UUID, so
	// the groups store carries the member-id contract, as at daemon boot.
	v.Groups().SetPersonKeyValidator(members.ValidateID)

	// A disabled member whose token was never revoked — exactly the gap the
	// gate closes (the admin PATCH path revokes; this token bypassed it).
	dis, err := reg.Add("dis@corp.com", "Dis")
	if err != nil {
		t.Fatal(err)
	}
	disabledStatus := members.StatusDisabled
	if _, err := reg.Update(dis.ID, nil, &disabledStatus); err != nil {
		t.Fatal(err)
	}
	tokDis, err := v.Tokens().AddToken("dis@corp.com", "member", nil)
	if err != nil {
		t.Fatal(err)
	}

	// An active member with a write grant, so Insert also clears the judge.
	act, err := reg.Add("act@corp.com", "Act")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MarkInvited(act.ID); err != nil {
		t.Fatal(err)
	}
	if err := reg.Activate(act.ID); err != nil {
		t.Fatal(err)
	}
	tokAct, err := v.Tokens().AddToken("act@corp.com", "member", nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := v.Groups().CreateGroup("eng", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Groups().Grant(act.ID, g.ID, groups.RoleWrite, "local-admin:test"); err != nil {
		t.Fatal(err)
	}

	srv := NewConsoleGRPC(v)
	ctx := context.Background()

	// Disabled member: denied on Search and Insert, before any engine work.
	if _, err := srv.Search(ctx, &pb.SearchRequest{Token: tokDis.Token, Vector: []float32{0.1, 0.2}, TopK: 5}); status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled member Search = %v, want PermissionDenied mentioning disabled", err)
	}
	if fake.called {
		t.Error("engine.Search ran for a disabled member")
	}
	if _, err := srv.Insert(ctx, &pb.InsertRequest{Token: tokDis.Token, RmpItem: []byte{0x01}, MmItem: []byte{0x01}}); status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled member Insert = %v, want PermissionDenied mentioning disabled", err)
	}

	// Active member: passes the gate on both paths.
	if _, err := srv.Search(ctx, &pb.SearchRequest{Token: tokAct.Token, Vector: []float32{0.1, 0.2}, TopK: 5}); err != nil {
		t.Errorf("active member Search = %v, want nil", err)
	}
	if !fake.called {
		t.Error("engine.Search did not run for the active member")
	}
	if _, err := srv.Insert(ctx, &pb.InsertRequest{Token: tokAct.Token, RmpItem: []byte{0x01}, MmItem: []byte{0x01}}); err != nil {
		t.Errorf("active member Insert = %v, want nil", err)
	}

	// A user with NO member-registry row (the demo/owner token) is not gated.
	if _, err := srv.Search(ctx, &pb.SearchRequest{Token: tokens.DemoToken, Vector: []float32{0.1, 0.2}, TopK: 5}); err != nil {
		t.Errorf("no-registry-row Search = %v, want nil", err)
	}
}
