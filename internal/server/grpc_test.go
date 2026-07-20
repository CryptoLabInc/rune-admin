package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
	pb "github.com/CryptoLabInc/rune-console/pkg/consolepb"
)

// ── error mapping ─────────────────────────────────────────────────

func TestMapTokenErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		code codes.Code
	}{
		{tokens.ErrTokenNotFound{}, codes.Unauthenticated},
		{tokens.ErrTokenExpired{User: "x"}, codes.Unauthenticated},
		{errors.New("random"), codes.Unauthenticated},
	}
	for _, c := range cases {
		got, _ := mapTokenError(c.err)
		if got != c.code {
			t.Errorf("mapTokenError(%v) = %v, want %v", c.err, got, c.code)
		}
	}
}

// ── handler — token error paths (no engine needed; auth runs first) ──

func newTestConsole(t *testing.T) *Console {
	t.Helper()
	cfg := &Config{
		Tokens: TokensConfig{TeamSecret: "test-secret"},
		Keys:   KeysConfig{Path: t.TempDir(), EmbeddingDim: 1024},
	}
	store := tokens.NewStore()
	store.LoadDefaultsWithDemoToken()
	audit, _ := NewAuditLogger(AuditConfig{Mode: ""})
	return NewConsole(cfg, store, groups.NewStore(), nil, audit)
}

func TestGetAgentManifestInvalidToken(t *testing.T) {
	srv := NewConsoleGRPC(newTestConsole(t))
	resp, err := srv.GetAgentManifest(context.Background(), &pb.GetAgentManifestRequest{
		Token: "evt_ffffffffffffffffffffffffffffffff",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
	if resp.GetError() == "" {
		t.Error("response.error is empty")
	}
}

func TestInsertInvalidToken(t *testing.T) {
	srv := NewConsoleGRPC(newTestConsole(t))
	_, err := srv.Insert(context.Background(), &pb.InsertRequest{
		Token:    "evt_ffffffffffffffffffffffffffffffff",
		RmpItem:  []byte{0x01},
		MmItem:   []byte{0x01},
		Metadata: `{"x":1}`,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestSearchInvalidToken(t *testing.T) {
	srv := NewConsoleGRPC(newTestConsole(t))
	_, err := srv.Search(context.Background(), &pb.SearchRequest{
		Token:  "evt_ffffffffffffffffffffffffffffffff",
		Vector: []float32{0.1, 0.2},
		TopK:   5,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", status.Code(err))
	}
}

// ── LookupWrap / Unwrap (invite redemption — pre-auth) ────────────

const testInviteToken = "evt_00000000000000000000000000000000"

// newRedemptionHarness wires a gRPC server with an in-memory member registry
// and invite store (no database attached, so the write-through persist is a
// no-op and the tests read the stores' own state), and issues one invite for
// kim@example.com with the given TTL. It returns the issued clear bundle for
// the tests to redeem.
func newRedemptionHarness(t *testing.T, ttl time.Duration) (*ConsoleGRPC, *invites.Store, *members.Store, *invites.ClearBundle) {
	t.Helper()
	ms := members.NewStore()
	is := invites.NewStore()
	m, err := ms.Add("kim@example.com", "Kim")
	if err != nil {
		t.Fatal(err)
	}
	b, err := is.Issue(invites.IssueParams{
		MemberID:     m.ID,
		Email:        m.Email,
		Role:         "member",
		TokenValue:   testInviteToken,
		CreationPath: inviteCreationPath,
		TTL:          ttl,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ms.MarkInvited(m.ID); err != nil {
		t.Fatal(err)
	}
	v := newTestConsole(t)
	v.SetMemberDirectory(ms)
	v.SetInviteRedemption(is, ms)
	return NewConsoleGRPC(v), is, ms, b
}

func TestLookupWrapReturnsInviteInfoWithoutConsuming(t *testing.T) {
	srv, _, _, b := newRedemptionHarness(t, time.Hour)
	// Read-only: a second lookup must succeed identically.
	for i := 0; i < 2; i++ {
		resp, err := srv.LookupWrap(context.Background(), &pb.LookupWrapRequest{Handle: b.Handle})
		if err != nil {
			t.Fatalf("lookup %d: %v", i+1, err)
		}
		if resp.GetEmail() != "kim@example.com" || resp.GetRole() != "member" {
			t.Errorf("lookup %d: got (%q, %q)", i+1, resp.GetEmail(), resp.GetRole())
		}
		if resp.GetCreationPath() != inviteCreationPath {
			t.Errorf("lookup %d: creation_path = %q", i+1, resp.GetCreationPath())
		}
	}
}

func TestUnwrapReleasesTokenOnceAndActivates(t *testing.T) {
	srv, _, ms, b := newRedemptionHarness(t, time.Hour)
	resp, err := srv.Unwrap(context.Background(), &pb.UnwrapRequest{Handle: b.Handle})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetToken() != testInviteToken {
		t.Errorf("token = %q, want the sealed test token", resp.GetToken())
	}
	m, err := ms.Get(resp.GetMemberId())
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != members.StatusActive {
		t.Errorf("member status = %q, want active", m.Status)
	}
	// The second redemption must fail loudly: "already used" is the
	// interception alarm (§8.3), not a silent NotFound.
	_, err = srv.Unwrap(context.Background(), &pb.UnwrapRequest{Handle: b.Handle})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("second unwrap code = %v, want FailedPrecondition", status.Code(err))
	}
	if !strings.Contains(err.Error(), "already been used") {
		t.Errorf("err = %v, want 'already been used'", err)
	}
}

func TestUnwrapExpiredInvite(t *testing.T) {
	srv, _, _, b := newRedemptionHarness(t, -time.Minute) // born expired
	_, err := srv.Unwrap(context.Background(), &pb.UnwrapRequest{Handle: b.Handle})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", status.Code(err))
	}
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("err = %v, want 'expired'", err)
	}
}

func TestLookupWrapUnknownHandle(t *testing.T) {
	srv, _, _, _ := newRedemptionHarness(t, time.Hour)
	_, err := srv.LookupWrap(context.Background(), &pb.LookupWrapRequest{Handle: strings.Repeat("f", 32)})
	if status.Code(err) != codes.NotFound {
		t.Errorf("code = %v, want NotFound", status.Code(err))
	}
}

func TestUnwrapCompromisedInvite(t *testing.T) {
	srv, is, _, b := newRedemptionHarness(t, time.Hour)
	if _, err := srv.Unwrap(context.Background(), &pb.UnwrapRequest{Handle: b.Handle}); err != nil {
		t.Fatal(err)
	}
	if err := is.ReportCompromise(b.LeaseID); err != nil {
		t.Fatal(err)
	}
	_, err := srv.Unwrap(context.Background(), &pb.UnwrapRequest{Handle: b.Handle})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("code = %v, want PermissionDenied", status.Code(err))
	}
}

func TestUnwrapDisabledMemberDoesNotConsume(t *testing.T) {
	srv, _, ms, b := newRedemptionHarness(t, time.Hour)
	m, err := ms.GetByEmail("kim@example.com")
	if err != nil {
		t.Fatal(err)
	}
	disabled := members.StatusDisabled
	if _, err := ms.Update(m.ID, nil, &disabled); err != nil {
		t.Fatal(err)
	}
	_, err = srv.Unwrap(context.Background(), &pb.UnwrapRequest{Handle: b.Handle})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", status.Code(err))
	}
	// The refusal must not burn the one-time code: restore, then redeem.
	invited := members.StatusInvited
	if _, err := ms.Update(m.ID, nil, &invited); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Unwrap(context.Background(), &pb.UnwrapRequest{Handle: b.Handle}); err != nil {
		t.Fatalf("unwrap after restore: %v", err)
	}
}

func TestRedemptionUnwiredIsUnimplemented(t *testing.T) {
	srv := NewConsoleGRPC(newTestConsole(t))
	_, err := srv.LookupWrap(context.Background(), &pb.LookupWrapRequest{Handle: strings.Repeat("a", 32)})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("lookup code = %v, want Unimplemented", status.Code(err))
	}
	_, err = srv.Unwrap(context.Background(), &pb.UnwrapRequest{Handle: strings.Repeat("a", 32)})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("unwrap code = %v, want Unimplemented", status.Code(err))
	}
}
