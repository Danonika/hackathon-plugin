package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
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
	logger         *log.Logger

	// published is the latest immutable snapshot, read by every subscriber and
	// the ?snapshot=1 endpoint without touching the file or the lock. A single
	// poller goroutine owns the file state and publishes new values.
	published atomic.Pointer[flyLiveSnapshot]
	stop      chan struct{}
	stopOnce  sync.Once
}

func newFlyLiveHandler(root string, logger *log.Logger) *flyLiveFeed {
	f := newFlyLiveFeed(root)
	f.logger = logger
	f.publish(false)
	go f.run()
	return f
}

// newFlyLiveFeed builds the feed without starting the poller, so callers can
// configure intervals before `run` reads them.
func newFlyLiveFeed(root string) *flyLiveFeed {
	return &flyLiveFeed{
		path:         filepath.Join(root, "logs", "fly-neuron-events.jsonl"),
		pollInterval: 250 * time.Millisecond,
		heartbeat:    15 * time.Second,
		stop:         make(chan struct{}),
	}
}

// run is the single owner of the file state: it refreshes on every poll and
// publishes an immutable snapshot, so readers never perform disk I/O or block
// one another behind the mutex.
func (f *flyLiveFeed) run() {
	ticker := time.NewTicker(f.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-ticker.C:
			f.publish(false)
		}
	}
}

// close stops the poller. Safe to call more than once.
func (f *flyLiveFeed) close() {
	f.stopOnce.Do(func() { close(f.stop) })
}

// publish refreshes the file state under the lock and stores the resulting
// immutable snapshot for lock-free readers.
func (f *flyLiveFeed) publish(initial bool) {
	f.mu.Lock()
	f.refresh()
	snapshot := &flyLiveSnapshot{
		SchemaVersion:  1,
		Revision:       strconv.FormatUint(f.revision, 10),
		SentAt:         time.Now().UTC().Format(time.RFC3339Nano),
		Initial:        initial,
		FeedAvailable:  f.available,
		Recommendation: append(json.RawMessage(nil), f.recommendation...),
		Feedback:       append(json.RawMessage(nil), f.feedback...),
	}
	f.mu.Unlock()
	f.published.Store(snapshot)
}

// snapshot returns the latest published snapshot, or a synthesized empty one if
// the poller has not run yet. It never blocks on the file. The SSE and REST
// paths call this; only the poller goroutine performs disk I/O.
func (f *flyLiveFeed) snapshot(initial bool) flyLiveSnapshot {
	if snap := f.published.Load(); snap != nil {
		out := *snap
		out.Initial = initial
		return out
	}
	return flyLiveSnapshot{SchemaVersion: 1, Revision: "0", Initial: initial, SentAt: time.Now().UTC().Format(time.RFC3339Nano)}
}

// refreshSnapshot refreshes the file state and returns the fresh snapshot. It is
// used by tests and direct callers; the serving paths use snapshot().
func (f *flyLiveFeed) refreshSnapshot(initial bool) flyLiveSnapshot {
	f.publish(initial)
	return f.snapshot(initial)
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

// refresh is called under mu by the poller. Unchanged polls only stat the file.
// Appends are consumed once and published to all subscribers, including
// split/unfinished lines. File errors are best-effort: the feed is marked
// unavailable and logged once per transition so a broken feed is diagnosable
// rather than indistinguishable from a deliberate outage.
func (f *flyLiveFeed) refresh() {
	info, err := os.Stat(f.path)
	if err != nil {
		f.markUnavailable(err)
		return
	}
	if f.info != nil && os.SameFile(f.info, info) && info.Size() == f.offset && info.ModTime().Equal(f.info.ModTime()) {
		f.setAvailable(true)
		return
	}
	file, err := os.Open(f.path)
	if err != nil {
		f.markUnavailable(err)
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
		var preceding [1]byte
		if _, err := file.ReadAt(preceding[:], f.offset-1); err != nil {
			f.markUnavailable(err)
			return
		}
		f.skipPartial = preceding[0] != '\n'
	}
	data := make([]byte, min(flyLiveReadLimit, max(0, info.Size()-f.offset)))
	n, err := file.ReadAt(data, f.offset)
	if err != nil && err != io.EOF {
		f.markUnavailable(err)
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

// markUnavailable records a file error and logs it once per unavailable
// transition so a transient or broken feed leaves an operator-visible trace.
func (f *flyLiveFeed) markUnavailable(err error) {
	if f.logger != nil && f.available {
		f.logger.Printf("fly-live: feed unavailable: %v (path %s)", err, f.path)
	}
	f.setAvailable(false)
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
