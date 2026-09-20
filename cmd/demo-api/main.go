package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/observability"
)

//go:embed static/*.html
var staticFS embed.FS

func main() {
	root := config.DiscoverRoot()
	config.LoadDotEnv(root)
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
	token := os.Getenv("HACKATHONKIT_DEMO_TOKEN")

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
		quoted, _ := json.Marshal(token)
		rendered := strings.ReplaceAll(string(page), `"__HACKATHONKIT_DEMO_TOKEN__"`, string(quoted))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(rendered))
	})
	mux.HandleFunc("/fly-model-list", func(w http.ResponseWriter, r *http.Request) {
		base := filepath.Join(root, "data", "fly-viz", "neuromechfly")
		files := []string{}
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			if strings.HasSuffix(name, ".obj") || strings.HasSuffix(name, ".stl") {
				if rel, relErr := filepath.Rel(base, path); relErr == nil {
					files = append(files, filepath.ToSlash(rel))
				}
			}
			return nil
		})
		sort.Strings(files)
		writeJSON(w, http.StatusOK, map[string]any{"files": files})
	})
	mux.HandleFunc("/fly-model/", func(w http.ResponseWriter, r *http.Request) {
		base := filepath.Join(root, "data", "fly-viz", "neuromechfly")
		if _, err := os.Stat(base); err != nil {
			http.Error(w, "NeuromechFly meshes are not vendored; run: git clone --depth 1 https://github.com/NeLy-EPFL/NeuromechFly data/fly-viz/neuromechfly", http.StatusNotFound)
			return
		}
		http.StripPrefix("/fly-model/", http.FileServer(http.Dir(base))).ServeHTTP(w, r)
	})
	mux.HandleFunc("/fly-brain/", func(w http.ResponseWriter, r *http.Request) {
		base := filepath.Join(root, "data", "fly-viz", "flybrain")
		if _, err := os.Stat(base); err != nil {
			http.Error(w, "flybrain assets are not vendored; run: git clone --depth 1 https://github.com/snedea/flybrain.git data/fly-viz/flybrain", http.StatusNotFound)
			return
		}
		http.StripPrefix("/fly-brain/", http.FileServer(http.Dir(base))).ServeHTTP(w, r)
	})
	mux.HandleFunc("/fly-body/", func(w http.ResponseWriter, r *http.Request) {
		base := filepath.Join(root, "data", "fly-viz", "menagerie", "flybody")
		if _, err := os.Stat(base); err != nil {
			http.Error(w, "flybody assets are not vendored; run: git clone --depth 1 --filter=blob:none --sparse https://github.com/google-deepmind/mujoco_menagerie data/fly-viz/menagerie && cd data/fly-viz/menagerie && git sparse-checkout set flybody", http.StatusNotFound)
			return
		}
		http.StripPrefix("/fly-body/", http.FileServer(http.Dir(base))).ServeHTTP(w, r)
	})
	mux.HandleFunc("/fly-body", func(w http.ResponseWriter, r *http.Request) {
		page, err := staticFS.ReadFile("static/fly-body.html")
		if err != nil {
			http.Error(w, "static page missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/fly-viz-3d", func(w http.ResponseWriter, r *http.Request) {
		page, err := staticFS.ReadFile("static/fly-viz-3d.html")
		if err != nil {
			http.Error(w, "static page missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/fly-viz", func(w http.ResponseWriter, r *http.Request) {
		page, err := staticFS.ReadFile("static/fly-viz.html")
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
			if !originAllowed(r) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden origin"})
				return
			}
			if token != "" && r.Header.Get("X-HackathonKit-Token") != token {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing or invalid X-HackathonKit-Token"})
				return
			}
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
		files, err := listFiles(mgr, mgr.Config().Storage.InputDir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": files})
	})
	mux.HandleFunc("/artifacts", func(w http.ResponseWriter, r *http.Request) {
		files, err := listFiles(mgr, mgr.Config().Storage.ArtifactDir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": files})
	})
	mux.HandleFunc("/calls", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"calls": tail(logger, root, "logs/mcp-calls.jsonl", queryLimit(r, 50)),
		})
	})
	mux.HandleFunc("/fly-events", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"events": tail(logger, root, "logs/fly-events.jsonl", queryLimit(r, 10)),
		})
	})
	mux.HandleFunc("/fly-feedback", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"events": tail(logger, root, "logs/fly-feedback.jsonl", queryLimit(r, 20)),
		})
	})
	mux.HandleFunc("/fly-neuron-events", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"events": tail(logger, root, "logs/fly-neuron-events.jsonl", queryLimit(r, 30)),
		})
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "# hackathonkit demo-api\n")
		fmt.Fprintf(w, "hackathonkit_routing_mode{mode=%q} 1\n", mgr.Routing().Mode)
	})

	addr := net.JoinHostPort(host, port)
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

func tail(logger *log.Logger, root, rel string, limit int) []json.RawMessage {
	entries, err := observability.TailJSONL(filepath.Join(root, filepath.FromSlash(rel)), limit)
	if err != nil {
		observability.Log(logger, "warn", "tail_failed", map[string]any{"path": rel, "error": err.Error()})
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

func listFiles(mgr *config.Manager, relDir string) ([]fileInfo, error) {
	base := filepath.Join(mgr.Root(), filepath.FromSlash(relDir))
	out := []fileInfo{}
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return relErr
		}
		st, statErr := d.Info()
		if statErr != nil {
			return statErr
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
	if os.IsNotExist(err) {
		err = nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" && originAllowed(r) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-HackathonKit-Token")
		}
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
