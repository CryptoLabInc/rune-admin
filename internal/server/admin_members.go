package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
)

// inviteCreationPath binds every admin-socket-issued invite to a known wrap
// path (§8.3). A future Lookup/Unwrap surface presents the same path.
const inviteCreationPath = "admin.member.invite"

// memberSubsystem bundles the member registry, invite store, mailer, and the
// invite defaults. It is threaded into buildAdminMux explicitly rather than
// stored on Console (see NewMemberAdminFactory / grpc.go note).
type memberSubsystem struct {
	members *members.Store
	invites *invites.Store
	mailer  Mailer
	conn    InviteConnInfo
	ttl     time.Duration
}

// registerMemberRoutes adds the member CRUD + invite routes. The token/role/
// group routes and all shared idioms (readJSON/writeJSON/writeError/
// localAdminActor) are reused from admin.go.
func registerMemberRoutes(mux *http.ServeMux, v *Console, ms *memberSubsystem) {
	mux.HandleFunc("GET /members", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"members": ms.members.List()})
	})

	mux.HandleFunc("POST /members", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
			Group       string `json:"group"`      // optional: group ref to grant on registration
			GroupRole   string `json:"group_role"` // required iff group is set: read|write|edit
			Actor       string `json:"actor"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Email == "" {
			writeError(w, http.StatusBadRequest, "Missing required field: email")
			return
		}
		// Atomic register+grant (design-decisions §8.3): when a group is named,
		// registration and the group grant are one transaction. Validate the
		// role before creating anything; then, if the grant fails, hard-remove
		// the just-created member so a rejected grant leaves no half-registered
		// ghost. No token is issued here — that is a separate, later step
		// (POST /members/{id}/invite), so a failed grant strands no credential.
		var grantRole groups.Role
		if body.Group != "" {
			if body.GroupRole == "" {
				writeError(w, http.StatusBadRequest, "Missing required field: group_role (required when group is set)")
				return
			}
			role, perr := groups.ParseRole(body.GroupRole)
			if perr != nil {
				writeError(w, http.StatusBadRequest, perr.Error())
				return
			}
			grantRole = role
		}
		m, err := ms.members.Add(body.Email, body.DisplayName)
		if err != nil {
			writeMemberError(w, err)
			return
		}
		if body.Group != "" {
			// Memberships are keyed by the immutable member UUID (m.ID), not
			// the email — the id was just minted by Add above.
			if _, gerr := v.Groups().Grant(m.ID, body.Group, grantRole, localAdminActor(adminActor(r, body.Actor))); gerr != nil {
				// The transaction did not commit: undo the member.
				_ = ms.members.Remove(m.ID)
				ms.members.Flush()
				writeGroupError(w, gerr)
				return
			}
			// Commit the pair durably. Two YAML stores can't share one fsync, so
			// a crash between the two flushes could still leave a member without
			// its grant; the rollback above covers the common (grant-rejected)
			// case, and the residual window is the same two-file gap the
			// token/group stores already live with (see DELETE /tokens).
			ms.members.Flush()
			v.Groups().Flush()
			auditAdmin(v, "admin.group.grant", adminActor(r, body.Actor), fmt.Sprintf("%s @ %s (%s)", body.Email, body.Group, body.GroupRole))
		}
		auditAdmin(v, "admin.member.create", adminActor(r, body.Actor), body.Email)
		writeJSON(w, http.StatusCreated, m)
	})

	mux.HandleFunc("PATCH /members/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Partial update: only the keys present in the body change (nil =
		// untouched). Same raw-map idiom as PUT /roles.
		var raw map[string]json.RawMessage
		if err := readJSON(r, &raw); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Email is immutable: it is the person's join key — tokens are keyed
		// by it, and the dataplane resolves it to the member UUID the group
		// memberships are keyed by — so a rename would have to move the
		// token ledger and the registry join in lockstep, and a
		// display-attribute edit must never be able to write to the
		// authorization ledger. Renaming a person is simply not supported.
		// Reject the field outright rather than special-casing "same value":
		// the contract is "email is not updatable".
		if _, ok := raw["email"]; ok {
			writeError(w, http.StatusBadRequest, "email is immutable: tokens are keyed by it and it is the join key to the member id, so a member cannot be renamed")
			return
		}
		displayName, err := optStringField(raw, "display_name")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		status, err := optStringField(raw, "status")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if displayName == nil && status == nil {
			writeError(w, http.StatusBadRequest, "No fields to update")
			return
		}
		actor := ""
		if v, ok := raw["actor"]; ok {
			_ = json.Unmarshal(v, &actor)
		}
		m, err := ms.members.Update(r.PathValue("id"), displayName, status)
		if err != nil {
			writeMemberError(w, err)
			return
		}
		// Member disable must cut access, not just relabel the row: revoke the
		// member's token so the evt_ credential stops authenticating at once.
		// Email is immutable, so there is exactly one address the token could
		// have been minted under — m.Email. Grants are deliberately KEPT —
		// restore preserves memberships — and the dataplane member-status gate
		// (grpc.go resolveMemberAccess) backstops any token issued out-of-band
		// while disabled. Run on every PATCH that lands on disabled (not only
		// the first): re-asserting "disabled ⇒ no token" is idempotent and
		// self-heals a token slipped in via the CLI.
		if status != nil && *status == members.StatusDisabled {
			// The member row is already disabled (Update committed above); if
			// the revocation does not commit, surface the partial state
			// instead of a clean 200 — the token row is still present, though
			// the dataplane member-status gate already refuses it for a
			// disabled member. Re-PATCHing disabled retries the revoke.
			if _, rerr := v.Tokens().RevokeToken(m.Email); rerr != nil {
				writeError(w, http.StatusInternalServerError,
					fmt.Sprintf("member disabled but token revocation did not commit (retry by re-applying status=disabled): %v", rerr))
				return
			}
			// A still-pending invite envelope for this member is NOT voided
			// here: the invites store has no by-member lookup (RevokePending
			// needs the handle, which this handler does not hold). Revoking the
			// token above already kills the sealed value — an Unwrap after this
			// point releases a token that no longer authenticates.
		}
		auditAdmin(v, "admin.member.update", adminActor(r, actor), m.Email)
		writeJSON(w, http.StatusOK, m)
	})

	mux.HandleFunc("POST /members/{id}/invite", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Role       string `json:"role"`
			TTLMinutes *int   `json:"ttl_minutes"`
			Actor      string `json:"actor"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Role == "" {
			writeError(w, http.StatusBadRequest, "Missing required field: role")
			return
		}
		id := r.PathValue("id")
		m, err := ms.members.Get(id)
		if err != nil {
			writeMemberError(w, err)
			return
		}
		// Only a registered member (first invite) or an already-invited member
		// (idempotent re-issue) may be invited. A disabled member must be
		// restored first; an active member has already accepted and holds a
		// token, so re-inviting it is a conflict.
		switch m.Status {
		case members.StatusDisabled:
			writeError(w, http.StatusConflict, fmt.Sprintf("member '%s' is disabled", id))
			return
		case members.StatusActive:
			writeError(w, http.StatusConflict, fmt.Sprintf("member '%s' is already active (invite already accepted)", id))
			return
		}
		// Pre-checks so error mapping is precise without matching untyped
		// tokens-store error text: unknown role → 400, existing token → 409.
		if !tokenRoleExists(v, body.Role) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("role '%s' does not exist", body.Role))
			return
		}
		if tokenExistsForUser(v, m.Email) {
			writeError(w, http.StatusConflict, fmt.Sprintf("a token already exists for member email '%s'", m.Email))
			return
		}
		// Mint the evt_ token via the existing tokens store, keyed by member
		// email, then wrap it. The clear bundle never carries the token.
		tok, err := v.Tokens().AddToken(m.Email, body.Role, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ttl := ms.ttl
		if body.TTLMinutes != nil && *body.TTLMinutes > 0 {
			ttl = time.Duration(*body.TTLMinutes) * time.Minute
		}
		bundle, err := ms.invites.Issue(invites.IssueParams{
			MemberID:     m.ID,
			Email:        m.Email,
			Role:         body.Role,
			TokenValue:   tok.Token,
			CreationPath: inviteCreationPath,
			TTL:          ttl,
		})
		if err != nil {
			// Roll back the just-minted token so a failed wrap leaves no
			// stranded token.
			v.Tokens().RevokeToken(m.Email)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Durability invariant: a returned bundle implies the wrapped token is
		// durable. Issue persisted the envelope synchronously (fsync+rename),
		// but AddToken above sits on the tokens store's 100ms persist debounce
		// — a crash inside that window would leave a durable envelope wrapping
		// a token that no longer exists: an invite that can never authenticate,
		// with no cleanup path. Flush the tokens store before the bundle (or
		// the invited status) escapes. Flush runs the debounced persist
		// synchronously and returns once it is on disk.
		v.Tokens().Flush()
		// The envelope is durably on disk (Issue is persist-before-return), so
		// the member may now advance registered→invited. On failure, void the
		// envelope and the token so no half-issued invite survives.
		if err := ms.members.MarkInvited(m.ID); err != nil {
			_ = ms.invites.RevokePending(bundle.Handle)
			v.Tokens().RevokeToken(m.Email)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("invite issued but member could not be advanced: %v", err))
			return
		}
		ms.members.Flush()
		// Email is a best-effort notification sent AFTER the invite is durably
		// issued (design-decisions §8.3): "invited" means the envelope is on
		// disk, not that mail was delivered. A delivery failure does NOT fail
		// the request or roll back state — the operator resends; the invite
		// stands.
		if err := ms.mailer.SendInvite(r.Context(), m.Email, *bundle, ms.conn); err != nil {
			slog.Warn("invite mail delivery failed; invite stands, operator can resend",
				"member_id", m.ID, "email", m.Email, "err", err)
		}
		auditAdmin(v, "admin.invite.issue", adminActor(r, body.Actor), m.Email)
		writeJSON(w, http.StatusCreated, bundle)
	})
}

// optStringField extracts an optional string field from a raw JSON map:
// absent → nil (untouched), present-but-not-a-string → error.
func optStringField(raw map[string]json.RawMessage, key string) (*string, error) {
	v, ok := raw[key]
	if !ok {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return nil, fmt.Errorf("%s must be a string", key)
	}
	return &s, nil
}

// tokenRoleExists reports whether roleName is a known token role.
func tokenRoleExists(v *Console, roleName string) bool {
	for _, r := range v.Tokens().ListRoles() {
		if r.Name == roleName {
			return true
		}
	}
	return false
}

// tokenExistsForUser reports whether the tokens store already holds a token
// for user (the store enforces 1-user-1-token).
func tokenExistsForUser(v *Console, user string) bool {
	for _, t := range v.Tokens().ListTokens() {
		if t.User == user {
			return true
		}
	}
	return false
}

// writeMemberError maps members package errors onto admin HTTP statuses:
// unknown member → 404, duplicate email → 409, invalid email/status → 400.
func writeMemberError(w http.ResponseWriter, err error) {
	switch {
	case errors.As(err, new(members.ErrMemberNotFound)):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.As(err, new(members.ErrDuplicateEmail)):
		writeError(w, http.StatusConflict, err.Error())
	case errors.As(err, new(members.ErrInvalidEmail)),
		errors.As(err, new(members.ErrInvalidStatus)):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
