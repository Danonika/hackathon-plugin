# Go Test Patterns

Patterns that fit this repository. Tests live next to the package as `_test.go`, use only the standard library, and never touch the network, the real clock or shared mutable state.

## Table-Driven Tests

One table, one loop, explicit subtests. The case name becomes the RED/GREEN evidence label.

```go
func TestSlug(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase", "Telemetry Anomalies", "telemetry-anomalies"},
		{"keeps digits", "S3 plan", "s3-plan"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := plans.Slug(tc.in); got != tc.want {
				t.Fatalf("Slug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
```

Rules: the table is a literal, not built by a helper; each row is independent; `t.Fatalf` stops the subtest at the first mismatch; expected values are written out, never computed with the function under test.

## Fixtures

Prefer `t.TempDir()` over committing fixture files; write inputs in the test and read them back.

```go
func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, []byte(`{"slug":"demo","revision":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := plans.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if plan.Revision != 2 {
		t.Fatalf("revision = %d, want 2", plan.Revision)
	}
}
```

- Fixtures that must be shared across tests live under `testdata/`; Go ignores that directory when building.
- A fixture that encodes a bug belongs in the same commit as the reproducing test.
- Never depend on the developer's `$HOME`, a fixed port or the current date; inject them or freeze them.

## Golden Files

Golden files capture a whole output (report body, rendered SVG, JSON envelope) and are regenerated deliberately.

```go
var update = flag.Bool("update", false, "rewrite golden files")

func TestRender(t *testing.T) {
	got := render(sampleInput)
	golden := filepath.Join("testdata", "render.golden")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("render mismatch (-update to rewrite)\n got: %s\nwant: %s", got, want)
	}
}
```

- Regenerate only when the diff is understood and intended; `-update` output goes into the commit message.
- Normalize volatile parts (timestamps, shas, paths) before comparing; a golden that changes on every run is a flake, not a test.

## Negative Cases

Every new branch gets a failing-path test, not just the happy path.

```go
func TestParseRejectsAbsolute(t *testing.T) {
	_, err := ParsePath("/etc/passwd")
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("err = %v, want ErrUnsafePath", err)
	}
}
```

- Assert the error identity (`errors.Is`) or a stable substring, not the full formatted string.
- Table-driven negative cases carry the expected error as a field:

```go
{"absolute path", "/etc/passwd", ErrUnsafePath},
{"traversal", "../secret", ErrUnsafePath},
{"empty", "", ErrEmptyPath},
```

- A negative case that only checks `err != nil` is coverage theater; name the error you expect.

## Running and Evidence

| Goal | Command |
|---|---|
| Focused RED/GREEN | `go test ./internal/plans/... -run TestSlug -v -count=1` |
| Package-wide | `go test ./internal/plans/...` |
| Repository-wide | `go test ./...` |
| Race detector | `go test -race ./internal/tools/...` |
| Regenerate goldens | `go test ./internal/render/... -run TestRender -update` |

- `-count=1` defeats the test cache; use it whenever a result looks suspiciously fast.
- A focused pass is not a package pass: run both before claiming GREEN.
- Paste the exact command and the tail of its output into the checkpoint, not a paraphrase.

## Anti-Patterns

- A test with no assertion, or an assertion that cannot fail.
- `time.Sleep` to wait for a goroutine; synchronize with a channel or `t.Cleanup`.
- Sharing a package-level fixture map between tests; order dependence makes flaky tests.
- Testing unexported helpers directly when the behavior is observable through an exported function.
- Updating a golden file without reading the diff.
- Skipping a failing test with `t.Skip` and calling the suite green.
