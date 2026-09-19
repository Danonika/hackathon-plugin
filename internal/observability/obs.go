package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type traceKey struct{}

func NewTraceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "kit-" + hex.EncodeToString(b)
}

func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

func TraceFrom(ctx context.Context) string {
	if v, ok := ctx.Value(traceKey{}).(string); ok {
		return v
	}
	return ""
}

type Metrics struct {
	counters sync.Map
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) Inc(name string) {
	m.Add(name, 1)
}

func (m *Metrics) Add(name string, delta int64) {
	v, _ := m.counters.LoadOrStore(name, new(atomic.Int64))
	v.(*atomic.Int64).Add(delta)
}

func (m *Metrics) Snapshot() map[string]int64 {
	out := map[string]int64{}
	m.counters.Range(func(k, v any) bool {
		out[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	return out
}

type Entry struct {
	Time   string         `json:"ts"`
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields,omitempty"`
}

func NewLogger(w io.Writer) *log.Logger {
	return log.New(w, "", 0)
}

func Log(l *log.Logger, level, msg string, fields map[string]any) {
	if l == nil {
		return
	}
	b, err := json.Marshal(Entry{
		Time:   time.Now().UTC().Format(time.RFC3339Nano),
		Level:  level,
		Msg:    msg,
		Fields: fields,
	})
	if err != nil {
		return
	}
	l.Println(string(b))
}

func AppendJSONL(path string, entry any) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(entry)
}

func TailJSONL(path string, limit int) ([]json.RawMessage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []json.RawMessage{}, nil
		}
		return nil, err
	}
	lines := []json.RawMessage{}
	for _, line := range splitLines(data) {
		trimmed := trimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		lines = append(lines, json.RawMessage(trimmed))
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

func splitLines(data []byte) [][]byte {
	out := [][]byte{}
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}
