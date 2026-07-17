package console

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// --- session store ---------------------------------------------------------

func newTestStore(t *testing.T) *sessionStore {
	t.Helper()
	st, err := newSessionStore(openTestDB(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSessionRoundTrip(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.create("tok123", "cloud_sess", json.RawMessage(`{"email":"a@x.io"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := st.get(sess.ID)
	if got == nil {
		t.Fatal("session not found after create")
	}
	if got.Token != "tok123" || got.CookieName != "cloud_sess" {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
	if got.CloudCookie() != "cloud_sess=tok123" {
		t.Errorf("CloudCookie = %q", got.CloudCookie())
	}
	if string(got.Me) != `{"email":"a@x.io"}` {
		t.Errorf("me = %s", got.Me)
	}
}

func TestSessionLazyExpiry(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.create("tok", "c", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Force the row past its expiry.
	if _, err := st.db.Exec(`UPDATE console_session SET expires_at = ? WHERE session_id = ?`,
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), sess.ID); err != nil {
		t.Fatal(err)
	}
	if got := st.get(sess.ID); got != nil {
		t.Error("expired session should return nil")
	}
	// The expired row must be dropped in place, not merely hidden.
	var n int
	if err := st.db.QueryRow(`SELECT count(*) FROM console_session WHERE session_id = ?`, sess.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expired row not deleted (count=%d)", n)
	}
}

func TestSessionRestartPrunesExpired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := newSessionStore(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := st.create("tok", "c", nil)
	_, _ = st.db.Exec(`UPDATE console_session SET expires_at = ? WHERE session_id = ?`,
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), sess.ID)
	_ = d.Close()

	// Reopen: startup prune must drop the lapsed row.
	d2, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	st2, err := newSessionStore(d2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := st2.get(sess.ID); got != nil {
		t.Error("expired session survived restart")
	}
}

// --- loginStore ------------------------------------------------------------

func TestLoginStoreSingleUse(t *testing.T) {
	l := newLoginStore()
	l.put("state1", loginTx{verifier: "v", redirectURI: "r", created: time.Now()})
	if _, ok := l.take("state1"); !ok {
		t.Fatal("first take should succeed")
	}
	if _, ok := l.take("state1"); ok {
		t.Error("second take should fail (single-use)")
	}
	if _, ok := l.take("unknown"); ok {
		t.Error("unknown state should fail")
	}
}

// --- handler / middleware --------------------------------------------------

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	h, _, err := NewHandler(Deps{
		Port:       8787,
		APIBaseURL: "http://cloud.invalid",
		WebBaseURL: "http://web.invalid",
		DB:         openTestDB(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestOriginGuardRejectsCrossSite(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest("GET", "/console/session", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("cross-site status = %d, want 403", rr.Code)
	}
}

func TestSessionEndpointNoCookie(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest("GET", "/console/session", nil) // same-origin (no Sec-Fetch-Site)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body["logged_in"] != false {
		t.Errorf("logged_in = %v, want false", body["logged_in"])
	}
}

func TestAPIRequiresSession(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/teams/tree", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated /api/v1 status = %d, want 401", rr.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	h := newTestHandler(t)
	// With no built frontend, "/" and any client-side route serve the HTML
	// placeholder (deep-link fallback), never a 404.
	for _, p := range []string{"/", "/groups", "/users/123"} {
		req := httptest.NewRequest("GET", p, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", p, rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s content-type = %q, want text/html", p, ct)
		}
	}
}

func TestAPINamespacesDoNotFallBackToSPA(t *testing.T) {
	h := newTestHandler(t)
	// Unmatched paths under the API namespaces must 404 (JSON), not serve the SPA.
	for _, p := range []string{"/api/nope", "/auth/nope"} {
		req := httptest.NewRequest("GET", p, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", p, rr.Code)
		}
	}
}

func TestCallbackErrorRedirects(t *testing.T) {
	h := newTestHandler(t)
	cases := map[string]string{
		"/auth/callback?error=access_denied": "provider",
		"/auth/callback":                     "invalid_state", // no code/state
		"/auth/callback?code=x&state=bogus":  "invalid_state", // unknown state
	}
	for path, wantCode := range cases {
		req := httptest.NewRequest("GET", path, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusFound {
			t.Errorf("%s status = %d, want 302", path, rr.Code)
			continue
		}
		loc := rr.Header().Get("Location")
		if !strings.Contains(loc, "/login?error="+wantCode) {
			t.Errorf("%s → %q, want error=%s", path, loc, wantCode)
		}
		// No session cookie may be set on a failed callback.
		for _, c := range rr.Result().Cookies() {
			if c.Name == cookieName && c.Value != "" {
				t.Errorf("%s set a session cookie on failure", path)
			}
		}
	}
}
