package metadata

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type execCall struct {
	sql  string
	args []any
}

type fakeExecutor struct {
	calls     []execCall
	err       error
	closeCall int
}

func (f *fakeExecutor) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.calls = append(f.calls, execCall{sql: sql, args: args})
	if f.err != nil {
		return pgconn.CommandTag{}, f.err
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (f *fakeExecutor) Close() { f.closeCall++ }

func TestDisabledRecorder(t *testing.T) {
	var recorder Recorder = Disabled{}
	if recorder.Enabled() {
		t.Fatal("disabled recorder must report Enabled() == false")
	}
	if err := recorder.RecordArtifact(context.Background(), ArtifactRecord{ArtifactID: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordRoutingRun(context.Background(), RoutingRun{Mode: "fly", Version: 2}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordFlyPrediction(context.Background(), FlyPrediction{Route: "data"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordEvalRun(context.Background(), EvalRun{Kind: "aggregate", Score: 84}); err != nil {
		t.Fatal(err)
	}
	recorder.Close()
}

func TestOpenPostgresEmptyDSN(t *testing.T) {
	if _, err := OpenPostgres(context.Background(), "   "); err == nil {
		t.Fatal("expected error for an empty dsn")
	}
}

func TestOpenPostgresInvalidDSN(t *testing.T) {
	if _, err := OpenPostgres(context.Background(), "postgres://user:pass@host:notaport/db"); err == nil {
		t.Fatal("expected error for an unparseable dsn")
	}
}

func TestOpenPostgresPingFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenPostgres(ctx, "postgres://user:pass@127.0.0.1:5432/db"); err == nil {
		t.Fatal("expected ping failure with a canceled context")
	}
}

type fakePool struct {
	*fakeExecutor
	pingErr error
}

func (f *fakePool) Ping(ctx context.Context) error { return f.pingErr }

func TestOpenPostgresPoolFactory(t *testing.T) {
	ctx := context.Background()
	const dsn = "postgres://user:pass@127.0.0.1:5432/db"

	t.Run("factory error", func(t *testing.T) {
		sentinel := errors.New("factory")
		if _, err := openPostgres(ctx, dsn, func(context.Context, *pgxpool.Config) (pgPool, error) {
			return nil, sentinel
		}); !errors.Is(err, sentinel) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("ping error closes pool", func(t *testing.T) {
		sentinel := errors.New("ping")
		pool := &fakePool{fakeExecutor: &fakeExecutor{}, pingErr: sentinel}
		if _, err := openPostgres(ctx, dsn, func(context.Context, *pgxpool.Config) (pgPool, error) {
			return pool, nil
		}); !errors.Is(err, sentinel) {
			t.Fatalf("err = %v", err)
		}
		if pool.closeCall != 1 {
			t.Fatalf("close calls = %d, want 1", pool.closeCall)
		}
	})

	t.Run("ddl error closes pool", func(t *testing.T) {
		sentinel := errors.New("ddl")
		pool := &fakePool{fakeExecutor: &fakeExecutor{err: sentinel}}
		if _, err := openPostgres(ctx, dsn, func(context.Context, *pgxpool.Config) (pgPool, error) {
			return pool, nil
		}); !errors.Is(err, sentinel) {
			t.Fatalf("err = %v", err)
		}
		if pool.closeCall != 1 {
			t.Fatalf("close calls = %d, want 1", pool.closeCall)
		}
	})

	t.Run("success", func(t *testing.T) {
		pool := &fakePool{fakeExecutor: &fakeExecutor{}}
		pg, err := openPostgres(ctx, dsn, func(context.Context, *pgxpool.Config) (pgPool, error) {
			return pool, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !pg.Enabled() {
			t.Fatal("expected enabled recorder")
		}
		if len(pool.calls) != 2 ||
			!strings.Contains(pool.calls[0].sql, "CREATE TABLE IF NOT EXISTS artifacts") ||
			!strings.Contains(pool.calls[1].sql, "ADD COLUMN IF NOT EXISTS") {
			t.Fatalf("unexpected ddl calls: %+v", pool.calls)
		}
	})
}

func TestPostgresEnabled(t *testing.T) {
	var nilRecorder *Postgres
	if nilRecorder.Enabled() {
		t.Fatal("nil Postgres must be disabled")
	}
	if (&Postgres{}).Enabled() {
		t.Fatal("Postgres without a pool must be disabled")
	}
	if !(&Postgres{pool: &fakeExecutor{}}).Enabled() {
		t.Fatal("Postgres with a pool must be enabled")
	}
}

func TestPostgresRecordMethods(t *testing.T) {
	ctx := context.Background()
	fake := &fakeExecutor{}
	pg := &Postgres{pool: fake}

	if err := pg.RecordArtifact(ctx, ArtifactRecord{
		ArtifactID: "reports/a.md", Kind: "artifact", Source: "test", Bytes: 7, SHA256: "abc",
	}); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || !strings.Contains(fake.calls[0].sql, "INSERT INTO artifacts") {
		t.Fatalf("unexpected artifact call: %+v", fake.calls)
	}
	args := fake.calls[0].args
	if len(args) != 5 || args[0] != "reports/a.md" || args[3] != 7 || args[4] != "abc" {
		t.Fatalf("unexpected artifact args: %#v", args)
	}

	if err := pg.RecordRoutingRun(ctx, RoutingRun{Mode: "fly", Version: 3}); err != nil {
		t.Fatal(err)
	}
	call := fake.calls[len(fake.calls)-1]
	if !strings.Contains(call.sql, "INSERT INTO routing_runs") || len(call.args) != 2 || call.args[0] != "fly" || call.args[1] != 3 {
		t.Fatalf("unexpected routing call: %+v", call)
	}

	if err := pg.RecordFlyPrediction(ctx, FlyPrediction{
		TraceID: "trace-1", Route: "data", Encoder: "hash", MaxScore: 0.9, Threshold: 0.5,
		CategoryScores:  map[string]float64{"data": 0.9},
		Recommendations: []string{"data.inspect"},
	}); err != nil {
		t.Fatal(err)
	}
	call = fake.calls[len(fake.calls)-1]
	if !strings.Contains(call.sql, "INSERT INTO fly_predictions") || len(call.args) != 7 {
		t.Fatalf("unexpected fly call: %+v", call)
	}
	if call.args[0] != "trace-1" || call.args[1] != "data" || call.args[4] != 0.5 {
		t.Fatalf("unexpected fly args: %#v", call.args)
	}
	if got, ok := call.args[5].([]byte); !ok || !strings.Contains(string(got), `"data":0.9`) {
		t.Fatalf("category scores not marshalled: %#v", call.args[5])
	}
	if got, ok := call.args[6].([]byte); !ok || !strings.Contains(string(got), "data.inspect") {
		t.Fatalf("recommendations not marshalled: %#v", call.args[6])
	}

	if err := pg.RecordEvalRun(ctx, EvalRun{
		Kind: "aggregate", SnapshotSHA: "snap", Score: 84, Target: 80, NoiseFloor: 1.5,
		Decision: "accept", Payload: map[string]any{"n": 3},
	}); err != nil {
		t.Fatal(err)
	}
	call = fake.calls[len(fake.calls)-1]
	if !strings.Contains(call.sql, "INSERT INTO eval_runs") || len(call.args) != 7 {
		t.Fatalf("unexpected eval call: %+v", call)
	}
	if call.args[0] != "aggregate" || call.args[2] != 84.0 || call.args[3] != 80.0 || call.args[5] != "accept" {
		t.Fatalf("unexpected eval args: %#v", call.args)
	}
	if got, ok := call.args[6].([]byte); !ok || !strings.Contains(string(got), `"n":3`) {
		t.Fatalf("payload not marshalled: %#v", call.args[6])
	}
}

func TestPostgresRecordErrors(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("boom")
	fake := &fakeExecutor{err: sentinel}
	pg := &Postgres{pool: fake}

	cases := []struct {
		name string
		call func() error
	}{
		{"artifact", func() error { return pg.RecordArtifact(ctx, ArtifactRecord{ArtifactID: "x"}) }},
		{"routing", func() error { return pg.RecordRoutingRun(ctx, RoutingRun{Mode: "fly"}) }},
		{"fly", func() error { return pg.RecordFlyPrediction(ctx, FlyPrediction{TraceID: "t"}) }},
		{"eval", func() error { return pg.RecordEvalRun(ctx, EvalRun{Kind: "aggregate"}) }},
	}
	for _, tc := range cases {
		if err := tc.call(); !errors.Is(err, sentinel) {
			t.Fatalf("%s: expected sentinel error, got %v", tc.name, err)
		}
	}
}

func TestPostgresRecordMarshalErrors(t *testing.T) {
	ctx := context.Background()
	pg := &Postgres{pool: &fakeExecutor{}}

	if err := pg.RecordFlyPrediction(ctx, FlyPrediction{CategoryScores: map[string]float64{"bad": math.NaN()}}); err == nil {
		t.Fatal("expected category scores marshal error")
	}
	if err := pg.RecordFlyPrediction(ctx, FlyPrediction{Recommendations: make(chan int)}); err == nil {
		t.Fatal("expected recommendations marshal error")
	}
	if err := pg.RecordEvalRun(ctx, EvalRun{Kind: "aggregate", Payload: make(chan int)}); err == nil {
		t.Fatal("expected eval payload marshal error")
	}
}

func TestPostgresDisabledRecordMethods(t *testing.T) {
	ctx := context.Background()
	var nilRecorder *Postgres
	if err := nilRecorder.RecordArtifact(ctx, ArtifactRecord{ArtifactID: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := nilRecorder.RecordRoutingRun(ctx, RoutingRun{Mode: "fly"}); err != nil {
		t.Fatal(err)
	}
	if err := nilRecorder.RecordFlyPrediction(ctx, FlyPrediction{Route: "data"}); err != nil {
		t.Fatal(err)
	}
	if err := nilRecorder.RecordEvalRun(ctx, EvalRun{Kind: "aggregate"}); err != nil {
		t.Fatal(err)
	}
	nilRecorder.Close()

	empty := &Postgres{}
	if err := empty.RecordArtifact(ctx, ArtifactRecord{ArtifactID: "x"}); err != nil {
		t.Fatal(err)
	}
	empty.Close()
}

func TestPostgresClose(t *testing.T) {
	fake := &fakeExecutor{}
	pg := &Postgres{pool: fake}
	pg.Close()
	if fake.closeCall != 1 {
		t.Fatalf("pool close calls = %d, want 1", fake.closeCall)
	}
}
