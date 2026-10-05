package plans

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveDefaults(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, Plan{Slug: "defaults", Revision: -3}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, "defaults")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != 1 {
		t.Fatalf("revision = %d, want 1", loaded.Revision)
	}
	if loaded.Stages == nil || len(loaded.Stages) != 0 {
		t.Fatalf("stages = %#v, want empty slice", loaded.Stages)
	}
	if loaded.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt must be set")
	}
}

func TestSaveErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "evals"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, Plan{Slug: "blocked"}); err == nil {
		t.Fatal("expected mkdir error when evals is a file")
	}

	root2 := t.TempDir()
	target := Path(root2, "occupied")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(root2, Plan{Slug: "occupied"}); err == nil {
		t.Fatal("expected rename error when the plan path is a non-empty directory")
	}

	if os.Geteuid() != 0 && runtime.GOOS != "windows" {
		// atomicfile.Write tightens an existing state directory back to 0700,
		// so a read-only plans directory can no longer be simulated that way;
		// block the parent instead so MkdirAll of evals/plans fails.
		root3 := t.TempDir()
		parent := filepath.Join(root3, "evals")
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(parent, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
		if err := Save(root3, Plan{Slug: "readonly"}); err == nil {
			t.Fatal("expected mkdir error in a read-only parent directory")
		}
	}
}

func TestLatestSkipsInvalidEntries(t *testing.T) {
	root := t.TempDir()
	dir := Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Latest(root); ok {
		t.Fatal("no valid plan must be found")
	}

	if _, ok := Latest(t.TempDir()); ok {
		t.Fatal("missing plans dir must return false")
	}
}

func TestLatestDirIsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "evals"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Dir(root), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Latest(root); ok {
		t.Fatal("a file at the plans dir must not yield a plan")
	}
}

func TestLoadParseErrorAndSlugFallback(t *testing.T) {
	root := t.TempDir()
	dir := Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "broken"); err == nil {
		t.Fatal("expected parse error")
	}

	if err := os.WriteFile(filepath.Join(dir, "no-slug.json"), []byte(`{"stages":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, "No Slug!")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Slug != "no-slug" {
		t.Fatalf("slug fallback = %q", loaded.Slug)
	}
}

func TestSlugEdges(t *testing.T) {
	long := strings.Repeat("a", 70)
	got := Slug(long)
	if len(got) > 60 {
		t.Fatalf("long slug len = %d (%q)", len(got), got)
	}
	// Slug must be idempotent: Save normalizes and Path re-normalizes, so a
	// second pass must not re-hash an already-truncated stem.
	if again := Slug(got); again != got {
		t.Fatalf("Slug is not idempotent: %q -> %q", got, again)
	}
	// Distinct names sharing the first 60 runes must not collide.
	if Slug(strings.Repeat("a", 61)) == Slug(strings.Repeat("a", 62)) {
		t.Fatal("truncated slugs must be disambiguated")
	}
	if got := Slug("Привет Мир"); got != "привет-мир" {
		t.Fatalf("cyrillic slug = %q", got)
	}
	dashAtCut := strings.Repeat("a", 59) + " b"
	cut := Slug(dashAtCut)
	if strings.Contains(cut, "--") || strings.HasPrefix(cut, "-") || strings.HasSuffix(cut, "-") {
		t.Fatalf("slug must not contain a leading/trailing/double dash: %q", cut)
	}
	if again := Slug(cut); again != cut {
		t.Fatalf("long slug is not idempotent: %q -> %q", cut, again)
	}
	if got := Slug("--"); got != "plan" {
		t.Fatalf("dashes-only slug = %q", got)
	}
}
