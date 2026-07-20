package console

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newWorkspaceHarness stands up the full console handler against a mock cloud
// with the data-plane routes mounted (Connector wired) and a logged-in session
// minted directly in the store (in-package shortcut for the OAuth flow), so
// tests can drive /api/v1/workspace* exactly as the SPA would.
func newWorkspaceHarness(t *testing.T, cloudURL string) (http.Handler, *Dataplane, *http.Cookie) {
	t.Helper()
	db := openTestDB(t)
	h, dp, err := NewHandler(Deps{
		Port:       8787,
		APIBaseURL: cloudURL,
		WebBaseURL: cloudURL,
		DB:         db,
		Connector:  &fakeConnector{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dp.Stop)
	sessions, err := newSessionStore(db, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.create("cloud-session-token", "rc_cloud", nil)
	if err != nil {
		t.Fatal(err)
	}
	return h, dp, &http.Cookie{Name: cookieName, Value: sess.ID}
}

// decodeErrorBody unmarshals the {code,message} error envelope.
func decodeErrorBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not the {code,message} envelope: %v (%s)", err, rr.Body.String())
	}
	return body
}

// TestWorkspaceStatusCloudAuthExpired: once the data plane records a rejected
// refresh credential, GET /api/v1/workspace flags reconnect:true on a 200 (the
// SC-03 badge feed) rather than erroring — the workspace itself is fine, but the
// data plane cannot restore itself without a session-driven reconnect. Surfacing
// it as a flag (like orphaned) keeps the SPA's poll loop alive and the badge
// populated; a 502 would blank both. A cleared flag drops the reconnect flag.
func TestWorkspaceStatusCloudAuthExpired(t *testing.T) {
	ts := mockCloud(t, "rs1.runespace.example")
	h, dp, cookie := newWorkspaceHarness(t, ts.URL)
	dp.setAuthExpired(true)

	req := httptest.NewRequest("GET", "/api/v1/workspace", nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var expired map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &expired); err != nil {
		t.Fatalf("workspace body is not JSON: %v (%s)", err, rr.Body.String())
	}
	if expired["reconnect"] != true {
		t.Errorf("reconnect = %v, want true (%s)", expired["reconnect"], rr.Body.String())
	}

	// A successful exchange lowers the flag; the poll goes healthy again.
	dp.setAuthExpired(false)
	req = httptest.NewRequest("GET", "/api/v1/workspace", nil)
	req.AddCookie(cookie)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status after recovery = %d, want 200", rr.Code)
	}
	// The healthy 200 body must be projected into the console contract the SPA
	// reads: the cloud phase "running" carried through, and the bare cloud host
	// rendered as a full endpoint URL under `endpointUrl` (not `host`).
	var view map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatalf("workspace body is not JSON: %v (%s)", err, rr.Body.String())
	}
	if view["phase"] != "running" {
		t.Errorf("phase = %v, want running", view["phase"])
	}
	if view["endpointUrl"] != "https://rs1.runespace.example:443" {
		t.Errorf("endpointUrl = %v, want https://rs1.runespace.example:443", view["endpointUrl"])
	}
	if _, ok := view["host"]; ok {
		t.Errorf("body still carries the off-contract `host` key: %v", rr.Body.String())
	}
	if view["reconnect"] == true {
		t.Errorf("reconnect flag still set after recovery: %v", rr.Body.String())
	}
}

// TestWorkspaceStatusOrphaned: when the cloud-held runespace was created under a
// different team_secret than this console holds (its stored team_hash != the
// console's fingerprint), GET /api/v1/workspace flags orphaned:true on a 200 so
// the SPA prompts delete+recreate — the runespace exists and is queryable, so it
// is a flag, not an error. A matching fingerprint (or an unconfigured one on this
// console) carries no orphaned flag.
func TestWorkspaceStatusOrphaned(t *testing.T) {
	cloudWith := func(teamHash string) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/v1/runespace", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "rs_1", "host": "rs1.runespace.example", "phase": "running",
				"tier": "free", "team_hash": teamHash,
			})
		})
		ts := httptest.NewServer(mux)
		t.Cleanup(ts.Close)
		return ts
	}
	build := func(t *testing.T, cloudURL, consoleTeamHash string) (http.Handler, *http.Cookie) {
		t.Helper()
		db := openTestDB(t)
		h, dp, err := NewHandler(Deps{
			Port: 8787, APIBaseURL: cloudURL, WebBaseURL: cloudURL,
			DB: db, Connector: &fakeConnector{}, TeamHash: consoleTeamHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(dp.Stop)
		sessions, err := newSessionStore(db, slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		sess, err := sessions.create("cloud-session-token", "rc_cloud", nil)
		if err != nil {
			t.Fatal(err)
		}
		return h, &http.Cookie{Name: cookieName, Value: sess.ID}
	}
	statusView := func(t *testing.T, h http.Handler, cookie *http.Cookie) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/workspace", nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		var view map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
			t.Fatalf("workspace body is not JSON: %v (%s)", err, rr.Body.String())
		}
		return rr.Code, view
	}

	t.Run("mismatch flags orphaned", func(t *testing.T) {
		h, cookie := build(t, cloudWith("OLD_FINGERPRINT").URL, "NEW_FINGERPRINT")
		code, view := statusView(t, h, cookie)
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if view["orphaned"] != true {
			t.Errorf("orphaned = %v, want true (%v)", view["orphaned"], view)
		}
	})

	t.Run("match carries no orphaned flag", func(t *testing.T) {
		h, cookie := build(t, cloudWith("SAME_FINGERPRINT").URL, "SAME_FINGERPRINT")
		code, view := statusView(t, h, cookie)
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if _, ok := view["orphaned"]; ok {
			t.Errorf("orphaned present on a matching workspace: %v", view)
		}
	})

	t.Run("unconfigured console team hash never orphans", func(t *testing.T) {
		h, cookie := build(t, cloudWith("OLD_FINGERPRINT").URL, "")
		code, view := statusView(t, h, cookie)
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if _, ok := view["orphaned"]; ok {
			t.Errorf("orphaned present with an unconfigured console team hash: %v", view)
		}
	})
}

// TestWorkspaceConnectAlreadyExists: a connect whose create races an existing
// runespace (GET says none, POST hits the cloud's 409 "already exists") must
// surface the doc's 409 WORKSPACE_ALREADY_EXISTS — not WORKSPACE_INVALID_PHASE,
// which would tell the SPA a state transition was rejected.
func TestWorkspaceConnectAlreadyExists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/runespace", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "NOT_FOUND", "message": "no runespace"})
	})
	mux.HandleFunc("POST /api/v1/runespace", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "CONFLICT", "message": "runespace already exists"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	h, _, cookie := newWorkspaceHarness(t, ts.URL)
	req := httptest.NewRequest("POST", "/api/v1/workspace", nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rr.Code)
	}
	if body := decodeErrorBody(t, rr); body["code"] != "WORKSPACE_ALREADY_EXISTS" {
		t.Errorf("code = %q, want WORKSPACE_ALREADY_EXISTS", body["code"])
	}
}
