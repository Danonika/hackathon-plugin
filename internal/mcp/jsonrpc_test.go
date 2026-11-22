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

func TestRejectMalformedRequestsOnWire(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"1.0","method":"ping","id":1}`,
		`{"jsonrpc":"2.0","method":42}`,
		`{"jsonrpc":"2.0","method":"ping","id":{}}`,
		`{"jsonrpc":"2.0","method":"ping"}`,
		`{"jsonrpc":"2.0","method":"ping","id":2}`,
	}, "\n")
	var output strings.Builder
	if err := NewServer(NewRegistry(), ServerOptions{}).ServeStreams(t.Context(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(output.String()))
	for _, id := range []string{"1", "null", "null"} {
		var response Response
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.Error == nil || response.Error.Code != CodeInvalidRequest || string(response.ID) != id {
			t.Fatalf("unexpected invalid request response %+v", response)
		}
	}
	var ping Response
	if err := decoder.Decode(&ping); err != nil || string(ping.ID) != "2" || ping.Result == nil {
		t.Fatalf("valid request after invalid frames: %+v, %v", ping, err)
	}
	if decoder.More() {
		t.Fatal("unexpected notification response")
	}
}
