package observability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendAndTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	for i := 0; i < 5; i++ {
		if err := AppendJSONL(path, map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := TailJSONL(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if !strings.Contains(string(entries[0]), `"i":2`) {
		t.Fatalf("expected tail to start at i=2, got %s", entries[0])
	}

	missing := filepath.Join(t.TempDir(), "nope.jsonl")
	entries, err = TailJSONL(missing, 5)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing file should return empty list, got %v %v", entries, err)
	}
}

func TestTailJSONLSkipsPartialTrailingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	for i := 0; i < 3; i++ {
		if err := AppendJSONL(path, map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"i":3,"partial`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	entries, err := TailJSONL(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected the partial line to be skipped, got %d entries", len(entries))
	}
	for _, entry := range entries {
		if !strings.HasSuffix(string(entry), "}") {
			t.Fatalf("entry is not complete JSON: %s", entry)
		}
	}
}
