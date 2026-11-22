package plans

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadLatest(t *testing.T) {
	root := t.TempDir()
	plan := Plan{
		Slug:   "Telemetry Anomalies!",
		Task:   "input -> report",
		Stages: DefaultStages,
	}
	if err := Save(root, plan); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(Path(root, "telemetry-anomalies"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("plan mode = %v, want 0600", info.Mode().Perm())
		}
	}
	loaded, err := Load(root, "telemetry-anomalies")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != 1 || loaded.Slug != "telemetry-anomalies" {
		t.Fatalf("unexpected plan %+v", loaded)
	}
	if len(loaded.Stages) != len(DefaultStages) {
		t.Fatalf("stages = %d, want %d", len(loaded.Stages), len(DefaultStages))
	}

	other := Plan{Slug: "second", Stages: DefaultStages}
	if err := Save(root, other); err != nil {
		t.Fatal(err)
	}
	// Make the second plan the newest by mtime.
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(Path(root, "second"), future, future)

	latest, ok := Latest(root)
	if !ok || latest.Slug != "second" {
		t.Fatalf("latest = %+v ok=%v, want second", latest, ok)
	}
	if _, err := Load(root, "missing"); err == nil {
		t.Fatal("missing plan must fail")
	}
}

func TestSummaryAndCurrentStage(t *testing.T) {
	plan := Plan{Slug: "demo", Revision: 2, Stages: []Stage{
		{ID: "S0", Status: "done"},
		{ID: "S1", Status: "doing"},
		{ID: "S2", Status: "todo"},
	}}
	if got := Summary(plan); !strings.Contains(got, "S1 (doing)") {
		t.Fatalf("summary = %q", got)
	}
	stage, ok := CurrentStage(plan)
	if !ok || stage.ID != "S1" {
		t.Fatalf("current stage = %+v ok=%v", stage, ok)
	}
	complete := Plan{Slug: "done", Revision: 1, Stages: []Stage{{ID: "S0", Status: "done"}, {ID: "S1", Status: "cut"}}}
	if _, ok := CurrentStage(complete); ok {
		t.Fatal("all done/cut stages must have no current stage")
	}
	if !strings.Contains(Summary(complete), "complete") {
		t.Fatalf("summary = %q", Summary(complete))
	}
}

func TestSlugAndStatusValidation(t *testing.T) {
	cases := map[string]string{
		"Telemetry Anomalies!": "telemetry-anomalies",
		"  --weird--  ":        "weird",
		"":                     "plan",
	}
	for input, want := range cases {
		if got := Slug(input); got != want {
			t.Fatalf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
	if !ValidStatus("doing") || ValidStatus("bogus") {
		t.Fatal("status validation failed")
	}
}

func TestSaveRejectsEmptySlug(t *testing.T) {
	if err := Save(t.TempDir(), Plan{}); err == nil {
		t.Fatal("empty slug must fail")
	}
}

func TestPlanDirUnderEvals(t *testing.T) {
	root := t.TempDir()
	if got := Dir(root); got != filepath.Join(root, "evals", "plans") {
		t.Fatalf("Dir = %q", got)
	}
}

func TestLatestIgnoresApprovalRecords(t *testing.T) {
	root := t.TempDir()
	const task = "Verify timetable"
	if err := Save(root, Plan{Slug: "release.v2", Revision: 3, Task: task, Stages: []Stage{{ID: "S1", Status: "doing"}}}); err != nil {
		t.Fatal(err)
	}
	approval := filepath.Join(Dir(root), "release-v2.approval.json")
	if err := os.WriteFile(approval, []byte(`{"slug":"release-v2","revision":3,"decision":"approve","decided_at":"2026-03-01T12:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}
	afterPlan := time.Now().Add(time.Hour)
	if err := os.Chtimes(approval, afterPlan, afterPlan); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LatestErr(root)
	if err != nil || !ok || got.Task != task || len(got.Stages) != 1 {
		t.Fatalf("latest plan after approval = %+v, found=%v, err=%v", got, ok, err)
	}
	if err := os.Remove(Path(root, "release.v2")); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LatestErr(root); ok || err != nil {
		t.Fatalf("approval alone returned found=%v, err=%v", ok, err)
	}
}
