package control

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackathonkit.local/agentkit/internal/plans"
)

func TestReplayUsesDecisionTimeInSessionHandoff(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	root := t.TempDir()
	write(t, filepath.Join(root, "logs", "sessions.jsonl"), "{\"ts\":\"2026-01-12T10:00:00Z\",\"type\":\"session_start\",\"session_id\":\"a\"}\n{\"ts\":\"2026-01-12T11:00:00Z\",\"type\":\"session_end\",\"session_id\":\"a\"}\n")
	// eval.compare persists a Comparison without a ts; the filename holds its UTC timestamp.
	write(t, filepath.Join(root, "evals", "decisions", "abc", "judge-20260112-103000.123456789.json"), `{"mode":"judge","decision":"rollback"}`)
	if _, err := s.Ingest(ctx, root); err != nil {
		t.Fatal(err)
	}
	h, err := BuildHandoff(ctx, s, nil, root, "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if h.PendingDecisions != 1 || h.Session.RiskScore != riskRollback {
		t.Fatalf("replayed session: decisions=%d risk=%v", h.PendingDecisions, h.Session.RiskScore)
	}
}

func TestReplayLateSessionStartRestoresOriginalWindow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	root := t.TempDir()
	write(t, filepath.Join(root, "logs", "sessions.jsonl"), "{\"ts\":\"2026-01-12T11:00:00Z\",\"type\":\"session_end\",\"session_id\":\"a\"}\n{\"ts\":\"2026-01-12T10:00:00Z\",\"type\":\"session_start\",\"session_id\":\"a\",\"root\":\"/project\"}\n")
	if _, err := s.Ingest(ctx, root); err != nil {
		t.Fatal(err)
	}
	row, err := s.Session(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if row.Root != "/project" || row.Status != "ended" || row.CreatedAt.Hour() != 10 || row.EndedAt == nil || row.EndedAt.Hour() != 11 || !row.UpdatedAt.Equal(*row.EndedAt) {
		t.Fatalf("replayed lifecycle: root=%q status=%s start=%v end=%v updated=%v", row.Root, row.Status, row.CreatedAt, row.EndedAt, row.UpdatedAt)
	}
}

func TestReplayRecentSessionEventPreventsFalseStall(t *testing.T) {
	now := time.Now().UTC()
	s := Session{ID: "a", Status: "active", UpdatedAt: now.Add(-3 * time.Hour)}
	for _, tc := range []struct {
		id   string
		want float64
	}{{"a", 0}, {"b", riskStalled}, {"", riskStalled}} {
		score, _ := EvaluateRisk(s, []Event{{SessionID: tc.id, Type: "tool_use", TS: now.Add(-time.Minute)}}, now)
		if score != tc.want {
			t.Fatalf("event for %q: score=%v want %v", tc.id, score, tc.want)
		}
	}
}

func TestResumeUsesSessionPlanAndWorktree(t *testing.T) {
	root := t.TempDir()
	worktree := t.TempDir()
	s := testStore(t)
	ctx := context.Background()
	for _, p := range []struct{ root, slug string }{{worktree, "assigned"}, {root, "wrong"}} {
		if err := plans.Save(p.root, plans.Plan{Slug: p.slug, Task: p.slug}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpsertSession(ctx, Session{ID: "a", PlanSlug: "assigned", Worktree: worktree}); err != nil {
		t.Fatal(err)
	}
	h, err := BuildHandoff(ctx, s, nil, root, "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if h.Plan == nil || h.Plan.Slug != "assigned" {
		t.Fatalf("expected assigned worktree plan, got %+v", h.Plan)
	}
	if err := s.UpsertSession(ctx, Session{ID: "a", PlanSlug: "missing"}); err != nil {
		t.Fatal(err)
	}
	h, err = BuildHandoff(ctx, s, nil, root, "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if h.Plan != nil || len(h.Notes) == 0 {
		t.Fatalf("expected warning for missing assigned plan, got %+v", h)
	}
}

func TestReplayConcurrentReadersDoNotDuplicateEvents(t *testing.T) {
	root := t.TempDir()
	path := DefaultPath(root)
	ctx := context.Background()
	write(t, filepath.Join(root, "logs", "session-events.jsonl"), strings.Repeat("{\"session_id\":\"a\",\"tool\":\"data.inspect\"}\n", 120))
	start := make(chan struct{})
	done := make(chan error, 3)
	for range 3 {
		store, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
		go func() { <-start; _, err := store.Ingest(ctx, root); done <- err }()
	}
	close(start)
	for range 3 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	s, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, events, err := s.Counts(ctx)
	if err != nil || events != 120 {
		t.Fatalf("replayed %d events from 120 input rows, err=%v", events, err)
	}
}

func TestExistingDecisionTimestampRepairedWithoutDuplicate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	root := t.TempDir()
	rel := "evals/decisions/abc/judge-20260112-103000.123456789.json"
	raw := `{"decision":"rollback"}`
	write(t, filepath.Join(root, filepath.FromSlash(rel)), raw)
	if err := s.AppendEvent(ctx, Event{Type: "eval_decision", Payload: map[string]any{"decision": "rollback"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCursor(ctx, "decision:"+rel, int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCursor(ctx, "decision:"+rel+":hash", int64(contentHash([]byte(raw)))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(ctx, root); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx, "", 10)
	if err != nil || len(events) != 1 || events[0].TS.Format(time.RFC3339Nano) != "2026-01-12T10:30:00.123456789Z" {
		t.Fatalf("legacy decision=%+v err=%v", events, err)
	}
}

func TestInvalidStoredOutputAndExplicitTimestampAreReported(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	root := t.TempDir()
	write(t, filepath.Join(root, "logs", "mcp-calls.jsonl"), "{\"tool\":\"one\",\"ts\":123}\n{\"tool\":\"two\",\"ts\":\"\"}\n{\"tool\":\"three\"}\n")
	result, err := s.Ingest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 2 || result.Events != 1 {
		t.Errorf("malformed timestamp results=%+v", result)
	}
	if err := s.AppendOutput(ctx, Output{SessionID: "a", Kind: "report", Ref: "r.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE outputs SET ts='invalid' WHERE session_id='a'"); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.Outputs(ctx, "a", 1); err == nil {
		t.Errorf("corrupt output date returned: %+v", rows)
	}
}

func TestAmbiguousLegacyDecisionTimestampsArePreserved(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	root := t.TempDir()
	rel := "evals/decisions/abc/technical-20260112-103000.123456789.json"
	raw := `{"decision":"accept"}`
	write(t, filepath.Join(root, filepath.FromSlash(rel)), raw)
	timestamp := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for range 2 {
		if err := s.AppendEvent(ctx, Event{Type: "eval_decision", TS: timestamp, Payload: map[string]any{"decision": "accept"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetCursor(ctx, "decision:"+rel, int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCursor(ctx, "decision:"+rel+":hash", int64(contentHash([]byte(raw)))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(ctx, root); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx, "", 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%v err=%v", events, err)
	}
	for _, e := range events {
		if !e.TS.Equal(timestamp) {
			t.Fatalf("ambiguous event modified: %+v", e)
		}
	}
}
