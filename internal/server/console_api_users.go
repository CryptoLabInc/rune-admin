package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
)

// membershipDTO is one (team, role) binding as the console renders it.
type membershipDTO struct {
	TeamID   string `json:"teamId"`
	TeamName string `json:"teamName"`
	Role     string `json:"role"`
}

// userDTO is the global user projection (GET /users, GET /users/{id}). The
// three timestamps are pointers so an inapplicable value serializes as null,
// per the doc (the client composes the per-status time display). lastAccessAt
// is fed by the token store's last_used stamp (persisted asynchronously, so
// it survives a daemon restart) and sessionExpiredAt by explicit deactivation
// or natural token expiry; either is null when its signal is absent.
//
// The struct keeps direct and inherited memberships DISTINCT internally (server
// logic depends on the difference — see the field comments), but the wire form
// flattens them into a single `memberships` array via MarshalJSON. Do not add or
// change json tags here expecting them to reach the client; MarshalJSON is the
// source of truth for the response shape.
type userDTO struct {
	UserID           string `json:"userId"`
	Account          string `json:"account"`
	Username         string `json:"username"`
	InvitationStatus string `json:"invitationStatus"`
	SessionStatus    string `json:"sessionStatus"`
	// Memberships is the user's DIRECT (explicit) memberships — the
	// (user, group, role) rows the console can PUT/DELETE.
	Memberships []membershipDTO `json:"memberships"`
	// InheritedMemberships is derived read access the user holds on descendant
	// groups purely by inheritance (no stored row). Role is always "read".
	// Distinct from Memberships because a role change here is a GRANT that
	// creates a direct membership, not an update of an existing one. Kept
	// separate in the server; merged into `memberships` on the wire, and the
	// GET /users teamId filter matches this set too (see coversTeam).
	InheritedMemberships []membershipDTO `json:"inheritedMemberships"`
	LastAccessAt         *string         `json:"lastAccessAt"`
	LastInvitedAt        *string         `json:"lastInvitedAt"`
	SessionExpiredAt     *string         `json:"sessionExpiredAt"`
}

// MarshalJSON renders the user for the console API. The server tracks direct
// (Memberships) and inherited-read (InheritedMemberships) access separately —
// the distinction drives grant-vs-update decisions and the direct-only teamId
// filter — but the console renders one flat access list, so the wire form emits
// a single `memberships` array (direct entries first, then inherited, each
// already name-sorted by userDTO()) and omits inheritedMemberships entirely.
// This is the ONE boundary where the two sets collapse into one; internal
// callers keep seeing the distinct fields. Direct entries lead so that clients
// keying off memberships[0] (the representative "base" role) still land on a
// real, mutable membership rather than a derived read.
func (u userDTO) MarshalJSON() ([]byte, error) {
	merged := make([]membershipDTO, 0, len(u.Memberships)+len(u.InheritedMemberships))
	merged = append(merged, u.Memberships...)
	merged = append(merged, u.InheritedMemberships...)
	// An explicit wire struct (not an embedded alias) so the response shape is
	// spelled out here and cannot regress: inheritedMemberships simply has no
	// field, so it can never leak back into the payload.
	return json.Marshal(userWire{
		UserID:           u.UserID,
		Account:          u.Account,
		Username:         u.Username,
		InvitationStatus: u.InvitationStatus,
		SessionStatus:    u.SessionStatus,
		Memberships:      merged,
		LastAccessAt:     u.LastAccessAt,
		LastInvitedAt:    u.LastInvitedAt,
		SessionExpiredAt: u.SessionExpiredAt,
	})
}

// userWire is the on-the-wire shape of a user: exactly userDTO with direct and
// inherited memberships already merged into one `memberships` list and no
// separate inherited field. Kept as a named type (rather than an anonymous
// struct) so MarshalJSON reads as a plain field copy.
type userWire struct {
	UserID           string          `json:"userId"`
	Account          string          `json:"account"`
	Username         string          `json:"username"`
	InvitationStatus string          `json:"invitationStatus"`
	SessionStatus    string          `json:"sessionStatus"`
	Memberships      []membershipDTO `json:"memberships"`
	LastAccessAt     *string         `json:"lastAccessAt"`
	LastInvitedAt    *string         `json:"lastInvitedAt"`
	SessionExpiredAt *string         `json:"sessionExpiredAt"`
}

// userIndex is a per-request snapshot of the cross-cutting state the user/member
// projections join over: group names, the member registry, which emails hold a
// token, each user's memberships, and each member's latest invite. Built once
// per request so list endpoints never re-scan the stores per row.
type userIndex struct {
	now               time.Time
	groupNames        map[string]string             // group id -> name
	memberByID        map[string]members.Member     // member UUID -> member
	tokenByEmail      map[string]tokenLive          // email -> its session token liveness
	membershipsByUser map[string][]membershipDTO    // member UUID -> direct memberships
	inheritedByUser   map[string][]membershipDTO    // member UUID -> derived-read memberships
	latestInvite      map[string]invites.InviteView // member UUID -> newest invite
}

// tokenLive is the console-relevant view of a member's session token: whether
// it exists, when it was last used (lastAccessAt), whether the agent has
// self-reported terminal activation (activatedAt — the online gate), whether it
// is past expiry, and its expiry (for the session-expired timestamp on natural
// expiry).
type tokenLive struct {
	lastUsed    string
	activatedAt string
	expires     string
	expired     bool
}

// memberStatuses is the two-axis console status: the invitation-code lifecycle
// and the session-token liveness, derived independently from the member, its
// latest invite, and its token. session_expired no longer exists — a redeemed
// member whose token is gone is {invite_redeemed, offline}.
type memberStatuses struct {
	invitation string // invite_pending | invite_expired | invite_redeemed
	session    string // online | offline
}

func (h *consoleAPI) newIndex() *userIndex {
	idx := &userIndex{
		now:               time.Now().UTC(),
		groupNames:        map[string]string{},
		memberByID:        map[string]members.Member{},
		tokenByEmail:      map[string]tokenLive{},
		membershipsByUser: map[string][]membershipDTO{},
		inheritedByUser:   map[string][]membershipDTO{},
		latestInvite:      map[string]invites.InviteView{},
	}
	for _, g := range h.v.Groups().ListGroups() {
		idx.groupNames[g.ID] = g.Name
	}
	for _, m := range h.ms.members.List() {
		idx.memberByID[m.ID] = m
	}
	for _, t := range h.v.Tokens().ListTokens() {
		idx.tokenByEmail[t.User] = tokenLive{lastUsed: t.LastUsed, activatedAt: t.ActivatedAt, expires: t.Expires, expired: t.Expired}
	}
	// membershipsByUser / inheritedByUser are populated lazily by the two
	// endpoints that project them (user list + detail) via fillGroupAccess,
	// so newIndex's other callers (stats, mutation acknowledgements, team
	// members) skip the per-user membership work entirely.
	// invites.List is newest-first, so the first view seen per member is latest.
	for _, v := range h.ms.invites.List() {
		if _, seen := idx.latestInvite[v.MemberID]; !seen {
			idx.latestInvite[v.MemberID] = v
		}
	}
	return idx
}

// putGroupAccess records one member's direct and inherited memberships on the
// index from a single consistent store snapshot (groups.GroupAccessView), so
// the two sets can never overlap or both drop a team under a concurrent
// grant/revoke. Direct memberships keep their real role; inherited entries are
// always read. Empty sets leave the maps untouched (userDTO normalizes to []).
func (idx *userIndex) putGroupAccess(id string, ga groups.GroupAccessView) {
	if len(ga.Direct) > 0 {
		ms := make([]membershipDTO, 0, len(ga.Direct))
		for _, m := range ga.Direct {
			ms = append(ms, membershipDTO{
				TeamID:   m.GroupID,
				TeamName: idx.groupNames[m.GroupID],
				Role:     string(m.Role),
			})
		}
		idx.membershipsByUser[id] = ms
	}
	if len(ga.Inherited) > 0 {
		ims := make([]membershipDTO, 0, len(ga.Inherited))
		for _, gid := range ga.Inherited {
			ims = append(ims, membershipDTO{
				TeamID:   gid,
				TeamName: idx.groupNames[gid],
				Role:     string(groups.RoleRead),
			})
		}
		idx.inheritedByUser[id] = ims
	}
}

// fillGroupAccess populates every member's direct + inherited memberships for
// the user-list projection, from one org-wide consistent snapshot. Split out
// of newIndex so its other callers (stats, mutation acknowledgements, team
// members) skip this work. Recomputed per request per the plan's no-cache
// constraint; a fast-follow after the SQLite migration can push the inherited
// derivation into the query layer.
func (h *consoleAPI) fillGroupAccess(idx *userIndex) {
	access := h.v.Groups().GroupAccessByUser()
	for id := range idx.memberByID {
		if ga, ok := access[id]; ok {
			idx.putGroupAccess(id, ga)
		}
	}
}

// statuses derives the two console status axes (SC-11 defs, 2026-07-20 split):
//
//	session:    online  = active member, live token, agent self-reported
//	                      activation (ReportActivation); offline otherwise.
//	invitation: invite_pending  = latest code is live (unused, not expired);
//	            invite_expired  = latest code expired/revoked, never redeemed;
//	            invite_redeemed = the latest code was used (no newer live code).
//
// Resend issues a fresh live code, so a redeemed/expired member flips back to
// invite_pending automatically — no special case here.
//
// Step-1 finding (2026-07-20): invites.Store.Unwrap sets the redeemed invite's
// Status to StatusConsumed (internal/invites/store.go:285), which is durable
// before Unwrap returns and is never StatusPending again. Real redemption
// (grpc.go Unwrap) also immediately Activates the member, so a consumed
// invite is only ever "latest" once the member is already active/disabled.
// The live-code check below therefore uses invitePendingLive (Status ==
// StatusPending + not-past-expiry) rather than merely "!inviteExpired", since
// inviteExpired does not treat StatusConsumed as expired — using it alone
// would misclassify a solo-redeemed member (no resend) as invite_pending.
func (idx *userIndex) statuses(m members.Member) memberStatuses {
	session := "offline"
	redeemed := false
	switch m.Status {
	case members.StatusActive:
		redeemed = true
		if tl, ok := idx.tokenByEmail[m.Email]; ok && !tl.expired && tl.activatedAt != "" {
			session = "online"
		}
	case members.StatusDisabled:
		redeemed = true
	}

	inv, ok := idx.latestInvite[m.ID]
	switch {
	case ok && invitePendingLive(inv, idx.now):
		// A live pending code is the most recent invitation event, even for a
		// member who previously redeemed (this is the resend case).
		return memberStatuses{invitation: "invite_pending", session: session}
	case redeemed:
		return memberStatuses{invitation: "invite_redeemed", session: session}
	case ok && idx.inviteExpired(inv):
		return memberStatuses{invitation: "invite_expired", session: session}
	default:
		// Invited with no invite record yet — treat as pending.
		return memberStatuses{invitation: "invite_pending", session: session}
	}
}

func (idx *userIndex) inviteExpired(inv invites.InviteView) bool {
	// An admin-revoked code renders as invite_expired too: the member cannot
	// redeem it, so "awaiting acceptance" (invite_pending) would be a lie and
	// would contradict cancelInvitation's own response. The revoked/expired
	// distinction stays visible on the raw invite status, not this derived enum.
	if inv.Status == invites.StatusExpired || inv.Status == invites.StatusCompromised ||
		inv.Status == invites.StatusRevoked {
		return true
	}
	if inv.Status == invites.StatusPending && inv.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, inv.ExpiresAt); err == nil && idx.now.After(t) {
			return true
		}
	}
	return false
}

func (idx *userIndex) userDTO(m members.Member) userDTO {
	ms := idx.membershipsByUser[m.ID]
	if ms == nil {
		ms = []membershipDTO{}
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].TeamName < ms[j].TeamName })
	ims := idx.inheritedByUser[m.ID]
	if ims == nil {
		ims = []membershipDTO{}
	}
	sort.Slice(ims, func(i, j int) bool { return ims[i].TeamName < ims[j].TeamName })
	// All three timestamps cross the wire boundary here: stored values are
	// canonical millisecond RFC3339, the console contract is second
	// precision, so wireTimePtr truncates at the DTO build (which also keeps
	// the list sort below operating on exactly what the client sees, as it
	// did before storage gained milliseconds).
	var lastInvited *string
	if inv, ok := idx.latestInvite[m.ID]; ok && inv.CreatedAt != "" {
		lastInvited = wireTimePtr(inv.CreatedAt)
	}
	tl, hasToken := idx.tokenByEmail[m.Email]
	var lastAccess *string
	if hasToken && tl.lastUsed != "" {
		lastAccess = wireTimePtr(tl.lastUsed)
	}
	// session-expired timestamp: an explicit deactivation is the authority
	// (the token row is gone); otherwise a natural token expiry surfaces the
	// token's own expiry date (date-only — wireTimePtr passes it through).
	var sessionExpired *string
	if m.SessionExpiredAt != "" {
		sessionExpired = wireTimePtr(m.SessionExpiredAt)
	} else if hasToken && tl.expired && tl.expires != "" && tl.expires != "never" {
		sessionExpired = wireTimePtr(tl.expires)
	}
	st := idx.statuses(m)
	return userDTO{
		UserID:               m.ID,
		Account:              m.Email,
		Username:             m.DisplayName,
		InvitationStatus:     st.invitation,
		SessionStatus:        st.session,
		Memberships:          ms,
		InheritedMemberships: ims,
		LastAccessAt:         lastAccess,
		LastInvitedAt:        lastInvited,
		SessionExpiredAt:     sessionExpired,
	}
}

// memberDTO builds the team-member-table row for a membership.
func (idx *userIndex) memberDTO(m groups.Membership) memberDTO {
	return idx.consoleTeamMemberDTO(groups.ConsoleTeamMember{
		User:          m.User,
		Role:          m.Role,
		SourceGroupID: m.GroupID,
		GrantedAt:     m.GrantedAt,
	})
}

// consoleTeamMemberDTO attaches member identity/status fields to the groups
// store's consistent direct-or-inherited team-member projection.
func (idx *userIndex) consoleTeamMemberDTO(m groups.ConsoleTeamMember) memberDTO {
	account, username := "", ""
	st := memberStatuses{invitation: "invite_pending", session: "offline"}
	if mem, ok := idx.memberByID[m.User]; ok {
		account, username, st = mem.Email, mem.DisplayName, idx.statuses(mem)
	}
	return memberDTO{
		UserID:           m.User,
		Account:          account,
		Username:         username,
		Role:             string(m.Role),
		InvitationStatus: st.invitation,
		SessionStatus:    st.session,
		// GrantedAt is the target membership timestamp for a direct row or the
		// nearest source membership timestamp for an inherited row.
		JoinedAt: wireTime(m.GrantedAt),
	}
}

func (idx *userIndex) memberDTOFrom(m groups.Membership) memberDTO { return idx.memberDTO(m) }

// ── users (global) ──────────────────────────────────────────────────────

func (h *consoleAPI) usersList(w http.ResponseWriter, r *http.Request) {
	page, size, err := parsePaging(r)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	q := r.URL.Query()
	search := strings.ToLower(strings.TrimSpace(q.Get("search")))
	statusFilter := strings.TrimSpace(q.Get("status"))
	teamFilter := strings.TrimSpace(q.Get("teamId")) // team id only; unknown id => empty result, not 404

	// status is an enum (doc §users): reject an out-of-enum value with 400
	// rather than silently returning an empty list (which reads as "no such
	// users" and hides the client bug). Empty = no filter.
	if statusFilter != "" && !validSessionStatus(statusFilter) {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "unknown status filter: "+statusFilter)
		return
	}

	// sort is an enum (doc §users): last_invited (default) | username | account.
	// Reject an out-of-enum value with 400 rather than silently ignoring it (which
	// makes the client's sort dropdown appear to do nothing), mirroring the
	// status-filter check above and the invitationsHistory sort validation.
	sortKey := strings.TrimSpace(q.Get("sort"))
	if sortKey == "" {
		sortKey = "last_invited"
	}
	if sortKey != "last_invited" && sortKey != "username" && sortKey != "account" {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "unknown sort: "+sortKey)
		return
	}

	idx := h.newIndex()
	h.fillGroupAccess(idx)
	items := make([]userDTO, 0, len(idx.memberByID))
	for _, m := range h.ms.members.List() {
		u := idx.userDTO(m)
		if search != "" &&
			!strings.Contains(strings.ToLower(u.Account), search) &&
			!strings.Contains(strings.ToLower(u.Username), search) {
			continue
		}
		if statusFilter != "" && u.SessionStatus != statusFilter {
			continue
		}
		if teamFilter != "" && !u.coversTeam(teamFilter) {
			continue
		}
		items = append(items, u)
	}
	switch sortKey {
	case "username":
		// Display name ascending; account tiebreak so equal/empty names stay
		// deterministic.
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Username != items[j].Username {
				return items[i].Username < items[j].Username
			}
			return items[i].Account < items[j].Account
		})
	case "account":
		// Account ascending (byte order — deterministic and stable).
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Account < items[j].Account
		})
	default:
		// last_invited desc (newest invitation first); nulls last, account tiebreak.
		sort.SliceStable(items, func(i, j int) bool {
			li, lj := "", ""
			if items[i].LastInvitedAt != nil {
				li = *items[i].LastInvitedAt
			}
			if items[j].LastInvitedAt != nil {
				lj = *items[j].LastInvitedAt
			}
			if li != lj {
				return li > lj
			}
			return items[i].Account < items[j].Account
		})
	}
	pageItems, total := pageSlice(items, page, size)
	writeJSON(w, http.StatusOK, pageEnvelope{Total: total, Page: page, Size: size, Items: pageItems})
}

// coversTeam reports whether the user's client-visible access includes the team,
// counting both direct and inherited memberships — the same flattened set the
// wire `memberships` exposes, so the teamId filter and the rendered list agree.
func (u userDTO) coversTeam(teamID string) bool {
	return hasTeam(u.Memberships, teamID) || hasTeam(u.InheritedMemberships, teamID)
}

// hasTeam reports whether any membership is in the given team. The doc's
// teamId filter is an id only, so this matches TeamID exactly — matching the
// team name too would let a name that collides with another team's id leak
// unrelated users into the result.
func hasTeam(ms []membershipDTO, teamID string) bool {
	for _, m := range ms {
		if m.TeamID == teamID {
			return true
		}
	}
	return false
}

// validSessionStatus reports whether s is a session-status value the derivation
// can emit (the values the GET /users status filter accepts). Kept in lockstep
// with statuses().
func validSessionStatus(s string) bool {
	return s == "online" || s == "offline"
}

func (h *consoleAPI) userDetail(w http.ResponseWriter, r *http.Request) {
	m, err := h.ms.members.Get(r.PathValue("id"))
	if err != nil {
		apiErr(w, http.StatusNotFound, "USER_NOT_FOUND", "no such user")
		return
	}
	idx := h.newIndex()
	idx.putGroupAccess(m.ID, h.v.Groups().GroupAccess(m.ID))
	writeJSON(w, http.StatusOK, idx.userDTO(*m))
}

func (h *consoleAPI) usersDeleteBatch(w http.ResponseWriter, r *http.Request) {
	ids := commaList(r, "userIds")
	if len(ids) == 0 {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "userIds query parameter is required")
		return
	}
	res := newBatchResult()
	actor := actorFromContext(r.Context())
	for _, id := range ids {
		m, err := h.ms.members.Get(id)
		if err != nil {
			res.fail(id, "USER_NOT_FOUND", "no such user")
			continue
		}
		// A self-invited org admin follows the same member lifecycle as everyone
		// else. Removing their member UUID and data-plane access does not erase
		// the email-keyed org-admin identity or the separate console session.
		// Cascade: destroy the session token, drop all group memberships, void
		// any unused invite codes, then remove the member — in that order so the
		// gRPC judge never sees a half-deleted identity (mirrors the admin
		// token-revoke cascade). A revocation refusal (write-through commit
		// failure) aborts this user's cascade: proceeding would hard-delete the
		// member while their credential stays live.
		if _, err := h.v.Tokens().RevokeToken(m.Email); err != nil {
			slog.Warn("console: user delete failed mid-batch", "userId", id, "err", err)
			res.fail(id, "USER_NOT_FOUND", "no such user")
			continue
		}
		if _, err := h.v.Groups().RemoveUser(m.ID); err != nil {
			// The membership cascade did not commit (write-through refusal):
			// stop before member removal so the judge never sees a member row
			// gone while its memberships persist. Log the cause; report
			// within the enum.
			slog.Warn("console: user delete failed mid-batch", "userId", id, "err", err)
			res.fail(id, "USER_NOT_FOUND", "no such user")
			continue
		}
		if _, err := h.ms.invites.RevokeAllPendingForMember(m.ID); err != nil {
			// A pending code must not outlive its member: redeeming one later
			// burns the code against a dead member id (safe but noisy, and it
			// strands issuance history pointing at nothing the admin can see).
			// Stop before member removal; re-running the delete converges.
			slog.Warn("console: user delete failed mid-batch", "userId", id, "err", err)
			res.fail(id, "USER_NOT_FOUND", "no such user")
			continue
		}
		if err := h.ms.members.Remove(m.ID); err != nil {
			// Get() succeeded above, so a failed Remove is an unexpected race
			// (deleted in another session). Log the cause; report within enum.
			slog.Warn("console: user delete failed mid-batch", "userId", id, "err", err)
			res.fail(id, "USER_NOT_FOUND", "no such user")
			continue
		}
		auditAdmin(h.v, "admin.member.delete", actor, m.Email)
		res.ok(id)
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *consoleAPI) userSessionDeactivate(w http.ResponseWriter, r *http.Request) {
	m, err := h.ms.members.Get(r.PathValue("id"))
	if err != nil {
		apiErr(w, http.StatusNotFound, "USER_NOT_FOUND", "no such user")
		return
	}
	// Deactivation requires a live session: gating on the derived session
	// status — not mere token-row presence — matters because an
	// invite_pending member's wrapped invite token would otherwise be destroyed
	// by the revoke below, silently invalidating the invitation code that was
	// just mailed.
	if st := h.newIndex().statuses(*m); st.session != "online" {
		apiErr(w, http.StatusConflict, "SESSION_NOT_ACTIVE", "user has no active session")
		return
	}
	revoked, err := h.v.Tokens().RevokeToken(m.Email)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "INTERNAL", "session token revocation did not commit; retry")
		return
	}
	if !revoked {
		apiErr(w, http.StatusConflict, "SESSION_NOT_ACTIVE", "no active session token to destroy")
		return
	}
	// Record the moment the session ended so the console can show it (the token
	// row is now gone, so this is the only place the time survives). A refusal
	// costs only the timestamp — the derived status flips regardless, from the
	// token's absence — but it must not be silent: the drawer's mandated
	// "session expired at" display stays blank until the operator notices.
	if err := h.ms.members.SetSessionExpired(m.ID); err != nil {
		slog.Warn("console: session deactivated but the expiry timestamp did not commit", "userId", m.ID, "err", err)
	}
	auditAdmin(h.v, "admin.token.revoke", actorFromContext(r.Context()), m.Email)
	writeJSON(w, http.StatusOK, map[string]string{
		"userId":           m.ID,
		"invitationStatus": h.newIndex().statuses(*m).invitation,
		"sessionStatus":    "offline",
	})
}

func (h *consoleAPI) userStats(w http.ResponseWriter, r *http.Request) {
	idx := h.newIndex()
	pending := 0
	for _, m := range h.ms.members.List() {
		if idx.statuses(m).invitation == "invite_pending" {
			pending++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"invitePending": pending})
}

// ── user memberships (user-drawer axis) ─────────────────────────────────

func (h *consoleAPI) userAddTeam(w http.ResponseWriter, r *http.Request) {
	m, err := h.ms.members.Get(r.PathValue("id"))
	if err != nil {
		apiErr(w, http.StatusNotFound, "USER_NOT_FOUND", "no such user")
		return
	}
	var body struct {
		TeamID string `json:"teamId"`
		Role   string `json:"role"`
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
	name, gerr := h.v.Groups().GroupName(body.TeamID)
	if gerr != nil {
		apiErr(w, http.StatusNotFound, "TEAM_NOT_FOUND", gerr.Error())
		return
	}
	if _, exists, _ := h.v.Groups().DirectRole(m.ID, body.TeamID); exists {
		apiErr(w, http.StatusConflict, "ALREADY_TEAM_MEMBER", "user is already a member of this team")
		return
	}
	if _, err := h.v.Groups().Grant(m.ID, body.TeamID, role, localAdminActor(actorFromContext(r.Context()))); err != nil {
		writeGroupAPIErr(w, err)
		return
	}
	auditAdmin(h.v, "admin.group.grant", actorFromContext(r.Context()), m.Email+" @ "+body.TeamID+" ("+body.Role+")")
	writeJSON(w, http.StatusCreated, membershipDTO{TeamID: body.TeamID, TeamName: name, Role: body.Role})
}

func (h *consoleAPI) userRolesBatch(w http.ResponseWriter, r *http.Request) {
	m, err := h.ms.members.Get(r.PathValue("id"))
	if err != nil {
		apiErr(w, http.StatusNotFound, "USER_NOT_FOUND", "no such user")
		return
	}
	var body struct {
		Updates []struct {
			TeamID string `json:"teamId"`
			Role   string `json:"role"`
		} `json:"updates"`
	}
	if err := readJSON(r, &body); err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	// Pre-validate roles → whole-request 400 (keeps failed[] enum to the
	// documented TEAM_NOT_FOUND | NOT_TEAM_MEMBER).
	for _, u := range body.Updates {
		if _, perr := groups.ParseRole(u.Role); perr != nil {
			apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", perr.Error())
			return
		}
	}
	res := newBatchResult()
	actor := localAdminActor(actorFromContext(r.Context()))
	// Inherited-read teams have no stored membership row, so a role change on one
	// is a first-time GRANT (which creates the direct row) rather than an update.
	// Snapshot the user's inherited set once up front so such a team is accepted
	// and promoted instead of rejected as NOT_TEAM_MEMBER. A team already granted
	// earlier in this same batch is found via DirectRole below, so the snapshot
	// going stale after a grant is harmless.
	inherited := map[string]bool{}
	for _, gid := range h.v.Groups().GroupAccess(m.ID).Inherited {
		inherited[gid] = true
	}
	for _, u := range body.Updates {
		role, _ := groups.ParseRole(u.Role) // validated above
		_, direct, gerr := h.v.Groups().DirectRole(m.ID, u.TeamID)
		if gerr != nil {
			res.fail(u.TeamID, "TEAM_NOT_FOUND", "team not found")
			continue
		}
		// Editable when the user already holds the team directly (role change) or
		// inherits read on it (promote to a direct grant). A team the user has no
		// access to at all is not a member of anything here → NOT_TEAM_MEMBER.
		if !direct && !inherited[u.TeamID] {
			res.fail(u.TeamID, "NOT_TEAM_MEMBER", "user is not a member of this team")
			continue
		}
		if _, err := h.v.Groups().Grant(m.ID, u.TeamID, role, actor); err != nil {
			slog.Warn("console: role change failed mid-batch",
				"userId", m.ID, "team", u.TeamID, "err", err)
			res.fail(u.TeamID, "NOT_TEAM_MEMBER", "user is not a member of this team")
			continue
		}
		res.ok(u.TeamID)
	}
	auditAdmin(h.v, "admin.group.grant", actorFromContext(r.Context()), "batch role change for "+m.Email)
	writeJSON(w, http.StatusOK, res)
}

func (h *consoleAPI) userMembershipsRemoveBatch(w http.ResponseWriter, r *http.Request) {
	m, err := h.ms.members.Get(r.PathValue("id"))
	if err != nil {
		apiErr(w, http.StatusNotFound, "USER_NOT_FOUND", "no such user")
		return
	}
	teamIDs := commaList(r, "teamIds")
	if len(teamIDs) == 0 {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "teamIds query parameter is required")
		return
	}
	res := newBatchResult()
	actor := localAdminActor(actorFromContext(r.Context()))
	for _, tid := range teamIDs {
		// Removing a team means the user must not reach it afterwards — not
		// merely that a stored row disappears: the direct grant is dropped
		// AND the read they would still INHERIT from an ancestor membership
		// is cut, in one store transaction. Done as two mutations, a crash
		// between them would leave the team popping straight back into the
		// list as inherited read with its memory still readable — exactly
		// what "remove this team" is supposed to prevent.
		revoked, excluded, rerr := h.v.Groups().Revoke(m.ID, tid, actor)
		if rerr != nil {
			var notFound groups.ErrGroupNotFound
			if errors.As(rerr, &notFound) {
				res.fail(tid, "TEAM_NOT_FOUND", "team not found")
				continue
			}
			// The removal did not commit: the user's access is unchanged
			// (both rows or neither). Say so instead of mislabeling it.
			slog.Warn("console: team removal did not commit", "userId", m.ID, "teamId", tid, "err", rerr)
			res.fail(tid, "INTERNAL", "removal did not commit; retry")
			continue
		}
		// Neither a grant to drop nor an inherited read to cancel: the user had
		// no access to this team to begin with.
		if !revoked && !excluded {
			res.fail(tid, "NOT_TEAM_MEMBER", "user is not a member of this team")
			continue
		}
		res.ok(tid)
	}
	auditAdmin(h.v, "admin.group.revoke", actorFromContext(r.Context()), "batch remove for "+m.Email)
	writeJSON(w, http.StatusOK, res)
}

// ── invitations ───────────────────────────────────────────────────────────

func (h *consoleAPI) createInvitation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Account     string `json:"account"`
		Username    string `json:"username"`
		Memberships []struct {
			TeamID string `json:"teamId"`
			Role   string `json:"role"`
		} `json:"memberships"`
	}
	if err := readJSON(r, &body); err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	if strings.TrimSpace(body.Account) == "" || len(body.Memberships) == 0 {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "account and at least one membership are required")
		return
	}
	// The org admin (Owner) may be invited to teams like any account: admin-ness
	// is a separate axis, not a bar to membership (see addTeamMember).
	// Validate roles + teams before mutating anything.
	type grant struct {
		teamID string
		role   groups.Role
	}
	grants := make([]grant, 0, len(body.Memberships))
	for _, mem := range body.Memberships {
		role, perr := groups.ParseRole(mem.Role)
		if perr != nil {
			apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", perr.Error())
			return
		}
		if _, gerr := h.v.Groups().GroupName(mem.TeamID); gerr != nil {
			apiErr(w, http.StatusNotFound, "TEAM_NOT_FOUND", gerr.Error())
			return
		}
		grants = append(grants, grant{teamID: mem.TeamID, role: role})
	}

	m, gerr := h.ms.members.GetByEmail(body.Account)
	newMember := false
	if gerr != nil {
		created, aerr := h.ms.members.Add(body.Account, body.Username)
		if aerr != nil {
			// GetByEmail missed, so a failed Add means a malformed account
			// (ErrInvalidEmail) — report it in the console error shape.
			apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", aerr.Error())
			return
		}
		m, newMember = created, true
	}

	// An existing member already on one of the target teams is a conflict
	// (doc: 409 ALREADY_TEAM_MEMBER). A brand-new member can't be, so skip.
	if !newMember {
		for _, g := range grants {
			if _, exists, _ := h.v.Groups().DirectRole(m.ID, g.teamID); exists {
				apiErr(w, http.StatusConflict, "ALREADY_TEAM_MEMBER", "user is already a member of a target team")
				return
			}
		}
	}

	// Grant memberships.
	granted := make([]string, 0, len(grants))
	actor := localAdminActor(actorFromContext(r.Context()))
	for _, g := range grants {
		if _, err := h.v.Groups().Grant(m.ID, g.teamID, g.role, actor); err != nil {
			h.rollbackInvitation(m, granted, newMember)
			writeGroupAPIErr(w, err)
			return
		}
		granted = append(granted, g.teamID)
	}

	// Per-status code-send judgment (design doc): online / invite_pending need no
	// code; new / invite_expired / offline (session ended) get one. See needsCode.
	codeSent := false
	if h.needsCode(m) {
		if err := h.issueCode(r, m); err != nil {
			h.rollbackInvitation(m, granted, newMember)
			slog.Error("console: invitation code send failed (invitation rolled back)",
				"account", body.Account, "userId", m.ID, "err", err)
			apiErr(w, http.StatusBadGateway, "MAIL_UPSTREAM_ERROR", "failed to send the invitation code email")
			return
		}
		codeSent = true
	}
	auditAdmin(h.v, "admin.invite.issue", actorFromContext(r.Context()), body.Account)
	// Re-fetch for the response status: issueCode mutated the stored member and
	// Get returns a copy (the m above is stale after the code-send branch).
	if fresh, ferr := h.ms.members.Get(m.ID); ferr == nil {
		m = fresh
	}
	st := h.newIndex().statuses(*m)
	writeJSON(w, http.StatusCreated, map[string]any{
		"userId":           m.ID,
		"account":          m.Email,
		"username":         m.DisplayName,
		"invitationStatus": st.invitation,
		"sessionStatus":    st.session,
		"codeSent":         codeSent,
	})
}

// rollbackInvitation undoes the memberships granted in this request and, for a
// member created in this request, removes the member so a failed invite leaves
// no half-provisioned identity (the doc mandates full rollback on failure).
func (h *consoleAPI) rollbackInvitation(m *members.Member, grantedTeams []string, newMember bool) {
	for _, tid := range grantedTeams {
		_, _ = h.v.Groups().RevokeDirectGrant(m.ID, tid)
	}
	if newMember {
		_ = h.ms.members.Remove(m.ID)
	}
}

func (h *consoleAPI) resendInvitation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID string `json:"userId"`
	}
	if err := readJSON(r, &body); err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	if strings.TrimSpace(body.UserID) == "" {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "userId is required")
		return
	}
	m, err := h.ms.members.Get(body.UserID)
	if err != nil {
		apiErr(w, http.StatusNotFound, "USER_NOT_FOUND", "no such user")
		return
	}
	if err := h.issueCode(r, m); err != nil {
		slog.Error("console: invitation code resend failed",
			"userId", m.ID, "account", m.Email, "err", err)
		apiErr(w, http.StatusBadGateway, "MAIL_UPSTREAM_ERROR", "failed to resend the invitation code email")
		return
	}
	auditAdmin(h.v, "admin.invite.issue", actorFromContext(r.Context()), m.Email)
	// Re-fetch: issueCode mutates the stored member (Reinvite/MarkInvited), and
	// Get returns a COPY, so the pre-issue m is stale. A session-expired member
	// is now invite_pending; an online member stays online.
	if fresh, ferr := h.ms.members.Get(m.ID); ferr == nil {
		m = fresh
	}
	st := h.newIndex().statuses(*m)
	writeJSON(w, http.StatusOK, map[string]string{
		"userId":           m.ID,
		"invitationStatus": st.invitation,
		"sessionStatus":    st.session,
	})
}

func (h *consoleAPI) cancelInvitation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID string `json:"userId"`
	}
	if err := readJSON(r, &body); err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	if strings.TrimSpace(body.UserID) == "" {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "userId is required")
		return
	}
	m, err := h.ms.members.Get(body.UserID)
	if err != nil {
		apiErr(w, http.StatusNotFound, "USER_NOT_FOUND", "no such user")
		return
	}
	// One user action, one transaction: every pending code of the member is
	// voided together (a per-handle loop could half-apply on a crash, leaving
	// the member still invite_pending through the surviving codes).
	voided, err := h.ms.invites.RevokeAllPendingForMember(m.ID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "INTERNAL", "cancellation did not commit; retry")
		return
	}
	if voided == 0 {
		apiErr(w, http.StatusConflict, "INVITATION_NOT_PENDING", "no pending invitation to cancel")
		return
	}
	auditAdmin(h.v, "admin.invite.cancel", actorFromContext(r.Context()), m.Email)
	st := h.newIndex().statuses(*m)
	writeJSON(w, http.StatusOK, map[string]string{
		"userId":           m.ID,
		"invitationStatus": st.invitation, // invite_expired after voiding
		"sessionStatus":    st.session,
	})
}

func (h *consoleAPI) invitationsHistory(w http.ResponseWriter, r *http.Request) {
	page, size, err := parsePaging(r)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	type historyRow struct {
		Account      string  `json:"account"`
		Username     string  `json:"username"`
		IssuedAt     string  `json:"issuedAt"`
		LastAccessAt *string `json:"lastAccessAt"`
	}
	idx := h.newIndex()
	all := h.ms.invites.List() // newest-first (issued_at desc)
	rows := make([]historyRow, 0, len(all))
	for _, inv := range all {
		// Both timestamps cross the wire boundary here: truncated to the
		// doc's second precision (storage is canonical millisecond RFC3339).
		var la *string
		if tl, ok := idx.tokenByEmail[inv.Email]; ok && tl.lastUsed != "" {
			la = wireTimePtr(tl.lastUsed)
		}
		rows = append(rows, historyRow{
			Account:      inv.Email,
			Username:     idx.memberByID[inv.MemberID].DisplayName,
			IssuedAt:     wireTime(inv.CreatedAt),
			LastAccessAt: la,
		})
	}
	// ?sort (design doc §invitations): last_access (DEFAULT — most-recent
	// access first, rows with no access sink to the bottom) | issued_at
	// (newest first) | username (A→Z by display name, account tiebreak). An empty
	// param takes the last_access default; List() already yields issued_at desc.
	sortKey := r.URL.Query().Get("sort")
	if sortKey == "" {
		sortKey = "last_access"
	}
	switch sortKey {
	case "username":
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Username != rows[j].Username {
				return rows[i].Username < rows[j].Username
			}
			return rows[i].Account < rows[j].Account
		})
	case "issued_at":
		// already issued_at desc from List() — no-op
	case "last_access":
		sort.SliceStable(rows, func(i, j int) bool {
			li, lj := "", ""
			if rows[i].LastAccessAt != nil {
				li = *rows[i].LastAccessAt
			}
			if rows[j].LastAccessAt != nil {
				lj = *rows[j].LastAccessAt
			}
			return li > lj // non-empty (recent) first; "" sinks last
		})
	default: // out-of-enum sort value → 400 (same contract as the status filter)
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "unknown sort: "+sortKey)
		return
	}
	pageItems, total := pageSlice(rows, page, size)
	writeJSON(w, http.StatusOK, pageEnvelope{Total: total, Page: page, Size: size, Items: pageItems})
}

// issueCode mints (or rotates) the member's evt_ token, wraps it in a fresh
// invite envelope, advances a not-yet-active member to "invited", and mails the
// registration string. A mail failure returns an error so the caller reports
// MAIL_UPSTREAM_ERROR; the invite envelope is voided so no unusable code lingers.
//
// v1 limitation: when the member already holds a token this ROTATES it (the raw
// token value is not readable to re-wrap in place), so an existing device would
// re-authenticate with the new code. The "online adds a device without
// disturbing the live token" nuance needs a token-value read path and is a
// documented fast-follow.
// hasValidToken reports whether email currently holds a non-expired session
// token (a data-plane credential that can authenticate today).
func (h *consoleAPI) hasValidToken(email string) bool {
	for _, t := range h.v.Tokens().ListTokens() {
		if t.User == email && !t.Expired {
			return true
		}
	}
	return false
}

// invitePendingLive reports whether inv is a pending invite code that has not
// yet passed its expiry — i.e. a code the member could still redeem.
func invitePendingLive(inv invites.InviteView, now time.Time) bool {
	if inv.Status != invites.StatusPending {
		return false
	}
	if inv.ExpiresAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, inv.ExpiresAt)
	return err != nil || now.Before(t) // unparseable → treat as live (fail-safe: don't spam a code)
}

// needsCode decides whether a member should be (re)sent an invitation code when
// added to a team / invited, per the design doc's per-status rule:
//   - online (active + valid token): NO code — already connected.
//   - invite_pending (a live, unexpired code exists): NO code — can still redeem.
//   - everyone else (new/registered, invite_expired, redeemed-but-offline): SEND a code.
//
// This replaces the earlier "any token row exists" gate, which wrongly skipped
// the code for an invite_expired member (whose evt_ token row lingers even
// though the emailed code is dead) and for offline (session-ended) reconnects.
func (h *consoleAPI) needsCode(m *members.Member) bool {
	if m.Status == members.StatusActive && h.hasValidToken(m.Email) {
		return false // online
	}
	if inv, ok := h.ms.invites.LatestByMember(m.ID); ok && invitePendingLive(inv, time.Now().UTC()) {
		return false // invite_pending — a live code already exists
	}
	return true
}

func (h *consoleAPI) issueCode(r *http.Request, m *members.Member) error {
	// Captured BEFORE minting: an online member (active + a valid token) stays
	// online on resend (adding a device); a session-expired member (active but
	// no valid token) must transition back to invite_pending after the resend.
	wasOnline := m.Status == members.StatusActive && h.hasValidToken(m.Email)
	var tokenValue string
	rotated := tokenExistsForUser(h.v, m.Email)
	if rotated {
		t, err := h.v.Tokens().RotateToken(m.Email)
		if err != nil {
			return fmt.Errorf("rotate token: %w", err)
		}
		tokenValue = t.Token
	} else {
		t, err := h.v.Tokens().AddToken(m.Email, nil)
		if err != nil {
			return fmt.Errorf("mint token: %w", err)
		}
		tokenValue = t.Token
	}

	bundle, err := h.ms.invites.Issue(invites.IssueParams{
		MemberID:     m.ID,
		Email:        m.Email,
		Role:         consoleInviteTokenRole,
		TokenValue:   tokenValue,
		CreationPath: inviteCreationPath,
		TTL:          h.ms.ttl,
	})
	if err != nil {
		if !rotated {
			// Best-effort compensation of the token minted above; a refusal
			// leaves an orphaned credential, which the next resend's clean
			// slate (or the boot orphan report) picks up.
			_, _ = h.v.Tokens().RevokeToken(m.Email)
		}
		return fmt.Errorf("issue invite: %w", err)
	}
	// Status transition after re-issuing a code:
	//   - online (was active + valid token): stays online — the resend just adds
	//     a device, no status change.
	//   - session-expired (active, no valid token): back to invited so the
	//     console shows invite_pending (a new code awaiting acceptance), not online.
	//   - registered / invited: advance to invited.
	var terr error
	switch {
	case wasOnline:
		// no status change
	case m.Status == members.StatusActive:
		terr = h.ms.members.Reinvite(m.ID)
	default:
		terr = h.ms.members.MarkInvited(m.ID)
	}
	if terr != nil {
		_ = h.ms.invites.RevokePending(bundle.Handle)
		if !rotated {
			// Best-effort compensation, as above.
			_, _ = h.v.Tokens().RevokeToken(m.Email)
		}
		return fmt.Errorf("advance member status: %w", terr)
	}
	if err := h.ms.mailer.SendInvite(r.Context(), m.Email, m.DisplayName, *bundle, h.ms.conn); err != nil {
		_ = h.ms.invites.RevokePending(bundle.Handle)
		return fmt.Errorf("send invite mail: %w", err)
	}
	return nil
}
