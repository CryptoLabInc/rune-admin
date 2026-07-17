package server

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/peer"
)

func TestParseAuditMode(t *testing.T) {
	cases := map[string]AuditMode{
		"":             {},
		"file":         {File: true},
		"stdout":       {Stdout: true},
		"file+stdout":  {File: true, Stdout: true},
		"stdout+file":  {File: true, Stdout: true},
		"FILE":         {File: true},
		"  file  ":     {File: true},
		"unknown":      {},
		"file+unknown": {File: true},
	}
	for in, want := range cases {
		got := ParseAuditMode(in)
		if got != want {
			t.Errorf("ParseAuditMode(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestAuditLoggerDisabledWhenModeEmpty(t *testing.T) {
	l, err := NewAuditLogger(AuditConfig{Mode: ""})
	if err != nil {
		t.Fatal(err)
	}
	if l.Enabled() {
		t.Error("logger enabled with empty mode")
	}
	// Log on disabled logger must be no-op (and not panic).
	l.Log(AuditEntry{UserID: "x"})
}

func TestAuditLoggerFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, err := NewAuditLogger(AuditConfig{Mode: "file", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	topK := int32(10)
	errMsg := "boom"
	l.Log(AuditEntry{
		Timestamp:   "2026-04-23T00:00:00.000000Z",
		UserID:      "alice",
		Method:      "decrypt_scores",
		TopK:        &topK,
		ResultCount: 7,
		Status:      "success",
		SourceIP:    "127.0.0.1",
		LatencyMs:   45.6789,
		Error:       &errMsg,
	})
	l.Close()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("audit log empty")
	}
	var got map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["user_id"] != "alice" {
		t.Errorf("user_id = %v, want alice", got["user_id"])
	}
	if got["method"] != "decrypt_scores" {
		t.Errorf("method = %v", got["method"])
	}
	if got["top_k"].(float64) != 10 {
		t.Errorf("top_k = %v", got["top_k"])
	}
	if got["latency_ms"].(float64) != 45.68 {
		t.Errorf("latency_ms = %v, want 45.68 (rounded)", got["latency_ms"])
	}
	if got["error"] != "boom" {
		t.Errorf("error = %v", got["error"])
	}
}

func TestAuditLoggerOmitsErrorWhenNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, err := NewAuditLogger(AuditConfig{Mode: "file", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Log(AuditEntry{UserID: "x", Method: "y", Status: "success"})
	l.Close()

	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), `"error"`) {
		t.Errorf("audit entry contains error field for non-error case: %s", body)
	}
}

func TestExtractSourceIPTCP(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 12345}
	got := ExtractSourceIP(&peer.Peer{Addr: addr})
	if got != "10.0.0.5" {
		t.Errorf("got %q, want 10.0.0.5", got)
	}
}

func TestExtractSourceIPUnix(t *testing.T) {
	addr := &net.UnixAddr{Name: "/tmp/x.sock", Net: "unix"}
	got := ExtractSourceIP(&peer.Peer{Addr: addr})
	if got != "unix:/tmp/x.sock" {
		t.Errorf("got %q, want unix:/tmp/x.sock", got)
	}
}

func TestExtractSourceIPNil(t *testing.T) {
	if got := ExtractSourceIP(nil); got != "unknown" {
		t.Errorf("nil peer: got %q, want unknown", got)
	}
}

func TestRoundTo(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{45.6789, 45.68},
		{45.6749, 45.67},
		{0, 0},
		{-1.236, -1.24},
	}
	for _, c := range cases {
		got := roundTo(c.in, 2)
		if got != c.want {
			t.Errorf("roundTo(%v,2) = %v, want %v", c.in, got, c.want)
		}
	}
}
