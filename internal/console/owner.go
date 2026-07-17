package console

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

// ownerStore records the single administrator this console is bound to. The
// console is a single-admin surface (BFF spec): the FIRST cloud account to
// complete login claims it, and every later login by a different account is
// refused (handleCallback). The claim is durable — it survives a restart — and
// is the local source of truth for "who owns this console", independent of the
// cloud session (which any workspace member could hold).
type ownerStore struct {
	db  *sql.DB
	log *slog.Logger
}

const ownerSchema = `
CREATE TABLE IF NOT EXISTS console_owner (
  id       INTEGER PRIMARY KEY CHECK (id = 1),
  email    TEXT NOT NULL,
  me       BLOB,
  bound_at TEXT NOT NULL
);`

// owner is the bound console administrator.
type owner struct {
	Email   string
	Me      json.RawMessage
	BoundAt time.Time
}

// newOwnerStore ensures the schema and returns the store.
func newOwnerStore(db *sql.DB, log *slog.Logger) (*ownerStore, error) {
	if _, err := db.Exec(ownerSchema); err != nil {
		return nil, err
	}
	return &ownerStore{db: db, log: log}, nil
}

// get returns the bound owner, or nil when the console is not yet claimed.
func (st *ownerStore) get() *owner {
	var (
		o       owner
		me      []byte
		boundAt string
	)
	err := st.db.QueryRow(`SELECT email, me, bound_at FROM console_owner WHERE id = 1`).
		Scan(&o.Email, &me, &boundAt)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			st.warn("read owner", err)
		}
		return nil
	}
	if t, perr := time.Parse(time.RFC3339, boundAt); perr == nil {
		o.BoundAt = t
	}
	if len(me) > 0 {
		o.Me = json.RawMessage(me)
	}
	return &o
}

// bindIfAbsent claims the console for email when it is not yet bound, and always
// returns the authoritative owner afterwards. The INSERT OR IGNORE against the
// fixed id=1 row makes the claim atomic: if two first-logins race, exactly one
// wins and the loser reads back the winner — so the caller then refuses the
// mismatched account instead of overwriting the binding.
func (st *ownerStore) bindIfAbsent(email string, me json.RawMessage) (*owner, error) {
	if _, err := st.db.Exec(
		`INSERT OR IGNORE INTO console_owner (id, email, me, bound_at) VALUES (1, ?, ?, ?)`,
		email, []byte(me), time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return nil, err
	}
	o := st.get()
	if o == nil {
		return nil, errors.New("console: owner row missing immediately after bind")
	}
	return o, nil
}

func (st *ownerStore) warn(msg string, err error) {
	if st.log != nil {
		st.log.Warn("console owner store: "+msg, "err", err.Error())
	}
}
