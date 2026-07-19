package console

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/cloud"
)

// fakeConnector records ConnectRunespace calls in place of the real engine dial.
// addr/token are mutex-guarded because a boot reconnect writes them from a
// background goroutine (Dataplane.Start) while the test reads them.
type fakeConnector struct {
	calls       atomic.Int32
	disconnects atomic.Int32
	mu          sync.Mutex
	addr        string
	token       string
}

func (f *fakeConnector) ConnectRunespace(_ context.Context, addr, token string) error {
	f.mu.Lock()
	f.addr, f.token = addr, token
	f.mu.Unlock()
	f.calls.Add(1)
	return nil
}

func (f *fakeConnector) EngineReady() bool { return f.calls.Load() > f.disconnects.Load() }

func (f *fakeConnector) DisconnectEngine() { f.disconnects.Add(1) }

func (f *fakeConnector) lastAddr() string  { f.mu.Lock(); defer f.mu.Unlock(); return f.addr }
func (f *fakeConnector) lastToken() string { f.mu.Lock(); defer f.mu.Unlock(); return f.token }

// mockCloud serves the minimal runespace-cloud endpoints the dataplane drives.
func mockCloud(t *testing.T, host string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/runespace", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rs_1", "host": host, "phase": "ready", "tier": "free"})
	})
	mux.HandleFunc("POST /api/v1/runespace/access/bootstrap", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"refresh_token": "refresh_abc", "runespace_id": "rs_1"})
	})
	mux.HandleFunc("POST /auth/runespace/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "jwt_xyz", "token_type": "Bearer", "expires_in": 1200})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestDataplaneConnectDialsEngineAndPersists(t *testing.T) {
	ts := mockCloud(t, "rs1.runespace.example")
	fc := &fakeConnector{}
	dp, err := newDataplane(openTestDB(t), cloud.New(ts.URL), fc, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dp.Stop)

	ws, err := dp.Connect(context.Background(), "sess=cookie")
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID != "rs_1" || ws.Phase != "ready" {
		t.Errorf("workspace = %+v", ws)
	}
	// Connect dials in the background (the multi-minute key upload must not block
	// the request), so poll for the engine dial.
	var calls int32
	for i := 0; i < 200; i++ {
		if calls = fc.calls.Load(); calls == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if calls != 1 {
		t.Fatalf("ConnectRunespace calls = %d, want 1", calls)
	}
	if fc.lastAddr() != "rs1.runespace.example:443" {
		t.Errorf("engine addr = %q, want host:443", fc.lastAddr())
	}
	if fc.lastToken() != "jwt_xyz" {
		t.Errorf("engine token = %q, want the exchanged access JWT", fc.lastToken())
	}
	// The durable refresh credential must be persisted for restart/refresh.
	if cred := dp.loadCred(); cred == nil || cred.RefreshToken != "refresh_abc" || cred.Addr != "rs1.runespace.example:443" {
		t.Errorf("refresh credential not persisted: %+v", cred)
	}
}

// TestDataplaneStartReconnectsFromPersistedCred proves a restart resumes the
// data plane from the persisted credential — no login, no bootstrap.
func TestDataplaneStartReconnectsFromPersistedCred(t *testing.T) {
	ts := mockCloud(t, "rs1.runespace.example")
	db := openTestDB(t)

	// First manager connects and persists the credential.
	fc1 := &fakeConnector{}
	dp1, err := newDataplane(db, cloud.New(ts.URL), fc1, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dp1.Connect(context.Background(), "sess=cookie"); err != nil {
		t.Fatal(err)
	}
	dp1.Stop()

	// A fresh manager over the same DB reconnects on Start without a session.
	fc2 := &fakeConnector{}
	dp2, err := newDataplane(db, cloud.New(ts.URL), fc2, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dp2.Stop)
	dp2.Start(context.Background())

	// Start reconnects in the background (non-blocking), so poll for the dial.
	var calls int32
	for i := 0; i < 200; i++ {
		if calls = fc2.calls.Load(); calls == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if calls != 1 {
		t.Fatalf("reconnect ConnectRunespace calls = %d, want 1", calls)
	}
	if fc2.lastToken() != "jwt_xyz" {
		t.Errorf("reconnect token = %q, want a freshly exchanged JWT", fc2.lastToken())
	}
}

// TestDataplaneConnectSwitchesWorkspaceOnChange is the regression test for the
// delete+reprovision bug: the engine can still report EngineReady against a
// deleted runespace (the gRPC connection to the shared tenant gateway outlives
// it), so Connect must key off the attached runespace id — coalesce when it is
// the SAME workspace, but detach + redial when the cloud returns a NEW one.
func TestDataplaneConnectSwitchesWorkspaceOnChange(t *testing.T) {
	var mu sync.Mutex
	id, host := "rs_1", "rs1.runespace.example"
	setWS := func(i, h string) { mu.Lock(); id, host = i, h; mu.Unlock() }
	getWS := func() (string, string) { mu.Lock(); defer mu.Unlock(); return id, host }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/runespace", func(w http.ResponseWriter, _ *http.Request) {
		i, h := getWS()
		_ = json.NewEncoder(w).Encode(map[string]any{"id": i, "host": h, "phase": "ready", "tier": "free"})
	})
	mux.HandleFunc("POST /api/v1/runespace/access/bootstrap", func(w http.ResponseWriter, _ *http.Request) {
		i, _ := getWS()
		_ = json.NewEncoder(w).Encode(map[string]any{"refresh_token": "refresh_" + i, "runespace_id": i})
	})
	mux.HandleFunc("POST /auth/runespace/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "jwt_xyz", "token_type": "Bearer", "expires_in": 1200})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	fc := &fakeConnector{}
	dp, err := newDataplane(openTestDB(t), cloud.New(ts.URL), fc, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dp.Stop)

	// waitSettled blocks until the background dial finished and the single-flight
	// guard cleared (so attachedID is set) — otherwise the next Connect races it.
	waitSettled := func(wantCalls int32) {
		t.Helper()
		for i := 0; i < 400; i++ {
			if fc.calls.Load() == wantCalls && !dp.connecting.Load() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("not settled: calls=%d connecting=%v, want calls=%d", fc.calls.Load(), dp.connecting.Load(), wantCalls)
	}

	// 1) First connect dials rs_1.
	if _, err := dp.Connect(context.Background(), "sess=cookie"); err != nil {
		t.Fatal(err)
	}
	waitSettled(1)
	if fc.lastAddr() != "rs1.runespace.example:443" {
		t.Fatalf("first dial addr = %q", fc.lastAddr())
	}

	// 2) Reconnecting to the SAME workspace must coalesce — no second dial, no detach.
	if _, err := dp.Connect(context.Background(), "sess=cookie"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if c := fc.calls.Load(); c != 1 {
		t.Fatalf("same-workspace reconnect re-dialed: calls = %d, want 1", c)
	}
	if d := fc.disconnects.Load(); d != 0 {
		t.Fatalf("same-workspace reconnect detached: disconnects = %d, want 0", d)
	}

	// 3) After delete+reprovision the cloud returns a NEW workspace. Connect must
	//    detach the stale engine and dial the new one. (The bug short-circuited on
	//    EngineReady here and never dialed rs_2.)
	setWS("rs_2", "rs2.runespace.example")
	if _, err := dp.Connect(context.Background(), "sess=cookie"); err != nil {
		t.Fatal(err)
	}
	waitSettled(2)
	if d := fc.disconnects.Load(); d != 1 {
		t.Fatalf("stale engine not detached before redial: disconnects = %d, want 1", d)
	}
	if fc.lastAddr() != "rs2.runespace.example:443" {
		t.Fatalf("did not dial the new workspace: addr = %q, want rs2...:443", fc.lastAddr())
	}
	if cred := dp.loadCred(); cred == nil || cred.RunespaceID != "rs_2" {
		t.Fatalf("new credential not persisted: %+v", cred)
	}
}

// TestDataplaneConnectCreateConflict covers the create race: GetWorkspace sees
// no runespace but CreateWorkspace hits the cloud's 409 "already exists".
// Connect must tag the error as errWorkspaceExists (→ WORKSPACE_ALREADY_EXISTS
// at the HTTP layer) instead of leaking the bare 409, which writeCloudError
// would misread as a phase conflict.
func TestDataplaneConnectCreateConflict(t *testing.T) {
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

	dp, err := newDataplane(openTestDB(t), cloud.New(ts.URL), &fakeConnector{}, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dp.Stop)

	if _, err := dp.Connect(context.Background(), "sess=cookie"); !errors.Is(err, errWorkspaceExists) {
		t.Fatalf("Connect err = %v, want errWorkspaceExists", err)
	}
}

// TestDataplaneAuthExpiredLifecycle proves the credential-expiry bookkeeping:
// a 401 on the token exchange (here at boot, where the persisted credential
// outlived its cloud-side revocation) drops the dead credential and raises
// AuthExpired; a later session-driven Connect whose exchange succeeds lowers
// it again.
func TestDataplaneAuthExpiredLifecycle(t *testing.T) {
	var reject atomic.Bool
	reject.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/runespace", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rs_1", "host": "rs1.runespace.example", "phase": "ready", "tier": "free"})
	})
	mux.HandleFunc("POST /api/v1/runespace/access/bootstrap", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"refresh_token": "refresh_new", "runespace_id": "rs_1"})
	})
	mux.HandleFunc("POST /auth/runespace/token", func(w http.ResponseWriter, _ *http.Request) {
		if reject.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "UNAUTHORIZED", "message": "refresh_token expired"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "jwt_xyz", "token_type": "Bearer", "expires_in": 1200})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	fc := &fakeConnector{}
	dp, err := newDataplane(openTestDB(t), cloud.New(ts.URL), fc, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dp.Stop)

	// Persist a (revoked) credential, then boot: the background reconnect's
	// exchange 401s — the dead credential must be dropped (a restart would just
	// replay the rejection) and the flag raised.
	if err := dp.saveCred(&dataplaneCred{RefreshToken: "refresh_dead", RunespaceID: "rs_1", Addr: "rs1.runespace.example:443"}); err != nil {
		t.Fatal(err)
	}
	dp.Start(context.Background())
	for i := 0; i < 400 && !dp.AuthExpired(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !dp.AuthExpired() {
		t.Fatal("AuthExpired not raised after a 401 exchange at boot")
	}
	if dp.loadCred() != nil {
		t.Fatal("rejected credential still persisted")
	}

	// Recovery: the cloud accepts exchanges again and the user reconnects with
	// a session — the successful exchange must lower the flag.
	reject.Store(false)
	if _, err := dp.Connect(context.Background(), "sess=cookie"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400 && dp.AuthExpired(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if dp.AuthExpired() {
		t.Fatal("AuthExpired not lowered by a successful exchange")
	}
}

// TestDataplaneConnectRebootstrapsWhenAuthExpired covers the stuck-badge edge:
// the refresh loop's 401 clears the credential and raises authExpired while the
// engine still looks attached on its last JWT. The user-driven reconnect
// (Connect) must NOT coalesce on "already attached" — it has the session the
// background paths lack, so it must re-bootstrap, re-dial, and lower the flag.
func TestDataplaneConnectRebootstrapsWhenAuthExpired(t *testing.T) {
	var bootstraps atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/runespace", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rs_1", "host": "rs1.runespace.example", "phase": "ready", "tier": "free"})
	})
	mux.HandleFunc("POST /api/v1/runespace/access/bootstrap", func(w http.ResponseWriter, _ *http.Request) {
		bootstraps.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"refresh_token": "refresh_new", "runespace_id": "rs_1"})
	})
	mux.HandleFunc("POST /auth/runespace/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "jwt_xyz", "token_type": "Bearer", "expires_in": 1200})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	fc := &fakeConnector{}
	dp, err := newDataplane(openTestDB(t), cloud.New(ts.URL), fc, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dp.Stop)

	// Attach normally first.
	if _, err := dp.Connect(context.Background(), "sess=cookie"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400 && !(fc.calls.Load() == 1 && !dp.connecting.Load()); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if fc.calls.Load() != 1 {
		t.Fatalf("initial dial calls = %d, want 1", fc.calls.Load())
	}

	// Simulate the refresh loop's 401 aftermath (what exchange does): the
	// credential is gone and the flag is up, but the engine is still attached.
	dp.clearCred()
	dp.setAuthExpired(true)

	// The session-driven reconnect must re-bootstrap instead of coalescing.
	if _, err := dp.Connect(context.Background(), "sess=cookie"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400 && dp.AuthExpired(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if dp.AuthExpired() {
		t.Fatal("AuthExpired still raised: Connect coalesced instead of re-bootstrapping")
	}
	if b := bootstraps.Load(); b != 2 {
		t.Fatalf("bootstrap calls = %d, want 2 (initial + recovery)", b)
	}
	if cred := dp.loadCred(); cred == nil || cred.RefreshToken != "refresh_new" {
		t.Fatalf("fresh credential not persisted after recovery: %+v", cred)
	}
}

// TestDataplaneStaleExchange401DoesNotClobberFreshCred is the regression test
// for the review-caught race: a background exchange of an OLD credential can
// return its 401 after a session-driven Connect has already bootstrapped and
// persisted a NEW one (the bootstrap itself revokes the old credential and
// manufactures that 401). The late 401 must not delete the fresh credential or
// raise a false CLOUD_AUTH_EXPIRED badge — only a 401 for the still-persisted
// token may clear and flag.
func TestDataplaneStaleExchange401DoesNotClobberFreshCred(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/runespace/token", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refresh_token"] == "refresh_dead" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "UNAUTHORIZED", "message": "refresh_token revoked"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "jwt_xyz", "token_type": "Bearer", "expires_in": 1200})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	dp, err := newDataplane(openTestDB(t), cloud.New(ts.URL), &fakeConnector{}, slog.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dp.Stop)

	// The fresh credential is already persisted (Connect won the race)…
	if err := dp.saveCred(&dataplaneCred{RefreshToken: "refresh_new", RunespaceID: "rs_1", Addr: "rs1.runespace.example:443"}); err != nil {
		t.Fatal(err)
	}
	// …when the old credential's exchange finally comes back 401.
	if _, err := dp.exchange(context.Background(), "refresh_dead"); err == nil {
		t.Fatal("stale exchange unexpectedly succeeded")
	}
	if cred := dp.loadCred(); cred == nil || cred.RefreshToken != "refresh_new" {
		t.Fatalf("stale 401 clobbered the fresh credential: %+v", cred)
	}
	if dp.AuthExpired() {
		t.Fatal("stale 401 raised a false CLOUD_AUTH_EXPIRED badge")
	}

	// A 401 for the credential that IS persisted must still clear and flag.
	dp.clearCred()
	if err := dp.saveCred(&dataplaneCred{RefreshToken: "refresh_dead", RunespaceID: "rs_1", Addr: "rs1.runespace.example:443"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dp.exchange(context.Background(), "refresh_dead"); err == nil {
		t.Fatal("exchange of the revoked credential unexpectedly succeeded")
	}
	if dp.loadCred() != nil {
		t.Fatal("rejected persisted credential not cleared")
	}
	if !dp.AuthExpired() {
		t.Fatal("AuthExpired not raised for the still-persisted rejected credential")
	}
}
