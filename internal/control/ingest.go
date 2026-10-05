package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxIngestBytes bounds one ingest cycle's read so an unbounded backlog or a
// rotated file cannot exhaust memory; the cursor resumes from the last complete
// line on the next cycle.
const maxIngestBytes = 64 << 20

type IngestResult struct {
	Sessions int      `json:"sessions"`
	Events   int      `json:"events"`
	Files    int      `json:"files"`
	Risk     int      `json:"risk_updated"`
	Rotated  []string `json:"rotated,omitempty"`
	Skipped  int      `json:"skipped_malformed,omitempty"`
}

// Ingest replays the append-only JSONL streams into SQLite using per-file byte
// cursors. The cursor is persisted after each record is applied, so ingestion is
// at-least-once: a crash between applying a record and persisting the cursor
// replays that record on the next run. The events table has no dedup key, so
// such a replay can produce a duplicate row.
func (s *Store) Ingest(ctx context.Context, root string) (IngestResult, error) {
	res := IngestResult{}
	jsonl := []struct {
		name   string
		path   string
		handle func(ctx context.Context, rec map[string]any) error
	}{
		{"sessions", filepath.Join(root, "logs", "sessions.jsonl"), s.handleSessionRecord},
		{"session-events", filepath.Join(root, "logs", "session-events.jsonl"), s.handleToolRecord},
		{"mcp-calls", filepath.Join(root, "logs", "mcp-calls.jsonl"), s.handleMCPCallRecord},
		{"fly-feedback", filepath.Join(root, "logs", "fly-feedback.jsonl"), s.handleFlyFeedbackRecord},
	}
	for _, source := range jsonl {
		count, skipped, rotated, err := s.ingestJSONL(ctx, source.name, source.path, source.handle)
		if err != nil {
			return res, err
		}
		if rotated {
			res.Rotated = append(res.Rotated, source.name)
		}
		if count > 0 {
			res.Files++
			res.Events += count
		}
		if skipped > 0 {
			log.Printf("control: skipped %d malformed line(s) in %s", skipped, source.name)
		}
		res.Skipped += skipped
	}

	decisionFiles, decisionEvents, err := s.ingestDecisions(ctx, root)
	if err != nil {
		return res, err
	}
	res.Files += decisionFiles
	res.Events += decisionEvents

	updated, err := s.RecomputeRisk(ctx)
	if err != nil {
		return res, err
	}
	res.Risk = updated

	sessions, _, err := s.Counts(ctx)
	if err != nil {
		return res, err
	}
	res.Sessions = sessions
	return res, nil
}

func (s *Store) ingestJSONL(ctx context.Context, name, path string, handle func(context.Context, map[string]any) error) (int, int, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, 0, false, err
	}
	offset, err := s.Cursor(ctx, name)
	if err != nil {
		return 0, 0, false, err
	}
	rotated := false
	if offset > info.Size() {
		offset = 0
		rotated = true
	}
	// A recreated file that regrows past the old offset is indistinguishable
	// from append-only growth by size alone, so also compare a head fingerprint.
	fingerprint, hasFP, ferr := fileFingerprint(f, info.Size())
	if ferr != nil {
		return 0, 0, false, ferr
	}
	if !rotated && hasFP {
		stored, set, cerr := s.CursorSet(ctx, name+":head")
		if cerr != nil {
			return 0, 0, false, cerr
		}
		if set && stored != fingerprint {
			offset = 0
			rotated = true
		}
	}
	if rotated {
		// Reset the persisted offset so a truncated or recreated file is
		// re-read from the beginning instead of seeking to a stale offset and
		// silently skipping the head of the new file.
		if err := s.SetCursor(ctx, name, 0); err != nil {
			return 0, 0, rotated, err
		}
		// Drop the stale head fingerprint too: the new file may still be
		// shorter than the fingerprint window, and a leftover value would be
		// mistaken for a second rotation once it grows.
		if err := s.DeleteCursor(ctx, name+":head"); err != nil {
			return 0, 0, rotated, err
		}
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, 0, false, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxIngestBytes))
	if err != nil {
		return 0, 0, false, err
	}
	if len(data) == 0 {
		if hasFP {
			if err := s.SetCursor(ctx, name+":head", fingerprint); err != nil {
				return 0, 0, rotated, err
			}
		}
		return 0, 0, rotated, nil
	}
	count := 0
	skipped := 0
	consumed := 0
	lastGood := 0
	flush := func(upTo int) error {
		if upTo <= 0 {
			return nil
		}
		return s.SetCursor(ctx, name, offset+int64(upTo))
	}
	for consumed < len(data) {
		nl := bytes.IndexByte(data[consumed:], '\n')
		if nl < 0 {
			// No newline in the window. If the window is full, the line exceeds
			// maxIngestBytes and can never be parsed: advance lastGood past the
			// whole window so the post-loop flush persists the skip and the
			// stream cannot wedge. Otherwise it is a trailing partial line from
			// a concurrent writer, so retry next cycle.
			if len(data) >= maxIngestBytes {
				consumed = len(data)
				lastGood = consumed
				skipped++
			}
			break
		}
		line := data[consumed : consumed+nl]
		consumed += nl + 1
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			// Skip malformed lines: one bad record must not stall the stream.
			// The cursor still advances past it, but the skip is counted.
			skipped++
			lastGood = consumed
			continue
		}
		if err := handle(ctx, record); err != nil {
			// A structurally malformed record (e.g. an unparseable ts) is
			// skippable like malformed JSON: advance past it so one bad line
			// cannot wedge the stream forever. Genuine failures (DB errors)
			// still abort.
			if errors.Is(err, errMalformedRecord) {
				skipped++
				lastGood = consumed
				continue
			}
			if cerr := flush(lastGood); cerr != nil {
				return count, skipped, rotated, fmt.Errorf("%w (also failed to persist cursor: %v)", err, cerr)
			}
			return count, skipped, rotated, err
		}
		count++
		lastGood = consumed
		// Persist the cursor after every applied record. This is
		// at-least-once, not exactly-once: a crash between handle() and this
		// write replays the record on the next run (see Ingest's doc comment).
		if cerr := flush(lastGood); cerr != nil {
			return count, skipped, rotated, cerr
		}
	}
	if err := flush(lastGood); err != nil {
		return count, skipped, rotated, err
	}
	if hasFP {
		if err := s.SetCursor(ctx, name+":head", fingerprint); err != nil {
			return count, skipped, rotated, err
		}
	}
	return count, skipped, rotated, nil
}

// fileFingerprint hashes a fixed first-byte window so a truncate-and-regrow
// rotation is distinguishable from append-only growth. Files smaller than the
// window are skipped: their head would change as they grow.
func fileFingerprint(f *os.File, size int64) (int64, bool, error) {
	const headBytes = 256
	if size < headBytes {
		return 0, false, nil
	}
	head := make([]byte, headBytes)
	if _, err := f.ReadAt(head, 0); err != nil {
		return 0, false, err
	}
	h := fnv.New64a()
	_, _ = h.Write(head)
	return int64(h.Sum64()), true, nil
}

func (s *Store) handleSessionRecord(ctx context.Context, rec map[string]any) error {
	id := stringField(rec, "session_id")
	if id == "" {
		return nil
	}
	ts, err := recordTime(rec)
	if err != nil {
		return err
	}
	switch stringField(rec, "type") {
	case "session_end":
		return s.EndSession(ctx, id, ts)
	default:
		return s.StartSession(ctx, Session{
			ID:        id,
			Root:      stringField(rec, "root"),
			Kind:      "agent",
			CreatedAt: ts,
		})
	}
}

func (s *Store) handleToolRecord(ctx context.Context, rec map[string]any) error {
	ts, err := recordTime(rec)
	if err != nil {
		return err
	}
	return s.AppendEvent(ctx, Event{
		SessionID: stringField(rec, "session_id"),
		TS:        ts,
		Type:      "tool_use",
		Tool:      stringField(rec, "tool"),
	})
}

func (s *Store) handleMCPCallRecord(ctx context.Context, rec map[string]any) error {
	ts, err := recordTime(rec)
	if err != nil {
		return err
	}
	return s.AppendEvent(ctx, Event{
		TS:         ts,
		Type:       "mcp_call",
		Tool:       stringField(rec, "tool"),
		Status:     stringField(rec, "status"),
		DurationMS: floatField(rec, "duration_ms"),
		Payload:    rec,
	})
}

func (s *Store) handleFlyFeedbackRecord(ctx context.Context, rec map[string]any) error {
	ts, err := recordTime(rec)
	if err != nil {
		return err
	}
	return s.AppendEvent(ctx, Event{
		TS:      ts,
		Type:    "fly_feedback",
		Payload: rec,
	})
}

// ingestDecisions treats every eval decision file as an immutable event
// source keyed by a content hash, so a touch or in-place rewrite does not
// append a duplicate event.
func (s *Store) ingestDecisions(ctx context.Context, root string) (int, int, error) {
	base := filepath.Join(root, "evals", "decisions")
	entries := []string{}
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		entries = append(entries, path)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, 0, err
	}
	sort.Strings(entries)
	files := 0
	events := 0
	for _, path := range entries {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		cursor := "decision:" + filepath.ToSlash(rel)
		data, readErr := readFileBounded(path, maxIngestBytes)
		if readErr != nil {
			// One unreadable or oversized file must not block the rest of
			// ingestion; surface it and retry on the next cycle.
			log.Printf("control: skipping decision file %s: %v", rel, readErr)
			continue
		}
		hash := int64(contentHash(data))
		hashCursor := cursor + ":hash"
		prevHash, cursorErr := s.Cursor(ctx, hashCursor)
		if cursorErr != nil {
			return files, events, cursorErr
		}
		if prevHash != 0 && prevHash == hash {
			// Already ingested this exact content; a rewrite with identical
			// bytes must not create a second event.
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			continue // partially written; retry next cycle
		}
		ts, tsErr := recordTime(payload)
		if tsErr != nil {
			continue // malformed timestamp; retry next cycle
		}
		if err := s.AppendEvent(ctx, Event{
			TS:      ts,
			Type:    "eval_decision",
			Payload: payload,
		}); err != nil {
			return files, events, err
		}
		if err := s.SetCursor(ctx, cursor, int64(len(data))); err != nil {
			return files, events, err
		}
		if err := s.SetCursor(ctx, hashCursor, hash); err != nil {
			return files, events, err
		}
		files++
		events++
	}
	return files, events, nil
}

func contentHash(data []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(data)
	return h.Sum64()
}

// readFileBounded reads a file but errors if it exceeds max bytes, instead of
// silently truncating an oversized decision file (which would then never parse
// and never be surfaced).
func readFileBounded(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file exceeds the %d-byte ingestion limit", max)
	}
	return data, nil
}

// errMalformedRecord marks a record that parsed as JSON but is structurally
// unusable (e.g. a present-but-unparseable timestamp). Such records are skipped
// rather than aborting the whole ingest cycle.
var errMalformedRecord = errors.New("malformed record")

// recordTime extracts a timestamp from a JSONL record. An absent ts is allowed
// (callers substitute the ingest time); a present-but-unparseable ts is an
// error so a corrupt record is surfaced instead of being silently stamped.
func recordTime(rec map[string]any) (time.Time, error) {
	raw := stringField(rec, "ts")
	if raw == "" {
		return time.Time{}, nil
	}
	ts, ok := parseTimeOK(raw)
	if !ok {
		return time.Time{}, fmt.Errorf("%w: malformed ts %q", errMalformedRecord, raw)
	}
	return ts, nil
}

func stringField(rec map[string]any, key string) string {
	if value, ok := rec[key].(string); ok {
		return value
	}
	return ""
}

func floatField(rec map[string]any, key string) float64 {
	if value, ok := rec[key].(float64); ok {
		return value
	}
	return 0
}
