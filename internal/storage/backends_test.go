package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/metadata"
)

type fakeMeta struct {
	records []metadata.ArtifactRecord
}

func (f *fakeMeta) RecordArtifact(ctx context.Context, rec metadata.ArtifactRecord) error {
	f.records = append(f.records, rec)
	return nil
}

type fakeObjects struct {
	puts map[string][]byte
}

func (f *fakeObjects) Put(ctx context.Context, name string, data []byte) error {
	f.puts[name] = append([]byte(nil), data...)
	return nil
}

func (f *fakeObjects) Get(ctx context.Context, name string) ([]byte, error) {
	if data, ok := f.puts[name]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func TestAttachBackends(t *testing.T) {
	root := t.TempDir()
	settings := config.StorageSettings{InputDir: "data/input", ArtifactDir: "data/artifacts", IndexDir: "data/index"}
	store := New(root, settings)
	meta := &fakeMeta{}
	objects := &fakeObjects{puts: map[string][]byte{}}
	store.Attach(meta, objects, nil)

	id, err := store.WriteArtifact("reports/a.md", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.records) != 1 || meta.records[0].ArtifactID != id || meta.records[0].Bytes != 5 {
		t.Fatalf("unexpected metadata records: %+v", meta.records)
	}
	if string(objects.puts[id]) != "hello" {
		t.Fatalf("object store did not receive the artifact: %+v", objects.puts)
	}

	if err := os.Remove(filepath.Join(root, "data", "artifacts", "reports", "a.md")); err != nil {
		t.Fatal(err)
	}
	data, err := store.ReadArtifact("reports/a.md")
	if err != nil {
		t.Fatalf("expected remote fallback read, got %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected remote content %q", data)
	}
}
