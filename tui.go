package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	styleHeader   = lipgloss.NewStyle().Bold(true)
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
)

// linesPerItem is the height of one session in the menu: title + details.
const linesPerItem = 2

type model struct {
	dir     string
	all     []Session // sessions with at least one message, newest first
	visible []Session // all, filtered by query
	cursor  int
	offset  int // index of the first visible item
	query   string
	confirm bool // waiting for y/n to delete visible[cursor]
	status  string
	width   int
	height  int
	chosen  *Session
	header  string // replaces the directory in the title line
	global  bool   // sessions come from several projects: show and delete by their Cwd
}

func newModel(dir string, sessions []Session) *model {
	m := &model{dir: dir, width: 80, height: 24}
	for _, s := range sessions {
		if s.Messages > 0 {
			m.all = append(m.all, s)
		}
	}
	m.filter()
	return m
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) filter() {
	m.visible = m.visible[:0]
	words := strings.Fields(strings.ToLower(m.query))
	for _, s := range m.all {
		hay := strings.ToLower(s.Title + " " + s.FirstPrompt + " " + s.Branch + " " + s.ID)
		match := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				match = false
				break
			}
		}
		if match {
			m.visible = append(m.visible, s)
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.scroll()
}

// pageSize is how many sessions fit on screen between header and footer.
func (m *model) pageSize() int {
	n := (m.height - 5) / linesPerItem
	if n < 1 {
		n = 1
	}
	return n
}

func (m *model) scroll() {
	ps := m.pageSize()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+ps {
		m.offset = m.cursor - ps + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *model) move(d int) {
	m.cursor += d
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.visible)-1 {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.scroll()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scroll()
	case tea.KeyMsg:
		if m.confirm {
			return m.updateConfirm(msg)
		}
		m.status = ""
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			if m.query != "" {
				m.query = ""
				m.filter()
				return m, nil
			}
			return m, tea.Quit
		case tea.KeyEnter:
			if len(m.visible) > 0 {
				s := m.visible[m.cursor]
				m.chosen = &s
				return m, tea.Quit
			}
		case tea.KeyUp, tea.KeyCtrlP:
			m.move(-1)
		case tea.KeyDown, tea.KeyCtrlN:
			m.move(1)
		case tea.KeyPgUp:
			m.move(-m.pageSize())
		case tea.KeyPgDown:
			m.move(m.pageSize())
		case tea.KeyHome:
			m.move(-len(m.visible))
		case tea.KeyEnd:
			m.move(len(m.visible))
		case tea.KeyDelete:
			m.askDelete()
		case tea.KeyBackspace:
			// On a Mac keyboard the "delete" key sends backspace: with an
			// empty search it deletes the session, otherwise it edits the search.
			if m.query == "" {
				m.askDelete()
			} else {
				r := []rune(m.query)
				m.query = string(r[:len(r)-1])
				m.filter()
			}
		case tea.KeyRunes, tea.KeySpace:
			m.query += string(msg.Runes)
			m.cursor = 0
			m.filter()
		}
	}
	return m, nil
}

func (m *model) askDelete() {
	if len(m.visible) > 0 {
		m.confirm = true
	}
}

func (m *model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlC:
		return m, tea.Quit
	case msg.String() == "y" || msg.String() == "Y" || msg.Type == tea.KeyEnter:
		s := m.visible[m.cursor]
		dir := m.dir
		if m.global && s.Cwd != "" {
			dir = s.Cwd
		}
		if err := deleteSession(dir, s.ID); err != nil {
			m.status = "delete failed: " + err.Error()
		} else {
			for i := range m.all {
				if m.all[i].ID == s.ID {
					m.all = append(m.all[:i], m.all[i+1:]...)
					break
				}
			}
			m.status = "deleted: " + s.Title
			m.filter()
		}
		m.confirm = false
	case msg.String() == "n" || msg.String() == "N" || msg.Type == tea.KeyEsc:
		m.confirm = false
	}
	return m, nil
}

func (m *model) View() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("Resume Session") + styleDim.Render("  "+firstNonEmpty(m.header, m.dir)) + "\n")
	if m.query != "" {
		b.WriteString("⌕ " + m.query + "\n")
	} else {
		b.WriteString(styleDim.Render("⌕ type to search") + "\n")
	}
	b.WriteString("\n")

	if len(m.visible) == 0 {
		if len(m.all) == 0 {
			b.WriteString(styleDim.Render("  no sessions in this directory") + "\n")
		} else {
			b.WriteString(styleDim.Render("  no sessions match") + "\n")
		}
	}
	end := m.offset + m.pageSize()
	if end > len(m.visible) {
		end = len(m.visible)
	}
	now := time.Now()
	for i := m.offset; i < end; i++ {
		s := m.visible[i]
		title := truncate(firstNonEmpty(s.Title, s.ID), m.width-4)
		details := ago(now, s.Updated) + " · " + plural(s.Messages, "message")
		if s.Branch != "" && s.Branch != "HEAD" {
			details += " · " + s.Branch
		}
		if m.global && s.Cwd != "" {
			details += " · " + s.Cwd
		}
		if i == m.cursor {
			b.WriteString(styleSelected.Render("❯ "+title) + "\n")
		} else {
			b.WriteString("  " + title + "\n")
		}
		b.WriteString("  " + styleDim.Render(truncate(details, m.width-4)) + "\n")
	}

	b.WriteString("\n")
	switch {
	case m.confirm:
		b.WriteString(styleWarn.Render(fmt.Sprintf("Delete %q from history? (y/n)", truncate(m.visible[m.cursor].Title, 50))))
	case m.status != "":
		b.WriteString(styleDim.Render(m.status))
	default:
		b.WriteString(styleDim.Render(fmt.Sprintf("%d sessions · ↑↓ select · enter resume · del delete · esc quit", len(m.visible))))
	}
	return b.String()
}

func truncate(s string, n int) string {
	if n < 4 {
		n = 4
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case t.IsZero():
		return "unknown"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " ago"
	default:
		return t.Local().Format("2006-01-02")
	}
}
