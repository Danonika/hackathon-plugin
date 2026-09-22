package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Variant is one way to execute a capability. A variant without a provider is
// always available and acts as the deterministic fallback.
type Variant struct {
	ID       string `json:"id" yaml:"id"`
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Note     string `json:"note,omitempty" yaml:"note,omitempty"`
}

// TrackPlan is the capability playbook of one hackathon track.
type TrackPlan struct {
	ID           string   `json:"id" yaml:"-"`
	Label        string   `json:"label" yaml:"label"`
	Scenario     string   `json:"scenario,omitempty" yaml:"scenario,omitempty"`
	Capabilities []string `json:"capabilities" yaml:"capabilities"`
}

// Matrix maps capabilities to ordered variants and tracks to playbooks.
type Matrix struct {
	Capabilities map[string][]Variant `json:"capabilities" yaml:"capabilities"`
	Tracks       map[string]TrackPlan `json:"tracks" yaml:"tracks"`
}

// Decision is the chosen variant for one capability of a track.
type Decision struct {
	Capability   string    `json:"capability"`
	Chosen       Variant   `json:"chosen"`
	Reason       string    `json:"reason"`
	Degraded     bool      `json:"degraded"`
	Alternatives []Variant `json:"alternatives,omitempty"`
	Missing      []string  `json:"missing,omitempty"`
}

// MatrixPath returns the tracked matrix location.
func MatrixPath(root string) string {
	return filepath.Join(root, "configs", "resource-matrix.yaml")
}

// LoadMatrix reads configs/resource-matrix.yaml and falls back to the built-in
// defaults when the file is absent.
func LoadMatrix(root string) (Matrix, error) {
	data, err := os.ReadFile(MatrixPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultMatrix(), nil
		}
		return Matrix{}, err
	}
	var matrix Matrix
	if err := yaml.Unmarshal(data, &matrix); err != nil {
		return Matrix{}, fmt.Errorf("configs/resource-matrix.yaml is not valid YAML: %w", err)
	}
	if err := matrix.Validate(); err != nil {
		return Matrix{}, err
	}
	return matrix, nil
}

// Validate checks that every track is known and every referenced capability
// exists.
func (m Matrix) Validate() error {
	if len(m.Capabilities) == 0 || len(m.Tracks) == 0 {
		return fmt.Errorf("resource matrix is empty")
	}
	for trackID, plan := range m.Tracks {
		if _, ok := LookupTrack(trackID); !ok {
			return fmt.Errorf("resource matrix track %q is not a known track", trackID)
		}
		for _, capability := range plan.Capabilities {
			if len(m.Capabilities[capability]) == 0 {
				return fmt.Errorf("resource matrix track %s references unknown capability %q", trackID, capability)
			}
		}
	}
	return nil
}

// Plan chooses the best ready variant for every capability of a track.
func Plan(matrix Matrix, statuses []Status, trackRef string) (TrackPlan, []Decision, error) {
	track, ok := LookupTrack(trackRef)
	if !ok {
		return TrackPlan{}, nil, fmt.Errorf("unknown track %q", trackRef)
	}
	plan, ok := matrix.Tracks[track.ID]
	if !ok {
		return TrackPlan{}, nil, fmt.Errorf("track %s has no playbook in configs/resource-matrix.yaml", track.ID)
	}
	resolved := TrackPlan{ID: track.ID, Label: track.Label, Scenario: plan.Scenario, Capabilities: plan.Capabilities}
	decisions := make([]Decision, 0, len(plan.Capabilities))
	for _, capability := range plan.Capabilities {
		variants := matrix.Capabilities[capability]
		if len(variants) == 0 {
			continue
		}
		decision := Decision{Capability: capability}
		chosenIndex := -1
		for i, variant := range variants {
			if variant.Provider == "" || Ready(statuses, variant.Provider) {
				decision.Chosen = variant
				chosenIndex = i
				break
			}
		}
		if chosenIndex < 0 {
			decision.Chosen = variants[len(variants)-1]
			chosenIndex = len(variants) - 1
			decision.Degraded = true
		} else if decision.Chosen.Provider == "" && chosenIndex > 0 {
			decision.Degraded = true
		}
		if decision.Chosen.Provider == "" {
			if decision.Degraded {
				decision.Reason = "no provider configured; deterministic fallback"
			} else {
				decision.Reason = "deterministic by design"
			}
		} else {
			decision.Reason = decision.Chosen.Provider + " is ready"
		}
		for _, variant := range variants {
			if variant.ID == decision.Chosen.ID && variant.Provider == decision.Chosen.Provider {
				continue
			}
			decision.Alternatives = append(decision.Alternatives, variant)
			if variant.Provider != "" && !Ready(statuses, variant.Provider) {
				decision.Missing = append(decision.Missing, providerMissing(statuses, variant.Provider)...)
			}
		}
		decision.Missing = dedupe(decision.Missing)
		decisions = append(decisions, decision)
	}
	return resolved, decisions, nil
}

// DecisionSummary renders "capability=variant" pairs for session output.
func DecisionSummary(decisions []Decision) string {
	parts := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		parts = append(parts, decision.Capability+"="+decision.Chosen.ID)
	}
	return strings.Join(parts, " ")
}

// DegradedCapabilities lists capabilities running on a deterministic fallback
// while a provider-backed variant exists.
func DegradedCapabilities(decisions []Decision) []string {
	out := []string{}
	for _, decision := range decisions {
		if decision.Degraded {
			out = append(out, decision.Capability)
		}
	}
	return out
}

func providerMissing(statuses []Status, providerID string) []string {
	for _, status := range statuses {
		if status.ID != providerID {
			continue
		}
		if len(status.MissingKeys) > 0 {
			return status.MissingKeys
		}
		return status.EnvKeys
	}
	return nil
}

func dedupe(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

// DefaultMatrix is the built-in copy of configs/resource-matrix.yaml. The
// tracked file is authoritative at runtime; a regression test keeps the two in
// sync.
func DefaultMatrix() Matrix {
	return Matrix{
		Capabilities: map[string][]Variant{
			"arithmetic": {
				{ID: "deterministic", Note: "data.query / data.analyze compute; never use an LLM for arithmetic"},
			},
			"tabular": {
				{ID: "deterministic", Note: "data.inspect / data.query / classification.classify"},
				{ID: "openai", Provider: "openai_api", Note: "optional LLM classification and extraction of messy columns"},
			},
			"timeseries": {
				{ID: "deterministic", Note: "timeseries.analyze / timeseries.detect_anomalies"},
				{ID: "openai", Provider: "openai_api", Note: "optional LLM interpretation of anomaly context"},
			},
			"classification": {
				{ID: "deterministic", Note: "classification.classify keyword rules"},
				{ID: "openai", Provider: "openai_api", Note: "optional LLM classification for ambiguous cases"},
			},
			"retrieval": {
				{ID: "deterministic", Note: "knowledge.index / knowledge.search (TF-IDF)"},
				{ID: "openai", Provider: "openai_api", Note: "embedding-backed retrieval via the Fly chain embedder"},
			},
			"reasoning": {
				{ID: "openai", Provider: "openai_api", Note: "gpt-4o-mini summaries, causes and next-step instructions"},
				{ID: "nvidia", Provider: "nvidia_api", Note: "NVIDIA NIM chat endpoint"},
				{ID: "deterministic", Note: "templates and rules only; no generated explanations"},
			},
			"embeddings": {
				{ID: "openai", Provider: "openai_api", Note: "text-embedding-3-small"},
				{ID: "nvidia", Provider: "nvidia_api", Note: "NVIDIA embedding NIM"},
				{ID: "deterministic", Note: "hash encoder + TF-IDF"},
			},
			"asr": {
				{ID: "openai", Provider: "openai_api", Note: "whisper-1 transcription (audio.transcribe)"},
				{ID: "nvidia", Provider: "nvidia_api", Note: "NVIDIA Parakeet/Riva ASR (adapter after the probe confirms the endpoint)"},
				{ID: "deterministic", Note: "no transcription without a provider; keep the audio step as a gap"},
			},
			"vision": {
				{ID: "openai", Provider: "openai_api", Note: "gpt-4o-mini vision (vision.analyze)"},
				{ID: "nvidia", Provider: "nvidia_api", Note: "NVIDIA vision NIM"},
				{ID: "deterministic", Note: "no image analysis without a provider"},
			},
			"tts": {
				{ID: "nvidia", Provider: "nvidia_api", Note: "NVIDIA Riva TTS / voices"},
				{ID: "deterministic", Note: "text-only output"},
			},
			"notifications": {
				{ID: "telegram", Provider: "telegram", Note: "telegram.send alerts and digests"},
				{ID: "deterministic", Note: "include the message in the report instead"},
			},
			"metadata": {
				{ID: "postgres", Provider: "postgres", Note: "control-plane mirroring to Postgres"},
				{ID: "deterministic", Note: "local SQLite control plane"},
			},
			"artifacts": {
				{ID: "minio", Provider: "minio", Note: "artifact mirroring to MinIO"},
				{ID: "deterministic", Note: "local data/artifacts only"},
			},
		},
		Tracks: map[string]TrackPlan{
			"01-energy": {
				Label:        "Energy - early equipment fault detection",
				Scenario:     "telemetry -> anomalies -> probable cause -> operator checklist",
				Capabilities: []string{"timeseries", "tabular", "reasoning", "notifications"},
			},
			"02-finance": {
				Label:        "Finance - bank employee AI assistant",
				Scenario:     "regulations -> answers with citations; documents -> field extraction and contradictions",
				Capabilities: []string{"retrieval", "tabular", "reasoning"},
			},
			"03-management": {
				Label:        "Management - executive AI assistant",
				Scenario:     "documents and reports -> problems -> tasks with owners, deadlines and risks",
				Capabilities: []string{"retrieval", "reasoning", "notifications"},
			},
			"04-telecom": {
				Label:        "Telecom - client problem diagnosis",
				Scenario:     "complaint + metrics -> probable cause -> checks -> operator recommendation",
				Capabilities: []string{"tabular", "timeseries", "classification", "reasoning"},
			},
			"05-logistics": {
				Label:        "Logistics - delay forecast and dispatcher assistant",
				Scenario:     "shipment history -> delay risk -> affected shipments -> alternative plan",
				Capabilities: []string{"timeseries", "tabular", "reasoning"},
			},
			"06-creative-media": {
				Label:        "Creative media - Kazakh/Russian speech, subtitles, summaries",
				Scenario:     "video/audio -> transcription -> subtitles -> summary -> archive search",
				Capabilities: []string{"asr", "retrieval", "reasoning", "artifacts"},
			},
			"07-education": {
				Label:        "Education - personal AI tutor",
				Scenario:     "student solution -> error -> explanation -> generated exercise",
				Capabilities: []string{"reasoning", "retrieval", "classification"},
			},
			"08-innovation": {
				Label:        "Innovation - universal multimodal industry agent",
				Scenario:     "photo + documents + data -> problem -> research -> solution",
				Capabilities: []string{"vision", "retrieval", "tabular", "reasoning", "artifacts"},
			},
			"09-communications": {
				Label:        "Communications - call/chat analytics",
				Scenario:     "calls/chats -> transcription -> topic -> sentiment -> summary -> recommendations",
				Capabilities: []string{"asr", "classification", "reasoning", "notifications"},
			},
			"10-retail": {
				Label:        "Retail - demand forecast and assortment",
				Scenario:     "sales + stock + expiry -> forecast -> recommended order",
				Capabilities: []string{"timeseries", "tabular", "reasoning"},
			},
		},
	}
}
