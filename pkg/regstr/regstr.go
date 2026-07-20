// Package regstr encodes and decodes the rune-mcp registration string — the
// single opaque token a team member pastes into /rune:configure to connect
// their rune-mcp to this console. It is a public package because rune-mcp (a
// separate module) decodes what rune-console encodes.
//
// Wire format:
//
//	runev1_<base64url-nopad(json)>_<crc32-ieee hex, 8 chars>
//
// The json payload bundles the three values the connection needs:
//
//	{"v":1,"endpoint":"host:port","token":"evt_…","ca_sha256":"<hex>"}
//
// The CRC guards against copy-paste truncation, not tampering — the string is a
// credential (it carries the wrapping token), so it must never be logged and is
// trusted only over the delivery channel (email) plus the token's own
// single-use/rotate property. ca_sha256 pins the CA the client fetches over the
// untrusted GetCACert bootstrap before it trusts the console's TLS.
package regstr

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"
)

// prefix versions the wire format so a future codec (the pinned PDF §06 shape)
// can coexist and be told apart at decode time.
const prefix = "runev1_"

// Registration is the decoded payload of a registration string.
type Registration struct {
	Version  int    `json:"v"`
	Endpoint string `json:"endpoint"`  // console gRPC endpoint, host:port
	Token    string `json:"token"`     // access token (evt_…) — a credential
	CASHA256 string `json:"ca_sha256"` // hex SHA-256 pin for the CA served by GetCACert
}

// Encode bundles r into the opaque registration string.
func Encode(r Registration) (string, error) {
	if r.Version == 0 {
		r.Version = 1
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("regstr: marshal: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	crc := crc32.ChecksumIEEE([]byte(payload))
	return fmt.Sprintf("%s%s_%08x", prefix, payload, crc), nil
}

// Decode parses and CRC-verifies a registration string.
func Decode(s string) (Registration, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, prefix) {
		return Registration{}, fmt.Errorf("regstr: not a %s string", prefix)
	}
	body := s[len(prefix):]
	i := strings.LastIndex(body, "_")
	if i < 0 {
		return Registration{}, fmt.Errorf("regstr: missing crc separator")
	}
	payload, crcHex := body[:i], body[i+1:]
	var wantCRC uint32
	if _, err := fmt.Sscanf(crcHex, "%08x", &wantCRC); err != nil {
		return Registration{}, fmt.Errorf("regstr: bad crc %q: %w", crcHex, err)
	}
	if got := crc32.ChecksumIEEE([]byte(payload)); got != wantCRC {
		return Registration{}, fmt.Errorf("regstr: crc mismatch (corrupt/truncated paste): got %08x want %08x", got, wantCRC)
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Registration{}, fmt.Errorf("regstr: base64: %w", err)
	}
	var r Registration
	if err := json.Unmarshal(raw, &r); err != nil {
		return Registration{}, fmt.Errorf("regstr: json: %w", err)
	}
	return r, nil
}
