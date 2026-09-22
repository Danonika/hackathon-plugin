package mcp

type Source struct {
	ArtifactID string `json:"artifact_id,omitempty"`
	Page       *int   `json:"page,omitempty"`
	Block      string `json:"block,omitempty"`
	Sheet      string `json:"sheet,omitempty"`
	Row        *int   `json:"row,omitempty"`
	Column     string `json:"column,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
	StartTime  string `json:"start_time,omitempty"`
	EndTime    string `json:"end_time,omitempty"`
	Region     string `json:"region,omitempty"`
}

// IntPtr returns a pointer to v, so callers can express an explicit zero index
// in a Source without it being indistinguishable from "not set".
func IntPtr(v int) *int {
	return &v
}

type Envelope struct {
	Status   string         `json:"status"`
	Data     any            `json:"data,omitempty"`
	Sources  []Source       `json:"sources,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`
	Metrics  map[string]any `json:"metrics,omitempty"`
	TraceID  string         `json:"trace_id"`
}

func OK(trace string, data any) Envelope {
	return Envelope{Status: "ok", Data: data, TraceID: trace}
}

func Partial(trace string, data any, warnings ...string) Envelope {
	return Envelope{Status: "partial", Data: data, Warnings: warnings, TraceID: trace}
}

func Fail(trace string, msg string) Envelope {
	return Envelope{Status: "error", Data: map[string]any{"error": msg}, TraceID: trace}
}

func (e Envelope) WithSources(sources ...Source) Envelope {
	// Copy first: appending to a shared backing array would mutate the caller.
	merged := make([]Source, 0, len(e.Sources)+len(sources))
	merged = append(merged, e.Sources...)
	merged = append(merged, sources...)
	e.Sources = merged
	return e
}

func (e Envelope) WithWarnings(warnings ...string) Envelope {
	e.Warnings = append(e.Warnings, warnings...)
	return e
}

func (e Envelope) WithMetrics(m map[string]any) Envelope {
	merged := make(map[string]any, len(e.Metrics)+len(m))
	for k, v := range e.Metrics {
		merged[k] = v
	}
	for k, v := range m {
		merged[k] = v
	}
	e.Metrics = merged
	return e
}
