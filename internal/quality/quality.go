// Package quality runs the deterministic build/verify gate and stores its
// evidence under evals/quality/<sha>/gate.json.
package quality

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"hackathonkit.local/agentkit/internal/atomicfile"
)

type Check struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output,omitempty"`
}

type Report struct {
	SHA        string         `json:"sha"`
	OK         bool           `json:"ok"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Checks     []Check        `json:"checks"`
	Summary    map[string]int `json:"summary"`
}

type checkSpec struct {
	name    string
	command []string
	timeout time.Duration
}

var defaultChecks = []checkSpec{
	{"gofmt", []string{"gofmt", "-l", "internal", "cmd"}, 60 * time.Second},
	{"build", []string{"go", "build", "./..."}, 5 * time.Minute},
	{"vet", []string{"go", "vet", "./..."}, 5 * time.Minute},
	{"test", []string{"go", "test", "./..."}, 10 * time.Minute},
	{"coverage", []string{"sh", "scripts/coverage-check.sh"}, 10 * time.Minute},
	{"selftest", []string{"go", "run", "./cmd/hackathonkit-mcp", "--selftest"}, 5 * time.Minute},
	{"skills", []string{"go", "run", "./cmd/hackathonkit-mcp", "--validate-skills"}, 5 * time.Minute},
}

// Run executes the gate in the repository root. The caller decides how to
// treat a failing report.
func Run(ctx context.Context, root string) Report {
	report := Report{
		SHA:       GitSHA(root),
		StartedAt: time.Now().UTC(),
		Checks:    []Check{},
		Summary:   map[string]int{},
	}
	for _, spec := range defaultChecks {
		report.Checks = append(report.Checks, runCheck(ctx, root, spec))
	}
	report.FinishedAt = time.Now().UTC()
	report.OK = true
	for _, check := range report.Checks {
		report.Summary[check.Status]++
		if check.Status != "ok" {
			report.OK = false
		}
	}
	return report
}

func runCheck(ctx context.Context, root string, spec checkSpec) Check {
	check := Check{Name: spec.name, Command: strings.Join(spec.command, " ")}
	runCtx, cancel := context.WithTimeout(ctx, spec.timeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(runCtx, spec.command[0], spec.command[1:]...)
	cmd.Dir = root
	cmd.Env = buildEnv()
	configureProcessGroup(cmd)
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return cmd.Process.Kill()
	}
	// Bound the wait even when a grandchild keeps stdout/stderr open.
	cmd.WaitDelay = 10 * time.Second
	var captured cappedBuffer
	cmd.Stdout = &captured
	cmd.Stderr = &captured
	err := cmd.Run()
	check.DurationMS = time.Since(start).Milliseconds()
	check.Output = tail(captured.String(), 2000)
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		check.Status = "timeout"
	case errors.Is(runCtx.Err(), context.Canceled):
		check.Status = "canceled"
	case errors.Is(err, exec.ErrWaitDelay):
		// The command exited successfully; WaitDelay only expired while
		// draining output pipes a lingering grandchild kept open. Report the
		// check as passing but record the anomaly instead of hiding it behind
		// an indistinguishable "failed".
		check.Status = "ok"
		if check.Output != "" {
			check.Output += "\n"
		}
		check.Output += "note: output pipes remained open past WaitDelay"
	case err != nil:
		check.Status = "failed"
	case spec.name == "gofmt" && strings.TrimSpace(captured.String()) != "":
		check.Status = "failed"
	default:
		check.Status = "ok"
	}
	return check
}

// buildEnv returns the current environment with -buildvcs=false appended to
// GOFLAGS. The gate runs inside containers as root over bind-mounted repos
// whose .git is owned by another user; git then refuses to operate and Go's
// VCS stamping fails with exit status 128.
func buildEnv() []string {
	base := []string{}
	existing := ""
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOFLAGS=") {
			existing = strings.TrimSpace(strings.TrimPrefix(kv, "GOFLAGS="))
			continue
		}
		base = append(base, kv)
	}
	// Split quote-aware so a quoted value such as -gcflags='-m -m' stays one
	// token, and drop only a token whose flag name is exactly -buildvcs (never
	// the first half of a quoted value that merely starts with that text).
	tokens := splitQuoted(existing)
	kept := tokens[:0]
	for _, token := range tokens {
		if token == "-buildvcs" || strings.HasPrefix(token, "-buildvcs=") {
			continue
		}
		kept = append(kept, token)
	}
	merged := strings.Join(append(kept, "-buildvcs=false"), " ")
	return append(base, "GOFLAGS="+merged)
}

// splitQuoted splits a shell-like quoted string on unquoted spaces, preserving
// each token's original text (including its quotes).
func splitQuoted(s string) []string {
	var out []string
	var b strings.Builder
	quote := rune(0)
	for _, r := range s {
		switch {
		case quote != 0:
			b.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
			b.WriteRune(r)
		case r == ' ' || r == '\t' || r == '\n':
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// cappedBuffer keeps at most maxCheckOutput bytes so a chatty test run cannot
// exhaust memory before tail() trims the output.
const maxCheckOutput = 256 << 10

type cappedBuffer struct {
	buf bytes.Buffer
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if remaining := maxCheckOutput - c.buf.Len(); remaining > 0 {
		chunk := p
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		_, _ = c.buf.Write(chunk)
	}
	// Report the full input as written: a short count would look like an I/O
	// error to the child process.
	return written, nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }

func tail(text string, limit int) string {
	text = strings.TrimRight(text, "\n")
	if len(text) <= limit {
		return text
	}
	cut := len(text) - limit
	// Do not split a multi-byte rune at the cut point.
	for cut < len(text) && !utf8.RuneStart(text[cut]) {
		cut++
	}
	return "..." + text[cut:]
}

func Dir(root string) string {
	return filepath.Join(root, "evals", "quality")
}

func Path(root, sha string) string {
	sha = strings.TrimSpace(sha)
	if !validSHA(sha) {
		sha = "unknown"
	}
	return filepath.Join(Dir(root), sha, "gate.json")
}

func validSHA(sha string) bool {
	if sha == "" || sha == "." || sha == ".." || strings.ContainsAny(sha, `/\`) {
		return false
	}
	for _, r := range sha {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// Save writes the report atomically and returns the repository-relative path.
func Save(root string, report Report) (string, error) {
	path := Path(root, report.SHA)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(path, append(data, '\n'), atomicfile.ModeState); err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(rel), nil
	}
	return path, nil
}

// Latest returns the most recently written gate report.
func Latest(root string) (Report, bool) {
	report, ok, _ := LatestReport(root)
	return report, ok
}

// LatestReport also surfaces why evidence is missing or partially unreadable,
// so callers can distinguish "never ran" from "corrupt evidence".
func LatestReport(root string) (Report, bool, error) {
	entries, err := os.ReadDir(Dir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return Report{}, false, nil
		}
		return Report{}, false, err
	}
	var best Report
	var bestTime time.Time
	var firstErr error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		gatePath := filepath.Join(Dir(root), entry.Name(), "gate.json")
		gateInfo, gateErr := os.Stat(gatePath)
		if gateErr != nil {
			if firstErr == nil {
				firstErr = gateErr
			}
			continue
		}
		if gateInfo.ModTime().Before(bestTime) {
			continue
		}
		data, readErr := os.ReadFile(gatePath)
		if readErr != nil {
			if firstErr == nil {
				firstErr = readErr
			}
			continue
		}
		var report Report
		if json.Unmarshal(data, &report) != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("gate evidence for %s is corrupt", entry.Name())
			}
			continue
		}
		best, bestTime = report, gateInfo.ModTime()
	}
	if bestTime.IsZero() {
		return Report{}, false, firstErr
	}
	return best, true, firstErr
}

// GitSHA resolves HEAD without requiring a git binary.
func GitSHA(root string) string {
	gitDir := filepath.Join(root, ".git")
	if info, err := os.Stat(gitDir); err == nil && !info.IsDir() {
		// Worktree-style .git file: "gitdir: <path>".
		if raw, err := os.ReadFile(gitDir); err == nil {
			line := strings.TrimSpace(string(raw))
			if strings.HasPrefix(line, "gitdir:") {
				target := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
				if !filepath.IsAbs(target) {
					target = filepath.Join(root, target)
				}
				gitDir = target
			}
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "unknown"
	}
	value := strings.TrimSpace(string(head))
	if !strings.HasPrefix(value, "ref: ") {
		if validGitSHA(value) {
			return value
		}
		return "unknown"
	}
	ref := strings.TrimSpace(strings.TrimPrefix(value, "ref: "))
	if !strings.HasPrefix(ref, "refs/") || strings.Contains(ref, "..") {
		return "unknown"
	}
	refPath := filepath.Join(gitDir, filepath.FromSlash(ref))
	if !strings.HasPrefix(refPath, gitDir+string(filepath.Separator)) {
		return "unknown"
	}
	if data, err := os.ReadFile(refPath); err == nil {
		if sha := strings.TrimSpace(string(data)); validGitSHA(sha) {
			return sha
		}
	}
	if packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(packed), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) == 2 && parts[1] == ref && validGitSHA(parts[0]) {
				return parts[0]
			}
		}
	}
	return "unknown"
}

// validGitSHA accepts only a full hex object id (SHA-1 or SHA-256), so a junk
// or multi-line .git/HEAD cannot become an evidence path segment.
func validGitSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// FailedChecks lists failed check names in deterministic order.
func FailedChecks(report Report) []string {
	out := []string{}
	for _, check := range report.Checks {
		if check.Status != "ok" {
			out = append(out, fmt.Sprintf("%s (%s)", check.Name, check.Status))
		}
	}
	sort.Strings(out)
	return out
}
