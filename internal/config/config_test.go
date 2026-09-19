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
