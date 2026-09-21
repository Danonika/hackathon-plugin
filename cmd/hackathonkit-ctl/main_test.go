package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"hackathonkit.local/agentkit/internal/control"
	"hackathonkit.local/agentkit/internal/learning"
	"hackathonkit.local/agentkit/internal/plans"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	return <-done
}

func tempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HACKATHONKIT_REPO_ROOT", root)
	return root
}

func gitRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "config", "user.email", "test@example.com")
	gitRun(t, root, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "README.md")
	gitRun(t, root, "commit", "-q", "-m", "init")
}

func fakeCodex(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	content := "#!/bin/sh\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func withPath(t *testing.T, dirs ...string) {
	t.Helper()
	t.Setenv("PATH", strings.Join(append(dirs, os.Getenv("PATH")), string(os.PathListSeparator)))
}

func seedSessionStore(t *testing.T, root string) {
	t.Helper()
	store, err := control.Open(control.DefaultPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	sessions := []control.Session{
		{ID: "s-1", Root: root, Kind: "agent", Task: "short task", Status: "active", RiskScore: 12.5, RiskReasons: []string{"tool errors"}},
		{ID: "s-2", Root: root, Kind: "agent", Task: strings.Repeat("long task ", 6), Status: "ended", RiskScore: 3},
	}
	for _, sess := range sessions {
		if err := store.UpsertSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AppendEvent(ctx, control.Event{SessionID: "s-1", Type: "tool_use", Tool: "system.context", Status: "error"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(ctx, control.Event{SessionID: "s-1", Type: "eval_decision", Payload: map[string]any{"decision": "accept"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendOutput(ctx, control.Output{SessionID: "s-1", Kind: "report", Ref: "artifact-1", Summary: "first output"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunDispatch(t *testing.T) {
	root := tempRoot(t)
	cases := []struct {
		args []string
		want int
		out  string
	}{
		{nil, 1, "Usage:"},
		{[]string{"version"}, 0, "hackathonkit-ctl " + version},
		{[]string{"--version"}, 0, version},
		{[]string{"-h"}, 0, "Usage:"},
		{[]string{"bogus"}, 1, "Usage:"},
		{[]string{"sessions"}, 1, ""},
		{[]string{"daemon"}, 1, ""},
		{[]string{"run"}, 1, ""},
	}
	for _, tc := range cases {
		var out strings.Builder
		var errOut strings.Builder
		code := run(tc.args, &out, &errOut)
		if code != tc.want {
			t.Fatalf("run(%v) = %d, want %d (stderr=%s)", tc.args, code, tc.want, errOut.String())
		}
		if tc.out != "" && !strings.Contains(out.String(), tc.out) {
			t.Fatalf("run(%v) output = %q, want substring %q", tc.args, out.String(), tc.out)
		}
	}

	ingestOut := captureStdout(t, func() {
		if code := run([]string{"ingest", "--root", root}, io.Discard, io.Discard); code != 0 {
			t.Fatalf("run(ingest) = %d", code)
		}
	})
	if !strings.Contains(ingestOut, `"sessions"`) {
		t.Fatalf("ingest output = %q", ingestOut)
	}
}

func TestUsageOutput(t *testing.T) {
	var buf strings.Builder
	usage(&buf)
	if !strings.Contains(buf.String(), "hackathonkit-ctl ingest") {
		t.Fatalf("usage = %q", buf.String())
	}
}

func TestRepoRootOpenStoreLearningStore(t *testing.T) {
	root := tempRoot(t)
	if got := repoRoot(); got != root {
		t.Fatalf("repoRoot = %q, want %q", got, root)
	}
	store, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := os.Stat(control.DefaultPath(root)); err != nil {
		t.Fatalf("store file: %v", err)
	}
	learn := learningStore(root)
	if want := filepath.Join(root, "data", "index", "learnings.json"); learn.Path() != want {
		t.Fatalf("learningStore path = %q, want %q", learn.Path(), want)
	}
}

func TestCmdIngestJSON(t *testing.T) {
	root := t.TempDir()
	logs := filepath.Join(root, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"sessions.jsonl":       `{"session_id":"s-1","ts":"2026-01-01T00:00:00Z","type":"session_start","root":"` + root + `"}` + "\n" + `{"session_id":"s-2","ts":"2026-01-01T00:10:00Z","type":"session_end"}` + "\n",
		"session-events.jsonl": `{"session_id":"s-1","ts":"2026-01-01T00:01:00Z","tool":"system.context"}` + "\n",
		"mcp-calls.jsonl":      `{"ts":"2026-01-01T00:02:00Z","tool":"files.describe","status":"ok","duration_ms":3}` + "\n",
		"fly-feedback.jsonl":   `{"ts":"2026-01-01T00:03:00Z","task":"x","chosen":["data.inspect"]}` + "\n",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(logs, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "evals", "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "evals", "decisions", "d1.json"), []byte(`{"ts":"2026-01-01T00:04:00Z","decision":"accept"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := cmdIngest([]string{"--root", root}); err != nil {
			t.Fatalf("cmdIngest: %v", err)
		}
	})
	var result control.IngestResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("ingest JSON = %q: %v", out, err)
	}
	if result.Sessions != 2 || result.Events != 5 || result.Files != 5 {
		t.Fatalf("ingest result = %+v", result)
	}
	store, err := control.Open(control.DefaultPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Session(context.Background(), "s-2"); err != nil {
		t.Fatalf("ingested session missing: %v", err)
	}
}

func TestCmdIngestReadError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "logs", "sessions.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cmdIngest([]string{"--root", root}); err == nil {
		t.Fatal("cmdIngest with directory-based JSONL must fail")
	}
}

func TestCmdIngestUsesRepoRoot(t *testing.T) {
	root := tempRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdIngest(nil); err != nil {
			t.Fatalf("cmdIngest: %v", err)
		}
	})
	if !strings.Contains(out, `"sessions"`) {
		t.Fatalf("ingest output = %q", out)
	}
}

func TestCmdSessionsList(t *testing.T) {
	root := tempRoot(t)
	seedSessionStore(t, root)
	out := captureStdout(t, func() {
		if err := cmdSessions([]string{"list", "--limit", "5"}); err != nil {
			t.Fatalf("cmdSessions list: %v", err)
		}
	})
	if !strings.Contains(out, "ID") || !strings.Contains(out, "s-1") || !strings.Contains(out, "s-2") {
		t.Fatalf("list output = %q", out)
	}
	if !strings.Contains(out, "...") {
		t.Fatalf("long task not truncated: %q", out)
	}
}

func TestCmdSessionsListEmpty(t *testing.T) {
	tempRoot(t)
	out := captureStdout(t, func() {
		if err := cmdSessions([]string{"list"}); err != nil {
			t.Fatalf("cmdSessions list: %v", err)
		}
	})
	if !strings.Contains(out, "no sessions recorded") {
		t.Fatalf("empty list output = %q", out)
	}
}

func TestCmdSessionsShowAndResume(t *testing.T) {
	root := tempRoot(t)
	seedSessionStore(t, root)

	learn := learning.NewStore(filepath.Join(root, "data", "index", "learnings.json"), learning.ProjectKey(root))
	for i := 0; i < 2; i++ {
		if _, err := learn.Record(context.Background(), learning.RecordInput{
			Trigger: "when testing sessions resume",
			Insight: "seed the learning store before calling resume",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := plans.Save(root, plans.Plan{
		Slug:   "demo-plan",
		Stages: []plans.Stage{{ID: "S0", Goal: "ship it", Status: "doing"}},
	}); err != nil {
		t.Fatal(err)
	}

	show := captureStdout(t, func() {
		if err := cmdSessions([]string{"show", "s-1"}); err != nil {
			t.Fatalf("cmdSessions show: %v", err)
		}
	})
	var payload struct {
		Session control.Session  `json:"session"`
		Events  []control.Event  `json:"events"`
		Outputs []control.Output `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(show), &payload); err != nil {
		t.Fatalf("show JSON = %q: %v", show, err)
	}
	if payload.Session.ID != "s-1" || len(payload.Events) != 2 || len(payload.Outputs) != 1 {
		t.Fatalf("show payload = %+v", payload)
	}

	resume := captureStdout(t, func() {
		if err := cmdSessions([]string{"resume", "s-1"}); err != nil {
			t.Fatalf("cmdSessions resume: %v", err)
		}
	})
	for _, want := range []string{"Session handoff", "s-1", "plan: demo-plan", "learnings to reuse", "pending eval decisions: 1"} {
		if !strings.Contains(resume, want) {
			t.Fatalf("resume output missing %q:\n%s", want, resume)
		}
	}

	detail := captureStdout(t, func() {
		if err := cmdSessions([]string{"show", "s-2"}); err != nil {
			t.Fatalf("cmdSessions show s-2: %v", err)
		}
	})
	if !strings.Contains(detail, "s-2") {
		t.Fatalf("detail output = %q", detail)
	}
}

func TestCmdSessionsErrors(t *testing.T) {
	tempRoot(t)
	cases := [][]string{
		nil,
		{"bogus"},
		{"show"},
		{"show", "missing"},
		{"resume"},
		{"resume", "missing"},
	}
	for _, args := range cases {
		if err := cmdSessions(args); err == nil {
			t.Fatalf("cmdSessions(%v) must fail", args)
		}
	}
}

func TestCmdWorktreeLifecycle(t *testing.T) {
	root := tempRoot(t)
	initGitRepo(t, root)

	createOut := captureStdout(t, func() {
		if err := cmdWorktree([]string{"create", "My Feature!"}); err != nil {
			t.Fatalf("worktree create: %v", err)
		}
	})
	dir := filepath.Join(root, "data", "worktrees", "my-feature")
	if !strings.Contains(createOut, dir) {
		t.Fatalf("create output = %q, want to contain %q", createOut, dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("worktree dir: %v", err)
	}
	listOut := captureStdout(t, func() {
		if err := cmdWorktree([]string{"list"}); err != nil {
			t.Fatalf("worktree list: %v", err)
		}
	})
	if !strings.Contains(listOut, "my-feature") {
		t.Fatalf("worktree list = %q", listOut)
	}

	if err := cmdWorktree([]string{"create", "My Feature!"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate create err = %v", err)
	}

	if err := cmdWorktree([]string{"remove", "My Feature!"}); err != nil {
		t.Fatalf("worktree remove: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present: %v", err)
	}

	// The branch survives worktree removal, so create must reuse it.
	captureStdout(t, func() {
		if err := cmdWorktree([]string{"create", "My Feature!"}); err != nil {
			t.Fatalf("worktree recreate: %v", err)
		}
	})
	branches := gitRun(t, root, "branch", "--list", "hackathonkit/my-feature")
	if !strings.Contains(branches, "hackathonkit/my-feature") {
		t.Fatalf("branch missing: %q", branches)
	}
}

func TestCmdWorktreeErrors(t *testing.T) {
	tempRoot(t)
	cases := [][]string{
		nil,
		{"bogus"},
		{"create"},
		{"remove"},
	}
	for _, args := range cases {
		if err := cmdWorktree(args); err == nil {
			t.Fatalf("cmdWorktree(%v) must fail", args)
		}
	}
	if err := cmdWorktree([]string{"remove", "does-not-exist"}); err == nil {
		t.Fatal("removing a missing worktree must fail")
	}
}

func TestSlugifyAndTruncate(t *testing.T) {
	slugCases := map[string]string{
		"":                "session",
		"!!!":             "session",
		"  Hello World  ": "hello-world",
		"a--b__c":         "a-b-c",
		"Feature/One":     "feature-one",
		"Уже готово":      "session",
		"v1.2.3":          "v1-2-3",
	}
	for in, want := range slugCases {
		if got := slugify(in); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if got := truncate("  hello   world  ", 20); got != "hello world" {
		t.Fatalf("truncate short = %q", got)
	}
	if got := truncate("abcdefghij", 4); got != "abcd" {
		t.Fatalf("truncate long = %q", got)
	}
	if got := truncate("hello world", 7); got != "hello w" {
		t.Fatalf("truncate boundary = %q", got)
	}
}

func TestNewCommandLookPathRunGit(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)

	if _, err := lookPath("git"); err != nil {
		t.Fatalf("lookPath(git): %v", err)
	}
	if _, err := lookPath("hackathonkit-no-such-binary-xyz"); err == nil {
		t.Fatal("lookPath of missing binary must fail")
	}
	cmd := newCommand("git", "--version")
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), "git version") {
		t.Fatalf("newCommand git --version: %v %q", err, out)
	}
	if err := runGit(root, "rev-parse", "--is-inside-work-tree"); err != nil {
		t.Fatalf("runGit: %v", err)
	}
	if err := runGit(root, "definitely-not-a-git-command"); err == nil {
		t.Fatal("runGit with unknown subcommand must fail")
	}
}

func TestCmdRunRequiresTask(t *testing.T) {
	tempRoot(t)
	if err := cmdRun(nil); err == nil || !strings.Contains(err.Error(), "--task is required") {
		t.Fatalf("cmdRun err = %v", err)
	}
}

func TestCmdRunWithoutCodex(t *testing.T) {
	root := tempRoot(t)
	t.Setenv("PATH", t.TempDir())
	err := cmdRun([]string{"--task", "do something"})
	if err == nil || !strings.Contains(err.Error(), "codex CLI not found") {
		t.Fatalf("cmdRun err = %v", err)
	}
	if _, statErr := os.Stat(control.DefaultPath(root)); statErr == nil {
		t.Fatal("cmdRun should not have opened the store without codex")
	}
}

func TestCmdRunSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub requires a POSIX shell")
	}
	root := tempRoot(t)
	withPath(t, fakeCodex(t, 0))
	out := captureStdout(t, func() {
		if err := cmdRun([]string{"--task", "Fix the login bug", "--slug", "My Fix", "--model", "gpt-test"}); err != nil {
			t.Fatalf("cmdRun: %v", err)
		}
	})
	if !strings.Contains(out, "running: codex exec") || !strings.Contains(out, "finished (ok)") {
		t.Fatalf("cmdRun output = %q", out)
	}
	fields := strings.Fields(out)
	sessionID := fields[1]
	store, err := control.Open(control.DefaultPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sess, err := store.Session(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("session missing: %v", err)
	}
	if sess.Status != "ended" || sess.Task != "Fix the login bug" {
		t.Fatalf("session = %+v", sess)
	}
}

func TestCmdRunCodexFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub requires a POSIX shell")
	}
	tempRoot(t)
	withPath(t, fakeCodex(t, 3))
	out := captureStdout(t, func() {
		if err := cmdRun([]string{"--task", "Break things"}); err == nil {
			t.Fatal("cmdRun must surface the codex failure")
		}
	})
	if !strings.Contains(out, "finished (error)") {
		t.Fatalf("cmdRun failure output = %q", out)
	}
	if !strings.Contains(out, "resume: hackathonkit-ctl sessions resume") {
		t.Fatalf("cmdRun failure output = %q", out)
	}
}

func TestCmdRunWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub requires a POSIX shell")
	}
	root := tempRoot(t)
	initGitRepo(t, root)
	withPath(t, fakeCodex(t, 0))
	out := captureStdout(t, func() {
		if err := cmdRun([]string{"--task", "Add feature", "--worktree"}); err != nil {
			t.Fatalf("cmdRun --worktree: %v", err)
		}
	})
	worktreeDir := filepath.Join(root, "data", "worktrees", "add-feature")
	if _, err := os.Stat(worktreeDir); err != nil {
		t.Fatalf("worktree dir: %v", err)
	}
	store, err := control.Open(control.DefaultPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessions, err := store.Sessions(context.Background(), 10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %+v err=%v", sessions, err)
	}
	if sessions[0].Worktree != worktreeDir {
		t.Fatalf("session worktree = %q, want %q", sessions[0].Worktree, worktreeDir)
	}
	if !strings.Contains(out, worktreeDir) {
		t.Fatalf("cmdRun output = %q", out)
	}
}

func TestCmdRunWorktreeFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub requires a POSIX shell")
	}
	tempRoot(t)
	withPath(t, fakeCodex(t, 0))
	if err := cmdRun([]string{"--task", "no git here", "--worktree"}); err == nil {
		t.Fatal("cmdRun --worktree outside a git repo must fail")
	}
}

func TestTUIModelLoadViewAndUpdate(t *testing.T) {
	root := tempRoot(t)
	seedSessionStore(t, root)
	store, err := control.Open(control.DefaultPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	model := newTUIModel(store, root)
	if len(model.sessions) != 2 {
		t.Fatalf("model sessions = %+v", model.sessions)
	}
	if model.status == "" {
		t.Fatal("model status is empty")
	}
	view := model.View()
	for _, want := range []string{"Hackathon Agent Kit control plane", "s-1", "EVENTS", "no events yet"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if model.Init() == nil || tickCmd() == nil {
		t.Fatal("tick commands must not be nil")
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(tuiModel)
	if model.width != 120 || model.height != 40 {
		t.Fatalf("window size not applied: %+v", model)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model = updated.(tuiModel)
	if model.selected != 1 {
		t.Fatalf("j selection = %d", model.selected)
	}
	if view := model.View(); !strings.Contains(view, "system.context") || !strings.Contains(view, "error") {
		t.Fatalf("selected view = %q", view)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	model = updated.(tuiModel)
	if model.selected != 0 {
		t.Fatalf("k selection = %d", model.selected)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(tuiModel)
	if model.selected != 1 {
		t.Fatalf("down selection = %d", model.selected)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(tuiModel)
	if model.selected != 0 {
		t.Fatalf("up selection = %d", model.selected)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	model = updated.(tuiModel)
	if len(model.sessions) != 2 {
		t.Fatalf("refresh sessions = %+v", model.sessions)
	}
	updated, _ = model.Update(refreshMsg{})
	model = updated.(tuiModel)
	if len(model.sessions) != 2 {
		t.Fatalf("tick refresh sessions = %+v", model.sessions)
	}
	_, quitCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if quitCmd == nil {
		t.Fatal("q must return a quit command")
	}
}

func TestTUIModelEmptyAndErrorStates(t *testing.T) {
	root := tempRoot(t)
	store, err := control.Open(control.DefaultPath(root))
	if err != nil {
		t.Fatal(err)
	}
	empty := newTUIModel(store, root)
	if view := empty.View(); !strings.Contains(view, "No sessions recorded yet.") {
		t.Fatalf("empty view = %q", view)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	broken := newTUIModel(store, root)
	broken.load()
	if view := broken.View(); !strings.Contains(view, "error:") {
		t.Fatalf("broken view = %q", view)
	}
}

func TestRiskStyle(t *testing.T) {
	cases := []struct {
		score float64
		style string
	}{
		{60, badStyle.Render("x")},
		{50, badStyle.Render("x")},
		{25, warnStyle.Render("x")},
		{20, warnStyle.Render("x")},
		{5, okStyle.Render("x")},
	}
	for _, tc := range cases {
		if got := riskStyle(tc.score).Render("x"); got != tc.style {
			t.Fatalf("riskStyle(%v) = %q, want %q", tc.score, got, tc.style)
		}
	}
}

func TestWorktreeSlugMatchesRunSlug(t *testing.T) {
	if slugify("Add feature") != "add-feature" {
		t.Fatal("slugify drift")
	}
	if fmt.Sprint(slugify("A B")) != "a-b" {
		t.Fatal("slugify drift for spaces")
	}
}
