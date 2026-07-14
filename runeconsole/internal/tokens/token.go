package tokens

import "time"

type Token struct {
	User     string `yaml:"user"`
	Token    string `yaml:"token"`
	Role     string `yaml:"role"`
	IssuedAt string `yaml:"issued_at"`         // ISO date
	Expires  string `yaml:"expires,omitempty"` // ISO date, empty = never
}

const dateFormat = "2006-01-02"

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
