package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveGitSHABranches(t *testing.T) {
	t.Run("missing git directory", func(t *testing.T) {
		if got := resolveGitSHA(t.TempDir()); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
	t.Run("detached head", func(t *testing.T) {
		root := t.TempDir()
		sha := strings.Repeat("a", 40)
		writeFile(t, filepath.Join(root, ".git", "HEAD"), sha+"\n")
		if got := resolveGitSHA(root); got != sha {
			t.Fatalf("got %q, want %q", got, sha)
		}
	})
	t.Run("short detached head", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "deadbeef\n")
		if got := resolveGitSHA(root); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
	t.Run("empty ref", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: \n")
		if got := resolveGitSHA(root); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
	t.Run("loose ref", func(t *testing.T) {
		root := t.TempDir()
		sha := strings.Repeat("b", 40)
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		writeFile(t, filepath.Join(root, ".git", "refs", "heads", "main"), sha+"\n")
		if got := resolveGitSHA(root); got != sha {
			t.Fatalf("got %q, want %q", got, sha)
		}
	})
	t.Run("packed refs", func(t *testing.T) {
		root := t.TempDir()
		sha := strings.Repeat("c", 40)
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		writeFile(t, filepath.Join(root, ".git", "packed-refs"),
			"# pack-refs with: peeled fully-peeled sorted\n^ignored-peel\n"+sha+" refs/heads/main\n")
		if got := resolveGitSHA(root); got != sha {
			t.Fatalf("got %q, want %q", got, sha)
		}
	})
	t.Run("empty loose ref falls back to packed", func(t *testing.T) {
		root := t.TempDir()
		sha := strings.Repeat("d", 40)
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		writeFile(t, filepath.Join(root, ".git", "refs", "heads", "main"), "")
		writeFile(t, filepath.Join(root, ".git", "packed-refs"), sha+" refs/heads/main\n")
		if got := resolveGitSHA(root); got != sha {
			t.Fatalf("got %q, want %q", got, sha)
		}
	})
	t.Run("packed refs without a match", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		writeFile(t, filepath.Join(root, ".git", "packed-refs"), strings.Repeat("e", 40)+" refs/heads/other\n")
		if got := resolveGitSHA(root); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
	t.Run("empty loose ref without packed refs", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		writeFile(t, filepath.Join(root, ".git", "refs", "heads", "main"), "")
		if got := resolveGitSHA(root); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
}

func TestFileSHA256AndCopyFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := fileSHA256(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("fileSHA256 on a missing file must fail")
	}
	if _, err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "out"), false); err == nil {
		t.Fatal("copyFile with a missing source must fail")
	}

	src := filepath.Join(dir, "src.txt")
	writeFile(t, src, "payload")
	if _, err := copyFile(src, filepath.Join(dir, "missing", "dst.txt"), false); err == nil {
		t.Fatal("copyFile must fail when the destination directory is missing")
	}
	dst := filepath.Join(dir, "dst.txt")
	n, err := copyFile(src, dst, false)
	if err != nil || n != int64(len("payload")) {
		t.Fatalf("copyFile = %d bytes, err=%v", n, err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", info.Mode().Perm())
	}
	sum, err := fileSHA256(dst)
	if err != nil || len(sum) != 64 {
		t.Fatalf("sha = %q err=%v, want 64 hex chars", sum, err)
	}
}

func TestHashBundleSkipsManifestFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "a")
	writeFile(t, filepath.Join(dir, "bundle.json"), "{}")
	writeFile(t, filepath.Join(dir, "bundle-hash.txt"), "x")
	entries, hash, err := hashBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "a.txt" || entries[0].Bytes != 1 {
		t.Fatalf("entries = %+v, want only a.txt", entries)
	}
	if len(hash) != 64 {
		t.Fatalf("hash = %q, want 64 hex chars", hash)
	}
	if _, _, err := hashBundle(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("hashBundle on a missing directory must fail")
	}
}

func TestWriteBundleMeta(t *testing.T) {
	dir := t.TempDir()
	if err := writeBundleMeta(dir, "unknown.txt", ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "unknown.txt"))
	if err != nil || string(data) != "unknown\n" {
		t.Fatalf("empty content = %q err=%v, want \"unknown\\n\"", data, err)
	}
	if err := writeBundleMeta(dir, "no-newline.txt", "value"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "no-newline.txt"))
	if err != nil || string(data) != "value\n" {
		t.Fatalf("content = %q err=%v, want \"value\\n\"", data, err)
	}
	if err := writeBundleMeta(dir, "with-newline.txt", "value\n"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "with-newline.txt"))
	if err != nil || string(data) != "value\n" {
		t.Fatalf("content = %q err=%v, want a single trailing newline", data, err)
	}
	if err := writeBundleMeta(filepath.Join(dir, "missing"), "x.txt", "v"); err == nil {
		t.Fatal("writing into a missing directory must fail")
	}
}

func TestCompileExcludesAndIsExcluded(t *testing.T) {
	rules := compileExcludes([]string{"  ", "data\\artifacts", "*.min.js", "a/b", "a/b"})
	if len(rules) != 3 {
		t.Fatalf("rules = %+v, want three after normalization and dedup", rules)
	}
	for _, rel := range []string{"data/artifacts", "data/artifacts/file.txt", "a/b", "a/b/c.txt"} {
		if !isExcluded(rel, rules) {
			t.Fatalf("%s must be excluded", rel)
		}
	}
	if !isExcluded("deep/x.min.js", rules) {
		t.Fatal("a glob rule must match the base name")
	}
	if isExcluded("deep/x.min.js.map", rules) {
		t.Fatal("a non-matching glob must not exclude")
	}
	if isExcluded("a/bb", rules) {
		t.Fatal("a slash rule must match whole path segments only")
	}
	if isExcluded("unrelated.txt", rules) {
		t.Fatal("an unrelated path must not be excluded")
	}
}

func TestFreezeBundleRejectsBadRoots(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "not-a-dir")
	writeFile(t, file, "x")
	if _, err := FreezeBundle(BundleOptions{Root: file}); err == nil {
		t.Fatal("a file as root must fail")
	}
	if _, err := FreezeBundle(BundleOptions{Root: filepath.Join(root, "missing")}); err == nil {
		t.Fatal("a missing root must fail")
	}
	if _, err := FreezeBundle(BundleOptions{Root: root, OutDir: root}); err == nil {
		t.Fatal("the output directory must not be the root itself")
	}
}

func TestFreezeBundleOutDirEdgeCases(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# x\n")

	bundle, err := FreezeBundle(BundleOptions{Root: root, OutDir: filepath.Join(root, "custom-out")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bundle.Dir, "README.md")); err != nil {
		t.Fatalf("custom out dir bundle missing README: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle.Dir, "custom-out")); !os.IsNotExist(err) {
		t.Fatal("the out dir must not be copied into itself")
	}

	blocker := filepath.Join(root, "blocker")
	writeFile(t, blocker, "x")
	if _, err := FreezeBundle(BundleOptions{Root: root, OutDir: filepath.Join(blocker, "out")}); err == nil {
		t.Fatal("an out dir under a file must fail")
	}
}

func TestFreezeBundleCustomExcludesAndSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.txt"), "keep")
	writeFile(t, filepath.Join(root, "skip.tmp"), "tmp")
	writeFile(t, filepath.Join(root, "vendor", "lib.go"), "package lib\n")
	writeFile(t, filepath.Join(root, "real.txt"), "real")
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	bundle, err := FreezeBundle(BundleOptions{
		Root:    root,
		Exclude: []string{"*.tmp", "vendor", " ", "dir\\sub"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, present := range []string{"keep.txt", "real.txt"} {
		if _, err := os.Stat(filepath.Join(bundle.Dir, present)); err != nil {
			t.Fatalf("expected %s in the bundle: %v", present, err)
		}
	}
	for _, absent := range []string{"skip.tmp", "vendor", "link.txt"} {
		if _, err := os.Stat(filepath.Join(bundle.Dir, absent)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be excluded or skipped", absent)
		}
	}
	for _, want := range []string{"*.tmp", "vendor", "dir/sub"} {
		found := false
		for _, got := range bundle.Excluded {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("exclude %q not reported in %v", want, bundle.Excluded)
		}
	}
}
