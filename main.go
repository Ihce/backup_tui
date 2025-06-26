package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	serviceName = "nightly-onedrive-backup.service"
	timerName   = "nightly-onedrive-backup.timer"
)

type unitItem struct {
	title, desc string
}
func (i unitItem) Title() string       { return i.title }
func (i unitItem) Description() string { return i.desc }
func (i unitItem) FilterValue() string { return i.title }

type tickMsg struct{}
type errMsg struct{ err error }
type outputMsg struct {
	tag string
	out string
}

type model struct {
	list        list.Model
	spin        spinner.Model
	status      string
	lastOutput  string
	loading     bool
	err         error
	cancelFunc  context.CancelFunc
}

func newModel() model {
	items := []list.Item{
		unitItem{timerName,   "Enable/Disable, View status, Run Now"},
		unitItem{serviceName, "Run Now, View logs"},
	}
	l := list.New(items, list.NewDefaultDelegate(), 50, 10)
	l.Title = "OneDrive Backup – systemd units"
	l.SetShowStatusBar(false)
	l.Styles.Title = lipgloss.NewStyle().Foreground(lipgloss.Color("#00afff")).Bold(true)

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	return model{list: l, spin: spin, status: "Ready"}
}

func main() {
	if _, err := exec.LookPath("systemctl"); err != nil {
		fmt.Fprintln(os.Stderr, "systemctl not found")
		os.Exit(1)
	}
	p := tea.NewProgram(newModel())
	if err := p.Start(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

// ---------- tea.Model interface ----------

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.cancelFunc != nil {
				m.cancelFunc()
			}
			return m, tea.Quit
		case "enter":
			selected := m.list.SelectedItem().(unitItem)
			return m, m.asyncRun("status", selected.title)
		case "r":
			selected := m.list.SelectedItem().(unitItem)
			return m, m.asyncRun("start", selected.title)
		case " ":
			selected := m.list.SelectedItem().(unitItem)
			action := "enable"
			if strings.HasSuffix(selected.title, ".timer") {
				action = "toggle"
			}
			return m, m.asyncRun(action, selected.title)
		case "l":
			selected := m.list.SelectedItem().(unitItem)
			return m, m.asyncRun("logs", selected.title)
		}
	case outputMsg:
		m.loading = false
		m.status = fmt.Sprintf("[%s] %s", msg.tag, firstLine(msg.out))
		m.lastOutput = msg.out
		if m.cancelFunc != nil {
			m.cancelFunc()
		}
	case errMsg:
		m.loading = false
		m.err = msg.err
		m.status = "Error: " + msg.err.Error()
		if m.cancelFunc != nil {
			m.cancelFunc()
		}
	case tickMsg:
		m.spin, _ = m.spin.Update(msg)
		cmds = append(cmds, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg{} }))
	}

	if m.loading {
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		cmds = append(cmds, cmd)
	}

	var cmd list.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.list.View())
	if m.loading {
		b.WriteRune('\n')
		b.WriteString(m.spin.View() + " " + m.status)
	} else {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafd7")).Render(m.status))
	}
	if m.lastOutput != "" {
		b.WriteString("\n\n" + lipgloss.NewStyle().Faint(true).Render(trimLines(m.lastOutput, 20)))
	}
	b.WriteString("\n\n[↑/↓] navigate • [space] enable/disable • [r] run • [enter] status • [l] logs • [q] quit\n")
	return b.String()
}

// ---------- helpers ----------

func (m *model) asyncRun(tag, unit string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel
	m.loading = true
	m.status = tag + " " + unit
	return tea.Batch(func() tea.Msg { return tickMsg{} }, func() tea.Msg {
		var cmd *exec.Cmd
		switch tag {
		case "status":
			cmd = exec.CommandContext(ctx, "systemctl", "status", "--no-pager", unit)
		case "start":
			cmd = exec.CommandContext(ctx, "systemctl", "start", unit)
		case "toggle":
			// enable --now if disabled, disable if enabled
			stateOut, _ := exec.Command("systemctl", "is-enabled", unit).Output()
			if strings.TrimSpace(string(stateOut)) == "enabled" {
				cmd = exec.CommandContext(ctx, "systemctl", "disable", "--now", unit)
			} else {
				cmd = exec.CommandContext(ctx, "systemctl", "enable", "--now", unit)
			}
		case "enable":
			cmd = exec.CommandContext(ctx, "systemctl", "enable", "--now", unit)
		case "logs":
			cmd = exec.CommandContext(ctx, "journalctl", "-u", unit, "-n", "50", "--no-pager")
		default:
			return errMsg{fmt.Errorf("unknown action")}
		}
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		if err != nil {
			return errMsg{err}
		}
		return outputMsg{tag: tag, out: buf.String()}
	})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
func trimLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

type tea = bubbletea
