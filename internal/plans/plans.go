// Package plans stores staged build plans as JSON files under evals/plans/.
// The build-planner skill and the session-context hook read the same files.
package plans

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/atomicfile"
)

type Stage struct {
	ID            string   `json:"id"`
	Goal          string   `json:"goal,omitempty"`
	Status        string   `json:"status"`
	Evidence      []string `json:"evidence,omitempty"`
	TimeboxMinute int      `json:"timebox_minutes,omitempty"`
}

type Plan struct {
	Slug              string         `json:"slug"`
	Revision          int            `json:"revision"`
	Task              string         `json:"task,omitempty"`
	MandatoryScenario map[string]any `json:"mandatory_scenario,omitempty"`
	Stages            []Stage        `json:"stages"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

var DefaultStages = []Stage{
	{ID: "S0", Goal: "outcome, mandatory scenario, constraints", Status: "todo", TimeboxMinute: 20},
	{ID: "S1", Goal: "inputs inventory and research brief", Status: "todo", TimeboxMinute: 30},
	{ID: "S2", Goal: "skeleton: build and MCP handshake with one tool", Status: "todo", TimeboxMinute: 45},
	{ID: "S3", Goal: "vertical slice: mandatory scenario passes on real input", Status: "todo", TimeboxMinute: 90},
	{ID: "S4", Goal: "hardening: error paths, tests, docs", Status: "todo", TimeboxMinute: 60},
	{ID: "S5", Goal: "evaluation: project-cost gate and judge round", Status: "todo", TimeboxMinute: 60},
	{ID: "S6", Goal: "freeze: clean-room gate and final bundle", Status: "todo", TimeboxMinute: 30},
}

var validStatus = map[string]bool{
	"todo": true, "doing": true, "done": true, "failed": true, "cut": true,
}

func ValidStatus(status string) bool {
	return validStatus[NormalizeStatus(status)]
}

// NormalizeStatus canonicalizes a stage status for storage and comparison.
func NormalizeStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func Dir(root string) string {
	return filepath.Join(root, "evals", "plans")
}

func Path(root, slug string) string {
	return filepath.Join(Dir(root), Slug(slug)+".json")
}

// Slug normalizes a plan name into a safe file stem.
func Slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r >= 'а' && r <= 'я', r == 'ё':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "plan"
	}
	if runes := []rune(out); len(runes) > 60 {
		// Reserve room for "-"+hash so the result stays within 60 runes and
		// re-slugging an already-normalized value is a no-op.
		suffix := "-" + shortHash(out)
		keep := 60 - len([]rune(suffix))
		if keep < 1 {
			keep = 1
		}
		trimmed := strings.Trim(string(runes[:keep]), "-")
		out = trimmed + suffix
	}
	return strings.Trim(out, "-")
}

func shortHash(value string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return fmt.Sprintf("%08x", h.Sum32())
}

func Load(root, slug string) (Plan, error) {
	path := Path(root, slug)
	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, err
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return Plan{}, fmt.Errorf("plan %s: %w", path, err)
	}
	if plan.Slug == "" {
		plan.Slug = Slug(slug)
	}
	return plan, nil
}

// Latest returns the most recently updated plan, if any.
// LatestErr also reports read/parse failures so callers can distinguish "no
// plan" from "the newest plan is unreadable".
func LatestErr(root string) (Plan, bool, error) {
	dir := Dir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Plan{}, false, nil
		}
		return Plan{}, false, err
	}
	var best Plan
	var bestTime time.Time
	var firstErr error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			if firstErr == nil {
				firstErr = infoErr
			}
			continue
		}
		if info.ModTime().Before(bestTime) {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			if firstErr == nil {
				firstErr = readErr
			}
			continue
		}
		var plan Plan
		if json.Unmarshal(data, &plan) != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("plan %s is corrupt", entry.Name())
			}
			continue
		}
		// Prefer the semantic timestamp; fall back to the file mtime when it is
		// absent or unparseable (e.g. after a bulk copy).
		candidate := info.ModTime()
		if !plan.UpdatedAt.IsZero() {
			candidate = plan.UpdatedAt
		}
		if candidate.Before(bestTime) {
			continue
		}
		best, bestTime = plan, candidate
	}
	if bestTime.IsZero() {
		return Plan{}, false, firstErr
	}
	return best, true, firstErr
}

func Latest(root string) (Plan, bool) {
	entries, err := os.ReadDir(Dir(root))
	if err != nil {
		return Plan{}, false
	}
	best := Plan{}
	bestTime := time.Time{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || info.ModTime().Before(bestTime) {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(Dir(root), entry.Name()))
		if readErr != nil {
			continue
		}
		var plan Plan
		if json.Unmarshal(data, &plan) != nil {
			continue
		}
		best, bestTime = plan, info.ModTime()
	}
	if bestTime.IsZero() {
		return Plan{}, false
	}
	return best, true
}

func Save(root string, plan Plan) error {
	slug := strings.TrimSpace(plan.Slug)
	if slug == "" {
		return fmt.Errorf("plan slug is required")
	}
	plan.Slug = Slug(slug)
	if plan.Revision < 1 {
		plan.Revision = 1
	}
	if plan.Stages == nil {
		plan.Stages = []Stage{}
	}
	plan.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	path := Path(root, plan.Slug)
	return atomicfile.Write(path, append(data, '\n'), atomicfile.ModeState)
}

func CurrentStage(plan Plan) (Stage, bool) {
	for _, stage := range plan.Stages {
		status := NormalizeStatus(stage.Status)
		if status != "done" && status != "cut" {
			return stage, true
		}
	}
	return Stage{}, false
}

// Summary is a one-line description used by hooks and the CLI.
func Summary(plan Plan) string {
	stage, ok := CurrentStage(plan)
	if !ok {
		return fmt.Sprintf("plan: %s r%d all stages complete", plan.Slug, plan.Revision)
	}
	return fmt.Sprintf("plan: %s r%d stage %s (%s)", plan.Slug, plan.Revision, stage.ID, stage.Status)
}
