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
		t, err := readCSVTable(path, "\t")
		return t, nil, err
	default:
		t, err := readCSVTable(path, "")
		return t, nil, err
	}
}

func readCSVTable(path string, delimiter string) (*table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("empty table")
	}
	headers := records[0]
	if len(headers) > 0 {
		headers[0] = strings.TrimPrefix(headers[0], "\ufeff")
	}
	return &table{Headers: headers, Rows: records[1:]}, nil
}

func (t *table) columnIndex(name string) int {
	for i, h := range t.Headers {
		if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(name)) {
			return i
		}
	}
	for i, h := range t.Headers {
		if strings.Contains(strings.ToLower(h), strings.ToLower(name)) {
			return i
		}
	}
	return -1
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
	return out
}

func (d Deps) loadTable(ctx context.Context, args map[string]any) (string, *table, []string, *mcp.Envelope) {
	path := argString(args, "path")
	if path == "" {
		env := mcp.Fail(trace(ctx), "path is required")
		return "", nil, nil, &env
	}
	full, err := d.Store.InputPath(path)
	if err != nil {
		env := mcp.Fail(trace(ctx), err.Error())
		return "", nil, nil, &env
	}
	t, warnings, err := readAnyTable(full, argString(args, "sheet"))
	if err != nil {
		env := mcp.Fail(trace(ctx), err.Error())
		return "", nil, nil, &env
	}
	return full, t, warnings, nil
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
			_, t, warnings, fail := d.loadTable(ctx, args)
			if fail != nil {
				return *fail, nil
			}
			profiles := profileColumns(t, argStringSlice(args, "columns"))
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
			_, t, warnings, fail := d.loadTable(ctx, args)
			if fail != nil {
				return *fail, nil
			}
			rows := t.Rows
			if where := argString(args, "where"); where != "" {
				filtered, ferr := applyWhere(t, where)
				if ferr != nil {
					return mcp.Fail(trace(ctx), ferr.Error()), nil
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
			if limit <= 0 || limit > 200 {
				limit = 20
			}
			out := make([]map[string]string, 0, limit)
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
					record[strings.TrimSpace(t.Headers[idx])] = value
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
			_, t, warnings, fail := d.loadTable(ctx, args)
			if fail != nil {
				return *fail, nil
			}
			profiles := profileColumns(t, argStringSlice(args, "columns"))
			numeric := []string{}
			for _, p := range profiles {
				if p.Kind == "numeric" {
					numeric = append(numeric, p.Name)
				}
			}
			type corr struct {
				A, B string
				R    float64
				N    int
			}
			corrs := []corr{}
			for i := 0; i < len(numeric); i++ {
				for j := i + 1; j < len(numeric); j++ {
					idxA, idxB := t.columnIndex(numeric[i]), t.columnIndex(numeric[j])
					var xs, ys []float64
					for _, row := range t.Rows {
						if idxA >= len(row) || idxB >= len(row) {
							continue
						}
						a, okA := parseNumber(row[idxA])
						b, okB := parseNumber(row[idxB])
						if okA && okB {
							xs = append(xs, a)
							ys = append(ys, b)
						}
					}
					if len(xs) < 5 {
						continue
					}
					r := pearson(xs, ys)
					if absFloat(r) >= 0.3 {
						corrs = append(corrs, corr{numeric[i], numeric[j], round2(r), len(xs)})
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

func applyWhere(t *table, where string) ([][]string, error) {
	fields := strings.Fields(where)
	if len(fields) < 3 {
		return nil, fmt.Errorf("where must be \"column op value\", got %q", where)
	}
	column, op := fields[0], fields[1]
	value := strings.Join(fields[2:], " ")
	idx := t.columnIndex(column)
	if idx < 0 {
		return nil, fmt.Errorf("unknown column in where: %s", column)
	}
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
			return nil, fmt.Errorf("unsupported operator %q", op)
		}
		if match {
			out = append(out, row)
		}
	}
	return out, nil
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
