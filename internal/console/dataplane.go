package console

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/cloud"
)

// DataplaneConnector attaches a dialed runespace engine to the gRPC server and
// reports whether one is currently attached. *server.Console satisfies it
// (ConnectRunespace opens the FHE keys, dials, and attaches under lock;
// EngineReady reflects the attach state). Kept as an interface so this package
// need not import internal/server.
type DataplaneConnector interface {
	ConnectRunespace(ctx context.Context, addr, token string) error
	EngineReady() bool
	// DisconnectEngine detaches and closes the engine, returning the gRPC
	// service to "runespace not configured". Called after a runespace delete.
	DisconnectEngine()
}

// accessRefreshWindow re-dials before the ~20m cloud access token expires.
const accessRefreshWindow = 15 * time.Minute

const (
	connectRetryBase = 15 * time.Second
	connectRetryMax  = 2 * time.Minute
)

// runespacePhaseReady is the runespace-cloud phase that signals the engine pod
// has passed its readiness probe and can accept the key-registration handshake.
// The cloud assigns a deterministic host (<id>.<domain>) and reports
// phase=provisioning/starting long before the pod is up, so the host alone is not
// a readiness signal — dialing at any other phase hits the gateway with no live
// upstream and fails RegisterKeys with EOF. Mirrors provisioner phaseRunning.
const runespacePhaseReady = "running"

// errWorkspaceExists tags a CreateWorkspace 409: the runespace already exists,
// meaning Connect's get-or-create raced another creator (or the cloud's read
// was stale). writeCloudError relays it as the doc's 409
// WORKSPACE_ALREADY_EXISTS instead of the generic phase-conflict 409.
var errWorkspaceExists = errors.New("runespace already exists")

// dataplaneCred is the persisted, durable data-plane credential: the refresh
// token mints short-lived access JWTs, and addr/runespaceID pin the engine
// target so the connection survives a restart without a login.
type dataplaneCred struct {
	RefreshToken string
	RunespaceID  string
	Addr         string
}

// Dataplane owns the console's single runespace engine connection: the
// provision+bootstrap flow (needs a session), the persisted refresh credential,
// and the background refresh loop that re-dials before the access JWT expires
// (no session needed). It bridges the BFF session and the gRPC data-plane engine.
type Dataplane struct {
	cloud     *cloud.Client
	connector DataplaneConnector
	db        *sql.DB
	log       *slog.Logger

	// teamHash is this console's team_secret fingerprint, sent with a workspace
	// create so the cloud records which install owns the runespace. Empty when no
	// team_secret is configured (no fingerprint to send). See crypto.TeamHash.
	teamHash string

	mu     sync.Mutex
	base   context.Context    // refresh-loop parent (daemon lifetime)
	cancel context.CancelFunc // stops the current refresh loop

	// connecting single-flights the login-driven Connect: the eval-key
	// registration is a multi-hundred-MB, multi-minute upload, so only one may
	// run at a time (a second Connect coalesces instead of racing/cancelling it).
	connecting atomic.Bool

	// attachedID is the runespace id the engine is currently attached to (set on
	// a successful reconnect, cleared on Disconnect). Connect compares it against
	// the resolved workspace: after a delete+reprovision the engine can still
	// report EngineReady (the gRPC connection to the shared tenant gateway
	// outlives the deleted runespace), so a plain EngineReady check would skip
	// dialing the NEW workspace forever. Guarded by mu.
	attachedID string

	// authExpired records that the cloud rejected the refresh credential on a
	// token exchange (401: expired or revoked). Background reconnects have no
	// session to re-bootstrap with, so the data plane cannot restore itself —
	// GET /api/v1/workspace surfaces 502 CLOUD_AUTH_EXPIRED (doc contract, the
	// SC-03 badge feed) until a session-driven Connect exchanges successfully.
	// Guarded by mu.
	authExpired bool

	lastConnectErr string
	lastConnectAt  time.Time

	attachTarget string // attached runespace id

	retryBase, retryMax time.Duration
}

const dataplaneSchema = `
CREATE TABLE IF NOT EXISTS dataplane_credential (
  id            INTEGER PRIMARY KEY CHECK (id = 1),
  refresh_token TEXT NOT NULL,
  runespace_id  TEXT NOT NULL,
  addr          TEXT NOT NULL,
  created_at    TEXT NOT NULL
);`

func newDataplane(db *sql.DB, cl *cloud.Client, conn DataplaneConnector, log *slog.Logger, teamHash string) (*Dataplane, error) {
	if _, err := db.Exec(dataplaneSchema); err != nil {
		return nil, err
	}
	return &Dataplane{
		cloud: cl, connector: conn, db: db, log: log, teamHash: teamHash,
		base:      context.Background(),
		retryBase: connectRetryBase, retryMax: connectRetryMax,
	}, nil
}

// Start sets the refresh loop's parent context (the daemon lifetime) and, if a
// credential is already persisted, reconnects in the background so a restart
// resumes the data plane without a login. The reconnect is asynchronous on
// purpose: it does network I/O (token exchange + gRPC dial) that can block or
// time out, and it must never hold up the console listeners.
func (d *Dataplane) Start(ctx context.Context) {
	d.mu.Lock()
	d.base = ctx
	d.mu.Unlock()

	cred := d.loadCred()
	if cred == nil {
		return
	}

	if !d.acquireAttachSlot(ctx, cred.RunespaceID) {
		return
	}

	d.startConnectLoop(cred)
}

// Connect runs the full provision+bootstrap flow for the logged-in session:
// resolve (or create) the runespace, mint+persist the durable refresh
// credential, exchange it for an access JWT, dial the engine, and start the
// refresh loop. Returns the workspace view (which may still be provisioning,
// in which case no engine is dialed yet).
func (d *Dataplane) Connect(ctx context.Context, sessionCookie string) (*cloud.Workspace, error) {
	ws, err := d.cloud.GetWorkspace(ctx, sessionCookie)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		if ws, err = d.cloud.CreateWorkspace(ctx, sessionCookie, d.teamHash); err != nil {
			var ae *cloud.APIError
			if errors.As(err, &ae) && ae.Status == http.StatusConflict {
				// GetWorkspace saw nothing but the create hit the cloud's 409:
				// another connect raced us. Tag it so writeCloudError relays
				// "already exists" instead of the generic phase-conflict 409.
				return nil, fmt.Errorf("%w: %v", errWorkspaceExists, err)
			}
			return nil, err
		}
	}

	if !connectable(ws) {
		return ws, nil
	}
	// Already connected to THIS workspace: return without re-bootstrapping
	// (bootstrap revokes the prior refresh credential) or kicking off a second
	// eval-key upload. The caller polls GET /console/session for engine_connected
	// to observe completion. The attached-id guard is what makes delete+reprovision
	// work: EngineReady alone can stay true against a deleted runespace (the gRPC
	// connection to the shared tenant gateway outlives it), so a plain check would
	// never dial the new workspace.
	d.mu.Lock()
	attached := d.attachedID
	expired := d.authExpired
	d.mu.Unlock()
	// authExpired bypasses the coalesce: the engine can still look attached on
	// its last JWT while the refresh credential is already dead (the loop
	// cleared it and exited) — this session-driven connect is exactly the
	// re-bootstrap that recovers it, so it must not short-circuit.
	if d.connector.EngineReady() && attached == ws.ID && !expired {
		return ws, nil
	}

	if !d.acquireAttachSlot(ctx, ws.ID) {
		return ws, nil
	}

	// Re-read workspace
	if cur, gerr := d.cloud.GetWorkspace(ctx, sessionCookie); gerr == nil && cur != nil {
		ws = cur
	}
	if !connectable(ws) {
		d.releaseAttachSlot()
		return ws, nil
	}

	d.mu.Lock()
	d.attachTarget = ws.ID
	attached = d.attachedID
	expired = d.authExpired
	d.mu.Unlock()

	if d.connector.EngineReady() && attached == ws.ID && !expired {
		d.releaseAttachSlot()
		return ws, nil
	}
	// The engine is attached to a different (stale/deleted) runespace — detach it
	// before dialing the new one, otherwise ConnectRunespace would attach a second
	// engine over the old one.
	if d.connector.EngineReady() {
		d.Stop()
		d.connector.DisconnectEngine()
		d.mu.Lock()
		d.attachedID = ""
		d.mu.Unlock()
	}
	addr := ws.Host + ":443"

	bs, err := d.cloud.BootstrapAccess(ctx, sessionCookie)
	if err != nil {
		d.releaseAttachSlot()
		return nil, err
	}
	cred := &dataplaneCred{RefreshToken: bs.RefreshToken, RunespaceID: ws.ID, Addr: addr}
	if err := d.saveCred(cred); err != nil {
		d.releaseAttachSlot()
		return nil, err
	}

	// Dial + register the keys in the background on the daemon context, NOT the
	// request context: the eval key is hundreds of MB and takes minutes to
	// upload, and a browser refresh/abort (or an overlapping click) must not
	// cancel the stream mid-flight (which the engine rejects as an integrity
	// failure). The single-flight guard above coalesces concurrent Connects.
	d.startConnectLoop(cred)
	return ws, nil
}

func connectable(ws *cloud.Workspace) bool {
	switch ws.Phase {
	case runespacePhaseReady, "provisioning", "starting":
		return ws.Host != ""
	}

	return false
}

func (d *Dataplane) acquireAttachSlot(ctx context.Context, wsID string) bool {
	const tick = 50 * time.Millisecond

	for i := 0; i < 50; i++ { // ~2.5s
		d.mu.Lock()
		if !d.connecting.Load() {
			d.connecting.Store(true)
			d.attachTarget = wsID
			d.mu.Unlock()

			return true
		}
		target := d.attachTarget
		d.mu.Unlock()

		if target == wsID {
			return false // live attach
		}

		d.Stop() // stale

		select {
		case <-ctx.Done():
			return false
		case <-time.After(tick):
		}
	}

	return false
}

func (d *Dataplane) releaseAttachSlot() {
	d.mu.Lock()
	d.attachTarget = ""
	d.connecting.Store(false)
	d.mu.Unlock()
}

func (d *Dataplane) startConnectLoop(cred *dataplaneCred) {
	d.mu.Lock()
	if d.cancel != nil {
		d.cancel()
	}

	loopCtx, cancel := context.WithCancel(d.base)
	d.cancel = cancel
	d.mu.Unlock()

	go func() {
		defer d.releaseAttachSlot()
		d.connectLoop(loopCtx, cred)
	}()
}

func (d *Dataplane) connectLoop(ctx context.Context, cred *dataplaneCred) {
	delay := d.retryBase

	for {
		err := d.reconnect(ctx, cred)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			return // cancelled (Stop/Pause/Disconncet)
		}

		if cloud.IsUnauthorized(err) {
			d.log.Warn("console: data-plane credential rejected; reconnect from the UI", "err", err.Error())
			return
		}
		d.log.Warn("console: data-plane attach failed; retrying", "err", err.Error(), "retry_in", delay.String())

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		if delay *= 2; delay > d.retryMax {
			delay = d.retryMax
		}

		// Credential is deleted or replaced
		if cur := d.loadCred(); cur == nil || cur.RefreshToken != cred.RefreshToken {
			return
		}
	}
}

// reconnect exchanges the refresh credential for a fresh access JWT, dials the
// engine, and (re)starts the refresh loop. Safe to call repeatedly.
func (d *Dataplane) reconnect(ctx context.Context, cred *dataplaneCred) error {
	tok, err := d.exchange(ctx, cred.RefreshToken)
	if err != nil {
		if ctx.Err() == nil {
			d.setLastConnectErr(err)
		}
		return err
	}
	if err := d.connector.ConnectRunespace(ctx, cred.Addr, tok.AccessToken); err != nil {
		if ctx.Err() == nil {
			d.setLastConnectErr(err)
		}
		return err
	}
	d.setLastConnectErr(nil)
	d.mu.Lock()
	d.attachedID = cred.RunespaceID
	d.mu.Unlock()
	d.log.Info("console: data-plane engine connected", "addr", cred.Addr, "runespace_id", cred.RunespaceID)
	d.startRefreshLoop()
	return nil
}

func (d *Dataplane) setLastConnectErr(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err == nil { // success
		d.lastConnectErr, d.lastConnectAt = "", time.Time{}
		return
	}

	d.lastConnectErr, d.lastConnectAt = err.Error(), time.Now().UTC()
}

func (d *Dataplane) LastConnectError() (string, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastConnectErr, d.lastConnectAt
}

// exchange wraps ExchangeAccessToken with credential-expiry bookkeeping. A 401
// means the cloud expired/revoked the refresh credential: the persisted copy is
// dead weight (a restart would just replay the rejection), so drop it and raise
// authExpired for GET /api/v1/workspace to surface. Any successful exchange
// lowers the flag — the credential demonstrably works again.
func (d *Dataplane) exchange(ctx context.Context, refreshToken string) (*cloud.AccessToken, error) {
	tok, err := d.cloud.ExchangeAccessToken(ctx, refreshToken)
	if err != nil {
		// Staleness gate: a background exchange can lose the race with a
		// session-driven Connect that just bootstrapped a NEW credential (the
		// cloud revokes the old one on bootstrap, manufacturing this 401). A
		// late 401 for a token that is no longer the persisted one must not
		// clobber the fresh row or raise a false badge — clear + flag only
		// when the rejected token is still what is stored (atomic in SQL).
		if cloud.IsUnauthorized(err) && d.clearCredIf(refreshToken) {
			d.setAuthExpired(true)
		}
		return nil, err
	}
	d.setAuthExpired(false)
	return tok, nil
}

func (d *Dataplane) setAuthExpired(v bool) {
	d.mu.Lock()
	d.authExpired = v
	d.mu.Unlock()
}

// AuthExpired reports whether the cloud rejected the data-plane refresh
// credential on the most recent token exchange — i.e. retrying is pointless and
// only a session-driven reconnect (POST /api/v1/workspace) can restore the
// data plane.
func (d *Dataplane) AuthExpired() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.authExpired
}

// startRefreshLoop (re)starts the background loop that re-dials before the
// access JWT expires. Any prior loop is cancelled first so there is exactly one.
func (d *Dataplane) startRefreshLoop() {
	d.mu.Lock()
	if d.cancel != nil {
		d.cancel()
	}
	loopCtx, cancel := context.WithCancel(d.base)
	d.cancel = cancel
	d.mu.Unlock()
	go d.refreshLoop(loopCtx)
}

// Stop cancels the refresh loop. Used at shutdown and in tests; the engine
// itself is released by the server's Console.Close.
func (d *Dataplane) Stop() {
	d.mu.Lock()
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.mu.Unlock()
}

// Disconnect tears down the local data-plane state after the runespace is
// deleted: stop the refresh loop, drop the persisted refresh credential (so a
// restart does not try to resume a dead runespace), and detach + close the
// engine so the gRPC service reports "runespace not configured" again.
func (d *Dataplane) Disconnect() {
	d.Stop()
	d.clearCred()
	d.connector.DisconnectEngine()
	d.mu.Lock()
	d.attachedID = ""
	// Teardown leaves nothing to reconnect; a lingering expired badge would be
	// stale noise over the (deleted) workspace's 404.
	d.authExpired = false
	d.lastConnectErr, d.lastConnectAt = "", time.Time{}
	d.attachTarget = ""
	d.mu.Unlock()
}

// Pause detaches the engine and stops the refresh loop WITHOUT dropping the
// persisted credential. It backs POST /workspace/stop: the cloud stops the
// runespace pod (volume retained), so the console must stop dialing the now-gone
// host and let the gRPC service report a clean "not configured", while keeping
// the credential so a later Resume reconnects without a re-login. This is the
// reversible sibling of Disconnect (which is the permanent post-delete teardown
// that also clears the credential).
func (d *Dataplane) Pause() {
	d.Stop()
	d.connector.DisconnectEngine()
	// The engine is now attached to nothing; clear attachedID so a later Connect
	// (e.g. the workspace changed while stopped) re-dials instead of trusting a
	// stale id — the same invariant Disconnect maintains.
	d.mu.Lock()
	d.attachedID = ""
	d.attachTarget = ""
	d.lastConnectErr, d.lastConnectAt = "", time.Time{}
	d.mu.Unlock()
}

// Resume reconnects the engine from the persisted credential after a stop. It
// backs POST /workspace/start: the reconnect is asynchronous (token exchange +
// gRPC dial can block), and it no-ops when no credential is persisted.
func (d *Dataplane) Resume() {
	cred := d.loadCred()
	if cred == nil {
		return
	}
	d.mu.Lock()
	base := d.base
	d.mu.Unlock()
	go func() {
		if !d.acquireAttachSlot(base, cred.RunespaceID) {
			return // a live attach of this workspace is already running
		}

		// Re-validate credential
		if cur := d.loadCred(); cur == nil || cur.RefreshToken != cred.RefreshToken {
			d.releaseAttachSlot()
			return
		}
		d.mu.Lock()
		attached := d.attachedID
		d.mu.Unlock()
		if d.connector.EngineReady() && attached == cred.RunespaceID {
			d.releaseAttachSlot()
			return
		}
		d.startConnectLoop(cred)
	}()
}

func (d *Dataplane) refreshLoop(ctx context.Context) {
	t := time.NewTicker(accessRefreshWindow)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cred := d.loadCred()
			if cred == nil {
				return // credential cleared (logout / revoked)
			}
			tok, err := d.exchange(ctx, cred.RefreshToken)
			if err != nil {
				if cloud.IsUnauthorized(err) {
					// Refresh credential revoked (e.g. a new bootstrap elsewhere).
					// exchange dropped it and raised authExpired; stop the loop —
					// only a session-driven connect can re-bootstrap.
					d.log.Warn("console: data-plane refresh credential rejected; reconnect required", "err", err.Error())
					return
				}
				d.log.Warn("console: data-plane token refresh failed; will retry", "err", err.Error())
				continue
			}
			if err := d.connector.ConnectRunespace(ctx, cred.Addr, tok.AccessToken); err != nil {
				d.log.Warn("console: data-plane re-dial failed; will retry", "err", err.Error())
			}
		}
	}
}

// --- credential persistence (single row, holds the refresh token at rest) ---

func (d *Dataplane) saveCred(c *dataplaneCred) error {
	_, err := d.db.Exec(
		`INSERT INTO dataplane_credential (id, refresh_token, runespace_id, addr, created_at)
		 VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   refresh_token = excluded.refresh_token,
		   runespace_id  = excluded.runespace_id,
		   addr          = excluded.addr,
		   created_at    = excluded.created_at`,
		c.RefreshToken, c.RunespaceID, c.Addr, nowRFC3339(),
	)
	return err
}

func (d *Dataplane) loadCred() *dataplaneCred {
	var c dataplaneCred
	err := d.db.QueryRow(`SELECT refresh_token, runespace_id, addr FROM dataplane_credential WHERE id = 1`).
		Scan(&c.RefreshToken, &c.RunespaceID, &c.Addr)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			d.log.Warn("console: read data-plane credential", "err", err.Error())
		}
		return nil
	}
	return &c
}

func (d *Dataplane) clearCred() {
	if _, err := d.db.Exec(`DELETE FROM dataplane_credential WHERE id = 1`); err != nil {
		d.log.Warn("console: clear data-plane credential", "err", err.Error())
	}
}

// clearCredIf deletes the persisted credential only if it still carries the
// given refresh token, reporting whether a row was actually deleted. The WHERE
// clause makes the check-and-delete atomic, so a concurrent saveCred of a
// newer credential can never be clobbered by a stale 401.
func (d *Dataplane) clearCredIf(refreshToken string) bool {
	res, err := d.db.Exec(`DELETE FROM dataplane_credential WHERE id = 1 AND refresh_token = ?`, refreshToken)
	if err != nil {
		d.log.Warn("console: clear data-plane credential", "err", err.Error())
		return false
	}
	n, err := res.RowsAffected()
	return err == nil && n > 0
}
