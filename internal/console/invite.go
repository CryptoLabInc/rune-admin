package console

import (
	"encoding/json"
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
	// the cloud principal cached on the session (no extra round-trip).
	var me struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(sess.Me, &me); err != nil || me.Email == "" {
		writeError(w, http.StatusBadGateway, "IDENTITY_UNAVAILABLE", "could not read the operator email from the session")
		return
	}
	name := me.Name
	if name == "" {
		name = me.Email
	}

	bundle, conn, err := s.inviter.IssueSelfInvite(me.Email, name)
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
	if err := s.cloud.SendInvite(r.Context(), sess.CloudCookie(), me.Email, name, reg, name, bundle.ExpiresAt); err != nil {
		s.writeCloudError(w, sess, err)
		return
	}

	// Never echo the registration string — delivery is mail-only by design.
	writeJSON(w, http.StatusOK, map[string]any{"sent": true, "email": me.Email})
}
