package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFreezeBundleExcludesRuntimeState(t *testing.T) {
	root := t.TempDir()
	const sha = "0123456789abcdef0123456789abcdef01234567"
	writeFile(t, filepath.Join(root, "README.md"), "# Demo\n")
	writeFile(t, filepath.Join(root, "configs", "routing.yaml"), "routing:\n  mode: normal\n")
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(root, ".git", "refs", "heads", "main"), sha+"\n")
	writeFile(t, filepath.Join(root, "logs", "mcp-calls.jsonl"), "{}\n")
	writeFile(t, filepath.Join(root, "evals", "judges", "x.json"), "{}\n")
	writeFile(t, filepath.Join(root, "data", "artifacts", "a.txt"), "artifact\n")
	writeFile(t, filepath.Join(root, "data", "index", "i.json"), "{}\n")
	writeFile(t, filepath.Join(root, "node_modules", "mod", "index.js"), "x\n")
	writeFile(t, filepath.Join(root, ".env"), "OPENAI_API_KEY=secret\n")
	writeFile(t, filepath.Join(root, ".env.local"), "OPENAI_API_KEY=secret\n")
	writeFile(t, filepath.Join(root, "apps", "web", ".env"), "OPENAI_API_KEY=secret\n")
	writeFile(t, filepath.Join(root, "apps", "web", ".env.production"), "OPENAI_API_KEY=secret\n")
	writeFile(t, filepath.Join(root, "apps", "web", "logs", "app.log"), "log\n")

	bundle, err := FreezeBundle(BundleOptions{Root: root, ReadOnly: true, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.GitSHA != sha {
		t.Fatalf("git sha = %q, want %q", bundle.GitSHA, sha)
	}
	if len(bundle.BundleHash) != 64 {
		t.Fatalf("bundle hash = %q, want 64 hex chars", bundle.BundleHash)
	}
	for _, present := range []string{"README.md", "configs/routing.yaml", "git-sha.txt", "rubric-version.txt", "bundle.json", "bundle-hash.txt"} {
		if _, err := os.Stat(filepath.Join(bundle.Dir, filepath.FromSlash(present))); err != nil {
			t.Fatalf("expected %s in bundle: %v", present, err)
		}
	}
	for _, absent := range []string{
		"logs", "evals", "node_modules", ".env", ".env.local",
		filepath.Join("apps", "web", ".env"), filepath.Join("apps", "web", ".env.production"),
		filepath.Join("apps", "web", "logs"),
		filepath.Join("data", "artifacts"), filepath.Join("data", "index"),
	} {
		if _, err := os.Stat(filepath.Join(bundle.Dir, absent)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be excluded from the bundle", absent)
		}
	}
	info, err := os.Stat(filepath.Join(bundle.Dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("bundle files must be read-only, got mode %v", info.Mode().Perm())
	}
	head, err := os.ReadFile(filepath.Join(bundle.Dir, "bundle-hash.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(head)) != bundle.BundleHash {
		t.Fatalf("bundle-hash.txt does not match the reported hash")
	}
}

func TestFreezeBundleIsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Demo\n")
	writeFile(t, filepath.Join(root, "src", "main.go"), "package main\n")

	first, err := FreezeBundle(BundleOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	second, err := FreezeBundle(BundleOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if first.BundleHash != second.BundleHash {
		t.Fatalf("bundle hash is not deterministic: %s vs %s", first.BundleHash, second.BundleHash)
	}
}

func TestFreezeBundleHashesNestedBundleJSON(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# demo\n")
	writeFile(t, filepath.Join(root, "nested", "bundle.json"), "{\"custom\":true}\n")

	withNested, err := FreezeBundle(BundleOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(withNested.Dir, "nested", "bundle.json")); err != nil {
		t.Fatalf("nested bundle.json must be copied: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "nested", "bundle.json")); err != nil {
		t.Fatal(err)
	}
	withoutNested, err := FreezeBundle(BundleOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if withNested.BundleHash == withoutNested.BundleHash {
		t.Fatal("a nested bundle.json must participate in the deterministic hash")
	}
}

func TestFreezeBundleRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := FreezeBundle(BundleOptions{Root: root, OutDir: filepath.Join(root, "..", "outside")}); err == nil {
		t.Fatal("expected an error for an output directory outside the root")
	}
}

func TestFreezeBundleDemoProfile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "TASK.md"), "# Task\n")
	writeFile(t, filepath.Join(root, "README.md"), "# Demo\n")
	writeFile(t, filepath.Join(root, "architecture.md"), "# Architecture\n")
	writeFile(t, filepath.Join(root, "known-limitations.md"), "none\n")
	writeFile(t, filepath.Join(root, "demo-artifacts", "run.json"), "{}\n")
	writeFile(t, filepath.Join(root, "selected-source", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "test-results.json"), "{}\n")
	writeFile(t, filepath.Join(root, "smoke-results.json"), "{}\n")

	bundle, err := FreezeBundle(BundleOptions{Root: root, Profile: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Profile != "demo" {
		t.Fatalf("profile = %q, want demo", bundle.Profile)
	}
	for _, present := range []string{"TASK.md", "README.md", "architecture.md", "known-limitations.md", filepath.Join("demo-artifacts", "run.json")} {
		if _, err := os.Stat(filepath.Join(bundle.Dir, present)); err != nil {
			t.Fatalf("demo bundle must contain %s: %v", present, err)
		}
	}
	for _, absent := range []string{"selected-source", "test-results.json", "smoke-results.json"} {
		if _, err := os.Stat(filepath.Join(bundle.Dir, absent)); !os.IsNotExist(err) {
			t.Fatalf("demo bundle must not contain %s (regulation 9.4: no code re-check)", absent)
		}
	}

	full, err := FreezeBundle(BundleOptions{Root: root, Profile: "technical"})
	if err != nil {
		t.Fatal(err)
	}
	if full.Profile != "full" {
		t.Fatalf("technical must normalize to full, got %q", full.Profile)
	}
	if _, err := os.Stat(filepath.Join(full.Dir, "selected-source", "main.go")); err != nil {
		t.Fatalf("technical bundle must contain source: %v", err)
	}
}
