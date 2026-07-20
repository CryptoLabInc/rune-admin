package updater

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestGitHubLatestVersionFetchesMetadataOnly(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path != "/repos/acme/console/releases/latest" {
			http.Error(w, "asset endpoint must not be called", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"url":"/download/never"}]}`))
	}))
	defer server.Close()

	got, err := (GitHubSource{
		Client:     server.Client(),
		Repository: "acme/console",
		APIBaseURL: server.URL,
		WebBaseURL: server.URL + "/download-must-not-be-used",
	}).LatestVersion(context.Background())
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if got != "v1.2.3" {
		t.Fatalf("version = %q, want v1.2.3", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(paths, []string{"/repos/acme/console/releases/latest"}) {
		t.Fatalf("requested paths = %v", paths)
	}
}

func TestGitHubLatestVersionRejectsBadMetadata(t *testing.T) {
	t.Parallel()
	t.Run("non-canonical tag", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"tag_name":"1.2.3"}`))
		}))
		defer server.Close()
		_, err := (GitHubSource{Client: server.Client(), APIBaseURL: server.URL}).LatestVersion(context.Background())
		if err == nil || !strings.Contains(err.Error(), "non-canonical") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(maxMetadataBytes+1))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		_, err := (GitHubSource{Client: server.Client(), APIBaseURL: server.URL}).LatestVersion(context.Background())
		if err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestWebControllerNonCapableStatesNeverCheckNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		current        string
		goos, goarch   string
		enable         bool
		wantCurrentRaw string
	}{
		{name: "missing helper", current: "v1.0.0", goos: "linux", goarch: "amd64", wantCurrentRaw: "v1.0.0"},
		{name: "development build", current: "dev", goos: "linux", goarch: "amd64", enable: true, wantCurrentRaw: "dev"},
		{name: "unsupported platform", current: "v1.0.0", goos: "darwin", goarch: "amd64", enable: true, wantCurrentRaw: "v1.0.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newWebControlTestEnv(t, test.enable)
			calls := 0
			controller := NewWebController(WebControllerOptions{
				CurrentVersion: test.current,
				GOOS:           test.goos,
				GOARCH:         test.goarch,
				Paths:          env.paths,
				Source: latestVersionSourceFunc(func(context.Context) (string, error) {
					calls++
					return "v9.9.9", nil
				}),
			})
			status, err := controller.Check(context.Background())
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if status.Capable || status.UpdateAvailable || status.State != UpdateStateIdle || status.CurrentVersion != test.wantCurrentRaw {
				t.Fatalf("status = %+v", status)
			}
			if calls != 0 {
				t.Fatalf("metadata source called %d times", calls)
			}
		})
	}
}

func TestWebControllerCachesLatestAndRefreshesAfterTTL(t *testing.T) {
	t.Parallel()
	env := newWebControlTestEnv(t, true)
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	calls := 0
	controller := NewWebController(WebControllerOptions{
		CurrentVersion: "v1.0.0",
		GOOS:           "linux",
		GOARCH:         "amd64",
		Paths:          env.paths,
		CacheTTL:       10 * time.Minute,
		Now:            func() time.Time { return now },
		Source: latestVersionSourceFunc(func(context.Context) (string, error) {
			calls++
			return "v1.1.0", nil
		}),
	})
	for i := 0; i < 2; i++ {
		status, err := controller.Check(context.Background())
		if err != nil || !status.Capable || !status.UpdateAvailable || status.TargetVersion != "v1.1.0" {
			t.Fatalf("Check %d = %+v, %v", i, status, err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls inside TTL = %d, want 1", calls)
	}
	now = now.Add(10 * time.Minute)
	if _, err := controller.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls after TTL = %d, want 2", calls)
	}
}

func TestWebControllerCoalescesConcurrentChecks(t *testing.T) {
	t.Parallel()
	env := newWebControlTestEnv(t, true)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	calls := 0
	controller := NewWebController(WebControllerOptions{
		CurrentVersion: "v1.0.0",
		GOOS:           "linux",
		GOARCH:         "amd64",
		Paths:          env.paths,
		Source: latestVersionSourceFunc(func(context.Context) (string, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			once.Do(func() { close(started) })
			<-release
			return "v1.1.0", nil
		}),
	})

	const count = 20
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, err := controller.Check(context.Background())
			if err == nil && (!status.UpdateAvailable || status.TargetVersion != "v1.1.0") {
				err = fmt.Errorf("unexpected status: %+v", status)
			}
			errs <- err
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("metadata calls = %d, want 1", calls)
	}
}

func TestWebControllerCheckFailureIsFailClosed(t *testing.T) {
	t.Parallel()
	env := newWebControlTestEnv(t, true)
	controller := NewWebController(WebControllerOptions{
		CurrentVersion: "v1.0.0", GOOS: "linux", GOARCH: "amd64", Paths: env.paths,
		Source: latestVersionSourceFunc(func(context.Context) (string, error) {
			return "", errors.New("offline")
		}),
	})
	status, err := controller.Check(context.Background())
	if !errors.Is(err, ErrUpdateCheckFailed) {
		t.Fatalf("error = %v", err)
	}
	if !status.Capable || status.UpdateAvailable || status.TargetVersion != "" {
		t.Fatalf("status = %+v", status)
	}
}

func TestWebControllerRequestPublishesCompletePinnedJobAndKeepsQueuedState(t *testing.T) {
	t.Parallel()
	env := newWebControlTestEnv(t, true)
	now := time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	latest := "v1.1.0"
	calls := 0
	controller := NewWebController(WebControllerOptions{
		CurrentVersion: "v1.0.0", GOOS: "linux", GOARCH: "amd64", Paths: env.paths,
		Now: func() time.Time { return now },
		Source: latestVersionSourceFunc(func(context.Context) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			return latest, nil
		}),
	})
	if _, err := controller.Request(context.Background(), "v1.1.0"); !errors.Is(err, ErrUpdateCheckRequired) {
		t.Fatalf("request before check error = %v", err)
	}
	if _, err := controller.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Request(context.Background(), "v1.2.0"); !errors.Is(err, ErrUpdateTargetMismatch) {
		t.Fatalf("mismatched request error = %v", err)
	}
	queued, err := controller.Request(context.Background(), "v1.1.0")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if queued.State != UpdateStateQueued || queued.TargetVersion != "v1.1.0" {
		t.Fatalf("queued status = %+v", queued)
	}
	entries, err := os.ReadDir(env.paths.Staging)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging entries after publish = %v, %v", entries, err)
	}
	requestInfo, err := os.Stat(env.paths.Request)
	if err != nil {
		t.Fatal(err)
	}
	if stat, ok := requestInfo.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		t.Fatalf("published request link count is not 1")
	}

	// The root worker has not written status.json yet. The secure inbox peek is
	// still authoritative, so a fast UI poll remains queued and does not enable
	// a second update button.
	mu.Lock()
	latest = "v1.2.0"
	mu.Unlock()
	polled, err := controller.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if polled.State != UpdateStateQueued || polled.TargetVersion != "v1.1.0" || !polled.UpdateAvailable {
		t.Fatalf("queued poll = %+v", polled)
	}
	mu.Lock()
	if calls != 1 {
		t.Fatalf("active job unexpectedly refreshed latest: %d calls", calls)
	}
	mu.Unlock()
	if _, err := controller.Request(context.Background(), "v1.1.0"); !errors.Is(err, ErrUpdateInProgress) {
		t.Fatalf("duplicate request error = %v", err)
	}

	request, err := ConsumeUpdateRequest(env.paths.Request)
	if err != nil {
		t.Fatalf("ConsumeUpdateRequest: %v", err)
	}
	if request.TargetVersion != "v1.1.0" || request.RequestedAt != now || !validJobID(request.JobID) {
		t.Fatalf("request = %+v", request)
	}
	if _, err := ConsumeUpdateRequest(env.paths.Request); !errors.Is(err, ErrNoUpdateRequest) {
		t.Fatalf("second consume error = %v", err)
	}

	running := UpdateJobStatus{
		JobID: request.JobID, CurrentVersion: "v1.0.0", TargetVersion: "v1.1.0",
		State: UpdateStateRunning, UpdatedAt: now.Add(time.Second),
	}
	if err := WriteUpdateStatus(env.paths.Status, running); err != nil {
		t.Fatalf("WriteUpdateStatus: %v", err)
	}
	polled, err = controller.Check(context.Background())
	if err != nil || polled.State != UpdateStateRunning || polled.TargetVersion != "v1.1.0" {
		t.Fatalf("running poll = %+v, %v", polled, err)
	}
}

func TestWebControllerRequestNeedsNewerRelease(t *testing.T) {
	t.Parallel()
	for _, latest := range []string{"v1.0.0", "v0.9.0"} {
		t.Run(latest, func(t *testing.T) {
			env := newWebControlTestEnv(t, true)
			controller := NewWebController(WebControllerOptions{
				CurrentVersion: "v1.0.0", GOOS: "linux", GOARCH: "amd64", Paths: env.paths,
				Source: latestVersionSourceFunc(func(context.Context) (string, error) { return latest, nil }),
			})
			status, err := controller.Check(context.Background())
			if err != nil || status.UpdateAvailable {
				t.Fatalf("Check = %+v, %v", status, err)
			}
			if _, err := controller.Request(context.Background(), latest); !errors.Is(err, ErrNoUpdateAvailable) {
				t.Fatalf("Request error = %v", err)
			}
		})
	}
}

func TestWebControllerRecoversRunningStatusFromInstalledVersionOrExpiredHeartbeat(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		currentVersion string
		updatedAt      time.Time
		wantState      UpdateState
	}{
		{
			name: "installed target after final status write failure", currentVersion: "v1.1.0",
			updatedAt: now, wantState: UpdateStateSucceeded,
		},
		{
			name: "worker heartbeat expired", currentVersion: "v1.0.0",
			updatedAt: now.Add(-3 * time.Minute), wantState: UpdateStateFailed,
		},
		{
			name: "live worker heartbeat", currentVersion: "v1.0.0",
			updatedAt: now.Add(-time.Minute), wantState: UpdateStateRunning,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newWebControlTestEnv(t, true)
			if err := WriteUpdateStatus(env.paths.Status, UpdateJobStatus{
				JobID: "abcdefghijklmnop", CurrentVersion: "v1.0.0", TargetVersion: "v1.1.0",
				State: UpdateStateRunning, UpdatedAt: test.updatedAt,
			}); err != nil {
				t.Fatal(err)
			}
			controller := NewWebController(WebControllerOptions{
				CurrentVersion: test.currentVersion, GOOS: "linux", GOARCH: "amd64", Paths: env.paths,
				Now: func() time.Time { return now }, StaleAfter: 2 * time.Minute,
				Source: latestVersionSourceFunc(func(context.Context) (string, error) { return "v1.1.0", nil }),
			})
			status, err := controller.Check(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status.State != test.wantState {
				t.Fatalf("state = %s, want %s (status=%+v)", status.State, test.wantState, status)
			}
		})
	}
}

func TestUpdateRequestAndStatusFilesAreStrictAndAtomic(t *testing.T) {
	t.Parallel()
	t.Run("installer 2770 directories are accepted", func(t *testing.T) {
		env := newWebControlTestEnv(t, true)
		request := UpdateJobRequest{
			JobID: "abcdefghijklmnop", TargetVersion: "v1.1.0", RequestedAt: time.Now().UTC(),
		}
		if err := enqueueUpdateRequest(env.paths.Staging, env.paths.Request, request); err != nil {
			t.Fatalf("enqueue in 2770 dirs: %v", err)
		}
		got, err := ConsumeUpdateRequest(env.paths.Request)
		if err != nil || got.JobID != request.JobID {
			t.Fatalf("consume = %+v, %v", got, err)
		}
	})
	t.Run("no overwrite", func(t *testing.T) {
		env := newWebControlTestEnv(t, true)
		first := UpdateJobRequest{JobID: "abcdefghijklmnop", TargetVersion: "v1.1.0", RequestedAt: time.Now().UTC()}
		second := UpdateJobRequest{JobID: "qrstuvwxyzABCDEF", TargetVersion: "v1.2.0", RequestedAt: time.Now().UTC()}
		if err := enqueueUpdateRequest(env.paths.Staging, env.paths.Request, first); err != nil {
			t.Fatal(err)
		}
		if err := enqueueUpdateRequest(env.paths.Staging, env.paths.Request, second); !errors.Is(err, ErrUpdateInProgress) {
			t.Fatalf("second enqueue error = %v", err)
		}
		got, err := ConsumeUpdateRequest(env.paths.Request)
		if err != nil || got.JobID != first.JobID {
			t.Fatalf("published request was overwritten: %+v, %v", got, err)
		}
		entries, _ := os.ReadDir(env.paths.Staging)
		if len(entries) != 0 {
			t.Fatalf("staging leaked %d files", len(entries))
		}
	})
	t.Run("cross-mount staging falls back to the inbox", func(t *testing.T) {
		env := newWebControlTestEnv(t, true)
		request := UpdateJobRequest{JobID: "abcdefghijklmnop", TargetVersion: "v1.1.0", RequestedAt: time.Now().UTC()}
		linkCalls := 0
		link := func(oldPath, newPath string) error {
			linkCalls++
			if linkCalls == 1 {
				return syscall.EXDEV
			}
			return os.Link(oldPath, newPath)
		}
		if err := enqueueUpdateRequestWithLink(env.paths.Staging, env.paths.Request, request, link); err != nil {
			t.Fatalf("enqueue across sandbox mounts: %v", err)
		}
		if linkCalls != 2 {
			t.Fatalf("link calls = %d, want primary attempt plus inbox fallback", linkCalls)
		}
		entries, err := os.ReadDir(env.paths.Staging)
		if err != nil || len(entries) != 0 {
			t.Fatalf("staging entries after fallback = %v, %v", entries, err)
		}
		got, err := ConsumeUpdateRequest(env.paths.Request)
		if err != nil || got.JobID != request.JobID {
			t.Fatalf("consume fallback request = %+v, %v", got, err)
		}
	})
	t.Run("strict request JSON", func(t *testing.T) {
		env := newWebControlTestEnv(t, true)
		body := `{"jobId":"abcdefghijklmnop","targetVersion":"v1.1.0","requestedAt":"2026-07-20T00:00:00Z","binaryPath":"/tmp/evil"}`
		if err := os.WriteFile(env.paths.Request, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ConsumeUpdateRequest(env.paths.Request); !errors.Is(err, ErrInvalidUpdateRequest) {
			t.Fatalf("unknown field error = %v", err)
		}
		if _, err := os.Stat(env.paths.Request); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid request was not consumed: %v", err)
		}
	})
	t.Run("unsafe request symlink", func(t *testing.T) {
		env := newWebControlTestEnv(t, true)
		target := filepath.Join(env.root, "target")
		if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, env.paths.Request); err != nil {
			t.Fatal(err)
		}
		if _, err := ConsumeUpdateRequest(env.paths.Request); !errors.Is(err, ErrUnsafeUpdateControlPath) {
			t.Fatalf("symlink error = %v", err)
		}
	})
	t.Run("status round trip", func(t *testing.T) {
		env := newWebControlTestEnv(t, true)
		want := UpdateJobStatus{
			JobID: "abcdefghijklmnop", CurrentVersion: "v1.0.0", TargetVersion: "v1.1.0",
			State: UpdateStateFailed, Error: "safe summary", UpdatedAt: time.Now().UTC().Truncate(time.Nanosecond),
		}
		if err := WriteUpdateStatus(env.paths.Status, want); err != nil {
			t.Fatal(err)
		}
		got, err := ReadUpdateStatus(env.paths.Status)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("status = %+v, %v, want %+v", got, err, want)
		}
		info, _ := os.Stat(env.paths.Status)
		if info.Mode().Perm() != 0o640 {
			t.Fatalf("status mode = %04o", info.Mode().Perm())
		}
	})
	t.Run("world-writable inbox rejected", func(t *testing.T) {
		env := newWebControlTestEnv(t, true)
		if err := os.Chmod(filepath.Dir(env.paths.Request), os.ModeSetgid|0o777); err != nil {
			t.Fatal(err)
		}
		request := UpdateJobRequest{JobID: "abcdefghijklmnop", TargetVersion: "v1.1.0", RequestedAt: time.Now().UTC()}
		if err := enqueueUpdateRequest(env.paths.Staging, env.paths.Request, request); !errors.Is(err, ErrUnsafeUpdateControlPath) {
			t.Fatalf("world-writable inbox error = %v", err)
		}
	})
	t.Run("unexpected producer uid rejected", func(t *testing.T) {
		env := newWebControlTestEnv(t, true)
		request := UpdateJobRequest{JobID: "abcdefghijklmnop", TargetVersion: "v1.1.0", RequestedAt: time.Now().UTC()}
		if err := enqueueUpdateRequest(env.paths.Staging, env.paths.Request, request); err != nil {
			t.Fatal(err)
		}
		if _, err := ConsumeUpdateRequestForUID(env.paths.Request, os.Geteuid()+1); !errors.Is(err, ErrUnsafeUpdateControlPath) {
			t.Fatalf("unexpected producer uid error = %v", err)
		}
	})
}

func TestSharedDirectoryPermissionModelMatchesInstaller(t *testing.T) {
	t.Parallel()
	installerMode := os.ModeDir | os.ModeSetgid | 0o770
	if !sharedDirectoryAccessAllowed(installerMode, 0, 991, 992, 991) {
		t.Fatal("root:runeconsole 2770 was not writable by the runeconsole daemon group")
	}
	if sharedDirectoryAccessAllowed(os.ModeDir|0o770, 0, 991, 992, 991) {
		t.Fatal("group-writable root directory without setgid was accepted")
	}
	if sharedDirectoryAccessAllowed(os.ModeDir|os.ModeSetgid|0o777, 0, 991, 992, 991) {
		t.Fatal("world-writable updater directory was accepted")
	}
	if sharedDirectoryAccessAllowed(installerMode, 0, 991, 992, 993) {
		t.Fatal("unrelated daemon group was accepted")
	}
}

type latestVersionSourceFunc func(context.Context) (string, error)

func (f latestVersionSourceFunc) LatestVersion(ctx context.Context) (string, error) {
	return f(ctx)
}

type webControlTestEnv struct {
	root  string
	paths UpdateControlPaths
}

func newWebControlTestEnv(t *testing.T, enabled bool) webControlTestEnv {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o750); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(root, "staging")
	inbox := filepath.Join(root, "inbox")
	for _, dir := range []string{staging, inbox} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, os.ModeSetgid|0o770); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dir)
		if err != nil || info.Mode()&os.ModeSetgid == 0 || info.Mode().Perm() != 0o770 {
			t.Fatalf("test directory does not match installer mode 2770: %s (%v, %v)", dir, info, err)
		}
	}
	paths := UpdateControlPaths{
		Enabled: filepath.Join(root, "enabled"),
		Staging: staging,
		Request: filepath.Join(inbox, "request"),
		Status:  filepath.Join(root, "status.json"),
	}
	if enabled {
		if err := os.WriteFile(paths.Enabled, nil, 0o440); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(paths.Enabled, 0o440); err != nil {
			t.Fatal(err)
		}
	}
	return webControlTestEnv{root: root, paths: paths}
}
