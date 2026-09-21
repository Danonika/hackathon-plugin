package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/openai"
	"hackathonkit.local/agentkit/internal/routing/fly"
)

func embeddingServer(t *testing.T, fail bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "embedding backend down", http.StatusInternalServerError)
			return
		}
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		type item struct {
			Embedding []float64 `json:"embedding"`
		}
		var out struct {
			Data []item `json:"data"`
		}
		for _, text := range req.Input {
			vec := []float64{0, 0, 0}
			switch {
			case strings.Contains(text, "alpha"):
				vec = []float64{1, 0, 0}
			case strings.Contains(text, "beta"):
				vec = []float64{0, 1, 0}
			case strings.Contains(text, "gamma"):
				vec = []float64{0, 0, 1}
			}
			out.Data = append(out.Data, item{Embedding: vec})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(server.Close)
	return server
}

func trainTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HACKATHONKIT_REPO_ROOT", root)
	return root
}

func writeDataset(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runTrain(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunRequiresAPIKey(t *testing.T) {
	trainTestRoot(t)
	t.Setenv("OPENAI_API_KEY", "")
	code, _, stderr := runTrain(t, "--dataset", "whatever.jsonl")
	if code != 1 || !strings.Contains(stderr, "OPENAI_API_KEY is required") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunMissingDataset(t *testing.T) {
	root := trainTestRoot(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1")
	code, _, stderr := runTrain(t, "--model", "mb")
	if code != 1 || !strings.Contains(stderr, "--dataset is required") {
		t.Fatalf("code=%d stderr=%q root=%s", code, stderr, root)
	}
}

func TestRunEmptyDataset(t *testing.T) {
	root := trainTestRoot(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1")
	path := writeDataset(t, root, "empty.jsonl", "# only comments\n\n")
	code, _, stderr := runTrain(t, "--dataset", path)
	if code != 1 || !strings.Contains(stderr, "dataset is empty") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunDatasetReadError(t *testing.T) {
	root := trainTestRoot(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1")
	code, _, stderr := runTrain(t, "--dataset", filepath.Join(root, "missing.jsonl"))
	if code != 1 || !strings.Contains(stderr, "fly-train:") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunEmbeddingsFailure(t *testing.T) {
	root := trainTestRoot(t)
	server := embeddingServer(t, true)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", server.URL)
	path := writeDataset(t, root, "train.jsonl", `{"task":"alpha one","category":"alpha"}`+"\n")
	code, _, stderr := runTrain(t, "--dataset", path, "--epochs", "1", "--kc", "16")
	if code != 1 || !strings.Contains(stderr, "embeddings failed") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunTrainAndEval(t *testing.T) {
	root := trainTestRoot(t)
	server := embeddingServer(t, false)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", server.URL)

	trainPath := writeDataset(t, root, "train.jsonl", strings.Join([]string{
		`{"task":"alpha one","category":"alpha","domain":"a"}`,
		`{"task":"alpha two","category":"alpha"}`,
		`{"task":"beta one","category":"beta"}`,
		`{"task":"no category row","category":""}`,
	}, "\n")+"\n")
	evalPath := writeDataset(t, root, "eval.jsonl", strings.Join([]string{
		`{"task":"alpha three","category":"alpha"}`,
		`{"task":"beta two","category":"beta"}`,
	}, "\n")+"\n")

	code, stdout, stderr := runTrain(t, "--dataset", trainPath, "--eval", evalPath, "--epochs", "3", "--kc", "64")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{"trained 3 examples, 2 categories", "fly-train eval: top-1", "-> map["} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	brainPath := filepath.Join(root, "data", "index", "fly-brain.json")
	data, err := os.ReadFile(brainPath)
	if err != nil {
		t.Fatalf("brain file: %v", err)
	}
	var brain fly.MBrain
	if err := json.Unmarshal(data, &brain); err != nil {
		t.Fatal(err)
	}
	if len(brain.Categories) != 2 || brain.Categories[0] != "alpha" || brain.Categories[1] != "beta" {
		t.Fatalf("brain categories = %v", brain.Categories)
	}
}

func TestRunEvalReadError(t *testing.T) {
	root := trainTestRoot(t)
	server := embeddingServer(t, false)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", server.URL)
	trainPath := writeDataset(t, root, "train.jsonl", `{"task":"alpha one","category":"alpha"}`+"\n")
	code, _, stderr := runTrain(t, "--dataset", trainPath, "--eval", filepath.Join(root, "missing-eval.jsonl"), "--epochs", "1", "--kc", "16")
	if code != 1 || !strings.Contains(stderr, "eval:") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunKNNFeedbackMissing(t *testing.T) {
	trainTestRoot(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1")
	code, _, stderr := runTrain(t, "--model", "knn")
	if code != 1 || !strings.Contains(stderr, "no feedback collected yet") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunKNNFeedbackSuccess(t *testing.T) {
	root := trainTestRoot(t)
	server := embeddingServer(t, false)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", server.URL)
	logs := filepath.Join(root, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	feedback := strings.Join([]string{
		`{"task":"alpha task","chosen":["data.inspect"],"source":"accepted"}`,
		`{"task":"alpha task","chosen":["data.inspect"],"source":"accepted"}`,
		``,
		`not json`,
		`{"task":"","chosen":["data.inspect"]}`,
		`{"task":"beta task","chosen":["reports.create"],"source":"fallback"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(logs, "fly-feedback.jsonl"), []byte(feedback), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTrain(t, "--model", "knn")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "processed 2 feedback tasks") {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "index", "fly-memory.json")); err != nil {
		t.Fatalf("fly memory file: %v", err)
	}
}

func TestRunFlagHandling(t *testing.T) {
	code, _, _ := runTrain(t, "--not-a-flag")
	if code != 2 {
		t.Fatalf("unknown flag code = %d, want 2", code)
	}
	code, _, _ = runTrain(t, "-h")
	if code != 0 {
		t.Fatalf("-h code = %d, want 0", code)
	}
}

func TestReadDataset(t *testing.T) {
	dir := t.TempDir()
	path := writeDataset(t, dir, "data.jsonl", strings.Join([]string{
		"# comment",
		"",
		"   ",
		"not-json",
		`{"task":"no category"}`,
		`{"category":"no task"}`,
		`{"task":"one","category":"a"}`,
		`  {"task":"two","category":"b","domain":"d"}  `,
	}, "\n"))
	rows, err := readDataset(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Task != "one" || rows[1].Domain != "d" {
		t.Fatalf("rows = %+v", rows)
	}
	if _, err := readDataset(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Fatal("missing dataset must fail")
	}
}

func TestVectorNorm(t *testing.T) {
	zero := []float64{0, 0, 0}
	if got := vectorNorm(zero); len(got) != 3 || got[0] != 0 {
		t.Fatalf("zero norm = %v", got)
	}
	got := vectorNorm([]float64{3, 4})
	if got[0] != 0.6 || got[1] != 0.8 {
		t.Fatalf("norm = %v", got)
	}
	if got := vectorNorm(nil); len(got) != 0 {
		t.Fatalf("nil norm = %v", got)
	}
}

func TestRunEvalZeroRanking(t *testing.T) {
	server := embeddingServer(t, false)
	client := &openai.Client{APIKey: "test", BaseURL: server.URL, HTTP: server.Client(), EmbeddingModel: "test"}
	brain := fly.NewMBrain(8, 0.5, nil, 1)
	var stdout, stderr strings.Builder
	runEval(client, brain, []datasetRow{{Task: "alpha one", Category: "alpha"}}, &stdout, &stderr)
	if !strings.Contains(stdout.String(), "top-1 0.0%") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunEvalEmbeddingError(t *testing.T) {
	server := embeddingServer(t, true)
	client := &openai.Client{APIKey: "test", BaseURL: server.URL, HTTP: server.Client(), EmbeddingModel: "test"}
	brain := fly.NewMBrain(8, 0.5, []string{"alpha"}, 1)
	var stdout, stderr strings.Builder
	runEval(client, brain, []datasetRow{{Task: "alpha one", Category: "alpha"}}, &stdout, &stderr)
	if stdout.String() != "" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "eval embeddings failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRebuildFromFeedbackMissingFile(t *testing.T) {
	root := t.TempDir()
	client := &openai.Client{APIKey: "test", BaseURL: "http://127.0.0.1:1", HTTP: http.DefaultClient}
	var stdout strings.Builder
	err := rebuildFromFeedback(root, config.NewManager(root).Config(), client, &stdout)
	if err == nil || !strings.Contains(err.Error(), "no feedback collected yet") {
		t.Fatalf("err = %v", err)
	}
}

func TestMin(t *testing.T) {
	if min(1, 2) != 1 || min(2, 1) != 1 || min(2, 2) != 2 {
		t.Fatal("min is broken")
	}
}
