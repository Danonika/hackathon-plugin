// Command fly-bench measures the Fly router against a deterministic keyword
// baseline on a task suite and stores the A/B evidence under evals/fly-ab/.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/openai"
	"hackathonkit.local/agentkit/internal/quality"
	"hackathonkit.local/agentkit/internal/routing/fly"
)

type taskRow struct {
	Task     string         `json:"task"`
	Expected []string       `json:"expected"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type rowResult struct {
	Task            string   `json:"task"`
	Expected        []string `json:"expected"`
	BaselineTop1    string   `json:"baseline_top1"`
	FlyTop1         string   `json:"fly_top1"`
	BaselineRecall  float64  `json:"baseline_recall"`
	FlyRecall       float64  `json:"fly_recall"`
	Agreement       bool     `json:"agreement"`
	Deterministic   bool     `json:"deterministic"`
	BaselineLatency float64  `json:"baseline_latency_ms"`
	FlyLatency      float64  `json:"fly_latency_ms"`
	FlyEncoder      string   `json:"fly_encoder"`
}

type summary struct {
	Tasks             int     `json:"tasks"`
	BaselineRecallAtK float64 `json:"baseline_recall_at_k"`
	FlyRecallAtK      float64 `json:"fly_recall_at_k"`
	AgreementRate     float64 `json:"agreement_rate"`
	DeterminismRate   float64 `json:"determinism_rate"`
	BaselineLatencyMS float64 `json:"baseline_latency_mean_ms"`
	FlyLatencyMS      float64 `json:"fly_latency_mean_ms"`
	TopK              int     `json:"top_k"`
}

type report struct {
	Determinism string      `json:"determinism,omitempty"`
	SHA         string      `json:"sha"`
	GeneratedAt time.Time   `json:"generated_at"`
	Suite       string      `json:"suite"`
	Summary     summary     `json:"summary"`
	Rows        []rowResult `json:"rows"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fly-bench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	suite := fs.String("suite", "configs/fly-tasks.jsonl", "task suite JSONL")
	repoRoot := fs.String("root", "", "repository root")
	jsonOnly := fs.Bool("json", false, "print the report JSON only")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	root := *repoRoot
	if root == "" {
		root = config.DiscoverRoot()
	}
	config.LoadDotEnv(root)
	mgr := config.NewManager(root)
	cfg := mgr.Config()

	rows, skipped, err := loadSuiteWithSkips(filepath.Join(root, filepath.FromSlash(*suite)))
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "error: suite has no tasks")
		return 1
	}
	if len(skipped) > 0 {
		fmt.Fprintf(stderr, "fly-bench: %d suite line(s) skipped: %s\n", len(skipped), strings.Join(skipped, "; "))
	}

	// Bound the whole run so a stalled provider cannot make the benchmark hang
	// indefinitely; the per-call HTTP timeout alone does not cap the total.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	baseline := fly.NewRouter(cfg.Routing)
	full := fly.NewRouter(cfg.Routing)
	oa := openai.New(cfg.OpenAI)
	embedder := fly.NewChainEmbedder(oa, fly.NewHashEncoder(512))
	full.WithMemory(filepath.Join(root, "data", "index", "fly-memory.json"))
	full.WithBrain(filepath.Join(root, "data", "index", "fly-brain.json"))
	full.WithLLM(oa)
	full.WithEmbeddings(embedder, filepath.Join(root, "data", "index", "fly-embeddings.json"), cfg.OpenAI.EmbeddingModel+"+hash512")

	// Warm the lazy caches (centroids, brain, memory) before timing so the
	// first row does not absorb their build cost. Verify the embedding path
	// actually engaged, otherwise a silent hash fallback would be published as
	// if the full pipeline had been measured.
	warmup := full.Recommend(ctx, "warmup", nil, nil)
	// The encoder label says "embedding" even when ChainEmbedder silently fell
	// back to the hash encoder, so probe the primary directly to catch a degraded
	// run instead of trusting the label.
	embeddingDegraded := false
	primaryErr := ""
	if embedder.Primary != nil {
		if _, err := embedder.Primary.Embed(ctx, []string{"warmup"}); err != nil {
			embeddingDegraded = true
			primaryErr = err.Error()
		}
	} else {
		embeddingDegraded = true
		primaryErr = "no primary embedder configured"
	}
	if embeddingDegraded || !strings.Contains(warmup.Encoder, "embedding") {
		fmt.Fprintf(stderr, "fly-bench: warning: embedding path did not engage (encoder=%q, primary error=%q); semantic signal may be degraded\n", warmup.Encoder, primaryErr)
	}

	results := make([]rowResult, 0, len(rows))
	// Normalize the effective k the way fly.NewRouter does (default 5 when the
	// configured value is not positive), so recall@k and the reported top_k
	// describe the top-k the router actually applied.
	topK := cfg.Routing.TopK
	if topK <= 0 {
		topK = 5
	}
	for _, row := range rows {
		// A shared deadline that expires mid-loop would make the remaining
		// full.Recommend calls fail fast and be recorded as zero-recall rows
		// while the keyword baseline keeps returning results, so abort instead of
		// publishing skewed partial A/B evidence.
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "error: benchmark deadline exceeded before all rows completed; refusing to publish partial A/B evidence")
			return 1
		}
		baselineStart := time.Now()
		baselineRes := baseline.Recommend(ctx, row.Task, row.Metadata, nil)
		baselineLatency := float64(time.Since(baselineStart).Microseconds()) / 1000

		flyStart := time.Now()
		flyRes := full.Recommend(ctx, row.Task, row.Metadata, nil)
		flyLatency := float64(time.Since(flyStart).Microseconds()) / 1000

		again := full.Recommend(ctx, row.Task, row.Metadata, nil)

		result := rowResult{
			Task:            row.Task,
			Expected:        row.Expected,
			BaselineTop1:    top1(baselineRes.Recommendations),
			FlyTop1:         top1(flyRes.Recommendations),
			BaselineRecall:  recall(row.Expected, baselineRes.Recommendations, topK),
			FlyRecall:       recall(row.Expected, flyRes.Recommendations, topK),
			Agreement:       len(baselineRes.Recommendations) > 0 && len(flyRes.Recommendations) > 0 && top1(baselineRes.Recommendations) == top1(flyRes.Recommendations),
			Deterministic:   len(flyRes.Recommendations) > 0 && sameOrder(flyRes.Recommendations, again.Recommendations),
			BaselineLatency: baselineLatency,
			FlyLatency:      flyLatency,
			FlyEncoder:      flyRes.Encoder,
		}
		results = append(results, result)
	}

	engaged := 0
	for _, r := range results {
		if r.FlyTop1 != "" {
			engaged++
		}
	}
	if engaged == 0 {
		fmt.Fprintln(stderr, "error: the Fly pipeline produced no recommendations; refusing to publish A/B evidence")
		return 1
	}
	rep := report{
		SHA:         quality.GitSHA(root),
		GeneratedAt: time.Now().UTC(),
		Suite:       *suite,
		Summary:     summarize(results, topK),
		Determinism: "warm router, full ranked order compared across repeats (caches primed before the loop)",
		Rows:        results,
	}
	path, err := save(root, rep)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if *jsonOnly {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "fly-bench: %d tasks (top_k=%d)\n", rep.Summary.Tasks, rep.Summary.TopK)
	fmt.Fprintf(stdout, "  baseline recall@k: %.2f  fly recall@k: %.2f\n", rep.Summary.BaselineRecallAtK, rep.Summary.FlyRecallAtK)
	fmt.Fprintf(stdout, "  top-1 agreement: %.0f%%  determinism: %.0f%%\n", rep.Summary.AgreementRate*100, rep.Summary.DeterminismRate*100)
	fmt.Fprintf(stdout, "  latency: baseline %.1fms  fly %.1fms\n", rep.Summary.BaselineLatencyMS, rep.Summary.FlyLatencyMS)
	fmt.Fprintf(stdout, "  evidence: %s\n", path)
	return 0
}

func loadSuite(path string) ([]taskRow, error) {
	rows, _, err := loadSuiteWithSkips(path)
	return rows, err
}

// loadSuiteWithSkips also reports lines that parsed but lacked a task, so a
// renamed field cannot silently shrink the suite.
func loadSuiteWithSkips(path string) ([]taskRow, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("suite %s: %w", path, err)
	}
	defer file.Close()
	rows := []taskRow{}
	skipped := []string{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var row taskRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, skipped, fmt.Errorf("suite line %d: %w", lineNumber, err)
		}
		if row.Task == "" {
			skipped = append(skipped, fmt.Sprintf("line %d: missing task field", lineNumber))
			continue
		}
		rows = append(rows, row)
	}
	return rows, skipped, scanner.Err()
}

func top1(recs []fly.Recommendation) string {
	if len(recs) == 0 {
		return ""
	}
	return recs[0].Capability
}

// sameOrder reports whether two recommendation lists are identical, including
// their full ranked order (not just the top-1 capability).
func sameOrder(a, b []fly.Recommendation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Capability != b[i].Capability {
			return false
		}
	}
	return true
}

func recall(expected []string, recs []fly.Recommendation, k int) float64 {
	if len(expected) == 0 {
		return 0
	}
	if k <= 0 || k > len(recs) {
		k = len(recs)
	}
	found := 0
	for _, want := range expected {
		for i := 0; i < k; i++ {
			if recs[i].Capability == want {
				found++
				break
			}
		}
	}
	return round2(float64(found) / float64(len(expected)))
}

func summarize(rows []rowResult, topK int) summary {
	if len(rows) == 0 {
		return summary{TopK: topK}
	}
	out := summary{Tasks: len(rows), TopK: topK}
	for _, row := range rows {
		out.BaselineRecallAtK += row.BaselineRecall
		out.FlyRecallAtK += row.FlyRecall
		out.BaselineLatencyMS += row.BaselineLatency
		out.FlyLatencyMS += row.FlyLatency
		if row.Agreement {
			out.AgreementRate++
		}
		if row.Deterministic {
			out.DeterminismRate++
		}
	}
	n := float64(len(rows))
	out.BaselineRecallAtK = round2(out.BaselineRecallAtK / n)
	out.FlyRecallAtK = round2(out.FlyRecallAtK / n)
	out.AgreementRate = round2(out.AgreementRate / n)
	out.DeterminismRate = round2(out.DeterminismRate / n)
	out.BaselineLatencyMS = round2(out.BaselineLatencyMS / n)
	out.FlyLatencyMS = round2(out.FlyLatencyMS / n)
	return out
}

func save(root string, rep report) (string, error) {
	dir := filepath.Join(root, "evals", "fly-ab")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.json", rep.SHA, rep.GeneratedAt.UTC().Format("20060102T150405.000000000")))
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "fly-ab-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path, nil
	}
	return filepath.ToSlash(rel), nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
