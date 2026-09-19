package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"hackathonkit.local/agentkit/internal/config"
)

func main() {
	root := config.DiscoverRoot()
	mgr := config.NewManager(root)

	port := os.Getenv("HACKATHONKIT_DEMO_PORT")
	if port == "" {
		port = "8787"
	}

	mux := http.NewServeMux()
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
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "# hackathonkit demo-api metrics are produced by the MCP server logs\n")
		fmt.Fprintf(w, "hackathonkit_routing_mode{mode=%q} 1\n", mgr.Routing().Mode)
	})

	addr := "127.0.0.1:" + port
	server := &http.Server{
		Addr:              addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "hackathonkit demo-api listening on http://%s (root: %s)\n", addr, root)
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "demo-api error:", err)
		os.Exit(1)
	}
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
