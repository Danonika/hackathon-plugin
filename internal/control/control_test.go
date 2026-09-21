package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackathonkit.local/agentkit/internal/learning"
	"hackathonkit.local/agentkit/internal/plans"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.UpsertSession(ctx, Session{ID: "s-1", Root: "/tmp/repo", Task: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	sess, err := store.Session(ctx, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != "active" || sess.Task != "demo" {
		t.Fatalf("unexpected session %+v", sess)
	}

	if err := store.AppendEvent(ctx, Event{SessionID: "s-1", Type: "tool_use", Tool: "system.context"}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(ctx, "s-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Tool != "system.context" {
		t.Fatalf("events = %+v", events)
	}

	if err := store.EndSession(ctx, "s-1", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	sess, _ = store.Session(ctx, "s-1")
	if sess.Status != "ended" || sess.EndedAt == nil {
		t.Fatalf("session not ended: %+v", sess)
	}

	// Ending an unknown session creates a minimal record.
	if err := store.EndSession(ctx, "s-2", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if sess, err := store.Session(ctx, "s-2"); err != nil || sess.Status != "ended" {
		t.Fatalf("unknown session end failed: %+v %v", sess, err)
	}
}

func TestIngestIsIdempotent(t *testing.T) {
	root := t.TempDir()
	store := testStore(t)
	ctx := context.Background()

	sessionStart, err := json.Marshal(map[string]any{
		"ts":         "2026-09-21T10:00:00Z",
		"type":       "session_start",
		"session_id": "s-1",
		"root":       root,
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "logs", "sessions.jsonl"), string(sessionStart)+"\n")
	write(t, filepath.Join(root, "logs", "session-events.jsonl"),
		`{"ts":"2026-09-21T10:00:01Z","type":"tool_use","session_id":"s-1","tool":"system.context"}`+"\n")
	write(t, filepath.Join(root, "logs", "mcp-calls.jsonl"),
		`{"ts":"2026-09-21T10:00:02Z","tool":"data.inspect","duration_ms":12,"status":"ok","trace_id":"kit-1"}`+"\n"+
			`{"ts":"2026-09-21T10:00:03Z","tool":"documents.extract","duration_ms":30,"status":"error","trace_id":"kit-2"}`+"\n")
	write(t, filepath.Join(root, "evals", "decisions", "abc", "technical-1.json"),
		`{"decision":"rollback","reason":"score decreased","ts":"2026-09-21T10:05:00Z"}`+"\n")

	first, err := store.Ingest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Events != 4 {
		t.Fatalf("first ingest events = %d, want 4", first.Events)
	}
	if first.Sessions != 1 {
		t.Fatalf("sessions = %d, want 1", first.Sessions)
	}
	if first.Risk != 1 {
		t.Fatalf("risk updates = %d, want 1", first.Risk)
	}

	second, err := store.Ingest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if second.Events != 0 {
		t.Fatalf("second ingest must not duplicate events, got %d", second.Events)
	}

	sess, err := store.Session(ctx, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.RiskScore <= 0 {
		t.Fatalf("expected a positive risk score, got %+v", sess)
	}
	if len(sess.RiskReasons) == 0 {
		t.Fatal("expected risk reasons")
	}
}

func TestOpenReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.db")
	if _, err := OpenReadOnly(path); err == nil {
		t.Fatal("read-only open must fail when the database does not exist")
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSession(context.Background(), Session{ID: "s-1", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	store.Close()

	reader, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	sessions, err := reader.Sessions(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
}

func TestEvaluateRiskDeterministic(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	sess := Session{ID: "s-1", Status: "ended"}
	events := []Event{
		{Type: "mcp_call", Status: "error"},
		{Type: "mcp_call", Status: "ok"},
		{Type: "eval_decision", Payload: map[string]any{"decision": "rollback"}},
	}
	score, reasons := EvaluateRisk(sess, events, now)
	if score != 33 {
		t.Fatalf("score = %v, want 33", score)
	}
	if len(reasons) != 2 {
		t.Fatalf("reasons = %v", reasons)
	}
	again, _ := EvaluateRisk(sess, events, now)
	if again != score {
		t.Fatalf("risk is not deterministic: %v vs %v", score, again)
	}

	stalled := Session{ID: "s-2", Status: "active", UpdatedAt: now.Add(-3 * time.Hour)}
	stalledScore, stalledReasons := EvaluateRisk(stalled, nil, now)
	if stalledScore != 10 || len(stalledReasons) != 1 {
		t.Fatalf("stalled risk = %v %v", stalledScore, stalledReasons)
	}
}

func TestHandoffRender(t *testing.T) {
	root := t.TempDir()
	store := testStore(t)
	ctx := context.Background()

	if err := store.UpsertSession(ctx, Session{ID: "s-1", Task: "demo task", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(ctx, Event{SessionID: "s-1", Type: "tool_use", Tool: "data.inspect", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := plans.Save(root, plans.Plan{Slug: "demo", Task: "t", Stages: plans.DefaultStages}); err != nil {
		t.Fatal(err)
	}
	learn := learning.NewStore(filepath.Join(root, "data", "index", "learnings.json"), learning.ProjectKey(root))
	if _, err := learn.Record(ctx, learning.RecordInput{Trigger: "when testing handoffs", Insight: "render the plan stage"}); err != nil {
		t.Fatal(err)
	}

	handoff, err := BuildHandoff(ctx, store, learn, root, "s-1", 5)
	if err != nil {
		t.Fatal(err)
	}
	text := RenderHandoff(handoff)
	for _, want := range []string{"Session handoff", "demo task", "plan: demo", "when testing handoffs", "data.inspect"} {
		if !strings.Contains(text, want) {
			t.Fatalf("handoff missing %q:\n%s", want, text)
		}
	}
}
