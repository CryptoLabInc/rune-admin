package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// Workspace is the runespace control-plane view (POST/GET /api/v1/runespace).
type Workspace struct {
	ID    string `json:"id"`
	Host  string `json:"host"`
	Tier  string `json:"tier"`
	Phase string `json:"phase"`
	Rows  int    `json:"rows,omitempty"`
}

// CreateWorkspace provisions the caller's (1:1) runespace. It sends no body: the
// cloud mints a random id server-side. A repeat returns the cloud's 409 as an error.
func (c *Client) CreateWorkspace(ctx context.Context, sessionCookie string) (*Workspace, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/runespace", nil)
	if err != nil {
		return nil, err
	}
	var out Workspace
	if err := c.do(req, sessionCookie, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetWorkspace reads the caller's runespace status. It returns (nil, nil) when
// the caller has no runespace yet (the cloud's 404 NOT_FOUND).
func (c *Client) GetWorkspace(ctx context.Context, sessionCookie string) (*Workspace, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/runespace", nil)
	if err != nil {
		return nil, err
	}
	var out Workspace
	if err := c.do(req, sessionCookie, &out); err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// DeleteWorkspace permanently deprovisions the caller's runespace. The cloud
// side is asynchronous (desired_state=absent, 202 Accepted; the provisioner
// tears down the StatefulSet and removes the volume). Irreversible.
func (c *Client) DeleteWorkspace(ctx context.Context, sessionCookie string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/api/v1/runespace", nil)
	if err != nil {
		return err
	}
	return c.do(req, sessionCookie, nil)
}

// StopWorkspace requests the cloud to stop (pause) the caller's runespace: the
// pod is evicted and compute billing stops while the volume is RETAINED, so the
// stored memory survives and a later StartWorkspace resumes on it. The cloud
// side is asynchronous (desired_state=stopped, 202 Accepted); the observed phase
// converges to "stopped" via GetWorkspace polling. Reversible — the opposite of
// DeleteWorkspace, which drops the volume.
func (c *Client) StopWorkspace(ctx context.Context, sessionCookie string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/runespace/stop", nil)
	if err != nil {
		return err
	}
	return c.do(req, sessionCookie, nil)
}

// StartWorkspace requests the cloud to start a stopped runespace: the pod is
// re-created on the retained volume. Asynchronous (desired_state=running, 202
// Accepted); the observed phase converges to "ready" via GetWorkspace polling.
func (c *Client) StartWorkspace(ctx context.Context, sessionCookie string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/runespace/start", nil)
	if err != nil {
		return err
	}
	return c.do(req, sessionCookie, nil)
}

// Bootstrap is the refresh credential from POST /api/v1/runespace/access/bootstrap.
type Bootstrap struct {
	RefreshToken string `json:"refresh_token"`
	RunespaceID  string `json:"runespace_id"`
	ExpiresAt    string `json:"expires_at"`
}

// BootstrapAccess mints the long-lived, revocable refresh credential for the
// caller's runespace. It is server-to-server on the cloud side (the Go client
// sends no Origin). Bootstrap revokes the runespace's prior refresh credential
// (one active per runespace), so call it sparingly — once, then persist and
// re-exchange for short-lived access tokens.
func (c *Client) BootstrapAccess(ctx context.Context, sessionCookie string) (*Bootstrap, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/runespace/access/bootstrap", nil)
	if err != nil {
		return nil, err
	}
	var out Bootstrap
	if err := c.do(req, sessionCookie, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AccessToken is the short-lived data-plane JWT from POST /auth/runespace/token.
type AccessToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"` // seconds
}

// ExchangeAccessToken trades a refresh credential for a short-lived access JWT
// (aud = runespace-id). This endpoint is public (credential-authed, no session),
// so it can run from the refresh loop without a live console session.
func (c *Client) ExchangeAccessToken(ctx context.Context, refreshToken string) (*AccessToken, error) {
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/auth/runespace/token", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out AccessToken
	if err := c.do(req, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// IsNotFound reports whether err is a 404 from the cloud.
func IsNotFound(err error) bool {
	ae, ok := err.(*APIError)
	return ok && ae.Status == http.StatusNotFound
}
