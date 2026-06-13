// Package tui provides the Bubbletea-based terminal UI.
//
// Package tui 提供基于 Bubbletea 的终端 UI。
//
// The Model has three responsibilities:
//  1. Render the dashboard (title bar, stats, live events, keymap)
//  2. Receive events from the core pipeline (via the Events() channel)
//  3. Forward Stats() pushes to the dashboard counters
//
// Bubbletea handles all event-loop concerns; we disable its default
// SIGINT handler (tea.WithoutSignalHandler) and let the main goroutine
// own the shutdown flow (see cmd/root.go).
//
// Bubbletea 处理所有事件循环；我们禁用其默认 SIGINT handler
// （tea.WithoutSignalHandler），由 main goroutine 负责关闭流程
// （见 cmd/root.go）。
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// maxLiveEvents caps how many events the TUI remembers for display.
// maxLiveEvents 限制 TUI 保留的最近事件数。
const maxLiveEvents = 200

// liveEvent is a single result entry rendered in the right column.
// liveEvent 是右栏显示的单条结果。
type liveEvent struct {
	when string
	tag  string // "scan" / "cred"
	host string
	port int
	svc  string
	text string
}

// Model is the Bubbletea model for the dashboard.
// Model 是 Bubbletea dashboard 的 model。
type Model struct {
	// Width / height are the terminal size; bubbletea auto-updates them
	// via WindowSizeMsg.
	// Width / height 是终端尺寸；bubbletea 通过 WindowSizeMsg 自动更新。
	width  int
	height int

	// Stats snapshot / 统计快照
	counters types.CountersView
	elapsed  string
	mode     string
	project  string

	// Live events (newest last) / 实时事件（最新在末尾）
	events []liveEvent

	// Quit flag / 退出标志
	quitting bool

	// Final summary printed after bubbletea exits / bubbletea 退出后打印的最终摘要
	finalSummary string
}

// NewModel constructs a fresh dashboard model.
// NewModel 构造一个新的 dashboard model。
func NewModel(cfg *types.Config) Model {
	mode := "scan"
	if cfg != nil {
		mode = string(cfg.Mode)
	}
	project := ""
	if cfg != nil {
		project = cfg.Project
	}
	return Model{
		mode:    mode,
		project: project,
	}
}

// Init is a no-op. Events arrive via Event/Stats pushers.
// Init 是空操作。事件通过 Event/Stats 推入。
func (m Model) Init() tea.Cmd { return nil }

// Update handles bubbletea messages (keypresses, window resize, etc.).
// Update 处理 bubbletea 消息（按键、窗口大小变化等）。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// View renders the dashboard. Returns a single string that lipgloss will
// then lay out.
// View 渲染 dashboard。返回 lipgloss 将布局的单个字符串。
func (m Model) View() string {
	if m.quitting {
		return m.finalSummary + "\n"
	}
	var sb strings.Builder

	// Title bar / 标题栏
	title := fmt.Sprintf(
		"%s FG-QIMEN v0.1 %s project: %s %s mode: %s %s",
		boxTL, boxH, m.project, boxH, m.mode, boxTR,
	)
	sb.WriteString(stTitle.Render(title))
	sb.WriteString("\n")

	// Stats bar / 状态条
	stats := fmt.Sprintf(
		"%s alive=%d ports=%d results=%d creds=%d errors=%d  elapsed=%s",
		symSpinner, m.counters.Alive, m.counters.Ports, m.counters.Results,
		m.counters.Creds, m.counters.Errors, m.elapsed,
	)
	sb.WriteString(stDim.Render(stats))
	sb.WriteString("\n\n")

	// Two columns: stats (left) and events (right). We use lipgloss
	// to lay them out side-by-side.
	// 两栏：左统计，右事件。
	left := m.renderStatsCol()
	right := m.renderEventsCol()
	row := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	sb.WriteString(row)
	sb.WriteString("\n\n")

	// Keymap / 快捷键
	keymap := fmt.Sprintf("[%sq%s] quit  [%sp%s] pause  [%sr%s] resume  [%s?%s] help",
		boxV, boxV, boxV, boxV, boxV, boxV, boxV, boxV)
	sb.WriteString(stDim.Render(keymap))
	sb.WriteString("\n")
	return sb.String()
}

// renderStatsCol builds the left "Targets" column.
// renderStatsCol 构建左侧 "Targets" 列。
func (m Model) renderStatsCol() string {
	body := fmt.Sprintf(
		"Targets\n"+
			"  alive      %d\n"+
			"  ports      %d\n"+
			"  results    %d\n"+
			"  creds      %d\n"+
			"  errors     %d",
		m.counters.Alive, m.counters.Ports, m.counters.Results,
		m.counters.Creds, m.counters.Errors,
	)
	return stBox.Width(28).Render(body)
}

// renderEventsCol builds the right "Live Events" column.
// renderEventsCol 构建右侧 "Live Events" 列。
func (m Model) renderEventsCol() string {
	var sb strings.Builder
	sb.WriteString("Live Events\n")
	start := 0
	if len(m.events) > maxLiveEvents {
		start = len(m.events) - maxLiveEvents
	}
	for _, ev := range m.events[start:] {
		var sym string
		var style lipgloss.Style
		switch ev.tag {
		case "cred":
			sym, style = symWarn, stWarn
		case "err":
			sym, style = symError, stError
		default:
			sym, style = symSuccess, stSuccess
		}
		line := fmt.Sprintf("  %s %s:%d  [%s]  %s", sym, ev.host, ev.port, ev.svc, ev.text)
		sb.WriteString(style.Render(line))
		sb.WriteString("\n")
	}
	return stBox.Render(sb.String())
}
