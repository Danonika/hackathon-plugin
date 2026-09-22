package learning

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, text := range texts {
		text = strings.ToLower(text)
		switch {
		case strings.Contains(text, "alpha"):
			out[i] = []float64{1, 0}
		case strings.Contains(text, "beta"):
			out[i] = []float64{0, 1}
		default:
			out[i] = []float64{0.7, 0.7}
		}
	}
	return out, nil
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "learnings.json"), "p-test")
}

func TestRecordDeduplicatesAndScoresConfidence(t *testing.T) {
	store := newTestStore(t)
	first, err := store.Record(context.Background(), RecordInput{
		Trigger: "when a Go tool handler returns an error",
		Insight: "return the shared envelope instead of a Go error",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created {
		t.Fatal("first record must create a learning")
	}
	if first.Learning.Confidence != 0.6 {
		t.Fatalf("confidence = %v, want 0.6", first.Learning.Confidence)
	}
	second, err := store.Record(context.Background(), RecordInput{
		Trigger: "when a Go tool handler returns an error",
		Insight: "return the shared envelope instead of a Go error",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Created {
		t.Fatal("repeated trigger+insight must update, not create")
	}
	if second.Learning.Observations != 2 || second.Learning.Confidence != 0.7 {
		t.Fatalf("unexpected update %+v", second.Learning)
	}
	if store.Count() != 1 {
		t.Fatalf("count = %d, want 1", store.Count())
	}
}

func TestRecordFailureLowersConfidence(t *testing.T) {
	store := newTestStore(t)
	_, _ = store.Record(context.Background(), RecordInput{Trigger: "t", Insight: "i"})
	result, err := store.Record(context.Background(), RecordInput{Trigger: "t", Insight: "i", Outcome: "failure"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Learning.Confidence != 0.5 {
		t.Fatalf("confidence = %v, want 0.5", result.Learning.Confidence)
	}
	if _, err := store.Record(context.Background(), RecordInput{Trigger: "t", Insight: "i", Outcome: "bogus"}); err == nil {
		t.Fatal("invalid outcome must fail")
	}
}

func TestRecallScopeAndEmbeddings(t *testing.T) {
	store := newTestStore(t).WithEmbedder(fakeEmbedder{})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := store.Record(ctx, RecordInput{Trigger: "alpha telemetry anomaly", Insight: "use rolling zscore", Evidence: []string{"ev-1"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Record(ctx, RecordInput{Trigger: "beta contract review", Insight: "cite clauses", Scope: "global"}); err != nil {
		t.Fatal(err)
	}

	projectMatches, err := store.Recall(ctx, "alpha anomaly detection", 5, 0.5, "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(projectMatches) != 2 {
		t.Fatalf("project recall = %d matches, want 2 (project + global)", len(projectMatches))
	}
	if !strings.Contains(projectMatches[0].Trigger, "alpha") {
		t.Fatalf("alpha learning must rank first, got %+v", projectMatches[0])
	}
	if projectMatches[0].Score <= projectMatches[1].Score {
		t.Fatalf("expected descending scores, got %+v", projectMatches)
	}

	globalMatches, err := store.Recall(ctx, "alpha anomaly detection", 5, 0.5, "global")
	if err != nil {
		t.Fatal(err)
	}
	if len(globalMatches) != 1 || globalMatches[0].Scope != "global" {
		t.Fatalf("global recall = %+v, want only the global learning", globalMatches)
	}

	if _, err := store.Recall(ctx, "alpha", 5, 0.95, "all"); err != nil {
		t.Fatal(err)
	}
}

func TestRecallKeywordFallback(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Record(ctx, RecordInput{Trigger: "when indexing a PDF without a text layer", Insight: "report the OCR limitation"}); err != nil {
		t.Fatal(err)
	}
	matches, err := store.Recall(ctx, "PDF OCR text layer", 5, 0.3, "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Score <= 0 {
		t.Fatalf("keyword recall failed: %+v", matches)
	}
}

func TestPromoteRequiresObservationsAndConfidence(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	var last RecordResult
	for i := 0; i < 3; i++ {
		result, err := store.Record(ctx, RecordInput{Trigger: "always validate input paths", Insight: "reject .. segments"})
		if err != nil {
			t.Fatal(err)
		}
		last = result
	}
	if _, promoted, err := store.Promote(last.Learning.ID, 5, 0.7); err != nil || promoted {
		t.Fatalf("promotion must be denied before enough observations (err=%v promoted=%v)", err, promoted)
	}
	item, promoted, err := store.Promote(last.Learning.ID, 3, 0.7)
	if err != nil {
		t.Fatal(err)
	}
	if !promoted || item.Scope != "global" {
		t.Fatalf("expected promotion, got %+v promoted=%v", item, promoted)
	}
	if _, promoted, err := store.Promote(last.Learning.ID, 3, 0.7); err != nil || promoted {
		t.Fatalf("second promotion must be a no-op (err=%v promoted=%v)", err, promoted)
	}
	if _, _, err := store.Promote("l-missing", 1, 0.3); err == nil {
		t.Fatal("unknown id must fail")
	}
}

func TestEvictKeepsNewest(t *testing.T) {
	base := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := base.Record(ctx, RecordInput{Trigger: "t", Insight: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := base.load()
	if err != nil {
		t.Fatal(err)
	}
	kept := evict(doc.Learnings, 3)
	if len(kept) != 3 {
		t.Fatalf("kept = %d, want 3", len(kept))
	}
	if kept[0].Insight != "c" || kept[2].Insight != "e" {
		t.Fatalf("eviction kept the wrong entries: %+v", kept)
	}
}

func TestProjectKeyIsStable(t *testing.T) {
	root := t.TempDir()
	if ProjectKey(root) != ProjectKey(root) {
		t.Fatal("project key must be stable")
	}
	if ProjectKey(root) == ProjectKey(root+"/other") {
		t.Fatal("different roots must not share a key")
	}
}
