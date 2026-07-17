// Package console implements the local console BFF: the loopback PKCE auth
// endpoints against runespace-cloud, a persisted cookie-session store, the
// origin/cookie middleware, and the HTTP handler that composes them with the
// embedded SPA, the cookie-gated /api/v1 surface (skeleton), and the admin
// operations. It binds 127.0.0.1 only and is served by internal/server.
package console

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/cloud"
	"github.com/CryptoLabInc/rune-console/internal/server"
)

// Deps are the primitives internal/server and the daemon inject to build the
// console handler. AdminHandler is mounted (cookie-gated) under /admin/;
// passing nil omits the admin surface.
type Deps struct {
	Port         int
	APIBaseURL   string
	WebBaseURL   string
	FrontendDir  string // empty => placeholder page (embed wired separately)
	DB           *sql.DB
	AdminHandler http.Handler
	// DomainHandler is the /api/v1 domain surface (teams, users, memberships,
	// invitations) built by internal/server with direct RBAC-store access. It is
	// mounted origin + session gated, with the /api/v1 prefix stripped and the
	// authenticated operator injected as the audit actor. Nil leaves the domain
	// routes returning 501 (skeleton).
	DomainHandler http.Handler
	// Connector attaches the dialed runespace engine to the gRPC server. When
	// set, the data-plane manager + /api/v1/workspace endpoints are mounted;
	// nil disables the data-plane flow (workspace routes fall through to the
	// stub).
	Connector DataplaneConnector
	// Inviter issues a self-invite (registration string) for the connection
	// test; nil omits the POST /api/v1/invite route.
	Inviter InviteIssuer
	// RunespaceInsecure dials the engine plaintext (local dev).
	RunespaceInsecure bool
	Logger            *slog.Logger
}

// Service holds the wired collaborators shared by the auth handlers.
type Service struct {
	apiBase     string
	webBase     string
	selfBase    string // http://127.0.0.1:<port>
	cloud       *cloud.Client
	sessions    *sessionStore
	owner       *ownerStore
	logins      *loginStore
	dp          *Dataplane   // nil when no connector is wired
	engineReady func() bool  // reports data-plane engine connection status
	inviter     InviteIssuer // nil when self-invite issuance is not wired
	log         *slog.Logger
}

// NewHandler builds the console HTTP handler and, when a data-plane connector
// is provided, the Dataplane manager (which the caller must Start with the
// daemon context). The returned handler is bound to 127.0.0.1 by the caller
// (loopback invariant).
func NewHandler(d Deps) (http.Handler, *Dataplane, error) {
	if d.DB == nil {
		return nil, nil, errors.New("console: DB is required")
	}
	if d.APIBaseURL == "" {
		return nil, nil, errors.New("console: APIBaseURL is required")
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	sessions, err := newSessionStore(d.DB, log)
	if err != nil {
		return nil, nil, err
	}
	owner, err := newOwnerStore(d.DB, log)
	if err != nil {
		return nil, nil, err
	}
	s := &Service{
		apiBase:     d.APIBaseURL,
		webBase:     d.WebBaseURL,
		selfBase:    "http://127.0.0.1:" + strconv.Itoa(d.Port),
		cloud:       cloud.New(d.APIBaseURL),
		sessions:    sessions,
		owner:       owner,
		logins:      newLoginStore(),
		engineReady: func() bool { return false },
		inviter:     d.Inviter,
		log:         log,
	}

	var dp *Dataplane
	if d.Connector != nil {
		dp, err = newDataplane(d.DB, s.cloud, d.Connector, d.RunespaceInsecure, log)
		if err != nil {
			return nil, nil, err
		}
		s.dp = dp
		s.engineReady = d.Connector.EngineReady
	}

	mux := http.NewServeMux()

	// Catch-all: SPA + static assets + deep-link fallback to index.html.
	mux.Handle("/", s.spaHandler(d.FrontendDir))

	// API namespaces must 404 (JSON) rather than fall back to the SPA. The
	// specific routes below take precedence over these subtree guards.
	mux.HandleFunc("/api/", notFoundJSON)
	mux.HandleFunc("/console/", notFoundJSON)
	mux.HandleFunc("/auth/", notFoundJSON)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Loopback callback: cross-site top-level navigation, so it sits OUTSIDE
	// the origin guard and is protected by the single-use state instead.
	mux.HandleFunc("GET /auth/callback", s.handleCallback)

	// Console BFF endpoints — origin-guarded. session/start self-judge (no
	// cookie requirement); logout requires a live session.
	mux.Handle("POST /console/auth/start", s.origin(http.HandlerFunc(s.handleAuthStart)))
	mux.Handle("GET /console/session", s.origin(http.HandlerFunc(s.handleSession)))
	// Logout is idempotent (doc: 204 even with no live session), so it does NOT
	// sit behind requireSession — a stale-session logout must still return 204,
	// not 401.
	mux.Handle("POST /console/auth/logout", s.origin(http.HandlerFunc(s.handleLogout)))

	// Workspace (data-plane) endpoints: provision + connect the runespace
	// engine, and report status. Mounted only when a connector is wired.
	if dp != nil {
		mux.Handle("POST /api/v1/workspace", s.origin(s.requireSession(http.HandlerFunc(s.handleWorkspaceConnect))))
		mux.Handle("GET /api/v1/workspace", s.origin(s.requireSession(http.HandlerFunc(s.handleWorkspaceStatus))))
		mux.Handle("DELETE /api/v1/workspace", s.origin(s.requireSession(http.HandlerFunc(s.handleWorkspaceDelete))))
		mux.Handle("POST /api/v1/workspace/stop", s.origin(s.requireSession(http.HandlerFunc(s.handleWorkspaceStop))))
		mux.Handle("POST /api/v1/workspace/start", s.origin(s.requireSession(http.HandlerFunc(s.handleWorkspaceStart))))
	}

	// Self-invite (connection-test) endpoint: issues a registration string for
	// the operator and mails it via the cloud public API. Mounted only when an
	// issuer is wired.
	if s.inviter != nil {
		mux.Handle("POST /api/v1/invite", s.origin(s.requireSession(http.HandlerFunc(s.handleInvite))))
	}

	// Domain API (origin + cookie gated): teams, users, memberships, invitations.
	// The specific /api/v1/workspace* and /api/v1/invite routes above take
	// precedence (Go 1.22 pattern specificity); this subtree handles the rest.
	// The /api/v1 prefix is stripped so the mounted mux sees /teams/tree etc.,
	// and withActor injects the authenticated operator as the audit actor. When
	// no domain handler is wired the group falls back to the 501 skeleton.
	if d.DomainHandler != nil {
		mux.Handle("/api/v1/", s.origin(s.requireSession(s.withActor(http.StripPrefix("/api/v1", d.DomainHandler)))))
	} else {
		mux.Handle("/api/v1/", s.origin(s.requireSession(http.HandlerFunc(apiNotImplemented))))
	}

	// Admin operations (origin + cookie gated), mounted under /admin/.
	if d.AdminHandler != nil {
		mux.Handle("/admin/", s.origin(s.requireSession(http.StripPrefix("/admin", d.AdminHandler))))
	}

	return mux, dp, nil
}

// handleSession (GET /console/session) is the route guard's single source of
// truth, so it always returns 200 — never 401.
func (s *Service) handleSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if sess := s.sessions.get(c.Value); sess != nil {
			resp := map[string]any{
				"logged_in":        true,
				"expires_at":       sess.ExpiresAt.UTC().Format(time.RFC3339),
				"engine_connected": s.engineReady(),
			}
			if len(sess.Me) > 0 {
				resp["me"] = meWithAvatar(sess.Me)
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logged_in": false, "engine_connected": s.engineReady()})
}

// origin enforces the same-origin/localhost-CSRF guard: requests with a
// cross-site Sec-Fetch-Site are refused. Applied to /console/* and /api/v1/*.
func (s *Service) origin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Sec-Fetch-Site") {
		case "", "same-origin", "none":
			next.ServeHTTP(w, r)
		default:
			writeError(w, http.StatusForbidden, "ORIGIN_FORBIDDEN", "cross-origin request refused")
		}
	})
}

// withActor tags the request context with the authenticated operator's email
// (from the rc_session principal) so the mounted domain handlers audit the real
// principal. It runs after requireSession, so a session is guaranteed present;
// a principal missing an email flows through as "" (audited as "unknown").
func (s *Service) withActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := ""
		if c, err := r.Cookie(cookieName); err == nil {
			if sess := s.sessions.get(c.Value); sess != nil {
				actor = emailFromMe(sess.Me)
			}
		}
		next.ServeHTTP(w, r.WithContext(server.WithActor(r.Context(), actor)))
	})
}

// meWithAvatar returns the cached cloud principal with an `avatar` field the
// SPA expects (design doc me:{email,avatar}). The cloud /me principal carries
// the image under `picture`, so avatar mirrors picture when avatar is absent.
// On a parse failure the raw principal is returned verbatim (never drop it).
func meWithAvatar(me json.RawMessage) any {
	var m map[string]any
	if err := json.Unmarshal(me, &m); err != nil || m == nil {
		return me
	}
	if _, has := m["avatar"]; !has {
		if pic, ok := m["picture"].(string); ok && pic != "" {
			m["avatar"] = pic
		}
	}
	return m
}

// emailFromMe extracts the email from the cached cloud principal (GET /me:
// {id, email, name, picture, ...}). Returns "" when absent or unparseable.
func emailFromMe(me json.RawMessage) string {
	if len(me) == 0 {
		return ""
	}
	var p struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(me, &p); err != nil {
		return ""
	}
	return p.Email
}

// requireSession rejects requests without a live console session cookie.
func (s *Service) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || s.sessions.get(c.Value) == nil {
			writeError(w, http.StatusUnauthorized, "SESSION_INVALID", "not logged in")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// spaHandler picks the SPA source: an explicit frontend_dir on disk, else the
// embedded build baked into the binary, else a placeholder page (frontend not
// yet built). All variants fall back to index.html for client-side routes so
// deep links and refreshes work.
func (s *Service) spaHandler(dir string) http.Handler {
	if dir != "" {
		return diskSPA(dir)
	}
	if fsys, ok := spaFS(); ok {
		return fsSPA(fsys)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, placeholderHTML)
	})
}

// diskSPA serves the SPA from a directory (deep-link fallback to index.html).
func diskSPA(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			http.ServeFile(w, r, p)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

// fsSPA serves the SPA from an embedded fs.FS (deep-link fallback to index.html).
func fsSPA(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if f, err := fsys.Open(name); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		b, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
}

const placeholderHTML = `<!doctype html><meta charset="utf-8"><title>Rune Console</title>` +
	`<body style="font-family:system-ui;padding:3rem;max-width:40rem;margin:auto">` +
	`<h1>Rune Console</h1><p>The console backend is running. Build the SPA ` +
	`(<code>mise run fe:build</code>) and set <code>server.console.frontend_dir</code>, ` +
	`or wait for the embedded build.</p></body>`

var jsonNull = json.RawMessage("null")

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"code": code, "message": msg})
}

func notFoundJSON(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
}

func apiNotImplemented(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "domain API not implemented yet")
}
