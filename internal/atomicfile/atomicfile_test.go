package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteCreatesParentsAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "state.json")
	if err := Write(path, []byte("first"), ModeContent); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatalf("read = %q, %v", data, err)
	}
}

func TestWriteOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := Write(path, []byte("one"), ModeContent); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("two"), ModeContent); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "two" {
		t.Fatalf("read = %q, %v", data, err)
	}
}

func TestWritePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are POSIX-only")
	}
	cases := []struct {
		name string
		mode os.FileMode
		want os.FileMode
	}{
		{"content", ModeContent, 0o644},
		{"state", ModeState, 0o600},
		{"zero defaults to content", 0, 0o644},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := Write(path, []byte("x"), c.mode); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != c.want {
			t.Fatalf("%s: mode = %v, want %v", c.name, info.Mode().Perm(), c.want)
		}
	}
}

func TestWriteParentIsFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(blocker, "state.json"), []byte("x"), ModeContent); err == nil {
		t.Fatal("expected an error when the parent path is a file")
	}
}

func TestWriteDestinationIsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, []byte("x"), ModeContent); err == nil {
		t.Fatal("expected an error when the destination is a directory")
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("temporary file was not cleaned up: %v", entries)
	}
}

func TestWriteLeavesNoTempFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	if err := Write(path, []byte("x"), ModeContent); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("leftovers: %v", entries)
	}
}
