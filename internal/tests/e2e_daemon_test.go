//go:build e2e

package tests

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/CryptoLabInc/rune-console/internal/db"
	"github.com/CryptoLabInc/rune-console/internal/storedb"
	pb "github.com/CryptoLabInc/rune-console/pkg/consolepb"
)

// rpcTimeout bounds one gRPC call against the booted daemon. Generous: it also
// absorbs the window between /healthz answering and gRPC accepting (Serve binds
// the gRPC listener first but calls gs.Serve after starting the console one).
const rpcTimeout = 15 * time.Second

// freePort reserves an ephemeral loopback TCP port and returns it. The
// listener is closed before the daemon starts, so a parallel process could in
// principle steal the port — acceptable for a smoke test.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// writeBootConfig writes a minimal boot config for dataDir at the given ports
// and returns its path. TLS is mandatory on the gRPC listener (there is no
// disable switch), so it generates a self-signed serving cert under dataDir and
// points the config at it. No static runespace endpoint, console enabled so
// /healthz is served. cloud.* is required-when-console-enabled but only dialed
// on login, and the explicit members.console_endpoint skips the boot-time
// public-IP probe (no network dependency in CI). The FIRST boot over a data dir
// generates FHE keys under keys.path, which dominates startup time; a later
// boot over the same dir reuses them.
func writeBootConfig(t *testing.T, dataDir, name string, grpcPort, consolePort int) string {
	t.Helper()
	certPath, keyPath := writeSelfSignedCert(t, dataDir)
	conf := fmt.Sprintf(`server:
  grpc: { host: 127.0.0.1, port: %d, tls: { cert: %s, key: %s } }
  console: { enabled: true, port: %d }
cloud: { api_base_url: https://cloud.invalid, web_base_url: https://cloud.invalid }
keys: { path: %s, embedding_dim: 1024 }
tokens: { team_secret: e2e-team-secret }
storage: { data_dir: %s }
members: { console_endpoint: "127.0.0.1:%d" }
audit: { mode: stdout }
`,
		grpcPort, certPath, keyPath, consolePort,
		filepath.Join(dataDir, "keys"),
		dataDir,
		grpcPort)
	path := filepath.Join(dataDir, name)
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeSelfSignedCert generates a self-signed TLS cert+key valid for 127.0.0.1
// under dir and returns their paths. It is its own trust anchor (IsCA), so a
// client pins the same cert file as its root to dial the daemon over TLS. The
// pair is generated once per dir: successive boots over one data dir (boot1,
// boot2) then present an identical cert the client pins a single time.
func writeSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	certPath = filepath.Join(dir, "server.pem")
	keyPath = filepath.Join(dir, "server.key")
	if _, err := os.Stat(certPath); err == nil {
		return certPath, keyPath
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "runeconsole-e2e"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile(certPath,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath,
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

// daemon is one runeconsole process under test.
type daemon struct {
	cmd     *exec.Cmd
	logPath string
	started time.Time

	// exited is closed once cmd.Wait returns, and waitErr holds the exit
	// status (safe to read after receiving from exited). Wait runs exactly
	// once, in the goroutine startDaemon spawns, so BOTH the readiness poll
	// and shutdown can observe the exit: cmd.ProcessState only becomes
	// non-nil after Wait, so polling it instead would never fire.
	exited  chan struct{}
	waitErr error
}

// startDaemon launches the pre-built binary against confPath, teeing its
// output to logPath. The process is killed at test end as a safety net.
func startDaemon(t *testing.T, binary, confPath, logPath string) *daemon {
	t.Helper()
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })

	cmd := exec.Command(binary, "--config", confPath, "daemon", "start")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	d := &daemon{cmd: cmd, logPath: logPath, started: time.Now(), exited: make(chan struct{})}
	go func() {
		d.waitErr = cmd.Wait()
		close(d.exited)
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() }) // no-op after a clean shutdown
	return d
}

func (d *daemon) dumpLog(t *testing.T) {
	t.Helper()
	if data, err := os.ReadFile(d.logPath); err == nil {
		t.Logf("daemon log (%s):\n%s", filepath.Base(d.logPath), data)
	}
}

// waitReady polls the console listener's /healthz until it answers 200,
// failing fast if the daemon dies first.
func (d *daemon) waitReady(t *testing.T, consolePort int, budget time.Duration) {
	t.Helper()
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", consolePort)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if resp, err := client.Get(healthURL); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-d.exited:
			d.dumpLog(t)
			t.Fatalf("daemon exited before becoming ready: %v (%s)", d.waitErr, healthURL)
		default:
		}
		time.Sleep(200 * time.Millisecond)
	}
	d.dumpLog(t)
	t.Fatalf("daemon not ready after %s (%s)", time.Since(d.started).Round(time.Second), healthURL)
}

// shutdown SIGTERMs the daemon and requires a zero exit (Serve returns nil).
func (d *daemon) shutdown(t *testing.T) {
	t.Helper()
	if err := d.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal daemon: %v", err)
	}
	select {
	case <-d.exited:
		if d.waitErr != nil {
			d.dumpLog(t)
			t.Fatalf("daemon exited uncleanly after SIGTERM: %v", d.waitErr)
		}
	case <-time.After(30 * time.Second):
		d.dumpLog(t)
		_ = d.cmd.Process.Kill()
		t.Fatal("daemon did not exit within 30s of SIGTERM")
	}
}

// ── seeded store fixture ──────────────────────────────────────────
//
// One coherent identity spread across all four store tables, so a single
// restart can be asked to serve it back. Every value is chosen to satisfy the
// schema CHECKs and the loaders' Go-side validation (internal/storedb/schema.go).
const (
	// seedMemberEmail is BOTH the member's person key and the token's user:
	// that overlap is what makes the dataplane resolve the token's email to
	// the member UUID the group membership below is keyed by.
	seedMemberEmail = "seed-member@example.com"
	// seedMemberID must pass members.ValidateID (canonical lowercase UUID) —
	// the person-key validator the daemon injects into the group store.
	seedMemberID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

	// The token is 'evt_' + 32 hex (36 chars, the proto's fixed length) and
	// names a role that is NOT a built-in: tokens.LoadFromDB seeds admin and
	// member itself, so a custom role proves the roles TABLE was read too
	// (Validate is fail-closed — a token whose role is missing is refused).
	seedTokenValue = "evt_0123456789abcdef0123456789abcdef"
	seedRoleName   = "e2e-seeded-role"

	// Two groups, parent and child, with the membership held on the PARENT:
	// the reachable tree then spans both, which can only be built if both
	// group rows AND the membership row loaded.
	seedParentGroupID   = "11111111-2222-3333-4444-555555555555"
	seedParentGroupName = "seeded-engineering"
	seedChildGroupID    = "66666666-7777-8888-9999-aaaaaaaaaaaa"
	seedChildGroupName  = "seeded-platform"
	seedMembershipRole  = "write"

	// The invite: handle and lease are 32 lowercase hex (schema CHECK), and
	// the creation path mirrors the unexported server.inviteCreationPath —
	// LookupWrap/Unwrap refuse a wrap minted by any other surface, so a
	// mismatch here reads as NotFound.
	seedInviteHandle       = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	seedInviteLease        = "0f8e7d6c5b4a39281706f5e4d3c2b1a0"
	seedInviteCreationPath = "admin.member.invite"
	// The sealed token Unwrap releases. Deliberately NOT seedTokenValue, so
	// the assertion can only pass by reading this invite row.
	seedSealedToken = "evt_fedcba9876543210fedcba9876543210"
)

// seedStoreDB writes one row into each of the four store tables of a STOPPED
// daemon's runeconsole.db. Raw SQL on purpose: the point is to plant state the
// daemon can only serve by loading it, so nothing here goes through a store.
func seedStoreDB(t *testing.T, path string) {
	t.Helper()
	database, err := db.OpenStrict(path)
	if err != nil {
		t.Fatalf("open store db for seeding: %v", err)
	}
	defer func() { _ = database.Close() }()

	now := time.Now().UTC()
	// Canonical RFC3339-UTC-with-milliseconds. The invites loader REJECTS a
	// non-canonical expires_at outright (it sweeps the column textually), so
	// the shared helper is used rather than a hand-rolled literal.
	created := storedb.FormatTime(now)
	expires := storedb.FormatTime(now.Add(time.Hour))

	rows := []struct {
		what string
		stmt string
		args []any
	}{
		{"role", `INSERT INTO roles (name, scope, top_k, rate_limit) VALUES (?, ?, ?, ?)`,
			[]any{seedRoleName, `["get_public_key"]`, 5, "30/60s"}},
		{"token", `INSERT INTO tokens (user, token, role, issued_at, expires, last_used)
		           VALUES (?, ?, ?, ?, NULL, NULL)`, // NULL expires = never expires
			[]any{seedMemberEmail, seedTokenValue, seedRoleName, now.Format("2006-01-02")}}, // issued_at is date-only by contract
		{"member", `INSERT INTO members (id, email, display_name, status, disabled_from, created_at, session_expired_at)
		            VALUES (?, ?, ?, 'invited', NULL, ?, NULL)`, // 'invited' so Unwrap's activation has a legal hop
			[]any{seedMemberID, seedMemberEmail, "Seed Member", created}},
		{"parent group", `INSERT INTO groups (id, name, parent_id, created_at) VALUES (?, ?, '', ?)`, // '' = root sentinel
			[]any{seedParentGroupID, seedParentGroupName, created}},
		{"child group", `INSERT INTO groups (id, name, parent_id, created_at) VALUES (?, ?, ?, ?)`,
			[]any{seedChildGroupID, seedChildGroupName, seedParentGroupID, created}},
		{"membership", `INSERT INTO memberships (user, group_id, role, granted_by, granted_at) VALUES (?, ?, ?, 'e2e-seed', ?)`,
			[]any{seedMemberID, seedParentGroupID, seedMembershipRole, created}},
		{"invite", `INSERT INTO invites (handle, lease_id, member_id, email, token_value, role, creation_path, created_at, expires_at, status)
		            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')`,
			[]any{seedInviteHandle, seedInviteLease, seedMemberID, seedMemberEmail,
				seedSealedToken, seedRoleName, seedInviteCreationPath, created, expires}},
	}

	tx, err := database.Begin()
	if err != nil {
		t.Fatalf("begin seed transaction: %v", err)
	}
	for _, r := range rows {
		if _, err := tx.Exec(r.stmt, r.args...); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed %s row: %v", r.what, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed transaction: %v", err)
	}
}

// assertRedemptionPersisted re-opens the stopped daemon's store database and
// requires the invite redemption's writes to actually be on disk. Serving the
// seeded state proves the LoadFromDB reads ran; this proves the same call also
// attached the write-through sink — the half that fails silently, because a
// sink-less store's persist() returns nil without writing anything, so it
// accepts every mutation and loses it.
func assertRedemptionPersisted(t *testing.T, path string) {
	t.Helper()
	database, err := db.OpenStrict(path)
	if err != nil {
		t.Fatalf("reopen store db: %v", err)
	}
	defer func() { _ = database.Close() }()

	var inviteStatus string
	var tokenValue sql.NullString
	if err := database.QueryRow(
		`SELECT status, token_value FROM invites WHERE handle = ?`, seedInviteHandle,
	).Scan(&inviteStatus, &tokenValue); err != nil {
		t.Fatalf("read back seeded invite: %v", err)
	}
	if inviteStatus != "consumed" {
		t.Errorf("invite status on disk = %q, want %q — the redemption did not reach the invites table", inviteStatus, "consumed")
	}
	if tokenValue.Valid {
		t.Errorf("invite token_value on disk = %q, want NULL — the released token was not scrubbed", tokenValue.String)
	}

	var memberStatus string
	if err := database.QueryRow(
		`SELECT status FROM members WHERE id = ?`, seedMemberID,
	).Scan(&memberStatus); err != nil {
		t.Fatalf("read back seeded member: %v", err)
	}
	if memberStatus != "active" {
		t.Errorf("member status on disk = %q, want %q — the activation did not reach the members table", memberStatus, "active")
	}
}

// TestDaemonBootsServesSeededStoresAndShutsDownCleanly is the E2E harness: it
// boots the pre-built runeconsole binary (RUNECONSOLE_TEST_BINARY, exported by
// `mise run go:test:e2e`) against a minimal temp-dir config, waits for the
// console listener's /healthz, and SIGTERMs it expecting a clean exit.
//
// It then seeds rows directly into runeconsole.db while the daemon is stopped
// and reboots over the SAME data directory, requiring the daemon to SERVE that
// state. That second phase is the store-load guard. Every store is an
// in-memory cache over the unified store database, attached by its own
// LoadFromDB call in internal/commands/daemon.go; a store that never gets its
// sink starts EMPTY and silently accepts every write and drops it (persist()
// returns nil when no database is attached). internal/commands has no unit
// tests, so losing any one of those four calls is otherwise invisible — this
// is the test that fails. Coverage, per store:
//
//   - tokens   — GetPermissions authenticates the seeded token against the
//     seeded (non-built-in) role.
//   - groups   — the same call returns the seeded membership and the
//     parent+child tree it reaches.
//   - members  — the same call only finds that membership if the token's email
//     resolved to the member UUID it is keyed by; Unwrap then activates that
//     member.
//   - invites  — LookupWrap serves the seeded invite and Unwrap releases its
//     sealed token.
//
// The RPCs used are exactly those reachable without a console login: the
// pre-auth redemption pair and one token-authenticated call.
func TestDaemonBootsServesSeededStoresAndShutsDownCleanly(t *testing.T) {
	binary := os.Getenv("RUNECONSOLE_TEST_BINARY")
	if binary == "" {
		t.Skip("RUNECONSOLE_TEST_BINARY not set — run `mise run go:build` then `mise run go:test:e2e`")
	}

	dir := t.TempDir()
	storeDBPath := filepath.Join(dir, "runeconsole.db")

	// ── Boot 1: cold start over an empty data dir (installs the schema and
	// generates the FHE key set, so the budget is generous). ──
	grpcPort1, consolePort1 := freePort(t), freePort(t)
	d1 := startDaemon(t, binary,
		writeBootConfig(t, dir, "boot1.conf", grpcPort1, consolePort1),
		filepath.Join(dir, "boot1.log"))
	d1.waitReady(t, consolePort1, 120*time.Second)

	// The daemon opens the unified store database unconditionally at boot: it
	// must exist in the data directory with owner-only permissions.
	dbInfo, err := os.Stat(storeDBPath)
	if err != nil {
		d1.dumpLog(t)
		t.Fatalf("store database missing after boot: %v", err)
	}
	if perm := dbInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("store database mode = %04o, want 0600", perm)
	}

	// Graceful shutdown: SIGTERM must return a zero exit (Serve returns nil).
	d1.shutdown(t)

	// ── Seed the stopped daemon's database, then boot 2 over the same dir. ──
	seedStoreDB(t, storeDBPath)

	grpcPort2, consolePort2 := freePort(t), freePort(t)
	d2 := startDaemon(t, binary,
		writeBootConfig(t, dir, "boot2.conf", grpcPort2, consolePort2),
		filepath.Join(dir, "boot2.log"))
	d2.waitReady(t, consolePort2, 120*time.Second)
	defer func() {
		if t.Failed() {
			d2.dumpLog(t)
		}
	}()

	// TLS gRPC: the daemon serves with the self-signed cert writeBootConfig
	// generated in dir; the client pins that same cert as its root (ServerName
	// 127.0.0.1 matches the cert SAN). WaitForReady on each call absorbs the gap
	// between /healthz answering and gs.Serve accepting.
	creds, err := credentials.NewClientTLSFromFile(filepath.Join(dir, "server.pem"), "127.0.0.1")
	if err != nil {
		t.Fatalf("load client TLS creds: %v", err)
	}
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcPort2),
		grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("dial gRPC: %v", err)
	}
	defer func() { _ = conn.Close() }()
	client := pb.NewConsoleServiceClient(conn)

	// invites: pre-auth, read-only — it must not consume the code, so it runs
	// before Unwrap.
	t.Run("LookupWrapServesSeededInvite", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		resp, err := client.LookupWrap(ctx,
			&pb.LookupWrapRequest{Handle: seedInviteHandle}, grpc.WaitForReady(true))
		if err != nil {
			t.Fatalf("LookupWrap: %v — the invite store did not serve the seeded row (invites.LoadFromDB)", err)
		}
		if got := resp.GetEmail(); got != seedMemberEmail {
			t.Errorf("email = %q, want %q", got, seedMemberEmail)
		}
		if got := resp.GetRole(); got != seedRoleName {
			t.Errorf("role = %q, want %q", got, seedRoleName)
		}
		if got := resp.GetCreationPath(); got != seedInviteCreationPath {
			t.Errorf("creation_path = %q, want %q", got, seedInviteCreationPath)
		}
	})

	// tokens + groups + members in one token-authenticated call.
	t.Run("GetPermissionsServesSeededTokenGroupsAndMember", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		resp, err := client.GetPermissions(ctx,
			&pb.GetPermissionsRequest{Token: seedTokenValue}, grpc.WaitForReady(true))
		if err != nil {
			t.Fatalf("GetPermissions: %v — the token store did not authenticate the seeded token against its seeded role (tokens.LoadFromDB)", err)
		}
		if got := resp.GetMe(); got != seedMemberEmail {
			t.Errorf("me = %q, want %q", got, seedMemberEmail)
		}

		// The membership row is keyed by the member UUID while the token
		// carries the email, so finding it requires BOTH the group store's
		// memberships (groups.LoadFromDB) and the registry's email → UUID
		// resolution (members.LoadFromDB).
		var memberships []string
		for _, m := range resp.GetMemberships() {
			memberships = append(memberships, fmt.Sprintf("%s/%s", m.GetGroupName(), m.GetRole()))
		}
		wantMemberships := []string{seedParentGroupName + "/" + seedMembershipRole}
		if !slices.Equal(memberships, wantMemberships) {
			t.Errorf("memberships = %v, want %v — the seeded membership was not served (groups.LoadFromDB + members.LoadFromDB)",
				memberships, wantMemberships)
		}

		// The reachable tree spans the seeded parent and, by inheritance, its
		// child — so it can only be built from both seeded group rows.
		var tree []string
		for _, n := range resp.GetTree() {
			tree = append(tree, fmt.Sprintf("%s/%d/%s", n.GetName(), n.GetDepth(), n.GetEffectiveRole()))
		}
		wantTree := []string{
			fmt.Sprintf("%s/0/%s", seedParentGroupName, seedMembershipRole),
			fmt.Sprintf("%s/1/%s", seedChildGroupName, seedMembershipRole),
		}
		if !slices.Equal(tree, wantTree) {
			t.Errorf("tree = %v, want %v — the seeded group tree was not served (groups.LoadFromDB)", tree, wantTree)
		}
	})

	// invites + members: consumes the code and advances the member
	// invited→active in the same call.
	t.Run("UnwrapReleasesSeededTokenAndActivatesMember", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		resp, err := client.Unwrap(ctx,
			&pb.UnwrapRequest{Handle: seedInviteHandle}, grpc.WaitForReady(true))
		if err != nil {
			t.Fatalf("Unwrap: %v — the invite store must release the seeded sealed token and the member registry must activate %s (invites.LoadFromDB + members.LoadFromDB)",
				err, seedMemberID)
		}
		if got := resp.GetToken(); got != seedSealedToken {
			t.Errorf("token = %q, want the seeded sealed token %q", got, seedSealedToken)
		}
		if got := resp.GetMemberId(); got != seedMemberID {
			t.Errorf("member_id = %q, want %q", got, seedMemberID)
		}
	})

	d2.shutdown(t)

	// With the daemon stopped, the redemption's writes must be on disk.
	assertRedemptionPersisted(t, storeDBPath)
}
