package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// The dashboard observes this append-only stream. It never runs the router or
// produces training feedback. Only the most recent recommendation and feedback
// are retained; startup and unusually large appends read at most this tail.
const flyLiveReadLimit int64 = 4 << 20

type flyLiveSnapshot struct {
	SchemaVersion  int             `json:"schema_version"`
	Revision       string          `json:"revision"`
	SentAt         string          `json:"sent_at"`
	Initial        bool            `json:"initial"`
	FeedAvailable  bool            `json:"feed_available"`
	Recommendation json.RawMessage `json:"recommendation"`
	Feedback       json.RawMessage `json:"feedback"`
}

type flyLiveFeed struct {
	mu             sync.Mutex
	path           string
	info           os.FileInfo
	offset         int64
	pending        []byte
	skipPartial    bool
	available      bool
	revision       uint64
	recommendation json.RawMessage
	feedback       json.RawMessage
	pollInterval   time.Duration
	heartbeat      time.Duration
}

func newFlyLiveHandler(root string) http.Handler {
	return &flyLiveFeed{
		path:         filepath.Join(root, "logs", "fly-neuron-events.jsonl"),
		pollInterval: 250 * time.Millisecond,
		heartbeat:    15 * time.Second,
	}
}

func (f *flyLiveFeed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	w.Header().Set("Cache-Control", "no-cache, no-store")
	if r.URL.Query().Get("snapshot") == "1" {
		writeJSON(w, http.StatusOK, f.snapshot(true))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	write := func(snapshot flyLiveSnapshot) bool {
		body, err := json.Marshal(snapshot)
		if err != nil {
			return false
		}
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err = fmt.Fprintf(w, "id: %s\nevent: snapshot\ndata: %s\n\n", snapshot.Revision, body)
		flusher.Flush()
		return err == nil
	}
	// The first frame always restores history. Consumers must not interpret an
	// existing feedback entry as a newly delivered reward on reconnect.
	first := f.snapshot(true)
	if !write(first) {
		return
	}
	lastRevision := first.Revision
	ticker := time.NewTicker(f.pollInterval)
	defer ticker.Stop()
	heartbeat := time.NewTicker(f.heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			snapshot := f.snapshot(false)
			if snapshot.Revision == lastRevision {
				continue
			}
			if !write(snapshot) {
				return
			}
			lastRevision = snapshot.Revision
		case now := <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := fmt.Fprintf(w, ": heartbeat %s\n\n", now.UTC().Format(time.RFC3339Nano)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (f *flyLiveFeed) snapshot(initial bool) flyLiveSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh()
	return flyLiveSnapshot{
		SchemaVersion: 1,
		Revision:      strconv.FormatUint(f.revision, 10),
		SentAt:        time.Now().UTC().Format(time.RFC3339Nano),
		Initial:       initial, FeedAvailable: f.available,
		Recommendation: f.recommendation, Feedback: f.feedback,
	}
}

// refresh is called under mu. Unchanged polls only stat the file. Appends are
// consumed once, shared by all subscribers, including split/unfinished lines.
func (f *flyLiveFeed) refresh() {
	info, err := os.Stat(f.path)
	if err != nil {
		f.setAvailable(false)
		return
	}
	if f.info != nil && os.SameFile(f.info, info) && info.Size() == f.offset && info.ModTime().Equal(f.info.ModTime()) {
		f.setAvailable(true)
		return
	}
	file, err := os.Open(f.path)
	if err != nil {
		f.setAvailable(false)
		return
	}
	defer file.Close()
	if f.info == nil || !os.SameFile(f.info, info) || info.Size() < f.offset || (info.Size() == f.offset && !info.ModTime().Equal(f.info.ModTime())) {
		f.offset = 0
		f.pending = nil
		f.skipPartial = false
	}
	if info.Size()-f.offset > flyLiveReadLimit {
		f.offset = info.Size() - flyLiveReadLimit
		f.pending = nil
		f.skipPartial = f.offset > 0
	}
	data := make([]byte, min(flyLiveReadLimit, max(0, info.Size()-f.offset)))
	n, err := file.ReadAt(data, f.offset)
	if err != nil && err != io.EOF {
		f.setAvailable(false)
		return
	}
	f.offset += int64(n)
	f.info = info
	f.setAvailable(true)
	data = append(f.pending, data[:n]...)
	f.pending = nil
	for {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			break
		}
		if f.skipPartial {
			f.skipPartial = false
		} else {
			f.consume(bytes.TrimSpace(data[:end]))
		}
		data = data[end+1:]
	}
	if int64(len(data)) > flyLiveReadLimit {
		// A malformed oversized line must not grow memory indefinitely.
		f.skipPartial = true
	} else {
		f.pending = bytes.Clone(data)
	}
}

func (f *flyLiveFeed) setAvailable(available bool) {
	if f.available != available {
		f.available = available
		f.revision++
	}
}

func (f *flyLiveFeed) consume(line []byte) {
	var event map[string]json.RawMessage
	if json.Unmarshal(line, &event) != nil || event == nil {
		return
	}
	var kind string
	if json.Unmarshal(event["type"], &kind) != nil || (kind != "recommend" && kind != "feedback") {
		return
	}
	// Old logs have no explicit ID. A content digest is stable across polling,
	// restarts, and reconnects, unlike the time this HTTP request was served.
	digest := sha256.Sum256(line)
	event["event_id"], _ = json.Marshal(hex.EncodeToString(digest[:16]))
	if kind == "recommend" {
		// Legacy top-64 snapshots are partial; never silently present their absent
		// neurons as inactive. Retain the unknown total active count for old logs.
		if _, ok := event["activation_complete"]; !ok {
			event["activation_complete"] = json.RawMessage("false")
		}
		if _, ok := event["activation_available"]; !ok {
			var cells []json.RawMessage
			_ = json.Unmarshal(event["active_kc"], &cells)
			event["activation_available"], _ = json.Marshal(len(cells) > 0)
		}
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	target := &f.recommendation
	if kind == "feedback" {
		target = &f.feedback
	}
	if !bytes.Equal(*target, encoded) {
		*target = encoded
		f.revision++
	}
}
