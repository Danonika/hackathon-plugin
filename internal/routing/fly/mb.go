package fly

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	MBKenyonCells   = 4133
	MBMBONCount     = 96
	MBPAMCount      = 307
	MBPPL1Count     = 24
	MBDefaultSparse = 0.06
	mbDopamineEta   = 0.05
	mbWeightLimit   = 5.0
)

type MBrain struct {
	Model      string               `json:"model"`
	Hash       string               `json:"hash"`
	KC         int                  `json:"kc"`
	MBON       int                  `json:"mbon"`
	Sparsity   float64              `json:"sparsity"`
	Seed       int64                `json:"seed"`
	Categories []string             `json:"categories"`
	Readout    map[string][]float64 `json:"readout"`
	Bias       map[string]float64   `json:"bias"`
	Dopamine   mbDopamine           `json:"dopamine"`
	Source     string               `json:"source"`
	UpdatedAt  string               `json:"updated_at"`
}

type mbDopamine struct {
	PAM  int `json:"pam"`
	PPL1 int `json:"ppl1"`
}

type DopamineStats struct {
	PAM   int `json:"pam"`
	PPL1  int `json:"ppl1"`
	Total int `json:"total"`
}

var (
	brainMu sync.Mutex
)

func (r *Router) WithBrain(path string) *Router {
	r.brainPath = path
	return r
}

func (r *Router) loadBrain() *MBrain {
	brainMu.Lock()
	if r.brain != nil {
		cached := r.brain
		brainMu.Unlock()
		return cached
	}
	path := r.brainPath
	brainMu.Unlock()
	if path == "" {
		return nil
	}
	// Read and parse outside the lock: a slow disk must not serialize every
	// Router in the process.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var brain MBrain
	if json.Unmarshal(data, &brain) != nil || brain.KC == 0 || len(brain.Readout) == 0 {
		return nil
	}
	brainMu.Lock()
	if r.brain == nil {
		r.brain = &brain
	}
	cached := r.brain
	brainMu.Unlock()
	return cached
}

// loadBrainLocked is the lock-free body of loadBrain for callers that already
// hold brainMu.
func (r *Router) loadBrainLocked() *MBrain {
	if r.brain != nil {
		return r.brain
	}
	if r.brainPath == "" {
		return nil
	}
	data, err := os.ReadFile(r.brainPath)
	if err != nil {
		return nil
	}
	var brain MBrain
	if json.Unmarshal(data, &brain) != nil || brain.KC == 0 || len(brain.Readout) == 0 {
		return nil
	}
	r.brain = &brain
	return r.brain
}

// clone deep-copies the brain so an update can be saved before it is published.
func (b *MBrain) clone() *MBrain {
	if b == nil {
		return nil
	}
	out := *b
	out.Categories = append([]string(nil), b.Categories...)
	out.Readout = make(map[string][]float64, len(b.Readout))
	for k, v := range b.Readout {
		out.Readout[k] = append([]float64(nil), v...)
	}
	out.Bias = make(map[string]float64, len(b.Bias))
	for k, v := range b.Bias {
		out.Bias[k] = v
	}
	return &out
}

func (r *Router) saveBrain(brain *MBrain) error {
	if r.brainPath == "" {
		return nil
	}
	brain.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Dir(r.brainPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(brain, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(r.brainPath), "fly-brain-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, r.brainPath); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func (r *Router) Dopamine() DopamineStats {
	brainMu.Lock()
	defer brainMu.Unlock()
	brain := r.loadBrainLocked()
	if brain == nil {
		return DopamineStats{}
	}
	return DopamineStats{
		PAM:   brain.Dopamine.PAM,
		PPL1:  brain.Dopamine.PPL1,
		Total: brain.Dopamine.PAM + brain.Dopamine.PPL1,
	}
}

func splitMix64(state *uint64) float64 {
	*state += 0x9e3779b97f4a7c15
	z := *state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return float64(z>>11)/float64(1<<53)*2 - 1
}

func (b *MBrain) activate(input []float64) []float64 {
	n := b.KC
	if n <= 0 {
		return nil
	}
	state := uint64(b.Seed)
	scale := 1.0 / math.Sqrt(float64(len(input)+1))
	activations := make([]float64, n)
	for i := 0; i < n; i++ {
		sum := 0.0
		for _, x := range input {
			sum += splitMix64(&state) * x
		}
		sum += splitMix64(&state)
		sum *= scale
		if sum < 0 {
			sum = 0
		}
		activations[i] = sum
	}
	sparsity := b.Sparsity
	if sparsity <= 0 || sparsity >= 1 {
		sparsity = MBDefaultSparse
	}
	k := int(float64(n) * sparsity)
	if k < 1 {
		k = 1
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return activations[order[i]] > activations[order[j]] })
	code := make([]float64, n)
	norm := 0.0
	for i := 0; i < k; i++ {
		idx := order[i]
		code[idx] = activations[idx]
		norm += activations[idx]
	}
	if norm > 0 {
		for i := 0; i < k; i++ {
			idx := order[i]
			code[idx] /= norm
		}
	}
	return code
}

func (b *MBrain) scores(input []float64) (map[string]float64, []float64, bool) {
	code := b.activate(input)
	if code == nil {
		return nil, nil, false
	}
	logits := map[string]float64{}
	best := math.Inf(-1)
	for _, category := range b.Categories {
		weights := b.Readout[category]
		if len(weights) != len(code) {
			// A short/long readout is a corrupt brain, not a category to skip.
			return nil, nil, false
		}
		sum := b.Bias[category]
		for i, v := range code {
			if v != 0 {
				sum += weights[i] * v
			}
		}
		logits[category] = sum
		if sum > best {
			best = sum
		}
	}
	if len(logits) == 0 {
		return nil, nil, false
	}
	total := 0.0
	exp := map[string]float64{}
	for category, v := range logits {
		e := math.Exp(v - best)
		exp[category] = e
		total += e
	}
	out := map[string]float64{}
	for category, e := range exp {
		out[category] = e / total
	}
	return out, code, true
}

func (r *Router) mbCategoryScores(ctx context.Context, task string) (map[string]float64, bool) {
	brain := r.loadBrain()
	if brain == nil || r.embedder == nil {
		return nil, false
	}
	vec, ok := r.embedTask(ctx, task)
	if !ok {
		return nil, false
	}
	brainMu.Lock()
	scores, _, ok := brain.scores(vec)
	brainMu.Unlock()
	return scores, ok
}

func (r *Router) applyDopamine(ctx context.Context, ev FeedbackEvent) bool {
	if r.embedder == nil {
		return false
	}
	// Embed outside the lock: the embedder can be a network client.
	vec, ok := r.embedTask(ctx, ev.Task)
	if !ok {
		return false
	}
	// Hold brainMu across the whole read-modify-persist-publish sequence so two
	// concurrent feedback calls cannot lose an update or overwrite the file
	// with a stale snapshot.
	brainMu.Lock()
	defer brainMu.Unlock()
	base := r.loadBrainLocked()
	if base == nil {
		return false
	}
	work := base.clone()
	_, code, ok := work.scores(vec)
	if !ok || code == nil {
		return false
	}
	reward := map[string]bool{}
	for _, category := range categoriesForCapabilities(ev.Chosen) {
		reward[category] = true
	}
	punish := map[string]bool{}
	for _, rec := range ev.Recommended {
		category := capabilityToCategory(rec.Capability)
		if category != "" && !reward[category] {
			punish[category] = true
		}
	}
	eta := mbDopamineEta
	switch ev.Source {
	case "accepted", "normal-teacher":
		for category := range reward {
			work.adjust(category, code, eta)
		}
		work.Dopamine.PAM++
	case "codex-correction", "fallback":
		for category := range punish {
			work.adjust(category, code, -eta)
		}
		for category := range reward {
			work.adjust(category, code, eta/2)
		}
		work.Dopamine.PPL1++
	default:
		return false
	}
	if err := r.saveBrain(work); err != nil {
		// In-memory state is unchanged: the caller sees a real failure.
		return false
	}
	r.brain = work
	return true
}

func (b *MBrain) adjust(category string, code []float64, eta float64) {
	weights, ok := b.Readout[category]
	if !ok || len(weights) < len(code) {
		return
	}
	for i, v := range code {
		if v == 0 {
			continue
		}
		updated := weights[i] + eta*v
		if updated > mbWeightLimit {
			updated = mbWeightLimit
		}
		if updated < -mbWeightLimit {
			updated = -mbWeightLimit
		}
		weights[i] = updated
	}
	b.Readout[category] = weights
}

func (b *MBrain) Train(input []float64, category string, eta float64) {
	if b.Readout == nil {
		b.Readout = map[string][]float64{}
	}
	if b.Bias == nil {
		b.Bias = map[string]float64{}
	}
	if len(b.Categories) == 0 || category == "" {
		return
	}
	if _, ok := b.Readout[category]; !ok {
		b.Readout[category] = make([]float64, b.KC)
		known := false
		for _, existing := range b.Categories {
			if existing == category {
				known = true
				break
			}
		}
		if !known {
			b.Categories = append(b.Categories, category)
		}
	}
	code := b.activate(input)
	if code == nil {
		return
	}
	b.adjust(category, code, eta)
	b.Bias[category] += eta * 0.1
	if ranking := b.Rank(input); len(ranking) > 0 && ranking[0] != category {
		b.adjust(ranking[0], code, -eta/2)
	}
}

func (b *MBrain) Rank(input []float64) []string {
	scores, _, ok := b.scores(input)
	if !ok {
		return nil
	}
	ranking := make([]string, 0, len(scores))
	for category := range scores {
		ranking = append(ranking, category)
	}
	sort.Slice(ranking, func(i, j int) bool { return scores[ranking[i]] > scores[ranking[j]] })
	return ranking
}

func (b *MBrain) SaveTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func NewMBrain(kc int, sparsity float64, categories []string, seed int64) *MBrain {
	if kc <= 0 {
		kc = MBKenyonCells
	}
	brain := &MBrain{
		Model:      "mushroom-body",
		KC:         kc,
		MBON:       MBMBONCount,
		Sparsity:   sparsity,
		Seed:       seed,
		Categories: categories,
		Readout:    map[string][]float64{},
		Bias:       map[string]float64{},
		Source:     "FlyWire v783 structure via lixiang1076/fly-brain (KC 4133, MBON 96, PAM 307, PPL1 24)",
	}
	for _, category := range categories {
		brain.Readout[category] = make([]float64, kc)
	}
	return brain
}
