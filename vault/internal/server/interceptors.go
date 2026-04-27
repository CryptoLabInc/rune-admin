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

	pb "github.com/CryptoLabInc/rune-admin/vault/pkg/vaultpb"
)

// vaultMethods enumerates the gRPC method paths owned by VaultService.
// Other services routed through the same gRPC server bypass runtime checks.
var vaultMethods = map[string]bool{
	"/rune.vault.v1.VaultService/GetPublicKey":    true,
	"/rune.vault.v1.VaultService/DecryptScores":   true,
	"/rune.vault.v1.VaultService/DecryptMetadata": true,
}

// NewValidationInterceptor returns a unary server interceptor that runs
// protovalidate against the request, then a runtime safety check on the
// token field. Validation errors are returned as InvalidArgument.
//
// Mirrors vault/validation_interceptor.py and vault/request_validator.py.
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
		if vaultMethods[info.FullMethod] {
			if err := runtimeCheckToken(req); err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
		}
		return handler(ctx, req)
	}, nil
}

// runtimeCheckToken pulls the token field from a Vault request and runs
// the supplementary checks the .proto annotations cannot express.
func runtimeCheckToken(req any) error {
	var token string
	switch r := req.(type) {
	case *pb.GetPublicKeyRequest:
		token = r.GetToken()
	case *pb.DecryptScoresRequest:
		token = r.GetToken()
	case *pb.DecryptMetadataRequest:
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
