package evaluation

import (
	"strings"
	"testing"
)

func mkReport(judgeID, sha string, scores map[string]float64) JudgeReport {
	return JudgeReport{
		JudgeID:     judgeID,
		SnapshotSHA: sha,
		Scores:      scores,
		Evidence:    []Evidence{{Criterion: "value", Finding: "scenario works", Source: "smoke-results.json"}},
	}
}

// uniform spreads a total score across the five criteria by their weights.
func uniform(total float64) map[string]float64 {
	return map[string]float64{
		"value":                total * 0.25,
		"result_quality":       total * 0.20,
		"innovation":           total * 0.15,
		"scalability":          total * 0.20,
		"presentation_demo_qa": total * 0.20,
	}
}

func TestAggregateReportsFiltersAndAverages(t *testing.T) {
	sha := "abc123"
	reports := []JudgeReport{
		mkReport("judge-01", sha, uniform(80)),
		mkReport("judge-02", sha, uniform(90)),
		mkReport("judge-03", sha, uniform(70)),
	}
	conflicted := mkReport("judge-04", sha, uniform(100))
	conflicted.ConflictOfInterest = true
	reports = append(reports, conflicted)
	noEvidence := mkReport("judge-05", sha, uniform(100))
	noEvidence.Evidence = nil
	reports = append(reports, noEvidence)
	foreign := mkReport("judge-06", "other-sha", uniform(100))
	reports = append(reports, foreign)

	agg := AggregateReports(reports, sha)
	if agg.ValidJudges != 3 {
		t.Fatalf("valid judges = %d, want 3 (%+v)", agg.ValidJudges, agg.DroppedInvalid)
	}
	if agg.Inconclusive {
		t.Fatal("three valid reports must not be inconclusive")
	}
	if agg.MeanTotal != 80 {
		t.Fatalf("mean = %v, want 80", agg.MeanTotal)
	}
	if agg.CriterionMeans["value"] != 20 {
		t.Fatalf("criterion mean = %v, want 20", agg.CriterionMeans["value"])
	}
	if agg.ScoreVariance != 100 { // sample variance of {80, 90, 70}
		t.Fatalf("variance = %v, want 100", agg.ScoreVariance)
	}
	if len(agg.DroppedInvalid) != 2 {
		t.Fatalf("dropped invalid = %d, want 2 (%+v)", len(agg.DroppedInvalid), agg.DroppedInvalid)
	}
	if len(agg.DroppedConflict) != 1 || agg.DroppedConflict[0] != "judge-04" {
		t.Fatalf("dropped conflict = %+v, want judge-04", agg.DroppedConflict)
	}
}

func TestAggregateReportsInconclusiveBelowThreshold(t *testing.T) {
	agg := AggregateReports([]JudgeReport{
		mkReport("judge-01", "sha", uniform(80)),
	}, "sha")
	if !agg.Inconclusive {
		t.Fatal("a single valid report must be inconclusive")
	}
	if agg.MeanTotal != 80 {
		t.Fatalf("mean = %v, want 80", agg.MeanTotal)
	}
}

func TestAggregateHighDisagreementAndGrouping(t *testing.T) {
	sha := "sha"
	first := mkReport("judge-01", sha, uniform(80))
	first.Deductions = []Deduction{
		{Criterion: "innovation", Points: 4, Reason: "Fly advantage not quantified"},
	}
	second := mkReport("judge-02", sha, uniform(100))
	second.Deductions = []Deduction{
		{Criterion: "innovation", Points: 3, Reason: "fly advantage not quantified"},
	}
	second.Questions = []string{"What happens if MCP fails?", "Why Fly?"}
	third := mkReport("judge-03", sha, uniform(80))
	third.Questions = []string{"What happens if MCP fails?"}

	agg := AggregateReports([]JudgeReport{first, second, third}, sha)
	if len(agg.CommonDeductions) != 1 {
		t.Fatalf("expected one grouped deduction, got %+v", agg.CommonDeductions)
	}
	if agg.CommonDeductions[0].Count != 2 || agg.CommonDeductions[0].Points != 7 {
		t.Fatalf("unexpected deduction group %+v", agg.CommonDeductions[0])
	}
	if len(agg.LikelyQuestions) != 2 || agg.LikelyQuestions[0].Count != 2 {
		t.Fatalf("unexpected question counts %+v", agg.LikelyQuestions)
	}
	if len(agg.HighDisagreement) != 2 {
		t.Fatalf("expected top-quartile disagreement in 2 criteria, got %+v", agg.HighDisagreement)
	}
}

func TestValidateJudgeReport(t *testing.T) {
	base := mkReport("judge-01", "sha", uniform(80))
	if problems := ValidateJudgeReport(base, "sha"); len(problems) != 0 {
		t.Fatalf("valid report rejected: %v", problems)
	}
	badTotal := base
	badTotal.Total = 99
	if problems := ValidateJudgeReport(badTotal, "sha"); len(problems) == 0 {
		t.Fatal("mismatched total must be rejected")
	}
	outOfRange := base
	outOfRange.Scores = uniform(80)
	outOfRange.Scores["innovation"] = 99
	if problems := ValidateJudgeReport(outOfRange, "sha"); len(problems) == 0 {
		t.Fatal("out-of-range score must be rejected")
	}
	unknown := base
	unknown.Scores = uniform(80)
	unknown.Scores["vibes"] = 1
	if problems := ValidateJudgeReport(unknown, "sha"); len(problems) == 0 {
		t.Fatal("unknown criterion must be rejected")
	}
	missing := base
	missing.Scores = map[string]float64{"value": 20}
	if problems := ValidateJudgeReport(missing, "sha"); len(problems) < 4 {
		t.Fatalf("missing criteria must be rejected, got %v", problems)
	}
}

func TestValidateJudgeReportUnknownCriteriaAreSorted(t *testing.T) {
	report := mkReport("judge-01", "sha", uniform(80))
	report.Scores["zzz"] = 1
	report.Scores["aaa"] = 1
	problems := ValidateJudgeReport(report, "sha")
	joined := strings.Join(problems, "\n")
	aaa := strings.Index(joined, "unknown criterion aaa")
	zzz := strings.Index(joined, "unknown criterion zzz")
	if aaa < 0 || zzz < 0 {
		t.Fatalf("unknown criteria must be reported: %v", problems)
	}
	if aaa > zzz {
		t.Fatalf("unknown criteria reasons must be sorted for reproducibility: %v", problems)
	}
}

func TestCalibrateUsesRetestDelta(t *testing.T) {
	run1 := Aggregate{ValidJudges: 10, MeanTotal: 80}
	run2 := Aggregate{ValidJudges: 10, MeanTotal: 83.4}
	cal, err := Calibrate(run1, run2, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if cal.RetestDelta != 3.4 {
		t.Fatalf("retest delta = %v, want 3.4", cal.RetestDelta)
	}
	if cal.NoiseFloor != 6.8 {
		t.Fatalf("noise floor = %v, want 6.8", cal.NoiseFloor)
	}
	// min_delta dominates when retest noise is small.
	cal, err = Calibrate(Aggregate{ValidJudges: 10, MeanTotal: 80}, Aggregate{ValidJudges: 10, MeanTotal: 80.2}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cal.NoiseFloor != 1 {
		t.Fatalf("noise floor = %v, want 1", cal.NoiseFloor)
	}
	if _, err := Calibrate(Aggregate{ValidJudges: 1}, Aggregate{ValidJudges: 3, MeanTotal: 80}, 0.5); err == nil {
		t.Fatal("inconclusive run must fail calibration")
	}
	if _, err := Calibrate(Aggregate{ValidJudges: 10, MeanTotal: 80, SnapshotSHA: "a"}, Aggregate{ValidJudges: 10, MeanTotal: 80, SnapshotSHA: "b"}, 0.5); err == nil {
		t.Fatal("calibration across different snapshots must fail")
	}
}

func TestCompareTechnicalLoop(t *testing.T) {
	cases := []struct {
		name string
		in   CompareInput
		want Decision
	}{
		{"mandatory failure rolls back", CompareInput{Mode: ModeTechnical, Candidate: 99, Best: 80, Target: 95, MandatoryScenarioFailed: true}, DecisionRollback},
		{"decrease rolls back", CompareInput{Mode: ModeTechnical, Candidate: 79, Best: 80, Target: 95}, DecisionRollback},
		{"target reached", CompareInput{Mode: ModeTechnical, Candidate: 96, Best: 80, Target: 95, NoiseFloor: 0.5}, DecisionReached},
		{"improvement below floor stops", CompareInput{Mode: ModeTechnical, Candidate: 80.4, Best: 80, Target: 95, NoiseFloor: 0.5}, DecisionStop},
		{"accepted", CompareInput{Mode: ModeTechnical, Candidate: 85, Best: 80, Target: 95, NoiseFloor: 0.5}, DecisionAccept},
	}
	for _, c := range cases {
		if got := Compare(c.in).Decision; got != c.want {
			t.Fatalf("%s: decision = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestCompareJudgeLoop(t *testing.T) {
	cases := []struct {
		name string
		in   CompareInput
		want Decision
	}{
		{"regression above floor rolls back", CompareInput{Mode: ModeJudge, Candidate: 87, Best: 90, Target: 90, NoiseFloor: 2}, DecisionRollback},
		{"small regression below floor stops", CompareInput{Mode: ModeJudge, Candidate: 89, Best: 90, Target: 92, NoiseFloor: 2}, DecisionStop},
		{"target reached", CompareInput{Mode: ModeJudge, Candidate: 91, Best: 90, Target: 90, NoiseFloor: 2}, DecisionReached},
		{"accepted", CompareInput{Mode: ModeJudge, Candidate: 93, Best: 90, Target: 95, NoiseFloor: 2}, DecisionAccept},
	}
	for _, c := range cases {
		if got := Compare(c.in).Decision; got != c.want {
			t.Fatalf("%s: decision = %s, want %s", c.name, got, c.want)
		}
	}
}
