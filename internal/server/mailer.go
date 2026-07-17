package server

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/invites"
)

// InviteConnInfo is the console connection detail an invitee needs to redeem
// their invite (endpoint + CA pin). It rides in the invite mail alongside the
// clear bundle; it carries no secret.
type InviteConnInfo struct {
	ConsoleEndpoint string `json:"console_endpoint"`
	CAPemURL        string `json:"ca_pem_url"`
	CAPemSHA256     string `json:"ca_pem_sha256"`
}

// Mailer delivers an invite to its recipient. The clear bundle is secret-free
// (no token), so a Mailer implementation may log or transmit it verbatim.
type Mailer interface {
	SendInvite(ctx context.Context, to string, b invites.ClearBundle, conn InviteConnInfo) error
}

// LogMailer is a stub Mailer that appends one JSON line per invite to a file
// (O_APPEND, 0600). It stands in for real SMTP delivery, which is on the
// roadmap; the append log lets operators/tests confirm an invite was "sent".
type LogMailer struct {
	mu   sync.Mutex
	path string
}

// NewLogMailer returns a LogMailer writing to path.
func NewLogMailer(path string) *LogMailer { return &LogMailer{path: path} }

// SendInvite appends a single JSON record for the invite. It is safe for
// concurrent use.
func (m *LogMailer) SendInvite(_ context.Context, to string, b invites.ClearBundle, conn InviteConnInfo) error {
	rec := map[string]any{
		"ts":     time.Now().UTC().Format(time.RFC3339),
		"to":     to,
		"bundle": b,
		"conn":   conn,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}
