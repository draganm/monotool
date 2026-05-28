package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/draganm/monotool/ui/internal/ringbuffer"
)

const (
	defaultRingCapacity = 2000
	tickInterval        = time.Second
)

type imageItem struct {
	Name      string
	ImageName string
	State     string
	Started   time.Time
	Finished  time.Time
	Output    *ringbuffer.Buffer
	Err       error
}

func (it *imageItem) terminal() bool {
	switch it.State {
	case "done", "already pushed", "failed", "cancelled":
		return true
	}
	return false
}

type Model struct {
	items    []*imageItem
	index    map[string]int
	selected int
	viewport viewport.Model
	width    int
	height   int
	done     bool
	failed   bool
	now      time.Time
}

func newModel(names []string) *Model {
	items := make([]*imageItem, len(names))
	index := make(map[string]int, len(names))
	for i, n := range names {
		items[i] = &imageItem{
			Name:   n,
			State:  "waiting",
			Output: ringbuffer.New(defaultRingCapacity),
		}
		index[n] = i
	}
	return &Model{
		items: items,
		index: index,
		now:   time.Now(),
	}
}

func (m *Model) initSizes() {
	leftWidth := m.width * 4 / 10
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth - 4
	if rightWidth < 20 {
		rightWidth = 20
	}
	vpHeight := m.height - 4
	if vpHeight < 5 {
		vpHeight = 5
	}
	m.viewport = viewport.New(rightWidth, vpHeight)
	m.refreshViewport()
}

func (m *Model) Init() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg{Now: t} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.initSizes()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		m.now = msg.Now
		return m, tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg{Now: t} })

	case imageNameMsg:
		if i, ok := m.index[msg.Name]; ok {
			m.items[i].ImageName = msg.ImageName
		}
		return m, nil

	case imageStateMsg:
		if i, ok := m.index[msg.Name]; ok {
			it := m.items[i]
			if it.Started.IsZero() {
				it.Started = msg.When
			}
			it.State = msg.State
			if it.terminal() && it.Finished.IsZero() {
				it.Finished = msg.When
			}
		}
		return m, nil

	case imageOutputMsg:
		if i, ok := m.index[msg.Name]; ok {
			m.items[i].Output.Append(msg.Line)
			if i == m.selected {
				m.refreshViewport()
			}
		}
		return m, nil

	case imageDoneMsg:
		if i, ok := m.index[msg.Name]; ok {
			it := m.items[i]
			it.Err = msg.Err
			if msg.Err != nil {
				it.State = "failed"
				m.failed = true
				it.Output.Append(fmt.Sprintf("ERROR: %v", msg.Err))
			} else if !it.terminal() {
				it.State = "done"
			}
			if it.Finished.IsZero() {
				it.Finished = time.Now()
			}
			if i == m.selected {
				m.refreshViewport()
			}
		}
		return m, nil

	case allDoneMsg:
		m.done = true
		if !m.failed {
			return m, tea.Quit
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if m.done {
			return m, tea.Quit
		}
		return m, nil
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.refreshViewport()
		}
		return m, nil
	case "down", "j":
		if m.selected < len(m.items)-1 {
			m.selected++
			m.refreshViewport()
		}
		return m, nil
	case "g":
		m.viewport.GotoTop()
		return m, nil
	case "G":
		m.viewport.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) refreshViewport() {
	if m.selected < 0 || m.selected >= len(m.items) {
		m.viewport.SetContent("")
		return
	}
	lines := m.items[m.selected].Output.Lines()
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
}

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	title := titleStyle.Render("monotool rollout")

	leftWidth := m.width*4/10 - 4
	if leftWidth < 26 {
		leftWidth = 26
	}

	var rows []string
	for i, it := range m.items {
		row := renderItemRow(it, m.now, leftWidth-2)
		if i == m.selected {
			row = selectedItemStyle.Render(row)
		} else {
			row = itemStyle.Render(row)
		}
		rows = append(rows, row)
	}

	leftBody := strings.Join(rows, "\n")
	leftHeight := m.height - 4

	left := leftPaneStyle.Width(leftWidth).Height(leftHeight).Render(leftBody)

	rightWidth := m.width - lipgloss.Width(left) - 2
	if rightWidth < 20 {
		rightWidth = 20
	}
	right := rightPaneStyle.Width(rightWidth).Height(leftHeight).Render(m.viewport.View())

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	doneCount := 0
	for _, it := range m.items {
		if it.terminal() {
			doneCount++
		}
	}

	footerText := "↑/↓ navigate · PgUp/PgDn scroll · g/G top/bottom"
	if m.done {
		if m.failed {
			footerText += " · q quit (failed)"
		} else {
			footerText += " · q quit"
		}
	} else {
		footerText += " · ctrl-c cancel"
	}
	footerText += fmt.Sprintf("    %d/%d complete", doneCount, len(m.items))
	footer := footerStyle.Render(footerText)

	return lipgloss.JoinVertical(lipgloss.Left, title, body, footer)
}

func renderItemRow(it *imageItem, now time.Time, width int) string {
	name := it.Name
	if len(name) > 14 {
		name = name[:14]
	}
	name = padRight(name, 14)

	state := padRight(it.State, 18)
	state = stateStyle(it.State).Render(state)

	var elapsed string
	switch {
	case it.Started.IsZero():
		elapsed = "  --"
	case it.Finished.IsZero():
		elapsed = formatDuration(now.Sub(it.Started))
	default:
		elapsed = formatDuration(it.Finished.Sub(it.Started))
	}

	row := fmt.Sprintf("%s %s %s", name, state, elapsed)
	if it.ImageName != "" {
		row += "  " + it.ImageName
	}
	return row
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}
