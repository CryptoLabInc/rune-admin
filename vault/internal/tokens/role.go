package tokens

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

type Role struct {
	Name      string   `yaml:"-"`
	Scope     []string `yaml:"scope"`
	TopK      int      `yaml:"top_k"`
	RateLimit string   `yaml:"rate_limit"`
}

var rateLimitRE = regexp.MustCompile(`^(\d+)/(\d+)s$`)

func (r *Role) RateLimitParsed() (max int, window time.Duration, err error) {
	m := rateLimitRE.FindStringSubmatch(r.RateLimit)
	if m == nil {
		return 0, 0, fmt.Errorf("invalid rate_limit format %q (expected '<max>/<window>s')", r.RateLimit)
	}
	maxReq, _ := strconv.Atoi(m[1])
	winSec, _ := strconv.Atoi(m[2])
	return maxReq, time.Duration(winSec) * time.Second, nil
}

func (r *Role) CheckScope(method string) error {
	for _, s := range r.Scope {
		if s == method {
			return nil
		}
	}
	return ErrScope{Method: method, RoleName: r.Name}
}

func validateRateLimit(s string) error {
	if !rateLimitRE.MatchString(s) {
		return fmt.Errorf("invalid rate_limit format %q (expected '<max>/<window>s')", s)
	}
	return nil
}

const DemoToken = "evt_0000000000000000000000000000demo"

func DefaultRoles() map[string]*Role {
	return map[string]*Role{
		"admin": {
			Name:      "admin",
			Scope:     []string{"get_public_key", "decrypt_scores", "decrypt_metadata", "manage_tokens"},
			TopK:      50,
			RateLimit: "150/60s",
		},
		"member": {
			Name:      "member",
			Scope:     []string{"get_public_key", "decrypt_scores", "decrypt_metadata"},
			TopK:      10,
			RateLimit: "30/60s",
		},
	}
}

func isDefaultRoleName(name string) bool {
	return name == "admin" || name == "member"
}
