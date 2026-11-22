package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

	"hackathonkit.local/agentkit/internal/atomicfile"
	"hackathonkit.local/agentkit/internal/control"
	"hackathonkit.local/agentkit/internal/filelock"
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
		if _, err := os.Stat(pidPath(root)); err == nil {
			unlock, err := filelock.Acquire(pidPath(root)+".guard", time.Second)
			if err != nil {
				return err
			}
			_, running := readPID(root)
			if !running {
				_ = os.Remove(pidPath(root))
			}
			unlock()
			if running {
				return fmt.Errorf("daemon identity changed before stop")
			}
		}
		fmt.Println("daemon is not running")
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	defer process.Release()
	// FindProcess pins the process object on Linux and Windows. Verify the
	// record again after opening that handle, before sending any signal.
	if current, ours := readPID(root); !ours || current != pid {
		return fmt.Errorf("daemon identity changed before stop")
	}
	switch err := process.WithHandle(func(uintptr) {}); {
	case err == nil:
		if err := terminateProcess(process); err != nil {
			return err
		}
	case errors.Is(err, os.ErrNoHandle):
		// A raw PID may be recycled between identity verification and signal
		// delivery. Have this exact daemon instance stop itself on its next tick.
		current, _, birth, err := readPIDFile(root)
		if err != nil || current != pid || birth == "" {
			return fmt.Errorf("cannot verify daemon instance for cooperative stop")
		}
		if err := atomicfile.Write(pidPath(root)+".stop", []byte(fmt.Sprintf("%d %s", pid, birth)), atomicfile.ModeState); err != nil {
			return err
		}
	default:
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
	mine := strconv.Itoa(os.Getpid())
	// Record the pid and the executable path so a recycled pid can be told
	// apart from our daemon without relying on /proc. Claim the file atomically
	// so two concurrent `daemon start` runs cannot both believe they own it.
	birth := processBirth(os.Getpid())
	self, _ := os.Executable()
	if err := claimPIDFile(root, []byte(mine+"\n"+self+"\n"+birth+"\n")); err != nil {
		return err
	}
	defer func() {
		unlock, err := filelock.Acquire(pidPath(root)+".guard", time.Second)
		if err != nil {
			return
		}
		defer unlock()
		// Only remove the pid file when it is still ours.
		if pid, _, _, err := readPIDFile(root); err == nil && pid == os.Getpid() {
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
				if requested, err := os.ReadFile(pidPath(root) + ".stop"); err == nil && birth != "" && string(requested) == mine+" "+birth {
					_ = os.Remove(pidPath(root) + ".stop")
					stop()
					return
				}
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
	// Track in-flight handlers so store.Close() runs only after they finish.
	// server.Shutdown returns as soon as its context times out, which does not
	// guarantee handlers have stopped.
	var handlerWG sync.WaitGroup
	base := controlHandler(store, token)
	server := &http.Server{
		Addr:        addr,
		BaseContext: func(net.Listener) context.Context { return ctx },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerWG.Add(1)
			defer handlerWG.Done()
			base.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
	}()
	fmt.Printf("control daemon listening on http://%s (pid %d)\n", addr, os.Getpid())
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		// Wait for Shutdown to return, then for in-flight handlers to finish
		// (bounded) before the deferred store.Close() runs.
		<-shutdownDone
		handlerWG.Wait()
		return nil
	}
	return err
}

// readPIDFile parses the pid file (pid on line 1, executable path on line 2).
func readPIDFile(root string) (int, string, string, error) {
	data, err := os.ReadFile(pidPath(root))
	if err != nil {
		return 0, "", "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || pid <= 0 {
		return 0, "", "", fmt.Errorf("daemon: malformed pid file %s", pidPath(root))
	}
	exe := ""
	if len(lines) > 1 {
		exe = strings.TrimSpace(lines[1])
	}
	var birth string
	if len(lines) > 2 {
		birth = strings.TrimSpace(lines[2])
	}
	return pid, exe, birth, nil
}

// claimPIDFile publishes the pid file atomically: the content is written to a
// temp file first and then hard-linked into place, so a concurrent reader never
// sees an empty pid file (which O_EXCL-create-then-write would expose). If one
// already exists, it is reclaimed only when no live daemon owns it.
func claimPIDFile(root string, content []byte) error {
	path := pidPath(root)
	dir := filepath.Dir(path)
	unlock, err := filelock.Acquire(path+".guard", time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	for attempt := 0; attempt < 3; attempt++ {
		tmp, err := os.CreateTemp(dir, "daemon.pid.*")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		if _, werr := tmp.Write(content); werr != nil {
			tmp.Close()
			os.Remove(tmpName)
			return werr
		}
		if cerr := tmp.Close(); cerr != nil {
			os.Remove(tmpName)
			return cerr
		}
		if cerr := os.Chmod(tmpName, 0o644); cerr != nil {
			os.Remove(tmpName)
			return cerr
		}
		if lerr := os.Link(tmpName, path); lerr == nil {
			os.Remove(tmpName)
			return nil
		} else if !os.IsExist(lerr) {
			os.Remove(tmpName)
			return lerr
		}
		os.Remove(tmpName)
		if pid, alive := readPID(root); alive && pid != os.Getpid() {
			return fmt.Errorf("daemon: another process (pid %d) already owns %s", pid, path)
		}
		// No live owner: remove the stale file and retry.
		if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
			return rerr
		}
	}
	return fmt.Errorf("daemon: could not claim %s", path)
}

func readPID(root string) (int, bool) {
	pid, recordedExe, birth, err := readPIDFile(root)
	if err != nil {
		return 0, false
	}
	if !processAlive(pid) {
		return pid, false
	}
	if recordedExe != "" && !processMatchesPath(pid, recordedExe) {
		return pid, false
	}
	if birth != "" {
		return pid, birth == processBirth(pid)
	}
	// Legacy records lack a process birth identity; verify the daemon command.
	if !processLooksLikeDaemon(pid, recordedExe) {
		return pid, false
	}
	return pid, true
}

// samePath reports whether two paths refer to the same file, tolerating
// symlinks and a trailing " (deleted)" marker on Linux.
func samePath(a, b string) bool {
	a = strings.TrimSuffix(a, " (deleted)")
	b = strings.TrimSuffix(b, " (deleted)")
	ra, errA := filepath.EvalSymlinks(a)
	if errA != nil {
		ra = a
	}
	rb, errB := filepath.EvalSymlinks(b)
	if errB != nil {
		rb = b
	}
	return ra == rb
}

func legacyDaemonArgs(args []string, recorded string) bool {
	if len(args) != 3 || args[1] != "daemon" || args[2] != "foreground" {
		return false
	}
	// A matching recorded executable was checked separately. For older PID-only
	// records also require the expected executable name.
	return recorded != "" || strings.TrimSuffix(filepath.Base(args[0]), ".exe") == "hackathonkit-ctl"
}

func controlHandler(store *control.Store, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		sessions, events, err := store.Counts(r.Context())
		if err != nil {
			log.Printf("control: healthz counts: %v", err)
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": "control store unavailable"})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]any{"status": "ok", "sessions": sessions, "events": events})
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		sessions, err := store.Sessions(r.Context(), queryInt(r, "limit", 50))
		if err != nil {
			log.Printf("control: list sessions: %v", err)
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list sessions"})
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
				log.Printf("control: session %q lookup: %v", id, err)
				writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": "session lookup failed"})
			}
			return
		}
		events, eventsErr := store.Events(r.Context(), id, queryInt(r, "events", 50))
		outputs, outputsErr := store.Outputs(r.Context(), id, 10)
		payload := map[string]any{"session": sess, "events": events, "outputs": outputs}
		if eventsErr != nil || outputsErr != nil {
			if eventsErr != nil {
				log.Printf("control: session %q events: %v", id, eventsErr)
			}
			if outputsErr != nil {
				log.Printf("control: session %q outputs: %v", id, outputsErr)
			}
			payload["errors"] = "events and/or outputs unavailable"
		}
		writeControlJSON(w, http.StatusOK, payload)
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		events, err := store.Events(r.Context(), r.URL.Query().Get("session_id"), queryInt(r, "limit", 50))
		if err != nil {
			log.Printf("control: list events: %v", err)
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list events"})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]any{"events": events})
	})
	mux.HandleFunc("/api/risk", func(w http.ResponseWriter, r *http.Request) {
		if _, err := store.RecomputeRisk(r.Context()); err != nil {
			log.Printf("control: recompute risk: %v", err)
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": "risk recompute failed"})
			return
		}
		sessions, err := store.Sessions(r.Context(), 200)
		if err != nil {
			log.Printf("control: risk sessions: %v", err)
			writeControlJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list sessions"})
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
		} else if (mutatingMethod(r.Method) || r.URL.Path == "/api/risk") && crossSiteRequest(r) {
			// Without a token, refuse browser-initiated mutations: a page in any
			// origin could otherwise POST to the loopback API.
			writeControlJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin request rejected; set HACKATHONKIT_CONTROL_TOKEN or use a non-browser client"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// crossSiteRequest reports a browser-initiated request that did not originate
// from this page. Origin/Referer are omitted for sub-resource GETs or with a
// no-referrer policy, so also consult Sec-Fetch-Site, which page scripts cannot
// set. A non-browser client (curl, the TUI) sends none of these.
func crossSiteRequest(r *http.Request) bool {
	if r.Header.Get("Origin") != "" || r.Header.Get("Referer") != "" {
		return true
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		return true
	}
	return false
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
