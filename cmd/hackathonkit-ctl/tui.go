package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hackathonkit.local/agentkit/internal/control"
)

type refreshMsg struct{}

type tuiModel struct {
	store    *control.Store
	root     string
	sessions []control.Session
	events   []control.Event
	selected int
	width    int
	height   int
	err      error
	status   string
}

func cmdTUI(args []string) error {
	root := repoRoot()
	store, err := openStore(root)
	if err != nil {
		return err
	}
	defer store.Close()
	if _, err := store.Ingest(context.Background(), root); err != nil {
		fmt.Fprintln(os.Stderr, "ingest:", err)
	}
	program := tea.NewProgram(newTUIModel(store, root), tea.WithAltScreen())
	_, err = program.Run()
	return err
}

func newTUIModel(store *control.Store, root string) tuiModel {
	model := tuiModel{store: store, root: root}
	model.load()
	return model
}

func (m *tuiModel) load() {
	ctx := context.Background()
	sessions, err := m.store.Sessions(ctx, 50)
	if err != nil {
		m.err = err
		return
	}
	m.err = nil
	m.sessions = sessions
	if m.selected >= len(sessions) {
		m.selected = len(sessions) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	m.events = nil
	if len(sessions) > 0 {
		if events, err := m.store.Events(ctx, sessions[m.selected].ID, 20); err == nil {
			m.events = events
		}
	}
	m.status = fmt.Sprintf("%d session(s) - refreshed %s", len(sessions), time.Now().Format("15:04:05"))
}

func (m tuiModel) Init() tea.Cmd {
	return tickCmd()
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
				m.load()
			}
		case "down", "j":
			if m.selected < len(m.sessions)-1 {
				m.selected++
				m.load()
			}
		case "r":
			m.load()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case refreshMsg:
		m.load()
		return m, tickCmd()
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
	leftWidth := width / 2
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := width - leftWidth - 4
	if rightWidth < 20 {
		rightWidth = 20
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

	var right strings.Builder
	selected := m.sessions[m.selected]
	right.WriteString(dimStyle.Render("SESSION "+selected.ID) + "\n")
	if selected.Task != "" {
		right.WriteString("task: " + truncate(selected.Task, rightWidth-6) + "\n")
	}
	if selected.Worktree != "" {
		right.WriteString(dimStyle.Render("worktree: "+truncate(selected.Worktree, rightWidth-10)) + "\n")
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
		right.WriteString(style.Render(fmt.Sprintf("%s %s", ev.TS.Format("15:04:05"), truncate(label, rightWidth-10))) + "\n")
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).Render(left.String()),
		lipgloss.NewStyle().Width(rightWidth).Render(right.String()),
	)
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
