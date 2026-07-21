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
	o, err := st.get()
	if err != nil {
		t.Fatalf("get on a fresh store: %v", err)
	}
	if o != nil {
		t.Errorf("fresh console should be unclaimed, got %+v", o)
	}
}

func TestOwnerReadErrorIsNotUnclaimed(t *testing.T) {
	// A failed read must not masquerade as "no owner": the caller derives the
	// org admin from this, and silently reporting "unclaimed" would leave the
	// console with no admin for the life of the process.
	dbc := openTestDB(t)
	st, err := newOwnerStore(dbc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dbc.Exec(`DROP TABLE console_owner`); err != nil {
		t.Fatal(err)
	}
	o, err := st.get()
	if err == nil {
		t.Error("get returned no error after the table went missing")
	}
	if o != nil {
		t.Errorf("get returned an owner on a failed read: %+v", o)
	}
}

func TestOwnerEmailStoredCanonical(t *testing.T) {
	// The claim is the org-admin key and is never rewritten, so the spelling
	// persisted here is the one the whole system lives with — it must match what
	// emailFromMe hands the login path.
	st := newTestOwnerStore(t)
	if _, err := st.bindIfAbsent("  Alice@X.IO  ", json.RawMessage(`{"email":"Alice@X.IO"}`)); err != nil {
		t.Fatal(err)
	}
	o, err := st.get()
	if err != nil {
		t.Fatal(err)
	}
	if o.Email != "alice@x.io" {
		t.Errorf("stored owner email = %q, want alice@x.io", o.Email)
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
	if got, err := st.get(); err != nil || got == nil || got.Email != "alice@x.io" {
		t.Errorf("owner after race = %+v (err %v), want alice@x.io", got, err)
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
	if got, err := st2.get(); err != nil || got == nil || got.Email != "alice@x.io" {
		t.Errorf("owner after restart = %+v (err %v), want alice@x.io", got, err)
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
	return sessionCookieValue(rr) != ""
}

// sessionCookieValue returns the value of the session cookie the response set,
// or "" when none was set (or it was cleared).
func sessionCookieValue(rr *httptest.ResponseRecorder) string {
	for _, c := range rr.Result().Cookies() {
		if c.Name == cookieName {
			return c.Value
		}
	}
	return ""
}

// getSession drives GET /console/session with the given session cookie and
// returns the decoded body.
func getSession(t *testing.T, h http.Handler, cookieVal string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/console/session", nil) // same-origin
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieVal})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("session body: %v (%s)", err, rr.Body.String())
	}
	return body
}

func TestCallbackBindsFirstAdminAndSoftGatesOthers(t *testing.T) {
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

	// First login (alice) claims the console: 302 to "/" with a session cookie,
	// and the session reports her as the owner.
	rr := doLogin(t, h, "tokA")
	if rr.Code != http.StatusFound {
		t.Fatalf("first login status = %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.HasSuffix(loc, "/") {
		t.Errorf("first login → %q, want redirect to /", loc)
	}
	if !hasSessionCookie(rr) {
		t.Fatal("first login set no session cookie")
	}
	if s := getSession(t, h, sessionCookieValue(rr)); s["is_owner"] != true {
		t.Errorf("owner session is_owner = %v, want true", s["is_owner"])
	}

	// A different account (bob) is SOFT-blocked: it still gets a console session
	// (302 to "/", cookie set, no error code), but /console/session reports
	// is_owner:false and names the owner so the app can show the owner-locked
	// notice instead of the admin shell.
	rr = doLogin(t, h, "tokB")
	if rr.Code != http.StatusFound {
		t.Fatalf("non-owner login status = %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); strings.Contains(loc, "error=") {
		t.Errorf("non-owner login → %q, want no error (soft block)", loc)
	}
	bobCookie := sessionCookieValue(rr)
	if bobCookie == "" {
		t.Fatal("non-owner login set no session cookie (soft block must grant one)")
	}
	bobSess := getSession(t, h, bobCookie)
	if bobSess["logged_in"] != true {
		t.Errorf("non-owner session logged_in = %v, want true", bobSess["logged_in"])
	}
	if bobSess["is_owner"] != false {
		t.Errorf("non-owner session is_owner = %v, want false", bobSess["is_owner"])
	}
	if bobSess["owner_email"] != "alice@x.io" {
		t.Errorf("non-owner session owner_email = %v, want alice@x.io", bobSess["owner_email"])
	}

	// The owner logging in again (fresh cloud session) still succeeds and stays
	// the owner.
	rr = doLogin(t, h, "tokA2")
	if rr.Code != http.StatusFound || !hasSessionCookie(rr) {
		t.Errorf("owner re-login status=%d cookie=%v, want 302 + cookie", rr.Code, hasSessionCookie(rr))
	}
	if loc := rr.Header().Get("Location"); strings.Contains(loc, "error=") {
		t.Errorf("owner re-login → %q, want no error", loc)
	}
	if s := getSession(t, h, sessionCookieValue(rr)); s["is_owner"] != true {
		t.Errorf("owner re-login session is_owner = %v, want true", s["is_owner"])
	}
}

func TestSessionExpiresWhenCloudAccountWithdrawn(t *testing.T) {
	// After the owner withdraws their cloud account, the cloud cascade-deletes
	// its sessions, so GET /me with the stored token returns 401. The console
	// session check must detect that and expire the local login on the spot,
	// rather than keep reporting logged-in off the local snapshot.
	var withdrawn bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/local/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		writeJSON(w, http.StatusOK, map[string]string{"session_token": r.FormValue("code"), "cookie_name": "rc_cloud"})
	})
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, _ *http.Request) {
		if withdrawn {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no session")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"email": "alice@x.io"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	h, _, err := NewHandler(Deps{Port: 8787, APIBaseURL: ts.URL, WebBaseURL: ts.URL, DB: openTestDB(t)})
	if err != nil {
		t.Fatal(err)
	}

	rr := doLogin(t, h, "tokA")
	if rr.Code != http.StatusFound || !hasSessionCookie(rr) {
		t.Fatalf("login status=%d cookie=%v, want 302 + cookie", rr.Code, hasSessionCookie(rr))
	}
	cookie := sessionCookieValue(rr)

	// While the account exists the session check reports logged-in.
	if s := getSession(t, h, cookie); s["logged_in"] != true {
		t.Fatalf("pre-withdrawal logged_in = %v, want true", s["logged_in"])
	}

	// After withdrawal the cloud rejects the stored token (401) → the session
	// check expires the local login.
	withdrawn = true
	if s := getSession(t, h, cookie); s["logged_in"] != false {
		t.Errorf("post-withdrawal logged_in = %v, want false", s["logged_in"])
	}
}

// --- owner → registrar (org-admin derivation) -------------------------------

func TestOwnerRegistrarReplayedAtBootWhenClaimed(t *testing.T) {
	// The owner claim is durable but feeds in-memory state (the org-admin
	// set), so a restarted handler must replay the registrar immediately —
	// a persisted cookie session or a seeded dev session may never run the
	// login callback again.
	dbc := openTestDB(t)
	st, err := newOwnerStore(dbc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.bindIfAbsent("alice@x.io", json.RawMessage(`{"email":"alice@x.io","name":"Alice"}`)); err != nil {
		t.Fatal(err)
	}

	var gotEmail, gotName string
	calls := 0
	_, _, err = NewHandler(Deps{
		Port:       8787,
		APIBaseURL: "http://127.0.0.1:0",
		DB:         dbc,
		OwnerRegistrar: func(email, displayName string) error {
			calls++
			gotEmail, gotName = email, displayName
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("registrar calls at boot = %d, want 1", calls)
	}
	if gotEmail != "alice@x.io" || gotName != "Alice" {
		t.Errorf("registrar got (%q, %q), want (alice@x.io, Alice)", gotEmail, gotName)
	}
}

func TestOwnerRegistrarNotReplayedWhenUnclaimed(t *testing.T) {
	calls := 0
	_, _, err := NewHandler(Deps{
		Port:           8787,
		APIBaseURL:     "http://127.0.0.1:0",
		DB:             openTestDB(t),
		OwnerRegistrar: func(string, string) error { calls++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("registrar calls on unclaimed console = %d, want 0", calls)
	}
}

func TestOwnerRegistrarRunsOnClaimingLoginOnly(t *testing.T) {
	ts := mockLoginCloud(t, map[string]string{
		"tokA": "alice@x.io",
		"tokB": "bob@x.io",
	})
	var emails []string
	h, _, err := NewHandler(Deps{
		Port:       8787,
		APIBaseURL: ts.URL,
		WebBaseURL: ts.URL,
		DB:         openTestDB(t),
		OwnerRegistrar: func(email, _ string) error {
			emails = append(emails, email)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The claiming login registers the owner (org-admin derivation included).
	if rr := doLogin(t, h, "tokA"); rr.Code != http.StatusFound {
		t.Fatalf("first login status = %d, want 302", rr.Code)
	}
	if len(emails) != 1 || emails[0] != "alice@x.io" {
		t.Fatalf("registrar after first login = %v, want [alice@x.io]", emails)
	}

	// A non-owner login is soft-blocked: it succeeds (302 + session cookie) but
	// must NOT reach the registrar — the incumbent keeps admin authority.
	if rr := doLogin(t, h, "tokB"); rr.Code != http.StatusFound || !hasSessionCookie(rr) {
		t.Fatalf("non-owner login status=%d cookie=%v, want 302 + cookie", rr.Code, hasSessionCookie(rr))
	}
	if len(emails) != 1 {
		t.Errorf("registrar ran for a non-owner login: %v", emails)
	}
}
