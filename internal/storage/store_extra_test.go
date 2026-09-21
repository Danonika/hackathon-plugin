package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/metadata"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	store := New(root, config.StorageSettings{
		InputDir:    "data/input",
		ArtifactDir: "data/artifacts",
		IndexDir:    "data/index",
	})
	return store, root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRoot(t *testing.T) {
	store, root := newTestStore(t)
	if store.Root() != root {
		t.Fatalf("Root = %q, want %q", store.Root(), root)
	}
}

func TestSanitizeMore(t *testing.T) {
	rejected := []string{"   ", ".", "a:b", `\\server\share`, "./.."}
	for _, input := range rejected {
		if _, err := sanitize(input); err == nil {
			t.Fatalf("expected %q to be rejected", input)
		}
	}
	got, err := sanitize(`reports\a.md`)
	if err != nil || got != filepath.Join("reports", "a.md") {
		t.Fatalf("sanitize backslash = %q, %v", got, err)
	}
}

func TestInputPath(t *testing.T) {
	store, root := newTestStore(t)
	writeFile(t, filepath.Join(root, "data", "input", "notes", "a.txt"), "hi")

	got, err := store.InputPath("notes/a.txt")
	if err != nil || got != filepath.Join(root, "data", "input", "notes", "a.txt") {
		t.Fatalf("InputPath = %q, %v", got, err)
	}
	if _, err := store.InputPath("missing.txt"); err == nil {
		t.Fatal("expected error for a missing input")
	}
	if _, err := store.InputPath("notes"); err == nil {
		t.Fatal("expected error for a directory input")
	}
	if _, err := store.InputPath("../secret"); err == nil {
		t.Fatal("expected error for path traversal")
	}
	if _, err := store.InputPath("/etc/passwd"); err == nil {
		t.Fatal("expected error for an absolute path")
	}
}

func TestInputPathSymlinkEscape(t *testing.T) {
	store, root := newTestStore(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "secret")
	link := filepath.Join(root, "data", "input", "link")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InputPath("link/secret.txt"); err == nil || !strings.Contains(err.Error(), "escapes repository root") {
		t.Fatalf("expected escape error, got %v", err)
	}
}

func TestInputPathMissingRoot(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "does-not-exist"), config.StorageSettings{
		InputDir: "data/input", ArtifactDir: "data/artifacts", IndexDir: "data/index",
	})
	if _, err := store.InputPath("a.txt"); err == nil {
		t.Fatal("expected error when root does not exist")
	}
}

func TestArtifactPath(t *testing.T) {
	store, root := newTestStore(t)

	got, err := store.ArtifactPath("reports/a.md", false)
	if err != nil || got != filepath.Join(root, "data", "artifacts", "reports", "a.md") {
		t.Fatalf("ArtifactPath = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Dir(got)); !os.IsNotExist(err) {
		t.Fatalf("parent must not be created when createParent is false")
	}

	got, err = store.ArtifactPath("reports/a.md", true)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Dir(got)); err != nil || !st.IsDir() {
		t.Fatalf("parent not created: %v", err)
	}

	if _, err := store.ArtifactPath("../escape", false); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestArtifactPathMkdirError(t *testing.T) {
	store, root := newTestStore(t)
	writeFile(t, filepath.Join(root, "data", "artifacts", "blocker"), "file")
	if _, err := store.ArtifactPath("blocker/sub/a.md", true); err == nil {
		t.Fatal("expected mkdir error when parent is a file")
	}
}

func TestArtifactPathSymlinkEscape(t *testing.T) {
	store, root := newTestStore(t)
	outside := t.TempDir()
	link := filepath.Join(root, "data", "artifacts", "link")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArtifactPath("link/a.md", true); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestWriteArtifactErrors(t *testing.T) {
	store, root := newTestStore(t)
	if _, err := store.WriteArtifact("../escape", []byte("x")); err == nil {
		t.Fatal("expected sanitize error")
	}

	writeFile(t, filepath.Join(root, "data", "artifacts", "blocker"), "file")
	if _, err := store.WriteArtifact("blocker/sub/a.md", []byte("x")); err == nil {
		t.Fatal("expected mkdir error")
	}

	writeFile(t, filepath.Join(root, "data", "artifacts", "asdir", "keep"), "x")
	if _, err := store.WriteArtifact("asdir", []byte("x")); err == nil {
		t.Fatal("expected write error when target is a directory")
	}
}

func TestWriteArtifactBackendErrorsReported(t *testing.T) {
	store, _ := newTestStore(t)
	metaErr := errors.New("meta down")
	objErr := errors.New("objects down")
	var reported []error
	store.Attach(errMeta{err: metaErr}, errObjects{putErr: objErr}, func(err error) {
		reported = append(reported, err)
	})
	if _, err := store.WriteArtifact("reports/a.md", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if len(reported) != 2 || !errors.Is(reported[0], metaErr) || !errors.Is(reported[1], objErr) {
		t.Fatalf("reported = %v", reported)
	}

	store.Attach(errMeta{err: metaErr}, nil, nil)
	if _, err := store.WriteArtifact("reports/b.md", []byte("hi")); err != nil {
		t.Fatal(err)
	}
}

type errMeta struct{ err error }

func (e errMeta) RecordArtifact(context.Context, metadata.ArtifactRecord) error { return e.err }

type errObjects struct {
	putErr error
	getErr error
	data   []byte
}

func (e errObjects) Put(context.Context, string, []byte) error { return e.putErr }

func (e errObjects) Get(context.Context, string) ([]byte, error) {
	if e.getErr != nil {
		return nil, e.getErr
	}
	return e.data, nil
}

func TestReadArtifactErrors(t *testing.T) {
	store, root := newTestStore(t)
	if _, err := store.ReadArtifact("../escape"); err == nil {
		t.Fatal("expected sanitize error")
	}
	if _, err := store.ReadArtifact("missing.md"); err == nil {
		t.Fatal("expected missing error without a remote backend")
	}

	writeFile(t, filepath.Join(root, "data", "artifacts", "asdir", "keep"), "x")
	if _, err := store.ReadArtifact("asdir"); err == nil {
		t.Fatal("expected read error when target is a directory")
	}
}

func TestReadArtifactRemoteFallbackError(t *testing.T) {
	store, _ := newTestStore(t)
	store.Attach(nil, errObjects{getErr: errors.New("remote down")}, nil)
	_, err := store.ReadArtifact("missing/a.md")
	if err == nil || !strings.Contains(err.Error(), "remote fetch failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadArtifactRehydrateFailureStillReturnsRemote(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	store, root := newTestStore(t)
	artifacts := filepath.Join(root, "data", "artifacts")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(artifacts, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(artifacts, 0o755) })

	store.Attach(nil, errObjects{data: []byte("remote")}, nil)
	data, err := store.ReadArtifact("missing/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "remote" {
		t.Fatalf("data = %q", data)
	}
}

func TestWriteIndexAndReadIndex(t *testing.T) {
	store, root := newTestStore(t)
	id, err := store.WriteIndex("snapshots/a.json", []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if id != "snapshots/a.json" {
		t.Fatalf("id = %q", id)
	}
	data, err := store.ReadIndex("snapshots/a.json")
	if err != nil || string(data) != `{"a":1}` {
		t.Fatalf("ReadIndex = %q, %v", data, err)
	}
	if _, err := store.ReadIndex("snapshots/missing.json"); err == nil {
		t.Fatal("expected error for a missing index")
	}
	if _, err := store.WriteIndex("../escape", []byte("x")); err == nil {
		t.Fatal("expected sanitize error")
	}
	if _, err := store.ReadIndex("../escape"); err == nil {
		t.Fatal("expected sanitize error")
	}

	writeFile(t, filepath.Join(root, "data", "index", "blocker"), "file")
	if _, err := store.WriteIndex("blocker/sub/a.json", []byte("x")); err == nil {
		t.Fatal("expected mkdir error")
	}
	writeFile(t, filepath.Join(root, "data", "index", "asdir", "keep"), "x")
	if _, err := store.WriteIndex("asdir", []byte("x")); err == nil {
		t.Fatal("expected write error when target is a directory")
	}
}

func TestListIndexes(t *testing.T) {
	store, root := newTestStore(t)
	if got, err := store.ListIndexes(); err != nil || got != nil {
		t.Fatalf("missing dir: %v, %v", got, err)
	}

	writeFile(t, filepath.Join(root, "data", "index", "a.json"), "{}")
	writeFile(t, filepath.Join(root, "data", "index", "sub", "b.json"), "{}")
	writeFile(t, filepath.Join(root, "data", "index", "sub", "notes.txt"), "x")
	writeFile(t, filepath.Join(root, "data", "index", "empty", ".keep"), "")

	got, err := store.ListIndexes()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.json", "sub/b.json"}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListIndexes = %v, want %v", got, want)
	}
}

func TestListIndexesBaseIsFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	store, root := newTestStore(t)
	base := filepath.Join(root, "data", "index")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(base, 0o755) })
	if _, err := store.ListIndexes(); err == nil {
		t.Fatal("expected walk error when index dir is unreadable")
	}
}

func TestListIndexesSkipsUnreadableSubdir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	store, root := newTestStore(t)
	base := filepath.Join(root, "data", "index")
	writeFile(t, filepath.Join(base, "a.json"), "{}")
	sub := filepath.Join(base, "locked")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	got, err := store.ListIndexes()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a.json"}) {
		t.Fatalf("ListIndexes = %v", got)
	}
}

func TestListInputs(t *testing.T) {
	store, root := newTestStore(t)
	if got, err := store.ListInputs(); err != nil || got != nil {
		t.Fatalf("missing dir: %v, %v", got, err)
	}

	writeFile(t, filepath.Join(root, "data", "input", "a.txt"), "a")
	writeFile(t, filepath.Join(root, "data", "input", "nested", "b.csv"), "b")

	got, err := store.ListInputs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.txt", "nested/b.csv"}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListInputs = %v, want %v", got, want)
	}
}

func TestListInputsBaseIsFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	store, root := newTestStore(t)
	base := filepath.Join(root, "data", "input")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(base, 0o755) })
	if _, err := store.ListInputs(); err == nil {
		t.Fatal("expected walk error when input dir is unreadable")
	}
}
