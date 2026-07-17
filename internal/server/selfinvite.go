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
// matches what GetCACert will serve; when TLS is disabled the pin is left as
// configured (typically empty).
func (s *SelfInviteIssuer) IssueSelfInvite(email, displayName string) (invites.ClearBundle, InviteConnInfo, error) {
	// Clean slate: drop any prior member + token for this email so a re-issue is
	// unconditional. RevokeToken/Remove are no-ops when nothing exists.
	if m, err := s.members.GetByEmail(email); err == nil {
		if _, rerr := s.console.Tokens().RevokeToken(email); rerr != nil {
			// The clean slate did not commit: re-issuing now would mint a
			// second credential while the old one stays live. Refuse.
			return invites.ClearBundle{}, InviteConnInfo{}, fmt.Errorf("revoke prior token: %w", rerr)
		}
		_ = s.members.Remove(m.ID)
		s.members.Flush()
	}

	m, err := s.members.Add(email, displayName)
	if err != nil {
		return invites.ClearBundle{}, InviteConnInfo{}, fmt.Errorf("add member: %w", err)
	}

	tok, err := s.console.Tokens().AddToken(email, s.role, nil)
	if err != nil {
		_ = s.members.Remove(m.ID)
		s.members.Flush()
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
		_ = s.members.Remove(m.ID)
		s.members.Flush()
		return invites.ClearBundle{}, InviteConnInfo{}, fmt.Errorf("issue invite: %w", err)
	}
	// Flush the token before the envelope's "invited" state escapes: a crash on
	// the tokens debounce window would otherwise wrap a token that no longer
	// exists (mirrors the admin invite path).
	s.console.Tokens().Flush()

	if err := s.members.MarkInvited(m.ID); err != nil {
		_ = s.invites.RevokePending(bundle.Handle)
		// Best-effort compensation, as above.
		_, _ = s.console.Tokens().RevokeToken(email)
		_ = s.members.Remove(m.ID)
		s.members.Flush()
		return invites.ClearBundle{}, InviteConnInfo{}, fmt.Errorf("mark invited: %w", err)
	}
	s.members.Flush()

	conn := s.conn
	if _, pin, perr := caPEMAndPin(s.console.Config()); perr == nil {
		conn.CAPemSHA256 = pin
	}
	return *bundle, conn, nil
}
