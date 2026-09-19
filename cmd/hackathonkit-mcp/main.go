package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/observability"
	"hackathonkit.local/agentkit/internal/objectstore"
	"hackathonkit.local/agentkit/internal/openai"
	"hackathonkit.local/agentkit/internal/routing/fly"
	"hackathonkit.local/agentkit/internal/storage"
	"hackathonkit.local/agentkit/internal/tools"
)

const version = "0.1.0"

func main() {
	selftest := flag.Bool("selftest", false, "run an in-process MCP handshake and exit")
	listTools := flag.Bool("list-tools", false, "print tool descriptors as JSON and exit")
	repoRoot := flag.String("repo-root", "", "repository root (defaults to auto-discovery)")
	setMode := flag.String("set-routing-mode", "", "set routing mode (normal|fly) and exit")
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

	cfg := mgr.Config()
	metrics := observability.NewMetrics()
	logger := observability.NewLogger(os.Stderr)
	store := storage.New(root, cfg.Storage)
	oa := openai.New(cfg.OpenAI)
	router := fly.NewRouter(cfg.Routing)
	if oa.Available() {
		router.WithEmbeddings(oa, filepath.Join(root, "data", "index", "fly-embeddings.json"), cfg.OpenAI.EmbeddingModel)
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
		Cfg:     mgr,
		Store:   store,
		OpenAI:  oa,
		Metrics: metrics,
		Router:  router,
		Meta:    meta,
		Version: version,
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
		out := []map[string]any{}
		for _, t := range reg.List() {
			schema := t.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			out = append(out, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": schema,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
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
		observability.Log(logger, "error", "server_stop", map[string]any{"error": err.Error()})
		os.Exit(1)
	}
}
