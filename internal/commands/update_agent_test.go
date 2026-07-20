package commands

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/console"
	"github.com/CryptoLabInc/rune-console/internal/updater"
)

func TestUpdateAgentRejectsNonRootBeforeConsumingRequest(t *testing.T) {
	t.Parallel()
	consumed := false
	err := runUpdateAgent(context.Background(), updateAgentOptions{}, updateAgentDependencies{
		euid: func() int { return 501 },
		consume: func(string) (updater.UpdateJobRequest, error) {
			consumed = true
			return updater.UpdateJobRequest{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "must run as root") {
		t.Fatalf("error = %v", err)
	}
	if consumed {
		t.Fatal("unprivileged agent consumed the queued request")
	}
}

func TestUpdateAgentRevalidatesAndRunsPinnedRelease(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	request := testUpdateJobRequest(now)
	var statuses []updater.UpdateJobStatus
	var gotOptions updateOptions
	err := runUpdateAgent(context.Background(), updateAgentOptions{
		requestPath: "/control/inbox/request",
		statusPath:  "/control/status.json",
	}, updateAgentDependencies{
		euid:           func() int { return 0 },
		now:            func() time.Time { return now },
		currentVersion: "v1.0.0",
		consume: func(path string) (updater.UpdateJobRequest, error) {
			if path != "/control/inbox/request" {
				t.Fatalf("consume path = %q", path)
			}
			return request, nil
		},
		latest: func(context.Context) (string, error) { return "v1.1.0", nil },
		writeStatus: func(path string, status updater.UpdateJobStatus) error {
			if path != "/control/status.json" {
				t.Fatalf("status path = %q", path)
			}
			statuses = append(statuses, status)
			return nil
		},
		runUpdate: func(_ context.Context, output io.Writer, opts updateOptions) error {
			if output != io.Discard {
				t.Fatal("update agent should not write CLI output")
			}
			gotOptions = opts
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runUpdateAgent: %v", err)
	}
	if gotOptions.version != request.TargetVersion || gotOptions.backupDir != updater.DefaultBackupDir {
		t.Fatalf("update options = %+v", gotOptions)
	}
	if gotOptions.archive != "" || gotOptions.checksums != "" || gotOptions.check || gotOptions.dryRun || gotOptions.binaryPath != "" {
		t.Fatalf("web agent received unsafe update options: %+v", gotOptions)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %d, want running and succeeded", len(statuses))
	}
	if statuses[0].State != updater.UpdateStateRunning || statuses[0].CurrentVersion != "v1.0.0" {
		t.Fatalf("running status = %+v", statuses[0])
	}
	if statuses[1].State != updater.UpdateStateSucceeded || statuses[1].CurrentVersion != "v1.1.0" {
		t.Fatalf("succeeded status = %+v", statuses[1])
	}
}

func TestUpdateAgentRejectsTargetThatIsNoLongerLatest(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	request := testUpdateJobRequest(now)
	var statuses []updater.UpdateJobStatus
	runnerCalled := false
	err := runUpdateAgent(context.Background(), updateAgentOptions{}, updateAgentDependencies{
		euid:           func() int { return 0 },
		now:            func() time.Time { return now },
		currentVersion: "v1.0.0",
		consume:        func(string) (updater.UpdateJobRequest, error) { return request, nil },
		latest:         func(context.Context) (string, error) { return "v1.2.0", nil },
		writeStatus: func(_ string, status updater.UpdateJobStatus) error {
			statuses = append(statuses, status)
			return nil
		},
		runUpdate: func(context.Context, io.Writer, updateOptions) error {
			runnerCalled = true
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no longer the latest stable release") {
		t.Fatalf("error = %v", err)
	}
	if runnerCalled {
		t.Fatal("update engine ran for a stale release")
	}
	if len(statuses) != 2 || statuses[1].State != updater.UpdateStateFailed || statuses[1].Error != updateAgentFailureSummary {
		t.Fatalf("statuses = %+v", statuses)
	}
}

func TestUpdateAgentRecordsGenericFailureAfterRollbackPath(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	request := testUpdateJobRequest(now)
	engineErr := errors.New("sensitive /customer/path failure")
	var statuses []updater.UpdateJobStatus
	err := runUpdateAgent(context.Background(), updateAgentOptions{}, updateAgentDependencies{
		euid:           func() int { return 0 },
		now:            func() time.Time { return now },
		currentVersion: "v1.0.0",
		consume:        func(string) (updater.UpdateJobRequest, error) { return request, nil },
		latest:         func(context.Context) (string, error) { return request.TargetVersion, nil },
		writeStatus: func(_ string, status updater.UpdateJobStatus) error {
			statuses = append(statuses, status)
			return nil
		},
		runUpdate: func(context.Context, io.Writer, updateOptions) error { return engineErr },
	})
	if !errors.Is(err, engineErr) {
		t.Fatalf("error = %v, want engine error", err)
	}
	if got := statuses[len(statuses)-1]; got.State != updater.UpdateStateFailed || got.Error != updateAgentFailureSummary || strings.Contains(got.Error, "/customer/path") {
		t.Fatalf("failed status = %+v", got)
	}
}

func TestUpdateAgentRefreshesRunningHeartbeat(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	request := testUpdateJobRequest(now)
	runnerStarted := make(chan struct{})
	releaseRunner := make(chan struct{})
	heartbeatSeen := make(chan struct{}, 1)
	result := make(chan error, 1)
	var mu sync.Mutex
	var statuses []updater.UpdateJobStatus

	go func() {
		result <- runUpdateAgent(context.Background(), updateAgentOptions{}, updateAgentDependencies{
			euid:           func() int { return 0 },
			now:            time.Now,
			currentVersion: "v1.0.0",
			consume:        func(string) (updater.UpdateJobRequest, error) { return request, nil },
			latest:         func(context.Context) (string, error) { return request.TargetVersion, nil },
			writeStatus: func(_ string, status updater.UpdateJobStatus) error {
				mu.Lock()
				statuses = append(statuses, status)
				runningWrites := 0
				for _, candidate := range statuses {
					if candidate.State == updater.UpdateStateRunning {
						runningWrites++
					}
				}
				mu.Unlock()
				if runningWrites >= 2 {
					select {
					case heartbeatSeen <- struct{}{}:
					default:
					}
				}
				return nil
			},
			runUpdate: func(context.Context, io.Writer, updateOptions) error {
				close(runnerStarted)
				<-releaseRunner
				return nil
			},
			heartbeatEvery: 5 * time.Millisecond,
		})
	}()

	<-runnerStarted
	select {
	case <-heartbeatSeen:
	case <-time.After(time.Second):
		t.Fatal("running heartbeat was not persisted")
	}
	close(releaseRunner)
	if err := <-result; err != nil {
		t.Fatalf("runUpdateAgent: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := statuses[len(statuses)-1].State; got != updater.UpdateStateSucceeded {
		t.Fatalf("final state = %s, want succeeded", got)
	}
}

func TestMapConsoleUpdateError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input error
		want  error
	}{
		{nil, nil},
		{updater.ErrUpdateInProgress, console.ErrUpdatePending},
		{updater.ErrUpdateCheckRequired, console.ErrUpdateStale},
		{updater.ErrUpdateTargetMismatch, console.ErrUpdateStale},
		{updater.ErrWebUpdateNotCapable, console.ErrUpdateUnavailable},
		{updater.ErrNoUpdateAvailable, console.ErrUpdateUnavailable},
		{updater.ErrInvalidUpdateRequest, console.ErrUpdateInvalid},
	}
	for _, tt := range tests {
		if got := mapConsoleUpdateError(tt.input); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("mapConsoleUpdateError(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func testUpdateJobRequest(now time.Time) updater.UpdateJobRequest {
	return updater.UpdateJobRequest{
		JobID:         "abcdefghijklmnop",
		TargetVersion: "v1.1.0",
		RequestedAt:   now,
	}
}
