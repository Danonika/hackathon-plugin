package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceHelpers(t *testing.T) {
	id := NewTraceID()
	if !strings.HasPrefix(id, "kit-") || len(id) != len("kit-")+16 {
		t.Fatalf("NewTraceID = %q", id)
	}
	if id == NewTraceID() {
		t.Fatal("trace ids must be unique")
	}
	ctx := WithTrace(context.Background(), id)
	if got := TraceFrom(ctx); got != id {
		t.Fatalf("TraceFrom = %q", got)
	}
	if got := TraceFrom(context.Background()); got != "" {
		t.Fatalf("empty context trace = %q", got)
	}
}

func TestMetrics(t *testing.T) {
	metrics := NewMetrics()
	metrics.Inc("a")
	metrics.Inc("a")
	metrics.Add("b", 5)
	metrics.Add("b", -2)
	snapshot := metrics.Snapshot()
	if snapshot["a"] != 2 || snapshot["b"] != 3 {
		t.Fatalf("snapshot = %v", snapshot)
	}
}

func TestLog(t *testing.T) {
	Log(nil, "info", "noop", nil)

	var buf bytes.Buffer
	logger := NewLogger(&buf)
	Log(logger, "info", "tool_call", map[string]any{"tool": "system.context"})
	var entry Entry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Level != "info" || entry.Msg != "tool_call" || entry.Fields["tool"] != "system.context" {
		t.Fatalf("entry = %+v", entry)
	}

	buf.Reset()
	Log(logger, "error", "bad", map[string]any{"bad": make(chan int)})
	// The line must survive with a fallback payload instead of vanishing.
	var fallback Entry
	if err := json.Unmarshal(buf.Bytes(), &fallback); err != nil {
		t.Fatalf("fallback line is not JSON: %q", buf.String())
	}
	if fallback.Msg != "bad" || fallback.Fields["log_error"] == nil {
		t.Fatalf("fallback entry = %+v", fallback)
	}
}

func TestAppendJSONLErrors(t *testing.T) {
	if err := AppendJSONL("", map[string]any{"i": 1}); err != nil {
		t.Fatal("empty path must be a no-op")
	}

	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendJSONL(filepath.Join(blocker, "events.jsonl"), map[string]any{"i": 1}); err == nil {
		t.Fatal("expected mkdir error")
	}

	if err := AppendJSONL(dir, map[string]any{"i": 1}); err == nil {
		t.Fatal("expected open error when the path is a directory")
	}

	if err := AppendJSONL(filepath.Join(dir, "events.jsonl"), make(chan int)); err == nil {
		t.Fatal("expected encode error")
	}
}

func TestTailJSONLLimitClamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	for i := 0; i < 3; i++ {
		if err := AppendJSONL(path, map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{-1, 0, 2000} {
		entries, err := TailJSONL(path, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 3 {
			t.Fatalf("limit %d: entries = %d", limit, len(entries))
		}
	}
}

func TestTailJSONLOpenError(t *testing.T) {
	if _, err := TailJSONL(t.TempDir(), 5); err == nil {
		t.Fatal("expected error when the path is a directory")
	}
}

func TestTailJSONLLargeFileMultiChunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("x", 200)
	for i := 0; i < 1000; i++ {
		if _, err := f.WriteString(`{"i":` + strings.Repeat("1", 1) + `,"pad":"` + payload + `"}` + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := TailJSONL(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(entries))
	}
	for _, entry := range entries {
		if !json.Valid(entry) {
			t.Fatalf("invalid entry: %s", entry)
		}
	}
}

func TestSplitLinesAndTrimSpace(t *testing.T) {
	lines := splitLines([]byte("a\nb"))
	if len(lines) != 2 || string(lines[0]) != "a" || string(lines[1]) != "b" {
		t.Fatalf("splitLines = %q", lines)
	}
	if got := trimSpace([]byte(" \t\rx\r\t ")); string(got) != "x" {
		t.Fatalf("trimSpace = %q", got)
	}
	if got := trimSpace(nil); len(got) != 0 {
		t.Fatalf("trimSpace nil = %q", got)
	}
}
