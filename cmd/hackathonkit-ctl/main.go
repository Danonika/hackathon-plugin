package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/control"
	"hackathonkit.local/agentkit/internal/learning"
)

const version = "0.7.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "ingest":
		err = cmdIngest(os.Args[2:])
	case "sessions":
		err = cmdSessions(os.Args[2:])
	case "daemon":
		err = cmdDaemon(os.Args[2:])
	case "worktree":
		err = cmdWorktree(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "tui":
		err = cmdTUI(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("hackathonkit-ctl " + version)
		return
	case "help", "--help", "-h":
		usage()
		return
	default:
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`hackathonkit-ctl - Hackathon Agent Kit control plane

Usage:
  hackathonkit-ctl ingest [--root DIR]              replay JSONL logs into SQLite
  hackathonkit-ctl sessions list [--limit N]        list sessions
  hackathonkit-ctl sessions show <id>               session detail (JSON)
  hackathonkit-ctl sessions resume <id>             handoff block for a new session
  hackathonkit-ctl daemon start|stop|status         background ingest + loopback API
  hackathonkit-ctl worktree create|list|remove      git worktree lifecycle
  hackathonkit-ctl run --task "..." [--worktree]   run codex exec in a session
  hackathonkit-ctl tui                              live dashboard
  hackathonkit-ctl version
`)
}

func repoRoot() string {
	root := config.DiscoverRoot()
	config.LoadDotEnv(root)
	return root
}

func openStore(root string) (*control.Store, error) {
	return control.Open(control.DefaultPath(root))
}

func learningStore(root string) *learning.Store {
	return learning.NewStore(filepath.Join(root, "data", "index", "learnings.json"), learning.ProjectKey(root))
}

func cmdIngest(args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	root := fs.String("root", "", "repository root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r := *root
	if r == "" {
		r = repoRoot()
	}
	store, err := openStore(r)
	if err != nil {
		return err
	}
	defer store.Close()
	result, err := store.Ingest(context.Background(), r)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func cmdSessions(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hackathonkit-ctl sessions list|show|resume")
	}
	root := repoRoot()
	store, err := openStore(root)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()

	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("sessions list", flag.ExitOnError)
		limit := fs.Int("limit", 20, "maximum sessions")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		sessions, err := store.Sessions(ctx, *limit)
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			fmt.Println("no sessions recorded; run a Codex session with the plugin or: hackathonkit-ctl ingest")
			return nil
		}
		fmt.Printf("%-28s %-8s %-6s %-20s %s\n", "ID", "STATUS", "RISK", "UPDATED", "TASK")
		for _, sess := range sessions {
			task := sess.Task
			if len(task) > 40 {
				task = task[:40] + "..."
			}
			fmt.Printf("%-28s %-8s %-6.1f %-20s %s\n", sess.ID, sess.Status, sess.RiskScore, sess.UpdatedAt.Format("2006-01-02 15:04:05"), task)
		}
		return nil
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: hackathonkit-ctl sessions show <id>")
		}
		sess, err := store.Session(ctx, args[1])
		if err != nil {
			return err
		}
		events, err := store.Events(ctx, sess.ID, 50)
		if err != nil {
			return err
		}
		outputs, err := store.Outputs(ctx, sess.ID, 10)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"session": sess, "events": events, "outputs": outputs})
	case "resume":
		if len(args) < 2 {
			return fmt.Errorf("usage: hackathonkit-ctl sessions resume <id>")
		}
		handoff, err := control.BuildHandoff(ctx, store, learningStore(root), root, args[1], 6)
		if err != nil {
			return err
		}
		fmt.Print(control.RenderHandoff(handoff))
		return nil
	default:
		return fmt.Errorf("unknown sessions subcommand %q", args[0])
	}
}

func cmdWorktree(args []string) error {
	root := repoRoot()
	if len(args) == 0 {
		return fmt.Errorf("usage: hackathonkit-ctl worktree create|list|remove")
	}
	base := filepath.Join(root, "data", "worktrees")
	switch args[0] {
	case "list":
		return runGit(root, "worktree", "list")
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: hackathonkit-ctl worktree create <slug>")
		}
		slug := slugify(args[1])
		dir := filepath.Join(base, slug)
		if err := os.MkdirAll(base, 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(dir); err == nil {
			return fmt.Errorf("worktree already exists: %s", dir)
		}
		branch := "hackathonkit/" + slug
		if err := runGit(root, "worktree", "add", "-b", branch, dir); err != nil {
			// Branch may already exist; retry without creating it.
			if err := runGit(root, "worktree", "add", dir, branch); err != nil {
				return err
			}
		}
		fmt.Println(dir)
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: hackathonkit-ctl worktree remove <slug>")
		}
		dir := filepath.Join(base, slugify(args[1]))
		return runGit(root, "worktree", "remove", "--force", dir)
	default:
		return fmt.Errorf("unknown worktree subcommand %q", args[0])
	}
}

func newCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func runGit(root string, args ...string) error {
	cmd := newCommand("git", args...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "session"
	}
	return out
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	task := fs.String("task", "", "task description passed to codex exec")
	slug := fs.String("slug", "", "session slug (default: derived from the task)")
	useWorktree := fs.Bool("worktree", false, "create and run inside a git worktree")
	model := fs.String("model", "", "optional model override for codex exec")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*task) == "" {
		return fmt.Errorf("--task is required")
	}
	if _, err := lookPath("codex"); err != nil {
		return fmt.Errorf("codex CLI not found on PATH; install it or use the hooks-based session flow")
	}

	root := repoRoot()
	store, err := openStore(root)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()

	name := slugify(*slug)
	if name == "session" {
		name = slugify(truncate(*task, 40))
	}
	sessionID := fmt.Sprintf("s-%s-%d", time.Now().UTC().Format("20060102-150405"), os.Getpid())
	worktree := ""
	dir := root
	if *useWorktree {
		wtDir := filepath.Join(root, "data", "worktrees", name)
		if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
			return err
		}
		if err := runGit(root, "worktree", "add", "-b", "hackathonkit/"+name, wtDir); err != nil {
			return err
		}
		worktree = wtDir
		dir = wtDir
	}

	if err := store.UpsertSession(ctx, control.Session{
		ID: sessionID, Root: root, Kind: "agent", Task: *task, Status: "active", Worktree: worktree,
	}); err != nil {
		return err
	}
	_ = store.AppendEvent(ctx, control.Event{SessionID: sessionID, Type: "agent_start", Payload: map[string]any{"task": *task, "dir": dir}})

	fmt.Printf("session %s\nrunning: codex exec --cd %s %q\n", sessionID, dir, *task)
	codexArgs := []string{"exec"}
	if *model != "" {
		codexArgs = append(codexArgs, "--model", *model)
	}
	codexArgs = append(codexArgs, "--cd", dir, *task)
	cmd := newCommand("codex", codexArgs...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()
	status := "ok"
	if runErr != nil {
		status = "error"
	}
	_ = store.AppendEvent(ctx, control.Event{SessionID: sessionID, Type: "agent_output", Status: status, Payload: map[string]any{"error": fmt.Sprint(runErr)}})
	_ = store.EndSession(ctx, sessionID, time.Now().UTC())
	if _, err := store.RecomputeRisk(ctx); err != nil {
		return err
	}
	fmt.Printf("\nsession %s finished (%s)\nresume: hackathonkit-ctl sessions resume %s\n", sessionID, status, sessionID)
	return runErr
}

func truncate(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
