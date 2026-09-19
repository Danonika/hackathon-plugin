package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"time"

	"hackathonkit.local/agentkit/internal/observability"
)

const DefaultProtocolVersion = "2025-06-18"

type ServerOptions struct {
	Name           string
	Version        string
	Logger         *log.Logger
	Metrics        *observability.Metrics
	CallTimeoutSec int
	CallLogPath    string
}

type Server struct {
	registry *Registry
	opts     ServerOptions
}

func NewServer(reg *Registry, opts ServerOptions) *Server {
	if opts.Name == "" {
		opts.Name = "hackathonkit-mcp"
	}
	if opts.Version == "" {
		opts.Version = "0.1.0"
	}
	if opts.CallTimeoutSec <= 0 {
		opts.CallTimeoutSec = 120
	}
	return &Server{registry: reg, opts: opts}
}

func (s *Server) Registry() *Registry {
	return s.registry
}

func (s *Server) Serve(ctx context.Context) error {
	return s.ServeStreams(ctx, nil, nil)
}

func (s *Server) ServeStreams(ctx context.Context, in io.Reader, out io.Writer) error {
	if in == nil {
		in = stdinReader()
	}
	if out == nil {
		out = stdoutWriter()
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(Response{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error:   &RPCError{Code: CodeParseError, Message: "parse error: " + err.Error()},
			})
			continue
		}
		resp := s.handle(ctx, &req)
		if resp == nil {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, req *Request) *Response {
	if req.IsNotification() {
		return nil
	}
	resp := &Response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		pv := p.ProtocolVersion
		if pv == "" {
			pv = DefaultProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": pv,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    s.opts.Name,
				"version": s.opts.Version,
			},
			"instructions": "Hackathon Agent Kit MCP capability layer. Call system.context first to read routing mode, then use capability tools. In fly mode call routing.fly_recommend before selecting domain tools.",
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.toolDescriptors()}
	case "tools/call":
		s.handleToolCall(ctx, req, resp)
	default:
		resp.Error = &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + req.Method}
	}
	return resp
}

func (s *Server) toolDescriptors() []map[string]any {
	tools := make([]map[string]any, 0, len(s.registry.List()))
	for _, t := range s.registry.List() {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": schema,
		})
	}
	return tools
}

func (s *Server) handleToolCall(ctx context.Context, req *Request, resp *Response) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		resp.Error = &RPCError{Code: CodeInvalidParams, Message: "invalid tools/call params: " + err.Error()}
		return
	}
	tool, ok := s.registry.Get(p.Name)
	if !ok {
		s.inc("mcp_tool_unknown_total")
		resp.Error = &RPCError{Code: CodeInvalidParams, Message: "unknown tool: " + p.Name}
		return
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}
	if s.opts.Metrics != nil {
		s.opts.Metrics.Inc("mcp_tool_calls_total")
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(s.opts.CallTimeoutSec)*time.Second)
	defer cancel()
	callCtx = observability.WithTrace(callCtx, observability.NewTraceID())

	start := time.Now()
	result, err := tool.Handler(callCtx, p.Arguments)
	duration := time.Since(start)
	status := "ok"
	if err != nil {
		status = "error"
		s.inc("mcp_tool_errors_total")
		result = Envelope{
			Status:  "error",
			Data:    map[string]any{"error": err.Error()},
			TraceID: observability.TraceFrom(callCtx),
		}
	}
	if s.opts.Metrics != nil {
		s.opts.Metrics.Add("tool_duration_ms_total", duration.Milliseconds())
	}
	observability.Log(s.opts.Logger, "info", "tool_call", map[string]any{
		"tool":        tool.Name,
		"duration_ms": duration.Milliseconds(),
		"status":      status,
		"trace_id":    observability.TraceFrom(callCtx),
	})
	if s.opts.CallLogPath != "" {
		_ = observability.AppendJSONL(s.opts.CallLogPath, map[string]any{
			"ts":          time.Now().UTC().Format(time.RFC3339Nano),
			"tool":        tool.Name,
			"duration_ms": duration.Milliseconds(),
			"status":      status,
			"trace_id":    observability.TraceFrom(callCtx),
		})
	}

	text, merr := json.MarshalIndent(result, "", "  ")
	if merr != nil {
		text = []byte(`{"status":"error","data":{"error":"result marshal failed"}}`)
	}
	resp.Result = map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(text)},
		},
		"isError": false,
	}
}

func (s *Server) inc(name string) {
	if s.opts.Metrics != nil {
		s.opts.Metrics.Inc(name)
	}
}

func (s *Server) SelfTest(ctx context.Context, out io.Writer) error {
	enc := json.NewEncoder(out)
	steps := []Request{
		{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize", Params: json.RawMessage(`{"protocolVersion":"` + DefaultProtocolVersion + `","clientInfo":{"name":"selftest","version":"0"}}`)},
		{JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "tools/list"},
		{JSONRPC: "2.0", ID: json.RawMessage("3"), Method: "tools/call", Params: json.RawMessage(`{"name":"system.context","arguments":{}}`)},
	}
	summary := map[string]any{"ok": true, "steps": []any{}}
	ctx = observability.WithTrace(ctx, observability.NewTraceID())
	for _, req := range steps {
		resp := s.handle(ctx, &req)
		if resp == nil {
			continue
		}
		entry := map[string]any{"method": req.Method, "id": string(req.ID)}
		if resp.Error != nil {
			entry["error"] = resp.Error.Message
			summary["ok"] = false
		} else {
			entry["result"] = resp.Result
		}
		summary["steps"] = append(summary["steps"].([]any), entry)
	}
	summary["tool_count"] = len(s.registry.Names())
	summary["tools"] = s.registry.Names()
	return enc.Encode(summary)
}
