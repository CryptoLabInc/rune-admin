// Package console implements the local console BFF: the loopback PKCE auth
// endpoints against runespace-cloud, a persisted cookie-session store, the
// origin/cookie middleware, and the HTTP handler that composes them with the
// embedded SPA and the cookie-gated /api/v1 surface. It binds 127.0.0.1 only
// and is served by internal/server.
package console

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/cloud"
	"github.com/CryptoLabInc/rune-console/internal/server"
)

// Deps are the primitives internal/server and the daemon inject to build the
// console handler.
type Deps struct {
	Port       int
	APIBaseURL string
	WebBaseURL string
	DB         *sql.DB
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
	// TeamHash is this console's team_secret fingerprint (crypto.TeamHash of the
	// configured team_secret). It is sent with a workspace create so the cloud
	// records which install owns the runespace, and compared on status so a
	// reinstalled console (whose team_secret — and thus fingerprint — changed) can
	// detect that the cloud-held runespace is orphaned. Empty disables both.
	TeamHash string
	// OwnerRegistrar, when set, runs whenever the console owner is established:
	// once at handler construction when the console is already claimed (a
	// restarted daemon must re-derive the owner's org-admin authority without
	// waiting for a login), and after the owner is bound on a successful login.
	// It must be idempotent — it runs on every login — and seeds neither a member
	// row nor any group membership (admin reach is granted, not implied). A
	// returned error is logged and does not block login. nil skips registration.
	OwnerRegistrar func(email, displayName string) error
	// Updates provides the local, privilege-separated update check/request
	// boundary. The implementation may enqueue work for a root-owned helper,
	// but it must never perform the update in this HTTP process.
	Updates UpdateManager
	Logger  *slog.Logger
}

// Service holds the wired collaborators shared by the auth handlers.
type Service struct {
	apiBase       string
	webBase       string
	selfBase      string // http://127.0.0.1:<port>
	cloud         *cloud.Client
	sessions      *sessionStore
	owner         *ownerStore
	logins        *loginStore
	dp            *Dataplane                            // nil when no connector is wired
	teamHash      string                                // team_secret fingerprint for orphan detection ("" disables)
	engineReady   func() bool                           // reports data-plane engine connection status
	inviter       InviteIssuer                          // nil when self-invite issuance is not wired
	registerOwner func(email, displayName string) error // derive the org admin from the owner (idempotent; seeds no member row); nil when unwired
	updates       UpdateManager                         // nil when the privileged update helper is not installed/wired
	log           *slog.Logger
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
		apiBase:       d.APIBaseURL,
		webBase:       d.WebBaseURL,
		selfBase:      "http://127.0.0.1:" + strconv.Itoa(d.Port),
		cloud:         cloud.New(d.APIBaseURL),
		sessions:      sessions,
		owner:         owner,
		logins:        newLoginStore(),
		teamHash:      d.TeamHash,
		engineReady:   func() bool { return false },
		inviter:       d.Inviter,
		registerOwner: d.OwnerRegistrar,
		updates:       d.Updates,
		log:           log,
	}

	// Replay for an already-claimed console — see Deps.OwnerRegistrar. A read
	// failure is NOT treated as "unclaimed": that would leave the console with
	// no org admin until someone completes a fresh browser login, which a live
	// cookie session actively prevents them from needing to do, so say it loudly.
	switch o, oerr := owner.get(); {
	case oerr != nil:
		log.Error("console: could not read the owner claim at boot — the org admin is NOT set; "+
			"restart once the session database is readable", "err", oerr.Error())
	case o != nil:
		s.registerOwnerBestEffort(o.Email, o.Me, "boot")
	default:
		// Say so rather than start silently: until someone logs in there is no
		// org admin, so admin-gated calls are refused, and the account that
		// claims the console decides who holds that authority for good.
		log.Info("console: not yet claimed — the first account to log in becomes the console owner and org admin")
	}

	var dp *Dataplane
	if d.Connector != nil {
		dp, err = newDataplane(d.DB, s.cloud, d.Connector, log, d.TeamHash)
		if err != nil {
			return nil, nil, err
		}
		s.dp = dp
		s.engineReady = d.Connector.EngineReady
	}

	mux := http.NewServeMux()

	// Catch-all: SPA + static assets + deep-link fallback to index.html.
	mux.Handle("/", s.spaHandler())

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

	// Self-update endpoints are loopback-only, same-origin, session-gated, and
	// explicitly owner-gated. Applying an update is only an asynchronous request
	// to the separately installed root helper; the HTTP daemon never elevates or
	// stops itself.
	if s.updates != nil {
		ownerOnly := func(h http.Handler) http.Handler {
			return s.origin(s.requireSession(s.requireOwner(h)))
		}
		mux.Handle("GET /api/v1/system/update", ownerOnly(http.HandlerFunc(s.handleUpdateStatus)))
		mux.Handle("POST /api/v1/system/update", ownerOnly(http.HandlerFunc(s.handleUpdateRequest)))
	}

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
	// and withOperator injects the authenticated operator (audit actor + cloud
	// session cookie for the invite relay). When no domain handler is wired the
	// group falls back to the 501 skeleton.
	if d.DomainHandler != nil {
		mux.Handle("/api/v1/", s.origin(s.requireSession(s.withOperator(http.StripPrefix("/api/v1", d.DomainHandler)))))
	} else {
		mux.Handle("/api/v1/", s.origin(s.requireSession(http.HandlerFunc(apiNotImplemented))))
	}

	return mux, dp, nil
}

// registerOwnerBestEffort runs the owner registrar (org-admin derivation only;
// no member row is seeded) for an established owner, taking the display name
// from the cloud principal. It is the single place that owns the "never block on a registrar
// failure" policy: the owner is bound and the session is valid either way, and
// the registration self-heals on the next login or boot. A nil registrar (not
// wired) is a no-op. `at` names the call site for the log line.
func (s *Service) registerOwnerBestEffort(email string, me json.RawMessage, at string) {
	if s.registerOwner == nil {
		return
	}
	if err := s.registerOwner(email, nameFromMe(me, email)); err != nil {
		s.log.Warn("console: owner registration failed", "at", at, "err", err.Error())
	}
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
				// TODO(plan-source): the account's subscription plan. The cloud
				// source is undecided (workspace tier vs principal vs billing
				// API), so this is a placeholder. Replace this one line with the
				// real lookup once the source is settled; the wire contract
				// (lowercase string, open value set) stays the same.
				"plan": "free",
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

// withOperator tags the request context with the authenticated operator's email
// (audit actor) and their runespace-cloud session cookie, so the mounted admin/
// domain handlers audit the real principal and the cloud-relay invite mailer can
// authenticate the send as the operator. It runs after requireSession, so a
// session is guaranteed present; a principal missing an email flows through as ""
// (audited as "unknown").
func (s *Service) withOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if c, err := r.Cookie(cookieName); err == nil {
			if sess := s.sessions.get(c.Value); sess != nil {
				ctx = server.WithActor(ctx, emailFromMe(sess.Me))
				ctx = server.WithCloudCookie(ctx, sess.CloudCookie())
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
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
//
// This is the console's ingress for the cloud identity, so it is also where
// that identity is canonicalized: the address is lower-cased and trimmed before
// it reaches any downstream keyspace. Everything the email keys — the org
// admin, the member registry, token usernames, the audit/grant actor — compares
// case-SENSITIVELY, while the owner lock compares with EqualFold; without one
// canonical form, two spellings of one human could key two member rows or leave
// a minted admin token failing IsOrgAdmin. Google returns a canonical lower-case
// address today, but nothing in this system enforces that, so it is pinned here
// rather than assumed.
//
// Use rawEmailFromMe instead when handing the address to a system that owns its
// own interpretation of it.
func emailFromMe(me json.RawMessage) string {
	return canonicalEmail(rawEmailFromMe(me))
}

// rawEmailFromMe returns the principal's address exactly as the cloud spelled
// it. Reserved for handing the address OUTWARD (mail delivery), where the
// receiving system — not this one — decides what the local part means: RFC 5321
// leaves local-part interpretation to the destination host, so canonicalizing an
// outbound recipient can address a different mailbox than the one that signed
// in. Every internal key uses emailFromMe.
func rawEmailFromMe(me json.RawMessage) string {
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

// canonicalEmail is the one spelling every identity keyspace agrees on.
func canonicalEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// nameFromMe extracts the display name from the cached cloud principal (GET
// /me: {id, email, name, picture, ...}), falling back to `fallback` when the
// name is absent or unparseable.
func nameFromMe(me json.RawMessage, fallback string) string {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(me, &p); err == nil && strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	return fallback
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
func (s *Service) spaHandler() http.Handler {
	if fsys, ok := spaFS(); ok {
		return fsSPA(fsys)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, placeholderHTML)
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
