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
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	Learning       Learning `json:"learning"`
	Created        bool     `json:"created"`
	EmbeddingError string   `json:"embedding_error,omitempty"`
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

	mu      sync.Mutex // guards embedder and now
	writeMu sync.Mutex // serializes the load-modify-save sequence
}

func NewStore(path, project string) *Store {
	return &Store{path: path, project: project, now: time.Now}
}

// WithEmbedder configures the embedder. It is safe to call concurrently with
// store operations; the field is guarded by the store mutex.
func (s *Store) WithEmbedder(e Embedder) *Store {
	s.mu.Lock()
	s.embedder = e
	s.mu.Unlock()
	return s
}

// WithClock overrides the store clock. It is safe to call concurrently.
func (s *Store) WithClock(now func() time.Time) *Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Store) currentEmbedder() Embedder {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.embedder
}

func (s *Store) clock() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().UTC()
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

	// Embed before taking the write lock: the embedder can be a network client
	// and must not block other store operations while it runs.
	var vector []float64
	var embedErr error
	if s.currentEmbedder() != nil {
		if v, err := s.embed(ctx, trigger+" "+insight); err == nil {
			vector = v
		} else {
			// A failed embed must be observable: the entry is still stored,
			// but semantic recall degrades to keyword matching.
			embedErr = err
		}
	}

	// Serialize in-process writers first so they do not contend on the
	// cross-process lock file itself.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	unlock, err := s.lockFile()
	if err != nil {
		return RecordResult{}, err
	}
	defer unlock()

	doc, err := s.load()
	if err != nil {
		return RecordResult{}, err
	}
	now := s.clock()
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
		// Never silently downgrade a promoted global learning back to project
		// scope: a re-record defaults to "project" and would otherwise make the
		// entry invisible to every scope query.
		if scope == "global" || l.Scope != "global" {
			l.Scope = scope
			if scope == "global" {
				l.Project = ""
			}
		}
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
			Observations: 1,
			Successes:    boolToInt(outcome == "success"),
			Failures:     boolToInt(outcome == "failure"),
			Evidence:     mergeEvidence(nil, in.Evidence),
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		// Keep a single on-disk representation of global learnings: a global
		// entry must not carry a stale project tag.
		if scope == "project" {
			l.Project = s.project
		}
		l.Confidence = confidence(l.Successes, l.Failures)
		doc.Learnings = append(doc.Learnings, l)
		result.Learning = l
	}
	if vector != nil {
		for i := range doc.Learnings {
			if doc.Learnings[i].ID == result.Learning.ID {
				doc.Learnings[i].Vector = vector
				result.Learning.Vector = vector
				break
			}
		}
	}
	if embedErr != nil {
		result.EmbeddingError = embedErr.Error()
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

	doc, err := s.load()
	if err != nil {
		return nil, err
	}

	var queryVector []float64
	if s.currentEmbedder() != nil {
		if vector, err := s.embed(ctx, query); err == nil {
			queryVector = vector
		} else {
			// Semantic recall is degraded for this call; keyword overlap is used.
			log.Printf("learning: embedding failed, falling back to keyword recall: %v", err)
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
	doc, err := s.load()
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

	// Acquire the in-process write lock before the cross-process lock file,
	// matching Record, so the two entry points cannot deadlock through
	// lock-order inversion.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	unlock, err := s.lockFile()
	if err != nil {
		return Learning{}, false, err
	}
	defer unlock()

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
		l.UpdatedAt = s.clock()
		if err := s.save(doc); err != nil {
			return Learning{}, false, err
		}
		return *l, true, nil
	}
	return Learning{}, false, fmt.Errorf("learning not found: %s", id)
}

// Count returns the number of stored learnings.
func (s *Store) Count() int {
	doc, err := s.load()
	if err != nil {
		log.Printf("learning: count failed: %v", err)
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
	embedder := s.currentEmbedder()
	if embedder == nil {
		return nil, fmt.Errorf("no embedder configured")
	}
	vectors, err := embedder.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	return normalize(vectors[0]), nil
}

// lockFile serializes load-modify-save across processes sharing this path.
const lockStaleAfter = 30 * time.Second

// lockOwnerPID reads the pid recorded in an existing lock file, or 0 when it is
// absent/unreadable/malformed.
func lockOwnerPID(lockPath string) int {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// processAlive is implemented per platform in proc_unix.go / proc_windows.go.

func (s *Store) lockFile() (func(), error) {
	lockPath := s.path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	mine := os.Getpid()
	for i := 0; i < 50; i++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			// The pid record is load-bearing: unlock() only removes a lock whose
			// pid matches, so a failed write would leave an unremovable lock.
			if _, werr := fmt.Fprintf(f, "%d\n", mine); werr != nil {
				f.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("write learning lock %s: %w", lockPath, werr)
			}
			if cerr := f.Close(); cerr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("close learning lock %s: %w", lockPath, cerr)
			}
			return func() {
				// Only remove a lock this process still owns: it may have been
				// reclaimed as stale and re-acquired by a peer.
				if lockOwnerPID(lockPath) == mine {
					_ = os.Remove(lockPath)
				}
			}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// Reclaim a lock only when its owner process is actually gone, or when
		// no pid was recorded and the file is older than the stale window.
		// mtime alone would evict a slow-but-live holder.
		owner := lockOwnerPID(lockPath)
		reclaim := false
		if owner != 0 {
			reclaim = !processAlive(owner)
		} else if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			reclaim = true
		}
		if reclaim && reclaimLock(lockPath, owner) {
			continue
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, fmt.Errorf("learning store is locked by another process")
}

// reclaimLock removes a stale lock without deleting a peer's freshly created
// lock. It atomically detaches the inode, verifies the detached file still
// records the stale owner, and only then deletes it; if a peer re-created the
// lock between the liveness check and the rename, the detached inode is
// restored instead. This closes the remove-by-path TOCTOU window.
func reclaimLock(lockPath string, stalePID int) bool {
	detached := fmt.Sprintf("%s.stale.%d.%d", lockPath, os.Getpid(), time.Now().UnixNano())
	if err := os.Rename(lockPath, detached); err != nil {
		return false
	}
	if lockOwnerPID(detached) == stalePID {
		_ = os.Remove(detached)
		return true
	}
	if _, err := os.Lstat(lockPath); os.IsNotExist(err) {
		_ = os.Rename(detached, lockPath)
	} else {
		_ = os.Remove(detached)
	}
	return false
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

// WithoutVector returns a copy safe to serialize to tool output: the internal
// embedding vector is dropped so envelopes and logs stay small and do not leak
// model state.
func (l Learning) WithoutVector() Learning {
	l.Vector = nil
	return l
}
