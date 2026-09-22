package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackathonkit.local/agentkit/internal/plans"
)

func TestDefaultPath(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "data", "control", "control.db")
	if got := DefaultPath(root); got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
}

func TestOpenMkdirError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(blocker, "control.db")); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestCloseNil(t *testing.T) {
	var store *Store
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (&Store{}).Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFormatAndParseTime(t *testing.T) {
	if got := formatTime(time.Time{}); got != "" {
		t.Fatalf("zero formatTime = %q", got)
	}
	if got := parseTime(""); !got.IsZero() {
		t.Fatalf("empty parseTime = %v", got)
	}
	if got := parseTime("not-a-time"); !got.IsZero() {
		t.Fatalf("invalid parseTime = %v", got)
	}
	legacy := parseTime("2026-09-21T10:00:00.000Z")
	if legacy.IsZero() {
		t.Fatal("legacy layout must parse")
	}
	if got := parseTime("2026-09-21T10:00:00Z"); !got.Equal(legacy) {
		t.Fatalf("rfc3339 parse = %v", got)
	}
}

func TestEventsLimitsAndClosedStore(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := store.AppendEvent(ctx, Event{SessionID: "s-1", Type: "tool_use"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{0, 5000} {
		events, err := store.Events(ctx, "s-1", limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 3 {
			t.Fatalf("limit %d: events = %d", limit, len(events))
		}
	}

	closed := testStore(t)
	if err := closed.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.Events(ctx, "s-1", 5); err == nil {
		t.Fatal("expected error on a closed store")
	}
}

func TestEventsBetweenAndLatestGlobalTS(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	from := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	if ts, err := store.latestGlobalTS(ctx); err != nil || !ts.IsZero() {
		t.Fatalf("empty latestGlobalTS = %v %v", ts, err)
	}
	if err := store.AppendEvent(ctx, Event{TS: from.Add(time.Minute), Type: "mcp_call", Tool: "data.inspect"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(ctx, Event{SessionID: "s-1", TS: from.Add(2 * time.Minute), Type: "tool_use"}); err != nil {
		t.Fatal(err)
	}
	ts, err := store.latestGlobalTS(ctx)
	if err != nil || !ts.Equal(from.Add(time.Minute)) {
		t.Fatalf("latestGlobalTS = %v %v", ts, err)
	}
	globals, err := store.EventsBetween(ctx, from, to, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(globals) != 1 || globals[0].Tool != "data.inspect" {
		t.Fatalf("globals = %+v", globals)
	}
	if _, err := store.EventsBetween(ctx, from, to, 5000); err != nil {
		t.Fatal(err)
	}
}

func TestOpenReadOnlyInvalidDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-db.sqlite")
	if err := os.WriteFile(path, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReadOnly(path); err == nil {
		t.Fatal("expected probe error for a non-sqlite file")
	}
}

func TestAppendAndOutputs(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := store.AppendOutput(ctx, Output{SessionID: "s-1", Kind: "artifact", Ref: "a", Summary: "s"}); err != nil {
			t.Fatal(err)
		}
	}
	outputs, err := store.Outputs(ctx, "s-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 3 || outputs[0].TS.IsZero() || outputs[0].ID == 0 {
		t.Fatalf("outputs = %+v", outputs)
	}
	limited, err := store.Outputs(ctx, "s-1", 600)
	if err != nil || len(limited) != 3 {
		t.Fatalf("limited outputs = %+v %v", limited, err)
	}
	limited, err = store.Outputs(ctx, "s-1", 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limited outputs = %+v %v", limited, err)
	}
	none, err := store.Outputs(ctx, "missing", 5)
	if err != nil || len(none) != 0 {
		t.Fatalf("missing outputs = %+v %v", none, err)
	}
}

func TestAppendEventPayloadError(t *testing.T) {
	store := testStore(t)
	if err := store.AppendEvent(context.Background(), Event{Payload: map[string]any{"bad": make(chan int)}}); err == nil {
		t.Fatal("expected payload marshal error")
	}
}

func TestIngestFlyFeedbackAndSessionEnd(t *testing.T) {
	root := t.TempDir()
	store := testStore(t)
	ctx := context.Background()

	write(t, filepath.Join(root, "logs", "sessions.jsonl"),
		`{"ts":"2026-09-21T10:00:00Z","type":"session_start","session_id":"s-1"}`+"\n"+
			`{"ts":"2026-09-21T11:00:00Z","type":"session_end","session_id":"s-1"}`+"\n"+
			`{"type":"session_start"}`+"\n")
	write(t, filepath.Join(root, "logs", "fly-feedback.jsonl"),
		`{"ts":"2026-09-21T10:30:00Z","trace_id":"kit-1","accepted":true}`+"\n")

	res, err := store.Ingest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 4 {
		t.Fatalf("events = %d, want 4", res.Events)
	}
	sess, err := store.Session(ctx, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != "ended" || sess.EndedAt == nil {
		t.Fatalf("session = %+v", sess)
	}

	globals, err := store.EventsBetween(ctx, time.Time{}, time.Now().Add(time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range globals {
		if ev.Type == "fly_feedback" && ev.Payload["trace_id"] == "kit-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fly feedback event missing: %+v", globals)
	}
}

func TestHandleSessionRecordMissingID(t *testing.T) {
	store := testStore(t)
	if err := store.handleSessionRecord(context.Background(), map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

func TestIngestJSONLEdgeCases(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.jsonl")

	count := 0
	handler := func(context.Context, map[string]any) error {
		count++
		return nil
	}

	if n, _, err := store.ingestJSONL(ctx, "missing", filepath.Join(dir, "nope.jsonl"), handler); err != nil || n != 0 {
		t.Fatalf("missing file = %d %v", n, err)
	}
	if _, _, err := store.ingestJSONL(ctx, "dir", dir, handler); err == nil {
		t.Fatal("expected read error for a directory")
	}

	write(t, path, "\n  \n"+`{"a":1}`+"\n"+`{"b":2`)
	n, _, err := store.ingestJSONL(ctx, "stream", path, handler)
	if err != nil || n != 1 || count != 1 {
		t.Fatalf("first pass = %d err=%v count=%d", n, err, count)
	}
	offset, err := store.Cursor(ctx, "stream")
	if err != nil || offset <= 0 {
		t.Fatalf("cursor = %d %v", offset, err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	n, _, err = store.ingestJSONL(ctx, "stream", path, handler)
	if err != nil || n != 1 || count != 2 {
		t.Fatalf("second pass = %d err=%v count=%d", n, err, count)
	}

	bad := filepath.Join(dir, "bad.jsonl")
	write(t, bad, "not json\n")
	if n, _, err := store.ingestJSONL(ctx, "bad", bad, handler); err != nil || n != 0 {
		t.Fatalf("malformed line = %d %v", n, err)
	}

	write(t, path, `{"c":3}`+"\n")
	if err := store.SetCursor(ctx, "stream", 5000); err != nil {
		t.Fatal(err)
	}
	n, rotated, err := store.ingestJSONL(ctx, "stream", path, handler)
	if err != nil || n != 1 || !rotated {
		t.Fatalf("oversized cursor = %d rotated=%v err=%v", n, rotated, err)
	}
}

func TestIngestDecisionsCursors(t *testing.T) {
	root := t.TempDir()
	store := testStore(t)
	ctx := context.Background()

	if n, err := store.ingestDecisions(ctx, root); err != nil || n != 0 {
		t.Fatalf("missing dir = %d %v", n, err)
	}

	decision := filepath.Join(root, "evals", "decisions", "run-1", "technical.json")
	write(t, decision, `{"decision":"accept","ts":"2026-09-21T10:00:00Z"}`)
	write(t, filepath.Join(root, "evals", "decisions", "run-1", "partial.json"), `{"decision":`)
	write(t, filepath.Join(root, "evals", "decisions", "notes.txt"), "ignored")

	if n, err := store.ingestDecisions(ctx, root); err != nil || n != 1 {
		t.Fatalf("first pass = %d %v", n, err)
	}
	if n, err := store.ingestDecisions(ctx, root); err != nil || n != 0 {
		t.Fatalf("second pass = %d %v", n, err)
	}

	f, err := os.OpenFile(decision, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if n, err := store.ingestDecisions(ctx, root); err != nil || n != 1 {
		t.Fatalf("appended pass = %d %v", n, err)
	}

	if err := store.SetCursor(ctx, "decision:evals/decisions/run-1/technical.json", 9999); err != nil {
		t.Fatal(err)
	}
	if n, err := store.ingestDecisions(ctx, root); err != nil || n != 1 {
		t.Fatalf("oversized cursor pass = %d %v", n, err)
	}
}

func TestRecomputeRiskStableAndEnded(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.UpsertSession(ctx, Session{ID: "stable", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.RecomputeRisk(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 0 {
		t.Fatalf("unchanged risk must not count as an update, got %d", updated)
	}
	if err := store.AppendEvent(ctx, Event{SessionID: "stable", Type: "mcp_call", Status: "error"}); err != nil {
		t.Fatal(err)
	}
	if updated, err = store.RecomputeRisk(ctx); err != nil || updated != 1 {
		t.Fatalf("updated = %d err=%v", updated, err)
	}
	if updated, err = store.RecomputeRisk(ctx); err != nil || updated != 0 {
		t.Fatalf("second run updated = %d err=%v", updated, err)
	}

	ended := time.Now().UTC().Add(time.Hour)
	if err := store.UpsertSession(ctx, Session{ID: "ended", Status: "ended", EndedAt: &ended}); err != nil {
		t.Fatal(err)
	}
	globalTS := time.Now().UTC().Add(2 * time.Hour)
	if err := store.AppendEvent(ctx, Event{TS: globalTS, Type: "mcp_call", Status: "error"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecomputeRisk(ctx); err != nil {
		t.Fatal(err)
	}
	sess, err := store.Session(ctx, "ended")
	if err != nil {
		t.Fatal(err)
	}
	if sess.RiskScore <= 0 {
		t.Fatalf("global event after endedAt must be attributed: %+v", sess)
	}
}

func TestCountsClosedStore(t *testing.T) {
	store := testStore(t)
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Counts(context.Background()); err == nil {
		t.Fatal("expected error on a closed store")
	}
}

func TestSessionsLimits(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if err := store.UpsertSession(ctx, Session{ID: "s-1", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 5000, 1} {
		sessions, err := store.Sessions(ctx, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions) != 1 {
			t.Fatalf("limit %d: sessions = %d", limit, len(sessions))
		}
	}
}

func TestBuildHandoffErrorsAndDefaults(t *testing.T) {
	root := t.TempDir()
	store := testStore(t)
	ctx := context.Background()

	if _, err := BuildHandoff(ctx, store, nil, root, "missing", 0); err == nil {
		t.Fatal("unknown session must fail")
	}

	if err := store.UpsertSession(ctx, Session{ID: "s-1", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(ctx, Event{SessionID: "s-1", Type: "eval_decision", Payload: map[string]any{"decision": "accept"}}); err != nil {
		t.Fatal(err)
	}
	handoff, err := BuildHandoff(ctx, store, nil, root, "s-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if handoff.Plan != nil || len(handoff.Learnings) != 0 {
		t.Fatalf("handoff = %+v", handoff)
	}
	if handoff.PendingDecisions != 1 {
		t.Fatalf("pending decisions = %d", handoff.PendingDecisions)
	}
}

func TestRenderHandoffBranches(t *testing.T) {
	plan := plans.Plan{Slug: "demo", Revision: 1, Stages: []plans.Stage{
		{ID: "S0", Status: "done"},
		{ID: "S1", Status: "doing", Goal: "finish the slice"},
	}}
	ended := time.Now().UTC()
	handoff := Handoff{
		Session: Session{
			ID:          "s-1",
			Status:      "ended",
			Task:        "ship it",
			Worktree:    "wt-1",
			RiskScore:   42,
			RiskReasons: []string{"failed calls"},
			EndedAt:     &ended,
		},
		Plan:             &plan,
		RecentEvents:     []Event{{TS: time.Now().UTC(), Type: "mcp_call", Tool: "data.inspect", Status: "ok"}},
		PendingDecisions: 2,
	}
	text := RenderHandoff(handoff)
	for _, want := range []string{"worktree: wt-1", "risk reasons: failed calls", "next:", "mcp_call data.inspect [ok]", "pending eval decisions: 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("handoff missing %q:\n%s", want, text)
		}
	}

	done := plans.Plan{Slug: "done", Revision: 1, Stages: []plans.Stage{{ID: "S0", Status: "done"}}}
	complete := Handoff{Session: Session{ID: "s-2", Status: "ended"}, Plan: &done}
	if text := RenderHandoff(complete); strings.Contains(text, "next:") {
		t.Fatalf("complete plan must not render a next stage:\n%s", text)
	}
}

func TestPayloadHasPanic(t *testing.T) {
	if payloadHasPanic(nil) {
		t.Fatal("nil payload must not panic")
	}
	if !payloadHasPanic(map[string]any{"error": "tool panicked"}) {
		t.Fatal("panic payload must be detected")
	}
	if payloadHasPanic(map[string]any{"bad": make(chan int)}) {
		t.Fatal("unmarshalable payload must not panic")
	}
}

func TestEvaluateRiskCaps(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	var events []Event
	for i := 0; i < 10; i++ {
		events = append(events,
			Event{Type: "mcp_call", Status: "error", Payload: map[string]any{"error": "panic"}},
			Event{Type: "eval_decision", Payload: map[string]any{"decision": "rollback"}},
		)
	}
	score, reasons := EvaluateRisk(Session{Status: "ended"}, events, now)
	if score != 100 {
		t.Fatalf("score = %v, want capped 100", score)
	}
	if len(reasons) != 3 {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestStringFloatField(t *testing.T) {
	rec := map[string]any{"s": "value", "n": 1.5, "i": 2}
	if got := stringField(rec, "s"); got != "value" {
		t.Fatalf("stringField = %q", got)
	}
	if got := stringField(rec, "n"); got != "" {
		t.Fatalf("stringField non-string = %q", got)
	}
	if got := floatField(rec, "n"); got != 1.5 {
		t.Fatalf("floatField = %v", got)
	}
	if got := floatField(rec, "i"); got != 0 {
		t.Fatalf("floatField non-float = %v", got)
	}
}

func TestIngestCursorAdvancesOnHandlerError(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.jsonl")
	write(t, path, `{"n":1}`+"\n"+`{"n":2}`+"\n"+`{"n":3}`+"\n")

	seen := []int{}
	handler := func(_ context.Context, rec map[string]any) error {
		n := int(rec["n"].(float64))
		if n == 3 {
			return errors.New("boom")
		}
		seen = append(seen, n)
		return nil
	}
	if _, _, err := store.ingestJSONL(ctx, "stream", path, handler); err == nil {
		t.Fatal("expected the handler error to propagate")
	}
	offset, err := store.Cursor(ctx, "stream")
	if err != nil {
		t.Fatal(err)
	}
	want := int64(len(`{"n":1}` + "\n" + `{"n":2}` + "\n"))
	if offset != want {
		t.Fatalf("cursor = %d, want %d (only applied records)", offset, want)
	}

	seen = nil
	retry := func(_ context.Context, rec map[string]any) error {
		seen = append(seen, int(rec["n"].(float64)))
		return nil
	}
	if _, _, err := store.ingestJSONL(ctx, "stream", path, retry); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != 3 {
		t.Fatalf("retry saw %v, want [3]", seen)
	}
}

func TestSessionEndIsNotResurrected(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	root := t.TempDir()
	start := `{"ts":"2026-09-21T10:00:00Z","type":"session_start","session_id":"s-1","root":"/x"}`
	end := `{"ts":"2026-09-21T10:10:00Z","type":"session_end","session_id":"s-1"}`
	logPath := filepath.Join(root, "logs", "sessions.jsonl")
	write(t, logPath, start+"\n"+end+"\n")

	if _, err := store.Ingest(ctx, root); err != nil {
		t.Fatal(err)
	}
	sess, err := store.Session(ctx, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != "ended" {
		t.Fatalf("status = %s, want ended", sess.Status)
	}

	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(start + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := store.Ingest(ctx, root); err != nil {
		t.Fatal(err)
	}
	sess, err = store.Session(ctx, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != "ended" {
		t.Fatalf("replayed start resurrected the session: %s", sess.Status)
	}
}
