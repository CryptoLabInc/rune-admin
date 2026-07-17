//go:build e2e

package tests

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/CryptoLabInc/rune-console/internal/db"
	pb "github.com/CryptoLabInc/rune-console/pkg/consolepb"
)

// Fixed identifiers for the seeded legacy data set, referenced across the
// YAML seed, the SQL assertions, and the serving check.
const (
	e2eAdminToken  = "evt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // owner@corp.com, role admin
	e2eMemberToken = "evt_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" // alice@corp.com, role auditor
	e2eInviteToken = "evt_cccccccccccccccccccccccccccccccc" // sealed inside the pending invite
	e2eAliceID     = "11111111-1111-1111-1111-111111111111"
	e2eBobID       = "22222222-2222-2222-2222-222222222222"
	e2eHandle      = "0123456789abcdef0123456789abcdef"
	e2eLease       = "fedcba9876543210fedcba9876543210"
)

// legacyYAMLBasenames are the six legacy store files the importer consumes,
// in the daemon's source order.
var legacyYAMLBasenames = []string{
	"roles.yml", "tokens.yml", "groups.yml", "memberships.yml", "members.yml", "invites.yml",
}

// writeLegacyYAML seeds dir with a realistic pre-migration data directory:
// members with mixed statuses (incl. a disabled one carrying disabled_from),
// a pending invite with sealed plaintext, tokens + a custom role, and a
// two-level group tree with a member-UUID-keyed membership. All files 0600
// (the invites loader refuses looser modes).
func writeLegacyYAML(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"roles.yml": `roles:
  auditor:
    scope: [get_public_key, decrypt_scores]
    top_k: 7
    rate_limit: 5/60s
`,
		"tokens.yml": fmt.Sprintf(`tokens:
  - user: owner@corp.com
    token: %s
    role: admin
    issued_at: "2026-01-05"
  - user: alice@corp.com
    token: %s
    role: auditor
    issued_at: "2026-02-01"
    expires: "2030-01-01"
`, e2eAdminToken, e2eMemberToken),
		"members.yml": fmt.Sprintf(`members:
  - id: %s
    email: alice@corp.com
    display_name: Alice
    status: active
    created_at: "2026-02-01T09:00:00Z"
  - id: %s
    email: bob@corp.com
    display_name: Bob
    status: disabled
    disabled_from: invited
    created_at: "2026-03-01T09:00:00Z"
`, e2eAliceID, e2eBobID),
		"invites.yml": fmt.Sprintf(`invites:
  - handle: %s
    lease_id: %s
    member_id: %s
    email: alice@corp.com
    token_value: %s
    role: member
    creation_path: admin.member.invite
    created_at: "2026-07-01T00:00:00Z"
    expires_at: "2030-01-01T00:00:00Z"
    status: pending
`, e2eHandle, e2eLease, e2eAliceID, e2eInviteToken),
		"groups.yml": `groups:
  - id: root-group-1
    name: Engineering
    created_at: "2026-01-01T00:00:00Z"
  - id: child-group-1
    name: Backend
    parent_id: root-group-1
    created_at: "2026-01-02T00:00:00Z"
`,
		"memberships.yml": fmt.Sprintf(`memberships:
  - user: %s
    group_id: child-group-1
    role: write
    granted_by: owner@corp.com
    granted_at: "2026-02-02T00:00:00Z"
`, e2eAliceID),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// startDaemon boots the runeconsole binary against confPath, teeing output
// to logPath, and registers a kill safety net.
func startDaemon(t *testing.T, binary, confPath, logPath string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(binary, "--config", confPath, "daemon", "start")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() }) // no-op after a clean Wait
	return cmd
}

// dumpDaemonLog attaches the daemon log to the test output on failure paths.
func dumpDaemonLog(t *testing.T, logPath string) {
	t.Helper()
	if data, err := os.ReadFile(logPath); err == nil {
		t.Logf("daemon log (%s):\n%s", logPath, data)
	}
}

// awaitHealthz polls the console /healthz until it answers 200. First boot
// includes FHE key generation, so the budget is generous.
func awaitHealthz(t *testing.T, consolePort int, logPath string) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", consolePort)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := client.Get(url); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	dumpDaemonLog(t, logPath)
	t.Fatalf("daemon not ready (%s)", url)
}

// stopDaemon SIGTERMs the daemon and requires a clean (zero) exit.
func stopDaemon(t *testing.T, cmd *exec.Cmd, logPath string) {
	t.Helper()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal daemon: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case werr := <-done:
		if werr != nil {
			dumpDaemonLog(t, logPath)
			t.Fatalf("daemon exited uncleanly after SIGTERM: %v", werr)
		}
	case <-time.After(30 * time.Second):
		dumpDaemonLog(t, logPath)
		_ = cmd.Process.Kill()
		t.Fatal("daemon did not exit within 30s of SIGTERM")
	}
}

// TestDaemonImportsLegacyYAMLAtBoot is the daemon-level wiring E2E for the
// YAML→SQLite migration (the storedb unit tests already cover import
// fidelity): a first boot over a seeded legacy data directory must import
// every store in one shot and park the files as *.yml.migrated; the database
// must hold the imported rows; a second boot with the version row present
// must come up clean, leave the .migrated files alone, and actually SERVE
// the imported state (gRPC GetAgentManifest authenticated by an imported
// token against an imported role).
func TestDaemonImportsLegacyYAMLAtBoot(t *testing.T) {
	binary := os.Getenv("RUNECONSOLE_TEST_BINARY")
	if binary == "" {
		t.Skip("RUNECONSOLE_TEST_BINARY not set — run `mise run go:build` then `mise run go:test:e2e`")
	}

	dir := t.TempDir()
	grpcPort := freePort(t)
	consolePort := freePort(t)
	writeLegacyYAML(t, dir)

	// Same minimal boot config shape as TestDaemonBootsAndShutsDownCleanly;
	// the four extra store files default beside tokens_file.
	conf := fmt.Sprintf(`server:
  grpc: { host: 127.0.0.1, port: %d, tls: { disable: true } }
  console: { enabled: true, port: %d }
cloud: { api_base_url: https://cloud.invalid, web_base_url: https://cloud.invalid }
keys: { path: %s, embedding_dim: 1024 }
runespace: { endpoint: "" }
tokens: { team_secret: e2e-team-secret, roles_file: %s, tokens_file: %s }
members: { console_endpoint: "127.0.0.1:%d" }
audit: { mode: stdout }
`,
		grpcPort, consolePort,
		filepath.Join(dir, "keys"),
		filepath.Join(dir, "roles.yml"), filepath.Join(dir, "tokens.yml"),
		grpcPort)
	confPath := filepath.Join(dir, "runeconsole.conf")
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	// ── First boot: import, rename, serve, shut down ────────────────
	log1 := filepath.Join(dir, "daemon-boot1.log")
	cmd1 := startDaemon(t, binary, confPath, log1)
	awaitHealthz(t, consolePort, log1)

	// The import runs before the stores are constructed, so a ready daemon
	// implies the renames already happened: every seeded *.yml is parked as
	// *.yml.migrated and the original is gone.
	for _, name := range legacyYAMLBasenames {
		orig := filepath.Join(dir, name)
		if _, err := os.Stat(orig); !os.IsNotExist(err) {
			dumpDaemonLog(t, log1)
			t.Errorf("%s still present after import (stat err=%v), want renamed away", name, err)
		}
		if _, err := os.Stat(orig + ".migrated"); err != nil {
			dumpDaemonLog(t, log1)
			t.Errorf("%s.migrated missing after import: %v", name, err)
		}
	}
	stopDaemon(t, cmd1, log1)

	// ── Database content: the imported rows are there ───────────────
	// Read-only inspection between boots, on the daemon's own file.
	assertImportedRows(t, filepath.Join(dir, "runeconsole.db"))

	// ── Second boot: version row present → warn-and-ignore, stores read
	// the database, imported state serves ───────────────────────────
	log2 := filepath.Join(dir, "daemon-boot2.log")
	cmd2 := startDaemon(t, binary, confPath, log2)
	awaitHealthz(t, consolePort, log2)

	// The .migrated artifacts are left alone and no *.yml reappears.
	for _, name := range legacyYAMLBasenames {
		if _, err := os.Stat(filepath.Join(dir, name+".migrated")); err != nil {
			t.Errorf("second boot disturbed %s.migrated: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("second boot rematerialized %s (stat err=%v)", name, err)
		}
	}

	// Serving check on a daemon that never saw the YAML. GetPermissions with
	// the imported member token exercises every imported store in one RPC:
	// tokens+roles (Validate), members (email → member-UUID resolution), and
	// groups+memberships (the judge view must show alice's write grant).
	// LookupWrap with the imported handle proves the pending invite —
	// including its still-sealed state — serves too. An unknown token is
	// refused.
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	defer func() { _ = conn.Close() }()
	client := pb.NewConsoleServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	perms, err := client.GetPermissions(ctx, &pb.GetPermissionsRequest{Token: e2eMemberToken})
	if err != nil {
		dumpDaemonLog(t, log2)
		t.Fatalf("GetPermissions with imported member token: %v", err)
	}
	if perms.GetMe() != "alice@corp.com" {
		t.Errorf("permissions me = %q, want alice@corp.com", perms.GetMe())
	}
	foundGrant := false
	for _, m := range perms.GetMemberships() {
		if m.GetGroupId() == "child-group-1" && m.GetRole() == "write" {
			foundGrant = true
		}
	}
	if !foundGrant {
		t.Errorf("imported membership missing from permissions view: %+v", perms.GetMemberships())
	}

	wrap, err := client.LookupWrap(ctx, &pb.LookupWrapRequest{Handle: e2eHandle})
	if err != nil {
		dumpDaemonLog(t, log2)
		t.Fatalf("LookupWrap with imported handle: %v", err)
	}
	if wrap.GetEmail() != "alice@corp.com" || wrap.GetRole() != "member" {
		t.Errorf("wrap = (%q, %q), want (alice@corp.com, member)", wrap.GetEmail(), wrap.GetRole())
	}

	if _, err := client.GetPermissions(ctx, &pb.GetPermissionsRequest{Token: "evt_00000000000000000000000000000000"}); err == nil {
		t.Error("GetPermissions with an unknown token succeeded, want Unauthenticated")
	}

	stopDaemon(t, cmd2, log2)
}

// assertImportedRows opens the store database directly (modernc driver, same
// strict open the daemon uses) and checks one signature row per store.
func assertImportedRows(t *testing.T, dbPath string) {
	t.Helper()
	database, err := db.OpenStrict(dbPath)
	if err != nil {
		t.Fatalf("open store db for inspection: %v", err)
	}
	defer func() { _ = database.Close() }()

	queryInt := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := database.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		return n
	}

	if n := queryInt(`SELECT COUNT(*) FROM schema_migrations WHERE version = 1`); n != 1 {
		t.Errorf("schema_migrations version-1 rows = %d, want 1", n)
	}
	if n := queryInt(`SELECT COUNT(*) FROM import_journal`); n != len(legacyYAMLBasenames) {
		t.Errorf("import_journal rows = %d, want %d", n, len(legacyYAMLBasenames))
	}

	// members: both rows, with the disabled marker intact.
	if n := queryInt(`SELECT COUNT(*) FROM members`); n != 2 {
		t.Errorf("members rows = %d, want 2", n)
	}
	var bobStatus, bobFrom string
	if err := database.QueryRow(
		`SELECT status, disabled_from FROM members WHERE id = ?`, e2eBobID).Scan(&bobStatus, &bobFrom); err != nil {
		t.Fatalf("read bob: %v", err)
	}
	if bobStatus != "disabled" || bobFrom != "invited" {
		t.Errorf("bob = (%q, %q), want (disabled, invited)", bobStatus, bobFrom)
	}

	// invites: the pending envelope kept its sealed plaintext.
	var invStatus, invToken string
	if err := database.QueryRow(
		`SELECT status, token_value FROM invites WHERE handle = ?`, e2eHandle).Scan(&invStatus, &invToken); err != nil {
		t.Fatalf("read invite: %v", err)
	}
	if invStatus != "pending" || invToken != e2eInviteToken {
		t.Errorf("invite = (%q, %q), want (pending, sealed plaintext)", invStatus, invToken)
	}

	// roles: the custom role plus the merged defaults.
	var auditorTopK int
	if err := database.QueryRow(`SELECT top_k FROM roles WHERE name = 'auditor'`).Scan(&auditorTopK); err != nil {
		t.Fatalf("read auditor role: %v", err)
	}
	if auditorTopK != 7 {
		t.Errorf("auditor top_k = %d, want 7", auditorTopK)
	}
	if n := queryInt(`SELECT COUNT(*) FROM roles WHERE name IN ('admin','member')`); n != 2 {
		t.Errorf("default role rows = %d, want 2", n)
	}

	// tokens: both secrets imported verbatim; '' expires became NULL.
	var aliceExpires sql.NullString
	if err := database.QueryRow(
		`SELECT expires FROM tokens WHERE token = ?`, e2eMemberToken).Scan(&aliceExpires); err != nil {
		t.Fatalf("read alice token: %v", err)
	}
	if aliceExpires.String != "2030-01-01" {
		t.Errorf("alice expires = %q, want 2030-01-01", aliceExpires.String)
	}
	var ownerExpires sql.NullString
	if err := database.QueryRow(
		`SELECT expires FROM tokens WHERE token = ?`, e2eAdminToken).Scan(&ownerExpires); err != nil {
		t.Fatalf("read owner token: %v", err)
	}
	if ownerExpires.Valid {
		t.Errorf("owner expires = %q, want NULL (never)", ownerExpires.String)
	}

	// groups tree + the UUID-keyed membership.
	if n := queryInt(`SELECT COUNT(*) FROM groups`); n != 2 {
		t.Errorf("groups rows = %d, want 2", n)
	}
	if n := queryInt(
		`SELECT COUNT(*) FROM memberships WHERE user = ? AND group_id = 'child-group-1' AND role = 'write'`,
		e2eAliceID); n != 1 {
		t.Errorf("membership rows = %d, want 1", n)
	}
}
