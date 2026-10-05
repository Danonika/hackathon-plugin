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

	agg := AggregateReports(reports, sha, DefaultMinValidJudges)
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
	// Regulation 9.5: fewer than three valid scores require substitute judges;
	// a missing report is never counted as zero.
	twoValid := AggregateReports([]JudgeReport{
		mkReport("judge-01", "sha", uniform(80)),
		mkReport("judge-02", "sha", uniform(84)),
	}, "sha", DefaultMinValidJudges)
	if !twoValid.Inconclusive {
		t.Fatal("two valid reports must be inconclusive with min_valid_judges=3")
	}
	if twoValid.MinValid != 3 || twoValid.RubricVersion != RubricVersion {
		t.Fatalf("aggregate provenance missing: %+v", twoValid)
	}
	if twoValid.MeanTotal != 82 {
		t.Fatalf("mean = %v, want 82 (dropped reports are not zeros)", twoValid.MeanTotal)
	}

	threeValid := AggregateReports([]JudgeReport{
		mkReport("judge-01", "sha", uniform(80)),
		mkReport("judge-02", "sha", uniform(84)),
		mkReport("judge-03", "sha", uniform(82)),
	}, "sha", DefaultMinValidJudges)
	if threeValid.Inconclusive {
		t.Fatal("three valid reports must be conclusive")
	}
	if threeValid.MeanTotal != 82 || threeValid.MeanTotalRounded != 82 {
		t.Fatalf("mean = %v rounded = %v, want 82", threeValid.MeanTotal, threeValid.MeanTotalRounded)
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

	agg := AggregateReports([]JudgeReport{first, second, third}, sha, DefaultMinValidJudges)
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
	run1 := Aggregate{ValidJudges: 10, MeanTotal: 80, RubricVersion: RubricVersion}
	run2 := Aggregate{ValidJudges: 10, MeanTotal: 83.4, RubricVersion: RubricVersion}
	cal, err := Calibrate(run1, run2, 0.5, DefaultMinValidJudges)
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
	cal, err = Calibrate(Aggregate{ValidJudges: 10, MeanTotal: 80, RubricVersion: RubricVersion}, Aggregate{ValidJudges: 10, MeanTotal: 80.2, RubricVersion: RubricVersion}, 1, DefaultMinValidJudges)
	if err != nil {
		t.Fatal(err)
	}
	if cal.NoiseFloor != 1 {
		t.Fatalf("noise floor = %v, want 1", cal.NoiseFloor)
	}
	if _, err := Calibrate(Aggregate{ValidJudges: 1}, Aggregate{ValidJudges: 3, MeanTotal: 80}, 0.5, DefaultMinValidJudges); err == nil {
		t.Fatal("inconclusive run must fail calibration")
	}
	if _, err := Calibrate(Aggregate{ValidJudges: 10, MeanTotal: 80, SnapshotSHA: "a"}, Aggregate{ValidJudges: 10, MeanTotal: 80, SnapshotSHA: "b"}, 0.5, DefaultMinValidJudges); err == nil {
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

func rankAggregate(sha string, total, resultQuality, value, presentation float64) Aggregate {
	return Aggregate{
		SnapshotSHA:      sha,
		RubricVersion:    RubricVersion,
		MinValid:         DefaultMinValidJudges,
		ValidJudges:      3,
		MeanTotal:        total,
		MeanTotalRounded: round2(total),
		CriterionMeans: map[string]float64{
			"value":                value,
			"result_quality":       resultQuality,
			"innovation":           10,
			"scalability":          10,
			"presentation_demo_qa": presentation,
		},
	}
}

func TestRankAppliesOfficialTieBreaks(t *testing.T) {
	// Regulation 9.6: equal totals break on result_quality, then value, then
	// presentation_demo_qa.
	ranking, err := Rank([]Aggregate{
		rankAggregate("a", 80, 15, 20, 15),
		rankAggregate("b", 80, 16, 19, 15),
		rankAggregate("c", 75, 20, 20, 20),
	})
	if err != nil {
		t.Fatal(err)
	}
	order := []string{ranking.Entries[0].SnapshotSHA, ranking.Entries[1].SnapshotSHA, ranking.Entries[2].SnapshotSHA}
	if order[0] != "b" || order[1] != "a" || order[2] != "c" {
		t.Fatalf("order = %v, want [b a c]", order)
	}
	if ranking.Entries[1].TieBreak != "result_quality" {
		t.Fatalf("tie-break label = %q, want result_quality", ranking.Entries[1].TieBreak)
	}
	if ranking.Entries[0].Rank != 1 || ranking.Entries[2].Rank != 3 {
		t.Fatalf("ranks = %+v", ranking.Entries)
	}
	if len(ranking.Unresolved) != 0 {
		t.Fatalf("unexpected unresolved tie: %v", ranking.Unresolved)
	}

	valueTie, err := Rank([]Aggregate{
		rankAggregate("a", 80, 16, 19, 15),
		rankAggregate("b", 80, 16, 20, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	if valueTie.Entries[0].SnapshotSHA != "b" || valueTie.Entries[1].TieBreak != "value" {
		t.Fatalf("value tie-break failed: %+v", valueTie.Entries)
	}

	presentationTie, err := Rank([]Aggregate{
		rankAggregate("a", 80, 16, 20, 15),
		rankAggregate("b", 80, 16, 20, 16),
	})
	if err != nil {
		t.Fatal(err)
	}
	if presentationTie.Entries[0].SnapshotSHA != "b" || presentationTie.Entries[1].TieBreak != "presentation_demo_qa" {
		t.Fatalf("presentation tie-break failed: %+v", presentationTie.Entries)
	}
}

func TestRankFlagsUnresolvedTie(t *testing.T) {
	ranking, err := Rank([]Aggregate{
		rankAggregate("a", 80, 16, 20, 15),
		rankAggregate("b", 80, 16, 20, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranking.Unresolved) != 2 {
		t.Fatalf("unresolved = %v, want both snapshots", ranking.Unresolved)
	}
}

func TestApplyCommissionDecidesTie(t *testing.T) {
	ranking, err := Rank([]Aggregate{
		rankAggregate("a", 80, 16, 20, 15),
		rankAggregate("b", 80, 16, 20, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	decided, err := ApplyCommission(ranking, Commission{
		Members: []string{"judge-05", "judge-06", "judge-07"},
		Votes:   map[string]int{"a": 2, "b": 1},
		Grounds: "a demonstrates the mandatory scenario end to end",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decided.Entries[0].SnapshotSHA != "a" || decided.Entries[0].TieBreak != "commission" {
		t.Fatalf("commission order = %+v", decided.Entries)
	}
	if len(decided.Unresolved) != 0 || decided.Commission == nil {
		t.Fatalf("commission not recorded: %+v", decided)
	}

	if _, err := ApplyCommission(ranking, Commission{Members: []string{"j1", "j2"}, Votes: map[string]int{"a": 1, "b": 1}}); err == nil {
		t.Fatal("a commission with fewer than 3 members must fail")
	}
	if _, err := ApplyCommission(ranking, Commission{Members: []string{"j1", "j2", "j3"}, Votes: map[string]int{"a": 1, "b": 1}}); err == nil {
		t.Fatal("a vote total that does not match the members must fail")
	}
	if _, err := ApplyCommission(ranking, Commission{Members: []string{"j1", "j2", "j3"}, Votes: map[string]int{"a": 2, "b": 2}}); err == nil {
		t.Fatal("a non-strict majority must fail")
	}
}

func TestRankRejectsInconclusiveAndMixedRubric(t *testing.T) {
	if _, err := Rank([]Aggregate{{SnapshotSHA: "a", Inconclusive: true, ValidJudges: 1, MinValid: 3}}); err == nil {
		t.Fatal("inconclusive aggregates must not be ranked")
	}
	mixed := rankAggregate("a", 80, 16, 20, 15)
	mixed.RubricVersion = "1.0.0"
	if _, err := Rank([]Aggregate{mixed}); err == nil {
		t.Fatal("aggregates from another rubric version must not be ranked")
	}
	if _, err := Rank(nil); err == nil {
		t.Fatal("ranking without aggregates must fail")
	}
}

func TestCompareUsesUnroundedValues(t *testing.T) {
	// Raw improvement 0.004 clears a 0.001 noise floor; rounding to 2 decimals
	// would wrongly report no improvement.
	out := Compare(CompareInput{Mode: ModeTechnical, Candidate: 80.004, Best: 80, Target: 0, NoiseFloor: 0.001})
	if out.Decision != DecisionAccept {
		t.Fatalf("decision = %s, want accept (raw improvement)", out.Decision)
	}
	if out.Improvement != 0 {
		t.Fatalf("displayed improvement = %v, want 0 after rounding", out.Improvement)
	}
}
