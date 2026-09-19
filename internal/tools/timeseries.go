package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/mcp"
)

type seriesPoint struct {
	T   time.Time
	V   float64
	Raw string
}

func loadSeries(t *table, timeColumn, valueColumn string) ([]seriesPoint, string, string, error) {
	profiles := profileColumns(t, nil)
	timeIdx := -1
	valueIdx := -1
	if timeColumn != "" {
		timeIdx = t.columnIndex(timeColumn)
	} else {
		for i, p := range profiles {
			if p.Kind == "timestamp" {
				timeIdx = i
				timeColumn = p.Name
				break
			}
		}
	}
	if valueColumn != "" {
		valueIdx = t.columnIndex(valueColumn)
	} else {
		for i, p := range profiles {
			if p.Kind == "numeric" && i != timeIdx {
				valueIdx = i
				valueColumn = p.Name
				break
			}
		}
	}
	if timeIdx < 0 || valueIdx < 0 {
		return nil, "", "", fmt.Errorf("could not detect time/value columns; pass time_column and value_column explicitly")
	}
	points := []seriesPoint{}
	for _, row := range t.Rows {
		if timeIdx >= len(row) || valueIdx >= len(row) {
			continue
		}
		ts, okT := parseTimeValue(row[timeIdx])
		val, okV := parseNumber(row[valueIdx])
		if !okT || !okV {
			continue
		}
		points = append(points, seriesPoint{T: ts, V: val, Raw: row[timeIdx]})
	}
	if len(points) < 3 {
		return nil, "", "", fmt.Errorf("not enough valid (time, value) rows: %d", len(points))
	}
	sort.Slice(points, func(i, j int) bool { return points[i].T.Before(points[j].T) })
	return points, timeColumn, valueColumn, nil
}

func downsample(points []seriesPoint, maxPoints int) []seriesPoint {
	if maxPoints <= 0 || len(points) <= maxPoints {
		return points
	}
	step := float64(len(points)) / float64(maxPoints)
	out := make([]seriesPoint, 0, maxPoints)
	for i := 0; i < maxPoints; i++ {
		idx := int(float64(i) * step)
		if idx >= len(points) {
			idx = len(points) - 1
		}
		out = append(out, points[idx])
	}
	return out
}

func seriesSlopePerHour(points []seriesPoint) float64 {
	n := float64(len(points))
	if n < 2 {
		return 0
	}
	base := points[0].T
	var sumX, sumY, sumXY, sumXX float64
	for _, p := range points {
		x := p.T.Sub(base).Seconds()
		sumX += x
		sumY += p.V
		sumXY += x * p.V
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	slopePerSec := (n*sumXY - sumX*sumY) / denom
	return slopePerSec * 3600
}

func registerTimeSeries(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"timeseries.analyze",
		"Analyze a time series from a CSV/XLSX table: range, statistics, trend slope and a downsampled series for charts.",
		map[string]any{
			"path":         strProp("Relative path under data/input"),
			"sheet":        strProp("XLSX sheet name (default first sheet)"),
			"time_column":  strProp("Timestamp column (auto-detected when omitted)"),
			"value_column": strProp("Numeric column (auto-detected when omitted)"),
			"max_points":   numProp("Maximum points in the returned series (default 120)"),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			_, t, warnings, fail := d.loadTable(ctx, args)
			if fail != nil {
				return *fail, nil
			}
			points, timeCol, valueCol, err := loadSeries(t, argString(args, "time_column"), argString(args, "value_column"))
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			values := make([]float64, 0, len(points))
			for _, p := range points {
				values = append(values, p.V)
			}
			maxPoints := argInt(args, "max_points", 120)
			if maxPoints <= 0 || maxPoints > 5000 {
				maxPoints = 120
			}
			sample := downsample(points, maxPoints)
			series := make([]map[string]any, 0, len(sample))
			for _, p := range sample {
				series = append(series, map[string]any{
					"t": p.T.UTC().Format(time.RFC3339),
					"v": round2(p.V),
				})
			}
			first, last := points[0], points[len(points)-1]
			env := mcp.OK(trace(ctx), map[string]any{
				"time_column":    timeCol,
				"value_column":   valueCol,
				"count":          len(points),
				"start":          first.T.UTC().Format(time.RFC3339),
				"end":            last.T.UTC().Format(time.RFC3339),
				"min":            round2(minOf(values)),
				"max":            round2(maxOf(values)),
				"mean":           round2(mean(values)),
				"std":            round2(stddev(values)),
				"slope_per_hour": round2(seriesSlopePerHour(points)),
				"series":         series,
			}).WithSources(mcp.Source{Row: mcp.IntPtr(0), Column: valueCol, Timestamp: first.Raw})
			if len(warnings) > 0 {
				env = env.WithWarnings(warnings...)
			}
			return env, nil
		},
	))

	reg.Register(tool(
		"timeseries.detect_anomalies",
		"Detect anomalies in a time series using rolling z-score or IQR. Returns anomalies with timestamps and scores.",
		map[string]any{
			"path":         strProp("Relative path under data/input"),
			"sheet":        strProp("XLSX sheet name (default first sheet)"),
			"time_column":  strProp("Timestamp column (auto-detected when omitted)"),
			"value_column": strProp("Numeric column (auto-detected when omitted)"),
			"method":       strProp("zscore (default) or iqr"),
			"window":       numProp("Rolling window size for zscore (default 24)"),
			"threshold":    numProp("Z-score threshold (default 3.0)"),
			"limit":        numProp("Maximum anomalies to return (default 100)"),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			_, t, warnings, fail := d.loadTable(ctx, args)
			if fail != nil {
				return *fail, nil
			}
			points, timeCol, valueCol, err := loadSeries(t, argString(args, "time_column"), argString(args, "value_column"))
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			method := strings.ToLower(argString(args, "method"))
			if method == "" {
				method = "zscore"
			}
			type anomaly struct {
				Timestamp string  `json:"timestamp"`
				Value     float64 `json:"value"`
				Score     float64 `json:"score"`
				Method    string  `json:"method"`
			}
			anomalies := []anomaly{}
			switch method {
			case "iqr":
				values := make([]float64, 0, len(points))
				for _, p := range points {
					values = append(values, p.V)
				}
				sorted := append([]float64(nil), values...)
				sort.Float64s(sorted)
				q1 := quantile(sorted, 0.25)
				q3 := quantile(sorted, 0.75)
				iqr := q3 - q1
				low, high := q1-1.5*iqr, q3+1.5*iqr
				for _, p := range points {
					if p.V < low || p.V > high {
						dist := p.V - high
						if p.V < low {
							dist = low - p.V
						}
						score := 0.0
						if iqr > 0 {
							score = dist / iqr
						}
						anomalies = append(anomalies, anomaly{
							Timestamp: p.T.UTC().Format(time.RFC3339),
							Value:     round2(p.V),
							Score:     round2(score),
							Method:    "iqr",
						})
					}
				}
			default:
				window := argInt(args, "window", 24)
				threshold := argFloat(args, "threshold", 3.0)
				for i := 1; i < len(points); i++ {
					start := i - window
					if start < 0 {
						start = 0
					}
					if i-start < 3 {
						continue
					}
					values := make([]float64, 0, i-start)
					for j := start; j < i; j++ {
						values = append(values, points[j].V)
					}
					m, s := mean(values), stddev(values)
					if s == 0 {
						if points[i].V != m {
							anomalies = append(anomalies, anomaly{
								Timestamp: points[i].T.UTC().Format(time.RFC3339),
								Value:     round2(points[i].V),
								Score:     round2(threshold),
								Method:    "zscore",
							})
						}
						continue
					}
					z := (points[i].V - m) / s
					if z < 0 {
						z = -z
					}
					if z >= threshold {
						anomalies = append(anomalies, anomaly{
							Timestamp: points[i].T.UTC().Format(time.RFC3339),
							Value:     round2(points[i].V),
							Score:     round2(z),
							Method:    "zscore",
						})
					}
				}
			}
			sort.Slice(anomalies, func(i, j int) bool { return anomalies[i].Timestamp < anomalies[j].Timestamp })
			limit := argInt(args, "limit", 100)
			if limit <= 0 || limit > 10000 {
				limit = 100
			}
			if len(anomalies) > limit {
				anomalies = anomalies[:limit]
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"time_column":   timeCol,
				"value_column":  valueCol,
				"method":        method,
				"count":         len(points),
				"anomaly_count": len(anomalies),
				"anomalies":     anomalies,
			})
			for _, a := range anomalies {
				env = env.WithSources(mcp.Source{Timestamp: a.Timestamp, Column: valueCol})
			}
			if len(warnings) > 0 {
				env = env.WithWarnings(warnings...)
			}
			return env, nil
		},
	))
}

func minOf(values []float64) float64 {
	m := values[0]
	for _, v := range values {
		if v < m {
			m = v
		}
	}
	return m
}

func maxOf(values []float64) float64 {
	m := values[0]
	for _, v := range values {
		if v > m {
			m = v
		}
	}
	return m
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := q * float64(len(sorted)-1)
	lower := int(pos)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := pos - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
