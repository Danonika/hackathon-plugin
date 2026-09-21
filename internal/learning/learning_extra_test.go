package learning

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type errEmbedder struct{ err error }

func (e errEmbedder) Embed(context.Context, []string) ([][]float64, error) { return nil, e.err }

type scriptedEmbedder struct {
	vectors [][]float64
}

func (e scriptedEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		if i < len(e.vectors) {
			out[i] = e.vectors[i]
		}
	}
	return out, nil
}

type querySwitchEmbedder struct{}

func (querySwitchEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, text := range texts {
		if strings.Contains(text, "three-d") {
			out[i] = []float64{1, 0, 0}
		} else {
			out[i] = []float64{1, 0}
		}
	}
	return out, nil
}

func TestStorePathAndClock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learnings.json")
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store := NewStore(path, "p-test").WithClock(func() time.Time { return fixed })
	if store.Path() != path {
		t.Fatalf("Path = %q", store.Path())
	}
	if store.WithClock(nil) != store {
		t.Fatal("WithClock must be chainable")
	}
	result, err := store.Record(context.Background(), RecordInput{Trigger: "clock trigger", Insight: "clock insight"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Learning.CreatedAt.Equal(fixed) || !result.Learning.UpdatedAt.Equal(fixed) {
		t.Fatalf("timestamps = %+v", result.Learning)
	}
}

func TestRecordValidation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Record(ctx, RecordInput{Insight: "no trigger"}); err == nil {
		t.Fatal("empty trigger must fail")
	}
	if _, err := store.Record(ctx, RecordInput{Trigger: "no insight"}); err == nil {
		t.Fatal("empty insight must fail")
	}
	if _, err := store.Record(ctx, RecordInput{Trigger: "t", Insight: "i", Scope: "galaxy"}); err == nil {
		t.Fatal("invalid scope must fail")
	}
}

func TestRecordLoadAndSaveErrors(t *testing.T) {
	dir := t.TempDir()
	asDir := NewStore(dir, "p")
	if _, err := asDir.Record(context.Background(), RecordInput{Trigger: "t", Insight: "i"}); err == nil {
		t.Fatal("expected load error when the store path is a directory")
	}

	if os.Geteuid() == 0 {
		return
	}
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
	store := NewStore(filepath.Join(readOnly, "learnings.json"), "p")
	if _, err := store.Record(context.Background(), RecordInput{Trigger: "t", Insight: "i"}); err == nil {
		t.Fatal("expected save error in a read-only directory")
	}
}

func TestRecordEmbedFailuresAreIgnored(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t).WithEmbedder(errEmbedder{err: errors.New("embed down")})
	result, err := store.Record(ctx, RecordInput{Trigger: "embed trigger", Insight: "embed insight"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Learning.Vector) != 0 {
		t.Fatalf("vector = %v", result.Learning.Vector)
	}

	for _, vectors := range [][][]float64{{}, {{}}} {
		s := newTestStore(t).WithEmbedder(scriptedEmbedder{vectors: vectors})
		if _, err := s.Record(ctx, RecordInput{Trigger: "empty trigger", Insight: "empty insight"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTopOrderingAndFiltering(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := 0
	store.WithClock(func() time.Time {
		step++
		return base.Add(time.Duration(step) * time.Minute)
	})

	for i := 0; i < 3; i++ {
		if _, err := store.Record(ctx, RecordInput{Trigger: "alpha", Insight: "one"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Record(ctx, RecordInput{Trigger: "beta", Insight: "two"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, RecordInput{Trigger: "gamma", Insight: "three", Scope: "global"}); err != nil {
		t.Fatal(err)
	}

	top, err := store.Top(0, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 3 {
		t.Fatalf("Top = %d entries, want 3 (project + global): %+v", len(top), top)
	}
	if top[0].Trigger != "alpha" {
		t.Fatalf("highest confidence must rank first: %+v", top)
	}
	limited, err := store.Top(1, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].Trigger != "alpha" {
		t.Fatalf("limited Top = %+v", limited)
	}
	none, err := store.Top(5, 0.99)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("confidence filter failed: %+v", none)
	}
}

func TestTopLoadError(t *testing.T) {
	store := NewStore(t.TempDir(), "p")
	if _, err := store.Top(5, 0.3); err == nil {
		t.Fatal("expected load error")
	}
}

func TestPromoteEdges(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	result, err := store.Record(ctx, RecordInput{Trigger: "promote me", Insight: "now"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Promote("  ", 1, 0.3); err == nil {
		t.Fatal("empty id must fail")
	}
	item, promoted, err := store.Promote(result.Learning.ID, 0, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if !promoted || item.Scope != "global" || item.Project != "" {
		t.Fatalf("promotion = %+v promoted=%v", item, promoted)
	}

	if os.Geteuid() == 0 {
		return
	}
	readOnly := t.TempDir()
	store2 := NewStore(filepath.Join(readOnly, "learnings.json"), "p")
	res2, err := store2.Record(ctx, RecordInput{Trigger: "save fails", Insight: "later"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
	if _, _, err := store2.Promote(res2.Learning.ID, 1, 0.3); err == nil {
		t.Fatal("expected save error during promotion")
	}
}

func TestLoadEdgeCases(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(dir, "broken.json"), "p")
	if _, err := store.load(); err == nil {
		t.Fatal("expected parse error")
	}
	if store.Count() != 0 {
		t.Fatalf("Count on a corrupt store = %d", store.Count())
	}

	nulls := filepath.Join(dir, "nulls.json")
	if err := os.WriteFile(nulls, []byte(`{"version":1,"learnings":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := NewStore(nulls, "p").load()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Learnings == nil {
		t.Fatal("nil learnings must be normalized to an empty slice")
	}

	if os.Geteuid() != 0 {
		locked := filepath.Join(dir, "locked.json")
		if err := os.WriteFile(locked, []byte("{}"), 0o000); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(locked, "p").load(); err == nil {
			t.Fatal("expected permission error")
		}
	}
}

func TestSaveErrors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(blocker, "learnings.json"), "p")
	if err := store.save(fileFormat{Version: 1}); err == nil {
		t.Fatal("expected mkdir error")
	}

	occupied := filepath.Join(dir, "occupied")
	if err := os.MkdirAll(occupied, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(occupied, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(occupied, "p").save(fileFormat{Version: 1}); err == nil {
		t.Fatal("expected rename error when the target is a directory")
	}
}

func TestEmbedValidation(t *testing.T) {
	for _, tc := range []scriptedEmbedder{{vectors: nil}, {vectors: [][]float64{{}}}} {
		store := newTestStore(t).WithEmbedder(tc)
		if _, err := store.embed(context.Background(), "x"); err == nil {
			t.Fatal("expected empty embedding error")
		}
	}
}

func TestRecallEdges(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Recall(ctx, "   ", 5, 0.3, "project"); err == nil {
		t.Fatal("empty query must fail")
	}
	if _, err := store.Recall(ctx, "anything", 100, 0.3, "all"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Recall(ctx, "anything", 0, 0.3, ""); err != nil {
		t.Fatal(err)
	}

	errStore := NewStore(t.TempDir(), "p")
	if _, err := errStore.Recall(ctx, "q", 5, 0.3, "project"); err == nil {
		t.Fatal("expected load error")
	}

	embedStore := newTestStore(t).WithEmbedder(errEmbedder{err: errors.New("down")})
	if _, err := embedStore.Record(ctx, RecordInput{Trigger: "fallback query", Insight: "keyword path"}); err != nil {
		t.Fatal(err)
	}
	if _, err := embedStore.Recall(ctx, "fallback query", 5, 0.3, "project"); err != nil {
		t.Fatal(err)
	}
}

func TestRecallVectorLengthMismatchFallsBackToKeywords(t *testing.T) {
	store := newTestStore(t).WithEmbedder(querySwitchEmbedder{})
	ctx := context.Background()
	if _, err := store.Record(ctx, RecordInput{Trigger: "three-d trigger", Insight: "two-d vector"}); err != nil {
		t.Fatal(err)
	}
	matches, err := store.Recall(ctx, "three-d", 5, 0.3, "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Score <= 0 {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestMergeEvidenceDedupeAndCap(t *testing.T) {
	existing := []string{"a", "b"}
	merged := mergeEvidence(existing, []string{"  ", "b", "c"})
	if len(merged) != 3 || merged[0] != "a" || merged[1] != "b" || merged[2] != "c" {
		t.Fatalf("merged = %v", merged)
	}
	dupes := mergeEvidence([]string{"a", "a"}, []string{"a"})
	if len(dupes) != 2 {
		t.Fatalf("existing duplicates must be preserved as-is: %v", dupes)
	}
	var many []string
	for i := 0; i < maxEvidence+5; i++ {
		many = append(many, string(rune('a'+i)))
	}
	capped := mergeEvidence(nil, many)
	if len(capped) != maxEvidence {
		t.Fatalf("capped len = %d, want %d", len(capped), maxEvidence)
	}
	if capped[0] != many[len(many)-maxEvidence] {
		t.Fatalf("cap must keep the newest evidence: %v", capped)
	}
}

func TestSimilarityAndNormalizeEdges(t *testing.T) {
	if got := keywordSimilarity(nil, []string{"a"}); got != 0 {
		t.Fatalf("empty query similarity = %v", got)
	}
	if got := cosine01([]float64{1, 0}, []float64{0, 0}); got != 0 {
		t.Fatalf("zero vector cosine = %v", got)
	}
	if got := cosine01([]float64{1, 0}, []float64{-1, 0}); got != 0 {
		t.Fatalf("opposite cosine = %v", got)
	}
	if got := normalize([]float64{0, 0}); got[0] != 0 || got[1] != 0 {
		t.Fatalf("zero normalize = %v", got)
	}
	normalized := normalize([]float64{3, 4})
	if normalized[0] != 0.6 || normalized[1] != 0.8 {
		t.Fatalf("normalize = %v", normalized)
	}
}

func TestProjectKeyDeletedCwd(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	got := ProjectKey("")
	if !strings.HasPrefix(got, "p-") {
		t.Fatalf("ProjectKey = %q", got)
	}
}
