package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/metadata"
)

const backendTimeout = 30 * time.Second

func backendContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), backendTimeout)
}

type MetaRecorder interface {
	RecordArtifact(ctx context.Context, rec metadata.ArtifactRecord) error
}

type ObjectStore interface {
	Put(ctx context.Context, name string, data []byte) error
	Get(ctx context.Context, name string) ([]byte, error)
}

type Store struct {
	root        string
	inputDir    string
	artifactDir string
	indexDir    string

	meta    MetaRecorder
	objects ObjectStore
	onError func(error)
}

func New(root string, s config.StorageSettings) *Store {
	return &Store{
		root:        root,
		inputDir:    filepath.FromSlash(s.InputDir),
		artifactDir: filepath.FromSlash(s.ArtifactDir),
		indexDir:    filepath.FromSlash(s.IndexDir),
	}
}

func (s *Store) Attach(meta MetaRecorder, objects ObjectStore, onError func(error)) {
	s.meta = meta
	s.objects = objects
	s.onError = onError
}

func (s *Store) report(err error) {
	if err != nil && s.onError != nil {
		s.onError(err)
	}
}

func (s *Store) ensureWithin(full string) error {
	rootReal, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return nil
	}
	p := full
	for {
		real, err := filepath.EvalSymlinks(p)
		if err == nil {
			if real == rootReal || strings.HasPrefix(real, rootReal+string(filepath.Separator)) {
				return nil
			}
			return fmt.Errorf("resolved path escapes repository root: %s", full)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

func (s *Store) Root() string {
	return s.root
}

func sanitize(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("empty path")
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(rel, "/") || (len(rel) > 1 && rel[1] == ':') {
		return "", fmt.Errorf("absolute paths are not allowed: %s", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == ".." {
			return "", fmt.Errorf("path traversal not allowed: %s", rel)
		}
	}
	if clean == "." {
		return "", fmt.Errorf("invalid path: %s", rel)
	}
	return clean, nil
}

func (s *Store) InputPath(name string) (string, error) {
	clean, err := sanitize(name)
	if err != nil {
		return "", err
	}
	full := filepath.Join(s.root, s.inputDir, clean)
	if err := s.ensureWithin(full); err != nil {
		return "", err
	}
	if st, err := os.Stat(full); err != nil || st.IsDir() {
		return "", fmt.Errorf("input file not found: %s", name)
	}
	return full, nil
}

func (s *Store) ArtifactPath(name string, createParent bool) (string, error) {
	clean, err := sanitize(name)
	if err != nil {
		return "", err
	}
	full := filepath.Join(s.root, s.artifactDir, clean)
	if err := s.ensureWithin(full); err != nil {
		return "", err
	}
	if createParent {
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
	}
	return full, nil
}

func (s *Store) WriteArtifact(rel string, data []byte) (string, error) {
	clean, err := sanitize(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(s.root, s.artifactDir, clean)
	if err := s.ensureWithin(full); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	id := filepath.ToSlash(clean)
	if s.meta != nil {
		sum := sha256.Sum256(data)
		ctx, cancel := backendContext()
		s.report(s.meta.RecordArtifact(ctx, metadata.ArtifactRecord{
			ArtifactID: id,
			Kind:       "artifact",
			Bytes:      len(data),
			SHA256:     hex.EncodeToString(sum[:]),
		}))
		cancel()
	}
	if s.objects != nil {
		ctx, cancel := backendContext()
		s.report(s.objects.Put(ctx, id, data))
		cancel()
	}
	return id, nil
}

func (s *Store) ReadArtifact(rel string) ([]byte, error) {
	clean, err := sanitize(rel)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(s.root, s.artifactDir, clean)
	if err := s.ensureWithin(full); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err == nil {
		return data, nil
	}
	if s.objects != nil && os.IsNotExist(err) {
		ctx, cancel := backendContext()
		remote, oerr := s.objects.Get(ctx, filepath.ToSlash(clean))
		cancel()
		if oerr != nil {
			return nil, fmt.Errorf("artifact %s: local read failed (%v); remote fetch failed: %w", clean, err, oerr)
		}
		if mkErr := os.MkdirAll(filepath.Dir(full), 0o755); mkErr == nil {
			_ = os.WriteFile(full, remote, 0o644)
		}
		return remote, nil
	}
	return nil, err
}

func (s *Store) WriteIndex(rel string, data []byte) (string, error) {
	clean, err := sanitize(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(s.root, s.indexDir, clean)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(clean), nil
}

func (s *Store) ReadIndex(rel string) ([]byte, error) {
	clean, err := sanitize(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(s.root, s.indexDir, clean))
}

func (s *Store) ListIndexes() ([]string, error) {
	base := filepath.Join(s.root, s.indexDir)
	var out []string
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == base {
				return err
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}

func (s *Store) ListInputs() ([]string, error) {
	base := filepath.Join(s.root, s.inputDir)
	var out []string
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == base {
				return err
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}
