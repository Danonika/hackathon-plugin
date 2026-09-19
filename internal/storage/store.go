package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hackathonkit.local/agentkit/internal/config"
)

type Store struct {
	root        string
	inputDir    string
	artifactDir string
	indexDir    string
}

func New(root string, s config.StorageSettings) *Store {
	return &Store{
		root:        root,
		inputDir:    filepath.FromSlash(s.InputDir),
		artifactDir: filepath.FromSlash(s.ArtifactDir),
		indexDir:    filepath.FromSlash(s.IndexDir),
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
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(clean), nil
}

func (s *Store) ReadArtifact(rel string) ([]byte, error) {
	clean, err := sanitize(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(s.root, s.artifactDir, clean))
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
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return nil
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
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}
