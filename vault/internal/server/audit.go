package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/peer"
	"gopkg.in/natefinch/lumberjack.v2"
)

// AuditMode parses AuditConfig.Mode strings into per-sink booleans.
// Valid values: "", "file", "stdout", "file+stdout".
type AuditMode struct {
	File   bool
	Stdout bool
}

func ParseAuditMode(mode string) AuditMode {
	out := AuditMode{}
	if mode == "" {
		return out
	}
	for _, p := range strings.Split(mode, "+") {
		switch strings.TrimSpace(strings.ToLower(p)) {
		case "file":
			out.File = true
		case "stdout":
			out.Stdout = true
		}
	}
	return out
}

// AuditEntry is the JSON structure written per request. Fields and order
// must match vault/audit.py:118-145 to keep golden compat tests aligned.
type AuditEntry struct {
	Timestamp   string  `json:"timestamp"`
	UserID      string  `json:"user_id"`
	Method      string  `json:"method"`
	TopK        *int32  `json:"top_k"`
	ResultCount int     `json:"result_count"`
	Status      string  `json:"status"`
	SourceIP    string  `json:"source_ip"`
	LatencyMs   float64 `json:"latency_ms"`
	Error       *string `json:"error,omitempty"`
}

// AuditLogger writes structured audit entries. Closed loggers are no-ops.
type AuditLogger struct {
	mu      sync.Mutex
	writers []io.Writer
	closers []io.Closer
}

// NewAuditLogger constructs a logger for the given mode + file path.
// Returns a logger with Enabled() == false when the mode is empty.
func NewAuditLogger(cfg AuditConfig) (*AuditLogger, error) {
	mode := ParseAuditMode(cfg.Mode)
	l := &AuditLogger{}

	if mode.File {
		path := cfg.Path
		if path == "" {
			path = "/var/log/rune-vault/audit.log"
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("audit: mkdir log dir: %w", err)
		}
		// Lumberjack handles daily-ish rotation by size + age. Match the
		// Python deployment's 30-day retention; size cap is high enough
		// that audit volume drives rotation by age, not size.
		rot := &lumberjack.Logger{
			Filename:   path,
			MaxSize:    100, // MB
			MaxAge:     30,
			MaxBackups: 30,
			LocalTime:  false,
			Compress:   false,
		}
		l.writers = append(l.writers, rot)
		l.closers = append(l.closers, rot)
	}

	if mode.Stdout {
		l.writers = append(l.writers, os.Stdout)
	}
	return l, nil
}

// Enabled reports whether at least one sink is configured.
func (a *AuditLogger) Enabled() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.writers) > 0
}

// Log emits a single audit entry. Round-trip latency is rounded to 2dp
// to match Python's `round(latency_ms, 2)`.
func (a *AuditLogger) Log(e AuditEntry) {
	if a == nil || !a.Enabled() {
		return
	}
	e.LatencyMs = roundTo(e.LatencyMs, 2)

	buf, err := json.Marshal(&e)
	if err != nil {
		return
	}
	buf = append(buf, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, w := range a.writers {
		_, _ = w.Write(buf)
	}
}

// Close flushes file writers and prevents future Log calls from writing.
func (a *AuditLogger) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var firstErr error
	for _, c := range a.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	a.writers = nil
	a.closers = nil
	return firstErr
}

func roundTo(v float64, decimals int) float64 {
	mult := 1.0
	for i := 0; i < decimals; i++ {
		mult *= 10
	}
	if v >= 0 {
		return float64(int64(v*mult+0.5)) / mult
	}
	return float64(int64(v*mult-0.5)) / mult
}

// ExtractSourceIP mirrors vault/audit.py:55-78 — peer addresses come in
// gRPC's "ipv4:H:P", "ipv6:[::1]:P", or "unix:/path" form.
func ExtractSourceIP(p *peer.Peer) string {
	if p == nil || p.Addr == nil {
		return "unknown"
	}
	addr := p.Addr.String()
	switch a := p.Addr.(type) {
	case *net.TCPAddr:
		if a.IP == nil {
			return addr
		}
		return a.IP.String()
	case *net.UnixAddr:
		return "unix:" + a.Name
	}
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func nowUTCISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z07:00")
}
