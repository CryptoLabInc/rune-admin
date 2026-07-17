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
// refresh credential, GET /api/v1/workspace must surface the doc's 502
// CLOUD_AUTH_EXPIRED (the SC-03 badge feed) instead of a healthy 200 — the
// workspace itself is fine, but the data plane cannot restore itself without a
// session-driven reconnect. A cleared flag returns the status to 200.
func TestWorkspaceStatusCloudAuthExpired(t *testing.T) {
	ts := mockCloud(t, "rs1.runespace.example")
	h, dp, cookie := newWorkspaceHarness(t, ts.URL)
	dp.setAuthExpired(true)

	req := httptest.NewRequest("GET", "/api/v1/workspace", nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rr.Code)
	}
	if body := decodeErrorBody(t, rr); body["code"] != "CLOUD_AUTH_EXPIRED" {
		t.Errorf("code = %q, want CLOUD_AUTH_EXPIRED", body["code"])
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
	// reads: the raw cloud phase "ready" mapped to "running", and the bare cloud
	// host rendered as a full endpoint URL under `endpointUrl` (not `host`).
	var view map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatalf("workspace body is not JSON: %v (%s)", err, rr.Body.String())
	}
	if view["phase"] != "running" {
		t.Errorf("phase = %v, want running (cloud 'ready' must be mapped)", view["phase"])
	}
	if view["endpointUrl"] != "https://rs1.runespace.example:443" {
		t.Errorf("endpointUrl = %v, want https://rs1.runespace.example:443", view["endpointUrl"])
	}
	if _, ok := view["host"]; ok {
		t.Errorf("body still carries the off-contract `host` key: %v", rr.Body.String())
	}
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
