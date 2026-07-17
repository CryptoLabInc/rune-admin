package members

import (
	"errors"
	"path/filepath"
	"testing"
)

// primeStatus drives a fresh member through the real lifecycle hooks to the
// wanted pre-disable status (never through Update, which forbids forward hops).
func primeStatus(t *testing.T, s *Store, id, status string) {
	t.Helper()
	switch status {
	case StatusRegistered:
		// Add's entry state — nothing to do.
	case StatusInvited:
		if err := s.MarkInvited(id); err != nil {
			t.Fatalf("prime MarkInvited: %v", err)
		}
	case StatusActive:
		if err := s.MarkInvited(id); err != nil {
			t.Fatalf("prime MarkInvited: %v", err)
		}
		if err := s.Activate(id); err != nil {
			t.Fatalf("prime Activate: %v", err)
		}
	default:
		t.Fatalf("primeStatus: unsupported %q", status)
	}
}

// TestDisableRestoreReturnsToPriorStatus locks in restore-to-prior-status:
// disabled→X is allowed iff X is the status the member was disabled from, so
// a disable/restore round-trip can never advance the lifecycle (in particular
// registered→disabled→active must NOT mint an active — a billable,
// seat-counted label owned exclusively by Activate).
func TestDisableRestoreReturnsToPriorStatus(t *testing.T) {
	cases := []struct {
		name    string
		prior   string // status held when disabled
		restore string // requested restore target
		wantOK  bool
	}{
		{"registered->disabled->registered ok", StatusRegistered, StatusRegistered, true},
		{"registered->disabled->active rejected", StatusRegistered, StatusActive, false},
		{"registered->disabled->invited rejected", StatusRegistered, StatusInvited, false},
		{"invited->disabled->invited ok", StatusInvited, StatusInvited, true},
		{"invited->disabled->active rejected", StatusInvited, StatusActive, false},
		{"invited->disabled->registered rejected", StatusInvited, StatusRegistered, false},
		{"active->disabled->active ok", StatusActive, StatusActive, true},
		{"active->disabled->registered rejected", StatusActive, StatusRegistered, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewStore()
			m, err := s.Add("t@corp.com", "T")
			if err != nil {
				t.Fatal(err)
			}
			primeStatus(t, s, m.ID, c.prior)

			// Disable records the prior status.
			if _, err := s.Update(m.ID, nil, strptr(StatusDisabled)); err != nil {
				t.Fatalf("disable from %s: %v", c.prior, err)
			}
			got, _ := s.Get(m.ID)
			if got.DisabledFrom != c.prior {
				t.Fatalf("DisabledFrom = %q, want %q", got.DisabledFrom, c.prior)
			}
			// A disabled->disabled no-op must not clobber the recorded status.
			if _, err := s.Update(m.ID, nil, strptr(StatusDisabled)); err != nil {
				t.Fatalf("disabled->disabled no-op: %v", err)
			}
			if got, _ = s.Get(m.ID); got.DisabledFrom != c.prior {
				t.Fatalf("DisabledFrom after no-op = %q, want %q", got.DisabledFrom, c.prior)
			}

			restored, err := s.Update(m.ID, nil, strptr(c.restore))
			if !c.wantOK {
				if !errors.As(err, new(ErrInvalidStatus)) {
					t.Fatalf("restore to %s = %v, want ErrInvalidStatus", c.restore, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("restore to %s: %v", c.restore, err)
			}
			if restored.Status != c.restore {
				t.Errorf("restored status = %q, want %q", restored.Status, c.restore)
			}
			// Restore clears the marker.
			if restored.DisabledFrom != "" {
				t.Errorf("DisabledFrom after restore = %q, want empty", restored.DisabledFrom)
			}
		})
	}
}

// TestDisabledFromDBRoundTrip: the restore target must survive a restart
// (reopen the same database path, load a fresh store) — a lost DisabledFrom
// would demote every disabled member to the legacy restore-to-registered
// path.
func TestDisabledFromDBRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	d1 := openTestDB(t, path)
	s := newDBStore(t, d1)
	m, err := s.Add("rt@corp.com", "RT")
	if err != nil {
		t.Fatal(err)
	}
	primeStatus(t, s, m.ID, StatusInvited)
	if _, err := s.Update(m.ID, nil, strptr(StatusDisabled)); err != nil {
		t.Fatal(err)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := newDBStore(t, openTestDB(t, path))
	got, err := s2.Get(m.ID)
	if err != nil || got.Status != StatusDisabled || got.DisabledFrom != StatusInvited {
		t.Fatalf("reloaded member = (%+v, %v), want disabled with DisabledFrom=invited", got, err)
	}
	// The reloaded store enforces the same restore rule and clears the marker.
	if _, err := s2.Update(m.ID, nil, strptr(StatusActive)); !errors.As(err, new(ErrInvalidStatus)) {
		t.Errorf("reloaded restore invited->active = %v, want ErrInvalidStatus", err)
	}
	restored, err := s2.Update(m.ID, nil, strptr(StatusInvited))
	if err != nil || restored.Status != StatusInvited || restored.DisabledFrom != "" {
		t.Errorf("reloaded restore = (%+v, %v), want invited with empty DisabledFrom", restored, err)
	}
}

// TestLegacyDisabledRowRestoresToRegistered: rows disabled before
// DisabledFrom existed carry no marker (the importer writes NULL for them);
// they restore to registered — the lifecycle entry state — never straight to
// active.
func TestLegacyDisabledRowRestoresToRegistered(t *testing.T) {
	database := newTestDB(t)
	const id = "33333333-3333-4333-8333-333333333333"
	if _, err := database.Exec(
		`INSERT INTO members (id, email, status, disabled_from, created_at)
		 VALUES (?, 'legacy@corp.com', 'disabled', NULL, '2026-07-08T00:00:00Z')`, id); err != nil {
		t.Fatal(err)
	}
	s := newDBStore(t, database)
	if _, err := s.Update(id, nil, strptr(StatusActive)); !errors.As(err, new(ErrInvalidStatus)) {
		t.Errorf("legacy restore ->active = %v, want ErrInvalidStatus", err)
	}
	got, err := s.Update(id, nil, strptr(StatusRegistered))
	if err != nil || got.Status != StatusRegistered {
		t.Errorf("legacy restore ->registered = (%+v, %v), want registered", got, err)
	}
}

// TestStatusByEmail covers the dataplane-gate lookup: a known email reports
// its status; an unknown email (owner/CLI token users) reports ok=false.
func TestStatusByEmail(t *testing.T) {
	s := NewStore()
	m, _ := s.Add("gate@corp.com", "G")
	if st, ok := s.StatusByEmail("gate@corp.com"); !ok || st != StatusRegistered {
		t.Errorf("StatusByEmail = (%q, %v), want (registered, true)", st, ok)
	}
	if _, err := s.Update(m.ID, nil, strptr(StatusDisabled)); err != nil {
		t.Fatal(err)
	}
	if st, ok := s.StatusByEmail("gate@corp.com"); !ok || st != StatusDisabled {
		t.Errorf("StatusByEmail after disable = (%q, %v), want (disabled, true)", st, ok)
	}
	if _, ok := s.StatusByEmail("owner-cli-token-user"); ok {
		t.Error("StatusByEmail(unknown) ok = true, want false (no registry row)")
	}
}
