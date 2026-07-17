package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pb "github.com/CryptoLabInc/rune-console/pkg/consolepb"
)

// Serve starts the gRPC listener and, when consoleHandler is non-nil, the
// loopback console HTTP listener (127.0.0.1), and blocks until ctx is
// cancelled or a SIGTERM/SIGINT is received. A nil consoleHandler disables
// the console surface (useful for unit tests that exercise gRPC alone).
//
// Returns nil on graceful shutdown. Listener bind errors and server runtime
// errors are returned eagerly.
func Serve(ctx context.Context, v *Console, consoleHandler http.Handler) error {
	cfg := v.Config()
	// runespace eval-key registration is handled by crypto.OpenEngine at daemon
	// startup; no separate cloud-setup step is needed.

	// gRPC listener
	grpcAddr := fmt.Sprintf("%s:%d", grpcHost(cfg), cfg.Server.GRPC.Port)
	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("server: listen %s: %w", grpcAddr, err)
	}
	defer grpcLis.Close()

	tlsCreds, err := loadTLSCredentials(cfg.Server.GRPC.TLS)
	if err != nil {
		return fmt.Errorf("server: tls: %w", err)
	}

	interceptor, err := NewValidationInterceptor(v.engineReady)
	if err != nil {
		return fmt.Errorf("server: interceptor: %w", err)
	}

	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(MaxMessageSize),
		grpc.MaxSendMsgSize(MaxMessageSize),
		grpc.UnaryInterceptor(interceptor),
	}
	if tlsCreds != nil {
		opts = append(opts, grpc.Creds(tlsCreds))
	}
	gs := grpc.NewServer(opts...)
	pb.RegisterConsoleServiceServer(gs, NewConsoleGRPC(v))

	// Health + reflection.
	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("rune.console.v1.ConsoleService", healthpb.HealthCheckResponse_SERVING)
	healthSvc.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gs, healthSvc)
	reflection.Register(gs)

	// Console HTTP listener (optional): loopback-only, plain HTTP. The OAuth
	// redirect is a 127.0.0.1 callback and this surface must never bind a
	// public interface, so the host is fixed. It serves the SPA, the BFF auth
	// endpoints, and the cookie-gated /api/v1 + admin surface.
	var consoleSrv *http.Server
	if consoleHandler != nil {
		consoleAddr := fmt.Sprintf("127.0.0.1:%d", cfg.ConsolePort())
		consoleLis, lerr := net.Listen("tcp", consoleAddr)
		if lerr != nil {
			return fmt.Errorf("server: listen console %s: %w", consoleAddr, lerr)
		}
		consoleSrv = &http.Server{
			Handler:           consoleHandler,
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			if serr := consoleSrv.Serve(consoleLis); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
				slog.Error("console: HTTP server error", "err", serr)
			}
		}()
		slog.Info("console: HTTP listening", "addr", consoleAddr)
	}

	scheme := "insecure"
	if tlsCreds != nil {
		scheme = "tls"
	}
	slog.Info("console: gRPC listening", "addr", grpcAddr, "scheme", scheme)

	// Run gRPC in a goroutine; wait for shutdown signal or ctx cancellation.
	errCh := make(chan error, 1)
	go func() {
		errCh <- gs.Serve(grpcLis)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	select {
	case <-ctx.Done():
		slog.Info("console: context cancelled, shutting down")
	case sig := <-sigCh:
		slog.Info("console: signal received, shutting down", "signal", sig.String())
	case err := <-errCh:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return fmt.Errorf("server: grpc serve: %w", err)
		}
	}

	healthSvc.Shutdown()
	stopGracefullyOrForce(gs, 5*time.Second)
	if consoleSrv != nil {
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = consoleSrv.Shutdown(shCtx)
	}
	return nil
}

// stopGracefullyOrForce gives gRPC up to grace to drain in-flight RPCs;
// on timeout, falls back to a hard Stop so the daemon never hangs at
// shutdown waiting for an idle reflection/health stream to close.
func stopGracefullyOrForce(gs *grpc.Server, grace time.Duration) {
	done := make(chan struct{})
	go func() {
		gs.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(grace):
		slog.Warn("console: graceful stop timed out, forcing", "grace", grace)
		gs.Stop()
		<-done
	}
}

func grpcHost(cfg *Config) string {
	if cfg.Server.GRPC.Host == "" {
		return "0.0.0.0"
	}
	return cfg.Server.GRPC.Host
}

func loadTLSCredentials(t TLSConfig) (credentials.TransportCredentials, error) {
	if t.Disable {
		slog.Warn("console: TLS disabled — gRPC traffic is unencrypted (dev mode only)")
		return nil, nil
	}
	if t.Cert == "" || t.Key == "" {
		return nil, errors.New("server.grpc.tls.cert and server.grpc.tls.key are required (or set disable=true)")
	}
	cert, err := tls.LoadX509KeyPair(t.Cert, t.Key)
	if err != nil {
		return nil, fmt.Errorf("load x509 key pair: %w", err)
	}
	return credentials.NewServerTLSFromCert(&cert), nil
}
