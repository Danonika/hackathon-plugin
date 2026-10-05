package control

import (
	"context"
	"path/filepath"
	"testing"
)

// A record that parses as JSON but carries an unparseable ts must be skipped
// (and the cursor advanced) instead of wedging the stream forever.
func TestIngestSkipsMalformedTimestampAndAdvances(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	root := t.TempDir()
	start := `{"ts":"2026-09-21T10:00:00Z","type":"session_start","session_id":"s-1","root":"/x"}`
	bad := `{"ts":"not-a-time","type":"session_start","session_id":"s-bad","root":"/x"}`
	end := `{"ts":"2026-09-21T10:10:00Z","type":"session_end","session_id":"s-1"}`
	write(t, filepath.Join(root, "logs", "sessions.jsonl"), start+"\n"+bad+"\n"+end+"\n")

	res, err := store.Ingest(ctx, root)
	if err != nil {
		t.Fatalf("a malformed ts must not abort ingest: %v", err)
	}
	if res.Skipped != 1 {
		t.Fatalf("skipped = %d, want 1", res.Skipped)
	}
	sess, err := store.Session(ctx, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != "ended" {
		t.Fatalf("valid records after the bad one were not applied: status = %s", sess.Status)
	}

	// The cursor must have advanced past the bad line: re-ingesting is a no-op.
	again, err := store.Ingest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if again.Events != 0 || again.Skipped != 0 {
		t.Fatalf("re-ingest replayed records: %+v", again)
	}
}
