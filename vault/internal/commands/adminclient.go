package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/CryptoLabInc/rune-admin/vault/internal/server"
)

// AdminClient talks to the Vault admin UDS server.
type AdminClient struct {
	socket string
	hc     *http.Client
}

// NewAdminClient builds a client that dials the given UDS path.
// Returns ErrSocketMissing if the socket file does not exist on disk —
// gives the CLI a friendlier message than a connection-refused on the
// first request.
func NewAdminClient(socketPath string) (*AdminClient, error) {
	if socketPath == "" {
		return nil, errors.New("admin socket path is empty (set server.admin.socket or pass --admin-socket)")
	}
	if _, err := os.Stat(socketPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("admin socket %s not found — is the daemon running?", socketPath)
		}
		return nil, err
	}
	hc := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
			DisableKeepAlives: true,
		},
	}
	return &AdminClient{socket: socketPath, hc: hc}, nil
}

// adminError is what the server returns on 4xx/5xx.
type adminError struct {
	Status  int
	Message string
}

func (e *adminError) Error() string { return e.Message }

// Do sends a JSON request and decodes the response into dst (which may
// be nil to discard). 4xx/5xx responses become *adminError.
func (a *AdminClient) Do(method, path string, body, dst any) error {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewReader(b)
	}
	url := "http://admin" + path
	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return fmt.Errorf("admin: %w (socket: %s)", err, a.socket)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if jerr := json.Unmarshal(respBody, &e); jerr != nil || e.Error == "" {
			return &adminError{Status: resp.StatusCode, Message: strings.TrimSpace(string(respBody))}
		}
		return &adminError{Status: resp.StatusCode, Message: e.Error}
	}
	if dst != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, dst); err != nil {
			return fmt.Errorf("admin: parse response: %w", err)
		}
	}
	return nil
}

// resolveAdminClient returns an AdminClient using either the explicit
// --admin-socket flag or the socket field from the resolved runevault.conf.
func resolveAdminClient() (*AdminClient, error) {
	socket := globals.adminSocket
	if socket == "" {
		cfg, err := server.LoadConfig(globals.configPath)
		if err != nil {
			return nil, err
		}
		socket = cfg.Server.Admin.Socket
	}
	if socket == "" {
		return nil, errors.New("admin socket not configured (set server.admin.socket or pass --admin-socket)")
	}
	return NewAdminClient(socket)
}
