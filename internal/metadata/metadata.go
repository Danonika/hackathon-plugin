package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

type Recorder interface {
	Enabled() bool
	RecordArtifact(ctx context.Context, rec ArtifactRecord) error
	RecordRoutingRun(ctx context.Context, run RoutingRun) error
	RecordFlyPrediction(ctx context.Context, pred FlyPrediction) error
	Close()
}

type Disabled struct{}

func (Disabled) Enabled() bool                                             { return false }
func (Disabled) RecordArtifact(context.Context, ArtifactRecord) error      { return nil }
func (Disabled) RecordRoutingRun(context.Context, RoutingRun) error        { return nil }
func (Disabled) RecordFlyPrediction(context.Context, FlyPrediction) error  { return nil }
func (Disabled) Close()                                                    {}

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
`

type Postgres struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("metadata: empty postgres dsn")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Enabled() bool {
	return p != nil && p.pool != nil
}

func (p *Postgres) RecordArtifact(ctx context.Context, rec ArtifactRecord) error {
	if !p.Enabled() {
		return nil
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO artifacts (artifact_id, kind, source, bytes, sha256) VALUES ($1,$2,$3,$4,$5)`,
		rec.ArtifactID, rec.Kind, rec.Source, rec.Bytes, rec.SHA256)
	return err
}

func (p *Postgres) RecordRoutingRun(ctx context.Context, run RoutingRun) error {
	if !p.Enabled() {
		return nil
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO routing_runs (mode, version) VALUES ($1,$2)`,
		run.Mode, run.Version)
	return err
}

func (p *Postgres) RecordFlyPrediction(ctx context.Context, pred FlyPrediction) error {
	if !p.Enabled() {
		return nil
	}
	scores, _ := json.Marshal(pred.CategoryScores)
	recs, _ := json.Marshal(pred.Recommendations)
	_, err := p.pool.Exec(ctx,
		`INSERT INTO fly_predictions (trace_id, route, encoder, max_score, threshold, category_scores, recommendations)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		pred.TraceID, pred.Route, pred.Encoder, pred.MaxScore, pred.Threshold, scores, recs)
	return err
}

func (p *Postgres) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}
