package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"hackathonkit.local/agentkit/internal/control"
)

const (
	defaultControlPort = "8788"
	ingestInterval     = 2 * time.Second
)

func pidPath(root string) string {
	return filepath.Join(root, "data", "control", "daemon.pid")
}

func logPath(root string) string {
	return filepath.Join(root, "data", "control", "daemon.log")
}

func cmdDaemon(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hackathonkit-ctl daemon start|stop|status|foreground")
	}
	root := repoRoot()
	switch args[0] {
	case "start":
		return daemonStart(root)
	case "stop":
		return daemonStop(root)
	case "status":
		return daemonStatus(root)
	case "foreground":
		return daemonForeground(root)
	default:
		return fmt.Errorf("unknown daemon subcommand %q", args[0])
	}
}

func daemonStart(root string) error {
	if pid, alive := readPID(root); alive {
		fmt.Printf("daemon already running (pid %d)\n", pid)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(pidPath(root)), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath(root), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "daemon", "foreground")
	cmd.Dir = root
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = detachSysProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pid, alive := readPID(root); alive {
			fmt.Printf("daemon started (pid %d), log: %s\n", pid, logPath(root))
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start; check %s", logPath(root))
}

func daemonStop(root string) error {
	pid, alive := readPID(root)
	if !alive {
		_ = os.Remove(pidPath(root))
		fmt.Println("daemon is not running")
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, alive := readPID(root); !alive {
			fmt.Println("daemon stopped")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon pid %d did not stop", pid)
}

func daemonStatus(root string) error {
	pid, alive := readPID(root)
	if !alive {
		fmt.Println("daemon: stopped")
		return nil
	}
	fmt.Printf("daemon: running (pid %d)\n", pid)
	return nil
}

func daemonForeground(root string) error {
	store, err := openStore(root)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := os.MkdirAll(filepath.Dir(pidPath(root)), 0o755); err != nil {
		return err
	}
	if existing, ok := readPID(root); ok && existing != os.Getpid() {
		return fmt.Errorf("daemon: another process (pid %d) already owns %s", existing, pidPath(root))
	}
	mine := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(pidPath(root), []byte(mine), 0o644); err != nil {
		return err
	}
	defer func() {
		// Only remove the pid file when it is still ours.
		if data, err := os.ReadFile(pidPath(root)); err == nil && strings.TrimSpace(string(data)) == mine {
			os.Remove(pidPath(root))
		}
	}()

	var ingestWG sync.WaitGroup
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Registered before stop() so that, with LIFO defers, stop() cancels the
	// context first and the ingest goroutine can exit before we wait.
	defer ingestWG.Wait()
	defer stop()

	// Initial ingest, then periodic replay.
	if _, err := store.Ingest(ctx, root); err != nil {
		fmt.Fprintln(os.Stderr, "ingest:", err)
	}
	ingestWG.Add(1)
	go func() {
		defer ingestWG.Done()
		ticker := time.NewTicker(ingestInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := store.Ingest(ctx, root); err != nil {
					fmt.Fprintln(os.Stderr, "ingest:", err)
				}
			}
		}
	}()

	port := os.Getenv("HACKATHONKIT_CONTROL_PORT")
	if port == "" {
		port = defaultControlPort
	}
	token := os.Getenv("HACKATHONKIT_CONTROL_TOKEN")
	addr := net.JoinHostPort("127.0.0.1", port)
	server := &http.Server{
		Addr:              addr,
		Handler:           controlHandler(store, token),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Printf("control daemon listening on http://%s (pid %d)\n", addr, os.Getpid())
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func readPID(root string) (int, bool) {
	data, err := os.ReadFile(pidPath(root))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return pid, false
	}
	if !processLooksLikeDaemon(pid) {
		// Stale pid recycled by an unrelated process.
		return pid, false
	}
	return pid, true
}

// processLooksLikeDaemon verifies the process identity on Linux (via
// /proc/<pid>/cmdline) and accepts the signal check elsewhere.
func processLooksLikeDaemon(pid int) bool {
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		// Non-Linux / restricted /proc: identity cannot be verified, so fall
		// back to the caller's liveness check (documented residual risk).
		return true
	}
	return strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), "hackathonkit-ctl")
}

func controlHandler(store *control.Store, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		sessions, events, err := store.Counts(r.Context())
		if err != nil {
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]any{"status": "ok", "sessions": sessions, "events": events})
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		sessions, err := store.Sessions(r.Context(), queryInt(r, "limit", 50))
		if err != nil {
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	})
	mux.HandleFunc("/api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		if id == "" {
			writeControlJSON(w, http.StatusBadRequest, map[string]any{"error": "session id required"})
			return
		}
		sess, err := store.Session(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeControlJSON(w, http.StatusNotFound, map[string]any{"error": "session not found"})
			} else {
				writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": "session lookup failed"})
			}
			return
		}
		events, eventsErr := store.Events(r.Context(), id, queryInt(r, "events", 50))
		outputs, outputsErr := store.Outputs(r.Context(), id, 10)
		payload := map[string]any{"session": sess, "events": events, "outputs": outputs}
		if eventsErr != nil || outputsErr != nil {
			parts := []string{}
			if eventsErr != nil {
				parts = append(parts, "events: "+eventsErr.Error())
			}
			if outputsErr != nil {
				parts = append(parts, "outputs: "+outputsErr.Error())
			}
			payload["errors"] = strings.Join(parts, "; ")
		}
		writeControlJSON(w, http.StatusOK, payload)
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		events, err := store.Events(r.Context(), r.URL.Query().Get("session_id"), queryInt(r, "limit", 50))
		if err != nil {
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]any{"events": events})
	})
	mux.HandleFunc("/api/risk", func(w http.ResponseWriter, r *http.Request) {
		if _, err := store.RecomputeRisk(r.Context()); err != nil {
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		sessions, err := store.Sessions(r.Context(), 200)
		if err != nil {
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			// Constant-time comparison so the token cannot be probed by timing.
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-HackathonKit-Token")), []byte(token)) != 1 {
				writeControlJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing or invalid X-HackathonKit-Token"})
				return
			}
		} else if (mutatingMethod(r.Method) || r.URL.Path == "/api/risk") && (r.Header.Get("Origin") != "" || r.Header.Get("Referer") != "") {
			// Without a token, refuse browser-initiated mutations: a page in any
			// origin could otherwise POST to the loopback API.
			writeControlJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin request rejected; set HACKATHONKIT_CONTROL_TOKEN or use a non-browser client"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func mutatingMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func queryInt(r *http.Request, key string, def int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return def
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return def
	}
	return n
}

func writeControlJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}
