package server

import (
	"fmt"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
)

// SelfInviteIssuer issues a fresh single-use invite addressed to the operator
// themselves, for the console↔rune-mcp connection test. It mints a data-plane
// token, wraps it behind a one-time handle (issueWrapped), and returns the
// clear bundle plus the console connection info a redeemer needs. The console
// BFF turns that into a registration string and mails it via the cloud public
// API; rune-mcp later Unwraps the handle to recover the real token.
//
// It is deliberately clean-slate and idempotent: a repeat call for the same
// email revokes any prior token and drops the stale member row before
// re-issuing, so the dev test button can be pressed repeatedly without hitting
// the "member already active" / "token already exists" guards on the normal
// admin invite path.
type SelfInviteIssuer struct {
	console *Console
	members *members.Store
	invites *invites.Store
	conn    InviteConnInfo
	ttl     time.Duration
	role    string
}

// NewSelfInviteIssuer wires the issuer to the token store (via the Console),
// the member registry, and the invite wrap store. role is the token role bound
// to the issued invite; conn is the endpoint/CA info baked into the mail.
func NewSelfInviteIssuer(v *Console, m *members.Store, i *invites.Store, conn InviteConnInfo, ttl time.Duration, role string) *SelfInviteIssuer {
	return &SelfInviteIssuer{console: v, members: m, invites: i, conn: conn, ttl: ttl, role: role}
}

// IssueSelfInvite (re)issues an invite for email and returns the clear bundle
// (Handle = wrapping token) plus the connection info. The CA pin in the
// returned conn is recomputed live from the console's own CA/cert so it always
// matches what GetCACert will serve; on a missing-cert misconfiguration the pin
// is left as configured (typically empty).
func (s *SelfInviteIssuer) IssueSelfInvite(email, displayName string) (invites.ClearBundle, InviteConnInfo, error) {
	// Reuse the operator's existing member row (and its immutable UUID) when one
	// is present: group memberships are keyed by that UUID, so deleting and
	// re-adding the row would mint a new UUID and orphan every (self-granted)
	// membership — and for the org admin it would also break the no-delete-admin
	// rule. On re-issue we revoke only the prior token and keep the row; only a
	// row THIS call created is rolled back on failure (dropIfNew).
	var m *members.Member
	newMember := false
	if existing, err := s.members.GetByEmail(email); err == nil {
		if _, rerr := s.console.Tokens().RevokeToken(email); rerr != nil {
			// The prior credential did not clear: re-issuing now would mint a
			// second live token. Refuse.
			return invites.ClearBundle{}, InviteConnInfo{}, fmt.Errorf("revoke prior token: %w", rerr)
		}
		m = existing
	} else {
		added, aerr := s.members.Add(email, displayName)
		if aerr != nil {
			return invites.ClearBundle{}, InviteConnInfo{}, fmt.Errorf("add member: %w", aerr)
		}
		m, newMember = added, true
	}

	tok, err := s.console.Tokens().AddToken(email, s.role, nil)
	if err != nil {
		s.dropIfNew(m.ID, newMember)
		return invites.ClearBundle{}, InviteConnInfo{}, fmt.Errorf("mint token (role %q): %w", s.role, err)
	}

	bundle, err := s.invites.Issue(invites.IssueParams{
		MemberID:     m.ID,
		Email:        email,
		Role:         s.role,
		TokenValue:   tok.Token,
		CreationPath: inviteCreationPath,
		TTL:          s.ttl,
	})
	if err != nil {
		// Best-effort compensation: a revoke refusal here leaves an orphaned
		// credential, which the next re-issue's clean slate retries.
		_, _ = s.console.Tokens().RevokeToken(email)
		s.dropIfNew(m.ID, newMember)
		return invites.ClearBundle{}, InviteConnInfo{}, fmt.Errorf("issue invite: %w", err)
	}

	// Reinvite (not MarkInvited): a reused row may already be active, and
	// MarkInvited only advances from registered — Reinvite moves any non-disabled
	// state to invited, which is exactly what a re-issue means.
	if err := s.members.Reinvite(m.ID); err != nil {
		_ = s.invites.RevokePending(bundle.Handle)
		// Best-effort compensation, as above.
		_, _ = s.console.Tokens().RevokeToken(email)
		s.dropIfNew(m.ID, newMember)
		return invites.ClearBundle{}, InviteConnInfo{}, fmt.Errorf("mark invited: %w", err)
	}

	conn := s.conn
	if _, pin, perr := caPEMAndPin(s.console.Config()); perr == nil {
		conn.CAPemSHA256 = pin
	}
	return *bundle, conn, nil
}

// dropIfNew removes a member row that THIS issue attempt created (newMember),
// leaving a reused pre-existing row (e.g. the org admin's) intact on failure.
func (s *SelfInviteIssuer) dropIfNew(id string, newMember bool) {
	if !newMember {
		return
	}
	_ = s.members.Remove(id)
}
