package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hackathonkit.local/agentkit/internal/control"
)

type refreshMsg struct{}

// loadedMsg carries query results back to the update loop.
type loadedMsg struct {
	sessions []control.Session
	events   []control.Event
	err      error
	selected int
}

type tuiModel struct {
	store     *control.Store
	sessions  []control.Session
	events    []control.Event
	selected  int
	width     int
	height    int
	err       error
	ingestErr error
	status    string
}

func cmdTUI(args []string) error {
	root := repoRoot()
	store, err := openStore(root)
	if err != nil {
		return err
	}
	defer store.Close()
	var ingestErr error
	if _, err := store.Ingest(context.Background(), root); err != nil {
		ingestErr = err
	}
	program := tea.NewProgram(newTUIModel(store, ingestErr), tea.WithAltScreen())
	_, err = program.Run()
	return err
}

func newTUIModel(store *control.Store, ingestErr error) tuiModel {
	return tuiModel{store: store, ingestErr: ingestErr}
}

// loadCmd runs the store queries off the update goroutine with a deadline.
func loadCmd(store *control.Store, selected int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sessions, err := store.Sessions(ctx, 50)
		if err != nil {
			return loadedMsg{err: err}
		}
		if selected >= len(sessions) {
			selected = len(sessions) - 1
		}
		if selected < 0 {
			selected = 0
		}
		events := []control.Event{}
		if len(sessions) > 0 {
			events, err = store.Events(ctx, sessions[selected].ID, 20)
		}
		return loadedMsg{sessions: sessions, events: events, err: err, selected: selected}
	}
}

func (m *tuiModel) applyLoaded(msg loadedMsg) {
	if msg.sessions == nil {
		// The sessions query itself failed: keep the previous view.
		m.err = msg.err
		return
	}
	m.err = msg.err // may be an events-only failure
	m.sessions = msg.sessions
	m.selected = msg.selected
	m.events = msg.events
	m.status = fmt.Sprintf("%d session(s) - refreshed %s", len(m.sessions), time.Now().Format("15:04:05"))
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(tickCmd(), loadCmd(m.store, m.selected))
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return refreshMsg{} })
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				return m, loadCmd(m.store, m.selected)
			}
		case "down", "j":
			if m.selected < len(m.sessions)-1 {
				m.selected++
				return m, loadCmd(m.store, m.selected)
			}
		case "r":
			return m, loadCmd(m.store, m.selected)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case loadedMsg:
		m.applyLoaded(msg)
	case refreshMsg:
		return m, tea.Batch(tickCmd(), loadCmd(m.store, m.selected))
	}
	return m, nil
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	badStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

func (m tuiModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Hackathon Agent Kit control plane"))
	b.WriteString(dimStyle.Render("   q quit · j/k move · r refresh"))
	b.WriteString("\n\n")

	if m.ingestErr != nil {
		b.WriteString(warnStyle.Render("ingest failed: "+m.ingestErr.Error()) + "\n")
	}
	if m.err != nil {
		b.WriteString(badStyle.Render("error: "+m.err.Error()) + "\n")
		return b.String()
	}
	if len(m.sessions) == 0 {
		b.WriteString("No sessions recorded yet.\n")
		b.WriteString(dimStyle.Render("Run a Codex session with the plugin, or: hackathonkit-ctl ingest") + "\n\n")
		b.WriteString(dimStyle.Render(m.status))
		return b.String()
	}

	width := m.width
	if width <= 0 {
		width = 100
	}
	// Keep the panes inside the terminal: apply the minimum widths only when
	// the terminal is wide enough for them.
	leftWidth := width / 2
	rightWidth := width - leftWidth - 4
	if width >= 54 {
		if leftWidth < 30 {
			leftWidth = 30
		}
		rightWidth = width - leftWidth - 4
		if rightWidth < 20 {
			rightWidth = 20
		}
		leftWidth = width - rightWidth - 4
	} else {
		rightWidth = 0
	}

	var left strings.Builder
	left.WriteString(dimStyle.Render("SESSIONS") + "\n")
	for i, sess := range m.sessions {
		line := fmt.Sprintf("%-20s %-7s risk %-5.1f %s", truncate(sess.ID, 20), sess.Status, sess.RiskScore, sess.UpdatedAt.Format("15:04"))
		if i == m.selected {
			left.WriteString(selectedStyle.Render("> "+line) + "\n")
			continue
		}
		left.WriteString("  " + line + "\n")
	}

	// Negative limits would panic in truncate on very narrow terminals.
	rightLimit := func(n int) int {
		if n < 1 {
			return 1
		}
		return n
	}
	var right strings.Builder
	selected := m.sessions[m.selected]
	right.WriteString(dimStyle.Render("SESSION "+selected.ID) + "\n")
	if selected.Task != "" {
		right.WriteString("task: " + truncate(selected.Task, rightLimit(rightWidth-6)) + "\n")
	}
	if selected.Worktree != "" {
		right.WriteString(dimStyle.Render("worktree: "+truncate(selected.Worktree, rightLimit(rightWidth-10))) + "\n")
	}
	right.WriteString("risk: " + riskStyle(selected.RiskScore).Render(fmt.Sprintf("%.1f", selected.RiskScore)))
	if len(selected.RiskReasons) > 0 {
		right.WriteString(dimStyle.Render(" (" + strings.Join(selected.RiskReasons, "; ") + ")"))
	}
	right.WriteString("\n\n")
	right.WriteString(dimStyle.Render("EVENTS") + "\n")
	if len(m.events) == 0 {
		right.WriteString(dimStyle.Render("no events yet") + "\n")
	}
	for _, ev := range m.events {
		label := ev.Type
		if ev.Tool != "" {
			label += " " + ev.Tool
		}
		if ev.Status != "" {
			label += " [" + ev.Status + "]"
		}
		style := dimStyle
		if strings.EqualFold(ev.Status, "error") {
			style = badStyle
		} else if ev.Status != "" {
			style = okStyle
		}
		right.WriteString(style.Render(fmt.Sprintf("%s %s", ev.TS.Format("15:04:05"), truncate(label, rightLimit(rightWidth-10)))) + "\n")
	}

	body := lipgloss.NewStyle().Width(leftWidth).Render(left.String())
	if rightWidth > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(leftWidth).Render(left.String()),
			lipgloss.NewStyle().Width(rightWidth).Render(right.String()),
		)
	}
	b.WriteString(body)
	b.WriteString("\n" + dimStyle.Render(m.status))
	return b.String()
}

func riskStyle(score float64) lipgloss.Style {
	switch {
	case score >= 50:
		return badStyle
	case score >= 20:
		return warnStyle
	default:
		return okStyle
	}
}
