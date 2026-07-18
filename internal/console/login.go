package console

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// cookieName is the opaque console session cookie. It carries only the local
// session ID — unrelated to the cloud's own cookie name (BFF spec §9).
const cookieName = "rc_session"

// loginTx is an in-flight loopback handshake, keyed by the opaque state value.
type loginTx struct {
	verifier    string
	redirectURI string
	created     time.Time
}

// loginStore tracks in-flight handshakes (normally just one). Entries older
// than 10 minutes are evicted on put (the cloud code TTL is ~60s).
type loginStore struct {
	mu  sync.Mutex
	txs map[string]loginTx
}

func newLoginStore() *loginStore { return &loginStore{txs: map[string]loginTx{}} }

func (l *loginStore) put(state string, tx loginTx) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, v := range l.txs {
		if time.Since(v.created) > 10*time.Minute {
			delete(l.txs, k)
		}
	}
	l.txs[state] = tx
}

// take returns and removes the tx for state (single-use).
func (l *loginStore) take(state string) (loginTx, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	tx, ok := l.txs[state]
	if ok {
		delete(l.txs, state)
	}
	return tx, ok
}

// handleAuthStart (POST /console/auth/start) begins a loopback PKCE handshake:
// it generates verifier/challenge/state, records the tx, and returns the
// runespace-cloud sign-in URL for the frontend to navigate the browser to.
func (s *Service) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	verifier, err := randToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "generate verifier")
		return
	}
	state, err := randToken(16)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "generate state")
		return
	}
	redirectURI := s.selfBase + "/auth/callback"
	s.logins.put(state, loginTx{verifier: verifier, redirectURI: redirectURI, created: time.Now()})

	// Route the browser through the visible /signin page (not straight at the
	// API authorize endpoint) so the user consciously passes through the web
	// origin; the page forwards to the authorize URL (adding intent).
	authorizeURL := s.cloud.AuthorizeURL(redirectURI, state, s256(verifier))
	signinURL := s.webBase + "/signin?authorize=" + url.QueryEscape(authorizeURL)
	writeJSON(w, http.StatusOK, map[string]string{"authorize_url": signinURL})
}

// handleCallback (GET /auth/callback) is the loopback redirect target. It sits
// outside the origin guard (cross-site top-level navigation) and is protected
// by the single-use state instead. On any failure it bounces the browser to
// /login?error=<code> with no cookie set; the codes are the only detail the
// frontend sees (BFF spec §4.2, LD8).
func (s *Service) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("error") != "" {
		s.failCallback(w, r, "provider")
		return
	}
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		s.failCallback(w, r, "invalid_state")
		return
	}
	tx, ok := s.logins.take(state) // single-use
	if !ok {
		s.failCallback(w, r, "invalid_state")
		return
	}

	tok, err := s.cloud.ExchangeLocalToken(r.Context(), code, tx.verifier, tx.redirectURI)
	if err != nil {
		s.log.Warn("console: local token exchange failed", "err", err.Error())
		s.failCallback(w, r, "exchange_failed")
		return
	}

	// Best-effort principal capture for the login-state display.
	var me = jsonNull
	if raw, merr := s.cloud.Me(r.Context(), tok.CookieName+"="+tok.SessionToken); merr == nil && raw != nil {
		me = raw
	}

	// Single-admin binding: the FIRST account to log in claims this console; a
	// later login by a different account is refused (BFF spec: the console is a
	// single-admin surface). The principal email is the stable ownership key.
	email := emailFromMe(me)
	if email == "" {
		// No principal email means ownership cannot be established or verified —
		// refuse rather than bind an empty owner (which would lock the console to
		// no one and let anyone in thereafter).
		s.log.Warn("console: login principal has no email; cannot establish console owner")
		s.failCallback(w, r, "no_identity")
		return
	}
	own, err := s.owner.bindIfAbsent(email, me)
	if err != nil {
		s.log.Warn("console: bind console owner failed", "err", err.Error())
		s.failCallback(w, r, "exchange_failed")
		return
	}
	if !strings.EqualFold(own.Email, email) {
		// A different account already owns this console. Best-effort revoke the
		// cloud session we just minted (this login gets no console session) and
		// bounce to the locked-out login screen naming the owning account.
		_ = s.cloud.RevokeSession(r.Context(), tok.CookieName+"="+tok.SessionToken)
		s.log.Warn("console: login refused — console is bound to another admin", "owner", own.Email, "attempted", email)
		s.failOwnerLocked(w, r, own.Email)
		return
	}

	// First-login registration (option A): ensure the console owner (the single
	// org admin) has a member registry row so they appear in member listings and
	// can be granted memberships by UUID like anyone. Idempotent — a no-op on
	// every later login — and creates zero group memberships (admin reach is
	// granted, never implied). Never blocks login (see the helper).
	s.registerOwnerBestEffort(email, me, "login")

	sess, err := s.sessions.create(tok.SessionToken, tok.CookieName, me)
	if err != nil {
		s.log.Warn("console: persist session failed", "err", err.Error())
		s.failCallback(w, r, "exchange_failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// No Max-Age/Expires: session cookie; the server-side 12h is the truth.
	})
	http.Redirect(w, r, s.selfBase+"/", http.StatusFound)
}

// handleLogout (POST /console/auth/logout) destroys the local session (the
// essence of logout), best-effort revokes the cloud session, and clears the
// cookie. It never touches the user's public web browser session.
func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if sess := s.sessions.get(c.Value); sess != nil {
			_ = s.cloud.RevokeSession(r.Context(), sess.CloudCookie()) // best-effort
		}
		s.sessions.delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

// failCallback clears any cookie and redirects the browser to the login page
// with the coarse error code. Detail stays in the server log.
func (s *Service) failCallback(w http.ResponseWriter, r *http.Request, code string) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	http.Redirect(w, r, s.selfBase+"/login?error="+url.QueryEscape(code), http.StatusFound)
}

// failOwnerLocked clears any cookie and bounces to the login page with the
// admin_locked code plus the owning account, so the SPA can tell the user which
// account manages this console (and whom to contact). Naming the owner is a
// deliberate choice: the console is a loopback-only surface, the owner email is
// not otherwise sensitive here, and the message is useless without it.
func (s *Service) failOwnerLocked(w http.ResponseWriter, r *http.Request, ownerAccount string) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	loc := s.selfBase + "/login?error=admin_locked&owner=" + url.QueryEscape(ownerAccount)
	http.Redirect(w, r, loc, http.StatusFound)
}

func randToken(nbytes int) (string, error) {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
