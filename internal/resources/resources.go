// Package resources models which external API resources the user has, asks
// for their answers once, persists them (booleans only - secrets stay in .env)
// and reports how each provider maps to the harness's execution variants.
package resources

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/atomicfile"
	"hackathonkit.local/agentkit/internal/filelock"
)

// Def describes one externally billable provider the harness can use.
type Def struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	EnvKeys []string `json:"env_keys"`
	Manual  bool     `json:"manual"`
}

var defs = []Def{
	{ID: "openai_api", Label: "OpenAI API (chat, vision, whisper, embeddings)", EnvKeys: []string{"OPENAI_API_KEY"}},
	{ID: "nvidia_api", Label: "NVIDIA API (LLM, ASR/voices, vision)", EnvKeys: []string{"NVIDIA_API_KEY"}},
	{ID: "telegram", Label: "Telegram bot (project notifications)", EnvKeys: []string{"TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID"}},
	{ID: "postgres", Label: "Postgres (control-plane metadata)", EnvKeys: []string{"POSTGRES_DSN"}},
	{ID: "minio", Label: "MinIO (artifact mirroring)", EnvKeys: []string{"MINIO_ENDPOINT", "MINIO_ACCESS_KEY", "MINIO_SECRET_KEY"}},
	{ID: "codex_pro", Label: "ChatGPT/Codex Pro subscription (agent reasoning)", Manual: true},
}

// Track is one hackathon track the harness can plan for.
type Track struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var tracks = []Track{
	{ID: "01-energy", Label: "Energy - early equipment fault detection"},
	{ID: "02-finance", Label: "Finance - bank employee AI assistant"},
	{ID: "03-management", Label: "Management - executive AI assistant"},
	{ID: "04-telecom", Label: "Telecom - client problem diagnosis"},
	{ID: "05-logistics", Label: "Logistics - delay forecast and dispatcher assistant"},
	{ID: "06-creative-media", Label: "Creative media - Kazakh/Russian speech, subtitles, summaries"},
	{ID: "07-education", Label: "Education - personal AI tutor"},
	{ID: "08-innovation", Label: "Innovation - universal multimodal industry agent"},
	{ID: "09-communications", Label: "Communications - call/chat analytics"},
	{ID: "10-retail", Label: "Retail - demand forecast and assortment"},
}

// ProfileVersion is the schema version of configs/resources.json.
const ProfileVersion = 1

// Profile stores the user's answers. It never contains secrets.
type Profile struct {
	Version   int             `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
	Track     string          `json:"track,omitempty"`
	CodexPro  bool            `json:"codex_pro"`
	Claimed   map[string]bool `json:"claimed"`
	Note      string          `json:"note,omitempty"`
}

// Status merges detection from the environment with the user's answers.
type Status struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	EnvKeys     []string `json:"env_keys"`
	Manual      bool     `json:"manual"`
	Detected    bool     `json:"detected"`
	Claimed     bool     `json:"claimed"`
	Ready       bool     `json:"ready"`
	MissingKeys []string `json:"missing_keys,omitempty"`
	Hint        string   `json:"hint,omitempty"`
}

// knownProvider reports whether id is a defined provider.
func knownProvider(id string) bool {
	for _, d := range defs {
		if d.ID == id {
			return true
		}
	}
	return false
}

// Defs returns the provider definitions.
func Defs() []Def {
	out := make([]Def, len(defs))
	for i, def := range defs {
		def.EnvKeys = append([]string(nil), def.EnvKeys...)
		out[i] = def
	}
	return out
}

// Tracks returns the known hackathon tracks.
func Tracks() []Track {
	out := make([]Track, len(tracks))
	copy(out, tracks)
	return out
}

// LookupTrack normalizes a track reference (full id, slug, leading number or
// label) to its canonical id.
func LookupTrack(ref string) (Track, bool) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return Track{}, false
	}
	number := ref
	slugRef := ""
	if i := strings.IndexAny(number, "- "); i > 0 {
		slugRef = strings.TrimSpace(number[i+1:])
		number = number[:i]
	}
	if len(number) == 1 && number >= "0" && number <= "9" {
		number = "0" + number
	}
	matches := 0
	var partial Track
	for _, t := range tracks {
		prefix, slug, ok := strings.Cut(t.ID, "-")
		if !ok {
			continue
		}
		label := strings.ToLower(t.Label)
		switch {
		case t.ID == ref,
			slug == ref,
			slugRef != "" && slug == slugRef,
			slugRef == "" && number == prefix,
			label == ref:
			return t, true
		}
		if len(ref) >= 4 && strings.Contains(label, ref) {
			partial = t
			matches++
		}
	}
	return partial, matches == 1
}

// Path returns the profile location under the repository root.
func Path(root string) string {
	return filepath.Join(root, "configs", "resources.json")
}

// Load reads the profile. A missing file is not an error.
func Load(root string) (Profile, bool, error) {
	data, err := os.ReadFile(Path(root))
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, false, nil
		}
		return Profile{}, false, err
	}
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, false, fmt.Errorf("configs/resources.json is not valid JSON: %w", err)
	}
	if profile.Version != 0 && profile.Version != ProfileVersion {
		return Profile{}, false, fmt.Errorf("configs/resources.json uses unsupported version %d (this build supports %d)", profile.Version, ProfileVersion)
	}
	if profile.Claimed == nil {
		profile.Claimed = map[string]bool{}
	}
	// Drop hand-edited/legacy provider ids and keep CodexPro consistent with the
	// Claimed map so the two representations cannot diverge.
	for id := range profile.Claimed {
		if !knownProvider(id) {
			delete(profile.Claimed, id)
		}
	}
	profile.CodexPro = profile.Claimed["codex_pro"] || profile.CodexPro
	profile.Claimed["codex_pro"] = profile.CodexPro
	// Normalize a persisted track reference (mirroring Save) so a hand-edited
	// or legacy value cannot propagate an invalid track into routing.variant.
	if track, ok := LookupTrack(profile.Track); ok {
		profile.Track = track.ID
	} else {
		profile.Track = ""
	}
	return profile, true, nil
}

// Update serializes profile read-modify-write for both MCP tools and CLI callers.
func Update(root string, mutate func(*Profile) error) error {
	release, err := filelock.Acquire(Path(root)+".lock", 5*time.Second)
	if err != nil {
		return err
	}
	defer release()
	profile, _, err := Load(root)
	if err != nil {
		return err
	}
	if err = mutate(&profile); err != nil {
		return err
	}
	return Save(root, profile)
}

// Save writes the profile atomically with least-privilege permissions.
func Save(root string, profile Profile) error {
	// Profile is passed by value but Claimed is a shared map; copy it so Save
	// does not mutate the caller's map.
	claimed := make(map[string]bool, len(profile.Claimed))
	for k, v := range profile.Claimed {
		claimed[k] = v
	}
	if profile.Version != 0 && profile.Version != ProfileVersion {
		return fmt.Errorf("refusing to save unsupported profile version %d (this build supports %d)", profile.Version, ProfileVersion)
	}
	profile.Claimed = claimed
	for id := range profile.Claimed {
		if !knownProvider(id) {
			delete(profile.Claimed, id)
		}
	}
	if profile.Version == 0 {
		profile.Version = ProfileVersion
	}
	profile.UpdatedAt = time.Now().UTC()
	if profile.Claimed == nil {
		profile.Claimed = map[string]bool{}
	}
	// Reconcile CodexPro and Claimed["codex_pro"] with an OR-merge (matching
	// Load and Statuses) so a caller that set only one of the two
	// representations does not have its claim silently reverted on save.
	profile.CodexPro = profile.CodexPro || profile.Claimed["codex_pro"]
	profile.Claimed["codex_pro"] = profile.CodexPro
	if track, ok := LookupTrack(profile.Track); ok {
		profile.Track = track.ID
	} else if strings.TrimSpace(profile.Track) == "" {
		profile.Track = ""
	} else {
		return fmt.Errorf("unknown track %q", profile.Track)
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(Path(root), append(data, '\n'), atomicfile.ModeState)
}

// SetAnswer records one provider answer on the profile.
func SetAnswer(profile *Profile, providerID string, value bool) error {
	if !knownProvider(providerID) {
		return fmt.Errorf("unknown provider %q", providerID)
	}
	if profile.Claimed == nil {
		profile.Claimed = map[string]bool{}
	}
	profile.Claimed[providerID] = value
	if providerID == "codex_pro" {
		profile.CodexPro = value
	}
	return nil
}

// Statuses merges environment detection with the profile answers.
func Statuses(profile Profile) []Status {
	out := make([]Status, 0, len(defs))
	for _, d := range defs {
		status := Status{
			ID:       d.ID,
			Label:    d.Label,
			EnvKeys:  append([]string{}, d.EnvKeys...),
			Manual:   d.Manual,
			Claimed:  profile.Claimed[d.ID] || (d.ID == "codex_pro" && profile.CodexPro),
			Detected: !d.Manual && envAllPresent(d.EnvKeys),
		}
		if d.Manual {
			status.Ready = status.Claimed
		} else {
			status.Ready = status.Detected
		}
		if status.Claimed && !status.Detected && !d.Manual {
			for _, key := range d.EnvKeys {
				if os.Getenv(key) == "" {
					status.MissingKeys = append(status.MissingKeys, key)
				}
			}
		}
		if len(status.MissingKeys) > 0 {
			commands := make([]string, 0, len(status.MissingKeys))
			for _, key := range status.MissingKeys {
				commands = append(commands, fmt.Sprintf("sh scripts/env-set.sh .env %s <value>", key))
			}
			status.Hint = strings.Join(commands, " && ")
		}
		out = append(out, status)
	}
	return out
}

// View renders the profile, provider statuses and next steps as shared data
// for the system.resources tool and the --resources CLI flag.
func View(profile Profile, present bool) map[string]any {
	statuses := Statuses(profile)
	data := map[string]any{
		"profile": map[string]any{
			"present":    present,
			"version":    profile.Version,
			"updated_at": profile.UpdatedAt,
			"track":      profile.Track,
			"codex_pro":  profile.CodexPro,
			"note":       profile.Note,
		},
		"providers": statuses,
		"summary":   Summary(statuses),
		"tracks":    Tracks(),
		"missing":   Missing(statuses),
	}
	next := []string{}
	if !present {
		providers := make([]string, 0, len(defs))
		for _, d := range defs {
			providers = append(providers, d.ID)
		}
		next = append(next, "ask the user about "+strings.Join(providers, ", ")+" and the hackathon track; then call system.resources_set")
	}
	for _, status := range statuses {
		if status.Hint != "" {
			next = append(next, status.Hint)
		}
	}
	if len(next) > 0 {
		data["next_steps"] = next
	}
	return data
}

// Summary renders a compact one-line view for session context.
func Summary(statuses []Status) string {
	parts := make([]string, 0, len(statuses))
	for _, s := range statuses {
		value := "no"
		if s.Ready {
			value = "yes"
		} else if s.Claimed {
			value = "claimed"
		}
		parts = append(parts, s.ID+"="+value)
	}
	return strings.Join(parts, " ")
}

// Missing returns the env keys that were claimed but are not present.
func Missing(statuses []Status) []string {
	out := []string{}
	for _, s := range statuses {
		out = append(out, s.MissingKeys...)
	}
	sort.Strings(out)
	return out
}

// Ready reports whether the provider is usable right now.
func Ready(statuses []Status, providerID string) bool {
	for _, s := range statuses {
		if s.ID == providerID {
			return s.Ready
		}
	}
	return false
}

func envAllPresent(keys []string) bool {
	for _, key := range keys {
		if os.Getenv(key) == "" {
			return false
		}
	}
	return true
}
