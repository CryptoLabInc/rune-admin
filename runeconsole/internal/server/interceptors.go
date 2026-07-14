package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/CryptoLabInc/rune-console/runeconsole/pkg/consolepb"
)

// consoleMethods enumerates the gRPC method paths owned by ConsoleService.
// Other services routed through the same gRPC server bypass runtime checks.
var consoleMethods = map[string]bool{
	"/rune.console.v1.ConsoleService/GetAgentManifest": true,
	"/rune.console.v1.ConsoleService/Insert":           true,
	"/rune.console.v1.ConsoleService/Search":           true,
}

// NewValidationInterceptor returns a unary server interceptor that runs
// protovalidate against the request, then a runtime safety check on the
// token field. Validation errors are returned as InvalidArgument.
func NewValidationInterceptor() (grpc.UnaryServerInterceptor, error) {
	v, err := protovalidate.New()
	if err != nil {
		return nil, fmt.Errorf("interceptors: new protovalidate: %w", err)
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		msg, ok := req.(proto.Message)
		if ok {
			if err := v.Validate(msg); err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
		}
		if consoleMethods[info.FullMethod] {
			if err := runtimeCheckToken(req); err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
		}
		return handler(ctx, req)
	}, nil
}

// runtimeCheckToken pulls the token field from a Console request and runs
// the supplementary checks the .proto annotations cannot express.
func runtimeCheckToken(req any) error {
	var token string
	switch r := req.(type) {
	case *pb.GetAgentManifestRequest:
		token = r.GetToken()
	case *pb.InsertRequest:
		token = r.GetToken()
	case *pb.SearchRequest:
		token = r.GetToken()
	default:
		return nil
	}
	return CheckTokenSafety(token)
}

// CheckTokenSafety rejects tokens with control characters or surrounding
// whitespace. Exposed so unit tests can exercise the rule directly.
func CheckTokenSafety(token string) error {
	for _, r := range token {
		if r < 0x20 || r == 0x7f {
			return errors.New("token: must not contain control characters")
		}
		if unicode.IsControl(r) {
			return errors.New("token: must not contain control characters")
		}
	}
	if token != strings.TrimSpace(token) {
		return errors.New("token: must not have leading or trailing whitespace")
	}
	return nil
}
