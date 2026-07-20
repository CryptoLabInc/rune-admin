package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
)

// caPEMAndPin reads the console's CA (or, for self-signed deployments, its
// serving cert) and returns the PEM bytes plus their lowercase-hex SHA-256.
// GetCACert serves these to a bootstrapping client, and the self-invite issuer
// pins the same digest into the registration string — reading from the one
// source keeps the served CA and the pinned digest from drifting apart.
//
// It returns an error when no CA/cert path is configured (a misconfiguration —
// TLS is mandatory), so the caller emits an empty pin rather than a stale one.
func caPEMAndPin(cfg *Config) (pem []byte, sha256hex string, err error) {
	path := cfg.Server.GRPC.TLS.CA
	if path == "" {
		// Self-signed deployments have no separate CA — the serving cert is its
		// own trust anchor.
		path = cfg.Server.GRPC.TLS.Cert
	}
	if path == "" {
		return nil, "", errors.New("console has no CA/cert configured")
	}
	pem, err = os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(pem)
	return pem, hex.EncodeToString(sum[:]), nil
}
