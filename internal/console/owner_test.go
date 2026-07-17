package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CryptoLabInc/rune-console/internal/db"
)

// --- ownerStore ------------------------------------------------------------

func newTestOwnerStore(t *testing.T) *ownerStore {
	t.Helper()
	st, err := newOwnerStore(openTestDB(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestOwnerUnclaimed(t *testing.T) {
	st := newTestOwnerStore(t)
	if o := st.get(); o != nil {
		t.Errorf("fresh console should be unclaimed, got %+v", o)
	}
}

func TestOwnerBindOnceWins(t *testing.T) {
	st := newTestOwnerStore(t)
	first, err := st.bindIfAbsent("alice@x.io", json.RawMessage(`{"email":"alice@x.io"}`))
	if err != nil {
		t.Fatal(err)
	}
	if first.Email != "alice@x.io" {
		t.Fatalf("first bind owner = %q, want alice@x.io", first.Email)
	}
	// A second, different account must NOT overwrite — bindIfAbsent returns the
	// incumbent so the caller can refuse the newcomer.
	second, err := st.bindIfAbsent("bob@x.io", json.RawMessage(`{"email":"bob@x.io"}`))
	if err != nil {
		t.Fatal(err)
	}
	if second.Email != "alice@x.io" {
		t.Errorf("second bind returned %q, want incumbent alice@x.io", second.Email)
	}
	if got := st.get(); got == nil || got.Email != "alice@x.io" {
		t.Errorf("owner after race = %+v, want alice@x.io", got)
	}
}

func TestOwnerPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := newOwnerStore(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.bindIfAbsent("alice@x.io", json.RawMessage(`{"email":"alice@x.io"}`)); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()

	// Reopen: the binding must survive.
	d2, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	st2, err := newOwnerStore(d2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := st2.get(); got == nil || got.Email != "alice@x.io" {
		t.Errorf("owner after restart = %+v, want alice@x.io", got)
	}
}

// --- callback binding / duplicate-admin block ------------------------------

// mockLoginCloud serves the two endpoints handleCallback drives: the local
// token exchange (echoes the code as the session token) and /api/v1/me (maps
// the session token back to a controllable email). It lets a test log in "as"
// any account by choosing the callback code.
func mockLoginCloud(t *testing.T, tokenToEmail map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/local/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		writeJSON(w, http.StatusOK, map[string]string{
			"session_token": r.FormValue("code"),
			"cookie_name":   "rc_cloud",
		})
	})
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		email := ""
		if c, err := r.Cookie("rc_cloud"); err == nil {
			email = tokenToEmail[c.Value]
		}
		writeJSON(w, http.StatusOK, map[string]string{"email": email})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// loginStateFromStart drives POST /console/auth/start and extracts the opaque
// PKCE state the callback needs (the same one the server stashed), by unwrapping
// the returned /signin?authorize=<cloud authorize url> link.
func loginStateFromStart(t *testing.T, h http.Handler) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/console/auth/start", nil) // same-origin
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("auth/start status = %d, want 200", rr.Code)
	}
	var body struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("auth/start body: %v (%s)", err, rr.Body.String())
	}
	outer, err := url.Parse(body.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := url.Parse(outer.Query().Get("authorize"))
	if err != nil {
		t.Fatal(err)
	}
	state := inner.Query().Get("state")
	if state == "" {
		t.Fatalf("no state in authorize url %q", body.AuthorizeURL)
	}
	return state
}

// doLogin runs one full callback for the given code and returns the recorder.
func doLogin(t *testing.T, h http.Handler, code string) *httptest.ResponseRecorder {
	t.Helper()
	state := loginStateFromStart(t, h)
	req := httptest.NewRequest("GET", "/auth/callback?code="+code+"&state="+state, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func hasSessionCookie(rr *httptest.ResponseRecorder) bool {
	for _, c := range rr.Result().Cookies() {
		if c.Name == cookieName && c.Value != "" {
			return true
		}
	}
	return false
}

func TestCallbackBindsFirstAdminAndBlocksOthers(t *testing.T) {
	ts := mockLoginCloud(t, map[string]string{
		"tokA":  "alice@x.io",
		"tokA2": "alice@x.io",
		"tokB":  "bob@x.io",
	})
	h, _, err := NewHandler(Deps{
		Port:       8787,
		APIBaseURL: ts.URL,
		WebBaseURL: ts.URL,
		DB:         openTestDB(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	// First login (alice) claims the console: 302 to "/" with a session cookie.
	rr := doLogin(t, h, "tokA")
	if rr.Code != http.StatusFound {
		t.Fatalf("first login status = %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.HasSuffix(loc, "/") {
		t.Errorf("first login → %q, want redirect to /", loc)
	}
	if !hasSessionCookie(rr) {
		t.Error("first login set no session cookie")
	}

	// A different account (bob) is refused: 302 to the admin_locked screen naming
	// the owner, and no session cookie.
	rr = doLogin(t, h, "tokB")
	if rr.Code != http.StatusFound {
		t.Fatalf("blocked login status = %d, want 302", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "/login?error=admin_locked") {
		t.Errorf("blocked login → %q, want error=admin_locked", loc)
	}
	if !strings.Contains(loc, "owner="+url.QueryEscape("alice@x.io")) {
		t.Errorf("blocked login → %q, want owner=alice@x.io", loc)
	}
	if hasSessionCookie(rr) {
		t.Error("blocked login must not set a session cookie")
	}

	// The owner logging in again (fresh cloud session) still succeeds.
	rr = doLogin(t, h, "tokA2")
	if rr.Code != http.StatusFound || !hasSessionCookie(rr) {
		t.Errorf("owner re-login status=%d cookie=%v, want 302 + cookie", rr.Code, hasSessionCookie(rr))
	}
	if loc := rr.Header().Get("Location"); strings.Contains(loc, "error=") {
		t.Errorf("owner re-login → %q, want no error", loc)
	}
}
