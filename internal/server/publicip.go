package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// publicIPServices are external echo endpoints that return the caller's public
// IPv4 as a bare string. Tried in order; the first valid IP wins.
var publicIPServices = []string{
	"https://ifconfig.me/ip",
	"https://api.ipify.org",
	"https://icanhazip.com",
}

// DetectPublicIP returns the host's public IPv4 by asking an external echo
// service (ifconfig.me, with fallbacks). It is used to advertise a reachable
// endpoint in the invite registration string — a remote rune-mcp cannot reach a
// loopback/private address, and the console's TLS cert already carries the
// public IP in its SAN. Returns "" if no service answers within the timeout.
func DetectPublicIP(ctx context.Context) string {
	client := &http.Client{Timeout: 5 * time.Second}
	for _, url := range publicIPServices {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		_ = resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}
