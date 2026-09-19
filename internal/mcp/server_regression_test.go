package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolPanicIsContained(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Tool{
		Name:        "danger.panic",
		Description: "panics",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			panic("slice bounds out of range")
		},
	})
	reg.Register(Tool{
		Name:        "danger.fail",
		Description: "returns an error envelope",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return Fail("trace-1", "bad input"), nil
		},
	})
	srv := NewServer(reg, ServerOptions{})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"danger.panic","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"danger.fail","arguments":{}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := srv.ServeStreams(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %s", len(lines), out.String())
	}
	for i, line := range lines {
		var resp struct {
			Error  *RPCError `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Error != nil {
			t.Fatalf("response %d: panic must not become a JSON-RPC error: %s", i, line)
		}
		if !resp.Result.IsError {
			t.Fatalf("response %d: expected isError=true, got %s", i, line)
		}
	}
	if !strings.Contains(lines[0], "panicked") {
		t.Fatalf("panic response must name the failure: %s", lines[0])
	}
	if !strings.Contains(lines[1], "bad input") {
		t.Fatalf("fail envelope must keep its message: %s", lines[1])
	}
}
