package commands

import (
	"fmt"
	"regexp"
	"strconv"
)

var durationRE = regexp.MustCompile(`^(\d+)([dwm])$`)

// parseDuration converts strings like "90d", "12w", "6m" into days.
// Mirrors vault_admin_cli.py:_parse_duration (m = 30 days approximation).
func parseDuration(value string) (int, error) {
	m := durationRE.FindStringSubmatch(value)
	if m == nil {
		return 0, fmt.Errorf("Invalid duration '%s'. Use <number><d|w|m> (e.g. 90d, 12w, 6m)", value)
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "d":
		return n, nil
	case "w":
		return n * 7, nil
	default:
		return n * 30, nil
	}
}
