package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRoutingFile(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "routing.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingLiveRead(t *testing.T) {
	root := t.TempDir()
	writeRoutingFile(t, root, "routing:\n  mode: normal\n  version: 1\n")
	mgr := NewManager(root)
	if got := mgr.Routing().Mode; got != "normal" {
		t.Fatalf("expected normal, got %s", got)
	}
	writeRoutingFile(t, root, "routing:\n  mode: fly\n  version: 2\n")
	if got := mgr.Routing().Mode; got != "fly" {
		t.Fatalf("expected live re-read to return fly, got %s", got)
	}
}

func TestSetRoutingMode(t *testing.T) {
	root := t.TempDir()
	writeRoutingFile(t, root, "routing:\n  mode: normal\n  version: 1\n")
	mgr := NewManager(root)
	state, err := mgr.SetRoutingMode("fly")
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != "fly" || state.Version != 2 {
		t.Fatalf("unexpected state %+v", state)
	}
	if got := mgr.Routing().Mode; got != "fly" {
		t.Fatalf("expected cached state fly, got %s", got)
	}
	if _, err := mgr.SetRoutingMode("turbo"); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestRoutingModeNormalizationOnRead(t *testing.T) {
	root := t.TempDir()
	writeRoutingFile(t, root, "routing:\n  mode: \" Fly \"\n  version: 3\n")
	mgr := NewManager(root)
	if got := mgr.Routing().Mode; got != "fly" {
		t.Fatalf("expected normalized mode fly, got %q", got)
	}

	root2 := t.TempDir()
	writeRoutingFile(t, root2, "routing:\n  mode: turbo\n  version: 4\n")
	mgr2 := NewManager(root2)
	if got := mgr2.Routing().Mode; got != "normal" {
		t.Fatalf("invalid mode must fall back to normal, got %q", got)
	}
}

func TestConfigExplicitZeroIsPreserved(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("routing:\n  top_k: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(root)
	if got := mgr.Config().Routing.TopK; got != 0 {
		t.Fatalf("explicit top_k: 0 must override the default, got %d", got)
	}
}

func TestConfigNonPositiveMaxTextCharsIgnored(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("tools:\n  max_text_chars: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(root)
	if got := mgr.Config().Tools.MaxTextChars; got != 60000 {
		t.Fatalf("non-positive max_text_chars must keep the default 60000, got %d", got)
	}
}
