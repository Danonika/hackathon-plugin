package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"hackathonkit.local/agentkit/internal/atomicfile"
	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/control"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/observability"
	"hackathonkit.local/agentkit/internal/plans"
)

//go:embed static/*.html static/*.js
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

	addr := net.JoinHostPort(host, port)
	server := &http.Server{
		Addr:              addr,
		Handler:           newHandler(root, mgr, meta, token, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	observability.Log(logger, "info", "demo_api_start", map[string]any{"addr": addr, "root": root})

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "demo-api error:", err)
			meta.Close()
			os.Exit(1)
		}
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "demo-api shutdown error:", err)
		}
	}
}

func newHandler(root string, mgr *config.Manager, meta metadata.Recorder, token string, logger *log.Logger) http.Handler {
	// Serializes the read-compare-write of a plan decision.
	var decisionMu sync.Mutex
	mux := http.NewServeMux()
	mux.Handle("/fly-live", newFlyLiveHandler(root, logger))
	mux.HandleFunc("/fly-static/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/fly-static/")
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".js") {
			http.NotFound(w, r)
			return
		}
		content, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(content)
	})
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
	mux.HandleFunc("/vfb-mesh/", func(w http.ResponseWriter, r *http.Request) {
		base := filepath.Join(root, "data", "fly-viz", "vfb")
		if _, err := os.Stat(base); err != nil {
			http.Error(w, "VFB meshes are not downloaded; see data/fly-viz/vfb", http.StatusNotFound)
			return
		}
		http.StripPrefix("/vfb-mesh/", http.FileServer(http.Dir(base))).ServeHTTP(w, r)
	})
	mux.HandleFunc("/brain-demo", func(w http.ResponseWriter, r *http.Request) {
		page, err := staticFS.ReadFile("static/brain-demo.html")
		if err != nil {
			http.Error(w, "static page missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/vfb-brain", func(w http.ResponseWriter, r *http.Request) {
		page, err := staticFS.ReadFile("static/vfb-brain.html")
		if err != nil {
			http.Error(w, "static page missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/mb-3d", func(w http.ResponseWriter, r *http.Request) {
		page, err := staticFS.ReadFile("static/mb-3d.html")
		if err != nil {
			http.Error(w, "static page missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
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
		// The token is substituted into the page; JS-escape it so a value with
		// quotes or markup cannot break out of the surrounding script.
		_, _ = w.Write([]byte(strings.ReplaceAll(string(page), "__HACKATHONKIT_DEMO_TOKEN__", template.JSEscapeString(token))))
	})
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		page, err := staticFS.ReadFile("static/control.html")
		if err != nil {
			http.Error(w, "static page missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/control/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		store, err := control.OpenReadOnly(control.DefaultPath(root))
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "control database not available; run `hackathonkit-ctl ingest` or `hackathonkit-ctl daemon start`",
			})
			return
		}
		defer store.Close()
		sessions, err := store.Sessions(r.Context(), queryLimit(r, 50))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	})
	mux.HandleFunc("/control/api/session", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
			return
		}
		store, err := control.OpenReadOnly(control.DefaultPath(root))
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "control database not available"})
			return
		}
		defer store.Close()
		session, err := store.Session(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "session not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "session lookup failed"})
			}
			return
		}
		events, eventsErr := store.Events(r.Context(), id, queryLimit(r, 50))
		outputs, outputsErr := store.Outputs(r.Context(), id, 10)
		payload := map[string]any{"session": session, "events": events, "outputs": outputs}
		if eventsErr != nil || outputsErr != nil {
			payload["errors"] = strings.TrimSpace(fmt.Sprintf("%v %v", eventsErr, outputsErr))
		}
		writeJSON(w, http.StatusOK, payload)
	})
	mux.HandleFunc("/plan", func(w http.ResponseWriter, r *http.Request) {
		page, err := staticFS.ReadFile("static/plan.html")
		if err != nil {
			http.Error(w, "static page missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/plan/api/latest", func(w http.ResponseWriter, r *http.Request) {
		plan, ok := plans.Latest(root)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"plan": nil, "approval": nil})
			return
		}
		approval, err := readApproval(root, plan.Slug)
		if err != nil {
			// A corrupt/unreadable approval must not be reported as "none".
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"plan":     plan,
			"approval": approval,
		})
	})
	mux.HandleFunc("/plan/decision", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if !authorizeMutation(w, r, token) {
			return
		}
		decisionMu.Lock()
		defer decisionMu.Unlock()
		var body struct {
			Slug     string `json:"slug"`
			Revision int    `json:"revision"`
			Decision string `json:"decision"`
			Note     string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json: " + err.Error()})
			return
		}
		if body.Decision != "approve" && body.Decision != "request_changes" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "decision must be approve or request_changes"})
			return
		}
		plan, ok := plans.Latest(root)
		if body.Slug != "" {
			loaded, err := plans.Load(root, body.Slug)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "plan not found"})
				return
			}
			plan, ok = loaded, true
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "no plan to review"})
			return
		}
		// Require the client's revision to match the current plan. A client that
		// omits it (zero) or sends a negative value must not bypass the guard,
		// otherwise a stale client could overwrite a decision taken against a
		// newer revision.
		if body.Revision != plan.Revision {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "plan revision changed; reload before deciding", "revision": plan.Revision})
			return
		}
		record, err := writeApproval(root, plan.Slug, plan.Revision, body.Decision, body.Note)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"approval": record})
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
			if !authorizeMutation(w, r, token) {
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

	return withCORS(mux)
}

type planApproval struct {
	Slug      string    `json:"slug"`
	Revision  int       `json:"revision"`
	Decision  string    `json:"decision"`
	Note      string    `json:"note,omitempty"`
	DecidedAt time.Time `json:"decided_at"`
}

func approvalPath(root, slug string) string {
	return filepath.Join(plans.Dir(root), slug+".approval.json")
}

// validPlanSlug rejects a slug that could escape evals/plans when joined into a
// path (a plan body may carry an arbitrary "slug" value).
func validPlanSlug(slug string) bool {
	if slug == "" || slug == "." || slug == ".." {
		return false
	}
	if strings.ContainsAny(slug, `/\`) {
		return false
	}
	return filepath.Base(slug) == slug
}

// readApproval loads a plan approval. A missing file is (nil, nil); any other
// failure (permission, corrupt JSON) is returned as an error so a corrupt
// approval is not silently indistinguishable from "not yet decided".
func readApproval(root, slug string) (*planApproval, error) {
	if !validPlanSlug(slug) {
		return nil, fmt.Errorf("invalid plan slug %q", slug)
	}
	data, err := os.ReadFile(approvalPath(root, slug))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read approval for %s: %w", slug, err)
	}
	var record planApproval
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("approval for %s is corrupt: %w", slug, err)
	}
	return &record, nil
}

func writeApproval(root, slug string, revision int, decision, note string) (planApproval, error) {
	record := planApproval{
		Slug:      slug,
		Revision:  revision,
		Decision:  decision,
		Note:      note,
		DecidedAt: time.Now().UTC(),
	}
	if !validPlanSlug(slug) {
		return record, fmt.Errorf("invalid plan slug %q", slug)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return record, err
	}
	path := approvalPath(root, slug)
	if err := atomicfile.Write(path, append(data, '\n'), atomicfile.ModeState); err != nil {
		return record, err
	}
	return record, nil
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

// authorizeMutation guards state-changing endpoints. With a configured token
// the header must match (constant time); without one, browser-driven mutations
// still require a loopback Origin so a random page cannot POST to the daemon.
func authorizeMutation(w http.ResponseWriter, r *http.Request, token string) bool {
	if !originAllowed(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden origin"})
		return false
	}
	if token == "" {
		// Origin is client-controlled and spoofable by any local process, so it
		// is not sufficient authorization for a mutation.
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "mutations require HACKATHONKIT_DEMO_TOKEN to be set (and sent as X-HackathonKit-Token)"})
		return false
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-HackathonKit-Token")), []byte(token)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing or invalid X-HackathonKit-Token"})
		return false
	}
	return true
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

// corsOrigin returns the Origin to reflect for CORS, or "" when it is not the
// server's own origin. Reflecting any loopback origin would let a page on any
// local port read cross-origin responses, including /fly-viz, which embeds the
// demo token; a same-origin page does not need CORS at all.
func corsOrigin(r *http.Request) string {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return ""
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	if r.Host == "" || !strings.EqualFold(u.Host, r.Host) {
		return ""
	}
	return origin
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		if origin := corsOrigin(r); origin != "" {
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
