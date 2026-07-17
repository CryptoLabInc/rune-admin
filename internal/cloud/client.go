// Package cloud is a thin HTTP client for the public runespace-cloud control
// plane. The console drives it server-to-server: it completes the loopback
// PKCE token exchange, then reads the principal with the resulting session
// cookie.
//
// Because these calls originate from the Go http.Client (not a browser) they
// carry no Origin / Sec-Fetch-* headers, which is exactly what the cloud's
// server-to-server guard requires — never add those headers here.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to a single runespace-cloud API origin.
type Client struct {
	base string
	http *http.Client
}

// New builds a Client for the given API base URL (e.g. https://api.runespace.click).
func New(baseURL string) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// AuthorizeURL builds the loopback PKCE authorize URL the browser is sent to.
func (c *Client) AuthorizeURL(redirectURI, state, codeChallenge string) string {
	q := url.Values{}
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	return c.base + "/auth/local/authorize?" + q.Encode()
}

// LocalToken is the JSON returned by POST /auth/local/token.
type LocalToken struct {
	SessionToken string `json:"session_token"`
	CookieName   string `json:"cookie_name"`
	TokenType    string `json:"token_type"`
	ExpiresAt    string `json:"expires_at"`
}

// ExchangeLocalToken completes the loopback handshake: it trades the single-use
// code (plus the PKCE verifier and the exact redirect_uri) for a local-console
// session token.
func (c *Client) ExchangeLocalToken(ctx context.Context, code, verifier, redirectURI string) (*LocalToken, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/auth/local/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var out LocalToken
	if err := c.do(req, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Me fetches the authenticated principal (GET /api/v1/me) as raw JSON, for the
// console to display login state. Returns (nil, nil) if unauthenticated (401).
func (c *Client) Me(ctx context.Context, sessionCookie string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/me", nil)
	if err != nil {
		return nil, err
	}
	raw, status, err := c.raw(req, sessionCookie)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, nil
	}
	if status/100 != 2 {
		return nil, apiErrorFrom(status, raw)
	}
	return json.RawMessage(raw), nil
}

// SendInvite posts a single-recipient team invite to the cloud public relay
// (POST /api/v1/invites). registrationString is a credential (it carries the
// wrapping token): it is passed verbatim to the cloud and never logged here.
// inviterName and expiry are display-only fields the cloud renders into the
// email. A non-nil error means the cloud rejected the request or every send
// failed; a nil error means at least this recipient was accepted for delivery.
func (c *Client) SendInvite(ctx context.Context, sessionCookie, toEmail, toName, registrationString, inviterName, expiry string) error {
	body := map[string]any{
		"inviter_name": inviterName,
		"expiry":       expiry,
		"recipients": []map[string]string{{
			"email":               toEmail,
			"name":                toName,
			"registration_string": registrationString,
		}},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/invites", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	var out struct {
		Sent []struct {
			Email string `json:"email"`
		} `json:"sent"`
		Failed []struct {
			Email string `json:"email"`
			Error string `json:"error"`
		} `json:"failed"`
	}
	if err := c.do(req, sessionCookie, &out); err != nil {
		return err
	}
	if len(out.Sent) == 0 {
		if len(out.Failed) > 0 {
			return fmt.Errorf("cloud accepted the request but delivery failed: %s", out.Failed[0].Error)
		}
		return fmt.Errorf("cloud reported no invite sent")
	}
	return nil
}

// RevokeSession best-effort ends the cloud-side session bound to sessionCookie.
// The public endpoint is not settled yet (BFF spec §12-1), so this is a no-op
// today; logout still destroys the local session record regardless. Kept as a
// seam so the revoke call can be wired without touching the logout handler.
func (c *Client) RevokeSession(_ context.Context, _ string) error { return nil }

// APIError carries a runespace-cloud error body ({code, message}) plus the HTTP status.
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("runespace-cloud %d %s: %s", e.Status, e.Code, e.Message)
}

// IsUnauthorized reports whether err is a 401 from the cloud (stale session).
func IsUnauthorized(err error) bool {
	ae, ok := err.(*APIError)
	return ok && ae.Status == http.StatusUnauthorized
}

func apiErrorFrom(status int, body []byte) *APIError {
	ae := &APIError{Status: status}
	_ = json.Unmarshal(body, ae) // best-effort; leaves code/message empty on non-JSON
	if ae.Message == "" {
		ae.Message = strings.TrimSpace(string(body))
	}
	return ae
}

// do sends req (optionally with a session Cookie header) and decodes a 2xx JSON
// body into out. Non-2xx becomes an *APIError.
func (c *Client) do(req *http.Request, sessionCookie string, out any) error {
	body, status, err := c.raw(req, sessionCookie)
	if err != nil {
		return err
	}
	if status/100 != 2 {
		return apiErrorFrom(status, body)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

// raw sends req and returns the body + status. It attaches the session cookie
// verbatim (name=value) and never adds Origin/Sec-Fetch headers.
func (c *Client) raw(req *http.Request, sessionCookie string) ([]byte, int, error) {
	if sessionCookie != "" {
		req.Header.Set("Cookie", sessionCookie)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
