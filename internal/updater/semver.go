package updater

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Version struct {
	Major uint64
	Minor uint64
	Patch uint64
	Pre   []string
}

func ParseVersion(input string) (Version, error) {
	s := strings.TrimSpace(input)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return Version{}, errors.New("version is empty")
	}
	if plus := strings.IndexByte(s, '+'); plus >= 0 {
		if plus == len(s)-1 || !validIdentifiers(s[plus+1:], false) {
			return Version{}, errors.New("invalid build metadata")
		}
		s = s[:plus]
	}
	var pre []string
	if dash := strings.IndexByte(s, '-'); dash >= 0 {
		if dash == len(s)-1 || !validIdentifiers(s[dash+1:], true) {
			return Version{}, errors.New("invalid prerelease")
		}
		pre = strings.Split(s[dash+1:], ".")
		s = s[:dash]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, errors.New("expected MAJOR.MINOR.PATCH")
	}
	values := make([]uint64, 3)
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return Version{}, errors.New("invalid numeric component")
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return Version{}, errors.New("invalid numeric component")
			}
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return Version{}, errors.New("numeric component is too large")
		}
		values[i] = value
	}
	return Version{Major: values[0], Minor: values[1], Patch: values[2], Pre: pre}, nil
}

func validIdentifiers(s string, prerelease bool) bool {
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		numeric := true
		for _, r := range id {
			if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-') {
				return false
			}
			if r < '0' || r > '9' {
				numeric = false
			}
		}
		if prerelease && numeric && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}

func (v Version) String() string {
	base := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		base += "-" + strings.Join(v.Pre, ".")
	}
	return base
}

func (v Version) Compare(other Version) int {
	for _, pair := range [][2]uint64{{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(v.Pre) == 0 && len(other.Pre) == 0 {
		return 0
	}
	if len(v.Pre) == 0 {
		return 1
	}
	if len(other.Pre) == 0 {
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(other.Pre); i++ {
		a, b := v.Pre[i], other.Pre[i]
		if a == b {
			continue
		}
		an, aerr := strconv.ParseUint(a, 10, 64)
		bn, berr := strconv.ParseUint(b, 10, 64)
		switch {
		case aerr == nil && berr == nil:
			if an < bn {
				return -1
			}
			return 1
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		case a < b:
			return -1
		default:
			return 1
		}
	}
	if len(v.Pre) < len(other.Pre) {
		return -1
	}
	if len(v.Pre) > len(other.Pre) {
		return 1
	}
	return 0
}
