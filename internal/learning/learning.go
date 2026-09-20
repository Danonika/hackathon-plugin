// Package learning implements durable, confidence-scored learnings
// ("instincts"): small trigger/insight pairs recorded from completed tasks and
// injected back into future sessions. Learnings are project-scoped by default
// and can be promoted to global scope after repeated confirmation.
package learning

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// MinConfidence and MaxConfidence bound the confidence score.
	MinConfidence = 0.3
	MaxConfidence = 0.9
	maxEntries    = 500
	maxEvidence   = 10
	fileVersion   = 1
)

// Embedder is the minimal embedding surface shared with the Fly router.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// Learning is one durable trigger/insight pair.
type Learning struct {
	ID           string    `json:"id"`
	Trigger      string    `json:"trigger"`
	Insight      string    `json:"insight"`
	Scope        string    `json:"scope"`
	Project      string    `json:"project,omitempty"`
	Confidence   float64   `json:"confidence"`
	Observations int       `json:"observations"`
	Successes    int       `json:"successes"`
	Failures     int       `json:"failures"`
	Evidence     []string  `json:"evidence,omitempty"`
	Vector       []float64 `json:"vector,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Match is a recall hit with its relevance score.
type Match struct {
	Learning
	Score float64 `json:"score"`
}

type RecordInput struct {
	Trigger  string
	Insight  string
	Outcome  string
	Scope    string
	Evidence []string
}

type RecordResult struct {
	Learning Learning `json:"learning"`
	Created  bool     `json:"created"`
}

type fileFormat struct {
	Version   int        `json:"version"`
	Project   string     `json:"project,omitempty"`
	Learnings []Learning `json:"learnings"`
}

// Store is a mutex-guarded JSON-backed learning store.
type Store struct {
	path     string
	project  string
	embedder Embedder
	now      func() time.Time

	mu sync.Mutex
}

func NewStore(path, project string) *Store {
	return &Store{path: path, project: project, now: time.Now}
}

func (s *Store) WithEmbedder(e Embedder) *Store {
	s.embedder = e
	return s
}

func (s *Store) WithClock(now func() time.Time) *Store {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Store) Path() string { return s.path }

// ProjectKey derives a stable short key for a repository root.
func ProjectKey(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha1.Sum([]byte(filepath.ToSlash(abs)))
	return "p-" + hex.EncodeToString(sum[:4])
}

// Record inserts or reinforces a learning. A repeated trigger+insight pair
// updates confidence instead of creating a duplicate.
func (s *Store) Record(ctx context.Context, in RecordInput) (RecordResult, error) {
	trigger := strings.Join(strings.Fields(in.Trigger), " ")
	insight := strings.Join(strings.Fields(in.Insight), " ")
	if trigger == "" || insight == "" {
		return RecordResult{}, fmt.Errorf("trigger and insight are required")
	}
	outcome := strings.ToLower(strings.TrimSpace(in.Outcome))
	if outcome == "" {
		outcome = "success"
	}
	if outcome != "success" && outcome != "failure" && outcome != "neutral" {
		return RecordResult{}, fmt.Errorf("invalid outcome %q: expected success, failure or neutral", in.Outcome)
	}
	scope := strings.ToLower(strings.TrimSpace(in.Scope))
	if scope == "" {
		scope = "project"
	}
	if scope != "project" && scope != "global" {
		return RecordResult{}, fmt.Errorf("invalid scope %q: expected project or global", in.Scope)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := s.load()
	if err != nil {
		return RecordResult{}, err
	}
	now := s.now().UTC()
	key := normalizeKey(trigger, insight)
	result := RecordResult{Created: true}
	for i := range doc.Learnings {
		if normalizeKey(doc.Learnings[i].Trigger, doc.Learnings[i].Insight) != key {
			continue
		}
		l := &doc.Learnings[i]
		l.Observations++
		switch outcome {
		case "success":
			l.Successes++
		case "failure":
			l.Failures++
		}
		l.Evidence = mergeEvidence(l.Evidence, in.Evidence)
		l.Confidence = confidence(l.Successes, l.Failures)
		l.UpdatedAt = now
		result.Created = false
		result.Learning = *l
		break
	}
	if result.Created {
		l := Learning{
			ID:           "l-" + shortHash(key),
			Trigger:      trigger,
			Insight:      insight,
			Scope:        scope,
			Project:      s.project,
			Observations: 1,
			Successes:    boolToInt(outcome == "success"),
			Failures:     boolToInt(outcome == "failure"),
			Evidence:     mergeEvidence(nil, in.Evidence),
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		l.Confidence = confidence(l.Successes, l.Failures)
		doc.Learnings = append(doc.Learnings, l)
		result.Learning = l
	}
	if s.embedder != nil {
		if vector, err := s.embed(ctx, trigger+" "+insight); err == nil {
			for i := range doc.Learnings {
				if doc.Learnings[i].ID == result.Learning.ID {
					doc.Learnings[i].Vector = vector
					result.Learning.Vector = vector
					break
				}
			}
		}
	}
	doc.Learnings = evict(doc.Learnings, maxEntries)
	if err := s.save(doc); err != nil {
		return RecordResult{}, err
	}
	return result, nil
}

// Recall returns the most relevant learnings for a query, filtered by scope
// and minimum confidence. Semantic similarity is used when vectors exist,
// keyword overlap otherwise.
func (s *Store) Recall(ctx context.Context, query string, limit int, minConfidence float64, scope string) ([]Match, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "project"
	}

	s.mu.Lock()
	doc, err := s.load()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}

	var queryVector []float64
	if s.embedder != nil {
		if vector, err := s.embed(ctx, query); err == nil {
			queryVector = vector
		}
	}
	queryTokens := tokenize(query)

	matches := []Match{}
	for _, l := range doc.Learnings {
		if !s.inScope(l, scope) {
			continue
		}
		if l.Confidence < minConfidence {
			continue
		}
		similarity := keywordSimilarity(queryTokens, tokenize(l.Trigger+" "+l.Insight))
		if len(queryVector) > 0 && len(l.Vector) == len(queryVector) {
			similarity = cosine01(queryVector, l.Vector)
		}
		matches = append(matches, Match{
			Learning: l,
			Score:    round4(0.6*similarity + 0.4*l.Confidence),
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		if matches[i].Confidence != matches[j].Confidence {
			return matches[i].Confidence > matches[j].Confidence
		}
		return matches[i].ID < matches[j].ID
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

// Top returns the highest-confidence learnings eligible for session injection.
func (s *Store) Top(limit int, minConfidence float64) ([]Learning, error) {
	if limit <= 0 {
		limit = 6
	}
	s.mu.Lock()
	doc, err := s.load()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := []Learning{}
	for _, l := range doc.Learnings {
		if l.Confidence < minConfidence || !s.inScope(l, "project") {
			continue
		}
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Promote moves a project learning to global scope when it has been observed
// often enough and is confident enough.
func (s *Store) Promote(id string, minObservations int, minConfidence float64) (Learning, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Learning{}, false, fmt.Errorf("id is required")
	}
	if minObservations < 1 {
		minObservations = 1
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := s.load()
	if err != nil {
		return Learning{}, false, err
	}
	for i := range doc.Learnings {
		if doc.Learnings[i].ID != id {
			continue
		}
		l := &doc.Learnings[i]
		if l.Scope == "global" {
			return *l, false, nil
		}
		if l.Observations < minObservations || l.Confidence < minConfidence {
			return *l, false, nil
		}
		l.Scope = "global"
		l.Project = ""
		l.UpdatedAt = s.now().UTC()
		if err := s.save(doc); err != nil {
			return Learning{}, false, err
		}
		return *l, true, nil
	}
	return Learning{}, false, fmt.Errorf("learning not found: %s", id)
}

// Count returns the number of stored learnings.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return 0
	}
	return len(doc.Learnings)
}

func (s *Store) inScope(l Learning, scope string) bool {
	switch scope {
	case "all":
		return true
	case "global":
		return l.Scope == "global"
	default: // project
		return l.Scope == "global" || l.Project == s.project
	}
}

func (s *Store) embed(ctx context.Context, text string) ([]float64, error) {
	vectors, err := s.embedder.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	return normalize(vectors[0]), nil
}

func (s *Store) load() (fileFormat, error) {
	doc := fileFormat{Version: fileVersion, Project: s.project, Learnings: []Learning{}}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return doc, err
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fileFormat{}, fmt.Errorf("learning store %s: %w", s.path, err)
	}
	if doc.Learnings == nil {
		doc.Learnings = []Learning{}
	}
	return doc, nil
}

func (s *Store) save(doc fileFormat) error {
	doc.Version = fileVersion
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "learnings-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.path)
}

func confidence(successes, failures int) float64 {
	c := MinConfidence + 0.6*float64(successes)/float64(successes+failures+1)
	if c > MaxConfidence {
		c = MaxConfidence
	}
	if c < MinConfidence {
		c = MinConfidence
	}
	return round2(c)
}

func mergeEvidence(existing, incoming []string) []string {
	out := append([]string{}, existing...)
	for _, item := range incoming {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		dup := false
		for _, have := range out {
			if have == item {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, item)
		}
	}
	if len(out) > maxEvidence {
		out = out[len(out)-maxEvidence:]
	}
	return out
}

func evict(entries []Learning, limit int) []Learning {
	if len(entries) <= limit {
		return entries
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.Before(entries[j].UpdatedAt)
	})
	return entries[len(entries)-limit:]
}

func normalizeKey(trigger, insight string) string {
	return strings.ToLower(trigger) + "\x00" + strings.ToLower(insight)
}

func shortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:4])
}

func tokenize(text string) []string {
	out := []string{}
	for _, field := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && !(r >= 'а' && r <= 'я') && r != 'ё'
	}) {
		if len([]rune(field)) >= 3 {
			out = append(out, field)
		}
	}
	return out
}

func keywordSimilarity(query, doc []string) float64 {
	if len(query) == 0 {
		return 0
	}
	seen := map[string]bool{}
	for _, token := range doc {
		seen[token] = true
	}
	hits := 0
	for _, token := range query {
		if seen[token] {
			hits++
		}
	}
	return float64(hits) / float64(len(query))
}

func cosine01(a, b []float64) float64 {
	dot, na, nb := 0.0, 0.0, 0.0
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	cos := dot / (math.Sqrt(na) * math.Sqrt(nb))
	return math.Max(0, math.Min(1, (cos+1)/2))
}

func normalize(vector []float64) []float64 {
	norm := 0.0
	for _, v := range vector {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return vector
	}
	out := make([]float64, len(vector))
	for i, v := range vector {
		out[i] = v / norm
	}
	return out
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
