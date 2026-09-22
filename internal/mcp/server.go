package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"runtime/debug"
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

// oversizeFrame marks a discarded over-long input line in the line channel.
var oversizeFrame = []byte{0x00}

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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if in == nil {
		in = stdinReader()
	}
	if out == nil {
		out = stdoutWriter()
	}
	lines := make(chan []byte)
	scanErr := make(chan error, 1)
	go func() {
		defer close(lines)
		reader := bufio.NewReaderSize(in, 64*1024)
		for {
			scanner := bufio.NewScanner(reader)
			scanner.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
			for scanner.Scan() {
				line := bytes.TrimSpace(scanner.Bytes())
				if len(line) == 0 {
					continue
				}
				buf := append([]byte(nil), line...)
				select {
				case lines <- buf:
				case <-ctx.Done():
					scanErr <- ctx.Err()
					return
				}
			}
			err := scanner.Err()
			if errors.Is(err, bufio.ErrTooLong) {
				// Discard the remainder of the oversized frame in bounded
				// chunks and keep serving the session.
				for {
					_, derr := reader.ReadSlice('\n')
					if derr == nil {
						break
					}
					if errors.Is(derr, bufio.ErrBufferFull) {
						continue
					}
					scanErr <- nil
					return
				}
				select {
				case lines <- oversizeFrame:
				case <-ctx.Done():
					scanErr <- ctx.Err()
					return
				}
				continue
			}
			scanErr <- err
			return
		}
	}()
	enc := json.NewEncoder(out)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return <-scanErr
			}
			if bytes.Equal(line, oversizeFrame) {
				_ = enc.Encode(Response{
					JSONRPC: "2.0",
					ID:      json.RawMessage("null"),
					Error:   &RPCError{Code: CodeInvalidParams, Message: "request exceeds the maximum frame size"},
				})
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
	}
}

func (s *Server) handle(ctx context.Context, req *Request) *Response {
	if req.IsNotification() {
		return nil
	}
	resp := &Response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": DefaultProtocolVersion,
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
	return s.ToolDescriptors()
}

// ToolDescriptors returns the same descriptors served by tools/list, so CLI
// listings and the live server cannot drift apart.
func (s *Server) ToolDescriptors() []map[string]any {
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
	timeout := time.Duration(s.opts.CallTimeoutSec) * time.Second
	if s.opts.CallTimeoutSec <= 0 || timeout <= 0 || timeout > 24*time.Hour {
		timeout = 30 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	callCtx = observability.WithTrace(callCtx, observability.NewTraceID())

	start := time.Now()
	result, err := s.invokeTool(callCtx, tool, p.Arguments)
	duration := time.Since(start)
	status := "ok"
	if err != nil || resultStatus(result) == "error" {
		status = "error"
		s.inc("mcp_tool_errors_total")
	}
	if err != nil {
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
	isError := status == "error"
	if merr != nil {
		text = []byte(`{"status":"error","data":{"error":"result marshal failed"}}`)
		isError = true
	}
	resp.Result = map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(text)},
		},
		"isError": isError,
	}
}

func (s *Server) invokeTool(ctx context.Context, tool *Tool, args map[string]any) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			observability.Log(s.opts.Logger, "error", "tool_panic", map[string]any{
				"tool":  tool.Name,
				"error": fmt.Sprintf("%v", r),
				"stack": string(stack),
			})
			err = fmt.Errorf("tool %s panicked: %v", tool.Name, r)
		}
	}()
	return tool.Handler(ctx, args)
}

func resultStatus(result any) string {
	switch v := result.(type) {
	case Envelope:
		return v.Status
	case *Envelope:
		if v != nil {
			return v.Status
		}
	}
	return ""
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
			if result, ok := resp.Result.(map[string]any); ok {
				if isErr, ok := result["isError"].(bool); ok && isErr {
					summary["ok"] = false
				}
			}
		}
		summary["steps"] = append(summary["steps"].([]any), entry)
	}
	summary["tool_count"] = len(s.registry.Names())
	summary["tools"] = s.registry.Names()
	if err := enc.Encode(summary); err != nil {
		return err
	}
	if ok, _ := summary["ok"].(bool); !ok {
		return fmt.Errorf("selftest failed: one or more steps returned an error")
	}
	return nil
}
