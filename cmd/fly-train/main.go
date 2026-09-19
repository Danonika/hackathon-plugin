package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	dataset := flag.String("dataset", "", "training dataset JSONL with task/category/domain")
	evalPath := flag.String("eval", "", "optional held-out dataset JSONL")
	model := flag.String("model", "mb", "mb (mushroom body readout) or knn (feedback memory)")
	kc := flag.Int("kc", fly.MBKenyonCells, "number of Kenyon cells")
	sparsity := flag.Float64("sparsity", fly.MBDefaultSparse, "KC sparsity fraction")
	epochs := flag.Int("epochs", 30, "training epochs")
	flag.Parse()

	root := config.DiscoverRoot()
	config.LoadDotEnv(root)
	cfg := config.NewManager(root).Config()

	client := openai.New(cfg.OpenAI)
	if !client.Available() {
		fmt.Fprintln(os.Stderr, "fly-train: OPENAI_API_KEY is required to compute embeddings")
		os.Exit(1)
	}

	if *dataset == "" && *model == "knn" {
		rebuildFromFeedback(root, cfg, client)
		return
	}
	if *dataset == "" {
		fmt.Fprintln(os.Stderr, "fly-train: --dataset is required for --model mb")
		os.Exit(1)
	}

	rows, err := readDataset(*dataset)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fly-train:", err)
		os.Exit(1)
	}
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "fly-train: dataset is empty")
		os.Exit(1)
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
	vectors, err := client.Embed(context.Background(), texts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fly-train: embeddings failed:", err)
		os.Exit(1)
	}

	brain := fly.NewMBrain(*kc, *sparsity, categories, 42)
	const learningRate = 0.15
	for epoch := 0; epoch < *epochs; epoch++ {
		for i, row := range rows {
			if !seen[row.Category] {
				continue
			}
			brain.Train(vectorNorm(vectors[i]), row.Category, learningRate)
		}
	}
	brainPath := filepath.Join(root, "data", "index", "fly-brain.json")
	if err := brain.SaveTo(brainPath); err != nil {
		fmt.Fprintln(os.Stderr, "fly-train:", err)
		os.Exit(1)
	}
	fmt.Printf("fly-train: trained %d examples, %d categories, KC=%d sparsity=%.2f -> %s\n",
		len(rows), len(categories), *kc, *sparsity, brainPath)

	if *evalPath != "" {
		evalRows, err := readDataset(*evalPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fly-train: eval:", err)
			os.Exit(1)
		}
		runEval(client, brain, evalRows)
	}
}

func rebuildFromFeedback(root string, cfg config.File, client *openai.Client) {
	feedbackPath := filepath.Join(root, "logs", "fly-feedback.jsonl")
	f, err := os.Open(feedbackPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fly-train: no feedback collected yet:", err)
		os.Exit(1)
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
		if _, err := router.AddFeedback(context.Background(), fly.FeedbackEvent{
			Task: line.Task, Chosen: line.Chosen, Source: line.Source,
		}); err != nil {
			fmt.Fprintln(os.Stderr, "fly-train:", err)
			os.Exit(1)
		}
		count++
	}
	fmt.Printf("fly-train: processed %d feedback tasks, memory=%d, dopamine=%+v\n",
		count, router.MemorySize(), router.Dopamine())
}

func runEval(client *openai.Client, brain *fly.MBrain, rows []datasetRow) {
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, row.Task)
	}
	vectors, err := client.Embed(context.Background(), texts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fly-train: eval embeddings failed:", err)
		return
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
	fmt.Printf("fly-train eval: top-1 %.1f%%, top-3 %.1f%% (n=%d)\n", 100*float64(top1)/n, 100*float64(top3)/n, len(rows))
	keys := make([]string, 0, len(confusion))
	for key := range confusion {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  %-16s -> %v\n", key, confusion[key])
	}
}

func readDataset(path string) ([]datasetRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows := []datasetRow{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var row datasetRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.Task == "" || row.Category == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
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
