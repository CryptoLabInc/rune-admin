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

// owner is the bound console administrator.
type owner struct {
	Email   string
	Me      json.RawMessage
	BoundAt time.Time
}

// newOwnerStore ensures the schema and returns the store.
func newOwnerStore(db *sql.DB, log *slog.Logger) (*ownerStore, error) {
	if err := EnsureDBSchema(db); err != nil {
		return nil, err
	}
	return &ownerStore{db: db, log: log}, nil
}

// get returns the bound owner, (nil, nil) when the console is not yet claimed,
// or an error when the claim could not be read. Those last two are deliberately
// distinct: the owner is the org admin, so treating a failed read as "unclaimed"
// would silently leave the console with no admin (and no retry) for the rest of
// the process.
//
// The stored email is canonicalized on the way out, matching the ingress
// normalization in emailFromMe. A row written before that normalization existed
// (or by any principal spelling its address differently) would otherwise key the
// org admin and the member registry differently at boot than at login.
func (st *ownerStore) get() (*owner, error) {
	var (
		o       owner
		me      []byte
		boundAt string
	)
	err := st.db.QueryRow(`SELECT email, me, bound_at FROM console_owner WHERE id = 1`).
		Scan(&o.Email, &me, &boundAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		st.warn("read owner", err)
		return nil, err
	}
	o.Email = canonicalEmail(o.Email)
	if t, perr := time.Parse(time.RFC3339, boundAt); perr == nil {
		o.BoundAt = t
	}
	if len(me) > 0 {
		o.Me = json.RawMessage(me)
	}
	return &o, nil
}

// bindIfAbsent claims the console for email when it is not yet bound, and always
// returns the authoritative owner afterwards. The INSERT OR IGNORE against the
// fixed id=1 row makes the claim atomic: if two first-logins race, exactly one
// wins and the loser reads back the winner — so the caller then refuses the
// mismatched account instead of overwriting the binding.
//
// The email is stored canonicalized (the claim is the org-admin key, and this
// row is never rewritten afterwards, so the spelling persisted here is the one
// the system lives with).
func (st *ownerStore) bindIfAbsent(email string, me json.RawMessage) (*owner, error) {
	if _, err := st.db.Exec(
		`INSERT OR IGNORE INTO console_owner (id, email, me, bound_at) VALUES (1, ?, ?, ?)`,
		canonicalEmail(email), []byte(me), time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return nil, err
	}
	o, err := st.get()
	if err != nil {
		return nil, err
	}
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
