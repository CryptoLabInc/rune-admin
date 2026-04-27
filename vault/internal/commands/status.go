package commands

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/CryptoLabInc/rune-admin/vault/internal/server"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report daemon health and socket liveness",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd)
		},
	}
}

type statusReport struct {
	adminSocket  string
	adminUp      bool
	adminError   string
	grpcBind     string
	grpcProbe    string
	grpcServing  bool
	grpcError    string
	configSource string
}

func runStatus(cmd *cobra.Command) error {
	cfg, err := server.LoadConfig(globals.configPath)
	if err != nil {
		return err
	}

	bindHost := cfg.Server.GRPC.Host
	probeHost := bindHost
	if probeHost == "" || probeHost == "0.0.0.0" {
		probeHost = "127.0.0.1"
	}

	r := statusReport{
		adminSocket:  cfg.Server.Admin.Socket,
		grpcBind:     fmt.Sprintf("%s:%d", bindHost, cfg.Server.GRPC.Port),
		grpcProbe:    fmt.Sprintf("%s:%d", probeHost, cfg.Server.GRPC.Port),
		configSource: cfg.Source,
	}
	if globals.adminSocket != "" {
		r.adminSocket = globals.adminSocket
	}

	r.adminUp, r.adminError = probeAdminUDS(r.adminSocket)
	r.grpcServing, r.grpcError = probeGRPCHealth(r.grpcProbe, cfg.Server.GRPC.TLS.Disable)

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Config:       %s\n", r.configSource)
	fmt.Fprintf(out, "Admin socket: %s (%s)\n", r.adminSocket, healthStr(r.adminUp, r.adminError))
	fmt.Fprintf(out, "gRPC:         %s (%s)\n", r.grpcBind, healthStr(r.grpcServing, r.grpcError))

	if !(r.adminUp && r.grpcServing) {
		os.Exit(2)
	}
	return nil
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

func probeGRPCHealth(addr string, tlsDisabled bool) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var creds grpc.DialOption
	if tlsDisabled {
		creds = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		// InsecureSkipVerify is intentional: status probe is local-only and
		// does not need to verify the self-signed server cert.
		creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})) //nolint:gosec
	}
	conn, err := grpc.NewClient(addr, creds)
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
