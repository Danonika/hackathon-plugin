package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/control"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/plans"
)

type fakeMeta struct {
	routingRuns []metadata.RoutingRun
}

func (f *fakeMeta) Enabled() bool { return true }
func (f *fakeMeta) RecordArtifact(context.Context, metadata.ArtifactRecord) error {
	return nil
}
func (f *fakeMeta) RecordRoutingRun(_ context.Context, run metadata.RoutingRun) error {
	f.routingRuns = append(f.routingRuns, run)
	return nil
}
func (f *fakeMeta) RecordFlyPrediction(context.Context, metadata.FlyPrediction) error {
	return nil
}
func (f *fakeMeta) RecordEvalRun(context.Context, metadata.EvalRun) error { return nil }
func (f *fakeMeta) Close()                                                {}

func testHandler(root string, mgr *config.Manager, meta metadata.Recorder, token string) http.Handler {
	return newHandler(root, mgr, meta, token, log.New(io.Discard, "", 0))
}

func doRequest(t *testing.T, h http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return out
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStaticPages(t *testing.T) {
	root := t.TempDir()
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "s3cret")

	for _, path := range []string{"/", "/fly-viz", "/control", "/plan"} {
		rec := doRequest(t, h, http.MethodGet, path, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Fatalf("GET %s content-type = %q", path, got)
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("GET %s returned an empty body", path)
		}
	}

	// The token must never be baked into the served pages: operators enter it
	// in the page and it is kept in sessionStorage.
	for _, path := range []string{"/", "/plan", "/control", "/fly-viz"} {
		page := doRequest(t, h, http.MethodGet, path, "", nil)
		if body := page.Body.String(); strings.Contains(body, "s3cret") || strings.Contains(body, "__HACKATHONKIT_DEMO_TOKEN__") {
			t.Fatalf("GET %s leaks or embeds the demo token", path)
		}
	}

	rec := doRequest(t, h, http.MethodGet, "/does-not-exist", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /does-not-exist = %d, want 404", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	root := t.TempDir()
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "")
	rec := doRequest(t, h, http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := decodeBody[map[string]any](t, rec)
	if body["status"] != "ok" || body["root"] != root || body["mode"] != "normal" {
		t.Fatalf("unexpected healthz body: %v", body)
	}
}

func TestRoutingEndpoint(t *testing.T) {
	root := t.TempDir()
	mgr := config.NewManager(root)
	meta := &fakeMeta{}
	h := testHandler(root, mgr, meta, "s3cret")

	rec := doRequest(t, h, http.MethodGet, "/routing", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /routing = %d", rec.Code)
	}
	if state := decodeBody[config.RoutingState](t, rec); state.Mode != "normal" || state.Version != 1 {
		t.Fatalf("initial routing = %+v", state)
	}

	if rec := doRequest(t, h, http.MethodPost, "/routing", `{"mode":"fly"}`, map[string]string{"Origin": "http://evil.example"}); rec.Code != http.StatusForbidden {
		t.Fatalf("bad origin = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, h, http.MethodPost, "/routing", `{"mode":"fly"}`, map[string]string{"X-HackathonKit-Token": "wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token = %d, want 401", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodPost, "/routing", `not json`, map[string]string{"X-HackathonKit-Token": "s3cret"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodPost, "/routing", `{"mode":"turbo"}`, map[string]string{"X-HackathonKit-Token": "s3cret"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad mode = %d, want 400", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodDelete, "/routing", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE = %d, want 405", rec.Code)
	}

	rec = doRequest(t, h, http.MethodPost, "/routing", `{"mode":"fly"}`, map[string]string{
		"X-HackathonKit-Token": "s3cret",
		"Origin":           "http://localhost:8787",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("valid toggle = %d: %s", rec.Code, rec.Body.String())
	}
	state := decodeBody[config.RoutingState](t, rec)
	if state.Mode != "fly" || state.Version != 2 {
		t.Fatalf("toggled routing = %+v", state)
	}
	data, err := os.ReadFile(mgr.RoutingPath())
	if err != nil {
		t.Fatalf("routing.yaml not written: %v", err)
	}
	if !strings.Contains(string(data), "mode: fly") {
		t.Fatalf("routing.yaml = %s", data)
	}
	if len(meta.routingRuns) != 1 || meta.routingRuns[0].Mode != "fly" {
		t.Fatalf("metadata runs = %+v", meta.routingRuns)
	}

	rec = doRequest(t, h, http.MethodGet, "/routing", "", nil)
	if got := decodeBody[config.RoutingState](t, rec); got.Mode != "fly" {
		t.Fatalf("GET after toggle = %+v", got)
	}
}

func TestControlEndpointsWithoutDatabase(t *testing.T) {
	root := t.TempDir()
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "")

	if rec := doRequest(t, h, http.MethodGet, "/control/api/sessions", "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("sessions without db = %d, want 503", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodGet, "/control/api/session", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("session without id = %d, want 400", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodGet, "/control/api/session?id=x", "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("session without db = %d, want 503", rec.Code)
	}
}

func TestControlEndpointsWithDatabase(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	store, err := control.Open(control.DefaultPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSession(ctx, control.Session{ID: "sess-1", Root: root, Kind: "codex", Task: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(ctx, control.Event{SessionID: "sess-1", Type: "mcp_call", Tool: "files.describe", Status: "ok", Payload: map[string]any{"a": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendOutput(ctx, control.Output{SessionID: "sess-1", Kind: "artifact", Ref: "data/artifacts/x"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "")

	rec := doRequest(t, h, http.MethodGet, "/control/api/sessions", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions = %d: %s", rec.Code, rec.Body.String())
	}
	sessions := decodeBody[struct {
		Sessions []control.Session `json:"sessions"`
	}](t, rec)
	if len(sessions.Sessions) != 1 || sessions.Sessions[0].ID != "sess-1" {
		t.Fatalf("sessions = %+v", sessions.Sessions)
	}

	rec = doRequest(t, h, http.MethodGet, "/control/api/session?id=sess-1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("session = %d: %s", rec.Code, rec.Body.String())
	}
	detail := decodeBody[struct {
		Session control.Session  `json:"session"`
		Events  []control.Event  `json:"events"`
		Outputs []control.Output `json:"outputs"`
	}](t, rec)
	if detail.Session.ID != "sess-1" || len(detail.Events) != 1 || len(detail.Outputs) != 1 {
		t.Fatalf("session detail = %+v", detail)
	}

	if rec := doRequest(t, h, http.MethodGet, "/control/api/session?id=missing", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session = %d, want 404", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodGet, "/control/api/sessions?limit=1", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("sessions limit = %d", rec.Code)
	}
}

func TestPlanEndpoints(t *testing.T) {
	root := t.TempDir()
	mgr := config.NewManager(root)
	h := testHandler(root, mgr, metadata.Disabled{}, "s3cret")

	if rec := doRequest(t, h, http.MethodGet, "/plan/api/latest", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("latest without plan = %d", rec.Code)
	} else {
		body := decodeBody[map[string]any](t, rec)
		if body["plan"] != nil || body["approval"] != nil {
			t.Fatalf("latest = %v", body)
		}
	}

	if rec := doRequest(t, h, http.MethodGet, "/plan/decision", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET decision = %d, want 405", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"decision":"approve"}`, map[string]string{"Origin": "http://evil.example"}); rec.Code != http.StatusForbidden {
		t.Fatalf("bad origin = %d, want 403", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"decision":"approve"}`, map[string]string{"X-HackathonKit-Token": "nope"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token = %d, want 401", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodPost, "/plan/decision", `not json`, map[string]string{"X-HackathonKit-Token": "s3cret"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"decision":"maybe"}`, map[string]string{"X-HackathonKit-Token": "s3cret"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad decision = %d, want 400", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"decision":"approve"}`, map[string]string{"X-HackathonKit-Token": "s3cret"}); rec.Code != http.StatusNotFound {
		t.Fatalf("decision without plan = %d, want 404", rec.Code)
	}

	if err := plans.Save(root, plans.Plan{
		Slug:     "demo-plan",
		Task:     "raise coverage",
		Stages:   plans.DefaultStages,
		Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"decision":"approve","note":"looks good","revision":1}`, map[string]string{"X-HackathonKit-Token": "s3cret"})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d: %s", rec.Code, rec.Body.String())
	}
	approval := decodeBody[struct {
		Approval planApproval `json:"approval"`
	}](t, rec)
	if approval.Approval.Slug != "demo-plan" || approval.Approval.Decision != "approve" || approval.Approval.Note != "looks good" {
		t.Fatalf("approval = %+v", approval.Approval)
	}
	if _, err := os.Stat(approvalPath(root, "demo-plan")); err != nil {
		t.Fatalf("approval file missing: %v", err)
	}
	if loaded, err := readApproval(root, "demo-plan"); err != nil || loaded == nil || loaded.Decision != "approve" {
		t.Fatalf("readApproval = %+v err=%v", loaded, err)
	}

	rec = doRequest(t, h, http.MethodGet, "/plan/api/latest", "", nil)
	latest := decodeBody[struct {
		Plan     *plans.Plan   `json:"plan"`
		Approval *planApproval `json:"approval"`
	}](t, rec)
	if latest.Plan == nil || latest.Plan.Slug != "demo-plan" || latest.Approval == nil {
		t.Fatalf("latest = %+v", latest)
	}

	rec = doRequest(t, h, http.MethodPost, "/plan/decision", `{"decision":"request_changes","note":"tighten S4","revision":1}`, map[string]string{"X-HackathonKit-Token": "s3cret"})
	if rec.Code != http.StatusOK {
		t.Fatalf("request_changes = %d: %s", rec.Code, rec.Body.String())
	}
	if loaded, err := readApproval(root, "demo-plan"); err != nil || loaded == nil || loaded.Decision != "request_changes" {
		t.Fatalf("readApproval after changes = %+v err=%v", loaded, err)
	}

	if rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"slug":"missing-plan","decision":"approve"}`, map[string]string{"X-HackathonKit-Token": "s3cret"}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown slug = %d, want 404", rec.Code)
	}

	rec = doRequest(t, h, http.MethodPost, "/plan/decision", `{"slug":"demo-plan","decision":"approve","revision":1}`, map[string]string{"X-HackathonKit-Token": "s3cret"})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve by slug = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPlanDecisionApprovalWriteFailure(t *testing.T) {
	root := t.TempDir()
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "s3cret")
	if err := plans.Save(root, plans.Plan{Slug: "broken-plan", Stages: []plans.Stage{{ID: "S0", Status: "todo"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(approvalPath(root, "broken-plan"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(approvalPath(root, "broken-plan"), "keep"), "x")
	rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"decision":"approve","revision":1}`, map[string]string{"X-HackathonKit-Token": "s3cret"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("decision with blocked approval path = %d, want 500: %s", rec.Code, rec.Body.String())
	}
}

func TestInputsAndArtifacts(t *testing.T) {
	root := t.TempDir()
	mgr := config.NewManager(root)
	h := testHandler(root, mgr, metadata.Disabled{}, "")

	rec := doRequest(t, h, http.MethodGet, "/inputs", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("inputs = %d", rec.Code)
	}
	if files := decodeBody[struct {
		Files []fileInfo `json:"files"`
	}](t, rec).Files; len(files) != 0 {
		t.Fatalf("empty inputs = %+v", files)
	}

	writeTestFile(t, filepath.Join(root, "data", "input", "b.txt"), "bbb")
	writeTestFile(t, filepath.Join(root, "data", "input", "a.txt"), "a")
	writeTestFile(t, filepath.Join(root, "data", "input", "nested", "c.txt"), "cc")
	writeTestFile(t, filepath.Join(root, "data", "artifacts", "result.bin"), "binary")

	rec = doRequest(t, h, http.MethodGet, "/inputs", "", nil)
	files := decodeBody[struct {
		Files []fileInfo `json:"files"`
	}](t, rec).Files
	if len(files) != 3 || files[0].Path != "a.txt" || files[1].Path != "b.txt" || files[2].Path != "nested/c.txt" {
		t.Fatalf("inputs = %+v", files)
	}
	if files[0].SizeBytes != 1 || files[0].Modified == "" {
		t.Fatalf("file metadata = %+v", files[0])
	}

	rec = doRequest(t, h, http.MethodGet, "/artifacts", "", nil)
	artifacts := decodeBody[struct {
		Files []fileInfo `json:"files"`
	}](t, rec).Files
	if len(artifacts) != 1 || artifacts[0].Path != "result.bin" {
		t.Fatalf("artifacts = %+v", artifacts)
	}

	if listed, err := listFiles(mgr, "data/does-not-exist"); err != nil || len(listed) != 0 {
		t.Fatalf("listFiles missing dir = %+v, %v", listed, err)
	}
}

func TestListFilesStopsAtLimit(t *testing.T) {
	root := t.TempDir()
	mgr := config.NewManager(root)
	dir := filepath.Join(root, "data", "input")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 505; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := listFiles(mgr, "data/input")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 500 {
		t.Fatalf("expected the walk to stop beyond the cap, got %d", len(files))
	}
}

func TestInputsPermissionError(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("chmod cannot make a directory unreadable for root or on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "data", "input")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "")
	rec := doRequest(t, h, http.MethodGet, "/inputs", "", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("unreadable inputs = %d, want 500", rec.Code)
	}
}

func TestLogTailEndpoints(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "logs", "mcp-calls.jsonl"), "{\"n\":1}\n{\"n\":2}\n{\"n\":3}\n")
	writeTestFile(t, filepath.Join(root, "logs", "fly-feedback.jsonl"), "{\"ok\":true}\n")
	writeTestFile(t, filepath.Join(root, "logs", "fly-neuron-events.jsonl"), "{\"neuron\":\"x\"}\n")
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "")

	rec := doRequest(t, h, http.MethodGet, "/calls?limit=2", "", nil)
	calls := decodeBody[struct {
		Calls []json.RawMessage `json:"calls"`
	}](t, rec).Calls
	if len(calls) != 2 {
		t.Fatalf("calls = %s", rec.Body.String())
	}
	var first map[string]int
	if err := json.Unmarshal(calls[0], &first); err != nil || first["n"] != 2 {
		t.Fatalf("first call = %s (%v)", calls[0], err)
	}

	rec = doRequest(t, h, http.MethodGet, "/fly-events", "", nil)
	if events := decodeBody[struct {
		Events []json.RawMessage `json:"events"`
	}](t, rec).Events; len(events) != 0 {
		t.Fatalf("missing fly-events = %s", rec.Body.String())
	}

	rec = doRequest(t, h, http.MethodGet, "/fly-feedback", "", nil)
	if events := decodeBody[struct {
		Events []json.RawMessage `json:"events"`
	}](t, rec).Events; len(events) != 1 {
		t.Fatalf("fly-feedback = %s", rec.Body.String())
	}

	rec = doRequest(t, h, http.MethodGet, "/fly-neuron-events", "", nil)
	if events := decodeBody[struct {
		Events []json.RawMessage `json:"events"`
	}](t, rec).Events; len(events) != 1 {
		t.Fatalf("fly-neuron-events = %s", rec.Body.String())
	}
}

func TestTailAndQueryLimitHelpers(t *testing.T) {
	root := t.TempDir()
	logger := log.New(io.Discard, "", 0)
	if got := tail(logger, root, "logs/missing.jsonl", 5); len(got) != 0 {
		t.Fatalf("missing tail = %+v", got)
	}
	writeTestFile(t, filepath.Join(root, "logs", "x.jsonl"), "{\"a\":1}\n")
	if got := tail(logger, root, "logs/x.jsonl", 3); len(got) != 1 {
		t.Fatalf("tail = %+v", got)
	}

	r := httptest.NewRequest(http.MethodGet, "/calls?limit=7", nil)
	if got := queryLimit(r, 50); got != 7 {
		t.Fatalf("queryLimit valid = %d", got)
	}
	r = httptest.NewRequest(http.MethodGet, "/calls?limit=abc", nil)
	if got := queryLimit(r, 50); got != 50 {
		t.Fatalf("queryLimit invalid = %d", got)
	}
	r = httptest.NewRequest(http.MethodGet, "/calls", nil)
	if got := queryLimit(r, 50); got != 50 {
		t.Fatalf("queryLimit default = %d", got)
	}
}

func TestMetrics(t *testing.T) {
	root := t.TempDir()
	mgr := config.NewManager(root)
	h := testHandler(root, mgr, metadata.Disabled{}, "")
	if _, err := mgr.SetRoutingMode("fly"); err != nil {
		t.Fatal(err)
	}
	rec := doRequest(t, h, http.MethodGet, "/metrics", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `hackathonkit_routing_mode{mode="fly"} 1`) {
		t.Fatalf("metrics body = %q", rec.Body.String())
	}
}

func TestFlyModelEndpoints(t *testing.T) {
	root := t.TempDir()
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "")

	rec := doRequest(t, h, http.MethodGet, "/fly-model-list", "", nil)
	if files := decodeBody[struct {
		Files []string `json:"files"`
	}](t, rec).Files; len(files) != 0 {
		t.Fatalf("empty model list = %+v", files)
	}
	if rec := doRequest(t, h, http.MethodGet, "/fly-model/a.obj", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing meshes = %d, want 404", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodGet, "/fly-brain/a.json", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing brain assets = %d, want 404", rec.Code)
	}

	writeTestFile(t, filepath.Join(root, "data", "fly-viz", "neuromechfly", "b.stl"), "stl")
	writeTestFile(t, filepath.Join(root, "data", "fly-viz", "neuromechfly", "nested", "a.obj"), "obj")
	writeTestFile(t, filepath.Join(root, "data", "fly-viz", "neuromechfly", "ignore.txt"), "txt")
	writeTestFile(t, filepath.Join(root, "data", "fly-viz", "flybrain", "brain.json"), `{"ok":true}`)

	rec = doRequest(t, h, http.MethodGet, "/fly-model-list", "", nil)
	files := decodeBody[struct {
		Files []string `json:"files"`
	}](t, rec).Files
	if len(files) != 2 || files[0] != "b.stl" || files[1] != "nested/a.obj" {
		t.Fatalf("model list = %+v", files)
	}
	if rec := doRequest(t, h, http.MethodGet, "/fly-model/b.stl", "", nil); rec.Code != http.StatusOK || rec.Body.String() != "stl" {
		t.Fatalf("mesh fetch = %d %q", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, h, http.MethodGet, "/fly-brain/brain.json", "", nil); rec.Code != http.StatusOK || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("brain fetch = %d %q", rec.Code, rec.Body.String())
	}
}

func TestWithCORS(t *testing.T) {
	root := t.TempDir()
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "")

	// A different loopback port must not receive ACAO: reflecting it would let a
	// page on any local port read token-bearing responses such as /fly-viz.
	rec := doRequest(t, h, http.MethodOptions, "/healthz", "", map[string]string{"Origin": "http://localhost:5173"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("foreign loopback origin got ACAO %q", got)
	}

	// The server's own origin is reflected.
	rec = doRequest(t, h, http.MethodOptions, "/healthz", "", map[string]string{"Origin": "http://example.com"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Fatalf("ACAO = %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" || rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatalf("preflight headers missing: %v", rec.Header())
	}
	if !strings.Contains(strings.Join(rec.Header().Values("Vary"), ","), "Origin") {
		t.Fatalf("Vary = %v", rec.Header().Values("Vary"))
	}

	rec = doRequest(t, h, http.MethodGet, "/healthz", "", map[string]string{"Origin": "http://evil.example"})
	if rec.Code != http.StatusOK {
		t.Fatalf("denied origin GET = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("denied origin got ACAO %q", got)
	}

	rec = doRequest(t, h, http.MethodGet, "/healthz", "", nil)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("no origin got ACAO %q", got)
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusTeapot, map[string]any{"a": 1})
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "\"a\": 1") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestApprovalHelpers(t *testing.T) {
	root := t.TempDir()
	if got, want := approvalPath(root, "p"), filepath.Join(root, "evals", "plans", "p.approval.json"); got != want {
		t.Fatalf("approvalPath = %q, want %q", got, want)
	}
	if got, err := readApproval(root, "missing"); err != nil || got != nil {
		t.Fatalf("readApproval missing = %+v err=%v", got, err)
	}

	record, err := writeApproval(root, "p", 2, "approve", "note")
	if err != nil {
		t.Fatal(err)
	}
	if record.Slug != "p" || record.Revision != 2 || record.DecidedAt.IsZero() {
		t.Fatalf("record = %+v", record)
	}
	loaded, err := readApproval(root, "p")
	if err != nil || loaded == nil || loaded.Decision != "approve" || loaded.Note != "note" {
		t.Fatalf("loaded = %+v err=%v", loaded, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(approvalPath(root, "p"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("approval mode = %v, want 0600", info.Mode().Perm())
		}
	}

	writeTestFile(t, approvalPath(root, "corrupt"), "{not json")
	if got, err := readApproval(root, "corrupt"); err == nil || got != nil {
		t.Fatalf("corrupt readApproval = %+v err=%v (want an error)", got, err)
	}

	blockedRoot := t.TempDir()
	writeTestFile(t, filepath.Join(blockedRoot, "evals"), "a file blocks the plans dir")
	if _, err := writeApproval(blockedRoot, "p", 1, "approve", ""); err == nil {
		t.Fatal("writeApproval must fail when evals is a file")
	}
}
func TestPlanDecisionRevisionGuard(t *testing.T) {
	root := t.TempDir()
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "s3cret")
	if err := plans.Save(root, plans.Plan{Slug: "rev-plan", Stages: plans.DefaultStages, Revision: 3}); err != nil {
		t.Fatal(err)
	}
	rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"slug":"rev-plan","decision":"approve","revision":2}`, map[string]string{"X-HackathonKit-Token": "s3cret"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale revision = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	// An omitted revision (zero) and a negative revision must not bypass the
	// guard.
	rec = doRequest(t, h, http.MethodPost, "/plan/decision", `{"slug":"rev-plan","decision":"approve"}`, map[string]string{"X-HackathonKit-Token": "s3cret"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("omitted revision = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, h, http.MethodPost, "/plan/decision", `{"slug":"rev-plan","decision":"approve","revision":-1}`, map[string]string{"X-HackathonKit-Token": "s3cret"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("negative revision = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, h, http.MethodPost, "/plan/decision", `{"slug":"rev-plan","decision":"approve","revision":3}`, map[string]string{"X-HackathonKit-Token": "s3cret"})
	if rec.Code != http.StatusOK {
		t.Fatalf("matching revision = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMutationWithoutTokenRequiresOrigin(t *testing.T) {
	root := t.TempDir()
	h := testHandler(root, config.NewManager(root), metadata.Disabled{}, "")
	if err := plans.Save(root, plans.Plan{Slug: "origin-plan", Stages: plans.DefaultStages, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"decision":"approve"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("header-less POST = %d, want 401", rec.Code)
	}
	// Origin alone is spoofable, so a token-less mutation stays unauthorized.
	if rec := doRequest(t, h, http.MethodPost, "/plan/decision", `{"decision":"approve"}`, map[string]string{"Origin": "http://127.0.0.1:8787"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("loopback-origin POST without token = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}
