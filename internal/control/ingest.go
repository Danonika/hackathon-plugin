package control

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type IngestResult struct {
	Sessions int `json:"sessions"`
	Events   int `json:"events"`
	Files    int `json:"files"`
	Risk     int `json:"risk_updated"`
}

// Ingest replays the append-only JSONL streams into SQLite using per-file byte
// cursors, so repeated runs never duplicate records.
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
		count, err := s.ingestJSONL(ctx, source.name, source.path, source.handle)
		if err != nil {
			return res, err
		}
		if count > 0 {
			res.Files++
			res.Events += count
		}
	}

	decisionFiles, err := s.ingestDecisions(ctx, root)
	if err != nil {
		return res, err
	}
	res.Files += decisionFiles

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

func (s *Store) ingestJSONL(ctx context.Context, name, path string, handle func(context.Context, map[string]any) error) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	offset, err := s.Cursor(ctx, name)
	if err != nil {
		return 0, err
	}
	if offset > int64(len(data)) {
		offset = 0
	}
	chunk := data[offset:]
	if len(chunk) == 0 {
		return 0, nil
	}
	count := 0
	consumed := 0
	lastGood := 0
	for consumed < len(chunk) {
		nl := bytes.IndexByte(chunk[consumed:], '\n')
		if nl < 0 {
			// Unterminated trailing line from a concurrent writer: retry next cycle.
			break
		}
		line := chunk[consumed : consumed+nl]
		consumed += nl + 1
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			// Malformed line: stop and retry from here next cycle.
			consumed -= nl + 1
			break
		}
		if err := handle(ctx, record); err != nil {
			// Records handled before this line were applied; persist the cursor
			// up to the last good line so the next cycle does not duplicate them.
			if lastGood > 0 {
				_ = s.SetCursor(ctx, name, offset+int64(lastGood))
			}
			return count, err
		}
		count++
		lastGood = consumed
	}
	if consumed > 0 {
		if err := s.SetCursor(ctx, name, offset+int64(consumed)); err != nil {
			return count, err
		}
	}
	return count, nil
}

func (s *Store) handleSessionRecord(ctx context.Context, rec map[string]any) error {
	id := stringField(rec, "session_id")
	if id == "" {
		return nil
	}
	ts := parseTime(stringField(rec, "ts"))
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
	return s.AppendEvent(ctx, Event{
		SessionID: stringField(rec, "session_id"),
		TS:        parseTime(stringField(rec, "ts")),
		Type:      "tool_use",
		Tool:      stringField(rec, "tool"),
	})
}

func (s *Store) handleMCPCallRecord(ctx context.Context, rec map[string]any) error {
	return s.AppendEvent(ctx, Event{
		TS:         parseTime(stringField(rec, "ts")),
		Type:       "mcp_call",
		Tool:       stringField(rec, "tool"),
		Status:     stringField(rec, "status"),
		DurationMS: floatField(rec, "duration_ms"),
		Payload:    rec,
	})
}

func (s *Store) handleFlyFeedbackRecord(ctx context.Context, rec map[string]any) error {
	return s.AppendEvent(ctx, Event{
		TS:      parseTime(stringField(rec, "ts")),
		Type:    "fly_feedback",
		Payload: rec,
	})
}

// ingestDecisions treats every eval decision file as an immutable event
// source: the cursor is the file size.
func (s *Store) ingestDecisions(ctx context.Context, root string) (int, error) {
	base := filepath.Join(root, "evals", "decisions")
	entries := []string{}
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		entries = append(entries, path)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	sort.Strings(entries)
	files := 0
	for _, path := range entries {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		cursor := "decision:" + filepath.ToSlash(rel)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		offset, cursorErr := s.Cursor(ctx, cursor)
		if cursorErr != nil {
			return files, cursorErr
		}
		if offset > int64(len(data)) {
			offset = 0
		}
		if offset == int64(len(data)) {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			continue // partially written; retry next cycle
		}
		if err := s.AppendEvent(ctx, Event{
			TS:      parseTime(stringField(payload, "ts")),
			Type:    "eval_decision",
			Payload: payload,
		}); err != nil {
			return files, err
		}
		if err := s.SetCursor(ctx, cursor, int64(len(data))); err != nil {
			return files, err
		}
		files++
	}
	return files, nil
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
