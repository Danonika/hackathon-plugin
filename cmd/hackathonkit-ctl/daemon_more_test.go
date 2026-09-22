package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPidAndLogPaths(t *testing.T) {
	root := t.TempDir()
	if got, want := pidPath(root), filepath.Join(root, "data", "control", "daemon.pid"); got != want {
		t.Fatalf("pidPath = %q, want %q", got, want)
	}
	if got, want := logPath(root), filepath.Join(root, "data", "control", "daemon.log"); got != want {
		t.Fatalf("logPath = %q, want %q", got, want)
	}
}

func TestReadPID(t *testing.T) {
	root := t.TempDir()
	if pid, alive := readPID(root); alive || pid != 0 {
		t.Fatalf("missing pid file: pid=%d alive=%v", pid, alive)
	}
	if err := os.MkdirAll(filepath.Dir(pidPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath(root), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	if pid, alive := readPID(root); !alive || pid != os.Getpid() {
		t.Fatalf("live pid: pid=%d alive=%v", pid, alive)
	}
	for _, bad := range []string{"", "  ", "not-a-number", "-4", "0"} {
		if err := os.WriteFile(pidPath(root), []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if pid, alive := readPID(root); alive || pid != 0 {
			t.Fatalf("bad pid %q: pid=%d alive=%v", bad, pid, alive)
		}
	}
	if err := os.WriteFile(pidPath(root), []byte("999999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pid, alive := readPID(root); alive {
		t.Fatalf("dead pid reported alive: %d", pid)
	}
}

func TestDaemonStatusWithoutPID(t *testing.T) {
	root := t.TempDir()
	out := captureStdout(t, func() {
		if err := daemonStatus(root); err != nil {
			t.Fatalf("daemonStatus: %v", err)
		}
	})
	if !strings.Contains(out, "daemon: stopped") {
		t.Fatalf("status output = %q", out)
	}
}

func TestDaemonStatusWithLivePID(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(pidPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath(root), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := daemonStatus(root); err != nil {
			t.Fatalf("daemonStatus: %v", err)
		}
	})
	if !strings.Contains(out, "daemon: running") {
		t.Fatalf("status output = %q", out)
	}
}

func TestDaemonStopWithoutPID(t *testing.T) {
	root := t.TempDir()
	out := captureStdout(t, func() {
		if err := daemonStop(root); err != nil {
			t.Fatalf("daemonStop: %v", err)
		}
	})
	if !strings.Contains(out, "daemon is not running") {
		t.Fatalf("stop output = %q", out)
	}

	if err := os.MkdirAll(filepath.Dir(pidPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath(root), []byte("999999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		if err := daemonStop(root); err != nil {
			t.Fatalf("daemonStop stale: %v", err)
		}
	})
	if !strings.Contains(out, "daemon is not running") {
		t.Fatalf("stale stop output = %q", out)
	}
	if _, err := os.Stat(pidPath(root)); !os.IsNotExist(err) {
		t.Fatalf("stale pid file not removed: %v", err)
	}
}

func TestDaemonStartAlreadyRunning(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(pidPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath(root), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := daemonStart(root); err != nil {
			t.Fatalf("daemonStart: %v", err)
		}
	})
	if !strings.Contains(out, "daemon already running") {
		t.Fatalf("daemonStart output = %q", out)
	}
}

func TestCmdDaemonDispatch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HACKATHONKIT_REPO_ROOT", root)
	out := captureStdout(t, func() {
		if err := cmdDaemon([]string{"status"}); err != nil {
			t.Fatalf("cmdDaemon status: %v", err)
		}
	})
	if !strings.Contains(out, "daemon: stopped") {
		t.Fatalf("status output = %q", out)
	}
	out = captureStdout(t, func() {
		if err := cmdDaemon([]string{"stop"}); err != nil {
			t.Fatalf("cmdDaemon stop: %v", err)
		}
	})
	if !strings.Contains(out, "daemon is not running") {
		t.Fatalf("stop output = %q", out)
	}
}

func TestCmdDaemonUsageAndUnknown(t *testing.T) {
	if err := cmdDaemon(nil); err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("cmdDaemon(nil) err = %v", err)
	}
	if err := cmdDaemon([]string{"bogus"}); err == nil || !strings.Contains(err.Error(), "unknown daemon subcommand") {
		t.Fatalf("cmdDaemon(bogus) err = %v", err)
	}
}

func TestQueryInt(t *testing.T) {
	cases := []struct {
		url  string
		key  string
		def  int
		want int
	}{
		{"/x?limit=7", "limit", 50, 7},
		{"/x?limit=abc", "limit", 50, 50},
		{"/x", "limit", 50, 50},
		{"/x?limit=-3", "limit", 50, -3},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		if got := queryInt(req, tc.key, tc.def); got != tc.want {
			t.Fatalf("queryInt(%s, %s, %d) = %d, want %d", tc.url, tc.key, tc.def, got, tc.want)
		}
	}
}

func TestWriteControlJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeControlJSON(rec, http.StatusTeapot, map[string]any{"hello": "world"})
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content type = %q", ct)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["hello"] != "world" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestControlHandlerAdditionalEndpoints(t *testing.T) {
	store := testControlStore(t)
	handler := controlHandler(store, "")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events?session_id=s-1&limit=5", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("events status = %d", rec.Code)
	}
	var eventsPayload struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &eventsPayload); err != nil {
		t.Fatal(err)
	}
	if len(eventsPayload.Events) != 1 {
		t.Fatalf("events = %+v", eventsPayload.Events)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/risk", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("risk status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty session id status = %d, want 400", rec.Code)
	}
}
