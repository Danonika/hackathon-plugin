package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	riskToolError    = 8.0
	riskToolErrorCap = 40.0
	riskPanic        = 15.0
	riskPanicCap     = 30.0
	riskRollback     = 25.0
	riskRollbackCap  = 50.0
	riskStalled      = 10.0
	stallWindow      = 2 * time.Hour
)

// EvaluateRisk is deterministic: the same session and events always produce
// the same score and reasons.
func EvaluateRisk(sess Session, events []Event, now time.Time) (float64, []string) {
	score := 0.0
	reasons := []string{}

	toolErrors := 0
	panics := 0
	rollbacks := 0
	for _, ev := range events {
		switch ev.Type {
		case "mcp_call":
			if strings.EqualFold(ev.Status, "error") {
				toolErrors++
			}
			if payloadHasPanic(ev.Payload) {
				panics++
			}
		case "eval_decision":
			if decision, _ := ev.Payload["decision"].(string); strings.EqualFold(strings.TrimSpace(decision), "rollback") {
				rollbacks++
			}
		}
	}
	if toolErrors > 0 {
		score += math.Min(riskToolErrorCap, float64(toolErrors)*riskToolError)
		reasons = append(reasons, fmt.Sprintf("%d failed tool call(s)", toolErrors))
	}
	if panics > 0 {
		score += math.Min(riskPanicCap, float64(panics)*riskPanic)
		reasons = append(reasons, fmt.Sprintf("%d tool panic(s)", panics))
	}
	if rollbacks > 0 {
		score += math.Min(riskRollbackCap, float64(rollbacks)*riskRollback)
		reasons = append(reasons, fmt.Sprintf("%d eval rollback(s)", rollbacks))
	}
	if sess.Status == "active" && !sess.UpdatedAt.IsZero() && now.Sub(sess.UpdatedAt) > stallWindow {
		score += riskStalled
		reasons = append(reasons, "session stalled without events")
	}
	if score > 100 {
		score = 100
	}
	return math.Round(score*100) / 100, reasons
}

var (
	panicWord = regexp.MustCompile(`(?i)(^|[^a-z])panic`)
	// panicNegation matches prose that explicitly reports the absence of a
	// panic, so "no panic observed", "without panic", "panic-free" and
	// "0 panics" are not counted as panic events.
	panicNegation = regexp.MustCompile(`(no|without|zero|0)\s+(errors?\s+or\s+)?panics?|panics?[\s_-]*free|panics?[_ ]?(count)?[\s:=]*(zero|0)`)
	// panicFieldKey matches a structured panic flag/count such as "panic",
	// "panics" or "panic_count". These carry an explicit boolean/numeric state
	// and must be interpreted by value, not by the key name appearing in text.
	panicFieldKey = regexp.MustCompile(`(?i)^panics?(_count|_total)?$`)
)

// payloadHasPanic reports whether the payload records a panic. A structured
// panic field ({"panic":false}, {"panics":0}, {"panic_count":3}) is
// authoritative and read by value; free-text payloads fall back to matching the
// word "panic" after stripping negated prose.
func payloadHasPanic(payload map[string]any) bool {
	if len(payload) == 0 {
		return false
	}
	if found, asserted := scanPanicFields(payload); found {
		return asserted
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	text := strings.ToLower(string(data))
	// "no panic observed" and similar prose must not count as a panic event.
	// Strip common negated prose before matching.
	text = panicNegation.ReplaceAllString(text, "")
	return panicWord.MatchString(text)
}

// scanPanicFields walks the payload (including nested maps and slices) looking
// for explicit panic fields. It reports whether any were found and whether any
// of them asserted a panic, so a benign {"panic":false} is not scored as a
// panic merely because the key contains the word.
func scanPanicFields(v any) (found, asserted bool) {
	switch value := v.(type) {
	case map[string]any:
		for key, item := range value {
			if panicFieldKey.MatchString(key) {
				found = true
				if panicValueAsserted(item) {
					asserted = true
				}
				continue
			}
			f, a := scanPanicFields(item)
			found = found || f
			asserted = asserted || a
		}
	case []any:
		for _, item := range value {
			f, a := scanPanicFields(item)
			found = found || f
			asserted = asserted || a
		}
	}
	return found, asserted
}

// panicValueAsserted interprets a structured panic field's value. Only an
// explicit boolean/numeric (or numeric string) truthy value counts as a panic;
// unknown shapes are treated as not asserted rather than defaulting to a panic.
func panicValueAsserted(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case float64:
		return value > 0
	case float32:
		return value > 0
	case int:
		return value > 0
	case int32:
		return value > 0
	case int64:
		return value > 0
	case json.Number:
		f, err := value.Float64()
		return err == nil && f > 0
	case string:
		switch strings.TrimSpace(strings.ToLower(value)) {
		case "true", "yes":
			return true
		case "", "false", "no", "none", "0":
			return false
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return f > 0
		}
		return false
	}
	return false
}

// RecomputeRisk refreshes risk for every session, attributing global MCP call
// events to sessions by their time window.
// riskScanLimit caps how many rows a single risk pass loads. It must stay
// within the Store query helpers' supported range (<=10000); a larger value is
// silently clamped to a small default, which would score only the newest rows.
const riskScanLimit = 10000

func (s *Store) RecomputeRisk(ctx context.Context) (int, error) {
	sessions, err := s.Sessions(ctx, riskScanLimit)
	if err != nil {
		return 0, err
	}
	if len(sessions) == riskScanLimit {
		log.Printf("control: risk scan truncated at %d sessions; older sessions were not rescored", riskScanLimit)
	}
	now := time.Now().UTC()
	updated := 0
	for _, sess := range sessions {
		events, err := s.Events(ctx, sess.ID, riskScanLimit)
		if err != nil {
			return updated, err
		}
		if len(events) == riskScanLimit {
			log.Printf("control: risk scan for session %s truncated at %d events; older events were not scored", sess.ID, riskScanLimit)
		}
		// A session's window ends at its own end (or now); global events after
		// that must not be attributed to it.
		end := now
		if sess.EndedAt != nil && sess.EndedAt.Before(end) {
			end = *sess.EndedAt
		}
		globals, err := s.EventsBetween(ctx, sess.CreatedAt, end, riskScanLimit)
		if err != nil {
			return updated, err
		}
		if len(globals) == riskScanLimit {
			log.Printf("control: risk global scan for session %s truncated at %d events; older events were not scored", sess.ID, riskScanLimit)
		}
		combined := append(append([]Event{}, events...), globals...)
		score, reasons := EvaluateRisk(sess, combined, now)
		if score != sess.RiskScore || strings.Join(reasons, "|") != strings.Join(sess.RiskReasons, "|") {
			if err := s.UpdateRisk(ctx, sess.ID, score, reasons); err != nil {
				return updated, err
			}
			updated++
		}
	}
	return updated, nil
}
