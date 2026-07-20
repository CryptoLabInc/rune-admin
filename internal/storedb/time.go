package storedb

import (
	"fmt"
	"time"
)

// TimeFormat is the canonical storage rendering of an instant: RFC3339 UTC
// with FIXED three-digit milliseconds ("2026-07-17T05:00:00.123Z").
//
// The width is fixed on purpose. Several stores compare timestamp columns
// TEXTUALLY (the invites aged-pending sweep runs `expires_at <= ?` in SQL,
// and list orderings sort the TEXT column), which is only correct while
// lexicographic order equals chronological order. That equivalence holds
// within ONE fixed-width rendering, but breaks the moment second-precision
// and millisecond values mix: "2026-07-17T05:00:00Z" sorts AFTER
// "2026-07-17T05:00:00.999Z" ('Z' > '.'), so a mixed column interleaves
// wrongly — an aged invite could be swept too early or kept alive past its
// TTL. Hence every stored instant is normalized to this one canonical width;
// sub-millisecond digits are truncated by Format.
const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// FormatTime renders t in the canonical storage form: UTC, fixed
// three-digit milliseconds (see TimeFormat for why the width is fixed).
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeFormat)
}

// CanonicalizeTime re-renders an RFC3339 timestamp string in the canonical
// storage form. It accepts any valid RFC3339 input — offsets and arbitrary
// fractional-second widths included (Go's time.Parse with the RFC3339 layout
// consumes fractional seconds) — and returns the one canonical rendering, so
// values from hand-written or legacy sources cannot interleave wrongly with
// store-written ones in textual comparisons (see TimeFormat). The error is
// the parse error for a non-RFC3339 input, unwrapped decision left to the
// caller.
func CanonicalizeTime(s string) (string, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", fmt.Errorf("storedb: canonicalize time %q: %w", s, err)
	}
	return FormatTime(t), nil
}
