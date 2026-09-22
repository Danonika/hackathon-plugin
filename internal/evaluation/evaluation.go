// Package evaluation implements the deterministic parts of the Hackathon Agent Kit
// evaluation subsystem: judge-report validation and aggregation, judge
// calibration and loop decisions. The qualitative scoring itself stays with
// the judge skills; this package only does the arithmetic and the rules.
package evaluation

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// RubricVersion identifies the aggregation rules. Every stored result must
// carry it so scores from different rubric versions are never compared.
// 2.0.0: regulation 9.5 requires at least three valid non-conflicted scores
// (missing scores are never zero) and ranking follows the 9.6 tie-break order.
const RubricVersion = "2.0.0"

// DefaultMinValidJudges is the fallback minimum number of valid judge reports
// when the caller passes no configured value (configs/evaluation.yaml
// min_valid_judges defaults to 3, matching regulation 9.5).
const DefaultMinValidJudges = 3

type Criterion struct {
	Key string  `json:"key"`
	Max float64 `json:"max"`
}

// judgeCriteria are the fixed Demo Day weights from the official rubric.
// Unexported: callers must not mutate the rubric in place.
var judgeCriteria = []Criterion{
	{Key: "value", Max: 25},
	{Key: "result_quality", Max: 20},
	{Key: "innovation", Max: 15},
	{Key: "scalability", Max: 20},
	{Key: "presentation_demo_qa", Max: 20},
}

func criterionKnown(criteria []Criterion, key string) bool {
	for _, c := range criteria {
		if c.Key == key {
			return true
		}
	}
	return false
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

	// TotalSet records whether the JSON carried a "total" field, so an omitted
	// total stays tolerated while an explicit total of 0 is validated.
	TotalSet bool `json:"-"`
}

func (r *JudgeReport) UnmarshalJSON(data []byte) error {
	type alias JudgeReport
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var probe struct {
		Total *float64 `json:"total"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && probe.Total != nil {
		decoded.TotalSet = true
	}
	*r = JudgeReport(decoded)
	return nil
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

// Aggregate is the deterministic result of one judge run. MeanTotal and
// CriterionMeans stay unrounded for ranking and comparisons; the *Rounded
// fields are for publication (regulation 9.5 allows two decimals in the
// published ranking only).
type Aggregate struct {
	SnapshotSHA           string             `json:"snapshot_sha,omitempty"`
	RubricVersion         string             `json:"rubric_version"`
	MinValid              int                `json:"min_valid_judges"`
	ValidJudges           int                `json:"valid_judges"`
	Inconclusive          bool               `json:"inconclusive"`
	MeanTotal             float64            `json:"mean_total"`
	MeanTotalRounded      float64            `json:"mean_total_rounded"`
	CriterionMeans        map[string]float64 `json:"criterion_means"`
	CriterionMeansRounded map[string]float64 `json:"criterion_means_rounded"`
	ScoreVariance         float64            `json:"score_variance"`
	HighDisagreement      []string           `json:"high_disagreement"`
	CommonDeductions      []DeductionGroup   `json:"common_deductions"`
	LikelyQuestions       []Counted          `json:"likely_questions"`
	TopImprovements       []Counted          `json:"top_improvements"`
	DroppedInvalid        []InvalidReport    `json:"dropped_invalid"`
	DroppedConflict       []string           `json:"dropped_conflict"`
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
	for _, c := range judgeCriteria {
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
		if !criterionKnown(judgeCriteria, key) {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		problems = append(problems, "unknown criterion "+key)
	}
	if (r.TotalSet || r.Total != 0) && math.Abs(r.Total-sum) > 0.01 {
		problems = append(problems, fmt.Sprintf("total %v does not match criterion sum %v", r.Total, sum))
	}
	if len(r.Evidence) == 0 {
		problems = append(problems, "no evidence entries")
	}
	for _, d := range r.Deductions {
		if !criterionKnown(judgeCriteria, d.Criterion) {
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
// are dropped; a missing report is never counted as zero. Fewer than minValid
// valid reports marks the run inconclusive and requires substitute passes
// (regulation 9.5).
func AggregateReports(reports []JudgeReport, snapshotSHA string, minValid int) Aggregate {
	if minValid < 1 {
		minValid = DefaultMinValidJudges
	}
	agg := Aggregate{
		SnapshotSHA:           snapshotSHA,
		RubricVersion:         RubricVersion,
		MinValid:              minValid,
		CriterionMeans:        map[string]float64{},
		CriterionMeansRounded: map[string]float64{},
		HighDisagreement:      []string{},
		CommonDeductions:      []DeductionGroup{},
		LikelyQuestions:       []Counted{},
		TopImprovements:       []Counted{},
		DroppedInvalid:        []InvalidReport{},
		DroppedConflict:       []string{},
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
	if len(valid) < minValid {
		agg.Inconclusive = true
	}
	if len(valid) == 0 {
		return agg
	}

	totals := make([]float64, len(valid))
	for i, r := range valid {
		totals[i] = reportTotal(r)
	}
	agg.MeanTotal = round6(average(totals))
	agg.MeanTotalRounded = round2(agg.MeanTotal)
	agg.ScoreVariance = round4(sampleVariance(totals))
	for _, c := range judgeCriteria {
		values := make([]float64, len(valid))
		for i, r := range valid {
			values[i] = r.Scores[c.Key]
		}
		mean := round6(average(values))
		agg.CriterionMeans[c.Key] = mean
		agg.CriterionMeansRounded[c.Key] = round2(mean)
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
	for _, c := range judgeCriteria {
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
	list := make([]ranked, 0, len(judgeCriteria))
	for _, c := range judgeCriteria {
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
	SnapshotSHA   string  `json:"snapshot_sha,omitempty"`
	RubricVersion string  `json:"rubric_version"`
	MinValid      int     `json:"min_valid_judges"`
	Run1Judges    int     `json:"run1_judges"`
	Run2Judges    int     `json:"run2_judges"`
	Run1Mean      float64 `json:"run1_mean"`
	Run2Mean      float64 `json:"run2_mean"`
	RetestDelta   float64 `json:"retest_delta"`
	MinDelta      float64 `json:"min_delta"`
	NoiseFloor    float64 `json:"noise_floor"`
}

// Calibrate computes retest_delta and noise_floor from two runs over the same
// frozen snapshot: noise_floor = max(min_delta, 2 * retest_delta).
func Calibrate(run1, run2 Aggregate, minDelta float64, minValid int) (Calibration, error) {
	if minValid < 1 {
		minValid = DefaultMinValidJudges
	}
	if run1.Inconclusive || run2.Inconclusive || run1.ValidJudges < minValid || run2.ValidJudges < minValid {
		return Calibration{}, fmt.Errorf("calibration needs at least %d valid reports per run (run1=%d, run2=%d); assign substitute judges",
			minValid, run1.ValidJudges, run2.ValidJudges)
	}
	if minDelta < 0 || math.IsNaN(minDelta) {
		return Calibration{}, fmt.Errorf("min_delta must be >= 0, got %v", minDelta)
	}
	if run1.SnapshotSHA != run2.SnapshotSHA {
		return Calibration{}, fmt.Errorf("calibration runs must score the same snapshot (%q vs %q)", run1.SnapshotSHA, run2.SnapshotSHA)
	}
	delta := math.Abs(run1.MeanTotal - run2.MeanTotal)
	return Calibration{
		SnapshotSHA:   run1.SnapshotSHA,
		RubricVersion: RubricVersion,
		MinValid:      minValid,
		Run1Judges:    run1.ValidJudges,
		Run2Judges:    run2.ValidJudges,
		Run1Mean:      run1.MeanTotal,
		Run2Mean:      run2.MeanTotal,
		RetestDelta:   round2(delta),
		MinDelta:      minDelta,
		NoiseFloor:    round2(math.Max(minDelta, 2*delta)),
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
	TargetSet               bool
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
	InputError      string   `json:"input_error,omitempty"`
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
	// Decisions use unrounded values (regulation 9.5: the unrounded mean
	// determines the place); rounding is display-only.
	if math.IsNaN(in.NoiseFloor) || math.IsInf(in.NoiseFloor, 0) || in.NoiseFloor < 0 {
		// Fail closed: an invalid floor must not disable the regression guard.
		in.NoiseFloor = 0
	}
	improvement := in.Candidate - in.Best
	out := Comparison{
		Mode:            mode,
		Candidate:       round2(in.Candidate),
		Best:            round2(in.Best),
		Target:          round2(in.Target),
		NoiseFloor:      round2(in.NoiseFloor),
		Improvement:     round2(improvement),
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
	if (in.TargetSet || in.Target > 0) && in.Candidate >= in.Target-epsilon {
		out.Decision = DecisionReached
		out.Reason = fmt.Sprintf("target %v reached with %v", out.Target, out.Candidate)
		return out
	}
	if improvement < in.NoiseFloor-epsilon {
		out.Decision = DecisionStop
		out.Reason = fmt.Sprintf("improvement %v below noise floor %v; stop iterating on this axis", round2(improvement), out.NoiseFloor)
		return out
	}
	out.Decision = DecisionAccept
	out.Reason = fmt.Sprintf("accepted with improvement %v above noise floor %v", round2(improvement), out.NoiseFloor)
	return out
}

// tieBreakOrder is the regulation 9.6 order applied when total scores are
// equal: result quality, then value, then presentation/demo/Q&A.
var tieBreakOrder = []string{"result_quality", "value", "presentation_demo_qa"}

type RankEntry struct {
	SnapshotSHA        string             `json:"snapshot_sha"`
	Rank               int                `json:"rank"`
	MeanTotal          float64            `json:"mean_total"`
	MeanTotalPublished float64            `json:"mean_total_published"`
	CriterionMeans     map[string]float64 `json:"criterion_means,omitempty"`
	ValidJudges        int                `json:"valid_judges"`
	TieBreak           string             `json:"tie_break,omitempty"`
}

// Commission records the regulation 9.6 vote of three non-conflicted jury
// members when the published criteria still leave a tie.
type Commission struct {
	Members []string       `json:"members"`
	Votes   map[string]int `json:"votes"`
	Grounds string         `json:"grounds"`
}

type Ranking struct {
	Entries       []RankEntry `json:"entries"`
	TieBreakOrder []string    `json:"tie_break_order"`
	Unresolved    []string    `json:"tie_unresolved,omitempty"`
	Commission    *Commission `json:"commission,omitempty"`
}

// Rank orders aggregates by the regulation 9.6 rules: the unrounded mean total
// first, then the criterion averages in tieBreakOrder; a remaining tie is
// flagged as unresolved until ApplyCommission records the jury vote.
func Rank(aggregates []Aggregate) (Ranking, error) {
	if len(aggregates) == 0 {
		return Ranking{}, fmt.Errorf("rank needs at least one aggregate")
	}
	for _, agg := range aggregates {
		if agg.Inconclusive {
			minValid := agg.MinValid
			if minValid < 1 {
				minValid = DefaultMinValidJudges
			}
			return Ranking{}, fmt.Errorf("aggregate %s is inconclusive (%d of %d valid judges); assign substitute judges before ranking",
				agg.SnapshotSHA, agg.ValidJudges, minValid)
		}
		if agg.RubricVersion == "" {
			return Ranking{}, fmt.Errorf("aggregate %s has no rubric version; re-aggregate with this build", agg.SnapshotSHA)
		}
		if agg.RubricVersion != RubricVersion {
			return Ranking{}, fmt.Errorf("aggregate %s uses rubric version %s, expected %s", agg.SnapshotSHA, agg.RubricVersion, RubricVersion)
		}
	}

	entries := make([]RankEntry, 0, len(aggregates))
	for _, agg := range aggregates {
		entries = append(entries, RankEntry{
			SnapshotSHA:        agg.SnapshotSHA,
			MeanTotal:          agg.MeanTotal,
			MeanTotalPublished: agg.MeanTotalRounded,
			CriterionMeans:     agg.CriterionMeans,
			ValidJudges:        agg.ValidJudges,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		order, _ := compareEntries(entries[i], entries[j])
		if order != 0 {
			return order < 0
		}
		return entries[i].SnapshotSHA < entries[j].SnapshotSHA
	})

	ranking := Ranking{Entries: entries, TieBreakOrder: append([]string{}, tieBreakOrder...)}
	for i := 1; i < len(entries); i++ {
		order, level := compareEntries(entries[i-1], entries[i])
		if order == 0 {
			if !containsString(ranking.Unresolved, entries[i-1].SnapshotSHA) {
				ranking.Unresolved = append(ranking.Unresolved, entries[i-1].SnapshotSHA)
			}
			if !containsString(ranking.Unresolved, entries[i].SnapshotSHA) {
				ranking.Unresolved = append(ranking.Unresolved, entries[i].SnapshotSHA)
			}
			continue
		}
		if level != "" {
			entries[i].TieBreak = level
		}
	}
	for i := range entries {
		entries[i].Rank = i + 1
	}
	sort.Strings(ranking.Unresolved)
	return ranking, nil
}

// ApplyCommission validates and records the 9.6 tie-break vote: at least three
// non-conflicted members, one vote each, a strict majority for the winner.
func ApplyCommission(ranking Ranking, commission Commission) (Ranking, error) {
	if len(ranking.Unresolved) == 0 {
		return Ranking{}, fmt.Errorf("no unresolved tie to decide")
	}
	if len(commission.Members) < 3 {
		return Ranking{}, fmt.Errorf("commission needs at least 3 non-conflicted members")
	}
	seen := map[string]bool{}
	for _, member := range commission.Members {
		if strings.TrimSpace(member) == "" {
			return Ranking{}, fmt.Errorf("commission members must be non-empty")
		}
		if seen[member] {
			return Ranking{}, fmt.Errorf("duplicate commission member %s", member)
		}
		seen[member] = true
	}
	total := 0
	for sha, votes := range commission.Votes {
		if !containsString(ranking.Unresolved, sha) {
			return Ranking{}, fmt.Errorf("vote for %s which is not part of the unresolved tie", sha)
		}
		if votes < 0 {
			return Ranking{}, fmt.Errorf("negative votes for %s", sha)
		}
		total += votes
	}
	for _, sha := range ranking.Unresolved {
		if _, ok := commission.Votes[sha]; !ok {
			return Ranking{}, fmt.Errorf("missing vote count for %s", sha)
		}
	}
	if total != len(commission.Members) {
		return Ranking{}, fmt.Errorf("votes (%d) must equal the number of commission members (%d)", total, len(commission.Members))
	}
	winner, max := "", 0
	for sha, votes := range commission.Votes {
		if votes > max {
			winner, max = sha, votes
		}
	}
	if winner == "" || max*2 <= len(commission.Members) {
		return Ranking{}, fmt.Errorf("no strict majority in commission votes %v", commission.Votes)
	}

	unresolvedSet := map[string]bool{}
	for _, sha := range ranking.Unresolved {
		unresolvedSet[sha] = true
	}
	// Resolve each contiguous tie group in place: entries are score-sorted, so
	// distinct tie groups at different score levels are handled separately.
	rest := make([]RankEntry, 0, len(ranking.Entries))
	for i := 0; i < len(ranking.Entries); {
		if !unresolvedSet[ranking.Entries[i].SnapshotSHA] {
			rest = append(rest, ranking.Entries[i])
			i++
			continue
		}
		run := []RankEntry{}
		for i < len(ranking.Entries) && unresolvedSet[ranking.Entries[i].SnapshotSHA] {
			run = append(run, ranking.Entries[i])
			i++
		}
		sort.SliceStable(run, func(a, b int) bool {
			votesA, votesB := commission.Votes[run[a].SnapshotSHA], commission.Votes[run[b].SnapshotSHA]
			if votesA != votesB {
				return votesA > votesB
			}
			return run[a].SnapshotSHA < run[b].SnapshotSHA
		})
		for _, entry := range run {
			entry.TieBreak = "commission"
			rest = append(rest, entry)
		}
	}
	for i := range rest {
		rest[i].Rank = i + 1
	}
	ranking.Entries = rest
	ranking.Unresolved = nil
	ranking.Commission = &commission
	return ranking, nil
}

// compareEntries returns -1/0/1 and the tie-break level that decided the order.
func compareEntries(a, b RankEntry) (int, string) {
	if math.Abs(a.MeanTotal-b.MeanTotal) > epsilon {
		if a.MeanTotal > b.MeanTotal {
			return -1, ""
		}
		return 1, ""
	}
	for _, key := range tieBreakOrder {
		av, bv := a.CriterionMeans[key], b.CriterionMeans[key]
		if math.Abs(av-bv) > epsilon {
			if av > bv {
				return -1, key
			}
			return 1, key
		}
	}
	return 0, ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func round6(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
