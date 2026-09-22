package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/observability"
)

func TestContractHelpers(t *testing.T) {
	if got := IntPtr(7); got == nil || *got != 7 {
		t.Fatalf("IntPtr = %v", got)
	}
	env := Partial("t-1", map[string]any{"n": 1}, "w1")
	if env.Status != "partial" || env.TraceID != "t-1" || len(env.Warnings) != 1 {
		t.Fatalf("Partial = %+v", env)
	}
	env = env.WithSources(Source{ArtifactID: "a"}, Source{Page: IntPtr(0)}).
		WithWarnings("w2").
		WithMetrics(map[string]any{"m": 2})
	if len(env.Sources) != 2 || len(env.Warnings) != 2 {
		t.Fatalf("WithSources/WithWarnings = %+v", env)
	}
	if env.Metrics["m"] != 2 {
		t.Fatalf("WithMetrics = %+v", env.Metrics)
	}
	env = env.WithMetrics(map[string]any{"n": 3})
	if env.Metrics["m"] != 2 || env.Metrics["n"] != 3 {
		t.Fatalf("WithMetrics merge = %+v", env.Metrics)
	}
}

func TestRegistryNames(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Tool{Name: "b"})
	reg.Register(Tool{Name: "a"})
	reg.Register(Tool{Name: "b"})
	names := reg.Names()
	if len(names) != 2 || names[0] != "b" || names[1] != "a" {
		t.Fatalf("Names = %v", names)
	}
	names[0] = "mutated"
	if reg.Names()[0] != "b" {
		t.Fatal("Names must return a copy")
	}
}

func TestServerRegistryAccessor(t *testing.T) {
	reg := NewRegistry()
	srv := NewServer(reg, ServerOptions{})
	if srv.Registry() != reg {
		t.Fatal("Registry() must return the wrapped registry")
	}
}

func TestToolDescriptorsNilSchema(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Tool{Name: "no.schema", Description: "no schema"})
	srv := NewServer(reg, ServerOptions{})
	descriptors := srv.ToolDescriptors()
	if len(descriptors) != 1 {
		t.Fatalf("descriptors = %v", descriptors)
	}
	schema, ok := descriptors[0]["inputSchema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("default schema = %#v", descriptors[0]["inputSchema"])
	}
}

func TestHandleMethods(t *testing.T) {
	srv := NewServer(NewRegistry(), ServerOptions{Name: "test", Version: "9"})

	initResp := srv.handle(context.Background(), &Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize"})
	result, ok := initResp.Result.(map[string]any)
	if !ok {
		t.Fatalf("initialize result = %#v", initResp.Result)
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] != "test" || serverInfo["version"] != "9" {
		t.Fatalf("serverInfo = %#v", result["serverInfo"])
	}

	ping := srv.handle(context.Background(), &Request{JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "ping"})
	if ping.Error != nil || ping.Result == nil {
		t.Fatalf("ping = %+v", ping)
	}

	missing := srv.handle(context.Background(), &Request{JSONRPC: "2.0", ID: json.RawMessage("3"), Method: "does/not/exist"})
	if missing.Error == nil || missing.Error.Code != CodeMethodNotFound {
		t.Fatalf("unknown method = %+v", missing.Error)
	}

	if got := srv.handle(context.Background(), &Request{Method: "ping"}); got != nil {
		t.Fatal("notifications must not produce a response")
	}
}

func TestHandleToolCallInvalidParams(t *testing.T) {
	srv := NewServer(NewRegistry(), ServerOptions{})
	resp := &Response{}
	srv.handleToolCall(context.Background(), &Request{Params: json.RawMessage(`"not-an-object"`)}, resp)
	if resp.Error == nil || resp.Error.Code != CodeInvalidParams {
		t.Fatalf("invalid params error = %+v", resp.Error)
	}
}

func TestHandleToolCallMetricsAndCallLog(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Tool{
		Name: "danger.err",
		Handler: func(context.Context, map[string]any) (any, error) {
			return &Envelope{Status: "error", TraceID: "t"}, nil
		},
	})
	reg.Register(Tool{
		Name: "unmarshalable",
		Handler: func(context.Context, map[string]any) (any, error) {
			return make(chan int), nil
		},
	})
	metrics := observability.NewMetrics()
	logPath := filepath.Join(t.TempDir(), "logs", "calls.jsonl")
	srv := NewServer(reg, ServerOptions{Metrics: metrics, CallLogPath: logPath})

	resp := &Response{}
	srv.handleToolCall(context.Background(), &Request{Params: json.RawMessage(`{"name":"danger.err","arguments":{}}`)}, resp)
	res := resp.Result.(map[string]any)
	if res["isError"] != true {
		t.Fatalf("pointer error envelope must set isError: %#v", res)
	}

	resp = &Response{}
	srv.handleToolCall(context.Background(), &Request{Params: json.RawMessage(`{"name":"unmarshalable","arguments":null}`)}, resp)
	res = resp.Result.(map[string]any)
	if res["isError"] != true {
		t.Fatalf("marshal failure must set isError: %#v", res)
	}
	content := res["content"].([]map[string]any)
	if !strings.Contains(content[0]["text"].(string), "result marshal failed") {
		t.Fatalf("marshal failure text = %q", content[0]["text"])
	}

	resp = &Response{}
	srv.handleToolCall(context.Background(), &Request{Params: json.RawMessage(`{"name":"missing.tool"}`)}, resp)
	if resp.Error == nil {
		t.Fatal("unknown tool must produce an error")
	}

	snapshot := metrics.Snapshot()
	if snapshot["mcp_tool_calls_total"] != 2 {
		t.Fatalf("calls metric = %v", snapshot)
	}
	if snapshot["mcp_tool_errors_total"] != 1 {
		t.Fatalf("errors metric = %v", snapshot)
	}
	if snapshot["mcp_tool_unknown_total"] != 1 {
		t.Fatalf("unknown metric = %v", snapshot)
	}
	if _, ok := snapshot["tool_duration_ms_total"]; !ok {
		t.Fatalf("duration metric missing: %v", snapshot)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(data)), "\n"); len(lines) != 2 {
		t.Fatalf("call log lines = %d: %s", len(lines), data)
	}
}

func TestResultStatus(t *testing.T) {
	if got := resultStatus(OK("t", nil)); got != "ok" {
		t.Fatalf("value envelope = %q", got)
	}
	if got := resultStatus(&Envelope{Status: "partial"}); got != "partial" {
		t.Fatalf("pointer envelope = %q", got)
	}
	var nilEnvelope *Envelope
	if got := resultStatus(nilEnvelope); got != "" {
		t.Fatalf("nil envelope = %q", got)
	}
	if got := resultStatus(map[string]any{}); got != "" {
		t.Fatalf("other result = %q", got)
	}
}

func TestInvokeToolError(t *testing.T) {
	srv := NewServer(NewRegistry(), ServerOptions{})
	tool := &Tool{
		Name: "boom",
		Handler: func(context.Context, map[string]any) (any, error) {
			return nil, errors.New("handler failed")
		},
	}
	if _, err := srv.invokeTool(context.Background(), tool, nil); err == nil {
		t.Fatal("expected handler error")
	}
}

func TestIncWithoutMetrics(t *testing.T) {
	srv := NewServer(NewRegistry(), ServerOptions{})
	srv.inc("noop")
}

func TestSelfTestSuccess(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Tool{
		Name: "system.context",
		Handler: func(context.Context, map[string]any) (any, error) {
			return OK("t", map[string]any{"routing_mode": "normal"}), nil
		},
	})
	srv := NewServer(reg, ServerOptions{})

	var out bytes.Buffer
	if err := srv.SelfTest(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	var summary struct {
		OK        bool             `json:"ok"`
		Steps     []map[string]any `json:"steps"`
		ToolCount int              `json:"tool_count"`
		Tools     []string         `json:"tools"`
	}
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if !summary.OK || len(summary.Steps) != 3 || summary.ToolCount != 1 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestSelfTestFailure(t *testing.T) {
	srv := NewServer(NewRegistry(), ServerOptions{})
	var out bytes.Buffer
	err := srv.SelfTest(context.Background(), &out)
	if err == nil || !strings.Contains(err.Error(), "selftest failed") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out.String(), `"ok":false`) {
		t.Fatalf("output = %s", out.String())
	}
}

func TestServeWithDefaults(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	oldStdin := os.Stdin
	os.Stdin = devNull
	t.Cleanup(func() { os.Stdin = oldStdin })

	srv := NewServer(NewRegistry(), ServerOptions{})
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatalf("Serve = %v", err)
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestSelfTestEncodeError(t *testing.T) {
	srv := NewServer(NewRegistry(), ServerOptions{})
	if err := srv.SelfTest(context.Background(), errWriter{}); err == nil {
		t.Fatal("expected encode error")
	}
}

func TestServeStreamsParseErrorAndBlankLines(t *testing.T) {
	srv := NewServer(NewRegistry(), ServerOptions{})
	input := "\n  \nnot json\n" +
		`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
	var out bytes.Buffer
	if err := srv.ServeStreams(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("responses = %d: %s", len(lines), out.String())
	}
	var parseResp struct {
		Error *RPCError `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &parseResp); err != nil {
		t.Fatal(err)
	}
	if parseResp.Error == nil || parseResp.Error.Code != CodeParseError {
		t.Fatalf("parse response = %s", lines[0])
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestServeStreamsScanError(t *testing.T) {
	srv := NewServer(NewRegistry(), ServerOptions{})
	err := srv.ServeStreams(context.Background(), errReader{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeStreamsEncodeError(t *testing.T) {
	srv := NewServer(NewRegistry(), ServerOptions{})
	input := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
	if err := srv.ServeStreams(context.Background(), strings.NewReader(input), errWriter{}); err == nil {
		t.Fatal("expected encode error")
	}
}

func TestServeStreamsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := NewServer(NewRegistry(), ServerOptions{})
	err := srv.ServeStreams(ctx, strings.NewReader(""), io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamDefaults(t *testing.T) {
	if stdinReader() == nil {
		t.Fatal("stdinReader must not be nil")
	}
	if stdoutWriter() == nil {
		t.Fatal("stdoutWriter must not be nil")
	}
}
