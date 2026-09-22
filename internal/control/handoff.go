package control

import (
	"context"
	"fmt"
	"strings"
	"time"

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

	if plan, ok, planErr := plans.LatestErr(root); ok {
		handoff.Plan = &plan
	} else if planErr != nil {
		handoff.Notes = append(handoff.Notes, "latest plan unreadable: "+planErr.Error())
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
	// eval_decision events carry no session id (they come from decision
	// files), so they must be counted from the global window.
	end := time.Now().UTC()
	if session.EndedAt != nil && session.EndedAt.Before(end) {
		end = *session.EndedAt
	}
	if globalEvents, derr := store.EventsBetween(ctx, session.CreatedAt, end, 10000); derr == nil {
		for _, ev := range globalEvents {
			if ev.Type == "eval_decision" {
				handoff.PendingDecisions++
			}
		}
	} else {
		handoff.Notes = append(handoff.Notes, "decision history unreadable: "+derr.Error())
	}
	// Older records may still carry a session id; count those too.
	if scopedEvents, derr := store.Events(ctx, sessionID, 10000); derr == nil {
		for _, ev := range scopedEvents {
			if ev.Type == "eval_decision" {
				handoff.PendingDecisions++
			}
		}
	} else {
		handoff.Notes = append(handoff.Notes, "scoped decision history unreadable: "+derr.Error())
	}
	return handoff, nil
}

// RenderHandoff formats a handoff as a paste-ready markdown block.
func RenderHandoff(h Handoff) string {
	var b strings.Builder
	line := func(value string) string {
		return strings.Join(strings.Fields(value), " ")
	}
	b.WriteString("## Session handoff\n")
	fmt.Fprintf(&b, "- session: %s (%s, risk %.2f)\n", line(h.Session.ID), line(h.Session.Status), h.Session.RiskScore)
	if h.Session.Task != "" {
		fmt.Fprintf(&b, "- task: %s\n", line(h.Session.Task))
	}
	if h.Session.Worktree != "" {
		fmt.Fprintf(&b, "- worktree: %s\n", line(h.Session.Worktree))
	}
	if len(h.Session.RiskReasons) > 0 {
		fmt.Fprintf(&b, "- risk reasons: %s\n", line(strings.Join(h.Session.RiskReasons, "; ")))
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
			fmt.Fprintf(&b, "  - [%.2f] when %s -> %s\n", item.Confidence, line(item.Trigger), line(item.Insight))
		}
	}
	if h.PendingDecisions > 0 {
		fmt.Fprintf(&b, "- pending eval decisions: %d\n", h.PendingDecisions)
	}
	if len(h.Notes) > 0 {
		b.WriteString("- warnings (handoff degraded):\n")
		for _, note := range h.Notes {
			fmt.Fprintf(&b, "  - %s\n", note)
		}
	}
	b.WriteString("Resume by following the plan's current stage; record new learnings with learning.record.\n")
	return b.String()
}
