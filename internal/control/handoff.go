package control

import (
	"context"
	"fmt"
	"strings"

	"hackathonkit.local/agentkit/internal/learning"
	"hackathonkit.local/agentkit/internal/plans"
)

type Handoff struct {
	Session          Session             `json:"session"`
	Plan             *plans.Plan         `json:"plan,omitempty"`
	Learnings        []learning.Learning `json:"learnings,omitempty"`
	RecentEvents     []Event             `json:"recent_events,omitempty"`
	Outputs          []Output            `json:"outputs,omitempty"`
	PendingDecisions int                 `json:"pending_decisions"`
	Notes            []string            `json:"notes,omitempty"`
}

// BuildHandoff assembles the context a new agent session needs to resume work.
func BuildHandoff(ctx context.Context, store *Store, learn *learning.Store, root, sessionID string, maxLearnings int) (Handoff, error) {
	if maxLearnings <= 0 {
		maxLearnings = 6
	}
	handoff := Handoff{}
	session, err := store.Session(ctx, sessionID)
	if err != nil {
		return handoff, fmt.Errorf("session %s: %w", sessionID, err)
	}
	handoff.Session = session

	if plan, ok := plans.Latest(root); ok {
		handoff.Plan = &plan
	}
	if learn != nil {
		if items, err := learn.Top(maxLearnings, 0.5); err == nil {
			handoff.Learnings = items
		} else {
			handoff.Notes = append(handoff.Notes, "learning store unreadable: "+err.Error())
		}
	}
	events, err := store.Events(ctx, sessionID, 10)
	if err != nil {
		return handoff, fmt.Errorf("session %s events: %w", sessionID, err)
	}
	handoff.RecentEvents = events
	outputs, err := store.Outputs(ctx, sessionID, 5)
	if err != nil {
		return handoff, fmt.Errorf("session %s outputs: %w", sessionID, err)
	}
	handoff.Outputs = outputs
	// Count decisions from a wider window than the 10 shown, so older pending
	// decisions are not silently invisible.
	if decisionEvents, derr := store.Events(ctx, sessionID, 500); derr == nil {
		for _, ev := range decisionEvents {
			if ev.Type == "eval_decision" {
				handoff.PendingDecisions++
			}
		}
	} else {
		handoff.Notes = append(handoff.Notes, "decision history unreadable: "+derr.Error())
	}
	return handoff, nil
}

// RenderHandoff formats a handoff as a paste-ready markdown block.
func RenderHandoff(h Handoff) string {
	var b strings.Builder
	b.WriteString("## Session handoff\n")
	fmt.Fprintf(&b, "- session: %s (%s, risk %.2f)\n", h.Session.ID, h.Session.Status, h.Session.RiskScore)
	if h.Session.Task != "" {
		fmt.Fprintf(&b, "- task: %s\n", h.Session.Task)
	}
	if h.Session.Worktree != "" {
		fmt.Fprintf(&b, "- worktree: %s\n", h.Session.Worktree)
	}
	if len(h.Session.RiskReasons) > 0 {
		fmt.Fprintf(&b, "- risk reasons: %s\n", strings.Join(h.Session.RiskReasons, "; "))
	}
	if h.Plan != nil {
		fmt.Fprintf(&b, "- plan: %s\n", plans.Summary(*h.Plan))
		if stage, ok := plans.CurrentStage(*h.Plan); ok && stage.Goal != "" {
			fmt.Fprintf(&b, "  next: %s - %s\n", stage.ID, stage.Goal)
		}
	}
	if len(h.RecentEvents) > 0 {
		b.WriteString("- recent events:\n")
		for _, ev := range h.RecentEvents {
			label := ev.Type
			if ev.Tool != "" {
				label += " " + ev.Tool
			}
			if ev.Status != "" {
				label += " [" + ev.Status + "]"
			}
			fmt.Fprintf(&b, "  - %s %s\n", ev.TS.Format("15:04:05"), label)
		}
	}
	if len(h.Learnings) > 0 {
		b.WriteString("- learnings to reuse:\n")
		for _, item := range h.Learnings {
			fmt.Fprintf(&b, "  - [%.2f] when %s -> %s\n", item.Confidence, item.Trigger, item.Insight)
		}
	}
	if h.PendingDecisions > 0 {
		fmt.Fprintf(&b, "- pending eval decisions: %d\n", h.PendingDecisions)
	}
	b.WriteString("Resume by following the plan's current stage; record new learnings with learning.record.\n")
	return b.String()
}
