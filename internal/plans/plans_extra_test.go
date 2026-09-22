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
		root3 := t.TempDir()
		dir := Dir(root3)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		if err := Save(root3, Plan{Slug: "readonly"}); err == nil {
			t.Fatal("expected create-temp error in a read-only directory")
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
	if got := Slug(long); len(got) != 60 {
		t.Fatalf("long slug len = %d (%q)", len(got), got)
	}
	if got := Slug("Привет Мир"); got != "привет-мир" {
		t.Fatalf("cyrillic slug = %q", got)
	}
	dashAtCut := strings.Repeat("a", 59) + " b"
	if got := Slug(dashAtCut); got != strings.Repeat("a", 59) {
		t.Fatalf("slug trailing dash = %q", got)
	}
	if got := Slug("--"); got != "plan" {
		t.Fatalf("dashes-only slug = %q", got)
	}
}
