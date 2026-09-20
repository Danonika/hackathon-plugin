// Package evaluation implements the deterministic parts of the Hackathon Agent Kit
// evaluation subsystem: judge-report validation and aggregation, judge
// calibration and loop decisions. The qualitative scoring itself stays with
// the judge skills; this package only does the arithmetic and the rules.
package evaluation

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// RubricVersion identifies the aggregation rules. Every stored result must
// carry it so scores from different rubric versions are never compared.
const RubricVersion = "1.0.0"

// MinValidJudges is the number of valid judge reports required before a run
// may be used as a stop condition: a majority of the three parallel judges, so
// one invalid or conflicted report does not void the run.
const MinValidJudges = 2

type Criterion struct {
	Key string  `json:"key"`
	Max float64 `json:"max"`
}

// JudgeCriteria are the fixed Demo Day weights from the official rubric.
var JudgeCriteria = []Criterion{
	{Key: "value", Max: 25},
	{Key: "result_quality", Max: 20},
	{Key: "innovation", Max: 15},
	{Key: "scalability", Max: 20},
	{Key: "presentation_demo_qa", Max: 20},
}

// TechnicalCriteria are the internal project-cost weights.
var TechnicalCriteria = []Criterion{
	{Key: "case_and_functionality", Max: 20},
	{Key: "technical_implementation", Max: 25},
	{Key: "readme_and_docs", Max: 20},
	{Key: "reproducibility", Max: 20},
	{Key: "reliability_and_security", Max: 15},
}

func criterionMax(criteria []Criterion, key string) (float64, bool) {
	for _, c := range criteria {
		if c.Key == key {
			return c.Max, true
		}
	}
	return 0, false
}

type Evidence struct {
	Criterion string `json:"criterion"`
	Finding   string `json:"finding"`
	Source    string `json:"source"`
}

type Deduction struct {
	Criterion string  `json:"criterion"`
	Points    float64 `json:"points"`
	Reason    string  `json:"reason"`
}

// JudgeReport is the per-judge output contract from judge/references/rubric.md.
type JudgeReport struct {
	JudgeID            string             `json:"judge_id"`
	SnapshotSHA        string             `json:"snapshot_sha"`
	ConflictOfInterest bool               `json:"conflict_of_interest"`
	Scores             map[string]float64 `json:"scores"`
	Total              float64            `json:"total"`
	Evidence           []Evidence         `json:"evidence"`
	Deductions         []Deduction        `json:"deductions"`
	Questions          []string           `json:"questions"`
	TopImprovements    []string           `json:"top_improvements"`
}

type InvalidReport struct {
	Index   int      `json:"index"`
	JudgeID string   `json:"judge_id,omitempty"`
	Path    string   `json:"path,omitempty"`
	Reasons []string `json:"reasons"`
}

type DeductionGroup struct {
	Criterion string  `json:"criterion"`
	Reason    string  `json:"reason"`
	Count     int     `json:"count"`
	Points    float64 `json:"points"`
}

type Counted struct {
	Item  string `json:"item"`
	Count int    `json:"count"`
}

// Aggregate is the deterministic result of one judge run.
type Aggregate struct {
	SnapshotSHA      string             `json:"snapshot_sha,omitempty"`
	ValidJudges      int                `json:"valid_judges"`
	Inconclusive     bool               `json:"inconclusive"`
	MeanTotal        float64            `json:"mean_total"`
	CriterionMeans   map[string]float64 `json:"criterion_means"`
	ScoreVariance    float64            `json:"score_variance"`
	HighDisagreement []string           `json:"high_disagreement"`
	CommonDeductions []DeductionGroup   `json:"common_deductions"`
	LikelyQuestions  []Counted          `json:"likely_questions"`
	TopImprovements  []Counted          `json:"top_improvements"`
	DroppedInvalid   []InvalidReport    `json:"dropped_invalid"`
	DroppedConflict  []string           `json:"dropped_conflict"`
}

// ValidateJudgeReport checks one report against the fixed rubric. It returns
// human-readable problems; an empty slice means the report is valid.
func ValidateJudgeReport(r JudgeReport, snapshotSHA string) []string {
	var problems []string
	if strings.TrimSpace(r.JudgeID) == "" {
		problems = append(problems, "missing judge_id")
	}
	if strings.TrimSpace(r.SnapshotSHA) == "" {
		problems = append(problems, "missing snapshot_sha")
	} else if snapshotSHA != "" && r.SnapshotSHA != snapshotSHA {
		problems = append(problems, fmt.Sprintf("snapshot_sha %q does not match bundle %q", r.SnapshotSHA, snapshotSHA))
	}
	sum := 0.0
	for _, c := range JudgeCriteria {
		v, ok := r.Scores[c.Key]
		if !ok {
			problems = append(problems, "missing score for "+c.Key)
			continue
		}
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > c.Max {
			problems = append(problems, fmt.Sprintf("score for %s out of range: %v (max %v)", c.Key, v, c.Max))
			continue
		}
		sum += v
	}
	unknown := make([]string, 0, len(r.Scores))
	for key := range r.Scores {
		if _, ok := criterionMax(JudgeCriteria, key); !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		problems = append(problems, "unknown criterion "+key)
	}
	if r.Total != 0 && math.Abs(r.Total-sum) > 0.01 {
		problems = append(problems, fmt.Sprintf("total %v does not match criterion sum %v", r.Total, sum))
	}
	if len(r.Evidence) == 0 {
		problems = append(problems, "no evidence entries")
	}
	for _, d := range r.Deductions {
		if _, ok := criterionMax(JudgeCriteria, d.Criterion); !ok {
			problems = append(problems, "deduction for unknown criterion "+d.Criterion)
		}
		if strings.TrimSpace(d.Reason) == "" {
			problems = append(problems, "deduction without a reason")
		}
		if d.Points < 0 || math.IsNaN(d.Points) {
			problems = append(problems, "deduction with negative points")
		}
	}
	return problems
}

// AggregateReports validates, filters and aggregates judge reports. Reports
// with a different snapshot sha, a malformed schema or a conflict of interest
// are dropped. Fewer than MinValidJudges valid reports marks the run
// inconclusive instead of guessing.
func AggregateReports(reports []JudgeReport, snapshotSHA string) Aggregate {
	agg := Aggregate{
		SnapshotSHA:      snapshotSHA,
		CriterionMeans:   map[string]float64{},
		HighDisagreement: []string{},
		CommonDeductions: []DeductionGroup{},
		LikelyQuestions:  []Counted{},
		TopImprovements:  []Counted{},
		DroppedInvalid:   []InvalidReport{},
		DroppedConflict:  []string{},
	}
	valid := make([]JudgeReport, 0, len(reports))
	for i, r := range reports {
		if problems := ValidateJudgeReport(r, snapshotSHA); len(problems) > 0 {
			agg.DroppedInvalid = append(agg.DroppedInvalid, InvalidReport{
				Index:   i,
				JudgeID: r.JudgeID,
				Reasons: problems,
			})
			continue
		}
		if r.ConflictOfInterest {
			agg.DroppedConflict = append(agg.DroppedConflict, r.JudgeID)
			continue
		}
		valid = append(valid, r)
	}
	agg.ValidJudges = len(valid)
	if len(valid) < MinValidJudges {
		agg.Inconclusive = true
	}
	if len(valid) == 0 {
		return agg
	}

	totals := make([]float64, len(valid))
	for i, r := range valid {
		totals[i] = reportTotal(r)
	}
	agg.MeanTotal = round2(average(totals))
	agg.ScoreVariance = round4(sampleVariance(totals))
	for _, c := range JudgeCriteria {
		values := make([]float64, len(valid))
		for i, r := range valid {
			values[i] = r.Scores[c.Key]
		}
		agg.CriterionMeans[c.Key] = round2(average(values))
	}
	agg.HighDisagreement = highDisagreement(valid)
	agg.CommonDeductions = groupDeductions(valid)
	agg.LikelyQuestions = countItems(questions(valid))
	agg.TopImprovements = countItems(improvements(valid))
	return agg
}

func reportTotal(r JudgeReport) float64 {
	if r.Total != 0 {
		return r.Total
	}
	sum := 0.0
	for _, c := range JudgeCriteria {
		sum += r.Scores[c.Key]
	}
	return sum
}

// highDisagreement returns the criteria whose per-criterion sample variance is
// in the top quartile of the five criteria. Rounded values with zero variance
// are never reported.
func highDisagreement(valid []JudgeReport) []string {
	type ranked struct {
		key      string
		variance float64
	}
	list := make([]ranked, 0, len(JudgeCriteria))
	for _, c := range JudgeCriteria {
		values := make([]float64, len(valid))
		for i, r := range valid {
			values[i] = r.Scores[c.Key]
		}
		list = append(list, ranked{key: c.Key, variance: sampleVariance(values)})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].variance != list[j].variance {
			return list[i].variance > list[j].variance
		}
		return list[i].key < list[j].key
	})
	quartile := int(math.Ceil(float64(len(list)) / 4.0))
	if quartile < 1 {
		quartile = 1
	}
	out := []string{}
	for i := 0; i < quartile && i < len(list); i++ {
		if list[i].variance > 0.0001 {
			out = append(out, list[i].key)
		}
	}
	sort.Strings(out)
	return out
}

func groupDeductions(valid []JudgeReport) []DeductionGroup {
	type group struct {
		criterion string
		reason    string
		count     int
		points    float64
	}
	groups := map[string]*group{}
	order := []string{}
	for _, r := range valid {
		for _, d := range r.Deductions {
			key := d.Criterion + "\x00" + normalizeText(d.Reason)
			g, ok := groups[key]
			if !ok {
				g = &group{criterion: d.Criterion, reason: strings.Join(strings.Fields(d.Reason), " ")}
				groups[key] = g
				order = append(order, key)
			}
			g.count++
			g.points += d.Points
		}
	}
	out := make([]DeductionGroup, 0, len(order))
	for _, key := range order {
		g := groups[key]
		out = append(out, DeductionGroup{
			Criterion: g.criterion,
			Reason:    g.reason,
			Count:     g.count,
			Points:    round2(g.points),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Criterion != out[j].Criterion {
			return out[i].Criterion < out[j].Criterion
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func questions(valid []JudgeReport) []string {
	out := []string{}
	for _, r := range valid {
		out = append(out, r.Questions...)
	}
	return out
}

func improvements(valid []JudgeReport) []string {
	out := []string{}
	for _, r := range valid {
		out = append(out, r.TopImprovements...)
	}
	return out
}

// countItems groups identical strings (case-insensitive, whitespace-collapsed)
// and returns them ordered by count desc, then text asc.
func countItems(items []string) []Counted {
	type group struct {
		item  string
		count int
	}
	groups := map[string]*group{}
	for _, item := range items {
		item = strings.Join(strings.Fields(item), " ")
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		g, ok := groups[key]
		if !ok {
			g = &group{item: item}
			groups[key] = g
		}
		g.count++
	}
	out := make([]Counted, 0, len(groups))
	for _, g := range groups {
		out = append(out, Counted{Item: g.item, Count: g.count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Item < out[j].Item
	})
	return out
}

// Calibration is the test-retest result that turns a score delta into a
// meaningful stop condition.
type Calibration struct {
	SnapshotSHA string  `json:"snapshot_sha,omitempty"`
	Run1Judges  int     `json:"run1_judges"`
	Run2Judges  int     `json:"run2_judges"`
	Run1Mean    float64 `json:"run1_mean"`
	Run2Mean    float64 `json:"run2_mean"`
	RetestDelta float64 `json:"retest_delta"`
	MinDelta    float64 `json:"min_delta"`
	NoiseFloor  float64 `json:"noise_floor"`
}

// Calibrate computes retest_delta and noise_floor from two runs over the same
// frozen snapshot: noise_floor = max(min_delta, 2 * retest_delta).
func Calibrate(run1, run2 Aggregate, minDelta float64) (Calibration, error) {
	if run1.Inconclusive || run2.Inconclusive || run1.ValidJudges < MinValidJudges || run2.ValidJudges < MinValidJudges {
		return Calibration{}, fmt.Errorf("calibration needs at least %d valid reports per run (run1=%d, run2=%d)",
			MinValidJudges, run1.ValidJudges, run2.ValidJudges)
	}
	if minDelta < 0 || math.IsNaN(minDelta) {
		return Calibration{}, fmt.Errorf("min_delta must be >= 0, got %v", minDelta)
	}
	if run1.SnapshotSHA != "" && run2.SnapshotSHA != "" && run1.SnapshotSHA != run2.SnapshotSHA {
		return Calibration{}, fmt.Errorf("calibration runs must score the same snapshot (%q vs %q)", run1.SnapshotSHA, run2.SnapshotSHA)
	}
	delta := math.Abs(run1.MeanTotal - run2.MeanTotal)
	return Calibration{
		SnapshotSHA: run1.SnapshotSHA,
		Run1Judges:  run1.ValidJudges,
		Run2Judges:  run2.ValidJudges,
		Run1Mean:    run1.MeanTotal,
		Run2Mean:    run2.MeanTotal,
		RetestDelta: round2(delta),
		MinDelta:    minDelta,
		NoiseFloor:  round2(math.Max(minDelta, 2*delta)),
	}, nil
}

// Mode selects the loop the comparison belongs to.
type Mode string

const (
	ModeTechnical Mode = "technical"
	ModeJudge     Mode = "judge"
)

// Decision is the deterministic outcome of one loop comparison.
type Decision string

const (
	DecisionReached  Decision = "target_reached"
	DecisionAccept   Decision = "accept"
	DecisionRollback Decision = "rollback"
	DecisionStop     Decision = "stop"
)

type CompareInput struct {
	Mode                    Mode
	Candidate               float64
	Best                    float64
	Target                  float64
	NoiseFloor              float64
	MandatoryScenarioFailed bool
}

type Comparison struct {
	Decision        Decision `json:"decision"`
	Reason          string   `json:"reason"`
	Mode            Mode     `json:"mode"`
	Candidate       float64  `json:"candidate"`
	Best            float64  `json:"best"`
	Target          float64  `json:"target"`
	NoiseFloor      float64  `json:"noise_floor"`
	Improvement     float64  `json:"improvement"`
	MandatoryFailed bool     `json:"mandatory_scenario_failed"`
}

const epsilon = 1e-9

// Compare applies the loop rules from self-improvement-controller:
//   - mandatory scenario failure always rolls back and stops;
//   - technical loop: any score decrease rolls back;
//   - judge loop: only a regression above the noise floor rolls back;
//   - reaching the target accepts and exits;
//   - an improvement below the noise floor stops further work on this axis.
func Compare(in CompareInput) Comparison {
	mode := in.Mode
	if mode != ModeJudge {
		mode = ModeTechnical
	}
	improvement := round2(in.Candidate - in.Best)
	out := Comparison{
		Mode:            mode,
		Candidate:       round2(in.Candidate),
		Best:            round2(in.Best),
		Target:          round2(in.Target),
		NoiseFloor:      round2(in.NoiseFloor),
		Improvement:     improvement,
		MandatoryFailed: in.MandatoryScenarioFailed,
	}
	if in.MandatoryScenarioFailed {
		out.Decision = DecisionRollback
		out.Reason = "mandatory scenario failed; rollback and stop"
		return out
	}
	if mode == ModeTechnical {
		if in.Candidate < in.Best-epsilon {
			out.Decision = DecisionRollback
			out.Reason = fmt.Sprintf("score decreased from %v to %v; rollback", out.Best, out.Candidate)
			return out
		}
	} else {
		regression := in.Best - in.Candidate
		if regression > in.NoiseFloor+epsilon {
			out.Decision = DecisionRollback
			out.Reason = fmt.Sprintf("judge regression %v exceeds noise floor %v; rollback", round2(regression), out.NoiseFloor)
			return out
		}
	}
	if in.Target > 0 && in.Candidate >= in.Target-epsilon {
		out.Decision = DecisionReached
		out.Reason = fmt.Sprintf("target %v reached with %v", out.Target, out.Candidate)
		return out
	}
	if improvement < in.NoiseFloor-epsilon {
		out.Decision = DecisionStop
		out.Reason = fmt.Sprintf("improvement %v below noise floor %v; stop iterating on this axis", improvement, out.NoiseFloor)
		return out
	}
	out.Decision = DecisionAccept
	out.Reason = fmt.Sprintf("accepted with improvement %v above noise floor %v", improvement, out.NoiseFloor)
	return out
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func sampleVariance(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := average(values)
	sum := 0.0
	for _, v := range values {
		d := v - mean
		sum += d * d
	}
	return sum / float64(len(values)-1)
}

func normalizeText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
