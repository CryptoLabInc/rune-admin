package commands

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/CryptoLabInc/rune-admin/vault/internal/server"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report daemon health, PID, and socket liveness",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd)
		},
	}
}

type statusReport struct {
	pidPath      string
	pidPresent   bool
	pid          int
	pidAlive     bool
	adminSocket  string
	adminUp      bool
	adminError   string
	grpcAddr     string
	grpcServing  bool
	grpcError    string
	configSource string
}

func runStatus(cmd *cobra.Command) error {
	cfg, err := server.LoadConfig(globals.configPath)
	if err != nil {
		return err
	}

	r := statusReport{
		pidPath:      cfg.Daemon.PIDFile,
		adminSocket:  cfg.Server.Admin.Socket,
		grpcAddr:     fmt.Sprintf("%s:%d", cfg.Server.GRPC.Host, cfg.Server.GRPC.Port),
		configSource: cfg.Source,
	}
	if globals.adminSocket != "" {
		r.adminSocket = globals.adminSocket
	}

	if pid, err := server.ReadPIDFile(r.pidPath); err == nil {
		r.pidPresent = true
		r.pid = pid
		r.pidAlive = server.PIDLive(pid)
	}

	r.adminUp, r.adminError = probeAdminUDS(r.adminSocket)
	r.grpcServing, r.grpcError = probeGRPCHealth(r.grpcAddr)

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Config:       %s\n", r.configSource)
	fmt.Fprintf(out, "PID file:     %s\n", r.pidPath)
	if r.pidPresent {
		fmt.Fprintf(out, "  PID:        %d (%s)\n", r.pid, aliveStr(r.pidAlive))
	} else {
		fmt.Fprintln(out, "  PID:        not present")
	}
	fmt.Fprintf(out, "Admin socket: %s (%s)\n", r.adminSocket, healthStr(r.adminUp, r.adminError))
	fmt.Fprintf(out, "gRPC:         %s (%s)\n", r.grpcAddr, healthStr(r.grpcServing, r.grpcError))

	// Non-zero exit if anything material is down.
	if !(r.adminUp && r.grpcServing && (!r.pidPresent || r.pidAlive)) {
		os.Exit(2)
	}
	return nil
}

func aliveStr(alive bool) string {
	if alive {
		return "alive"
	}
	return "dead — stale PID file"
}

func healthStr(ok bool, errMsg string) string {
	if ok {
		return "ok"
	}
	if errMsg != "" {
		return "down — " + errMsg
	}
	return "down"
}

func probeAdminUDS(path string) (bool, string) {
	if path == "" {
		return false, "socket path empty"
	}
	hc := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", path)
			},
			DisableKeepAlives: true,
		},
	}
	resp, err := hc.Get("http://admin/health")
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200, ""
}

func probeGRPCHealth(addr string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false, err.Error()
	}
	defer conn.Close()
	cli := healthpb.NewHealthClient(conn)
	resp, err := cli.Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return false, err.Error()
	}
	return resp.GetStatus() == healthpb.HealthCheckResponse_SERVING, ""
}
