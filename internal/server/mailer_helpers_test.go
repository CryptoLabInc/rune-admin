package server

import (
	"context"

	"github.com/CryptoLabInc/rune-console/internal/invites"
)

// recordedInvite is one invite captured by recordingMailer.
type recordedInvite struct {
	to, toName string
	bundle     invites.ClearBundle
	conn       InviteConnInfo
}

// recordingMailer captures invite relays in memory, standing in for the cloud
// relay so tests can inject a mailer without a live cloud.
type recordingMailer struct {
	sent []recordedInvite
}

func (m *recordingMailer) SendInvite(_ context.Context, to, toName string, b invites.ClearBundle, conn InviteConnInfo) error {
	m.sent = append(m.sent, recordedInvite{to: to, toName: toName, bundle: b, conn: conn})
	return nil
}
