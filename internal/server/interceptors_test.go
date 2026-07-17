package server

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/CryptoLabInc/rune-console/pkg/consolepb"
)

func TestCheckTokenSafetyAccepts(t *testing.T) {
	if err := CheckTokenSafety("evt_0123456789abcdef0123456789abcdef"); err != nil {
		t.Errorf("good token rejected: %v", err)
	}
}

func TestCheckTokenSafetyRejectsControlChar(t *testing.T) {
	for _, bad := range []string{"\x00token", "tok\x01en", "tok\x1fen", "tok\x7fen"} {
		if err := CheckTokenSafety(bad); err == nil {
			t.Errorf("control char accepted: %q", bad)
		}
	}
}

func TestCheckTokenSafetyRejectsWhitespace(t *testing.T) {
	for _, bad := range []string{" token", "token ", "\ttoken", "token\n"} {
		if err := CheckTokenSafety(bad); err == nil {
			t.Errorf("whitespace accepted: %q", bad)
		}
	}
}

// noopHandler is a grpc.UnaryHandler that returns the request unchanged.
func noopHandler(_ context.Context, req any) (any, error) { return req, nil }

func mustInterceptor(t *testing.T) grpc.UnaryServerInterceptor {
	t.Helper()
	ic, err := NewValidationInterceptor(func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return ic
}

func consoleMethodInfo(name string) *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: "/rune.console.v1.ConsoleService/" + name}
}

func TestInterceptorPassesValidRequest(t *testing.T) {
	ic := mustInterceptor(t)
	req := &pb.GetAgentManifestRequest{Token: "evt_0123456789abcdef0123456789abcdef"}
	out, err := ic(context.Background(), req, consoleMethodInfo("GetAgentManifest"), noopHandler)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out != req {
		t.Errorf("interceptor mutated request")
	}
}

func TestInterceptorRejectsBadProtovalidate(t *testing.T) {
	ic := mustInterceptor(t)
	// Token shorter than 36 fails the proto-level constraint.
	req := &pb.GetAgentManifestRequest{Token: "too_short"}
	_, err := ic(context.Background(), req, consoleMethodInfo("GetAgentManifest"), noopHandler)
	if err == nil {
		t.Fatal("err = nil, want validation error")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestInterceptorRejectsControlCharToken(t *testing.T) {
	ic := mustInterceptor(t)
	// 36-char token containing a control byte (\x00) inside.
	// protovalidate only checks length, so the runtime layer catches this.
	req := &pb.GetAgentManifestRequest{Token: "evt_0123456789abcdef0123456789abc\x00ef"}
	if len(req.Token) != 36 {
		t.Fatalf("test setup: token length = %d, want 36", len(req.Token))
	}
	_, err := ic(context.Background(), req, consoleMethodInfo("GetAgentManifest"), noopHandler)
	if err == nil {
		t.Fatal("err = nil, want runtime error")
	}
	if !strings.Contains(err.Error(), "control") {
		t.Errorf("err = %v, want 'control characters' message", err)
	}
}

func TestInterceptorGatesConsoleWhenRunespaceNotConfigured(t *testing.T) {
	ic, err := NewValidationInterceptor(func() bool { return false }) // engine not connected
	if err != nil {
		t.Fatal(err)
	}
	req := &pb.GetAgentManifestRequest{Token: "evt_0123456789abcdef0123456789abcdef"}
	_, err = ic(context.Background(), req, consoleMethodInfo("Search"), noopHandler)
	if err == nil {
		t.Fatal("err = nil, want 'runespace not configured'")
	}
	if status.Code(err) != codes.Unavailable {
		t.Errorf("code = %v, want Unavailable", status.Code(err))
	}
	if !strings.Contains(err.Error(), "runespace not configured") {
		t.Errorf("err = %v, want 'runespace not configured'", err)
	}
}

func TestInterceptorGateSkipsNonConsoleWhenNotReady(t *testing.T) {
	ic, err := NewValidationInterceptor(func() bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	req := &pb.GetAgentManifestRequest{Token: "evt_0123456789abcdef0123456789abcdef"}
	info := &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}
	if _, err := ic(context.Background(), req, info, noopHandler); err != nil {
		t.Errorf("health blocked by runespace gate: %v", err)
	}
}

func TestInterceptorAllowsNonConsoleMethod(t *testing.T) {
	ic := mustInterceptor(t)
	// Whitespace-around token would normally fail runtime check, but
	// non-Console methods skip runtime checks (and the proto for this
	// dummy message doesn't apply).
	req := &pb.GetAgentManifestRequest{Token: "evt_0123456789abcdef0123456789abcdef"}
	info := &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}
	if _, err := ic(context.Background(), req, info, noopHandler); err != nil {
		t.Errorf("non-console method blocked: %v", err)
	}
}
