package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
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
