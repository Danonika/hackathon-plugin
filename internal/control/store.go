// Package control implements the Hackathon Agent Kit control plane: a SQLite store fed
// from the JSONL event streams, deterministic session risk scoring and
// handoff generation. JSONL files stay the cross-process source of truth; the
// database is owned by the daemon or the CLI.
package control

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"modernc.org/sqlite"
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
	ingestLockPath string
	memoryIngest   chan struct{}
	db             *sql.DB
	legacyTimes    bool
}

// Legacy stores used variable-width RFC3339 strings. Normalize once for
// indexed comparisons, or compute the same key at read time for a read-only DB.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("hackathonkit_time", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if text, ok := args[0].(string); ok {
			if t, valid := parseTimeOK(text); valid {
				return formatTime(t), nil
			}
		}
		return args[0], nil
	})
}

func (s *Store) timeColumn(name string) string {
	if s.legacyTimes {
		return "hackathonkit_time(" + name + ")"
	}
	return name
}

func migrateTimeKeys(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 1 {
		return nil
	}
	for _, field := range []struct{ table, column string }{{"events", "ts"}, {"outputs", "ts"}, {"sessions", "created_at"}, {"sessions", "updated_at"}, {"sessions", "ended_at"}, {"cursors", "updated_at"}} {
		query := fmt.Sprintf("UPDATE %[1]s SET %[2]s=hackathonkit_time(%[2]s) WHERE %[2]s != hackathonkit_time(%[2]s)", field.table, field.column)
		if _, err = tx.Exec(query); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("PRAGMA user_version=1"); err != nil {
		return err
	}
	return tx.Commit()
}

func dsn(path string, readOnly bool) string {
	base := "file:" + (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()
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
	if err := migrateTimeKeys(db); err != nil {
		db.Close()
		return nil, err
	}
	// SQLite reports the canonical main database filename even when opened
	// through an alias. Independent in-memory stores have no filename or peers.
	var seq int
	var name, filename string
	if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &filename); err != nil {
		db.Close()
		return nil, err
	}
	if filename == "" {
		return &Store{db: db, memoryIngest: make(chan struct{}, 1)}, nil
	}
	return &Store{db: db, ingestLockPath: filename + ".ingest.lock"}, nil
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
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, legacyTimes: version < 1}, nil
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

// parseTimeOK parses a stored/stream timestamp and reports whether the value
// was usable, so callers can distinguish a corrupt timestamp from an unset one
// instead of fabricating a time.
func parseTimeOK(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	// Prefer the fixed-width layout written by formatTime so stored values sort
	// consistently; the others are read-compat fallbacks for older records.
	for _, layout := range []string{timeLayout, time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseTime(value string) time.Time {
	t, _ := parseTimeOK(value)
	return t
}

func storedTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	t, ok := parseTimeOK(value)
	if !ok {
		return time.Time{}, fmt.Errorf("invalid stored timestamp %q", value)
	}
	return t, nil
}

func (s *Store) UpsertSession(ctx context.Context, sess Session) error {
	return s.writeSession(ctx, sess, time.Now().UTC())
}

// writeSession merges lifecycle records atomically. The event time, not the
// replay time, determines the session window used for risk and handoff.
func (s *Store) writeSession(ctx context.Context, sess Session, when time.Time) error {
	reasons, err := json.Marshal(sess.RiskReasons)
	if err != nil {
		return err
	}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = when
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO sessions (id, root, kind, task, status, stage, plan_slug, worktree, risk_score, risk_reasons, created_at, updated_at, ended_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  root=COALESCE(NULLIF(excluded.root,''), sessions.root),
  kind=COALESCE(NULLIF(excluded.kind,''), sessions.kind),
  task=COALESCE(NULLIF(excluded.task,''), sessions.task),
  status=CASE WHEN excluded.ended_at IS NOT NULL OR sessions.ended_at IS NOT NULL THEN 'ended' ELSE COALESCE(NULLIF(excluded.status,''), sessions.status) END,
  stage=COALESCE(NULLIF(excluded.stage,''), sessions.stage),
  plan_slug=COALESCE(NULLIF(excluded.plan_slug,''), sessions.plan_slug),
  worktree=COALESCE(NULLIF(excluded.worktree,''), sessions.worktree),
  risk_score=CASE WHEN excluded.risk_reasons='null' AND excluded.risk_score=0 THEN sessions.risk_score ELSE excluded.risk_score END,
  risk_reasons=CASE WHEN excluded.risk_reasons='null' AND excluded.risk_score=0 THEN sessions.risk_reasons ELSE excluded.risk_reasons END,
  created_at=CASE WHEN NULLIF(sessions.created_at,'') IS NULL THEN excluded.created_at ELSE MIN(sessions.created_at, excluded.created_at) END,
  ended_at=CASE WHEN sessions.ended_at IS NULL THEN excluded.ended_at WHEN excluded.ended_at IS NULL THEN sessions.ended_at ELSE MIN(sessions.ended_at, excluded.ended_at) END,
  updated_at=CASE WHEN sessions.ended_at IS NOT NULL AND excluded.ended_at IS NULL THEN sessions.updated_at ELSE MAX(COALESCE(sessions.updated_at,''), excluded.updated_at) END`,
		sess.ID, sess.Root, sess.Kind, sess.Task, sess.Status, sess.Stage, sess.PlanSlug, sess.Worktree,
		sess.RiskScore, string(reasons), formatTime(sess.CreatedAt), formatTime(when), nullableTime(sess.EndedAt))
	return err
}

// StartSession creates the session when it is missing and otherwise refreshes
// last-seen without resurrecting an already ended session, so replayed
// session_start/heartbeat records cannot flip status back to active.
func (s *Store) StartSession(ctx context.Context, sess Session) error {
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now().UTC()
	}
	if sess.Kind == "" {
		sess.Kind = "agent"
	}
	sess.Status = "active"
	return s.writeSession(ctx, sess, sess.CreatedAt)
}

func (s *Store) EndSession(ctx context.Context, id string, ts time.Time) error {
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return s.writeSession(ctx, Session{ID: id, Status: "ended", EndedAt: &ts}, ts)
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
	if limit <= 0 || limit > 10000 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, root, kind, task, status, stage, plan_slug, worktree, risk_score, risk_reasons, created_at, updated_at, ended_at FROM sessions ORDER BY `+s.timeColumn("updated_at")+` DESC LIMIT ?`, limit)
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
		if err := json.Unmarshal([]byte(reasons.String), &sess.RiskReasons); err != nil {
			return Session{}, fmt.Errorf("session %s has malformed risk reasons: %w", sess.ID, err)
		}
	}
	var err error
	sess.CreatedAt, err = storedTime(created.String)
	if err != nil {
		return Session{}, err
	}
	sess.UpdatedAt, err = storedTime(updated.String)
	if err != nil {
		return Session{}, err
	}
	if ended.Valid && ended.String != "" {
		t, err := storedTime(ended.String)
		if err != nil {
			return Session{}, err
		}
		sess.EndedAt = &t
	}
	return sess, nil
}

type sqlWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Store) AppendEvent(ctx context.Context, ev Event) error { return appendEvent(ctx, s.db, ev) }

func appendEvent(ctx context.Context, db sqlWriter, ev Event) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO events (session_id, ts, type, tool, status, duration_ms, payload) VALUES (?,?,?,?,?,?,?)`,
		ev.SessionID, formatTime(ev.TS), ev.Type, ev.Tool, ev.Status, ev.DurationMS, string(payload))
	return err
}

func (s *Store) Events(ctx context.Context, sessionID string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 10000 {
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
	if limit <= 0 || limit > 10000 {
		limit = 200
	}
	// Zero bounds would format as "" and break the lexicographic window.
	fromText := formatTime(from)
	if fromText == "" {
		fromText = "0000-01-01T00:00:00.000000000Z"
	}
	toText := formatTime(to)
	if toText == "" {
		toText = "9999-12-31T23:59:59.999999999Z"
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, ts, type, tool, status, duration_ms, payload FROM events WHERE session_id='' AND `+s.timeColumn("ts")+`>=? AND `+s.timeColumn("ts")+`<=? ORDER BY id DESC LIMIT ?`,
		fromText, toText, limit)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (s *Store) latestGlobalTS(ctx context.Context) (time.Time, error) {
	var value string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(`+s.timeColumn("ts")+`),'') FROM events WHERE session_id=''`).Scan(&value); err != nil {
		return time.Time{}, err
	}
	return storedTime(value)
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
		var err error
		ev.TS, err = storedTime(ts)
		if err != nil {
			return nil, err
		}
		if payload != "" && payload != "null" {
			if err := json.Unmarshal([]byte(payload), &ev.Payload); err != nil {
				return nil, fmt.Errorf("event has a malformed payload: %w", err)
			}
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

// CursorUpdatedAt returns when the cursor was last written, used to detect
// in-place rewrites of immutable-looking files.
func (s *Store) CursorUpdatedAt(ctx context.Context, name string) (time.Time, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(updated_at,'') FROM cursors WHERE name=?`, name).Scan(&value)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return storedTime(value)
}

func (s *Store) Cursor(ctx context.Context, name string) (int64, error) {
	var offset int64
	err := s.db.QueryRowContext(ctx, `SELECT offset FROM cursors WHERE name=?`, name).Scan(&offset)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return offset, err
}

// CursorSet reports whether a cursor exists and its stored value, so callers
// can distinguish an unset cursor from a legitimately stored zero.
func (s *Store) CursorSet(ctx context.Context, name string) (int64, bool, error) {
	var offset int64
	err := s.db.QueryRowContext(ctx, `SELECT offset FROM cursors WHERE name=?`, name).Scan(&offset)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return offset, true, nil
}

func (s *Store) SetCursor(ctx context.Context, name string, offset int64) error {
	return setCursor(ctx, s.db, name, offset)
}

func setCursor(ctx context.Context, db sqlWriter, name string, offset int64) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO cursors (name, offset, updated_at) VALUES (?,?,?)
		 ON CONFLICT(name) DO UPDATE SET offset=excluded.offset, updated_at=excluded.updated_at`,
		name, offset, formatTime(time.Now().UTC()))
	return err
}

// claimChangedCursor acquires the SQLite write lock and reports whether the
// content is new, even when a second process attempts the same update.
func claimChangedCursor(ctx context.Context, tx *sql.Tx, name string, value int64) (bool, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO cursors (name, offset, updated_at) VALUES (?, ?, ?)
 ON CONFLICT(name) DO UPDATE SET offset=excluded.offset, updated_at=excluded.updated_at
 WHERE cursors.offset != excluded.offset`, name, value, formatTime(time.Now().UTC()))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n != 0, err
}

func (s *Store) recordDecision(ctx context.Context, cursor string, hash, size int64, ev Event, legacyFilenameTime bool) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	changed, err := claimChangedCursor(ctx, tx, cursor+":hash", hash)
	if err != nil {
		return false, err
	}
	if legacyFilenameTime {
		firstCheck, err := claimChangedCursor(ctx, tx, cursor+":time_checked", hash)
		if err != nil {
			return false, err
		}
		if !changed && firstCheck {
			if err := repairDecisionTime(ctx, tx, cursor, hash, size, ev); err != nil {
				return false, err
			}
		}
	}
	if changed {
		if err := appendEvent(ctx, tx, ev); err != nil {
			return false, err
		}
		if err := setCursor(ctx, tx, cursor, size); err != nil {
			return false, err
		}
	}
	return changed, tx.Commit()
}

// Earlier imports recorded ingestion time without a source ID on the event.
// Require the source cursor's original byte count, a unique matching hash
// cursor, and exactly one identical global event before amending a timestamp.
func repairDecisionTime(ctx context.Context, tx *sql.Tx, cursor string, hash, size int64, ev Event) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	var id sql.NullInt64
	var count int
	err = tx.QueryRowContext(ctx, `SELECT MIN(id),COUNT(*) FROM events
 WHERE session_id='' AND type='eval_decision' AND payload=?
   AND (SELECT offset FROM cursors WHERE name=?)=?
   AND (SELECT COUNT(*) FROM cursors WHERE name GLOB 'decision:*:hash' AND offset=?)=1`, string(payload), cursor, size, hash).Scan(&id, &count)
	if err != nil {
		return err
	}
	if count != 1 {
		log.Printf("control: legacy decision timestamp for %s could not be matched unambiguously", cursor)
		return nil
	}
	_, err = tx.ExecContext(ctx, "UPDATE events SET ts=? WHERE id=?", formatTime(ev.TS), id.Int64)
	return err
}

func (s *Store) DeleteCursor(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cursors WHERE name=?`, name)
	return err
}

// CountEventsByType counts events of one type for a session without
// materializing event rows. An empty sessionID counts only session-less/global
// events (session_id = ”), not all sessions.
func (s *Store) CountEventsByType(ctx context.Context, sessionID, eventType string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE session_id=? AND type=?`, sessionID, eventType).Scan(&count)
	return count, err
}

// CountEventsByTypeBetween counts global (session-less) events of one type
// inside a time window without materializing event rows.
func (s *Store) CountEventsByTypeBetween(ctx context.Context, from, to time.Time, eventType string) (int, error) {
	fromText := formatTime(from)
	if fromText == "" {
		fromText = "0000-01-01T00:00:00.000000000Z"
	}
	toText := formatTime(to)
	if toText == "" {
		toText = "9999-12-31T23:59:59.999999999Z"
	}
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE session_id='' AND `+s.timeColumn("ts")+`>=? AND `+s.timeColumn("ts")+`<=? AND type=?`,
		fromText, toText, eventType).Scan(&count)
	return count, err
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
		item.TS, err = storedTime(ts)
		if err != nil {
			return nil, fmt.Errorf("output %d: %w", item.ID, err)
		}
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
