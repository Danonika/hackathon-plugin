package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type OpenAISettings struct {
	ChatModel       string `yaml:"chat_model"`
	VisionModel     string `yaml:"vision_model"`
	TranscribeModel string `yaml:"transcribe_model"`
	EmbeddingModel  string `yaml:"embedding_model"`
}

type StorageSettings struct {
	InputDir    string `yaml:"input_dir"`
	ArtifactDir string `yaml:"artifact_dir"`
	IndexDir    string `yaml:"index_dir"`
}

type ToolSettings struct {
	DefaultTimeoutSec int `yaml:"default_timeout_sec"`
	MaxTextChars      int `yaml:"max_text_chars"`
}

type RoutingSettings struct {
	FlyThreshold float64 `yaml:"fly_threshold"`
	TopK         int     `yaml:"top_k"`
}

type File struct {
	OpenAI  OpenAISettings  `yaml:"openai"`
	Storage StorageSettings `yaml:"storage"`
	Tools   ToolSettings    `yaml:"tools"`
	Routing RoutingSettings `yaml:"routing"`
}

// filePatch mirrors the numeric fields with pointers so an explicit zero can be
// distinguished from an absent key.
type filePatch struct {
	Tools struct {
		DefaultTimeoutSec *int `yaml:"default_timeout_sec"`
		MaxTextChars      *int `yaml:"max_text_chars"`
	} `yaml:"tools"`
	Routing struct {
		FlyThreshold *float64 `yaml:"fly_threshold"`
		TopK         *int     `yaml:"top_k"`
	} `yaml:"routing"`
}

type RoutingState struct {
	Mode    string `yaml:"mode" json:"mode"`
	Version int    `yaml:"version" json:"version"`
}

type routingDoc struct {
	Routing RoutingState `yaml:"routing"`
}

type Manager struct {
	root string

	mu           sync.Mutex
	cfg          File
	routingCache RoutingState
	routingMtime time.Time
	routingInit  bool
}

func defaults() File {
	return File{
		OpenAI: OpenAISettings{
			ChatModel:       "gpt-4o-mini",
			VisionModel:     "gpt-4o-mini",
			TranscribeModel: "whisper-1",
			EmbeddingModel:  "text-embedding-3-small",
		},
		Storage: StorageSettings{
			InputDir:    "data/input",
			ArtifactDir: "data/artifacts",
			IndexDir:    "data/index",
		},
		Tools: ToolSettings{
			DefaultTimeoutSec: 120,
			MaxTextChars:      60000,
		},
		Routing: RoutingSettings{
			FlyThreshold: 0.55,
			TopK:         5,
		},
	}
}

func DiscoverRoot() string {
	if v := os.Getenv("HACKATHONKIT_REPO_ROOT"); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return abs
		}
		return v
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	cur := dir
	for {
		for _, marker := range []string{
			filepath.Join("configs", "routing.yaml"),
			"docker-compose.yml",
		} {
			if _, err := os.Stat(filepath.Join(cur, marker)); err == nil {
				return cur
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dir
		}
		cur = parent
	}
}

func NewManager(root string) *Manager {
	m := &Manager{root: root, cfg: defaults()}
	path := filepath.Join(root, "configs", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("config: cannot read %s: %v (using built-in defaults)", path, err)
		}
		return m
	}
	var loaded File
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		log.Printf("config: cannot parse %s: %v (using built-in defaults)", path, err)
		return m
	}
	mergeFile(&m.cfg, loaded)
	var patch filePatch
	if err := yaml.Unmarshal(data, &patch); err == nil {
		mergeNumeric(&m.cfg, patch)
	}
	return m
}

func mergeNumeric(dst *File, src filePatch) {
	if src.Tools.DefaultTimeoutSec != nil {
		dst.Tools.DefaultTimeoutSec = *src.Tools.DefaultTimeoutSec
	}
	if src.Tools.MaxTextChars != nil {
		if v := *src.Tools.MaxTextChars; v > 0 {
			dst.Tools.MaxTextChars = v
		} else {
			log.Printf("config: ignoring non-positive max_text_chars %d (keeping %d)", v, dst.Tools.MaxTextChars)
		}
	}
	if src.Routing.FlyThreshold != nil {
		dst.Routing.FlyThreshold = *src.Routing.FlyThreshold
	}
	if src.Routing.TopK != nil {
		dst.Routing.TopK = *src.Routing.TopK
	}
}

func mergeFile(dst *File, src File) {
	if src.OpenAI.ChatModel != "" {
		dst.OpenAI.ChatModel = src.OpenAI.ChatModel
	}
	if src.OpenAI.VisionModel != "" {
		dst.OpenAI.VisionModel = src.OpenAI.VisionModel
	}
	if src.OpenAI.TranscribeModel != "" {
		dst.OpenAI.TranscribeModel = src.OpenAI.TranscribeModel
	}
	if src.OpenAI.EmbeddingModel != "" {
		dst.OpenAI.EmbeddingModel = src.OpenAI.EmbeddingModel
	}
	if src.Storage.InputDir != "" {
		dst.Storage.InputDir = src.Storage.InputDir
	}
	if src.Storage.ArtifactDir != "" {
		dst.Storage.ArtifactDir = src.Storage.ArtifactDir
	}
	if src.Storage.IndexDir != "" {
		dst.Storage.IndexDir = src.Storage.IndexDir
	}
	if src.Tools.DefaultTimeoutSec > 0 {
		dst.Tools.DefaultTimeoutSec = src.Tools.DefaultTimeoutSec
	}
	if src.Tools.MaxTextChars > 0 {
		dst.Tools.MaxTextChars = src.Tools.MaxTextChars
	}
	if src.Routing.FlyThreshold > 0 {
		dst.Routing.FlyThreshold = src.Routing.FlyThreshold
	}
	if src.Routing.TopK > 0 {
		dst.Routing.TopK = src.Routing.TopK
	}
}

func (m *Manager) Root() string {
	return m.root
}

func (m *Manager) Config() File {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

func (m *Manager) RoutingPath() string {
	return filepath.Join(m.root, "configs", "routing.yaml")
}

func (m *Manager) Routing() RoutingState {
	m.mu.Lock()
	defer m.mu.Unlock()
	path := m.RoutingPath()
	st, err := os.Stat(path)
	if err == nil && m.routingInit && st.ModTime().Equal(m.routingMtime) {
		return m.routingCache
	}
	if err == nil {
		if data, readErr := os.ReadFile(path); readErr == nil {
			var doc routingDoc
			if yaml.Unmarshal(data, &doc) == nil && doc.Routing.Mode != "" {
				mode := normalizeMode(doc.Routing.Mode)
				if mode == "" {
					log.Printf("config: ignoring invalid routing mode %q in %s (expected normal or fly)", doc.Routing.Mode, path)
					fallback := RoutingState{Mode: "normal", Version: doc.Routing.Version}
					if fallback.Version <= 0 {
						fallback.Version = 1
					}
					m.routingCache = fallback
					m.routingMtime = st.ModTime()
					m.routingInit = true
					return fallback
				}
				doc.Routing.Mode = mode
				m.routingCache = doc.Routing
				m.routingMtime = st.ModTime()
				m.routingInit = true
				return m.routingCache
			}
		}
	}
	if m.routingInit {
		return m.routingCache
	}
	return RoutingState{Mode: "normal", Version: 1}
}

func normalizeMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "normal" && mode != "fly" {
		return ""
	}
	return mode
}

func (m *Manager) SetRoutingMode(mode string) (RoutingState, error) {
	normalized := normalizeMode(mode)
	if normalized == "" {
		return RoutingState{}, fmt.Errorf("invalid routing mode %q: expected normal or fly", mode)
	}
	mode = normalized
	m.mu.Lock()
	defer m.mu.Unlock()

	current := RoutingState{Mode: "normal", Version: 1}
	if data, err := os.ReadFile(m.RoutingPath()); err == nil {
		var doc routingDoc
		if yaml.Unmarshal(data, &doc) == nil && doc.Routing.Mode != "" {
			current = doc.Routing
			if normalized := normalizeMode(current.Mode); normalized != "" {
				current.Mode = normalized
			}
		}
	}
	next := RoutingState{Mode: mode, Version: current.Version + 1}
	data, err := yaml.Marshal(routingDoc{Routing: next})
	if err != nil {
		return RoutingState{}, err
	}
	dir := filepath.Dir(m.RoutingPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return RoutingState{}, err
	}
	tmp, err := os.CreateTemp(dir, "routing-*.tmp")
	if err != nil {
		return RoutingState{}, err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return RoutingState{}, err
	}
	if err := tmp.Chmod(0o644); err != nil {
		cleanup()
		return RoutingState{}, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return RoutingState{}, err
	}
	if err := os.Rename(tmpName, m.RoutingPath()); err != nil {
		os.Remove(tmpName)
		return RoutingState{}, err
	}
	m.routingCache = next
	m.routingInit = true
	if st, err := os.Stat(m.RoutingPath()); err == nil {
		m.routingMtime = st.ModTime()
	}
	return next, nil
}
