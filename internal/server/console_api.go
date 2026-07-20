package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
)

// consoleInviteTokenRole is the role label recorded on invites issued through
// the console domain API and echoed back by LookupWrap. Tokens are pure
// identity (authorization is the group RBAC judge); this is a legacy invite
// field kept at the "member" default. Per-team GROUP roles live on memberships.
const consoleInviteTokenRole = "member"

// actorCtxKey carries the authenticated console operator's email (parsed from
// the rc_session principal by the console middleware) into the domain handlers,
// where it becomes the audit actor. The console package sets it via WithActor
// after requireSession; server cannot import console (console imports server),
// so the key and its accessors live here.
type actorCtxKey struct{}

// WithActor returns ctx carrying the authenticated operator email. The console
// BFF calls this after validating rc_session so domain mutations audit the real
// principal instead of an anonymous local-admin actor.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, actor)
}

// actorFromContext returns the operator email set by WithActor, or "" when the
// request was not tagged (e.g. a test hitting the handler directly). "" flows
// through localAdminActor as "unknown", so audit never crashes on an untagged
// request.
func actorFromContext(ctx context.Context) string {
	s, _ := ctx.Value(actorCtxKey{}).(string)
	return s
}

// cloudCookieCtxKey carries the operator's runespace-cloud session cookie
// ("name=value") into the admin/domain handlers so the cloud-relay invite mailer
// can authenticate the send as the logged-in operator. The console BFF sets it
// via WithCloudCookie after requireSession; server cannot import console, so the
// key and its accessors live here alongside the actor plumbing.
type cloudCookieCtxKey struct{}

// WithCloudCookie returns ctx carrying the operator's cloud session cookie. The
// console BFF calls this after validating rc_session so the invite mailer relays
// through the cloud public API as the operator.
func WithCloudCookie(ctx context.Context, cookie string) context.Context {
	return context.WithValue(ctx, cloudCookieCtxKey{}, cookie)
}

// cloudCookieFromContext returns the cloud session cookie set by WithCloudCookie,
// or "" when the request was not tagged (the cloud mailer then refuses to send).
func cloudCookieFromContext(ctx context.Context) string {
	s, _ := ctx.Value(cloudCookieCtxKey{}).(string)
	return s
}

// consoleAPI holds the collaborators the /api/v1 domain handlers share. It
// reuses the same stores as the /admin surface (v.Groups()/v.Tokens() and the
// member subsystem) so both surfaces converge on one set of RBAC state.
type consoleAPI struct {
	v  *Console
	ms *memberSubsystem
}

// NewConsoleAPIHandler builds the /api/v1 domain surface (teams, team members,
// users, memberships, invitations) described by the console API design doc. It
// is mounted (origin + session gated, prefix-stripped) at /api/v1/ on the
// console listener by the daemon; the caller wraps it so the paths this handler
// sees are already stripped of the /api/v1 prefix. A nil member store omits the
// member-backed routes (users/memberships/invitations), leaving only teams.
func NewConsoleAPIHandler(v *Console, m *members.Store, i *invites.Store, mailer Mailer, conn InviteConnInfo, ttl time.Duration) http.Handler {
	var ms *memberSubsystem
	if m != nil {
		ms = &memberSubsystem{members: m, invites: i, mailer: mailer, conn: conn, ttl: ttl}
	}
	h := &consoleAPI{v: v, ms: ms}
	return h.mux()
}

func (h *consoleAPI) mux() http.Handler {
	mux := http.NewServeMux()

	// ── teams ──────────────────────────────────────────────────────────
	mux.HandleFunc("GET /teams/tree", h.teamsTree)
	mux.HandleFunc("POST /teams", h.createTeam)
	mux.HandleFunc("GET /teams/{id}", h.teamDetail)
	mux.HandleFunc("PUT /teams/{id}", h.renameTeam)
	mux.HandleFunc("DELETE /teams/{id}", h.deleteTeam)

	// ── team members (team-screen axis) ────────────────────────────────
	mux.HandleFunc("GET /teams/{id}/members", h.teamMembers)
	mux.HandleFunc("POST /teams/{id}/members", h.addTeamMember)
	mux.HandleFunc("PUT /teams/{id}/members/roles", h.teamRolesBatch)
	mux.HandleFunc("DELETE /teams/{id}/members", h.teamMembersRemoveBatch)

	if h.ms != nil {
		// ── users (global) ─────────────────────────────────────────────
		mux.HandleFunc("GET /users/stats", h.userStats)
		mux.HandleFunc("GET /users", h.usersList)
		mux.HandleFunc("DELETE /users", h.usersDeleteBatch)
		mux.HandleFunc("GET /users/{id}", h.userDetail)
		mux.HandleFunc("DELETE /users/{id}/session", h.userSessionDeactivate)

		// ── user memberships (user-drawer axis) ────────────────────────
		mux.HandleFunc("POST /users/{id}/members/roles", h.userAddTeam)
		mux.HandleFunc("PUT /users/{id}/members/roles", h.userRolesBatch)
		mux.HandleFunc("DELETE /users/{id}/members/roles", h.userMembershipsRemoveBatch)

		// ── invitations ────────────────────────────────────────────────
		mux.HandleFunc("POST /invitations", h.createInvitation)
		mux.HandleFunc("POST /invitations/resend", h.resendInvitation)
		mux.HandleFunc("POST /invitations/cancel", h.cancelInvitation)
		mux.HandleFunc("GET /invitations", h.invitationsHistory)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		apiErr(w, http.StatusNotFound, "NOT_FOUND", "no route for "+r.Method+" "+r.URL.Path)
	})
	return mux
}

// ── shared response helpers (design-doc contract) ──────────────────────

// wireTime renders a stored timestamp for the console SPA: parsed as RFC3339
// and re-rendered at SECOND precision, UTC ("2026-07-07T08:12:00Z") — the
// exact shape the console API design doc specifies for every timestamp
// field. Storage is canonical millisecond RFC3339 (storedb.TimeFormat, kept
// for operator auditability), so this truncation is applied at EVERY console
// DTO emission whose value originates from a store timestamp; the doc'd wire
// contract must not widen because storage did. Unparseable values pass
// through unchanged — deliberately: the session-expired fallback emits the
// token's DATE-ONLY expiry ("2026-12-31"), which predates this helper and
// must keep its shape — and "" passes through as "".
//
// The /admin surface (admin.go, admin_members.go) deliberately does NOT use
// this helper: it is the operator audit surface and emits the raw stored
// values, millisecond precision included.
func wireTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// wireTimePtr is wireTime for the nullable DTO fields: "" (the stores'
// empty-value convention) becomes nil so the field serializes as JSON null,
// anything else is truncated like wireTime.
func wireTimePtr(s string) *string {
	if s == "" {
		return nil
	}
	v := wireTime(s)
	return &v
}

// apiErr writes the doc's common error body {code, message}. Distinct from
// admin.go's writeError ({error}) — the console SPA branches on `code`.
func apiErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"code": code, "message": msg})
}

// pageEnvelope is the list response shape {total, page, size, items}.
type pageEnvelope struct {
	Total int `json:"total"`
	Page  int `json:"page"`
	Size  int `json:"size"`
	Items any `json:"items"`
}

// batchResult is the partial-failure report for batch endpoints. Both slices
// are non-nil so they marshal as [] not null.
type batchResult struct {
	Succeeded []string       `json:"succeeded"`
	Failed    []batchFailure `json:"failed"`
}

type batchFailure struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newBatchResult() *batchResult {
	return &batchResult{Succeeded: []string{}, Failed: []batchFailure{}}
}

func (b *batchResult) ok(id string) { b.Succeeded = append(b.Succeeded, id) }
func (b *batchResult) fail(id, code, msg string) {
	b.Failed = append(b.Failed, batchFailure{ID: id, Code: code, Message: msg})
}

// parsePaging reads ?page (1-based, default 1) and ?size (default 10, max 100).
// Out-of-range size is a 400; a page past the end is NOT an error (the slice
// helper returns empty items).
func parsePaging(r *http.Request) (page, size int, err error) {
	page, size = 1, 10
	if v := r.URL.Query().Get("page"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 {
			return 0, 0, errors.New("page must be a positive integer")
		}
		page = n
	}
	if v := r.URL.Query().Get("size"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 100 {
			return 0, 0, errors.New("size must be between 1 and 100")
		}
		size = n
	}
	return page, size, nil
}

// pageSlice returns the page-th window of items (1-based) and the total. A page
// past the end yields an empty (non-nil) slice, per the doc's paging bounds.
func pageSlice[T any](items []T, page, size int) ([]T, int) {
	total := len(items)
	// Guard the multiply against overflow: a pathological page (e.g. 1e18)
	// would wrap (page-1)*size to a negative start and panic on the slice.
	// page > total is past the end for any size >= 1, so return empty first.
	if page > total {
		return []T{}, total
	}
	start := (page - 1) * size
	if start >= total {
		return []T{}, total
	}
	end := start + size
	if end > total {
		end = total
	}
	return items[start:end], total
}

// commaList splits a comma-separated query value (e.g. ?userIds=a,b,c),
// trimming blanks. Returns nil when the param is absent or empty.
func commaList(r *http.Request, key string) []string {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// writeGroupAPIErr maps groups-package errors onto the doc's error codes.
func writeGroupAPIErr(w http.ResponseWriter, err error) {
	switch {
	case errors.As(err, new(groups.ErrGroupNotFound)):
		apiErr(w, http.StatusNotFound, "TEAM_NOT_FOUND", err.Error())
	case errors.As(err, new(groups.ErrDuplicateName)):
		apiErr(w, http.StatusConflict, "TEAM_NAME_DUPLICATE", err.Error())
	case errors.As(err, new(groups.ErrAmbiguousName)):
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.As(err, new(groups.ErrHasChildren)):
		apiErr(w, http.StatusConflict, "TEAM_HAS_CHILDREN", err.Error())
	default:
		apiErr(w, http.StatusBadRequest, "TEAM_NAME_INVALID", err.Error())
	}
}

// ── teams ───────────────────────────────────────────────────────────────

func (h *consoleAPI) teamsTree(w http.ResponseWriter, r *http.Request) {
	// Flat node array; the client assembles the tree. Empty => [].
	writeJSON(w, http.StatusOK, h.v.Groups().ConsoleTree())
}

func (h *consoleAPI) teamDetail(w http.ResponseWriter, r *http.Request) {
	d, err := h.v.Groups().TeamDetail(r.PathValue("id"))
	if err != nil {
		writeGroupAPIErr(w, err)
		return
	}
	// createdAt crosses the wire boundary: second precision per the doc
	// (storage is canonical millisecond RFC3339).
	d.CreatedAt = wireTime(d.CreatedAt)
	writeJSON(w, http.StatusOK, d)
}

func (h *consoleAPI) createTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		ParentID string `json:"parentId"`
	}
	if err := readJSON(r, &body); err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		apiErr(w, http.StatusBadRequest, "TEAM_NAME_INVALID", "name is required")
		return
	}
	g, err := h.v.Groups().CreateGroup(body.Name, body.ParentID)
	if err != nil {
		writeGroupAPIErr(w, err)
		return
	}
	auditAdmin(h.v, "admin.group.create", actorFromContext(r.Context()), g.Name)
	d, derr := h.v.Groups().TeamDetail(g.ID)
	if derr != nil {
		// The team was created; only the read-back for the response body
		// failed (unexpected — same store, same lock). Log and return the
		// bare group so the 201 is not lost.
		slog.Warn("console: team created but detail read-back failed", "team", g.ID, "err", derr)
	}
	if d.Children == nil {
		d.Children = []string{} // children is string[] on the wire, never null
	}
	d.CreatedAt = wireTime(d.CreatedAt) // wire boundary: second precision per the doc
	writeJSON(w, http.StatusCreated, d)
}

func (h *consoleAPI) renameTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		apiErr(w, http.StatusBadRequest, "TEAM_NAME_INVALID", "name is required")
		return
	}
	g, err := h.v.Groups().RenameGroup(r.PathValue("id"), body.Name)
	if err != nil {
		writeGroupAPIErr(w, err)
		return
	}
	auditAdmin(h.v, "admin.group.rename", actorFromContext(r.Context()), g.Name)
	d, derr := h.v.Groups().TeamDetail(g.ID)
	if derr != nil {
		slog.Warn("console: team renamed but detail read-back failed", "team", g.ID, "err", derr)
	}
	if d.Children == nil {
		d.Children = []string{} // children is string[] on the wire, never null
	}
	d.CreatedAt = wireTime(d.CreatedAt) // wire boundary: second precision per the doc
	writeJSON(w, http.StatusOK, d)
}

// ── team members (team-screen axis) ─────────────────────────────────────

type memberDTO struct {
	UserID           string `json:"userId"`
	Account          string `json:"account"`
	Username         string `json:"username"`
	Role             string `json:"role"`
	InvitationStatus string `json:"invitationStatus"`
	SessionStatus    string `json:"sessionStatus"`
	// JoinedAt is the membership's granted_at. It is a pointer because an
	// inherited-read member has no stored row and thus no join timestamp, so
	// that row serializes joinedAt as null; a direct member always carries a
	// real value. (Same nullable-timestamp convention as userDTO's stamps.)
	JoinedAt *string `json:"joinedAt"`
}

func (h *consoleAPI) teamMembers(w http.ResponseWriter, r *http.Request) {
	page, size, err := parsePaging(r)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ref := r.PathValue("id")
	// Resolve the team first so an unknown team is a 404 (not an empty page).
	gid, gerr := h.groupID(ref)
	if gerr != nil {
		writeGroupAPIErr(w, gerr)
		return
	}
	idx := h.newIndex()
	items := make([]memberDTO, 0)
	// Direct members: the stored (user, group, role) rows on this team.
	for _, m := range h.v.Groups().ListMemberships() {
		if m.GroupID != gid {
			continue
		}
		items = append(items, idx.memberDTO(m))
	}
	// Inherited-read members: users who reach this team only by downward
	// inheritance from an ancestor membership (no stored row on this team).
	// Groups().Inheritors computes this in the store (one ancestor-chain walk)
	// and already excludes anyone direct on this team, so the two sets never
	// overlap. Listed alongside direct members without distinction (API design
	// decision), rendered as read with a null joinedAt.
	for _, uid := range h.v.Groups().Inheritors(gid) {
		items = append(items, idx.inheritedMemberDTO(uid))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Account < items[j].Account })
	pageItems, total := pageSlice(items, page, size)
	writeJSON(w, http.StatusOK, pageEnvelope{Total: total, Page: page, Size: size, Items: pageItems})
}

func (h *consoleAPI) addTeamMember(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Account  string `json:"account"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := readJSON(r, &body); err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	role, perr := groups.ParseRole(body.Role)
	if perr != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", perr.Error())
		return
	}
	ref := r.PathValue("id")
	if _, err := h.groupID(ref); err != nil {
		writeGroupAPIErr(w, err)
		return
	}
	// The org admin (Owner) may hold a normal team membership like anyone else:
	// admin-ness is an independent axis (IsOrgAdmin governs grant authority), not
	// a bar to membership. An unknown account is created on the spot (mirroring
	// the invite flow), stamping the supplied username as its display name — the
	// team screen's "add member" is a create-or-attach, not existing-only.
	m, err := h.ms.members.GetByEmail(body.Account)
	newMember := false
	if err != nil {
		created, aerr := h.ms.members.Add(body.Account, body.Username)
		if aerr != nil {
			// GetByEmail missed, so a failed Add means a malformed account
			// (ErrInvalidEmail) — report it in the console error shape.
			apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", aerr.Error())
			return
		}
		m, newMember = created, true
	}
	if _, exists, _ := h.v.Groups().DirectRole(m.ID, ref); exists {
		apiErr(w, http.StatusConflict, "ALREADY_TEAM_MEMBER", "user is already a member of this team")
		return
	}
	mem, err := h.v.Groups().Grant(m.ID, ref, role, localAdminActor(actorFromContext(r.Context())))
	if err != nil {
		if newMember {
			_ = h.ms.members.Remove(m.ID)
		}
		writeGroupAPIErr(w, err)
		return
	}
	// Per-target-status judgment (doc §team-members): a member who holds no
	// valid session token (session-expired / not-yet-connected) gets a fresh
	// reconnect code mailed; an online member (token present) needs none. A
	// mail failure rolls the just-granted membership back and reports 502.
	if h.needsCode(m) {
		if ierr := h.issueCode(r, m); ierr != nil {
			_, _ = h.v.Groups().RevokeDirectGrant(m.ID, ref)
			if newMember {
				_ = h.ms.members.Remove(m.ID)
			}
			slog.Error("console: invite code send failed on add-member (membership rolled back)",
				"account", body.Account, "team", ref, "err", ierr)
			apiErr(w, http.StatusBadGateway, "MAIL_UPSTREAM_ERROR", "failed to send the invitation code email")
			return
		}
	}
	auditAdmin(h.v, "admin.group.grant", actorFromContext(r.Context()), body.Account+" @ "+ref+" ("+body.Role+")")
	writeJSON(w, http.StatusCreated, h.newIndex().memberDTOFrom(mem))
}

func (h *consoleAPI) teamRolesBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Updates []struct {
			UserID string `json:"userId"`
			Role   string `json:"role"`
		} `json:"updates"`
	}
	if err := readJSON(r, &body); err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ref := r.PathValue("id")
	gid, err := h.groupID(ref)
	if err != nil {
		writeGroupAPIErr(w, err)
		return
	}
	// Pre-validate every role: a malformed role is a request-format error, so
	// the whole request is rejected (400) before any mutation — the batch
	// failed[] enum stays USER_NOT_FOUND | NOT_TEAM_MEMBER per the doc.
	for _, u := range body.Updates {
		if _, perr := groups.ParseRole(u.Role); perr != nil {
			apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", perr.Error())
			return
		}
	}
	res := newBatchResult()
	actor := localAdminActor(actorFromContext(r.Context()))
	for _, u := range body.Updates {
		role, _ := groups.ParseRole(u.Role) // validated above
		if _, err := h.ms.members.Get(u.UserID); err != nil {
			res.fail(u.UserID, "USER_NOT_FOUND", "no such user")
			continue
		}
		// Editable when the user holds the team directly (role change) or inherits
		// read on it (first-time grant that creates the direct row) — the same rule
		// the user drawer's userRolesBatch applies, so a row the team member list
		// now surfaces as inherited can actually be promoted here. Checked per
		// user (cheap for a small batch, and always current) rather than
		// snapshotting every user's access.
		if _, exists, _ := h.v.Groups().DirectRole(u.UserID, ref); !exists && !h.v.Groups().InheritsRead(u.UserID, gid) {
			res.fail(u.UserID, "NOT_TEAM_MEMBER", "user is not a member of this team")
			continue
		}
		if _, err := h.v.Groups().Grant(u.UserID, ref, role, actor); err != nil {
			// Membership/team vanished between the check and the re-grant (race).
			slog.Warn("console: role change failed mid-batch",
				"team", ref, "userId", u.UserID, "err", err)
			res.fail(u.UserID, "NOT_TEAM_MEMBER", "user is not a member of this team")
			continue
		}
		res.ok(u.UserID)
	}
	auditAdmin(h.v, "admin.group.grant", actorFromContext(r.Context()), "batch role change @ "+ref)
	writeJSON(w, http.StatusOK, res)
}

func (h *consoleAPI) teamMembersRemoveBatch(w http.ResponseWriter, r *http.Request) {
	userIDs := commaList(r, "userIds")
	if len(userIDs) == 0 {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "userIds query parameter is required")
		return
	}
	ref := r.PathValue("id")
	if _, err := h.groupID(ref); err != nil {
		writeGroupAPIErr(w, err)
		return
	}
	res := newBatchResult()
	// Removing a member from a team must cut their ACCESS to it: Groups().Revoke
	// drops the direct grant AND cuts any read they would still inherit from an
	// ancestor, in one transaction. (The plain RevokeDirectGrant would leave that
	// inherited read intact, so the team would come straight back as inherited
	// read with its memory still readable.) The actor tags who cut it.
	actor := localAdminActor(actorFromContext(r.Context()))
	for _, uid := range userIDs {
		if _, err := h.ms.members.Get(uid); err != nil {
			res.fail(uid, "USER_NOT_FOUND", "no such user")
			continue
		}
		revoked, excluded, err := h.v.Groups().Revoke(uid, ref, actor)
		if err != nil {
			var notFound groups.ErrGroupNotFound
			if errors.As(err, &notFound) {
				// Resolved up front, so this means the team vanished mid-batch
				// (concurrent delete/rename) — not the user's membership state.
				res.fail(uid, "TEAM_NOT_FOUND", "team not found")
				continue
			}
			// The removal did not commit: the membership row is unchanged, so the
			// member STILL has access. Reporting NOT_TEAM_MEMBER would read as
			// "already not a member" and the operator would not retry while access
			// persists — surface it as retryable instead (mirrors
			// userMembershipsRemoveBatch).
			slog.Warn("console: team-member removal did not commit",
				"team", ref, "userId", uid, "err", err)
			res.fail(uid, "INTERNAL", "removal did not commit; retry")
			continue
		}
		// Neither a direct grant to drop nor an inherited read to cancel: the
		// user had no access to this team to begin with.
		if !revoked && !excluded {
			res.fail(uid, "NOT_TEAM_MEMBER", "user is not a member of this team")
			continue
		}
		res.ok(uid)
	}
	auditAdmin(h.v, "admin.group.revoke", actorFromContext(r.Context()), "batch remove @ "+ref)
	writeJSON(w, http.StatusOK, res)
}

// groupID resolves a team ref (id or name) to its id, returning a groups error
// (mapped to TEAM_NOT_FOUND) when absent.
func (h *consoleAPI) groupID(ref string) (string, error) {
	d, err := h.v.Groups().TeamDetail(ref)
	if err != nil {
		return "", err
	}
	return d.ID, nil
}
