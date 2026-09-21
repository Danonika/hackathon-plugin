package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func searchData(t *testing.T, env any) map[string]any {
	t.Helper()
	switch v := env.(type) {
	case map[string]any:
		return v
	default:
		t.Fatalf("unexpected data type %T", env)
		return nil
	}
}

func TestKnowledgeIndexFromArtifactAndScopedSearch(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "policy.txt"),
		"Политика безопасности: пароль должен содержать не менее двенадцати символов. "+
			"Доступ предоставляется только после проверки. Пароль хранится в защищенном хранилище.")

	ingest := call(t, reg, "documents.ingest", map[string]any{"path": "policy.txt"})
	if ingest.Status != "ok" {
		t.Fatalf("ingest status %s data %+v", ingest.Status, ingest.Data)
	}
	artifactID := ingest.Data.(map[string]any)["artifact_id"].(string)

	idx := call(t, reg, "knowledge.index", map[string]any{"artifact_id": artifactID, "corpus": "security"})
	if idx.Status != "ok" {
		t.Fatalf("index status %s data %+v warnings %v", idx.Status, idx.Data, idx.Warnings)
	}
	indexID := idx.Data.(map[string]any)["index_id"].(string)
	if idx.Data.(map[string]any)["source"] != artifactID {
		t.Fatalf("source = %v, want %s", idx.Data.(map[string]any)["source"], artifactID)
	}

	if env := call(t, reg, "knowledge.search", map[string]any{"query": "пароль хранилище", "index_id": indexID}); env.Status != "ok" {
		t.Fatalf("index_id search status %s data %+v", env.Status, env.Data)
	} else if searchData(t, env.Data)["matched"].(int) == 0 {
		t.Fatalf("expected scoped hits, got %+v", env.Data)
	}

	if env := call(t, reg, "knowledge.search", map[string]any{"query": "пароль", "index_ids": []any{indexID}, "text_chars": float64(40)}); env.Status != "ok" {
		t.Fatalf("index_ids search status %s data %+v", env.Status, env.Data)
	} else {
		raw, _ := json.Marshal(env.Data)
		var decoded struct {
			Hits []struct {
				Text       string `json:"text"`
				ArtifactID string `json:"artifact_id"`
				Block      string `json:"block"`
			} `json:"hits"`
		}
		_ = json.Unmarshal(raw, &decoded)
		if len(decoded.Hits) == 0 {
			t.Fatalf("expected hits: %s", raw)
		}
		if len([]rune(decoded.Hits[0].Text)) > 43 {
			t.Fatalf("text_chars not applied: %q", decoded.Hits[0].Text)
		}
		if decoded.Hits[0].ArtifactID != artifactID {
			t.Fatalf("hit artifact = %q, want %q", decoded.Hits[0].ArtifactID, artifactID)
		}
	}

	if env := call(t, reg, "knowledge.search", map[string]any{"query": "пароль", "corpus": "security"}); env.Status != "ok" {
		t.Fatalf("corpus search status %s", env.Status)
	}

	miss := call(t, reg, "knowledge.search", map[string]any{"query": "совершеннонесуществующееслово"})
	if miss.Status != "ok" {
		t.Fatalf("miss status %s", miss.Status)
	}
	if miss.Data.(map[string]any)["matched"].(int) != 0 {
		t.Fatalf("expected no hits, got %+v", miss.Data)
	}
	if len(miss.Warnings) == 0 || miss.Warnings[len(miss.Warnings)-1] != "no matches for the query" {
		t.Fatalf("expected no-match warning, got %v", miss.Warnings)
	}
}

func TestKnowledgeIndexErrors(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "data", "input", "empty.txt"), "")
	mustWrite(t, filepath.Join(root, "data", "input", "ok.txt"), "Текст для индексации про оборудование.")

	if env := call(t, reg, "knowledge.index", map[string]any{}); env.Status != "error" {
		t.Fatalf("missing source must fail, got %+v", env)
	}
	if env := call(t, reg, "knowledge.index", map[string]any{"path": "missing.txt"}); env.Status != "error" {
		t.Fatalf("missing file must fail, got %+v", env)
	}
	if env := call(t, reg, "knowledge.index", map[string]any{"path": "empty.txt"}); env.Status != "error" {
		t.Fatalf("empty text must fail, got %+v", env)
	}
	if env := call(t, reg, "knowledge.index", map[string]any{"artifact_id": "knowledge/missing.json"}); env.Status != "error" {
		t.Fatalf("missing artifact must fail, got %+v", env)
	}
	if env := call(t, reg, "knowledge.index", map[string]any{"path": "../../etc/passwd"}); env.Status != "error" {
		t.Fatalf("traversal must fail, got %+v", env)
	}

	// chunks with a custom size/overlap produce multiple chunks
	mustWrite(t, filepath.Join(root, "data", "input", "long.txt"), string([]rune("абвгдежзиклмнопрстуфх"))+"\n")
	env := call(t, reg, "knowledge.index", map[string]any{
		"path": "long.txt", "chunk_chars": float64(5), "overlap": float64(2),
	})
	if env.Status != "ok" {
		t.Fatalf("custom chunking status %s data %+v", env.Status, env.Data)
	}
	if got := env.Data.(map[string]any)["chunk_count"].(int); got < 3 {
		t.Fatalf("expected multiple chunks, got %d", got)
	}
}

func TestKnowledgeSearchEmptyAndUnmatchedScope(t *testing.T) {
	reg, root := testRegistry(t)

	if env := call(t, reg, "knowledge.search", map[string]any{"query": "anything"}); env.Status != "partial" {
		t.Fatalf("empty index search status %s, want partial: %+v", env.Status, env)
	} else if env.Data.(map[string]any)["index_count"].(int) != 0 {
		t.Fatalf("index_count = %v", env.Data)
	}

	mustWrite(t, filepath.Join(root, "data", "input", "doc.txt"), "Договор аренды помещения сроком на один год.")
	if env := call(t, reg, "knowledge.index", map[string]any{"path": "doc.txt", "corpus": "corp-a"}); env.Status != "ok" {
		t.Fatalf("index status %s data %+v", env.Status, env.Data)
	}
	if env := call(t, reg, "knowledge.search", map[string]any{"query": "договор", "corpus": "corp-b"}); env.Status != "partial" {
		t.Fatalf("mismatched corpus status %s, want partial: %+v", env.Status, env)
	}
	if env := call(t, reg, "knowledge.search", map[string]any{"query": ""}); env.Status != "error" {
		t.Fatalf("empty query must fail, got %+v", env)
	}
	// a foreign index id must not match anything
	if env := call(t, reg, "knowledge.search", map[string]any{"query": "договор", "index_id": "knowledge/nope.json"}); env.Status != "partial" {
		t.Fatalf("unknown index_id status %s, want partial: %+v", env.Status, env)
	}
	// a corrupt index file is skipped without failing the search
	if err := os.MkdirAll(filepath.Join(root, "data", "index", "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "index", "knowledge", "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if env := call(t, reg, "knowledge.search", map[string]any{"query": "договор"}); env.Status != "ok" {
		t.Fatalf("search with corrupt index status %s: %+v", env.Status, env)
	}
}

func TestChunkText(t *testing.T) {
	if got := chunkText("", 10, 2); got != nil {
		t.Fatalf("chunkText empty = %#v", got)
	}
	if got := chunkText("abcdef", 0, 0); len(got) != 1 || got[0] != "abcdef" {
		t.Fatalf("chunkText default size = %#v", got)
	}
	if got := chunkText("abcdef", 3, -1); len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Fatalf("chunkText negative overlap = %#v", got)
	}
	if got := chunkText("abcdef", 3, 99); len(got) != 2 {
		t.Fatalf("chunkText overlap >= size = %#v", got)
	}
	got := chunkText("abcdefg", 4, 2)
	if len(got) != 3 || got[0] != "abcd" || got[1] != "cdef" || got[2] != "efg" {
		t.Fatalf("chunkText overlapping = %#v", got)
	}
}
