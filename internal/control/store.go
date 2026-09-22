// Package control implements the Hackathon Agent Kit control plane: a SQLite store fed
// from the JSONL event streams, deterministic session risk scoring and
// handoff generation. JSONL files stay the cross-process source of truth; the
// database is owned by the daemon or the CLI.
package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  root TEXT,
  kind TEXT,
  task TEXT,
  status TEXT,
  stage TEXT,
  plan_slug TEXT,
  worktree TEXT,
  risk_score REAL NOT NULL DEFAULT 0,
  risk_reasons TEXT,
  created_at TEXT,
  updated_at TEXT,
  ended_at TEXT
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT,
  ts TEXT,
  type TEXT,
  tool TEXT,
  status TEXT,
  duration_ms REAL,
  payload TEXT
);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id, id);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
CREATE TABLE IF NOT EXISTS outputs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT,
  ts TEXT,
  kind TEXT,
  ref TEXT,
  summary TEXT
);
CREATE TABLE IF NOT EXISTS cursors (
  name TEXT PRIMARY KEY,
  offset INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT
);
`

type Session struct {
	ID          string     `json:"id"`
	Root        string     `json:"root,omitempty"`
	Kind        string     `json:"kind,omitempty"`
	Task        string     `json:"task,omitempty"`
	Status      string     `json:"status"`
	Stage       string     `json:"stage,omitempty"`
	PlanSlug    string     `json:"plan_slug,omitempty"`
	Worktree    string     `json:"worktree,omitempty"`
	RiskScore   float64    `json:"risk_score"`
	RiskReasons []string   `json:"risk_reasons,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
}

type Event struct {
	ID         int64          `json:"id"`
	SessionID  string         `json:"session_id,omitempty"`
	TS         time.Time      `json:"ts"`
	Type       string         `json:"type"`
	Tool       string         `json:"tool,omitempty"`
	Status     string         `json:"status,omitempty"`
	DurationMS float64        `json:"duration_ms,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type Output struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id,omitempty"`
	TS        time.Time `json:"ts"`
	Kind      string    `json:"kind"`
	Ref       string    `json:"ref,omitempty"`
	Summary   string    `json:"summary,omitempty"`
}

type Store struct {
	db *sql.DB
}

func dsn(path string, readOnly bool) string {
	base := "file:" + filepath.ToSlash(path)
	if readOnly {
		return base + "?mode=ro&_pragma=busy_timeout(2000)"
	}
	return base + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func OpenReadOnly(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var probe int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&probe); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// timeLayout is a fixed-width UTC timestamp: unlike RFC3339Nano it never drops
// trailing zeros, so stored TEXT values sort lexicographically in time order
// (all range queries compare strings).
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, timeLayout, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func (s *Store) UpsertSession(ctx context.Context, sess Session) error {
	reasons, err := json.Marshal(sess.RiskReasons)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	sess.UpdatedAt = now
	_, err = s.db.ExecContext(ctx, `
INSERT INTO sessions (id, root, kind, task, status, stage, plan_slug, worktree, risk_score, risk_reasons, created_at, updated_at, ended_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  root=excluded.root,
  kind=excluded.kind,
  task=excluded.task,
  status=excluded.status,
  stage=excluded.stage,
  plan_slug=excluded.plan_slug,
  worktree=excluded.worktree,
  updated_at=excluded.updated_at`,
		sess.ID, sess.Root, sess.Kind, sess.Task, sess.Status, sess.Stage, sess.PlanSlug, sess.Worktree,
		sess.RiskScore, string(reasons), formatTime(sess.CreatedAt), formatTime(sess.UpdatedAt), nullableTime(sess.EndedAt))
	return err
}

// StartSession creates the session when it is missing and otherwise refreshes
// last-seen without resurrecting an already ended session, so replayed
// session_start/heartbeat records cannot flip status back to active.
func (s *Store) StartSession(ctx context.Context, sess Session) error {
	now := time.Now().UTC()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET root=COALESCE(NULLIF(?,''), root), updated_at=? WHERE id=? AND status != 'ended'`,
		sess.Root, formatTime(now), sess.ID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		return nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id=?`, sess.ID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	sess.Status = "active"
	if sess.Kind == "" {
		sess.Kind = "agent"
	}
	return s.UpsertSession(ctx, sess)
}

func (s *Store) EndSession(ctx context.Context, id string, ts time.Time) error {
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET status='ended', ended_at=?, updated_at=? WHERE id=?`,
		formatTime(ts), formatTime(ts), id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return s.UpsertSession(ctx, Session{ID: id, Status: "ended", CreatedAt: ts, EndedAt: &ts})
	}
	return nil
}

func (s *Store) UpdateRisk(ctx context.Context, id string, score float64, reasons []string) error {
	data, err := json.Marshal(reasons)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sessions SET risk_score=?, risk_reasons=? WHERE id=?`, score, string(data), id)
	return err
}

func (s *Store) Session(ctx context.Context, id string) (Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, root, kind, task, status, stage, plan_slug, worktree, risk_score, risk_reasons, created_at, updated_at, ended_at FROM sessions WHERE id=?`, id)
	return scanSession(row)
}

func (s *Store) Sessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, root, kind, task, status, stage, plan_slug, worktree, risk_score, risk_reasons, created_at, updated_at, ended_at FROM sessions ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(row rowScanner) (Session, error) {
	var sess Session
	var reasons, created, updated, ended sql.NullString
	if err := row.Scan(&sess.ID, &sess.Root, &sess.Kind, &sess.Task, &sess.Status, &sess.Stage,
		&sess.PlanSlug, &sess.Worktree, &sess.RiskScore, &reasons, &created, &updated, &ended); err != nil {
		return Session{}, err
	}
	if reasons.Valid && reasons.String != "" {
		_ = json.Unmarshal([]byte(reasons.String), &sess.RiskReasons)
	}
	sess.CreatedAt = parseTime(created.String)
	sess.UpdatedAt = parseTime(updated.String)
	if ended.Valid && ended.String != "" {
		t := parseTime(ended.String)
		sess.EndedAt = &t
	}
	return sess, nil
}

func (s *Store) AppendEvent(ctx context.Context, ev Event) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO events (session_id, ts, type, tool, status, duration_ms, payload) VALUES (?,?,?,?,?,?,?)`,
		ev.SessionID, formatTime(ev.TS), ev.Type, ev.Tool, ev.Status, ev.DurationMS, string(payload))
	return err
}

func (s *Store) Events(ctx context.Context, sessionID string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 2000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, ts, type, tool, status, duration_ms, payload FROM events WHERE session_id=? ORDER BY id DESC LIMIT ?`,
		sessionID, limit)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// EventsBetween returns global events (no session id) inside a time window.
func (s *Store) EventsBetween(ctx context.Context, from, to time.Time, limit int) ([]Event, error) {
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, ts, type, tool, status, duration_ms, payload FROM events WHERE session_id='' AND ts>=? AND ts<=? ORDER BY id DESC LIMIT ?`,
		formatTime(from), formatTime(to), limit)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (s *Store) latestGlobalTS(ctx context.Context) (time.Time, error) {
	var value string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(ts),'') FROM events WHERE session_id=''`).Scan(&value); err != nil {
		return time.Time{}, err
	}
	return parseTime(value), nil
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var ev Event
		var ts, payload string
		if err := rows.Scan(&ev.ID, &ev.SessionID, &ts, &ev.Type, &ev.Tool, &ev.Status, &ev.DurationMS, &payload); err != nil {
			return nil, err
		}
		ev.TS = parseTime(ts)
		if payload != "" && payload != "null" {
			_ = json.Unmarshal([]byte(payload), &ev.Payload)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Return ascending by id for display.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (s *Store) Counts(ctx context.Context) (sessions int, events int, err error) {
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		return 0, 0, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&events)
	return sessions, events, err
}

func (s *Store) Cursor(ctx context.Context, name string) (int64, error) {
	var offset int64
	err := s.db.QueryRowContext(ctx, `SELECT offset FROM cursors WHERE name=?`, name).Scan(&offset)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return offset, err
}

func (s *Store) SetCursor(ctx context.Context, name string, offset int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cursors (name, offset, updated_at) VALUES (?,?,?)
		 ON CONFLICT(name) DO UPDATE SET offset=excluded.offset, updated_at=excluded.updated_at`,
		name, offset, formatTime(time.Now().UTC()))
	return err
}

func (s *Store) AppendOutput(ctx context.Context, out Output) error {
	if out.TS.IsZero() {
		out.TS = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO outputs (session_id, ts, kind, ref, summary) VALUES (?,?,?,?,?)`,
		out.SessionID, formatTime(out.TS), out.Kind, out.Ref, out.Summary)
	return err
}

func (s *Store) Outputs(ctx context.Context, sessionID string, limit int) ([]Output, error) {
	if limit <= 0 || limit > 500 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, ts, kind, ref, summary FROM outputs WHERE session_id=? ORDER BY id DESC LIMIT ?`,
		sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Output{}
	for rows.Next() {
		var item Output
		var ts string
		if err := rows.Scan(&item.ID, &item.SessionID, &ts, &item.Kind, &item.Ref, &item.Summary); err != nil {
			return nil, err
		}
		item.TS = parseTime(ts)
		out = append(out, item)
	}
	return out, rows.Err()
}

func nullableTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return formatTime(*t)
}

func DefaultPath(root string) string {
	return filepath.Join(root, "data", "control", "control.db")
}
