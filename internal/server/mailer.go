package server

import (
	"context"
	"fmt"

	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/pkg/regstr"
)

// InviteConnInfo is the console connection detail an invitee needs to redeem
// their invite (endpoint + CA pin). It rides in the invite mail alongside the
// clear bundle; it carries no secret.
type InviteConnInfo struct {
	ConsoleEndpoint string `json:"console_endpoint"`
	CAPemURL        string `json:"ca_pem_url"`
	CAPemSHA256     string `json:"ca_pem_sha256"`
}

// Mailer delivers an invite to its recipient by relaying the registration string
// to the runespace-cloud public API, which renders the email and sends it via
// OCI Email Delivery. Implementations must not log the registration string: it
// encodes the wrapping token, a credential.
type Mailer interface {
	SendInvite(ctx context.Context, to, toName string, b invites.ClearBundle, conn InviteConnInfo) error
}

// cloudInviteSender is the slice of the runespace-cloud client the mailer needs.
// It is declared here (not imported from internal/cloud) so package server keeps
// no dependency on the cloud client — the daemon injects a *cloud.Client, which
// satisfies this, and tests inject a fake.
type cloudInviteSender interface {
	SendInvite(ctx context.Context, sessionCookie, toEmail, toName, registrationString, inviterName, expiry string) error
}

// cloudMailer relays invites through the runespace-cloud public API
// (POST /api/v1/invites). The operator's cloud session cookie is read from the
// request context (the console BFF injects it via WithCloudCookie after
// requireSession); without it the relay cannot authenticate.
type cloudMailer struct {
	cloud cloudInviteSender
}

// NewCloudMailer returns a Mailer that relays invites through the cloud public API.
func NewCloudMailer(c cloudInviteSender) Mailer { return &cloudMailer{cloud: c} }

// SendInvite encodes the wrap handle + connection info into a registration string
// and posts it to the cloud relay as the logged-in operator. The inviter name is
// left blank so the cloud fills it from the authenticated session user; toName
// falls back to the email when the member has no display name (the cloud requires
// a non-empty recipient name).
func (m *cloudMailer) SendInvite(ctx context.Context, to, toName string, b invites.ClearBundle, conn InviteConnInfo) error {
	cookie := cloudCookieFromContext(ctx)
	if cookie == "" {
		return fmt.Errorf("no operator cloud session on the request; cannot relay invite")
	}
	reg, err := regstr.Encode(regstr.Registration{
		Endpoint: conn.ConsoleEndpoint,
		Token:    b.Handle,
		CASHA256: conn.CAPemSHA256,
	})
	if err != nil {
		return fmt.Errorf("encode registration string: %w", err)
	}
	if toName == "" {
		toName = to
	}
	return m.cloud.SendInvite(ctx, cookie, to, toName, reg, "", b.ExpiresAt)
}
