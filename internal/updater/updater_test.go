package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestVersionOrdering(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "1.0.0", 0},
		{"v1.0.1", "v1.0.0", 1},
		{"v2.0.0", "v10.0.0", -1},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"v1.0.0+build.1", "v1.0.0+build.2", 0},
	}
	for _, test := range tests {
		a, err := ParseVersion(test.a)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", test.a, err)
		}
		b, err := ParseVersion(test.b)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", test.b, err)
		}
		if got := a.Compare(b); got != test.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", test.a, test.b, got, test.want)
		}
	}
	for _, invalid := range []string{"", "dev", "v1.0", "v01.0.0", "v1.0.0-01", "v1.0.0+"} {
		if _, err := ParseVersion(invalid); err == nil {
			t.Errorf("ParseVersion(%q) succeeded", invalid)
		}
	}
}

func TestEngineHappyPathPreservesStateAndPreviousBinary(t *testing.T) {
	t.Parallel()
	env := newUpdateTestEnvironment(t, "v1.1.0")
	events := []string{}
	service := &fakeService{events: &events}
	health := &fakeHealth{events: &events}
	lock := &fakeLocker{events: &events}
	engine := testEngine(env, service, health, lock, scriptVerifier{})

	before := captureState(t, env.statePaths)
	result, err := engine.Run(context.Background(), env.request(false))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Updated || result.RolledBack || !result.Checked {
		t.Fatalf("result = %+v", result)
	}
	if got, want := events, []string{"lock", "stop", "start", "health"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	assertFileContains(t, env.binary, "v1.1.0")
	assertFileContains(t, env.binary+".previous", "v1.0.0")
	assertState(t, env.statePaths, before)
	assertPrivateBackup(t, env.backupDir, result.BackupPath)
}

func TestEngineHealthFailureRestoresBinaryAndAllStateInOrder(t *testing.T) {
	t.Parallel()
	env := newUpdateTestEnvironment(t, "v1.2.0")
	events := []string{}
	service := &fakeService{
		events: &events,
		onStart: func(start int) {
			if start == 1 {
				mutateState(t, env)
			}
		},
	}
	health := &fakeHealth{events: &events, errors: []error{errors.New("new daemon unhealthy"), nil}}
	engine := testEngine(env, service, health, &fakeLocker{events: &events}, scriptVerifier{})
	before := captureState(t, env.statePaths)

	result, err := engine.Run(context.Background(), env.request(false))
	if err == nil || !strings.Contains(err.Error(), "previous binary and state were restored") {
		t.Fatalf("Run error = %v", err)
	}
	if !result.RolledBack || result.Updated {
		t.Fatalf("result = %+v", result)
	}
	wantEvents := []string{"lock", "stop", "start", "health", "stop", "start", "health"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
	assertFileContains(t, env.binary, "v1.0.0")
	assertFileContains(t, env.binary+".previous", "v1.0.0")
	assertState(t, env.statePaths, before)
}

func TestRollbackUsesRecoveryContextAfterRequestCancellation(t *testing.T) {
	t.Parallel()
	env := newUpdateTestEnvironment(t, "v1.2.0")
	events := []string{}
	ctx, cancel := context.WithCancel(context.Background())
	service := &fakeService{events: &events, requireLiveContext: true}
	health := &fakeHealth{
		events:             &events,
		errors:             []error{errors.New("request canceled during health check"), nil},
		requireLiveContext: true,
		onCall: func(call int) {
			if call == 1 {
				cancel()
			}
		},
	}
	engine := testEngine(env, service, health, &fakeLocker{events: &events}, scriptVerifier{})
	result, err := engine.Run(ctx, env.request(false))
	if err == nil || !result.RolledBack || strings.Contains(err.Error(), "rollback was incomplete") {
		t.Fatalf("Run result = %+v, error = %v", result, err)
	}
	assertFileContains(t, env.binary, "v1.0.0")
}

func TestRollbackFailsClosedWhenCandidateCannotStop(t *testing.T) {
	t.Parallel()
	env := newUpdateTestEnvironment(t, "v1.2.0")
	events := []string{}
	service := &fakeService{
		events:     &events,
		stopErrors: []error{nil, errors.New("stop failed")},
		onStart: func(start int) {
			if start == 1 {
				mutateState(t, env)
			}
		},
	}
	health := &fakeHealth{events: &events, errors: []error{errors.New("unhealthy"), nil}}
	engine := testEngine(env, service, health, &fakeLocker{events: &events}, scriptVerifier{})
	before := captureState(t, env.statePaths)

	result, err := engine.Run(context.Background(), env.request(false))
	if err == nil || !strings.Contains(err.Error(), "rollback was incomplete") {
		t.Fatalf("Run error = %v", err)
	}
	if !result.RolledBack {
		t.Fatalf("result = %+v", result)
	}
	assertFileContains(t, env.binary, "v1.2.0")
	if got, want := events, []string{"lock", "stop", "start", "health", "stop"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback did not fail closed: got %v, want %v", got, want)
	}
	if reflect.DeepEqual(captureState(t, env.statePaths), before) {
		t.Fatal("state was restored even though the candidate could not be stopped")
	}
}

func TestInstallCandidatePartialFailureUsesRollback(t *testing.T) {
	t.Parallel()
	env := newUpdateTestEnvironment(t, "v1.1.0")
	events := []string{}
	verifier := verifierFunc(func(_ context.Context, candidate, _, _, _ string) error {
		// Candidate verification succeeds, then a simulated disk/race failure
		// makes installation fail after .previous was saved.
		return os.Remove(candidate)
	})
	engine := testEngine(env, &fakeService{events: &events}, &fakeHealth{events: &events}, &fakeLocker{events: &events}, verifier)
	before := captureState(t, env.statePaths)

	result, err := engine.Run(context.Background(), env.request(false))
	if err == nil || !result.RolledBack {
		t.Fatalf("Run result = %+v, error = %v", result, err)
	}
	if got, want := events, []string{"lock", "stop", "start", "health"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	assertFileContains(t, env.binary, "v1.0.0")
	assertState(t, env.statePaths, before)
}

func TestCheckOnlyHasNoServiceStateLockOrBinaryMutation(t *testing.T) {
	t.Parallel()
	env := newUpdateTestEnvironment(t, "v1.1.0")
	service := &fakeService{failOnUse: true}
	health := &fakeHealth{failOnUse: true}
	locker := &fakeLocker{failOnUse: true}
	engine := testEngine(env, service, health, locker, scriptVerifier{})
	before := captureState(t, env.statePaths)
	binaryBefore := readFile(t, env.binary)

	result, err := engine.Run(context.Background(), env.request(true))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Checked || result.Updated || result.BackupPath != "" {
		t.Fatalf("result = %+v", result)
	}
	if got := readFile(t, env.binary); got != binaryBefore {
		t.Fatal("check-only changed binary")
	}
	assertState(t, env.statePaths, before)
	if _, err := os.Stat(env.backupDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check-only created backup/lock directory: %v", err)
	}
}

func TestConfigChangeDuringReleasePreparationAbortsAndRecoversOriginalService(t *testing.T) {
	t.Parallel()
	env := newUpdateTestEnvironment(t, "v1.1.0")
	config := env.statePaths[0]
	events := []string{}
	service := &fakeService{events: &events}
	health := &fakeHealth{events: &events}
	source := releaseSourceFunc(func(ctx context.Context, requested, goos, goarch string) (Release, error) {
		release, err := env.source.Resolve(ctx, requested, goos, goarch)
		if err == nil {
			writeFile(t, config, []byte("administrator-edited-config"), 0o640)
		}
		return release, err
	})
	engine := &Engine{Source: source, Service: service, Health: health, Locker: &fakeLocker{events: &events}, Verifier: scriptVerifier{}}
	request := env.request(false)
	request.ImmutablePaths = []string{config}
	_, err := engine.Run(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("Run error = %v", err)
	}
	if got, want := events, []string{"lock", "stop", "start", "health"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	assertFileContains(t, env.binary, "v1.0.0")
	if got := readFile(t, config); got != "administrator-edited-config" {
		t.Fatalf("updater overwrote concurrent config edit: %q", got)
	}
}

func TestDowngradeAndCandidateMismatchNeverStopService(t *testing.T) {
	t.Parallel()
	t.Run("downgrade", func(t *testing.T) {
		env := newUpdateTestEnvironment(t, "v0.9.0")
		service := &fakeService{failOnUse: true}
		engine := testEngine(env, service, &fakeHealth{failOnUse: true}, &fakeLocker{}, scriptVerifier{})
		_, err := engine.Run(context.Background(), env.request(false))
		if !errors.Is(err, ErrDowngrade) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("candidate mismatch", func(t *testing.T) {
		env := newUpdateTestEnvironment(t, "v1.1.0")
		env.source.Version = "v1.2.0"
		service := &fakeService{failOnUse: true}
		engine := testEngine(env, service, &fakeHealth{failOnUse: true}, &fakeLocker{}, scriptVerifier{})
		_, err := engine.Run(context.Background(), env.request(false))
		if err == nil || !strings.Contains(err.Error(), "does not match release") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestFileLockerRejectsConcurrentUpdate(t *testing.T) {
	t.Parallel()
	lock := FileLocker{Path: filepath.Join(t.TempDir(), "private", "update.lock")}
	unlock, err := lock.Lock()
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	defer unlock()
	if _, err := lock.Lock(); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock error = %v, want ErrLocked", err)
	}
}

func TestFileLockerRejectsFinalSymlinkWithoutChangingTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lockDir := filepath.Join(root, "private")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "sensitive")
	writeFile(t, target, []byte("do-not-touch"), 0o640)
	if err := os.Symlink(target, filepath.Join(lockDir, "update.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileLocker{Path: filepath.Join(lockDir, "update.lock")}).Lock(); err == nil {
		t.Fatal("symlink lock path was accepted")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 || readFile(t, target) != "do-not-touch" {
		t.Fatal("lock attempt changed symlink target")
	}
}

func TestValidateUpdatePathsRejectsBroadAndOverlappingPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binary := writeExecutable(t, filepath.Join(root, "bin", "runeconsole"), "v1.0.0")
	if err := ValidateUpdatePaths(binary, filepath.Join(root, "backups"), []string{filepath.Join(root, "data", "db")}, []string{"/"}); err == nil {
		t.Fatal("managed root / was accepted")
	}
	state := filepath.Join(root, "state")
	if err := ValidateUpdatePaths(binary, filepath.Join(state, "backups"), []string{state}, []string{state}); err == nil {
		t.Fatal("backup inside state was accepted")
	}
}

func TestManagedRootRequiresOwnedMarkerAndRejectsNestedSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binary := writeExecutable(t, filepath.Join(root, "bin", "runeconsole"), "v1.0.0")
	managed := filepath.Join(root, "state", "data")
	if err := os.MkdirAll(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backups")
	binary, _ = filepath.EvalSymlinks(binary)
	managedPaths, err := canonicalizeDistinctPaths([]string{managed})
	if err != nil {
		t.Fatal(err)
	}
	managed = managedPaths[0]
	if err := ValidateUpdatePaths(binary, backup, []string{managed}, []string{managed}); err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("missing marker error = %v", err)
	}
	writeFile(t, filepath.Join(managed, ManagedRootMarker), []byte(ManagedRootMarkerContent), 0o600)
	if err := ValidateUpdatePaths(binary, backup, []string{managed}, []string{managed}); err != nil {
		t.Fatalf("valid managed root: %v", err)
	}
	outside := filepath.Join(root, "outside")
	writeFile(t, outside, []byte("outside"), 0o600)
	if err := os.Symlink(outside, filepath.Join(managed, "nested-link")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateUpdatePaths(binary, backup, []string{managed}, []string{managed}); err == nil || !strings.Contains(err.Error(), "unsafe entry") {
		t.Fatalf("nested symlink error = %v", err)
	}
}

func TestResolveServiceBinaryFailsClosedForCopiedCLI(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	serviceBinary := writeExecutable(t, filepath.Join(root, "installed", "runeconsole"), "v1.0.0")
	copyBinary := writeExecutable(t, filepath.Join(root, "copy", "runeconsole"), "v1.0.0")
	config := filepath.Join(root, "runeconsole.conf")
	writeFile(t, config, []byte("config"), 0o600)
	unit := filepath.Join(root, "runeconsole.service")
	writeFile(t, unit, []byte("[Service]\nExecStart=\""+serviceBinary+"\" daemon start --config \""+config+"\"\n"), 0o600)
	runner := commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("systemctl unavailable in test")
	})
	_, err := ResolveServiceBinary(context.Background(), BinaryResolutionOptions{
		GOOS:               "linux",
		ExecutablePath:     copyBinary,
		ExpectedConfigPath: config,
		Runner:             runner,
		SystemdUnitPaths:   []string{unit},
	})
	if err == nil || !strings.Contains(err.Error(), "not the service binary") {
		t.Fatalf("copied CLI error = %v", err)
	}
	resolved, err := ResolveServiceBinary(context.Background(), BinaryResolutionOptions{
		GOOS:               "linux",
		ExplicitPath:       serviceBinary,
		ExecutablePath:     copyBinary,
		ExpectedConfigPath: config,
		Runner:             runner,
		SystemdUnitPaths:   []string{unit},
	})
	wantResolved, resolveErr := filepath.EvalSymlinks(serviceBinary)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || resolved != wantResolved {
		t.Fatalf("explicit path resolved = %q, error = %v", resolved, err)
	}
}

func TestResolveServiceBinaryUsesSystemctlAndLaunchdDefinitions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binary := writeExecutable(t, filepath.Join(root, "bin", "runeconsole"), "v1.0.0")
	config := filepath.Join(root, "runeconsole.conf")
	writeFile(t, config, []byte("config"), 0o600)
	systemctlRunner := commandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("{ path=" + binary + " ; argv[]=" + binary + " daemon start --config " + config + " ; ignore_errors=no ; }\n"), nil
	})
	got, err := ResolveServiceBinary(context.Background(), BinaryResolutionOptions{
		GOOS: "linux", ExecutablePath: binary, ExpectedConfigPath: config, Runner: systemctlRunner,
	})
	wantResolved, resolveErr := filepath.EvalSymlinks(binary)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || got != wantResolved {
		t.Fatalf("systemctl resolve = %q, %v", got, err)
	}
	plist := filepath.Join(root, "service.plist")
	writeFile(t, plist, []byte(`<?xml version="1.0"?><plist><dict><key>ProgramArguments</key><array><string>`+binary+`</string><string>daemon</string><string>start</string><string>--config</string><string>`+config+`</string></array></dict></plist>`), 0o600)
	launchRunner := commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("program = " + binary + "\narguments = {\n" + binary + "\ndaemon\nstart\n--config\n" + config + "\n}\n"), nil
	})
	got, err = ResolveServiceBinary(context.Background(), BinaryResolutionOptions{
		GOOS: "darwin", ExecutablePath: binary, ExpectedConfigPath: config, Runner: launchRunner, LaunchdPlistPaths: []string{plist},
	})
	if err != nil || got != wantResolved {
		t.Fatalf("launchd resolve = %q, %v", got, err)
	}
}

func TestResolveServiceBinaryRejectsConfigMismatchAndMissingDescriptor(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binary := writeExecutable(t, filepath.Join(root, "bin", "runeconsole"), "v1.0.0")
	configA := filepath.Join(root, "a.conf")
	configB := filepath.Join(root, "b.conf")
	writeFile(t, configA, []byte("a"), 0o600)
	writeFile(t, configB, []byte("b"), 0o600)
	runner := commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("{ path=" + binary + " ; argv[]=" + binary + " daemon start --config " + configA + " ; ignore_errors=no ; }"), nil
	})
	_, err := ResolveServiceBinary(context.Background(), BinaryResolutionOptions{
		GOOS: "linux", ExecutablePath: binary, ExpectedConfigPath: configB, Runner: runner,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("config mismatch error = %v", err)
	}

	missingRunner := commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("systemctl unavailable")
	})
	_, err = ResolveServiceBinary(context.Background(), BinaryResolutionOptions{
		GOOS: "linux", ExecutablePath: binary, ExpectedConfigPath: configA, Runner: missingRunner,
	})
	if err == nil || !strings.Contains(err.Error(), "could not identify") {
		t.Fatalf("missing descriptor error = %v", err)
	}

	plist := filepath.Join(root, "mismatch.plist")
	writeFile(t, plist, []byte(`<?xml version="1.0"?><plist><dict><key>ProgramArguments</key><array><string>`+binary+`</string><string>daemon</string><string>start</string><string>--config</string><string>`+configA+`</string></array></dict></plist>`), 0o600)
	launchRunner := commandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("program = " + binary + "\narguments = {\n" + binary + "\ndaemon\nstart\n--config\n" + configA + "\n}\n"), nil
	})
	_, err = ResolveServiceBinary(context.Background(), BinaryResolutionOptions{
		GOOS: "darwin", ExecutablePath: binary, ExpectedConfigPath: configB, Runner: launchRunner, LaunchdPlistPaths: []string{plist},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("launchd config mismatch error = %v", err)
	}
}

func TestSystemServiceDarwinStopErrorFailsClosed(t *testing.T) {
	t.Parallel()
	runner := commandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "launchctl" || len(args) == 0 || args[0] != "bootout" {
			t.Fatalf("unexpected command %s %v", name, args)
		}
		return nil, errors.New("unknown launchd failure")
	})
	if err := (SystemService{GOOS: "darwin", Runner: runner}).Stop(context.Background()); err == nil {
		t.Fatal("unknown launchctl failure was treated as an inactive service")
	}
}

func TestLocalArchiveSizeLimit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "too-large.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxArchiveBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArchiveChecksum(path, filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("error = %v", err)
	}
}

func TestGitHubSourceRejectsUnavailableDarwinAMD64Artifact(t *testing.T) {
	t.Parallel()
	_, err := (GitHubSource{}).Resolve(context.Background(), "v1.1.0", "darwin", "amd64")
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestEngineRejectsOfflineDarwinAMD64Update(t *testing.T) {
	t.Parallel()
	env := newUpdateTestEnvironment(t, "v1.1.0")
	request := env.request(true)
	request.GOOS, request.GOARCH = "darwin", "amd64"
	engine := testEngine(env, &fakeService{failOnUse: true}, &fakeHealth{failOnUse: true}, &fakeLocker{failOnUse: true}, scriptVerifier{})
	if _, err := engine.Run(context.Background(), request); err == nil || !strings.Contains(err.Error(), "darwin/amd64") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildInfoVerifierReadsQuotedReleaseLDFlagsWithoutExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a tiny Go executable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	sentinel := filepath.Join(dir, "candidate-executed")
	sourceBody := fmt.Sprintf("package main\nimport \"os\"\nvar buildVersion = \"dev\"\nfunc main(){_ = os.WriteFile(%q, []byte(buildVersion), 0600)}\n", sentinel)
	writeFile(t, source, []byte(sourceBody), 0o600)
	candidate := filepath.Join(dir, "runeconsole")
	cmd := exec.Command("go", "build", "-ldflags", "-s -w -X 'main.buildVersion=v9.9.9'", "-o", candidate, source)
	cmd.Env = append(os.Environ(), "GO111MODULE=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, output)
	}
	verifier := BuildInfoVerifier{VersionVariable: "main.buildVersion"}
	if err := verifier.Verify(context.Background(), candidate, "v9.9.9", runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("static verifier executed the candidate: %v", err)
	}
	if err := verifier.Verify(context.Background(), candidate, "v9.9.8", runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("wrong release version was accepted")
	}
	if err := verifier.Verify(context.Background(), candidate, "v9.9.9", "wrong-os", runtime.GOARCH); err == nil {
		t.Fatal("wrong target operating system was accepted")
	}
}

func TestRestoreStagesAndValidatesArchiveBeforeChangingLiveState(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		headers []tar.Header
	}{
		{name: "missing root"},
		{name: "negative index", headers: []tar.Header{{Name: "state/-00001", Typeflag: tar.TypeReg, Mode: 0o600, Size: 1}}},
		{name: "symlink root", headers: []tar.Header{{Name: "state/000000", Typeflag: tar.TypeSymlink, Linkname: "/tmp", Mode: 0o777}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			state := filepath.Join(root, "state.conf")
			writeFile(t, state, []byte("original"), 0o640)
			snapshot, err := BackupState(filepath.Join(root, "backups"), []string{state})
			if err != nil {
				t.Fatalf("BackupState: %v", err)
			}
			writeFile(t, state, []byte("candidate-live"), 0o640)
			overwriteSnapshotArchive(t, snapshot, test.headers)
			if err := snapshot.Restore(); err == nil {
				t.Fatal("unsafe archive was restored")
			}
			if got := readFile(t, state); got != "candidate-live" {
				t.Fatalf("live state changed before archive validation: %q", got)
			}
		})
	}
}

func TestRestoreLeavesUnchangedTopLevelFileInPlace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	config := filepath.Join(root, "runeconsole.conf")
	writeFile(t, config, []byte("unchanged-config"), 0o640)
	before, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := BackupState(filepath.Join(root, "backups"), []string{config})
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("unchanged config inode was replaced")
	}
}

type updateTestEnvironment struct {
	root       string
	binary     string
	backupDir  string
	dataDir    string
	realKeys   string
	keysLink   string
	statePaths []string
	source     LocalSource
}

func newUpdateTestEnvironment(t *testing.T, candidateVersion string) *updateTestEnvironment {
	t.Helper()
	root := t.TempDir()
	env := &updateTestEnvironment{
		root:      root,
		backupDir: filepath.Join(root, "backups"),
		dataDir:   filepath.Join(root, "state", "data"),
		realKeys:  filepath.Join(root, "state", "actual-keys"),
		keysLink:  filepath.Join(root, "state", "configured-keys"),
	}
	env.binary = writeExecutable(t, filepath.Join(root, "bin", "runeconsole"), "v1.0.0")
	for _, dir := range []string{env.dataDir, env.realKeys, filepath.Join(root, "state", "tls")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(env.realKeys, env.keysLink); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.dataDir, ManagedRootMarker), []byte(ManagedRootMarkerContent), 0o600)
	writeFile(t, filepath.Join(env.realKeys, ManagedRootMarker), []byte(ManagedRootMarkerContent), 0o600)
	paths := []string{
		filepath.Join(root, "state", "runeconsole.conf"),
		env.dataDir,
		filepath.Join(env.dataDir, "runeconsole.db"),
		filepath.Join(env.dataDir, "runeconsole.db-wal"),
		filepath.Join(env.dataDir, "runeconsole.db-shm"), // deliberately absent
		filepath.Join(env.dataDir, "console-session.db"),
		filepath.Join(env.dataDir, "console-session.db-wal"),
		filepath.Join(env.dataDir, "console-session.db-shm"),
		env.keysLink,
		filepath.Join(root, "state", "tls", "server.pem"),
		filepath.Join(root, "state", "tls", "server.key"),
		filepath.Join(root, "state", "tls", "ca.pem"),
	}
	for i, path := range paths {
		if path == env.keysLink || path == env.dataDir || strings.HasSuffix(path, "runeconsole.db-shm") {
			continue
		}
		writeFile(t, path, []byte(fmt.Sprintf("state-%d", i)), 0o640)
	}
	writeFile(t, filepath.Join(env.realKeys, "private.key"), []byte("secret-key"), 0o600)
	env.statePaths = paths
	archive, sums := makeRelease(t, root, candidateVersion)
	env.source = LocalSource{ArchivePath: archive, ChecksumsPath: sums, Version: candidateVersion}
	return env
}

func (e *updateTestEnvironment) request(check bool) Request {
	return Request{
		CurrentVersion:   "v1.0.0",
		RequestedVersion: e.source.Version,
		GOOS:             "linux",
		GOARCH:           "amd64",
		BinaryPath:       e.binary,
		PreviousPath:     e.binary + ".previous",
		BackupDir:        e.backupDir,
		StatePaths:       e.statePaths,
		ManagedRoots:     []string{e.dataDir, e.keysLink},
		CheckOnly:        check,
	}
}

func testEngine(env *updateTestEnvironment, service Service, health HealthChecker, lock Locker, verifier CandidateVerifier) *Engine {
	return &Engine{Source: env.source, Service: service, Health: health, Locker: lock, Verifier: verifier}
}

type fakeService struct {
	events             *[]string
	stops              int
	starts             int
	stopErrors         []error
	startErrors        []error
	onStart            func(int)
	failOnUse          bool
	requireLiveContext bool
}

func (s *fakeService) Stop(ctx context.Context) error {
	if s.failOnUse {
		panic("service Stop called")
	}
	if s.requireLiveContext && ctx.Err() != nil {
		return ctx.Err()
	}
	if s.events != nil {
		*s.events = append(*s.events, "stop")
	}
	index := s.stops
	s.stops++
	if index < len(s.stopErrors) {
		return s.stopErrors[index]
	}
	return nil
}

func (s *fakeService) Start(ctx context.Context) error {
	if s.failOnUse {
		panic("service Start called")
	}
	if s.requireLiveContext && ctx.Err() != nil {
		return ctx.Err()
	}
	if s.events != nil {
		*s.events = append(*s.events, "start")
	}
	s.starts++
	if s.onStart != nil {
		s.onStart(s.starts)
	}
	if s.starts-1 < len(s.startErrors) {
		return s.startErrors[s.starts-1]
	}
	return nil
}

type fakeHealth struct {
	events             *[]string
	calls              int
	errors             []error
	failOnUse          bool
	requireLiveContext bool
	onCall             func(int)
}

func (h *fakeHealth) WaitHealthy(ctx context.Context) error {
	if h.failOnUse {
		panic("health called")
	}
	if h.requireLiveContext && ctx.Err() != nil {
		return ctx.Err()
	}
	if h.events != nil {
		*h.events = append(*h.events, "health")
	}
	index := h.calls
	h.calls++
	if h.onCall != nil {
		h.onCall(h.calls)
	}
	if index < len(h.errors) {
		return h.errors[index]
	}
	return nil
}

type fakeLocker struct {
	events    *[]string
	failOnUse bool
}

func (l *fakeLocker) Lock() (func() error, error) {
	if l.failOnUse {
		panic("lock called")
	}
	if l.events != nil {
		*l.events = append(*l.events, "lock")
	}
	return func() error { return nil }, nil
}

type verifierFunc func(context.Context, string, string, string, string) error

func (f verifierFunc) Verify(ctx context.Context, candidate, version, goos, goarch string) error {
	return f(ctx, candidate, version, goos, goarch)
}

type scriptVerifier struct{}

func (scriptVerifier) Verify(_ context.Context, candidate, version, _, _ string) error {
	content, err := os.ReadFile(candidate)
	if err != nil {
		return err
	}
	if !strings.Contains(string(content), version) {
		return fmt.Errorf("candidate binary version does not match release %s", version)
	}
	return nil
}

type commandRunnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f commandRunnerFunc) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}

type releaseSourceFunc func(context.Context, string, string, string) (Release, error)

func (f releaseSourceFunc) Resolve(ctx context.Context, version, goos, goarch string) (Release, error) {
	return f(ctx, version, goos, goarch)
}

type stateCapture struct {
	exists bool
	mode   os.FileMode
	uid    uint32
	gid    uint32
	data   string
	link   string
}

func captureState(t *testing.T, paths []string) map[string]stateCapture {
	t.Helper()
	canonical, err := CanonicalizePaths(paths)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]stateCapture, len(canonical))
	for _, path := range canonical {
		result[path] = captureOne(t, path)
	}
	return result
}

func captureOne(t *testing.T, path string) stateCapture {
	t.Helper()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return stateCapture{}
	}
	if err != nil {
		t.Fatal(err)
	}
	capture := stateCapture{exists: true, mode: info.Mode()}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		capture.uid, capture.gid = stat.Uid, stat.Gid
	}
	if info.Mode()&os.ModeSymlink != 0 {
		capture.link, err = os.Readlink(path)
	} else if info.IsDir() {
		var values []string
		err = filepath.Walk(path, func(child string, childInfo os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, _ := filepath.Rel(path, child)
			if childInfo.Mode()&os.ModeSymlink != 0 {
				target, readErr := os.Readlink(child)
				values = append(values, rel+"->"+target)
				return readErr
			}
			if childInfo.Mode().IsRegular() {
				data, readErr := os.ReadFile(child)
				uid, gid := uint32(0), uint32(0)
				if stat, ok := childInfo.Sys().(*syscall.Stat_t); ok {
					uid, gid = stat.Uid, stat.Gid
				}
				values = append(values, rel+":"+string(data)+fmt.Sprintf(":%04o:%d:%d", childInfo.Mode().Perm(), uid, gid))
				return readErr
			}
			values = append(values, rel+"/")
			return nil
		})
		capture.data = strings.Join(values, "|")
	} else {
		data, readErr := os.ReadFile(path)
		capture.data, err = string(data), readErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return capture
}

func assertState(t *testing.T, paths []string, before map[string]stateCapture) {
	t.Helper()
	for path, want := range before {
		if got := captureOne(t, path); !reflect.DeepEqual(got, want) {
			t.Errorf("state entry %d changed: got %+v, want %+v", indexOf(paths, path), got, want)
		}
	}
}

func indexOf(paths []string, target string) int {
	for i, path := range paths {
		if path == target {
			return i
		}
	}
	return -1
}

func mutateState(t *testing.T, env *updateTestEnvironment) {
	t.Helper()
	canonical, err := CanonicalizePaths(env.statePaths)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range canonical {
		info, statErr := os.Lstat(path)
		switch {
		case errors.Is(statErr, os.ErrNotExist):
			writeFile(t, path, []byte("newly-created"), 0o666)
		case statErr != nil:
			t.Fatal(statErr)
		case info.IsDir():
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(path, "replacement"), []byte("changed"), 0o666)
		default:
			writeFile(t, path, []byte("changed"), 0o666)
		}
	}
}

func assertPrivateBackup(t *testing.T, root, directory string) {
	t.Helper()
	for _, test := range []struct {
		path string
		mode os.FileMode
	}{
		{root, 0o700}, {directory, 0o700},
		{filepath.Join(directory, backupArchiveName), 0o600},
		{filepath.Join(directory, backupManifestName), 0o600},
	} {
		info, err := os.Stat(test.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != test.mode {
			t.Errorf("backup component mode = %04o, want %04o", got, test.mode)
		}
	}
}

func makeRelease(t *testing.T, dir, version string) (string, string) {
	t.Helper()
	archivePath := filepath.Join(dir, fmt.Sprintf("runeconsole_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH))
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	script := []byte("#!/bin/sh\necho runeconsole " + version + " '(test build)'\n")
	if err := tw.WriteHeader(&tar.Header{Name: "./runeconsole", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(script); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(tw.Close(), gz.Close(), f.Close()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	checksums := filepath.Join(dir, "SHA256SUMS")
	writeFile(t, checksums, []byte(fmt.Sprintf("%x  %s\n", sum, filepath.Base(archivePath))), 0o600)
	return archivePath, checksums
}

func overwriteSnapshotArchive(t *testing.T, snapshot *Snapshot, headers []tar.Header) {
	t.Helper()
	path := filepath.Join(snapshot.directory, backupArchiveName)
	f, err := os.OpenFile(path, os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for i := range headers {
		hdr := headers[i]
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Size > 0 {
			if _, err := tw.Write(make([]byte, hdr.Size)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := errors.Join(tw.Close(), f.Sync(), f.Close()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	snapshot.archiveSHA256 = fmt.Sprintf("%x", sum)
}

func writeExecutable(t *testing.T, path, version string) string {
	t.Helper()
	writeFile(t, path, []byte("#!/bin/sh\necho runeconsole "+version+"\n"), 0o755)
	return path
}

func writeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertFileContains(t *testing.T, path, substring string) {
	t.Helper()
	if data := readFile(t, path); !strings.Contains(data, substring) {
		t.Fatalf("file content does not contain %q", substring)
	}
}
