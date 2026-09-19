package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/observability"
)

//go:embed static/index.html
var staticFS embed.FS

func main() {
	root := config.DiscoverRoot()
	mgr := config.NewManager(root)
	logger := observability.NewLogger(os.Stderr)

	var meta metadata.Recorder = metadata.Disabled{}
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		if pg, err := metadata.OpenPostgres(context.Background(), dsn); err != nil {
			observability.Log(logger, "warn", "metadata_disabled", map[string]any{"error": err.Error()})
		} else {
			meta = pg
		}
	}
	defer meta.Close()

	port := os.Getenv("HACKATHONKIT_DEMO_PORT")
	if port == "" {
		port = "8787"
	}
	host := os.Getenv("HACKATHONKIT_DEMO_HOST")
	if host == "" {
		host = "127.0.0.1"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		page, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "static page missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"root":   root,
			"mode":   mgr.Routing().Mode,
		})
	})
	mux.HandleFunc("/routing", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, mgr.Routing())
		case http.MethodPost:
			var body struct {
				Mode string `json:"mode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json: " + err.Error()})
				return
			}
			state, err := mgr.SetRoutingMode(body.Mode)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			if meta.Enabled() {
				_ = meta.RecordRoutingRun(r.Context(), metadata.RoutingRun{Mode: state.Mode, Version: state.Version})
			}
			writeJSON(w, http.StatusOK, state)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	})
	mux.HandleFunc("/inputs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"files": listFiles(mgr, mgr.Config().Storage.InputDir)})
	})
	mux.HandleFunc("/artifacts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"files": listFiles(mgr, mgr.Config().Storage.ArtifactDir)})
	})
	mux.HandleFunc("/calls", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"calls": tail(root, "logs/mcp-calls.jsonl", queryLimit(r, 50)),
		})
	})
	mux.HandleFunc("/fly-events", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"events": tail(root, "logs/fly-events.jsonl", queryLimit(r, 10)),
		})
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "# hackathonkit demo-api\n")
		fmt.Fprintf(w, "hackathonkit_routing_mode{mode=%q} 1\n", mgr.Routing().Mode)
	})

	addr := host + ":" + port
	server := &http.Server{
		Addr:              addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	observability.Log(logger, "info", "demo_api_start", map[string]any{"addr": addr, "root": root})
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "demo-api error:", err)
		os.Exit(1)
	}
}

func tail(root, rel string, limit int) []json.RawMessage {
	entries, err := observability.TailJSONL(filepath.Join(root, filepath.FromSlash(rel)), limit)
	if err != nil {
		return []json.RawMessage{}
	}
	return entries
}

func queryLimit(r *http.Request, def int) int {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

type fileInfo struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Modified  string `json:"modified"`
}

func listFiles(mgr *config.Manager, relDir string) []fileInfo {
	base := filepath.Join(mgr.Root(), filepath.FromSlash(relDir))
	out := []fileInfo{}
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return nil
		}
		st, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		out = append(out, fileInfo{
			Path:      filepath.ToSlash(rel),
			SizeBytes: st.Size(),
			Modified:  st.ModTime().UTC().Format(time.RFC3339),
		})
		if len(out) > 500 {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}
