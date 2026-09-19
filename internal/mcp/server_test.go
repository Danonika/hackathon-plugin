package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestServeStreams(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Tool{
		Name:        "system.ping",
		Description: "test tool",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return OK("t-1", map[string]any{"pong": true}), nil
		},
	})
	srv := NewServer(reg, ServerOptions{})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.ping","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"missing.tool","arguments":{}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := srv.ServeStreams(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 responses (notification produces none), got %d: %s", len(lines), out.String())
	}
	var listResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Result.Tools) != 1 || listResp.Result.Tools[0].Name != "system.ping" {
		t.Fatalf("unexpected tools/list result: %s", lines[1])
	}
	var callResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &callResp); err != nil {
		t.Fatal(err)
	}
	if len(callResp.Result.Content) != 1 || !strings.Contains(callResp.Result.Content[0].Text, `"pong": true`) {
		t.Fatalf("unexpected tools/call result: %s", lines[2])
	}
	var errResp struct {
		Error *RPCError `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &errResp); err != nil {
		t.Fatal(err)
	}
	if errResp.Error == nil || errResp.Error.Code != CodeInvalidParams {
		t.Fatalf("expected unknown tool error, got %s", lines[3])
	}
}
