package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponseMarshalKeepsEmptyResult(t *testing.T) {
	data, err := json.Marshal(Response{JSONRPC: "2.0", ID: json.RawMessage("1"), Result: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, `"result":{}`) {
		t.Fatalf("empty result must be serialized: %s", got)
	}
	if strings.Contains(got, `"error"`) {
		t.Fatalf("success response must not carry error: %s", got)
	}

	data, err = json.Marshal(Response{JSONRPC: "2.0", ID: json.RawMessage("2"), Error: &RPCError{Code: CodeInvalidParams, Message: "bad"}})
	if err != nil {
		t.Fatal(err)
	}
	got = string(data)
	if strings.Contains(got, `"result"`) {
		t.Fatalf("error response must not carry result: %s", got)
	}
	if !strings.Contains(got, `"error":{"code":-32602`) {
		t.Fatalf("error payload missing: %s", got)
	}

	data, err = json.Marshal(Response{})
	if err != nil {
		t.Fatal(err)
	}
	got = string(data)
	if !strings.Contains(got, `"jsonrpc":"2.0"`) || !strings.Contains(got, `"id":null`) {
		t.Fatalf("defaults missing: %s", got)
	}
	if !strings.Contains(got, `"result":null`) {
		t.Fatalf("nil result must still be present: %s", got)
	}
}
