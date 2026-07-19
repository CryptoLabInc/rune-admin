package console

import (
	"errors"
	"net/http"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/cloud"
)

// handleWorkspaceConnect (POST /api/v1/workspace) provisions the caller's
// runespace if it does not exist, bootstraps + persists the durable data-plane
// credential, and dials the gRPC engine. Async by nature: if the runespace is
// still provisioning it returns the phase and no engine is dialed yet — the
// caller polls GET /api/v1/workspace and retries.
func (s *Service) handleWorkspaceConnect(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "SESSION_INVALID", "not logged in")
		return
	}
	ws, err := s.dp.Connect(r.Context(), sess.CloudCookie())
	if err != nil {
		s.writeCloudError(w, sess, err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.setStatus(workspaceView(ws)))
}

// handleWorkspaceDelete (DELETE /api/v1/workspace) permanently deprovisions the
// caller's runespace and tears down the local data-plane connection (engine
// detached + closed, persisted refresh credential dropped). The cloud side is
// asynchronous, so this returns 202 Accepted.
func (s *Service) handleWorkspaceDelete(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "SESSION_INVALID", "not logged in")
		return
	}
	// Tear down the local data-plane regardless of the cloud outcome: the user
	// asked to delete, and an engine left attached to a (being-)deleted runespace
	// would otherwise block reconnecting to a future one.
	defer s.dp.Disconnect()
	if err := s.cloud.DeleteWorkspace(r.Context(), sess.CloudCookie()); err != nil {
		s.writeCloudError(w, sess, err)
		return
	}
	// Async delete: 202 with the GET /workspace shape (doc contract), whose phase
	// is now deleting (the cloud recorded it on the delete), not a bespoke
	// {deleted:true} body. The data-plane teardown is deferred above so it runs
	// regardless of outcome.
	s.writeWorkspaceAccepted(w, r, sess)
}

// handleWorkspaceStatus (GET /api/v1/workspace) reports the runespace status.
func (s *Service) handleWorkspaceStatus(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "SESSION_INVALID", "not logged in")
		return
	}
	ws, err := s.cloud.GetWorkspace(r.Context(), sess.CloudCookie())
	if err != nil {
		s.writeCloudError(w, sess, err)
		return
	}
	if ws == nil {
		writeError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "no runespace yet")
		return
	}
	if s.orphaned(ws) {
		// The cloud-held runespace was created under a different team_secret than
		// this console now holds — a reinstall mints a fresh secret, so the stored
		// data is encrypted under a key we no longer have and the runespace cannot
		// be adopted, only deleted and recreated. It still exists and is queryable,
		// so surface it as a flag on the 200 view (not an error) for the SPA to
		// prompt "delete & recreate?". This outranks the reconnect-oriented
		// CLOUD_AUTH_EXPIRED badge below: recreating supersedes reconnecting, and a
		// reinstall often has no persisted credential to expire in the first place.
		// Not logged — the SPA polls this every few seconds.
		view := s.setStatus(workspaceView(ws))
		view["orphaned"] = true
		writeJSON(w, http.StatusOK, view)
		return
	}
	if s.dp.AuthExpired() {
		// The cloud rejected the data-plane refresh credential and background
		// reconnects have no session to re-bootstrap with — retrying is
		// pointless until the SPA drives a reconnect (POST /api/v1/workspace).
		// Doc contract: 502 CLOUD_AUTH_EXPIRED on this polled endpoint is what
		// feeds the SC-03 navbar badge. (Ordered after the nil check: a deleted
		// workspace's 404 route-guard signal outranks a stale credential badge.
		// Not logged here — the SPA polls this every few seconds and the 401
		// that raised the flag is already logged once at its source.)
		writeError(w, http.StatusBadGateway, "CLOUD_AUTH_EXPIRED", "cloud credential expired; reconnect the workspace")
		return
	}
	writeJSON(w, http.StatusOK, s.setStatus(workspaceView(ws)))
}

func (s *Service) setStatus(view map[string]any) map[string]any {
	connected := s.engineReady()
	view["engine_connected"] = connected // whether connected or not

	if s.dp != nil && !connected {
		if msg, at := s.dp.LastConnectError(); msg != "" {
			view["last_connect_error"] = msg
			view["last_connect_error_at"] = at.Format(time.RFC3339)
		}
	}

	return view
}

// consolePhase validates the runespace-cloud observed phase against the phase
// vocabulary the SPA renders: provisioning|starting|running|stopping|stopped|
// deleting|error. The cloud reports these verbatim — the two vocabularies are
// identical by design, so there is nothing to translate — and this guards the
// contract: an unexpected value is surfaced as error rather than leaked raw to
// the SPA.
func consolePhase(cloudPhase string) string {
	switch cloudPhase {
	case "provisioning", "starting", "running", "stopping", "stopped", "deleting", "error":
		return cloudPhase
	default:
		return "error"
	}
}

// workspaceView is the GET /workspace response shape (console API design
// §Workspace): {phase, endpointUrl, rows}. The runespace is a cloud resource
// the console only proxies, so the values come from the runespace-cloud
// workspace response, projected into the console contract: the raw cloud phase
// is mapped to the console vocabulary, the bare cloud host is rendered as a full
// endpoint URL (null until one is assigned), and rows is null until the
// workspace is serving. The cloud does not expose a creation timestamp
// (runespace-cloud runespaceResponse omits it), so createdAt is not carried.
func workspaceView(ws *cloud.Workspace) map[string]any {
	phase := consolePhase(ws.Phase)
	var endpointURL any // null until the cloud assigns a host
	if ws.Host != "" {
		endpointURL = "https://" + ws.Host + ":443"
	}
	var rows any // null until the workspace is serving (SC-02 state D)
	if phase == "running" {
		rows = ws.Rows
	}
	return map[string]any{
		"phase":       phase,
		"endpointUrl": endpointURL,
		"rows":        rows,
	}
}

// orphaned reports whether the cloud-held runespace belongs to a different
// team_secret than this console currently holds. Both fingerprints must be
// present to compare: an unconfigured team_secret (s.teamHash == "") or a
// runespace the cloud recorded no fingerprint for (ws.TeamHash == "") yields no
// orphan signal — there is nothing to contradict a match. A reinstall mints a
// fresh team_secret (changing s.teamHash) while the cloud keeps the fingerprint
// recorded at create time, so the two diverge and this reports true.
func (s *Service) orphaned(ws *cloud.Workspace) bool {
	return s.teamHash != "" && ws.TeamHash != "" && ws.TeamHash != s.teamHash
}

// writeWorkspaceAccepted responds to an async stop/start/delete with 202 and the
// GET /workspace shape read back from the cloud. The cloud records the in-flight
// transition (stopping/starting/deleting) optimistically on the action itself, so
// the phase already reflects it — no synthesis on the console side. If the status
// read fails, it still returns the full shape (null fields) so the SPA can begin
// polling without tripping over a missing key.
func (s *Service) writeWorkspaceAccepted(w http.ResponseWriter, r *http.Request, sess *Session) {
	view := map[string]any{"phase": nil, "endpointUrl": nil, "rows": nil}
	if ws, err := s.cloud.GetWorkspace(r.Context(), sess.CloudCookie()); err == nil && ws != nil {
		view = workspaceView(ws)
	}
	writeJSON(w, http.StatusAccepted, s.setStatus(view))
}

// handleWorkspaceStop (POST /api/v1/workspace/stop) asks the cloud to stop
// (pause) the runespace — volume retained, compute billing stops, reversible —
// then detaches the local engine cleanly (keeping the refresh credential) so the
// gRPC data plane reports a clean "not configured" rather than dialing a dead
// host. Async: returns 202 with phase=stopping, then the SPA polls.
func (s *Service) handleWorkspaceStop(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "SESSION_INVALID", "not logged in")
		return
	}
	if err := s.cloud.StopWorkspace(r.Context(), sess.CloudCookie()); err != nil {
		s.writeCloudError(w, sess, err)
		return
	}
	if s.dp != nil {
		s.dp.Pause()
	}
	s.writeWorkspaceAccepted(w, r, sess)
}

// handleWorkspaceStart (POST /api/v1/workspace/start) asks the cloud to start a
// stopped runespace (pod re-created on the retained volume), then re-attaches
// the local engine from the persisted credential. Async: 202 with
// phase=starting, then the SPA polls until phase=running + engine_connected.
func (s *Service) handleWorkspaceStart(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "SESSION_INVALID", "not logged in")
		return
	}
	if err := s.cloud.StartWorkspace(r.Context(), sess.CloudCookie()); err != nil {
		s.writeCloudError(w, sess, err)
		return
	}
	if s.dp != nil {
		s.dp.Resume()
	}
	s.writeWorkspaceAccepted(w, r, sess)
}

// sessionFrom re-reads the live session from the request cookie (the
// requireSession middleware has already validated one exists).
func (s *Service) sessionFrom(r *http.Request) *Session {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	return s.sessions.get(c.Value)
}

// writeCloudError maps a runespace-cloud error to an HTTP response: a 401 from
// the cloud means the held session is stale, so drop it and surface
// SESSION_INVALID; anything else is an upstream failure.
func (s *Service) writeCloudError(w http.ResponseWriter, sess *Session, err error) {
	if cloud.IsUnauthorized(err) {
		// The cloud session backing rc_session is gone — drop the local session
		// and trigger the SPA's global 401 handling.
		s.sessions.delete(sess.ID)
		writeError(w, http.StatusUnauthorized, "SESSION_INVALID", "cloud session expired")
		return
	}
	// Map the cloud's own status onto the doc's workspace error codes instead of
	// collapsing everything into a generic 502, so the SPA can distinguish
	// not-found / invalid-phase from a transient upstream failure. The specific
	// cloud cause goes to the server log for ops; the client gets a clean code.
	if cloud.IsNotFound(err) {
		s.log.Info("console: cloud reports no runespace", "err", err.Error())
		writeError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "no runespace")
		return
	}
	if errors.Is(err, errWorkspaceExists) {
		// Connect's get-or-create raced an existing runespace — relay the
		// cloud's own "already exists" under its doc code rather than the
		// generic phase-conflict 409 below.
		s.log.Info("console: workspace already exists", "err", err.Error())
		writeError(w, http.StatusConflict, "WORKSPACE_ALREADY_EXISTS", "workspace already exists")
		return
	}
	var ae *cloud.APIError
	if errors.As(err, &ae) && ae.Status == http.StatusConflict {
		s.log.Warn("console: cloud rejected workspace transition (conflict)", "err", err.Error())
		writeError(w, http.StatusConflict, "WORKSPACE_INVALID_PHASE", "workspace is not in a state that allows this action")
		return
	}
	s.log.Warn("console: cloud upstream error", "err", err.Error())
	writeError(w, http.StatusBadGateway, "CLOUD_UPSTREAM_ERROR", "cloud service is temporarily unavailable; please retry")
}
