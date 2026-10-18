package tui

import (
    "time"
	"strings"
	"io"

    "timebased_0cl/core"
    "github.com/charmbracelet/bubbles/textarea"
    "github.com/charmbracelet/bubbles/viewport"
    tea "github.com/charmbracelet/bubbletea"
)

type FocusedPanel int

const (
    FocusRequest FocusedPanel = iota
    FocusResponse
    FocusOptions
)

type OptionRow struct {
    Label    string
    Kind     optionKind
    Enabled  bool
    Dur      time.Duration
    Int      int
    Editing  bool
    EditBuf  string
}

type optionKind int

const (
    optBool optionKind = iota
    optDuration
    optInt
    optMode
)

type PaneMode int

const (
    PaneResponse PaneMode = iota
    PaneLogs
)

type Model struct {
    runner *core.Runner
	//left
    requestInput textarea.Model
	//right
	paneMode     PaneMode
    responseView viewport.Model
	logsView     viewport.Model
    logs         *LogBuffer
	logsFollow bool
	//bot
    options     []*OptionRow
    optionIndex int
    focused     FocusedPanel

    width, height int
    ready         bool

    status   string
    lastDur  time.Duration
	program *tea.Program
}

func New(runner *core.Runner, initialReq []byte, logOut io.Writer) Model {
    ta := textarea.New()
	ta.SetValue(strings.ReplaceAll(string(initialReq), "\r\n", "\n"))
    ta.CharLimit = 0
    ta.ShowLineNumbers = true
    ta.Focus()

	vpResp := viewport.New(0, 0)
    vpLogs := viewport.New(0, 0)

	lb := NewLogBuffer(2000, logOut)

    m := Model{
        runner:       runner,
        requestInput: ta,
        responseView: vpResp,
		logsView:     vpLogs,
		logs:		  lb,
		paneMode:	  PaneResponse,
        focused:      FocusRequest,
        status:       "Ready. Ctrl+R — send",
        options:      buildOptionRows(runner.Options),
		logsFollow: true,
    }

	lb.SetOnChange(func() {
        m.onLogAppend()
    })

    m.logs.Append(LogInfo, "session started")
    return m
}

func (m *Model) onLogAppend() {
    m.refreshLogsView()
}

func (m *Model) refreshLogsView() {
    atBottom := m.logsView.AtBottom()
    m.logsView.SetContent(m.logs.Render(m.logsView.Width))
    if m.logsFollow || atBottom {
        m.logsView.GotoBottom()
    }
}

func buildOptionRows(o core.SendOptions) []*OptionRow {
    return []*OptionRow{
        {Label: "Mode", Kind: optMode, Int: int(o.Mode)},
        {Label: "Pause", Kind: optDuration, Dur: o.Pause},
        {Label: "Repeat", Kind: optInt, Int: o.Repeat},
        {Label: "Threshold", Kind: optDuration, Dur: o.TimeThreshold},
        {Label: "Probe hard limit", Kind: optDuration, Dur: o.ProbeHardLimit},
        {Label: "Follow-up request", Kind: optBool, Enabled: o.FollowUp},
		{Label: "Add garbage data", Kind: optBool, Enabled: o.Obfuscate},
        {Label: "Update Content-Length", Kind: optBool, Enabled: o.UpdateContentLength},
    }
}
type logPollMsg struct{}

func logPollCmd() tea.Cmd {
    return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
        return logPollMsg{}
    })
}

func (m Model) Init() tea.Cmd {
    return tea.Batch(textarea.Blink, logPollCmd())
}

func (m *Model) SetProgram(p *tea.Program) {
    m.program = p
}