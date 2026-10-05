package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestFlyFeed(t *testing.T) *flyLiveFeed {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Build without the poller, configure, then start it explicitly.
	feed := newFlyLiveFeed(root)
	feed.pollInterval = time.Millisecond
	feed.heartbeat = time.Hour
	feed.close() // keep the poller stopped; tests drive refresh deterministically
	return feed
}

func appendFlyLog(t *testing.T, feed *flyLiveFeed, text string) {
	t.Helper()
	f, err := os.OpenFile(feed.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString(text)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append: %v, close: %v", writeErr, closeErr)
	}
}

func TestFlyLiveTailPreservesHistoryAndWaitsForWholeEvents(t *testing.T) {
	feed := newTestFlyFeed(t)
	if first := feed.refreshSnapshot(true); first.FeedAvailable || first.Recommendation != nil {
		t.Fatalf("missing log should be waiting, not fake activity: %+v", first)
	}
	appendFlyLog(t, feed, "{\"type\":\"recommend\",\"ts\":\"2026-09-21T10:00:00Z\",\"active_kc\":[{\"index\":4000,\"activation\":0.04}],\"kc_total\":4133}\n")
	first := feed.refreshSnapshot(true)
	if !first.Initial || !first.FeedAvailable || first.Recommendation == nil {
		t.Fatalf("missing initial history: %+v", first)
	}
	var event map[string]any
	_ = json.Unmarshal(first.Recommendation, &event)
	if event["activation_complete"] != false || event["activation_available"] != true || event["event_id"] == "" {
		t.Fatalf("legacy truncated data was not labelled correctly: %v", event)
	}
	if unchanged := feed.refreshSnapshot(false); unchanged.Revision != first.Revision || unchanged.Initial {
		t.Fatalf("unchanged poll replayed an event: %+v", unchanged)
	}
	appendFlyLog(t, feed, "{\"type\":\"feedback\",\"ts\":\"2026-09-21T10:00:01Z\"")
	if partial := feed.refreshSnapshot(false); partial.Revision != first.Revision || partial.Feedback != nil {
		t.Fatalf("unfinished JSON was emitted: %+v", partial)
	}
	appendFlyLog(t, feed, ",\"reward\":true}\n")
	updated := feed.refreshSnapshot(false)
	if updated.Initial || updated.Revision == first.Revision || updated.Feedback == nil || !bytes.Equal(updated.Recommendation, first.Recommendation) {
		t.Fatalf("feedback update lost coordinated recommendation: %+v", updated)
	}
	// A new consumer restores the same feedback ID, not a new reward.
	if reconnected := feed.refreshSnapshot(true); !reconnected.Initial || !bytes.Equal(reconnected.Feedback, updated.Feedback) || reconnected.Revision != updated.Revision {
		t.Fatalf("reconnection changed event identity: %+v", reconnected)
	}
}

func TestFlyLiveBoundedTailAndRotation(t *testing.T) {
	feed := newTestFlyFeed(t)
	prefix := strings.Repeat(" ", int(flyLiveReadLimit)+100)
	appendFlyLog(t, feed, prefix+"\n{\"type\":\"feedback\",\"ts\":\"old\",\"reward\":true}\n")
	first := feed.refreshSnapshot(true)
	if first.Feedback == nil || int64(len(feed.pending)) > flyLiveReadLimit {
		t.Fatalf("bounded tail did not find latest complete record: %+v", first)
	}
	if err := os.WriteFile(feed.path, []byte("{\"type\":\"feedback\",\"ts\":\"new\",\"reward\":false}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rotated := feed.refreshSnapshot(false)
	if rotated.Revision == first.Revision || !bytes.Contains(rotated.Feedback, []byte(`"ts":"new"`)) {
		t.Fatalf("truncated/rotated log not followed: %+v", rotated)
	}
}

// The HTTP handler is exercised without binding a port. Flush notifications
// let the test append real log data between stream frames deterministically.
type flyStreamRecorder struct {
	header http.Header
	frames chan string
	buffer bytes.Buffer
}

func (w *flyStreamRecorder) Header() http.Header         { return w.header }
func (w *flyStreamRecorder) WriteHeader(int)             {}
func (w *flyStreamRecorder) Write(p []byte) (int, error) { return w.buffer.Write(p) }
func (w *flyStreamRecorder) Flush() {
	w.frames <- w.buffer.String()
	w.buffer.Reset()
}

func readFlyFrame(t *testing.T, frames <-chan string) flyLiveSnapshot {
	t.Helper()
	select {
	case frame := <-frames:
		for _, line := range strings.Split(frame, "\n") {
			if strings.HasPrefix(line, "data: ") {
				var snapshot flyLiveSnapshot
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &snapshot); err != nil {
					t.Fatal(err)
				}
				return snapshot
			}
		}
		t.Fatalf("not a snapshot event: %q", frame)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out awaiting SSE frame")
	}
	return flyLiveSnapshot{}
}

func TestFlyLiveSSEOnlyChangesAndStopsOnCancel(t *testing.T) {
	// The poller must be running: this test asserts an appended event reaches a
	// connected subscriber.
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	feed := newFlyLiveFeed(root)
	feed.pollInterval = time.Millisecond
	feed.heartbeat = time.Hour
	go feed.run()
	t.Cleanup(feed.close)
	appendFlyLog(t, feed, "{\"type\":\"feedback\",\"ts\":\"initial\",\"reward\":true}\n")
	// Wait until the poller has picked up the initial event.
	deadline := time.Now().Add(2 * time.Second)
	for feed.snapshot(true).Feedback == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := &flyStreamRecorder{header: make(http.Header), frames: make(chan string, 8)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		feed.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/fly-live", nil).WithContext(ctx))
	}()
	first := readFlyFrame(t, recorder.frames)
	if !first.Initial || first.Feedback == nil {
		t.Fatalf("SSE failed to mark old feedback as history: %+v", first)
	}
	select {
	case frame := <-recorder.frames:
		t.Fatalf("unchanged state emitted again: %q", frame)
	case <-time.After(15 * time.Millisecond):
	}
	appendFlyLog(t, feed, "{\"type\":\"recommend\",\"ts\":\"live\",\"activation_available\":true,\"activation_complete\":true,\"active_kc\":[]}\n")
	second := readFlyFrame(t, recorder.frames)
	if second.Initial || second.Revision == first.Revision || second.Recommendation == nil || !bytes.Equal(first.Feedback, second.Feedback) {
		t.Fatalf("new recommendation replayed or displaced old feedback: %+v", second)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not stop on cancellation")
	}
}

func TestFlyLiveSnapshotAndReadOnlyMethods(t *testing.T) {
	feed := newTestFlyFeed(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rr := httptest.NewRecorder()
		feed.ServeHTTP(rr, httptest.NewRequest(method, "/fly-live", nil))
		if rr.Code != http.StatusMethodNotAllowed || rr.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("%s unexpectedly allowed: %d", method, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	feed.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/fly-live?snapshot=1", nil))
	var snapshot flyLiveSnapshot
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &snapshot) != nil || snapshot.SchemaVersion != 1 || !snapshot.Initial {
		t.Fatal(fmt.Sprintf("invalid snapshot fallback: %d %s", rr.Code, rr.Body.String()))
	}
}
