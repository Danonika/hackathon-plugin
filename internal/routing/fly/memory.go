package fly

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type FeedbackExample struct {
	Hash         string    `json:"hash"`
	Task         string    `json:"task"`
	Vector       []float64 `json:"vector,omitempty"`
	Categories   []string  `json:"categories"`
	Capabilities []string  `json:"capabilities"`
	Source       string    `json:"source"`
	CreatedAt    string    `json:"created_at"`
}

type memoryFile struct {
	Model    string            `json:"model"`
	Hash     string            `json:"hash"`
	Examples []FeedbackExample `json:"examples"`
}

type FeedbackEvent struct {
	Task          string
	Recommended   []Recommendation
	Chosen        []string
	Source        string
	InputMetadata map[string]any
}

var (
	capabilityIndexOnce sync.Once
	capabilityIndexMap  map[string][]string
)

func capabilityIndex() map[string][]string {
	capabilityIndexOnce.Do(func() {
		categories := make([]string, 0, len(categoryCapabilities))
		for category := range categoryCapabilities {
			categories = append(categories, category)
		}
		sort.Strings(categories)
		capabilityIndexMap = map[string][]string{}
		for _, category := range categories {
			for _, capability := range categoryCapabilities[category] {
				capabilityIndexMap[capability] = append(capabilityIndexMap[capability], category)
			}
		}
	})
	return capabilityIndexMap
}

func capabilityToCategory(capability string) string {
	categories := capabilityIndex()[capability]
	if len(categories) == 0 {
		return ""
	}
	return categories[0]
}

func categoriesForCapabilities(capabilities []string) []string {
	index := capabilityIndex()
	primary := []string{}
	ambiguous := []string{}
	for _, capability := range capabilities {
		categories := index[capability]
		switch len(categories) {
		case 0:
			continue
		case 1:
			primary = append(primary, categories[0])
		default:
			ambiguous = append(ambiguous, categories...)
		}
	}
	if len(primary) == 0 {
		primary = ambiguous
	}
	seen := map[string]bool{}
	out := []string{}
	for _, category := range primary {
		if seen[category] {
			continue
		}
		seen[category] = true
		out = append(out, category)
	}
	sort.Strings(out)
	return out
}

func hashTask(task string) string {
	sum := sha1.Sum([]byte(task))
	return hex.EncodeToString(sum[:8])
}

func (r *Router) WithMemory(path string) *Router {
	r.memoryPath = path
	return r
}

func (r *Router) loadMemory() (memoryFile, error) {
	if r.memoryPath == "" {
		return memoryFile{}, nil
	}
	data, err := os.ReadFile(r.memoryPath)
	if err != nil {
		if os.IsNotExist(err) && missingPathIsUsable(r.memoryPath) {
			return memoryFile{}, nil
		}
		// A transient/permission error must be surfaced to the caller so a
		// feedback write aborts instead of replacing good data with an empty
		// memory.
		return memoryFile{}, fmt.Errorf("read fly memory %s: %w", r.memoryPath, err)
	}
	var mem memoryFile
	if err := json.Unmarshal(data, &mem); err != nil {
		// Surface corruption instead of returning "no data": a read-only
		// accessor must not rename files as a side effect, and a write path
		// needs to distinguish corruption from an empty store.
		return memoryFile{}, &memoryCorruptError{path: r.memoryPath, err: err}
	}
	return mem, nil
}

// memoryCorruptError marks a memory file that exists but cannot be decoded.
type memoryCorruptError struct {
	path string
	err  error
}

func (e *memoryCorruptError) Error() string {
	return fmt.Sprintf("decode fly memory %s: %v", e.path, e.err)
}

func (e *memoryCorruptError) Unwrap() error { return e.err }

// missingPathIsUsable reports whether a path that reads as missing can still be
// created later. Windows maps ERROR_PATH_NOT_FOUND to IsNotExist when a path
// component is a file, so such an unreadable path must not be treated as an
// absent memory file.
func missingPathIsUsable(path string) bool {
	dir := filepath.Dir(path)
	for {
		info, err := os.Stat(dir)
		if err == nil {
			return info.IsDir()
		}
		if !os.IsNotExist(err) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return true
		}
		dir = parent
	}
}

func (r *Router) saveMemory(mem memoryFile) error {
	if r.memoryPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.memoryPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(r.memoryPath), "fly-memory-*.tmp")
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
	if err := os.Rename(tmpName, r.memoryPath); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func (r *Router) MemorySize() int {
	r.memoryMu.Lock()
	defer r.memoryMu.Unlock()
	mem, err := r.loadMemory()
	if err != nil {
		return 0
	}
	return len(mem.Examples)
}

func (r *Router) AddFeedback(ctx context.Context, ev FeedbackEvent) (int, bool, error) {
	example := FeedbackExample{
		Hash:         hashTask(ev.Task),
		Task:         ev.Task,
		Categories:   categoriesForCapabilities(ev.Chosen),
		Capabilities: ev.Chosen,
		Source:       ev.Source,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	// Embed before taking the lock: this can be a network call.
	if r.embedder != nil {
		vectors, err := r.embedder.Embed(ctx, []string{ev.Task})
		switch {
		case err != nil:
			log.Printf("fly: feedback embedding failed for %q: %v", example.Hash, err)
		case len(vectors) != 1:
			log.Printf("fly: feedback embedding returned %d vectors for 1 input", len(vectors))
		default:
			example.Vector = normalizeVector(vectors[0])
		}
	}

	var kept []FeedbackExample
	saveErr := func() error {
		r.memoryMu.Lock()
		defer r.memoryMu.Unlock()

		mem, err := r.loadMemory()
		if err != nil {
			var corrupt *memoryCorruptError
			if !errors.As(err, &corrupt) {
				return err
			}
			// Only the write path quarantines a corrupt file so the next save
			// does not overwrite recoverable data. A unique suffix keeps an
			// earlier quarantine from being silently replaced by a later one.
			backup := fmt.Sprintf("%s.corrupt.%d", r.memoryPath, time.Now().UnixNano())
			if rerr := os.Rename(r.memoryPath, backup); rerr != nil {
				return fmt.Errorf("quarantine corrupt fly memory %s: %w", r.memoryPath, rerr)
			}
			mem = memoryFile{}
		}
		if mem.Model != "" && r.modelTag != "" && mem.Model != r.modelTag {
			// Examples embedded under a different model are not comparable with
			// the current task vectors; drop the stale vectors (keyword-only
			// examples remain usable).
			pruned := make([]FeedbackExample, 0, len(mem.Examples))
			for _, existing := range mem.Examples {
				if len(existing.Vector) == 0 {
					pruned = append(pruned, existing)
				}
			}
			mem.Examples = pruned
		}
		mem.Model = r.modelTag
		kept = make([]FeedbackExample, 0, len(mem.Examples)+1)
		for _, existing := range mem.Examples {
			if existing.Hash != example.Hash {
				kept = append(kept, existing)
			}
		}
		kept = append(kept, example)
		const maxExamples = 500
		if len(kept) > maxExamples {
			kept = kept[len(kept)-maxExamples:]
		}
		mem.Examples = kept
		return r.saveMemory(mem)
	}()
	if saveErr != nil {
		// Nothing reached disk: report a zero count, not a success-shaped one.
		return 0, false, saveErr
	}
	learned := r.applyDopamine(ctx, ev)
	return len(kept), learned, nil
}

func (r *Router) memoryCategoryScores(taskVector []float64, k int) (map[string]float64, bool) {
	r.memoryMu.Lock()
	mem, err := r.loadMemory()
	r.memoryMu.Unlock()
	if err != nil || len(mem.Examples) == 0 {
		return nil, false
	}
	type scored struct {
		sim      float64
		category []string
	}
	list := []scored{}
	for _, example := range mem.Examples {
		if len(example.Vector) == 0 || len(example.Categories) == 0 {
			continue
		}
		if mem.Model != "" && r.modelTag != "" && mem.Model != r.modelTag {
			// Vectors from another embedding model are not comparable.
			continue
		}
		list = append(list, scored{cosine(taskVector, normalizeVector(example.Vector)), example.Categories})
	}
	if len(list) == 0 {
		return nil, false
	}
	sort.Slice(list, func(i, j int) bool { return list[i].sim > list[j].sim })
	if k <= 0 || k > len(list) {
		k = len(list)
	}
	votes := map[string]float64{}
	total := 0.0
	for _, item := range list[:k] {
		if item.sim <= 0 {
			continue
		}
		for _, category := range item.category {
			votes[category] += item.sim
		}
		total += item.sim
	}
	if total == 0 {
		return nil, false
	}
	for category := range votes {
		votes[category] /= total
	}
	return votes, true
}
