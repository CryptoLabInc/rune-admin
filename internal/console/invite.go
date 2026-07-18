package console

import (
	"net/http"

	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/server"
	"github.com/CryptoLabInc/rune-console/pkg/regstr"
)

// InviteIssuer issues a fresh single-use self-invite and returns the wrap
// bundle (Handle = wrapping token) plus the console connection info. It is
// implemented by *server.SelfInviteIssuer and injected via Deps so the BFF can
// mint a registration string without owning the member/token/invite stores.
type InviteIssuer interface {
	IssueSelfInvite(email, displayName string) (invites.ClearBundle, server.InviteConnInfo, error)
}

// handleInvite (POST /api/v1/invite) is the connection-test hook: it issues a
// fresh single-use invite addressed to the logged-in operator themselves,
// encodes it into an opaque registration string, and mails it via the cloud
// public API (POST /api/v1/invites). The registration string is a credential —
// it carries the wrapping token — so it is deliberately NOT echoed in the
// response; the only way to obtain it is the delivered email.
func (s *Service) handleInvite(w http.ResponseWriter, r *http.Request) {
	if s.inviter == nil {
		writeError(w, http.StatusNotImplemented, "INVITE_DISABLED", "invite issuance is not enabled on this console")
		return
	}
	sess := s.sessionFrom(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "SESSION_INVALID", "not logged in")
		return
	}

	// The invite is addressed to the operator themselves; identity comes from
	// the cloud principal cached on the session (no extra round-trip). Read it
	// through the shared helpers, not a local unmarshal: the member row and
	// token this mints are keyed by the email, so they must carry the same
	// canonical form as every other identity keyspace. The mail is addressed
	// with the raw spelling — the recipient's mail host, not this console,
	// decides what its local part means.
	email := emailFromMe(sess.Me)
	if email == "" {
		writeError(w, http.StatusBadGateway, "IDENTITY_UNAVAILABLE", "could not read the operator email from the session")
		return
	}
	mailTo := rawEmailFromMe(sess.Me)
	name := nameFromMe(sess.Me, email)

	bundle, conn, err := s.inviter.IssueSelfInvite(email, name)
	if err != nil {
		s.log.Warn("console: issue self-invite failed", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "INVITE_ISSUE_FAILED", err.Error())
		return
	}

	reg, err := regstr.Encode(regstr.Registration{
		Endpoint: conn.ConsoleEndpoint,
		Token:    bundle.Handle,
		CASHA256: conn.CAPemSHA256,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "REGSTR_ENCODE_FAILED", err.Error())
		return
	}

	// inviter == recipient (self-invite); expiry is the wrap TTL deadline.
	if err := s.cloud.SendInvite(r.Context(), sess.CloudCookie(), mailTo, name, reg, name, bundle.ExpiresAt); err != nil {
		s.writeCloudError(w, sess, err)
		return
	}

	// Never echo the registration string — delivery is mail-only by design.
	// The reported address is the one actually mailed.
	writeJSON(w, http.StatusOK, map[string]any{"sent": true, "email": mailTo})
}
