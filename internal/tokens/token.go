package tokens

import "time"

type Token struct {
	User        string `yaml:"user"`
	Token       string `yaml:"token"`
	IssuedAt    string `yaml:"issued_at"`              // ISO date
	Expires     string `yaml:"expires,omitempty"`      // ISO date, empty = never
	LastUsed    string `yaml:"last_used,omitempty"`    // RFC3339 UTC; stamped on Validate (throttled), empty = never used
	ActivatedAt string `yaml:"activated_at,omitempty"` // RFC3339 UTC; set on ReportActivation (agent reached active), empty = never activated
}

const dateFormat = "2006-01-02"

// DemoToken is the fixed token minted by LoadDefaultsWithDemoToken for
// dev/CI bootstraps that don't ship persisted state.
const DemoToken = "evt_0000000000000000000000000000demo"

func (t *Token) IsExpired() bool {
	return t.IsExpiredAt(time.Now().UTC())
}

func (t *Token) IsExpiredAt(now time.Time) bool {
	if t.Expires == "" {
		return false
	}
	exp, err := time.Parse(dateFormat, t.Expires)
	if err != nil {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return exp.Before(today)
}
