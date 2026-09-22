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
	"sort"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/openai"
	"hackathonkit.local/agentkit/internal/routing/fly"
)

type datasetRow struct {
	Task     string `json:"task"`
	Category string `json:"category"`
	Domain   string `json:"domain"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fly-train", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataset := fs.String("dataset", "", "training dataset JSONL with task/category/domain")
	evalPath := fs.String("eval", "", "optional held-out dataset JSONL")
	model := fs.String("model", "mb", "mb (mushroom body readout) or knn (feedback memory)")
	kc := fs.Int("kc", fly.MBKenyonCells, "number of Kenyon cells")
	sparsity := fs.Float64("sparsity", fly.MBDefaultSparse, "KC sparsity fraction")
	epochs := fs.Int("epochs", 30, "training epochs")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	root := config.DiscoverRoot()
	config.LoadDotEnv(root)
	cfg := config.NewManager(root).Config()

	client := openai.New(cfg.OpenAI)
	// The local hash embedder keeps training and evaluation usable without a
	// provider embeddings endpoint; the API path is preferred when available.
	var embedder fly.Embedder
	if client.Available() {
		embedder = fly.NewChainEmbedder(client, fly.NewHashEncoder(512))
	} else {
		fmt.Fprintln(stderr, "fly-train: no embeddings provider available; using the local hash embedder")
		embedder = fly.NewHashEncoder(512)
	}

	if *model != "mb" && *model != "knn" {
		fmt.Fprintf(stderr, "fly-train: --model must be mb or knn, got %q\n", *model)
		return 2
	}
	if *model == "knn" && *dataset != "" {
		fmt.Fprintln(stderr, "fly-train: --model knn ignores --dataset (feedback memory rebuild)")
	}
	if *epochs <= 0 {
		fmt.Fprintf(stderr, "fly-train: --epochs must be > 0, got %d\n", *epochs)
		return 2
	}
	if *model == "knn" {
		// --model knn ignores --dataset: rebuild feedback memory regardless.
		if err := rebuildFromFeedback(root, cfg, client, stdout); err != nil {
			fmt.Fprintln(stderr, "fly-train:", err)
			return 1
		}
		return 0
	}
	if *dataset == "" {
		fmt.Fprintln(stderr, "fly-train: --dataset is required for --model mb")
		return 1
	}

	rows, err := readDatasetWithWarnings(*dataset, func(line int, reason string) {
		fmt.Fprintf(stderr, "fly-train: skipping dataset line %d: %s\n", line, reason)
	})
	if err != nil {
		fmt.Fprintln(stderr, "fly-train:", err)
		return 1
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "fly-train: dataset is empty")
		return 1
	}

	categories := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Category == "" || seen[row.Category] {
			continue
		}
		seen[row.Category] = true
		categories = append(categories, row.Category)
	}
	sort.Strings(categories)

	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, row.Task)
	}
	vectors, err := embedBatched(context.Background(), embedder, texts)
	if err != nil {
		fmt.Fprintln(stderr, "fly-train: embeddings failed:", err)
		return 1
	}
	if len(vectors) != len(rows) {
		fmt.Fprintf(stderr, "fly-train: embeddings returned %d vectors for %d rows\n", len(vectors), len(rows))
		return 1
	}

	brain := fly.NewMBrain(*kc, *sparsity, categories, 42)
	const learningRate = 0.15
	for epoch := 0; epoch < *epochs; epoch++ {
		for i, row := range rows {
			brain.Train(vectorNorm(vectors[i]), row.Category, learningRate)
		}
	}
	brainPath := filepath.Join(root, "data", "index", "fly-brain.json")
	if err := brain.SaveTo(brainPath); err != nil {
		fmt.Fprintln(stderr, "fly-train:", err)
		return 1
	}
	fmt.Fprintf(stdout, "fly-train: trained %d examples, %d categories, KC=%d sparsity=%.2f -> %s\n",
		len(rows), len(categories), *kc, *sparsity, brainPath)

	if *evalPath != "" {
		evalRows, err := readDatasetWithWarnings(*evalPath, func(line int, reason string) {
			fmt.Fprintf(stderr, "fly-train: skipping eval line %d: %s\n", line, reason)
		})
		if err != nil {
			fmt.Fprintln(stderr, "fly-train: eval:", err)
			return 1
		}
		if err := runEval(context.Background(), embedder, brain, evalRows, stdout, stderr); err != nil {
			fmt.Fprintln(stderr, "fly-train:", err)
			return 1
		}
	}
	return 0
}

func rebuildFromFeedback(root string, cfg config.File, client *openai.Client, stdout io.Writer) error {
	feedbackPath := filepath.Join(root, "logs", "fly-feedback.jsonl")
	f, err := os.Open(feedbackPath)
	if err != nil {
		return fmt.Errorf("no feedback collected yet: %w", err)
	}
	defer f.Close()
	seen := map[string]bool{}
	router := fly.NewRouter(cfg.Routing).
		WithEmbeddings(client, filepath.Join(root, "data", "index", "fly-embeddings.json"), cfg.OpenAI.EmbeddingModel).
		WithMemory(filepath.Join(root, "data", "index", "fly-memory.json")).
		WithBrain(filepath.Join(root, "data", "index", "fly-brain.json"))
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	count := 0
	for scanner.Scan() {
		var line struct {
			Task   string   `json:"task"`
			Chosen []string `json:"chosen"`
			Source string   `json:"source"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil || line.Task == "" || len(line.Chosen) == 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line.Task))
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, _, err := router.AddFeedback(context.Background(), fly.FeedbackEvent{
			Task: line.Task, Chosen: line.Chosen, Source: line.Source,
		}); err != nil {
			return err
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", feedbackPath, err)
	}
	fmt.Fprintf(stdout, "fly-train: processed %d feedback tasks, memory=%d, dopamine=%+v\n",
		count, router.MemorySize(), router.Dopamine())
	return nil
}

func runEval(ctx context.Context, embedder fly.Embedder, brain *fly.MBrain, rows []datasetRow, stdout, stderr io.Writer) error {
	if len(rows) == 0 {
		return fmt.Errorf("eval set is empty; nothing to evaluate")
	}
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, row.Task)
	}
	vectors, err := embedBatched(ctx, embedder, texts)
	if err != nil {
		return fmt.Errorf("eval embeddings failed: %w", err)
	}
	if len(vectors) != len(rows) {
		return fmt.Errorf("eval embeddings returned %d vectors for %d rows", len(vectors), len(rows))
	}
	top1, top3 := 0, 0
	confusion := map[string]map[string]int{}
	for i, row := range rows {
		ranking := brain.Rank(vectorNorm(vectors[i]))
		if len(ranking) == 0 {
			continue
		}
		if ranking[0] == row.Category {
			top1++
		}
		for _, category := range ranking[:min(3, len(ranking))] {
			if category == row.Category {
				top3++
				break
			}
		}
		if confusion[row.Category] == nil {
			confusion[row.Category] = map[string]int{}
		}
		confusion[row.Category][ranking[0]]++
	}
	n := float64(len(rows))
	fmt.Fprintf(stdout, "fly-train eval: top-1 %.1f%%, top-3 %.1f%% (n=%d)\n", 100*float64(top1)/n, 100*float64(top3)/n, len(rows))
	keys := make([]string, 0, len(confusion))
	for key := range confusion {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(stdout, "  %-16s -> %v\n", key, confusion[key])
	}
	return nil
}

func readDataset(path string) ([]datasetRow, error) {
	return readDatasetWithWarnings(path, nil)
}

func readDatasetWithWarnings(path string, onSkip func(line int, reason string)) ([]datasetRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows := []datasetRow{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var row datasetRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			if onSkip != nil {
				onSkip(lineNumber, "malformed JSON")
			}
			continue
		}
		if row.Task == "" || row.Category == "" {
			if onSkip != nil {
				onSkip(lineNumber, "missing task or category")
			}
			continue
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

// embedBatched embeds in bounded batches with a per-batch deadline so large
// datasets neither exceed provider input limits nor hang forever.
func embedBatched(ctx context.Context, embedder fly.Embedder, texts []string) ([][]float64, error) {
	const batchSize = 64
	out := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		vectors, err := embedder.Embed(batchCtx, texts[start:end])
		cancel()
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d: %w", start, end, err)
		}
		if len(vectors) != end-start {
			return nil, fmt.Errorf("batch %d-%d returned %d vectors", start, end, len(vectors))
		}
		out = append(out, vectors...)
	}
	return out, nil
}

func vectorNorm(v []float64) []float64 {
	sum := 0.0
	for _, x := range v {
		sum += x * x
	}
	norm := math.Sqrt(sum)
	if norm == 0 {
		return v
	}
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
