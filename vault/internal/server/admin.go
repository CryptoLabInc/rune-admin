package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
)

// adminSocketMode is 0660: owner (runevault) + group (runevault) can connect;
// other users cannot. Installers add trusted operators to the runevault group.
const adminSocketMode = 0o660

// AdminFromConfig is an AdminFactory suitable for production: it binds the
// UDS at v.cfg.Server.Admin.Socket with mode 0660 (umask + chmod
// belt+suspenders), serves the route table, and returns a closer that
// gracefully stops the http.Server and unlinks the socket.
func AdminFromConfig(ctx context.Context, v *Vault) (func(context.Context) error, error) {
	cfg := v.Config()
	socket := cfg.Server.Admin.Socket
	if socket == "" {
		return nil, errors.New("server.admin.socket is empty")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0o750); err != nil {
		return nil, fmt.Errorf("admin: mkdir socket dir: %w", err)
	}
	// Stale socket recovery: remove leftover paths before Listen. Ignore
	// missing-file errors; surface anything else (eg. wrong type).
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("admin: remove stale socket: %w", err)
	}

	// Umask 0o007 lets the socket inherit group rw while blocking others.
	prevMask := syscall.Umask(0o007)
	lis, err := net.Listen("unix", socket)
	syscall.Umask(prevMask)
	if err != nil {
		return nil, fmt.Errorf("admin: listen unix %s: %w", socket, err)
	}
	// Belt + suspenders: even if umask leaked, force 0660.
	if err := os.Chmod(socket, adminSocketMode); err != nil {
		_ = lis.Close()
		return nil, fmt.Errorf("admin: chmod socket: %w", err)
	}

	mux := buildAdminMux(v)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("admin: server error", "err", err)
		}
	}()
	slog.Info("vault: admin UDS listening", "socket", socket, "mode", "0660")

	shutdown := func(ctx context.Context) error {
		err := srv.Shutdown(ctx)
		// Always best-effort unlink; the socket is gone if Shutdown succeeded.
		_ = os.Remove(socket)
		return err
	}
	return shutdown, nil
}

// buildAdminMux wires the admin route table. Exposed for tests.
// Daemon lifecycle (start/stop/restart) is owned by the OS service manager
// (systemd / launchd) and is intentionally not exposed over the admin socket.
func buildAdminMux(v *Vault) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /tokens", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tokens": v.Tokens().ListTokens()})
	})
	mux.HandleFunc("GET /roles", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"roles": v.Tokens().ListRoles()})
	})

	mux.HandleFunc("POST /tokens", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			User        string `json:"user"`
			Role        string `json:"role"`
			ExpiresDays *int   `json:"expires_days"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.User == "" || body.Role == "" {
			writeError(w, http.StatusBadRequest, "Missing required fields: user, role")
			return
		}
		tok, err := v.Tokens().AddToken(body.User, body.Role, body.ExpiresDays)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, tokenJSON(tok))
	})

	mux.HandleFunc("POST /tokens/{user}/rotate", func(w http.ResponseWriter, r *http.Request) {
		user := r.PathValue("user")
		tok, err := v.Tokens().RotateToken(user)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tokenJSON(tok))
	})

	mux.HandleFunc("POST /tokens/_rotate_all", func(w http.ResponseWriter, r *http.Request) {
		toks, err := v.Tokens().RotateAllTokens()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		entries := make([]map[string]string, 0, len(toks))
		for _, t := range toks {
			entries = append(entries, map[string]string{
				"user": t.User, "token": t.Token, "role": t.Role,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"rotated": len(toks),
			"tokens":  entries,
		})
	})

	mux.HandleFunc("DELETE /tokens/{user}", func(w http.ResponseWriter, r *http.Request) {
		user := r.PathValue("user")
		if v.Tokens().RevokeToken(user) {
			writeJSON(w, http.StatusOK, map[string]string{
				"message": fmt.Sprintf("Revoked token for '%s'", user),
			})
			return
		}
		writeError(w, http.StatusNotFound, fmt.Sprintf("No token found for user '%s'", user))
	})

	mux.HandleFunc("POST /roles", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name      string   `json:"name"`
			Scope     []string `json:"scope"`
			TopK      *int     `json:"top_k"`
			RateLimit string   `json:"rate_limit"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Name == "" || len(body.Scope) == 0 || body.TopK == nil || body.RateLimit == "" {
			writeError(w, http.StatusBadRequest, "Missing required fields: name, scope, top_k, rate_limit")
			return
		}
		role, err := v.Tokens().AddRole(body.Name, body.Scope, *body.TopK, body.RateLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, roleJSON(role))
	})

	mux.HandleFunc("PUT /roles/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var raw map[string]json.RawMessage
		if err := readJSON(r, &raw); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		opts := tokens.UpdateRoleOpts{}
		if v, ok := raw["scope"]; ok {
			var s []string
			if err := json.Unmarshal(v, &s); err != nil {
				writeError(w, http.StatusBadRequest, "scope must be a string array")
				return
			}
			opts.Scope = &s
		}
		if v, ok := raw["top_k"]; ok {
			var n int
			if err := json.Unmarshal(v, &n); err != nil {
				writeError(w, http.StatusBadRequest, "top_k must be an integer")
				return
			}
			opts.TopK = &n
		}
		if v, ok := raw["rate_limit"]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				writeError(w, http.StatusBadRequest, "rate_limit must be a string")
				return
			}
			opts.RateLimit = &s
		}
		if opts.Scope == nil && opts.TopK == nil && opts.RateLimit == nil {
			writeError(w, http.StatusBadRequest, "No fields to update")
			return
		}
		role, err := v.Tokens().UpdateRole(name, opts)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, roleJSON(role))
	})

	mux.HandleFunc("DELETE /roles/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if err := v.Tokens().DeleteRole(name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"message": fmt.Sprintf("Deleted role '%s'", name),
		})
	})

	// 404 fallback for routes that didn't match.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("No route for %s %s", r.Method, r.URL.Path))
	})

	return mux
}

func tokenJSON(t *tokens.Token) map[string]any {
	exp := t.Expires
	if exp == "" {
		exp = "never"
	}
	return map[string]any{
		"user":      t.User,
		"token":     t.Token,
		"role":      t.Role,
		"issued_at": t.IssuedAt,
		"expires":   exp,
	}
}

func roleJSON(r *tokens.Role) map[string]any {
	return map[string]any{
		"name":       r.Name,
		"scope":      r.Scope,
		"top_k":      r.TopK,
		"rate_limit": r.RateLimit,
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	buf, err := json.Marshal(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, dst any) error {
	if r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

// SocketURL is a stable host used in the URL for UDS HTTP. Clients
// substitute the actual socket file via http.Transport.DialContext.
const SocketURL = "http://admin"

// SanitizePathForLog hides socket directories that contain user names or
// secret prefixes. Used by status reporting.
func SanitizePathForLog(p string) string {
	if p == "" {
		return ""
	}
	return strings.TrimSuffix(p, "/")
}
