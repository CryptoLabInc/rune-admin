package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
)

// This file holds the small idioms the /api/v1 domain handlers (console_api*.go)
// share. They used to live alongside the /admin route table; that surface was
// removed (the BFF /api/v1 API is the sole cookie-gated management surface), so
// the survivors were relocated here.

// inviteCreationPath binds every console-issued invite to a known wrap path
// (§8.3) so the redeem-side Lookup/Unwrap presents the same path. It is a stable
// wire constant matched by grpc.go's Lookup — it must not change, or previously
// issued envelopes stop resolving.
const inviteCreationPath = "admin.member.invite"

// memberSubsystem bundles the member registry, invite store, mailer, and the
// invite defaults shared by the /api/v1 domain handlers (consoleAPI).
type memberSubsystem struct {
	members *members.Store
	invites *invites.Store
	mailer  Mailer
	conn    InviteConnInfo
	ttl     time.Duration
}

// localAdminActor builds the audit identity for console mutations under the
// "local-admin:" prefix. An empty actor (an untagged request) records as
// "unknown", so audit never crashes on an untagged request.
func localAdminActor(actor string) string {
	if actor == "" {
		actor = "unknown"
	}
	return "local-admin:" + actor
}

// auditAdmin emits an audit entry for a console mutation (methods
// admin.token.*, admin.group.*, admin.member.*, admin.invite.*). target names
// the entity acted ON — the token's user, the group, the "user @ group"
// membership — while UserID records who acted.
func auditAdmin(v *Console, method, actor, target string) {
	if v.audit == nil || !v.audit.Enabled() {
		return
	}
	v.audit.Log(AuditEntry{
		Timestamp: nowUTCISO(),
		UserID:    localAdminActor(actor),
		Method:    method,
		Status:    "success",
		SourceIP:  "console-bff",
		Target:    target,
	})
}

// tokenExistsForUser reports whether the tokens store already holds a token for
// user (the store enforces 1-user-1-token).
func tokenExistsForUser(v *Console, user string) bool {
	for _, t := range v.Tokens().ListTokens() {
		if t.User == user {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	buf, err := json.Marshal(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, dst any) error {
	if r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}
