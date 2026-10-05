package tools

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hackathonkit.local/agentkit/internal/mcp"
)

type table struct {
	Headers []string
	Rows    [][]string
}

func readAnyTable(path string, sheet string) (*table, []string, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "xlsx":
		return readXLSXTable(path, sheet)
	case "xls":
		return nil, nil, fmt.Errorf("legacy .xls (BIFF) tables are not supported; convert the file to .xlsx or .csv first")
	case "tsv":
		return readCSVTable(path, "\t")
	default:
		return readCSVTable(path, "")
	}
}

func readCSVTable(path string, delimiter string) (*table, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	if delimiter != "" {
		runes := []rune(delimiter)
		if len(runes) > 0 {
			r.Comma = runes[0]
		}
	}
	records, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("empty table")
	}
	headers := records[0]
	if len(headers) > 0 {
		headers[0] = strings.TrimPrefix(headers[0], "\ufeff")
	}
	rows := records[1:]
	warnings := []string{}
	ragged := 0
	for i, row := range rows {
		if len(row) == len(headers) {
			continue
		}
		ragged++
		if len(row) < len(headers) {
			// Actually pad short rows, matching the warning below, so
			// downstream consumers can index by header position.
			padded := make([]string, len(headers))
			copy(padded, row)
			rows[i] = padded
		}
	}
	if ragged > 0 {
		warnings = append(warnings, fmt.Sprintf("%d row(s) do not match the header column count; short rows are padded with empty values", ragged))
	}
	return &table{Headers: headers, Rows: rows}, warnings, nil
}

func (t *table) columnIndex(name string) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1
	}
	for i, h := range t.Headers {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return i
		}
	}
	needle := strings.ToLower(name)
	// An exact (case-insensitive) header always wins above. Otherwise fall back
	// to a unique substring match: a partial match may bind a column the caller
	// never named (e.g. "id" matching "grid"), which is why an ambiguous
	// substring match is refused rather than silently resolved.
	match := -1
	for i, h := range t.Headers {
		if strings.Contains(strings.ToLower(h), needle) {
			if match >= 0 {
				// Ambiguous partial match: refuse instead of silently picking
				// the first containing header.
				return -1
			}
			match = i
		}
	}
	return match
}

type columnProfile struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	NonEmpty int      `json:"non_empty"`
	Missing  int      `json:"missing"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Mean     *float64 `json:"mean,omitempty"`
	Std      *float64 `json:"std,omitempty"`
	Samples  []string `json:"samples,omitempty"`
}

func profileColumns(t *table, selected []string) []columnProfile {
	profiles, _ := profileColumnsIndexed(t, selected)
	return profiles
}

// profileColumnsIndexed profiles the selected columns and also returns the
// resolved header index for each profile, so callers do not have to re-resolve
// (and possibly re-resolve to a different column).
func profileColumnsIndexed(t *table, selected []string) ([]columnProfile, []int) {
	indices := []int{}
	if len(selected) == 0 {
		for i := range t.Headers {
			indices = append(indices, i)
		}
	} else {
		for _, name := range selected {
			if idx := t.columnIndex(name); idx >= 0 {
				indices = append(indices, idx)
			}
		}
	}
	out := make([]columnProfile, 0, len(indices))
	for _, idx := range indices {
		p := columnProfile{Name: strings.TrimSpace(t.Headers[idx])}
		var numbers []float64
		timeHits := 0
		for _, row := range t.Rows {
			value := ""
			if idx < len(row) {
				value = strings.TrimSpace(row[idx])
			}
			if value == "" {
				p.Missing++
				continue
			}
			p.NonEmpty++
			if len(p.Samples) < 3 {
				p.Samples = append(p.Samples, value)
			}
			if v, ok := parseNumber(value); ok {
				numbers = append(numbers, v)
			}
			if _, ok := parseTimeValue(value); ok {
				timeHits++
			}
		}
		switch {
		case p.NonEmpty > 0 && len(numbers) == p.NonEmpty:
			p.Kind = "numeric"
			mn, mx := numbers[0], numbers[0]
			for _, v := range numbers {
				if v < mn {
					mn = v
				}
				if v > mx {
					mx = v
				}
			}
			m, s := mean(numbers), stddev(numbers)
			p.Min, p.Max, p.Mean, p.Std = &mn, &mx, &m, &s
		case p.NonEmpty > 0 && timeHits*5 >= p.NonEmpty*4:
			p.Kind = "timestamp"
		default:
			p.Kind = "text"
		}
		out = append(out, p)
	}
	return out, indices
}

const (
	maxTableBytes         = 64 << 20
	maxTableRows          = 2_000_000
	maxCorrelationColumns = 32
)

func (d Deps) loadTable(ctx context.Context, args map[string]any) (*table, []string, *mcp.Envelope) {
	path := argString(args, "path")
	if path == "" {
		env := mcp.Fail(trace(ctx), "path is required")
		return nil, nil, &env
	}
	full, err := d.Store.InputPath(path)
	if err != nil {
		env := mcp.Fail(trace(ctx), err.Error())
		return nil, nil, &env
	}
	info, serr := os.Stat(full)
	if serr != nil {
		env := mcp.Fail(trace(ctx), "cannot stat table "+path+": "+serr.Error())
		return nil, nil, &env
	}
	if info.Size() > maxTableBytes {
		env := mcp.Fail(trace(ctx), fmt.Sprintf("table %s is %d bytes; exceeds the %d-byte limit", path, info.Size(), maxTableBytes))
		return nil, nil, &env
	}
	t, warnings, err := readAnyTable(full, argString(args, "sheet"))
	if err != nil {
		env := mcp.Fail(trace(ctx), err.Error())
		return nil, nil, &env
	}
	if len(t.Rows) > maxTableRows {
		warnings = append(warnings, fmt.Sprintf("table has %d rows; only the first %d were loaded", len(t.Rows), maxTableRows))
		t.Rows = t.Rows[:maxTableRows]
	}
	if err := ctx.Err(); err != nil {
		env := mcp.Fail(trace(ctx), err.Error())
		return nil, nil, &env
	}
	return t, warnings, nil
}

// uniqueHeaderKeys builds output keys that stay unique when headers collide
// after trimming (e.g. "id" and "id ") or are blank.
func uniqueHeaderKeys(headers []string) []string {
	keys := make([]string, len(headers))
	used := map[string]bool{}
	for i, h := range headers {
		base := strings.TrimSpace(h)
		if base == "" {
			base = fmt.Sprintf("column_%d", i)
		}
		key := base
		// Register every emitted key (including generated ones) and keep
		// probing, so a generated suffix cannot collide with a literal header.
		for n := 1; used[key]; n++ {
			key = fmt.Sprintf("%s_%d", base, n)
		}
		used[key] = true
		keys[i] = key
	}
	return keys
}

func registerData(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"data.inspect",
		"Inspect a CSV/TSV/XLSX table: rows, columns, column kinds, missing values and summary statistics.",
		map[string]any{
			"path":    strProp("Relative path under data/input"),
			"sheet":   strProp("XLSX sheet name (default first sheet)"),
			"columns": arrProp("Optional subset of columns", map[string]any{"type": "string"}),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			t, warnings, fail := d.loadTable(ctx, args)
			if fail != nil {
				return *fail, nil
			}
			requested := argStringSlice(args, "columns")
			profiles := profileColumns(t, requested)
			for _, name := range requested {
				if t.columnIndex(name) < 0 {
					warnings = append(warnings, "unknown column requested: "+name)
				}
			}
			if len(t.Headers) > 64 {
				warnings = append(warnings, "more than 64 columns detected; consider a column subset")
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"rows":    len(t.Rows),
				"columns": len(t.Headers),
				"headers": t.Headers,
				"profile": profiles,
			})
			if len(warnings) > 0 {
				env = env.WithWarnings(warnings...)
			}
			return env, nil
		},
	))

	reg.Register(tool(
		"data.query",
		"Query a CSV/TSV/XLSX table: select columns, filter with a simple condition and limit rows.",
		map[string]any{
			"path":    strProp("Relative path under data/input"),
			"sheet":   strProp("XLSX sheet name (default first sheet)"),
			"columns": arrProp("Columns to return (default all)", map[string]any{"type": "string"}),
			"where":   strProp(`Simple filter: "column op value", op in = == != > >= < <= contains`),
			"limit":   numProp("Maximum rows to return (default 20, max 200)"),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			t, warnings, fail := d.loadTable(ctx, args)
			if fail != nil {
				return *fail, nil
			}
			rows := t.Rows
			if where := argString(args, "where"); where != "" {
				filtered, skipped, ferr := applyWhere(t, where)
				if ferr != nil {
					return mcp.Fail(trace(ctx), ferr.Error()), nil
				}
				if skipped > 0 {
					warnings = append(warnings, fmt.Sprintf("%d row(s) skipped: the column value is not numeric for an ordering comparison", skipped))
				}
				rows = filtered
			}
			columns := argStringSlice(args, "columns")
			indices := []int{}
			if len(columns) == 0 {
				for i := range t.Headers {
					indices = append(indices, i)
				}
			} else {
				for _, name := range columns {
					idx := t.columnIndex(name)
					if idx < 0 {
						return mcp.Fail(trace(ctx), "unknown column: "+name), nil
					}
					indices = append(indices, idx)
				}
			}
			limit := argInt(args, "limit", 20)
			if limit <= 0 {
				limit = 20
			} else if limit > 200 {
				limit = 200
			}
			out := make([]map[string]string, 0, limit)
			outKeys := uniqueHeaderKeys(t.Headers)
			for i, row := range rows {
				if i >= limit {
					break
				}
				record := map[string]string{}
				for _, idx := range indices {
					value := ""
					if idx < len(row) {
						value = row[idx]
					}
					record[outKeys[idx]] = value
				}
				out = append(out, record)
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"matched_rows": len(rows),
				"returned":     len(out),
				"rows":         out,
			})
			if len(warnings) > 0 {
				env = env.WithWarnings(warnings...)
			}
			return env, nil
		},
	))

	reg.Register(tool(
		"data.analyze",
		"Analyze numeric columns of a table: statistics and strongest pairwise correlations.",
		map[string]any{
			"path":    strProp("Relative path under data/input"),
			"sheet":   strProp("XLSX sheet name (default first sheet)"),
			"columns": arrProp("Optional subset of columns", map[string]any{"type": "string"}),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			t, warnings, fail := d.loadTable(ctx, args)
			if fail != nil {
				return *fail, nil
			}
			requested := argStringSlice(args, "columns")
			profiles, indices := profileColumnsIndexed(t, requested)
			for _, name := range requested {
				if t.columnIndex(name) < 0 {
					warnings = append(warnings, "unknown column requested: "+name)
				}
			}
			// Parse each numeric column once (NaN marks an unparseable cell) so
			// the pairwise correlation loop does not reparse every row.
			type numericCol struct {
				name   string
				values []float64
			}
			// Cap how many numeric columns are parsed so peak memory is bounded
			// by maxCorrelationColumns, not by every numeric column.
			totalNumeric := 0
			for _, p := range profiles {
				if p.Kind == "numeric" {
					totalNumeric++
				}
			}
			cols := []numericCol{}
			for i, p := range profiles {
				if p.Kind != "numeric" {
					continue
				}
				if len(cols) >= maxCorrelationColumns {
					break
				}
				idx := indices[i]
				values := make([]float64, len(t.Rows))
				for ri, row := range t.Rows {
					if idx < len(row) {
						if v, ok := parseNumber(row[idx]); ok {
							values[ri] = v
							continue
						}
					}
					values[ri] = math.NaN()
				}
				cols = append(cols, numericCol{name: p.Name, values: values})
			}
			if totalNumeric > maxCorrelationColumns {
				warnings = append(warnings, fmt.Sprintf("%d numeric columns; correlations are computed for the first %d", totalNumeric, maxCorrelationColumns))
			}
			type corr struct {
				A, B string
				R    float64
				N    int
			}
			corrs := []corr{}
			// Reuse one pair of scratch buffers across every column pair
			// instead of allocating two fresh slices per pair (there can be
			// hundreds of pairs over millions of rows).
			scratchCap := 0
			for _, col := range cols {
				if len(col.values) > scratchCap {
					scratchCap = len(col.values)
				}
			}
			xs := make([]float64, 0, scratchCap)
			ys := make([]float64, 0, scratchCap)
			for i := 0; i < len(cols); i++ {
				if err := ctx.Err(); err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				for j := i + 1; j < len(cols); j++ {
					xs, ys = xs[:0], ys[:0]
					for k := range cols[i].values {
						a, b := cols[i].values[k], cols[j].values[k]
						if !math.IsNaN(a) && !math.IsNaN(b) {
							xs = append(xs, a)
							ys = append(ys, b)
						}
					}
					if len(xs) < 5 {
						continue
					}
					r := pearson(xs, ys)
					if absFloat(r) >= 0.3 {
						corrs = append(corrs, corr{cols[i].name, cols[j].name, round2(r), len(xs)})
					}
				}
			}
			sort.Slice(corrs, func(i, j int) bool { return absFloat(corrs[i].R) > absFloat(corrs[j].R) })
			if len(corrs) > 5 {
				corrs = corrs[:5]
			}
			corrOut := []map[string]any{}
			for _, c := range corrs {
				corrOut = append(corrOut, map[string]any{
					"columns": []string{c.A, c.B},
					"r":       c.R,
					"n":       c.N,
				})
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"rows":         len(t.Rows),
				"profile":      profiles,
				"correlations": corrOut,
			})
			if len(warnings) > 0 {
				env = env.WithWarnings(warnings...)
			}
			return env, nil
		},
	))
}

func applyWhere(t *table, where string) ([][]string, int, error) {
	fields := strings.Fields(where)
	if len(fields) < 3 {
		return nil, 0, fmt.Errorf("where must be \"column op value\", got %q", where)
	}
	column, op := fields[0], fields[1]
	value := strings.Join(fields[2:], " ")
	idx := t.columnIndex(column)
	if idx < 0 {
		return nil, 0, fmt.Errorf("unknown column in where: %s", column)
	}
	switch op {
	case "=", "==", "!=", ">", ">=", "<", "<=", "contains":
	default:
		return nil, 0, fmt.Errorf("unsupported operator %q", op)
	}
	skippedNonNumeric := 0
	out := [][]string{}
	for _, row := range t.Rows {
		cell := ""
		if idx < len(row) {
			cell = row[idx]
		}
		match := false
		switch op {
		case "=", "==":
			if cv, okC := parseNumber(cell); okC {
				if vv, okV := parseNumber(value); okV {
					match = cv == vv
					break
				}
			}
			match = strings.EqualFold(strings.TrimSpace(cell), value)
		case "!=":
			if cv, okC := parseNumber(cell); okC {
				if vv, okV := parseNumber(value); okV {
					match = cv != vv
					break
				}
			}
			match = !strings.EqualFold(strings.TrimSpace(cell), value)
		case ">", ">=", "<", "<=":
			cv, okC := parseNumber(cell)
			vv, okV := parseNumber(value)
			if !okC || !okV {
				skippedNonNumeric++
				continue
			}
			switch op {
			case ">":
				match = cv > vv
			case ">=":
				match = cv >= vv
			case "<":
				match = cv < vv
			case "<=":
				match = cv <= vv
			}
		case "contains":
			match = strings.Contains(strings.ToLower(cell), strings.ToLower(value))
		default:
			return nil, 0, fmt.Errorf("unsupported operator %q", op)
		}
		if match {
			out = append(out, row)
		}
	}
	return out, skippedNonNumeric, nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
