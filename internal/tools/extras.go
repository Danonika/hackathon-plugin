package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/mcp"
)

var classificationRules = map[string][]string{
	"security": {"уязвим", "атак", "фишинг", "phishing", "malware", "взлом", "подозрительн", "exploit", "cve", "инцидент", "утечк"},
	"finance":  {"платеж", "транзакц", "счет", "баланс", "кредит", "fraud", "мошеннич", "выручк", "списан"},
	"medical":  {"диагноз", "пациент", "симптом", "лечен", "дозировк", "анализ крови", "прием"},
	"legal":    {"договор", "иск", "суд", "претенз", "закон", "статья", "юрид", "соглашен"},
	"spam":     {"выигрыш", "бесплатно", "акция", "скидк", "подпишись", "spam", "розыгрыш"},
	"incident": {"error", "warning", "alert", "exception", "failed", "timeout", "critical", "panic"},
}

type chartPoint struct {
	X string  `json:"x"`
	Y float64 `json:"y"`
}

type chartSeries struct {
	Label  string       `json:"label"`
	Points []chartPoint `json:"points"`
}

func registerExtras(reg *mcp.Registry, d Deps) {
	registerClassification(reg, d)
	registerCharts(reg, d)
	registerReports(reg, d)
	registerMedia(reg, d)
}

func registerClassification(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"classification.classify",
		"Classify text into categories with deterministic keyword rules. Returns label, per-category scores and matched keywords.",
		map[string]any{
			"text":       strProp("Text to classify"),
			"categories": arrProp("Optional category names overriding the default rule set", map[string]any{"type": "string"}),
		},
		[]string{"text"},
		func(ctx context.Context, args map[string]any) (any, error) {
			text := strings.ToLower(argString(args, "text"))
			if text == "" {
				return mcp.Fail(trace(ctx), "text is required"), nil
			}
			categories := argStringSlice(args, "categories")
			rules := map[string][]string{}
			if len(categories) == 0 {
				for k, v := range classificationRules {
					rules[k] = v
				}
			} else {
				for _, c := range categories {
					key := strings.ToLower(strings.TrimSpace(c))
					if builtin, ok := classificationRules[key]; ok {
						rules[key] = builtin
					} else {
						rules[key] = []string{key}
					}
				}
			}
			scores := map[string]float64{}
			matched := map[string][]string{}
			best, bestScore := "unknown", 0.0
			ordered := make([]string, 0, len(rules))
			for category := range rules {
				ordered = append(ordered, category)
			}
			sort.Strings(ordered)
			for _, category := range ordered {
				keywords := rules[category]
				hits := 0
				terms := []string{}
				for _, kw := range keywords {
					if strings.Contains(text, kw) {
						hits++
						terms = append(terms, kw)
					}
				}
				if hits > 0 {
					score := float64(hits) / float64(maxInt(len(keywords), 1))
					if score > 1 {
						score = 1
					}
					scores[category] = round2(score)
					sort.Strings(terms)
					matched[category] = terms
					if score > bestScore {
						best = category
						bestScore = score
					}
				}
			}
			if len(scores) == 0 {
				return mcp.OK(trace(ctx), map[string]any{
					"label":  "unknown",
					"scores": map[string]float64{},
				}).WithWarnings("no rule matched; label is unknown"), nil
			}
			return mcp.OK(trace(ctx), map[string]any{
				"label":            best,
				"scores":           scores,
				"matched_keywords": matched[best],
			}), nil
		},
	))
}

func registerCharts(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"charts.create",
		"Render a line or bar chart from series data into an SVG artifact. Points accept x/y or t/v keys.",
		map[string]any{
			"title":      strProp("Chart title"),
			"chart_type": strProp("line (default) or bar"),
			"series": arrProp("Series list: [{label, points:[{x,y}]}]", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":  map[string]any{"type": "string"},
					"points": map[string]any{"type": "array"},
				},
			}),
		},
		[]string{"series"},
		func(ctx context.Context, args map[string]any) (any, error) {
			series, warnings, err := parseChartSeries(args)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			kind := strings.ToLower(argString(args, "chart_type"))
			if kind != "bar" {
				kind = "line"
			}
			if kind == "bar" && len(series) > 1 {
				warnings = append(warnings, "bar charts draw only the first series")
			}
			title := argString(args, "title")
			if title == "" {
				title = "Hackathon Agent Kit chart"
			}
			svg := renderChartSVG(title, kind, series)
			drawn := len(series)
			if kind == "bar" {
				drawn = 1
			}
			stamp := time.Now().UTC().Format("20060102-150405")
			artifact, werr := d.Store.WriteArtifact(
				fmt.Sprintf("charts/%s-%s.svg", slugify(title), stamp),
				[]byte(svg),
			)
			if werr != nil {
				return mcp.Fail(trace(ctx), werr.Error()), nil
			}
			total := 0
			for _, s := range series {
				total += len(s.Points)
			}
			envelope := mcp.OK(trace(ctx), map[string]any{
				"artifact_id":  artifact,
				"chart_type":   kind,
				"series":       len(series),
				"series_drawn": drawn,
				"points":       total,
			}).WithSources(mcp.Source{ArtifactID: artifact, Block: "chart"})
			if len(warnings) > 0 {
				envelope = envelope.WithWarnings(warnings...)
			}
			return envelope, nil
		},
	))
}

func parseChartSeries(args map[string]any) ([]chartSeries, []string, error) {
	raw, _ := args["series"].([]any)
	out := []chartSeries{}
	warnings := []string{}
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("series[%d] is not an object and was skipped", i))
			continue
		}
		label := "series"
		if s, ok := m["label"].(string); ok && strings.TrimSpace(s) != "" {
			label = s
		}
		points := parseChartPoints(m["points"])
		if len(points) == 0 {
			warnings = append(warnings, fmt.Sprintf("series %q has no usable points and was skipped", label))
			continue
		}
		out = append(out, chartSeries{Label: label, Points: points})
	}
	if len(out) == 0 {
		return nil, warnings, fmt.Errorf("series with at least one point is required")
	}
	return out, warnings, nil
}

func parseChartPoints(raw any) []chartPoint {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]chartPoint, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		x := ""
		switch xv := m["x"].(type) {
		case string:
			x = xv
		case float64:
			x = fmt.Sprintf("%g", xv)
		default:
			if tv, ok := m["t"].(string); ok {
				x = tv
			}
		}
		var y float64
		switch yv := m["y"].(type) {
		case float64:
			y = yv
		case int:
			y = float64(yv)
		default:
			if vv, ok := m["v"].(float64); ok {
				y = vv
			}
		}
		out = append(out, chartPoint{X: x, Y: y})
	}
	return out
}

// chartXValues returns a numeric X value per point (numbers or timestamps) when
// every point has one, so line charts honour real spacing instead of indices.
func chartXValues(series []chartSeries) ([]float64, bool) {
	values := []float64{}
	for _, s := range series {
		for _, p := range s.Points {
			if p.X == "" {
				return nil, false
			}
			if v, ok := parseNumber(p.X); ok {
				values = append(values, v)
				continue
			}
			if ts, ok := parseTimeValue(p.X); ok {
				values = append(values, float64(ts.Unix()))
				continue
			}
			return nil, false
		}
	}
	return values, len(values) > 0
}

func renderChartSVG(title, kind string, series []chartSeries) string {
	const w, h = 900, 420
	const left, right, top, bottom = 70.0, 30.0, 60.0, 50.0
	plotW := float64(w) - left - right
	plotH := float64(h) - top - bottom

	yMin, yMax := series[0].Points[0].Y, series[0].Points[0].Y
	for _, s := range series {
		for _, p := range s.Points {
			if p.Y < yMin {
				yMin = p.Y
			}
			if p.Y > yMax {
				yMax = p.Y
			}
		}
	}
	if yMax == yMin {
		yMax = yMin + 1
	}
	pad := (yMax - yMin) * 0.05
	yMin -= pad
	yMax += pad

	xPos := func(i, n int) float64 {
		if n <= 1 {
			return left + plotW/2
		}
		return left + float64(i)*plotW/float64(n-1)
	}
	xVals, useX := chartXValues(series)
	xMinV, xMaxV := 0.0, 0.0
	if useX {
		xMinV, xMaxV = xVals[0], xVals[0]
		for _, v := range xVals {
			if v < xMinV {
				xMinV = v
			}
			if v > xMaxV {
				xMaxV = v
			}
		}
	}
	xPosValue := func(v float64) float64 {
		if xMaxV == xMinV {
			return left + plotW/2
		}
		return left + (v-xMinV)/(xMaxV-xMinV)*plotW
	}
	yPos := func(v float64) float64 {
		return top + plotH - (v-yMin)/(yMax-yMin)*plotH
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="Segoe UI, Arial, sans-serif">`+"\n", w, h, w, h)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#ffffff"/>`+"\n", w, h)
	fmt.Fprintf(&b, `<text x="%.0f" y="30" font-size="18" font-weight="600" fill="#1f2937">%s</text>`+"\n", left, xmlEscape(title))

	ticks := 5
	for i := 0; i <= ticks; i++ {
		v := yMin + (yMax-yMin)*float64(i)/float64(ticks)
		y := yPos(v)
		fmt.Fprintf(&b, `<line x1="%.0f" y1="%.1f" x2="%.0f" y2="%.1f" stroke="#e5e7eb" stroke-width="1"/>`+"\n", left, y, left+plotW, y)
		fmt.Fprintf(&b, `<text x="%.0f" y="%.1f" font-size="11" fill="#6b7280" text-anchor="end">%s</text>`+"\n", left-8, y+4, fmt.Sprintf("%g", round2(v)))
	}
	fmt.Fprintf(&b, `<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#9ca3af"/>`+"\n", left, top, left, top+plotH)
	fmt.Fprintf(&b, `<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#9ca3af"/>`+"\n", left, top+plotH, left+plotW, top+plotH)

	colors := []string{"#2563eb", "#dc2626", "#059669", "#d97706", "#7c3aed"}
	if kind == "bar" {
		s := series[0]
		n := len(s.Points)
		if n > 0 {
			slot := plotW / float64(n)
			barW := slot * 0.7
			if barW > 40 {
				barW = 40
			}
			for i, p := range s.Points {
				cx := left + slot*float64(i) + slot/2
				y := yPos(p.Y)
				height := top + plotH - y
				fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" opacity="0.85"/>`+"\n",
					cx-barW/2, y, barW, height, colors[0])
			}
		}
	} else {
		offset := 0
		for si, s := range series {
			color := colors[si%len(colors)]
			pts := make([]string, 0, len(s.Points))
			for i, p := range s.Points {
				x := xPos(i, len(s.Points))
				if useX && offset+i < len(xVals) {
					x = xPosValue(xVals[offset+i])
				}
				pts = append(pts, fmt.Sprintf("%.1f,%.1f", x, yPos(p.Y)))
			}
			offset += len(s.Points)
			fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="2" points="%s"/>`+"\n", color, strings.Join(pts, " "))
		}
	}

	longest := series[0]
	for _, s := range series {
		if len(s.Points) > len(longest.Points) {
			longest = s
		}
	}
	if len(longest.Points) > 0 {
		positions := []int{0, len(longest.Points) / 2, len(longest.Points) - 1}
		seen := map[int]bool{}
		for _, idx := range positions {
			if seen[idx] {
				continue
			}
			seen[idx] = true
			label := longest.Points[idx].X
			if label == "" {
				label = fmt.Sprintf("%d", idx)
			}
			if runes := []rune(label); len(runes) > 24 {
				label = string(runes[:24]) + "..."
			}
			labelX := xPos(idx, len(longest.Points))
			if kind == "bar" {
				slot := plotW / float64(len(longest.Points))
				labelX = left + slot*float64(idx) + slot/2
			}
			fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-size="11" fill="#6b7280" text-anchor="middle">%s</text>`+"\n",
				labelX, top+plotH+18, xmlEscape(label))
		}
	}

	legendY := 50.0
	for si, s := range series {
		color := colors[si%len(colors)]
		x := left + float64(si)*170
		fmt.Fprintf(&b, `<rect x="%.0f" y="%.0f" width="10" height="10" fill="%s"/>`+"\n", x, legendY-9, color)
		fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-size="12" fill="#374151">%s</text>`+"\n", x+16, legendY, xmlEscape(s.Label))
	}

	b.WriteString("</svg>\n")
	return b.String()
}

func registerReports(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"reports.create",
		"Compose a markdown or HTML report from titled sections and store it as an artifact.",
		map[string]any{
			"title":  strProp("Report title"),
			"format": strProp("md (default) or html"),
			"sections": arrProp("Sections: [{heading, body}]", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"heading": map[string]any{"type": "string"},
					"body":    map[string]any{"type": "string"},
				},
			}),
		},
		[]string{"title", "sections"},
		func(ctx context.Context, args map[string]any) (any, error) {
			title := argString(args, "title")
			if title == "" {
				return mcp.Fail(trace(ctx), "title is required"), nil
			}
			rawSections, _ := args["sections"].([]any)
			if len(rawSections) == 0 {
				return mcp.Fail(trace(ctx), "at least one section is required"), nil
			}
			type section struct {
				Heading string
				Body    string
			}
			sections := []section{}
			for _, item := range rawSections {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				heading, _ := m["heading"].(string)
				body, _ := m["body"].(string)
				if strings.TrimSpace(heading) == "" && strings.TrimSpace(body) == "" {
					continue
				}
				sections = append(sections, section{Heading: heading, Body: body})
			}
			if len(sections) == 0 {
				return mcp.Fail(trace(ctx), "no valid sections provided"), nil
			}
			format := strings.ToLower(argString(args, "format"))
			ext := ".md"
			var content string
			if format == "html" {
				ext = ".html"
				var b strings.Builder
				fmt.Fprintf(&b, "<html><head><meta charset=\"utf-8\"><title>%s</title></head><body>\n", xmlEscape(title))
				fmt.Fprintf(&b, "<h1>%s</h1>\n", xmlEscape(title))
				for _, s := range sections {
					if s.Heading != "" {
						fmt.Fprintf(&b, "<h2>%s</h2>\n", xmlEscape(s.Heading))
					}
					for _, para := range strings.Split(s.Body, "\n\n") {
						if strings.TrimSpace(para) == "" {
							continue
						}
						fmt.Fprintf(&b, "<p>%s</p>\n", xmlEscape(para))
					}
				}
				b.WriteString("</body></html>\n")
				content = b.String()
			} else {
				var b strings.Builder
				fmt.Fprintf(&b, "# %s\n\n", title)
				for _, s := range sections {
					if s.Heading != "" {
						fmt.Fprintf(&b, "## %s\n\n", s.Heading)
					}
					b.WriteString(s.Body)
					if !strings.HasSuffix(s.Body, "\n") {
						b.WriteString("\n")
					}
					b.WriteString("\n")
				}
				content = b.String()
			}
			stamp := time.Now().UTC().Format("20060102-150405")
			artifact, werr := d.Store.WriteArtifact(
				fmt.Sprintf("reports/%s-%s%s", slugify(title), stamp, ext),
				[]byte(content),
			)
			if werr != nil {
				return mcp.Fail(trace(ctx), werr.Error()), nil
			}
			return mcp.OK(trace(ctx), map[string]any{
				"artifact_id": artifact,
				"sections":    len(sections),
				"chars":       len([]rune(content)),
				"preview":     snippet(content, 600),
			}).WithSources(mcp.Source{ArtifactID: artifact, Block: "report"}), nil
		},
	))
}

func registerMedia(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"audio.transcribe",
		"Transcribe an audio file with the OpenAI transcription model and store the transcript as an artifact.",
		map[string]any{
			"path":     strProp("Relative path under data/input"),
			"language": strProp("Optional ISO language hint, e.g. kk or ru"),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			path := argString(args, "path")
			if path == "" {
				return mcp.Fail(trace(ctx), "path is required"), nil
			}
			full, err := d.Store.InputPath(path)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if !d.OpenAI.Available() {
				return mcp.Fail(trace(ctx), "OPENAI_API_KEY is not configured; audio.transcribe requires OpenAI. Set it with: sh scripts/env-set.sh .env OPENAI_API_KEY <value> (see system.resources for the asr variant)"), nil
			}
			text, terr := d.OpenAI.Transcribe(ctx, full, argString(args, "language"))
			if terr != nil {
				return mcp.Fail(trace(ctx), terr.Error()), nil
			}
			base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			stamp := time.Now().UTC().Format("20060102-150405")
			artifact, werr := d.Store.WriteArtifact(
				fmt.Sprintf("audio/%s-%s.txt", slugify(base), stamp),
				[]byte(text),
			)
			if werr != nil {
				return mcp.Fail(trace(ctx), werr.Error()), nil
			}
			return mcp.OK(trace(ctx), map[string]any{
				"artifact_id": artifact,
				"text":        text,
				"chars":       len([]rune(text)),
			}).WithSources(mcp.Source{ArtifactID: artifact, StartTime: "00:00:00"}), nil
		},
	))

	reg.Register(tool(
		"vision.analyze",
		"Analyze an image with the OpenAI vision model and store the result as an artifact. Returns observable facts only.",
		map[string]any{
			"path":   strProp("Relative path under data/input"),
			"prompt": strProp("Analysis prompt"),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			path := argString(args, "path")
			if path == "" {
				return mcp.Fail(trace(ctx), "path is required"), nil
			}
			full, err := d.Store.InputPath(path)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if !d.OpenAI.Available() {
				return mcp.Fail(trace(ctx), "OPENAI_API_KEY is not configured; vision.analyze requires OpenAI. Set it with: sh scripts/env-set.sh .env OPENAI_API_KEY <value> (see system.resources for the vision variant)"), nil
			}
			st, serr := os.Stat(full)
			if serr != nil {
				return mcp.Fail(trace(ctx), serr.Error()), nil
			}
			if st.Size() > 20<<20 {
				return mcp.Fail(trace(ctx), "image larger than 20MB is not supported"), nil
			}
			raw, rerr := os.ReadFile(full)
			if rerr != nil {
				return mcp.Fail(trace(ctx), rerr.Error()), nil
			}
			prompt := argString(args, "prompt")
			if prompt == "" {
				prompt = "Analyze this image. Describe observable facts, visible objects, text and any anomalies. Do not speculate."
			}
			mime := mimeForExt(filepath.Ext(full))
			result, aerr := d.OpenAI.ChatWithImage(ctx, prompt, raw, mime)
			if aerr != nil {
				return mcp.Fail(trace(ctx), aerr.Error()), nil
			}
			base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			stamp := time.Now().UTC().Format("20060102-150405")
			artifact, werr := d.Store.WriteArtifact(
				fmt.Sprintf("vision/%s-%s.md", slugify(base), stamp),
				[]byte(result),
			)
			if werr != nil {
				return mcp.Fail(trace(ctx), werr.Error()), nil
			}
			return mcp.OK(trace(ctx), map[string]any{
				"artifact_id": artifact,
				"analysis":    result,
			}).WithSources(mcp.Source{ArtifactID: artifact, Block: "vision-analysis"}), nil
		},
	))
}

func mimeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "application/octet-stream"
	}
}
