package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
	"hackathonkit.local/agentkit/internal/routing/fly"
	"hackathonkit.local/agentkit/internal/storage"
	"hackathonkit.local/agentkit/internal/tools"
)

const version = "0.7.0"

func main() {
	selftest := flag.Bool("selftest", false, "run an in-process MCP handshake and exit")
	listTools := flag.Bool("list-tools", false, "print tool descriptors as JSON and exit")
	repoRoot := flag.String("repo-root", "", "repository root (defaults to auto-discovery)")
	setMode := flag.String("set-routing-mode", "", "set routing mode (normal|fly) and exit")
	sessionContext := flag.Bool("session-context", false, "print a compact session context block for hooks and exit")
	flag.Parse()

	root := *repoRoot
	if root == "" {
		root = config.DiscoverRoot()
	}
	config.LoadDotEnv(root)
	mgr := config.NewManager(root)

	if *setMode != "" {
		state, err := mgr.SetRoutingMode(*setMode)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Printf("routing mode set to %s (version %d)\n", state.Mode, state.Version)
		return
	}

	if *sessionContext {
		printSessionContext(root, mgr)
		return
	}

	cfg := mgr.Config()
	metrics := observability.NewMetrics()
	logger := observability.NewLogger(os.Stderr)
	store := storage.New(root, cfg.Storage)
	oa := openai.New(cfg.OpenAI)
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
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		if pg, err := metadata.OpenPostgres(context.Background(), dsn); err != nil {
			observability.Log(logger, "warn", "metadata_disabled", map[string]any{"error": err.Error()})
		} else {
			meta = pg
			observability.Log(logger, "info", "metadata_postgres_enabled", nil)
		}
	}
	var objects storage.ObjectStore
	if settings := objectstore.SettingsFromEnv(); settings.Endpoint != "" {
		if cli, err := objectstore.Open(context.Background(), settings); err != nil {
			observability.Log(logger, "warn", "objectstore_disabled", map[string]any{"error": err.Error()})
		} else {
			objects = cli
			observability.Log(logger, "info", "objectstore_minio_enabled", map[string]any{"bucket": settings.Bucket})
		}
	}
	store.Attach(meta, objects, func(err error) {
		observability.Log(logger, "warn", "storage_backend_error", map[string]any{"error": err.Error()})
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
		Meta:     meta,
		Version:  version,
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
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(srv.ToolDescriptors()); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if *selftest {
		if err := srv.SelfTest(context.Background(), os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "selftest error:", err)
			os.Exit(1)
		}
		return
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
			return
		}
		observability.Log(logger, "error", "server_stop", map[string]any{"error": err.Error()})
		os.Exit(1)
	}
}

// printSessionContext emits a compact context block for SessionStart hooks:
// routing mode, top learnings, current plan stage and pending eval decisions.
func printSessionContext(root string, mgr *config.Manager) {
	state := mgr.Routing()
	fmt.Println("Hackathon Agent Kit session context")
	fmt.Printf("routing: %s (config v%d)\n", state.Mode, state.Version)

	settings := mgr.Config().Learning
	if settings.Enabled {
		store := learning.NewStore(filepath.Join(root, "data", "index", "learnings.json"), learning.ProjectKey(root))
		if items, err := store.Top(settings.MaxInject, settings.InjectMinConfidence); err == nil && len(items) > 0 {
			fmt.Println("learnings:")
			for _, item := range items {
				fmt.Printf("- [%.2f] when %s -> %s\n", item.Confidence, clip(item.Trigger, 80), clip(item.Insight, 160))
			}
		}
	}
	if plan, ok := plans.Latest(root); ok {
		fmt.Println(plans.Summary(plan))
	}
	if count := countFiles(filepath.Join(root, "evals", "decisions")); count > 0 {
		fmt.Printf("pending eval decisions: %d\n", count)
	}
}

func countFiles(dir string) int {
	count := 0
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
		return nil
	})
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
