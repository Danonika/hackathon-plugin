package fly

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
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
	memoryMu            sync.Mutex
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
		if os.IsNotExist(err) {
			return memoryFile{}, nil
		}
		// A transient/permission error must be surfaced to the caller so a
		// feedback write aborts instead of replacing good data with an empty
		// memory.
		return memoryFile{}, fmt.Errorf("read fly memory %s: %w", r.memoryPath, err)
	}
	var mem memoryFile
	if json.Unmarshal(data, &mem) != nil {
		// Quarantine the corrupt file so the next save does not silently
		// overwrite recoverable data with an empty memory.
		if rerr := os.Rename(r.memoryPath, r.memoryPath+".corrupt"); rerr != nil {
			log.Printf("fly: cannot quarantine corrupt %s: %v", r.memoryPath, rerr)
		}
		return memoryFile{}, nil
	}
	return mem, nil
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
	memoryMu.Lock()
	defer memoryMu.Unlock()
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
		if vectors, err := r.embedder.Embed(ctx, []string{ev.Task}); err == nil && len(vectors) == 1 {
			example.Vector = normalizeVector(vectors[0])
		}
	}

	var kept []FeedbackExample
	saveErr := func() error {
		memoryMu.Lock()
		defer memoryMu.Unlock()

		mem, err := r.loadMemory()
		if err != nil {
			return err
		}
		if mem.Model != "" && mem.Model != r.modelTag {
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
		return len(kept), false, saveErr
	}
	learned := r.applyDopamine(ctx, ev)
	return len(kept), learned, nil
}

func (r *Router) memoryCategoryScores(taskVector []float64, k int) (map[string]float64, bool) {
	memoryMu.Lock()
	mem, err := r.loadMemory()
	memoryMu.Unlock()
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
