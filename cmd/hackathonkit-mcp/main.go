package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/learning"
	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/objectstore"
	"hackathonkit.local/agentkit/internal/observability"
	"hackathonkit.local/agentkit/internal/openai"
	"hackathonkit.local/agentkit/internal/plans"
	"hackathonkit.local/agentkit/internal/quality"
	"hackathonkit.local/agentkit/internal/resources"
	"hackathonkit.local/agentkit/internal/routing/fly"
	"hackathonkit.local/agentkit/internal/skills"
	"hackathonkit.local/agentkit/internal/storage"
	"hackathonkit.local/agentkit/internal/telegram"
	"hackathonkit.local/agentkit/internal/tools"
)

const version = "0.16.77"

// redactBackendError strips credentials from driver/connection errors before
// they are logged (DSNs can appear in Postgres and MinIO errors).
func redactBackendError(err error) string {
	if err == nil {
		return ""
	}
	text := redactURLUserinfo(err.Error())
	for _, marker := range []string{
		"password=", "passwd=", "secret=", "secret_key=", "access_key=",
		"api_key=", "apikey=", "token=", "credential=",
	} {
		text = redactMarkerValues(text, marker)
	}
	return text
}

// redactURLUserinfo replaces the userinfo of any URL (scheme://user:pass@host)
// with [redacted], leaving the rest of the authority intact. Errors that echo a
// connection string verbatim (Postgres DSNs, MinIO endpoints) are the main way
// embedded credentials leak into logs.
func redactURLUserinfo(text string) string {
	var b strings.Builder
	i := 0
	for {
		idx := strings.Index(text[i:], "://")
		if idx < 0 {
			b.WriteString(text[i:])
			break
		}
		idx += i
		b.WriteString(text[i : idx+3])
		rest := text[idx+3:]
		end := len(rest)
		for j := 0; j < len(rest); j++ {
			switch rest[j] {
			case '/', '?', '#', ' ', '\t', '\n', '\r', '"', '\'':
				end = j
			}
			if end == j {
				break
			}
		}
		authority := rest[:end]
		if at := strings.LastIndex(authority, "@"); at > 0 {
			b.WriteString("[redacted]")
			b.WriteString(authority[at:])
		} else {
			b.WriteString(authority)
		}
		i = idx + 3 + end
	}
	return b.String()
}

// redactMarkerValues replaces the value that follows every occurrence of
// marker, handling quoted values (e.g. password="a b") so the whole secret is
// removed rather than only up to the first space.
func redactMarkerValues(text, marker string) string {
	const replacement = "[redacted]"
	searchFrom := 0
	for {
		idx := indexFold(text[searchFrom:], marker)
		if idx < 0 {
			break
		}
		idx += searchFrom
		valueStart := idx + len(marker)
		quote := byte(0)
		if valueStart < len(text) && (text[valueStart] == '"' || text[valueStart] == '\'') {
			quote = text[valueStart]
			valueStart++
		}
		end := valueStart
		if quote != 0 {
			for end < len(text) && text[end] != quote {
				end++
			}
		} else {
			for end < len(text) && !isValueDelimiter(text[end]) {
				end++
			}
		}
		if end == valueStart {
			// Empty value: nothing to redact; advance to avoid a loop.
			searchFrom = idx + len(marker)
			continue
		}
		text = text[:valueStart] + replacement + text[end:]
		searchFrom = valueStart + len(replacement)
	}
	return text
}

func isValueDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '&', '"', '\'':
		return true
	}
	return false
}

// indexFold is a byte-length-preserving case-insensitive substring search, so
// offsets computed on the haystack remain valid for the original string.
func indexFold(haystack, needle string) int {
	if needle == "" {
		return 0
	}
	limit := len(haystack) - len(needle)
	for i := 0; i <= limit; i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if lowerASCII(haystack[i+j]) != lowerASCII(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hackathonkit-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	selftest := fs.Bool("selftest", false, "run an in-process MCP handshake and exit")
	listTools := fs.Bool("list-tools", false, "print tool descriptors as JSON and exit")
	repoRoot := fs.String("repo-root", "", "repository root (defaults to auto-discovery)")
	setMode := fs.String("set-routing-mode", "", "set routing mode (normal|fly) and exit")
	sessionContext := fs.Bool("session-context", false, "print a compact session context block for hooks and exit")
	resourcesJSON := fs.Bool("resources", false, "print detected API resources and the saved profile as JSON and exit")
	resourcesSet := fs.String("resources-set", "", "record resource answers as comma-separated k=v pairs (provider booleans, track, note) and exit")
	validateSkills := fs.Bool("validate-skills", false, "validate the skill catalog against docs/SKILL-STANDARD.md and exit")
	qualityGate := fs.Bool("quality-gate", false, "run the build/verify gate, store evidence under evals/quality/ and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	root := *repoRoot
	if root == "" {
		root = config.DiscoverRoot()
	}
	config.LoadDotEnv(root)
	mgr := config.NewManager(root)

	if *setMode != "" {
		state, err := mgr.SetRoutingMode(*setMode)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintf(stdout, "routing mode set to %s (version %d)\n", state.Mode, state.Version)
		return 0
	}

	if *sessionContext {
		printSessionContext(root, mgr, stdout, stderr)
		return 0
	}

	if *resourcesSet != "" {
		if err := applyResourcesSet(root, *resourcesSet); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if err := printResources(root, stdout); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}

	if *resourcesJSON {
		if err := printResources(root, stdout); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}

	if *validateSkills {
		report, err := skills.Validate(root)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if !report.OK {
			return 1
		}
		return 0
	}

	if *qualityGate {
		report := quality.Run(context.Background(), root)
		path, err := quality.Save(root, report)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"report": report, "path": path}); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if !report.OK {
			return 1
		}
		return 0
	}

	cfg := mgr.Config()
	metrics := observability.NewMetrics()
	logger := observability.NewLogger(stderr)
	store := storage.New(root, cfg.Storage)
	oa := openai.New(cfg.OpenAI)
	tg := telegram.New(telegram.SettingsFromEnv())
	router := fly.NewRouter(cfg.Routing)
	router.WithMemory(filepath.Join(root, "data", "index", "fly-memory.json"))
	router.WithBrain(filepath.Join(root, "data", "index", "fly-brain.json"))
	router.WithLLM(oa)
	embedder := fly.NewChainEmbedder(oa, fly.NewHashEncoder(512))
	router.WithEmbeddings(embedder, filepath.Join(root, "data", "index", "fly-embeddings.json"), cfg.OpenAI.EmbeddingModel+"+hash512")

	var learn *learning.Store
	if cfg.Learning.Enabled {
		learn = learning.NewStore(filepath.Join(root, "data", "index", "learnings.json"), learning.ProjectKey(root))
		learn.WithEmbedder(embedder)
	}

	var meta metadata.Recorder = metadata.Disabled{}
	if !*listTools && !*selftest {
		if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
			if pg, err := metadata.OpenPostgres(context.Background(), dsn); err != nil {
				observability.Log(logger, "warn", "metadata_disabled", map[string]any{"error": redactBackendError(err)})
			} else {
				meta = pg
				observability.Log(logger, "info", "metadata_postgres_enabled", nil)
			}
		}
	}
	var objects storage.ObjectStore
	if !*listTools && !*selftest {
		if settings := objectstore.SettingsFromEnv(); settings.Endpoint != "" {
			if cli, err := objectstore.Open(context.Background(), settings); err != nil {
				observability.Log(logger, "warn", "objectstore_disabled", map[string]any{"error": redactBackendError(err)})
			} else {
				objects = cli
				observability.Log(logger, "info", "objectstore_minio_enabled", map[string]any{"bucket": settings.Bucket})
			}
		}
	}
	store.Attach(meta, objects, func(err error) {
		observability.Log(logger, "warn", "storage_backend_error", map[string]any{"error": redactBackendError(err)})
	})
	defer meta.Close()

	reg := mcp.NewRegistry()
	tools.RegisterAll(reg, tools.Deps{
		Cfg:      mgr,
		Store:    store,
		OpenAI:   oa,
		Metrics:  metrics,
		Router:   router,
		Learning: learn,
		Telegram: tg,
		Meta:     meta,
	})

	srv := mcp.NewServer(reg, mcp.ServerOptions{
		Name:           "hackathonkit-mcp",
		Version:        version,
		Logger:         logger,
		Metrics:        metrics,
		CallTimeoutSec: cfg.Tools.DefaultTimeoutSec,
		CallLogPath:    filepath.Join(root, "logs", "mcp-calls.jsonl"),
	})

	if *listTools {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(srv.ToolDescriptors()); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}

	if *selftest {
		if err := srv.SelfTest(context.Background(), stdout); err != nil {
			fmt.Fprintln(stderr, "selftest error:", err)
			return 1
		}
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	observability.Log(logger, "info", "server_start", map[string]any{
		"root":    root,
		"mode":    mgr.Routing().Mode,
		"version": version,
	})
	if err := srv.Serve(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			observability.Log(logger, "info", "server_stop", map[string]any{"reason": "signal"})
			return 0
		}
		observability.Log(logger, "error", "server_stop", map[string]any{"error": err.Error()})
		return 1
	}
	return 0
}

// printSessionContext emits a compact context block for SessionStart hooks:
// routing mode, resource profile, top learnings, current plan stage and
// pending eval decisions.
func printSessionContext(root string, mgr *config.Manager, out, diag io.Writer) {
	state := mgr.Routing()
	fmt.Fprintln(out, "Hackathon Agent Kit session context")
	fmt.Fprintf(out, "routing: %s (config v%d)\n", state.Mode, state.Version)

	profile, present, rerr := resources.Load(root)
	switch {
	case rerr != nil:
		fmt.Fprintln(out, "resources: profile unreadable - call system.resources")
	case !present:
		fmt.Fprintln(out, "resources: not configured - ask the user (system.resources)")
	default:
		line := "resources: " + resources.Summary(resources.Statuses(profile))
		if profile.Track != "" {
			line += " track=" + profile.Track
		}
		fmt.Fprintf(out, "%s (profile v%d)\n", line, profile.Version)
	}

	settings := mgr.Config().Learning
	if settings.Enabled {
		store := learning.NewStore(filepath.Join(root, "data", "index", "learnings.json"), learning.ProjectKey(root))
		if items, err := store.Top(settings.MaxInject, settings.InjectMinConfidence); err == nil && len(items) > 0 {
			fmt.Fprintln(out, "learnings:")
			for _, item := range items {
				fmt.Fprintf(out, "- [%.2f] when %s -> %s\n", item.Confidence, clip(item.Trigger, 80), clip(item.Insight, 160))
			}
		}
	}
	if plan, ok := plans.Latest(root); ok {
		fmt.Fprintln(out, plans.Summary(plan))
	}
	if count := countFiles(filepath.Join(root, "evals", "decisions"), diag); count > 0 {
		fmt.Fprintf(out, "pending eval decisions: %d\n", count)
	}
}

// applyResourcesSet records k=v answers from the --resources-set flag. Only
// provider booleans, track and note are accepted; secrets stay in .env.
func applyResourcesSet(root, spec string) error {
	profile, _, err := resources.Load(root)
	if err != nil {
		return err
	}
	changed := false
	for _, pair := range strings.Split(spec, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return fmt.Errorf("expected key=value, got %q", pair)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "track":
			track, ok := resources.LookupTrack(value)
			if !ok {
				return fmt.Errorf("unknown track %q", value)
			}
			profile.Track = track.ID
		case "note":
			profile.Note = value
		default:
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("provider %q expects true or false, got %q", key, value)
			}
			if err := resources.SetAnswer(&profile, key, parsed); err != nil {
				return err
			}
		}
		changed = true
	}
	if !changed {
		return errors.New("no answers provided")
	}
	return resources.Save(root, profile)
}

func printResources(root string, out io.Writer) error {
	profile, present, err := resources.Load(root)
	if err != nil {
		return fmt.Errorf("profile unreadable: %w", err)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(resources.View(profile, present))
}

func countFiles(dir string, diag io.Writer) int {
	count := 0
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
		return nil
	}); err != nil && !os.IsNotExist(err) {
		// The session context is advisory: an unreadable directory must not
		// silently look like zero pending decisions, so report it.
		fmt.Fprintf(diag, "session-context: cannot read %s: %v\n", dir, err)
	}
	return count
}

func clip(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
