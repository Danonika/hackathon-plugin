package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Close()
}

type pgPool interface {
	pgExecutor
	Ping(ctx context.Context) error
}

type ArtifactRecord struct {
	ArtifactID string
	Kind       string
	Source     string
	Bytes      int
	SHA256     string
}

type RoutingRun struct {
	Mode    string
	Version int
}

type FlyPrediction struct {
	TraceID         string
	Route           string
	Encoder         string
	MaxScore        float64
	Threshold       float64
	CategoryScores  map[string]float64
	Recommendations any
}

type EvalRun struct {
	Kind        string
	SnapshotSHA string
	Score       float64
	Target      float64
	NoiseFloor  float64
	Decision    string
	Payload     any
}

type Recorder interface {
	Enabled() bool
	RecordArtifact(ctx context.Context, rec ArtifactRecord) error
	RecordRoutingRun(ctx context.Context, run RoutingRun) error
	RecordFlyPrediction(ctx context.Context, pred FlyPrediction) error
	RecordEvalRun(ctx context.Context, run EvalRun) error
	Close()
}

type Disabled struct{}

func (Disabled) Enabled() bool                                            { return false }
func (Disabled) RecordArtifact(context.Context, ArtifactRecord) error     { return nil }
func (Disabled) RecordRoutingRun(context.Context, RoutingRun) error       { return nil }
func (Disabled) RecordFlyPrediction(context.Context, FlyPrediction) error { return nil }
func (Disabled) RecordEvalRun(context.Context, EvalRun) error             { return nil }
func (Disabled) Close()                                                   {}

// The schema is applied idempotently on every startup. Column additions are
// expressed as ADD COLUMN IF NOT EXISTS statements below so existing
// deployments pick them up instead of keeping a stale table definition.
const schema = `
CREATE TABLE IF NOT EXISTS artifacts (
	id BIGSERIAL PRIMARY KEY,
	artifact_id TEXT NOT NULL,
	kind TEXT,
	source TEXT,
	bytes BIGINT,
	sha256 TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS routing_runs (
	id BIGSERIAL PRIMARY KEY,
	mode TEXT NOT NULL,
	version INT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS fly_predictions (
	id BIGSERIAL PRIMARY KEY,
	trace_id TEXT,
	route TEXT,
	encoder TEXT,
	max_score DOUBLE PRECISION,
	threshold DOUBLE PRECISION,
	category_scores JSONB,
	recommendations JSONB,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS eval_runs (
	id BIGSERIAL PRIMARY KEY,
	kind TEXT NOT NULL,
	snapshot_sha TEXT,
	score DOUBLE PRECISION,
	target DOUBLE PRECISION,
	noise_floor DOUBLE PRECISION,
	decision TEXT,
	payload JSONB,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`

// migrations are additive and safe to run repeatedly.
const migrations = `
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS kind TEXT;
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS source TEXT;
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS bytes BIGINT;
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS sha256 TEXT;
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE routing_runs ADD COLUMN IF NOT EXISTS mode TEXT;
ALTER TABLE routing_runs ADD COLUMN IF NOT EXISTS version INT;
ALTER TABLE routing_runs ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE fly_predictions ADD COLUMN IF NOT EXISTS trace_id TEXT;
ALTER TABLE fly_predictions ADD COLUMN IF NOT EXISTS route TEXT;
ALTER TABLE fly_predictions ADD COLUMN IF NOT EXISTS encoder TEXT;
ALTER TABLE fly_predictions ADD COLUMN IF NOT EXISTS max_score DOUBLE PRECISION;
ALTER TABLE fly_predictions ADD COLUMN IF NOT EXISTS threshold DOUBLE PRECISION;
ALTER TABLE fly_predictions ADD COLUMN IF NOT EXISTS category_scores JSONB;
ALTER TABLE fly_predictions ADD COLUMN IF NOT EXISTS recommendations JSONB;
ALTER TABLE fly_predictions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE eval_runs ADD COLUMN IF NOT EXISTS kind TEXT;
ALTER TABLE eval_runs ADD COLUMN IF NOT EXISTS snapshot_sha TEXT;
ALTER TABLE eval_runs ADD COLUMN IF NOT EXISTS score DOUBLE PRECISION;
ALTER TABLE eval_runs ADD COLUMN IF NOT EXISTS target DOUBLE PRECISION;
ALTER TABLE eval_runs ADD COLUMN IF NOT EXISTS noise_floor DOUBLE PRECISION;
ALTER TABLE eval_runs ADD COLUMN IF NOT EXISTS decision TEXT;
ALTER TABLE eval_runs ADD COLUMN IF NOT EXISTS payload JSONB;
ALTER TABLE eval_runs ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
`

// isDuplicateDDL tolerates a concurrent creator winning the DDL race.
func isDuplicateDDL(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "42P07") || strings.Contains(msg, "42710")
}

type Postgres struct {
	mu   sync.RWMutex
	pool pgExecutor
}

func OpenPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	return openPostgres(ctx, dsn, func(ctx context.Context, cfg *pgxpool.Config) (pgPool, error) {
		return pgxpool.NewWithConfig(ctx, cfg)
	})
}

func openPostgres(ctx context.Context, dsn string, newPool func(context.Context, *pgxpool.Config) (pgPool, error)) (*Postgres, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("metadata: empty postgres dsn")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := newPool(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	ddlCtx, ddlCancel := context.WithTimeout(ctx, 15*time.Second)
	defer ddlCancel()
	if _, err := pool.Exec(ddlCtx, schema); err != nil && !isDuplicateDDL(err) {
		pool.Close()
		return nil, err
	}
	if _, err := pool.Exec(ddlCtx, migrations); err != nil && !isDuplicateDDL(err) {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

// poolRef returns the current pool under the lock so callers use a stable
// reference: Close may nil the field concurrently.
func (p *Postgres) poolRef() pgExecutor {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pool
}

func (p *Postgres) Enabled() bool {
	return p.poolRef() != nil
}

func (p *Postgres) RecordArtifact(ctx context.Context, rec ArtifactRecord) error {
	pool := p.poolRef()
	if pool == nil {
		return nil
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO artifacts (artifact_id, kind, source, bytes, sha256) VALUES ($1,$2,$3,$4,$5)`,
		rec.ArtifactID, rec.Kind, rec.Source, rec.Bytes, rec.SHA256)
	return err
}

func (p *Postgres) RecordRoutingRun(ctx context.Context, run RoutingRun) error {
	pool := p.poolRef()
	if pool == nil {
		return nil
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO routing_runs (mode, version) VALUES ($1,$2)`,
		run.Mode, run.Version)
	return err
}

func (p *Postgres) RecordFlyPrediction(ctx context.Context, pred FlyPrediction) error {
	pool := p.poolRef()
	if pool == nil {
		return nil
	}
	scores, err := json.Marshal(pred.CategoryScores)
	if err != nil {
		return fmt.Errorf("metadata: marshal category scores: %w", err)
	}
	recs, err := json.Marshal(pred.Recommendations)
	if err != nil {
		return fmt.Errorf("metadata: marshal recommendations: %w", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO fly_predictions (trace_id, route, encoder, max_score, threshold, category_scores, recommendations)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		pred.TraceID, pred.Route, pred.Encoder, pred.MaxScore, pred.Threshold, scores, recs)
	return err
}

func (p *Postgres) RecordEvalRun(ctx context.Context, run EvalRun) error {
	pool := p.poolRef()
	if pool == nil {
		return nil
	}
	payload, err := json.Marshal(run.Payload)
	if err != nil {
		return fmt.Errorf("metadata: marshal eval payload: %w", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO eval_runs (kind, snapshot_sha, score, target, noise_floor, decision, payload)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		run.Kind, run.SnapshotSHA, run.Score, run.Target, run.NoiseFloor, run.Decision, payload)
	return err
}

func (p *Postgres) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	pool := p.pool
	p.pool = nil
	p.mu.Unlock()
	if pool != nil {
		pool.Close()
	}
}
