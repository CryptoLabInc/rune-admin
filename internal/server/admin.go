package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
)

// NewAdminHandler builds the admin operations handler (token/role/group and,
// when a member subsystem is wired, member/invite routes). It is mounted
// cookie-gated under /admin/ on the console HTTP listener. Pass a nil member
// store to omit the member/invite routes.
func NewAdminHandler(v *Console, m *members.Store, i *invites.Store, mailer Mailer, conn InviteConnInfo, ttl time.Duration) http.Handler {
	var ms *memberSubsystem
	if m != nil {
		ms = &memberSubsystem{members: m, invites: i, mailer: mailer, conn: conn, ttl: ttl}
	}
	return buildAdminMux(v, ms)
}

// buildAdminMux wires the admin route table. Exposed for tests.
// Daemon lifecycle (start/stop/restart) is owned by the OS service manager
// (systemd / launchd) and is intentionally not exposed over the admin socket.
//
// ms may be nil: the member/invite routes are registered only when a member
// subsystem is wired (production via NewMemberAdminFactory). token/role/group
// routes are always served.
func buildAdminMux(v *Console, ms *memberSubsystem) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /tokens", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tokens": v.Tokens().ListTokens()})
	})
	mux.HandleFunc("GET /roles", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"roles": v.Tokens().ListRoles()})
	})

	mux.HandleFunc("POST /tokens", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			User        string `json:"user"`
			Role        string `json:"role"`
			ExpiresDays *int   `json:"expires_days"`
			Actor       string `json:"actor"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.User == "" || body.Role == "" {
			writeError(w, http.StatusBadRequest, "Missing required fields: user, role")
			return
		}
		tok, err := v.Tokens().AddToken(body.User, body.Role, body.ExpiresDays)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditAdmin(v, "admin.token.issue", body.Actor, body.User)
		writeJSON(w, http.StatusCreated, tokenJSON(tok))
	})

	mux.HandleFunc("POST /tokens/{user}/rotate", func(w http.ResponseWriter, r *http.Request) {
		user := r.PathValue("user")
		tok, err := v.Tokens().RotateToken(user)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditAdmin(v, "admin.token.rotate", r.URL.Query().Get("actor"), user)
		writeJSON(w, http.StatusOK, tokenJSON(tok))
	})

	mux.HandleFunc("POST /tokens/_rotate_all", func(w http.ResponseWriter, r *http.Request) {
		toks, err := v.Tokens().RotateAllTokens()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		entries := make([]map[string]string, 0, len(toks))
		for _, t := range toks {
			entries = append(entries, map[string]string{
				"user": t.User, "token": t.Token, "role": t.Role,
			})
		}
		auditAdmin(v, "admin.token.rotate_all", r.URL.Query().Get("actor"), fmt.Sprintf("%d tokens", len(toks)))
		writeJSON(w, http.StatusOK, map[string]any{
			"rotated": len(toks),
			"tokens":  entries,
		})
	})

	mux.HandleFunc("DELETE /tokens/{user}", func(w http.ResponseWriter, r *http.Request) {
		user := r.PathValue("user")
		revoked, verr := v.Tokens().RevokeToken(user)
		if verr != nil {
			// The token exists but the revocation did not commit — it is
			// still live. Say so; a 404 here would read as "nothing to do".
			writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to revoke token for '%s' (still live, retry): %v", user, verr))
			return
		}
		if revoked {
			// Token gone = identity gone: drop group memberships in the
			// same flow so the two YAML stores cannot drift (plan §6-D2).
			// With a member registry wired, memberships are keyed by the
			// member UUID — resolve the token email to it; a user with no
			// member row keeps the raw key, which holds no memberships on
			// this branch, so the cascade removes nothing.
			key, ok := memberPersonKey(ms, user)
			if !ok {
				key = user
			}
			removed, rerr := v.Groups().RemoveUser(key)
			auditAdmin(v, "admin.token.revoke", r.URL.Query().Get("actor"), user)
			msg := fmt.Sprintf("Revoked token for '%s'", user)
			if rerr != nil {
				// The token is already gone; the membership cascade did not
				// commit (write-through refusal, memberships unchanged).
				// Surface the partial state instead of hiding it.
				writeError(w, http.StatusInternalServerError,
					fmt.Sprintf("%s but failed to remove its group memberships: %v", msg, rerr))
				return
			}
			if removed > 0 {
				msg = fmt.Sprintf("%s and removed %d group membership(s) for person key '%s'", msg, removed, key)
			}
			writeJSON(w, http.StatusOK, map[string]string{"message": msg})
			return
		}
		writeError(w, http.StatusNotFound, fmt.Sprintf("No token found for user '%s'", user))
	})

	mux.HandleFunc("POST /roles", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name      string   `json:"name"`
			Scope     []string `json:"scope"`
			TopK      *int     `json:"top_k"`
			RateLimit string   `json:"rate_limit"`
			Actor     string   `json:"actor"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Name == "" || len(body.Scope) == 0 || body.TopK == nil || body.RateLimit == "" {
			writeError(w, http.StatusBadRequest, "Missing required fields: name, scope, top_k, rate_limit")
			return
		}
		role, err := v.Tokens().AddRole(body.Name, body.Scope, *body.TopK, body.RateLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditAdmin(v, "admin.role.create", body.Actor, role.Name)
		writeJSON(w, http.StatusCreated, roleJSON(role))
	})

	mux.HandleFunc("PUT /roles/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var raw map[string]json.RawMessage
		if err := readJSON(r, &raw); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		opts := tokens.UpdateRoleOpts{}
		if v, ok := raw["scope"]; ok {
			var s []string
			if err := json.Unmarshal(v, &s); err != nil {
				writeError(w, http.StatusBadRequest, "scope must be a string array")
				return
			}
			opts.Scope = &s
		}
		if v, ok := raw["top_k"]; ok {
			var n int
			if err := json.Unmarshal(v, &n); err != nil {
				writeError(w, http.StatusBadRequest, "top_k must be an integer")
				return
			}
			opts.TopK = &n
		}
		if v, ok := raw["rate_limit"]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				writeError(w, http.StatusBadRequest, "rate_limit must be a string")
				return
			}
			opts.RateLimit = &s
		}
		if opts.Scope == nil && opts.TopK == nil && opts.RateLimit == nil {
			writeError(w, http.StatusBadRequest, "No fields to update")
			return
		}
		role, err := v.Tokens().UpdateRole(name, opts)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		actor := ""
		if av, ok := raw["actor"]; ok {
			_ = json.Unmarshal(av, &actor)
		}
		auditAdmin(v, "admin.role.update", actor, name)
		writeJSON(w, http.StatusOK, roleJSON(role))
	})

	mux.HandleFunc("DELETE /roles/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if err := v.Tokens().DeleteRole(name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditAdmin(v, "admin.role.delete", r.URL.Query().Get("actor"), name)
		writeJSON(w, http.StatusOK, map[string]string{
			"message": fmt.Sprintf("Deleted role '%s'", name),
		})
	})

	// ── group RBAC routes ──────────────────────────────────────────────
	// Plan §6-D8 layer 1: the local admin socket is an operator surface —
	// full power plus audit. The §5 grant judge (groups.CanGrant) is NOT
	// enforced here because a socket connection carries no identity; every
	// mutation records "local-admin:<actor>" instead. The judge is
	// enforced where authenticated identities exist (M2b RPC layer).

	mux.HandleFunc("GET /groups", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"groups": v.Groups().ListGroups()})
	})

	mux.HandleFunc("GET /memberships", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"memberships": v.Groups().ListMemberships()})
	})

	mux.HandleFunc("POST /groups", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name   string `json:"name"`
			Parent string `json:"parent"`
			Actor  string `json:"actor"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "Missing required field: name")
			return
		}
		g, err := v.Groups().CreateGroup(body.Name, body.Parent)
		if err != nil {
			writeGroupError(w, err)
			return
		}
		auditAdmin(v, "admin.group.create", body.Actor, g.Name)
		writeJSON(w, http.StatusCreated, g)
	})

	mux.HandleFunc("PUT /groups/{ref}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name  string `json:"name"`
			Actor string `json:"actor"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "Missing required field: name")
			return
		}
		g, err := v.Groups().RenameGroup(r.PathValue("ref"), body.Name)
		if err != nil {
			writeGroupError(w, err)
			return
		}
		auditAdmin(v, "admin.group.rename", body.Actor, fmt.Sprintf("%s → %s", r.PathValue("ref"), g.Name))
		writeJSON(w, http.StatusOK, g)
	})

	mux.HandleFunc("DELETE /groups/{ref}", func(w http.ResponseWriter, r *http.Request) {
		actor := r.URL.Query().Get("actor")
		g, err := v.Groups().DeleteGroup(r.PathValue("ref"), v.TagStats())
		if err != nil {
			writeGroupError(w, err)
			return
		}
		// Best-effort: sweep the dead group's tag off remaining multi-tag
		// items. The group is already gone, so cleanup outcomes are reported,
		// never turned into a delete failure.
		cleanup := v.PurgeGroupTag(r.Context(), g.ID, actor)
		// Name + immutable id: the group is gone and its name may be reused,
		// so the id is what disambiguates the entry later.
		auditAdmin(v, "admin.group.delete", actor, fmt.Sprintf("%s (%s)", g.Name, g.ID))
		writeJSON(w, http.StatusOK, map[string]string{
			"message":     fmt.Sprintf("Deleted group '%s' (%s)", g.Name, g.ID),
			"tag_cleanup": cleanup,
		})
	})

	mux.HandleFunc("POST /groups/{ref}/grant", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			User  string `json:"user"`
			Role  string `json:"role"`
			Actor string `json:"actor"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.User == "" || body.Role == "" {
			writeError(w, http.StatusBadRequest, "Missing required fields: user, role")
			return
		}
		role, err := groups.ParseRole(body.Role)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// The admin surface speaks emails (the human identity); memberships
		// are keyed by the member UUID, resolved here. A member registry
		// makes "grants exist only for registered members" enforceable: an
		// unregistered email is refused, never written.
		key, ok := memberPersonKey(ms, body.User)
		if !ok {
			writeError(w, http.StatusNotFound,
				fmt.Sprintf("no member registered for email '%s': grants require a registered member", body.User))
			return
		}
		m, err := v.Groups().Grant(key, r.PathValue("ref"), role, localAdminActor(body.Actor))
		if err != nil {
			writeGroupError(w, err)
			return
		}
		// TODO(member-invite): auto-trigger invite after register+grant completes (spec §11.1 #13/#18)
		auditAdmin(v, "admin.group.grant", body.Actor, fmt.Sprintf("%s @ %s (%s)", body.User, r.PathValue("ref"), body.Role))
		writeJSON(w, http.StatusCreated, m)
	})

	mux.HandleFunc("POST /groups/{ref}/revoke", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			User  string `json:"user"`
			Actor string `json:"actor"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.User == "" {
			writeError(w, http.StatusBadRequest, "Missing required field: user")
			return
		}
		// Same email → member-UUID resolution as the grant route: an
		// unregistered email can hold no membership to revoke.
		key, resolved := memberPersonKey(ms, body.User)
		if !resolved {
			writeError(w, http.StatusNotFound,
				fmt.Sprintf("no member registered for email '%s': grants require a registered member", body.User))
			return
		}
		ok, err := v.Groups().Revoke(key, r.PathValue("ref"))
		if err != nil {
			writeGroupError(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound,
				fmt.Sprintf("No membership for user '%s' on group '%s'", body.User, r.PathValue("ref")))
			return
		}
		auditAdmin(v, "admin.group.revoke", body.Actor, fmt.Sprintf("%s @ %s", body.User, r.PathValue("ref")))
		writeJSON(w, http.StatusOK, map[string]string{
			"message": fmt.Sprintf("Revoked membership of '%s' on group '%s'", body.User, r.PathValue("ref")),
		})
	})

	// ── member registry + invite routes (optional subsystem) ───────────
	if ms != nil {
		registerMemberRoutes(mux, v, ms)
	}

	// 404 fallback for routes that didn't match.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("No route for %s %s", r.Method, r.URL.Path))
	})

	return mux
}

// memberPersonKey resolves an admin-surface user email to the groups person
// key. With a member subsystem wired (this branch's daemon always wires one,
// same plumbing as registerMemberRoutes) memberships are keyed by the
// immutable member UUID, so the email must resolve to a registered member
// row; ok=false reports that no row exists. Without a subsystem the email
// itself is the key (core behavior, default email validator).
func memberPersonKey(ms *memberSubsystem, email string) (string, bool) {
	if ms == nil {
		return email, true
	}
	m, err := ms.members.GetByEmail(email)
	if err != nil {
		return "", false
	}
	return m.ID, true
}

// localAdminActor builds the audit identity for admin-socket mutations
// (plan §6-D8: socket access is not an identity, so mutations record the
// operator-declared --actor value under the "local-admin:" prefix).
func localAdminActor(actor string) string {
	if actor == "" {
		actor = "unknown"
	}
	return "local-admin:" + actor
}

// auditAdmin emits an audit entry for a mutation performed over the admin
// socket (methods admin.token.*, admin.group.*, admin.member.*,
// admin.invite.*). target names the entity acted ON — the token's user, the
// group, the "user @ group" membership — the subject an auditor asks about
// first, while the UserID column stays "who acted".
func auditAdmin(v *Console, method, actor, target string) {
	if v.audit == nil || !v.audit.Enabled() {
		return
	}
	v.audit.Log(AuditEntry{
		Timestamp: nowUTCISO(),
		UserID:    localAdminActor(actor),
		Method:    method,
		Status:    "success",
		SourceIP:  "admin-uds",
		Target:    target,
	})
}

// writeGroupError maps groups package errors onto admin HTTP statuses:
// unknown group → 404, blocked delete / duplicate name → 409, rest → 400.
func writeGroupError(w http.ResponseWriter, err error) {
	switch {
	case errors.As(err, new(groups.ErrGroupNotFound)):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.As(err, new(groups.ErrDuplicateName)),
		errors.As(err, new(groups.ErrHasChildren)),
		errors.As(err, new(groups.ErrHasMembers)),
		errors.As(err, new(groups.ErrSoleTagRecords)),
		errors.As(err, new(groups.ErrTagStatsUnavailable)):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func tokenJSON(t *tokens.Token) map[string]any {
	exp := t.Expires
	if exp == "" {
		exp = "never"
	}
	return map[string]any{
		"user":      t.User,
		"token":     t.Token,
		"role":      t.Role,
		"issued_at": t.IssuedAt,
		"expires":   exp,
	}
}

func roleJSON(r *tokens.Role) map[string]any {
	return map[string]any{
		"name":       r.Name,
		"scope":      r.Scope,
		"top_k":      r.TopK,
		"rate_limit": r.RateLimit,
	}
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

// SocketURL is a stable host used in the URL for UDS HTTP. Clients
// substitute the actual socket file via http.Transport.DialContext.
const SocketURL = "http://admin"

// SanitizePathForLog hides socket directories that contain user names or
// secret prefixes. Used by status reporting.
func SanitizePathForLog(p string) string {
	if p == "" {
		return ""
	}
	return strings.TrimSuffix(p, "/")
}
