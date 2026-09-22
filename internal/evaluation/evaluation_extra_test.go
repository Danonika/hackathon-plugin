package evaluation

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestValidateJudgeReportEdgeCases(t *testing.T) {
	base := mkReport("judge-01", "sha", uniform(80))

	cases := []struct {
		name   string
		mutate func(*JudgeReport)
		want   string
	}{
		{"nan score", func(r *JudgeReport) {
			r.Scores = uniform(80)
			r.Scores["value"] = math.NaN()
		}, "out of range"},
		{"positive infinity score", func(r *JudgeReport) {
			r.Scores = uniform(80)
			r.Scores["innovation"] = math.Inf(1)
		}, "out of range"},
		{"negative infinity score", func(r *JudgeReport) {
			r.Scores = uniform(80)
			r.Scores["innovation"] = math.Inf(-1)
		}, "out of range"},
		{"negative score", func(r *JudgeReport) {
			r.Scores = uniform(80)
			r.Scores["value"] = -1
		}, "out of range"},
		{"negative deduction", func(r *JudgeReport) {
			r.Deductions = []Deduction{{Criterion: "value", Points: -2, Reason: "unjustified"}}
		}, "non-finite or negative point value"},
		{"nan deduction", func(r *JudgeReport) {
			r.Deductions = []Deduction{{Criterion: "value", Points: math.NaN(), Reason: "unjustified"}}
		}, "non-finite or negative point value"},
		{"unknown deduction criterion", func(r *JudgeReport) {
			r.Deductions = []Deduction{{Criterion: "vibes", Points: 1, Reason: "unjustified"}}
		}, "unknown criterion"},
		{"deduction without a reason", func(r *JudgeReport) {
			r.Deductions = []Deduction{{Criterion: "value", Points: 1, Reason: "  "}}
		}, "without a reason"},
		{"missing judge id", func(r *JudgeReport) { r.JudgeID = "  " }, "missing judge_id"},
		{"missing snapshot", func(r *JudgeReport) { r.SnapshotSHA = "" }, "missing snapshot_sha"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := base
			report.Scores = uniform(80)
			tc.mutate(&report)
			problems := ValidateJudgeReport(report, "sha")
			if len(problems) == 0 {
				t.Fatal("expected validation problems")
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("problems = %v, want a %q problem", problems, tc.want)
			}
		})
	}

	if problems := ValidateJudgeReport(base, "other-sha"); len(problems) == 0 {
		t.Fatal("a snapshot mismatch must be rejected")
	}
	if problems := ValidateJudgeReport(base, ""); len(problems) != 0 {
		t.Fatalf("an empty expected snapshot skips the mismatch check: %v", problems)
	}
}

func TestAggregateReportsCustomMinValid(t *testing.T) {
	reports := []JudgeReport{
		mkReport("judge-01", "sha", uniform(80)),
		mkReport("judge-02", "sha", uniform(90)),
	}
	agg := AggregateReports(reports, "sha", 2)
	if agg.Inconclusive || agg.MinValid != 2 || agg.ValidJudges != 2 {
		t.Fatalf("minValid=2 must accept two reports: %+v", agg)
	}
	if agg.MeanTotal != 85 {
		t.Fatalf("mean = %v, want 85", agg.MeanTotal)
	}
	agg = AggregateReports(reports, "sha", 3)
	if !agg.Inconclusive {
		t.Fatal("minValid=3 must reject two reports")
	}

	empty := AggregateReports(nil, "sha", 0)
	if empty.MinValid != DefaultMinValidJudges || !empty.Inconclusive || empty.ValidJudges != 0 {
		t.Fatalf("empty aggregate = %+v", empty)
	}
	if empty.MeanTotal != 0 || len(empty.CriterionMeans) != 0 || len(empty.CommonDeductions) != 0 {
		t.Fatalf("empty aggregate must not fabricate values: %+v", empty)
	}
}

func TestAggregateUsesExplicitTotal(t *testing.T) {
	first := mkReport("judge-01", "sha", uniform(80))
	first.Total = 80
	agg := AggregateReports([]JudgeReport{
		first,
		mkReport("judge-02", "sha", uniform(80)),
		mkReport("judge-03", "sha", uniform(80)),
	}, "sha", 3)
	if agg.MeanTotal != 80 {
		t.Fatalf("mean = %v, want the explicit totals to be used", agg.MeanTotal)
	}
}

func TestGroupDeductionsSortsByCountCriterionReason(t *testing.T) {
	sha := "sha"
	first := mkReport("judge-01", sha, uniform(80))
	first.Deductions = []Deduction{
		{Criterion: "value", Points: 1, Reason: "reason a"},
		{Criterion: "innovation", Points: 1, Reason: "reason i"},
	}
	second := mkReport("judge-02", sha, uniform(80))
	second.Deductions = []Deduction{
		{Criterion: "value", Points: 1, Reason: "reason a"},
		{Criterion: "value", Points: 1, Reason: "reason c"},
	}
	third := mkReport("judge-03", sha, uniform(80))
	third.Deductions = []Deduction{{Criterion: "value", Points: 1, Reason: "reason b"}}
	third.TopImprovements = []string{"", "  ", "Improve X", "improve x", "Improve Y"}

	agg := AggregateReports([]JudgeReport{first, second, third}, sha, 3)
	type key struct {
		criterion string
		reason    string
		count     int
	}
	got := make([]key, 0, len(agg.CommonDeductions))
	for _, g := range agg.CommonDeductions {
		got = append(got, key{g.Criterion, g.Reason, g.Count})
	}
	want := []key{
		{"value", "reason a", 2},
		{"innovation", "reason i", 1},
		{"value", "reason b", 1},
		{"value", "reason c", 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deduction groups = %v, want %v", got, want)
	}

	items := make([]string, 0, len(agg.TopImprovements))
	for _, item := range agg.TopImprovements {
		items = append(items, item.Item)
	}
	if strings.Join(items, "|") != "Improve X|Improve Y" {
		t.Fatalf("improvements = %v, want empty entries skipped", items)
	}
	if agg.TopImprovements[0].Count != 2 {
		t.Fatalf("case-insensitive grouping count = %d, want 2", agg.TopImprovements[0].Count)
	}
}

func TestRankInconclusiveUsesDefaultMinValid(t *testing.T) {
	_, err := Rank([]Aggregate{{SnapshotSHA: "a", Inconclusive: true, ValidJudges: 1}})
	if err == nil || !strings.Contains(err.Error(), "3") {
		t.Fatalf("err = %v, want the default min valid judges", err)
	}
}

func TestApplyCommissionBreaksEqualVotesBySnapshot(t *testing.T) {
	ranking, err := Rank([]Aggregate{
		rankAggregate("a", 80, 16, 20, 15),
		rankAggregate("b", 80, 16, 20, 15),
		rankAggregate("c", 80, 16, 20, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	decided, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", "j2", "j3", "j4", "j5"},
		Votes:   map[string]int{"a": 3, "b": 1, "c": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, len(decided.Entries))
	for _, e := range decided.Entries {
		order = append(order, e.SnapshotSHA)
	}
	if strings.Join(order, ",") != "a,b,c" {
		t.Fatalf("commission order = %v, want [a b c]", order)
	}
}

func TestAverageAndVarianceEmptyInputs(t *testing.T) {
	if got := average(nil); got != 0 {
		t.Fatalf("average(nil) = %v, want 0", got)
	}
	if got := sampleVariance(nil); got != 0 {
		t.Fatalf("sampleVariance(nil) = %v, want 0", got)
	}
	if got := sampleVariance([]float64{5}); got != 0 {
		t.Fatalf("sampleVariance of one value = %v, want 0", got)
	}
	if got := average([]float64{2, 4}); got != 3 {
		t.Fatalf("average = %v, want 3", got)
	}
	if got := sampleVariance([]float64{2, 4}); got != 2 {
		t.Fatalf("sampleVariance = %v, want 2", got)
	}
}

func TestRoundingBoundaries(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{1.005, 1}, // 1.005*100 is just below 100.5 in float64
		{1.235, 1.24},
		{2.675, 2.68},
		{0.005, 0.01},
		{-1.005, -1},
		{99.999, 100},
	}
	for _, c := range cases {
		if got := round2(c.in); got != c.want {
			t.Fatalf("round2(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	if got := round6(1.0000005); got != 1.000001 {
		t.Fatalf("round6(1.0000005) = %v, want 1.000001", got)
	}
	if got := round6(1.0000004999); got != 1 {
		t.Fatalf("round6(1.0000004999) = %v, want 1", got)
	}
	if got := round6(1.2345675); got != 1.234568 {
		t.Fatalf("round6(1.2345675) = %v, want 1.234568", got)
	}
	if got := round4(1.00005); got != 1.0001 {
		t.Fatalf("round4(1.00005) = %v, want 1.0001", got)
	}
	if got := round4(1.00004999); got != 1 {
		t.Fatalf("round4(1.00004999) = %v, want 1", got)
	}
}

func TestRankMixedTieLevels(t *testing.T) {
	ranking, err := Rank([]Aggregate{
		rankAggregate("top", 90, 20, 20, 20),
		rankAggregate("rq", 80, 18, 15, 15),
		rankAggregate("val", 80, 16, 20, 15),
		rankAggregate("pres", 80, 16, 16, 18),
		rankAggregate("tie1", 80, 16, 16, 15),
		rankAggregate("tie2", 80, 16, 16, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, len(ranking.Entries))
	labels := map[string]string{}
	for _, e := range ranking.Entries {
		order = append(order, e.SnapshotSHA)
		labels[e.SnapshotSHA] = e.TieBreak
	}
	want := []string{"top", "rq", "val", "pres", "tie1", "tie2"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if labels["val"] != "result_quality" || labels["pres"] != "value" || labels["tie1"] != "presentation_demo_qa" {
		t.Fatalf("tie-break labels = %v", labels)
	}
	if len(ranking.Unresolved) != 2 || ranking.Unresolved[0] != "tie1" || ranking.Unresolved[1] != "tie2" {
		t.Fatalf("unresolved = %v, want [tie1 tie2]", ranking.Unresolved)
	}
	for i, e := range ranking.Entries {
		if e.Rank != i+1 {
			t.Fatalf("entry %d rank = %d", i, e.Rank)
		}
	}
}

func TestCompareEntriesEpsilonBoundaries(t *testing.T) {
	mk := func(total, rq float64) RankEntry {
		return RankEntry{MeanTotal: total, CriterionMeans: map[string]float64{"result_quality": rq}}
	}
	if order, level := compareEntries(mk(80, 15), mk(80+epsilon/2, 15)); order != 0 || level != "" {
		t.Fatalf("totals within epsilon must tie, got %d %q", order, level)
	}
	if order, level := compareEntries(mk(80+2*epsilon, 15), mk(80, 15)); order != -1 || level != "" {
		t.Fatalf("a total above epsilon must decide, got %d %q", order, level)
	}
	if order, level := compareEntries(mk(80, 15+2*epsilon), mk(80, 15)); order != -1 || level != "result_quality" {
		t.Fatalf("a criterion above epsilon must decide, got %d %q", order, level)
	}
	if order, level := compareEntries(mk(80, 15), mk(80, 15+epsilon/2)); order != 0 || level != "" {
		t.Fatalf("criterion values within epsilon must tie, got %d %q", order, level)
	}
	if order, level := compareEntries(mk(80, 15+2*epsilon), mk(80-epsilon/2, 15)); order != -1 || level != "result_quality" {
		t.Fatalf("a tied total must fall through to the criterion, got %d %q", order, level)
	}
}

func TestApplyCommissionValidationErrors(t *testing.T) {
	ranking, err := Rank([]Aggregate{
		rankAggregate("a", 80, 16, 20, 15),
		rankAggregate("b", 80, 16, 20, 15),
		rankAggregate("c", 80, 16, 20, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranking.Unresolved) != 3 {
		t.Fatalf("unresolved = %v, want three entries", ranking.Unresolved)
	}

	if _, err := ApplyCommission(Ranking{Entries: ranking.Entries}, Commission{
		Members: []string{"j1", "j2", "j3"},
		Votes:   map[string]int{"a": 3},
	}); err == nil {
		t.Fatal("a ranking without an unresolved tie must fail")
	}
	if _, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", "j1", "j2"},
		Votes:   map[string]int{"a": 1, "b": 1, "c": 1},
	}); err == nil {
		t.Fatal("a duplicate member must fail")
	}
	if _, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", " ", "j2"},
		Votes:   map[string]int{"a": 1, "b": 1, "c": 1},
	}); err == nil {
		t.Fatal("a blank member must fail")
	}
	if _, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", "j2", "j3"},
		Votes:   map[string]int{"a": 1, "b": 1, "c": 1, "d": 0},
	}); err == nil {
		t.Fatal("a vote for a non-tied snapshot must fail")
	}
	if _, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", "j2", "j3"},
		Votes:   map[string]int{"a": -1, "b": 2, "c": 2},
	}); err == nil {
		t.Fatal("negative votes must fail")
	}
	if _, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", "j2", "j3"},
		Votes:   map[string]int{"a": 2, "b": 1},
	}); err == nil {
		t.Fatal("a missing vote count must fail")
	}
	if _, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", "j2", "j3"},
		Votes:   map[string]int{"a": 1, "b": 1, "c": 1},
	}); err == nil {
		t.Fatal("a non-strict majority must fail")
	}

	decided, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", "j2", "j3"},
		Votes:   map[string]int{"a": 2, "b": 1, "c": 0},
		Grounds: "a demonstrates the mandatory scenario end to end",
	})
	if err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, len(decided.Entries))
	for _, e := range decided.Entries {
		order = append(order, e.SnapshotSHA)
	}
	if strings.Join(order, ",") != "a,b,c" {
		t.Fatalf("commission order = %v, want [a b c]", order)
	}
	for i, e := range decided.Entries {
		if e.Rank != i+1 || e.TieBreak != "commission" {
			t.Fatalf("entry %d = %+v", i, e)
		}
	}
	if decided.Commission == nil || len(decided.Unresolved) != 0 {
		t.Fatalf("commission not recorded: %+v", decided)
	}
}

func TestApplyCommissionKeepsDecidedEntriesFirst(t *testing.T) {
	ranking, err := Rank([]Aggregate{
		rankAggregate("top", 90, 20, 20, 20),
		rankAggregate("a", 80, 16, 20, 15),
		rankAggregate("b", 80, 16, 20, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranking.Unresolved) != 2 {
		t.Fatalf("unresolved = %v, want [a b]", ranking.Unresolved)
	}
	decided, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", "j2", "j3"},
		Votes:   map[string]int{"a": 3, "b": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, len(decided.Entries))
	for _, e := range decided.Entries {
		order = append(order, e.SnapshotSHA)
	}
	if strings.Join(order, ",") != "top,a,b" {
		t.Fatalf("commission order = %v, want [top a b]", order)
	}
}

func TestCalibrateValidationAndRounding(t *testing.T) {
	run1 := Aggregate{ValidJudges: 3, MeanTotal: 80, SnapshotSHA: "sha"}
	run2 := Aggregate{ValidJudges: 3, MeanTotal: 83.456, SnapshotSHA: "sha"}
	cal, err := Calibrate(run1, run2, 0.5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cal.MinValid != DefaultMinValidJudges {
		t.Fatalf("minValid = %d, want the default", cal.MinValid)
	}
	if cal.RetestDelta != 3.46 {
		t.Fatalf("retest delta = %v, want 3.46", cal.RetestDelta)
	}
	if cal.NoiseFloor != 6.91 {
		t.Fatalf("noise floor = %v, want 6.91", cal.NoiseFloor)
	}
	if cal.Run1Judges != 3 || cal.Run2Judges != 3 || cal.Run1Mean != 80 || cal.Run2Mean != 83.456 {
		t.Fatalf("calibration runs not recorded: %+v", cal)
	}
	if cal.SnapshotSHA != "sha" || cal.RubricVersion != RubricVersion {
		t.Fatalf("calibration provenance missing: %+v", cal)
	}

	if _, err := Calibrate(Aggregate{Inconclusive: true, ValidJudges: 3}, run2, 0.5, 3); err == nil {
		t.Fatal("an inconclusive run1 must fail calibration")
	}
	if _, err := Calibrate(run1, Aggregate{Inconclusive: true, ValidJudges: 3}, 0.5, 3); err == nil {
		t.Fatal("an inconclusive run2 must fail calibration")
	}
	if _, err := Calibrate(Aggregate{ValidJudges: 2}, run2, 0.5, 3); err == nil {
		t.Fatal("run1 below minValid must fail calibration")
	}
	if _, err := Calibrate(run1, Aggregate{ValidJudges: 2}, 0.5, 3); err == nil {
		t.Fatal("run2 below minValid must fail calibration")
	}
	if _, err := Calibrate(run1, run2, -1, 3); err == nil {
		t.Fatal("a negative min_delta must fail calibration")
	}
	if _, err := Calibrate(run1, run2, math.NaN(), 3); err == nil {
		t.Fatal("a NaN min_delta must fail calibration")
	}

	cal, err = Calibrate(
		Aggregate{ValidJudges: 3, MeanTotal: 80},
		Aggregate{ValidJudges: 3, MeanTotal: 80},
		0, 3,
	)
	if err != nil || cal.RetestDelta != 0 || cal.NoiseFloor != 0 {
		t.Fatalf("zero delta calibration = %+v err=%v", cal, err)
	}
}

func TestCompareEpsilonBoundaries(t *testing.T) {
	out := Compare(CompareInput{Mode: ModeTechnical, Candidate: 80 - epsilon/2, Best: 80, NoiseFloor: 0})
	if out.Decision != DecisionAccept {
		t.Fatalf("a sub-epsilon decrease must not roll back, got %s", out.Decision)
	}

	out = Compare(CompareInput{Mode: ModeJudge, Candidate: 78, Best: 80, NoiseFloor: 2, Target: 90})
	if out.Decision != DecisionStop {
		t.Fatalf("a regression exactly at the noise floor must stop, got %s", out.Decision)
	}

	out = Compare(CompareInput{Mode: ModeJudge, Candidate: 80 - 2 - 2*epsilon, Best: 80, NoiseFloor: 2, Target: 90})
	if out.Decision != DecisionRollback {
		t.Fatalf("a regression above the noise floor must roll back, got %s", out.Decision)
	}

	out = Compare(CompareInput{Mode: ModeJudge, Candidate: 82, Best: 80, NoiseFloor: 2, Target: 90})
	if out.Decision != DecisionAccept {
		t.Fatalf("an improvement exactly at the noise floor must be accepted, got %s", out.Decision)
	}

	out = Compare(CompareInput{Mode: ModeTechnical, Candidate: 95 - epsilon/2, Best: 80, Target: 95, NoiseFloor: 0.5})
	if out.Decision != DecisionReached {
		t.Fatalf("a candidate within epsilon of the target must count as reached, got %s", out.Decision)
	}

	out = Compare(CompareInput{Candidate: 79, Best: 80, Target: 90})
	if out.Decision != DecisionRollback || out.Mode != ModeTechnical {
		t.Fatalf("an unknown mode must default to technical, got %s/%s", out.Mode, out.Decision)
	}
}

func TestApplyCommissionKeepsTiePosition(t *testing.T) {
	ranking, err := Rank([]Aggregate{
		rankAggregate("a", 90, 18, 20, 15),
		rankAggregate("b", 80, 16, 20, 15),
		rankAggregate("c", 80, 16, 20, 15),
		rankAggregate("d", 70, 14, 20, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	decided, err := ApplyCommission(ranking, Commission{
		Members: []string{"j1", "j2", "j3"},
		Votes:   map[string]int{"b": 2, "c": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, len(decided.Entries))
	ranks := map[string]int{}
	breaks := map[string]string{}
	for _, e := range decided.Entries {
		order = append(order, e.SnapshotSHA)
		ranks[e.SnapshotSHA] = e.Rank
		breaks[e.SnapshotSHA] = e.TieBreak
	}
	if strings.Join(order, ",") != "a,b,c,d" {
		t.Fatalf("commission order = %v, want [a b c d]", order)
	}
	if ranks["a"] != 1 || ranks["b"] != 2 || ranks["c"] != 3 || ranks["d"] != 4 {
		t.Fatalf("ranks = %v, want a=1 b=2 c=3 d=4", ranks)
	}
	if breaks["b"] != "commission" || breaks["c"] != "commission" {
		t.Fatalf("tie breaks = %v", breaks)
	}
}
