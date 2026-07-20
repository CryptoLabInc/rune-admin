package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/CryptoLabInc/rune-console/internal/server"
	"github.com/CryptoLabInc/rune-console/internal/updater"
)

const updateAgentFailureSummary = "update failed; inspect the runeconsole updater service logs"
const updateAgentServiceUser = "runeconsole"

type updateAgentOptions struct {
	requestPath string
	statusPath  string
}

type updateAgentDependencies struct {
	euid           func() int
	now            func() time.Time
	currentVersion string
	consume        func(string) (updater.UpdateJobRequest, error)
	latest         func(context.Context) (string, error)
	writeStatus    func(string, updater.UpdateJobStatus) error
	runUpdate      updateCommandRunner
	heartbeatEvery time.Duration
	validateScope  func() error
}

func defaultUpdateAgentDependencies() updateAgentDependencies {
	return updateAgentDependencies{
		euid:           os.Geteuid,
		now:            time.Now,
		currentVersion: buildVersion,
		consume:        consumeServiceUpdateRequest,
		latest:         (updater.GitHubSource{}).LatestVersion,
		writeStatus:    updater.WriteUpdateStatus,
		runUpdate:      runUpdate,
		heartbeatEvery: updater.DefaultUpdateHeartbeatInterval,
		validateScope:  validateWebUpdateScope,
	}
}

func validateWebUpdateScope() error {
	cfg, err := server.LoadConfig(globals.configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !webUpdateStateWithinInstallRoot(cfg) {
		return errors.New("web update requires all durable state under the official installation root; use the CLI update flow")
	}
	return nil
}

func consumeServiceUpdateRequest(path string) (updater.UpdateJobRequest, error) {
	account, err := user.Lookup(updateAgentServiceUser)
	if err != nil {
		return updater.UpdateJobRequest{}, errors.New("look up runeconsole service account failed")
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil || uid < 0 {
		return updater.UpdateJobRequest{}, errors.New("runeconsole service account has an invalid uid")
	}
	return updater.ConsumeUpdateRequestForUID(path, uid)
}

func newUpdateAgentCmd() *cobra.Command {
	return newUpdateAgentCmdWithDependencies(defaultUpdateAgentDependencies())
}

func newUpdateAgentCmdWithDependencies(deps updateAgentDependencies) *cobra.Command {
	opts := updateAgentOptions{}
	cmd := &cobra.Command{
		Use:    "update-agent",
		Short:  "Consume a privileged console update request",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdateAgent(cmd.Context(), opts, deps)
		},
	}
	cmd.Flags().StringVar(&opts.requestPath, "request-path", updater.DefaultUpdateRequestPath,
		"Fixed update request hand-off path")
	cmd.Flags().StringVar(&opts.statusPath, "status-path", updater.DefaultUpdateStatusPath,
		"Fixed update status hand-off path")
	return cmd
}

func runUpdateAgent(ctx context.Context, opts updateAgentOptions, deps updateAgentDependencies) error {
	// Check privilege before touching the request. A manual unprivileged call
	// must not consume the one request intended for the supervised root worker.
	if deps.euid == nil || deps.euid() != 0 {
		return errors.New("runeconsole update agent must run as root")
	}
	request, err := deps.consume(opts.requestPath)
	if err != nil {
		return err
	}

	now := func() time.Time {
		if deps.now == nil {
			return time.Now().UTC()
		}
		return deps.now().UTC()
	}
	status := updater.UpdateJobStatus{
		JobID:          request.JobID,
		CurrentVersion: deps.currentVersion,
		TargetVersion:  request.TargetVersion,
		State:          updater.UpdateStateRunning,
		UpdatedAt:      now(),
	}
	if err := deps.writeStatus(opts.statusPath, status); err != nil {
		return fmt.Errorf("record running update state: %w", err)
	}
	stopHeartbeat := startUpdateHeartbeat(opts.statusPath, status, deps.heartbeatEvery, now, deps.writeStatus)
	defer stopHeartbeat()
	if deps.validateScope != nil {
		if err := deps.validateScope(); err != nil {
			stopHeartbeat()
			return failUpdateAgent(opts.statusPath, status, now(), deps.writeStatus,
				fmt.Errorf("validate web update state scope: %w", err))
		}
	}

	// The browser can only enqueue the version from its fresh cache. Recheck in
	// the root process immediately before mutation as a second trust boundary:
	// only the current latest stable GitHub release may reach the update engine.
	latest, err := deps.latest(ctx)
	if err != nil {
		stopHeartbeat()
		return failUpdateAgent(opts.statusPath, status, now(), deps.writeStatus,
			fmt.Errorf("revalidate latest release: %w", err))
	}
	if latest != request.TargetVersion {
		stopHeartbeat()
		return failUpdateAgent(opts.statusPath, status, now(), deps.writeStatus,
			fmt.Errorf("queued release %q is no longer the latest stable release", request.TargetVersion))
	}

	err = deps.runUpdate(ctx, io.Discard, updateOptions{
		version:   request.TargetVersion,
		backupDir: updater.DefaultBackupDir,
	})
	if err != nil {
		stopHeartbeat()
		return failUpdateAgent(opts.statusPath, status, now(), deps.writeStatus, err)
	}

	// Stop and join the heartbeat before the terminal write; otherwise a late
	// tick could overwrite succeeded with running.
	stopHeartbeat()
	status.CurrentVersion = request.TargetVersion
	status.State = updater.UpdateStateSucceeded
	status.Error = ""
	status.UpdatedAt = now()
	if err := deps.writeStatus(opts.statusPath, status); err != nil {
		return fmt.Errorf("record successful update state: %w", err)
	}
	return nil
}

func startUpdateHeartbeat(
	statusPath string,
	status updater.UpdateJobStatus,
	interval time.Duration,
	now func() time.Time,
	writeStatus func(string, updater.UpdateJobStatus) error,
) func() {
	if interval <= 0 || writeStatus == nil {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				status.UpdatedAt = now()
				// The terminal write remains authoritative. A transient heartbeat
				// failure is retried at the next tick and must not abort backup or
				// rollback while the service is stopped.
				_ = writeStatus(statusPath, status)
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}
}

func failUpdateAgent(
	statusPath string,
	status updater.UpdateJobStatus,
	failedAt time.Time,
	writeStatus func(string, updater.UpdateJobStatus) error,
	cause error,
) error {
	status.State = updater.UpdateStateFailed
	status.Error = updateAgentFailureSummary
	status.UpdatedAt = failedAt
	if err := writeStatus(statusPath, status); err != nil {
		return errors.Join(cause, fmt.Errorf("record failed update state: %w", err))
	}
	return cause
}
