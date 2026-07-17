package console

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"time"
)

// sessionTTL is the fixed console-session lifetime. Sessions do not slide;
// 12h from issue is the single source of truth for expiry (BFF spec §5, O4).
const sessionTTL = 12 * time.Hour

// Session is one authenticated console login. The cookie carries the opaque
// ID; the runespace-cloud bearer token stays server-side and is replayed to
// the cloud as CloudCookie() on server-to-server calls.
type Session struct {
	ID         string
	Token      string // runespace-cloud session token (secret, never leaves the process except to the cloud)
	CookieName string // the cloud's cookie name for Token
	ExpiresAt  time.Time
	Me         json.RawMessage
}

// CloudCookie renders the "name=value" the console replays to runespace-cloud.
func (s *Session) CloudCookie() string { return s.CookieName + "=" + s.Token }

// sessionStore persists console sessions in SQLite, keyed by opaque cookie ID.
// Multiple sessions may coexist (one per browser/profile); the console user is
// a single admin but session rows are N. Expiry is enforced lazily on read.
type sessionStore struct {
	db  *sql.DB
	log *slog.Logger
}

const sessionSchema = `
CREATE TABLE IF NOT EXISTS console_session (
  session_id      TEXT PRIMARY KEY,
  runespace_token TEXT NOT NULL,
  cookie_name     TEXT NOT NULL,
  me              BLOB,
  created_at      TEXT NOT NULL,
  expires_at      TEXT NOT NULL
);`

// newSessionStore ensures the schema and prunes any already-expired rows so a
// restart never resurrects a lapsed login.
func newSessionStore(db *sql.DB, log *slog.Logger) (*sessionStore, error) {
	if _, err := db.Exec(sessionSchema); err != nil {
		return nil, err
	}
	st := &sessionStore{db: db, log: log}
	if _, err := db.Exec(`DELETE FROM console_session WHERE expires_at < ?`, nowRFC3339()); err != nil {
		st.warn("prune expired sessions", err)
	}
	return st, nil
}

// create mints a new session (12h TTL) and returns it with its fresh ID.
func (st *sessionStore) create(token, cookieName string, me json.RawMessage) (*Session, error) {
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s := &Session{ID: id, Token: token, CookieName: cookieName, ExpiresAt: now.Add(sessionTTL), Me: me}
	if _, err := st.db.Exec(
		`INSERT INTO console_session (session_id, runespace_token, cookie_name, me, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		s.ID, s.Token, s.CookieName, []byte(me), now.Format(time.RFC3339), s.ExpiresAt.Format(time.RFC3339),
	); err != nil {
		return nil, err
	}
	return s, nil
}

// get returns the session for id, or nil if absent or expired. An expired row
// is deleted in place so it is never reported as logged-in.
func (st *sessionStore) get(id string) *Session {
	if id == "" {
		return nil
	}
	var (
		s       Session
		me      []byte
		expires string
	)
	err := st.db.QueryRow(
		`SELECT session_id, runespace_token, cookie_name, me, expires_at FROM console_session WHERE session_id = ?`, id,
	).Scan(&s.ID, &s.Token, &s.CookieName, &me, &expires)
	if err != nil {
		if err != sql.ErrNoRows {
			st.warn("read session", err)
		}
		return nil
	}
	if t, perr := time.Parse(time.RFC3339, expires); perr == nil {
		s.ExpiresAt = t
	}
	if !s.ExpiresAt.IsZero() && time.Now().After(s.ExpiresAt) {
		st.delete(id)
		return nil
	}
	if len(me) > 0 {
		s.Me = json.RawMessage(me)
	}
	return &s
}

// delete removes the session row (idempotent).
func (st *sessionStore) delete(id string) {
	if _, err := st.db.Exec(`DELETE FROM console_session WHERE session_id = ?`, id); err != nil {
		st.warn("delete session", err)
	}
}

func (st *sessionStore) warn(msg string, err error) {
	if st.log != nil {
		st.log.Warn("console session store: "+msg, "err", err.Error())
	}
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// newSessionID returns a 128-bit opaque, URL-safe session identifier.
func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
