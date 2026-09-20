package control

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
			if decision, _ := ev.Payload["decision"].(string); decision == "rollback" {
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

func payloadHasPanic(payload map[string]any) bool {
	if len(payload) == 0 {
		return false
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "panic")
}

// RecomputeRisk refreshes risk for every session, attributing global MCP call
// events to sessions by their time window.
func (s *Store) RecomputeRisk(ctx context.Context) (int, error) {
	sessions, err := s.Sessions(ctx, 1000)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	updated := 0
	for _, sess := range sessions {
		events, err := s.Events(ctx, sess.ID, 2000)
		if err != nil {
			return updated, err
		}
		end := now
		if sess.EndedAt != nil && sess.EndedAt.After(end) {
			end = *sess.EndedAt
		}
		if latest, err := s.latestGlobalTS(ctx); err == nil && latest.After(end) {
			end = latest
		}
		globals, err := s.EventsBetween(ctx, sess.CreatedAt, end, 2000)
		if err != nil {
			return updated, err
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
