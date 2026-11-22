package mcp

import "encoding/json"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r *Request) IsNotification() bool {
	// Only a missing id is a notification; an explicit "id": null is a
	// (discouraged) request and must still receive a response.
	return len(r.ID) == 0
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	// The field tags are used when decoding. MarshalJSON chooses which one to send.
	Result any       `json:"result"`
	Error  *RPCError `json:"error"`
}

// MarshalJSON keeps the JSON-RPC 2.0 wire contract: a response carries either
// result or error, and `result` is emitted even when it is an empty object
// (plain `omitempty` on Result silently dropped valid empty results).
func (r Response) MarshalJSON() ([]byte, error) {
	jsonrpc := r.JSONRPC
	if jsonrpc == "" {
		jsonrpc = "2.0"
	}
	id := r.ID
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	if r.Error != nil {
		return json.Marshal(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   *RPCError       `json:"error"`
		}{jsonrpc, id, r.Error})
	}
	return json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{jsonrpc, id, r.Result})
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeInvalidParams  = -32602
	CodeMethodNotFound = -32601
	CodeInternalError  = -32603
)
