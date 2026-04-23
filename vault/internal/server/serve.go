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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pb "github.com/CryptoLabInc/rune-admin/vault/pkg/vaultpb"
)

// Serve starts the gRPC + admin UDS listeners with the given Vault and
// blocks until ctx is cancelled or a SIGTERM/SIGINT is received. The
// admin listener is constructed by AdminFactory; passing nil disables the
// admin UDS surface (useful for unit tests that exercise gRPC alone).
//
// Returns nil on graceful shutdown. Listener bind errors and server runtime
// errors are returned eagerly.
func Serve(ctx context.Context, v *Vault, adminFactory AdminFactory) error {
	cfg := v.Config()

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

	interceptor, err := NewValidationInterceptor()
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
	pb.RegisterVaultServiceServer(gs, NewVaultGRPC(v))

	// Health + reflection (matches Python registration sites:
	// vault_grpc_server.py:317-331).
	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("rune.vault.v1.VaultService", healthpb.HealthCheckResponse_SERVING)
	healthSvc.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gs, healthSvc)
	reflection.Register(gs)

	// Admin UDS listener (optional)
	var adminShutdown func(context.Context) error
	if adminFactory != nil {
		adminShutdown, err = adminFactory(ctx, v)
		if err != nil {
			return fmt.Errorf("server: admin: %w", err)
		}
	}

	scheme := "insecure"
	if tlsCreds != nil {
		scheme = "tls"
	}
	slog.Info("vault: gRPC listening", "addr", grpcAddr, "scheme", scheme)

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
		slog.Info("vault: context cancelled, shutting down")
	case sig := <-sigCh:
		slog.Info("vault: signal received, shutting down", "signal", sig.String())
	case err := <-errCh:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return fmt.Errorf("server: grpc serve: %w", err)
		}
	}

	healthSvc.Shutdown()
	stopGracefullyOrForce(gs, 5*time.Second)
	if adminShutdown != nil {
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = adminShutdown(shCtx)
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
		slog.Warn("vault: graceful stop timed out, forcing", "grace", grace)
		gs.Stop()
		<-done
	}
}

// AdminFactory builds the admin UDS server and returns a shutdown closer.
// internal/server/admin.go (added in step 6) supplies the production impl.
type AdminFactory func(ctx context.Context, v *Vault) (shutdown func(context.Context) error, err error)

func grpcHost(cfg *Config) string {
	if cfg.Server.GRPC.Host == "" {
		return "0.0.0.0"
	}
	return cfg.Server.GRPC.Host
}

func loadTLSCredentials(t TLSConfig) (credentials.TransportCredentials, error) {
	if t.Disable {
		slog.Warn("vault: TLS disabled — gRPC traffic is unencrypted (dev mode only)")
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
