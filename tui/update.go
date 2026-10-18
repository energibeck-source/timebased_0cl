package tui

import (
    "strconv"
    "strings"
    "time"
	"fmt"

    "timebased_0cl/core"
	"github.com/muesli/reflow/wordwrap"
    tea "github.com/charmbracelet/bubbletea"
)

type resultMsg struct {
    res core.Result
}

type logUpdatedMsg struct{}

func (m *Model) sendCmd(payload []byte) tea.Cmd {
    runner := m.runner
    logs := m.logs
    return func() tea.Msg {
        logs.Append(LogInfo, "sending %d bytes (mode=%d, pause=%v, repeat=%d)",
            len(payload), runner.Options.Mode, runner.Options.Pause, runner.Options.Repeat)

        res := runner.Run(payload)

        if res.Err != nil {
            logs.Append(LogError, "request failed: %v, time.Since(start): %v", res.Err, res.TotalDur)
        } else if res.Hit {
            logs.Append(LogHit, "HIT  total=%v (first=%v, second=%v)", res.TotalDur, res.FirstDur, res.SecondDur)
            logs.Append(LogInfo, "time.Since(start): %v", res.TotalDur)
        } else {
            logs.Append(LogInfo, "MISS total=%v (first=%v, second=%v)", res.TotalDur, res.FirstDur, res.SecondDur)
            logs.Append(LogInfo, "time.Since(start): %v", res.TotalDur)
        }
        return resultMsg{res: res}
    }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmds []tea.Cmd

    switch msg := msg.(type) {

    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height
        m.resize()
        m.ready = true
        return m, nil

    case resultMsg:
        m.applyResult(msg.res)
        return m, nil
	case logPollMsg:
		m.refreshLogsView()
		return m, logPollCmd()
    case tea.KeyMsg:
        if m.editingOption() != nil {
            return m.updateEditing(msg)
        }

        switch msg.String() {
        case "ctrl+c", "esc":
            return m, tea.Quit
        case "tab":
            m.focused = (m.focused + 1) % 3
            m.applyFocus()
            return m, nil
        case "shift+tab":
            m.focused = (m.focused + 2) % 3
            m.applyFocus()
            return m, nil
        case "ctrl+r":
            m.syncOptionsToRunner()
            m.status = "Sending..."
            return m, m.sendCmd([]byte(m.requestInput.Value()))
		case "ctrl+l":
			if m.paneMode == PaneResponse {
				m.paneMode = PaneLogs
				m.focused = FocusResponse
			} else {
				m.paneMode = PaneResponse
			}
			m.refreshLogsView()
			return m, nil
		case "ctrl+k":
			m.logs.Clear()
			m.refreshLogsView()
			return m, nil
        case "up", "k":
            if m.focused == FocusOptions && m.optionIndex > 0 {
                m.optionIndex--
                return m, nil
            }
        case "down", "j":
            if m.focused == FocusOptions && m.optionIndex < len(m.options)-1 {
                m.optionIndex++
                return m, nil
            }
        case " ":
            if m.focused == FocusOptions {
                m.toggleBool()
                return m, nil
            }
        case "enter":
            if m.focused == FocusOptions {
                m.startEditing()
                return m, nil
            }
        case "left":
            if m.focused == FocusOptions {
                m.cycleMode(-1)
                return m, nil
            }
        case "right":
            if m.focused == FocusOptions {
                m.cycleMode(+1)
                return m, nil
            }
        }
    }

    var cmd tea.Cmd
    switch m.focused {
    case FocusRequest:
        m.requestInput, cmd = m.requestInput.Update(msg)
        cmds = append(cmds, cmd)
	case FocusResponse:
    if m.paneMode == PaneResponse {
        m.responseView, cmd = m.responseView.Update(msg)
        cmds = append(cmds, cmd)
    } else if m.paneMode == PaneLogs {
        m.logsView, cmd = m.logsView.Update(msg)
        cmds = append(cmds, cmd)
        m.logsFollow = m.logsView.AtBottom()
    }
}
    return m, tea.Batch(cmds...)
}

func (m *Model) editingOption() *OptionRow {
    if m.focused != FocusOptions {
        return nil
    }
    o := m.options[m.optionIndex]
    if o.Editing {
        return o
    }
    return nil
}

func (m *Model) updateEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    o := m.options[m.optionIndex]
    switch msg.String() {
    case "esc":
        o.Editing = false
        o.EditBuf = ""
    case "enter":
        m.commitEdit(o)
        o.Editing = false
        o.EditBuf = ""
    case "backspace":
        if len(o.EditBuf) > 0 {
            o.EditBuf = o.EditBuf[:len(o.EditBuf)-1]
        }
    default:
        if len(msg.String()) == 1 {
            o.EditBuf += msg.String()
        }
    }
    return m, nil
}

func (m *Model) startEditing() {
    o := m.options[m.optionIndex]
    switch o.Kind {
    case optDuration:
        o.EditBuf = o.Dur.String()
        o.Editing = true
    case optInt:
        o.EditBuf = strconv.Itoa(o.Int)
        o.Editing = true
    }
}

func (m *Model) commitEdit(o *OptionRow) {
    switch o.Kind {
    case optDuration:
        if d, err := time.ParseDuration(o.EditBuf); err == nil {
            o.Dur = d
        }
    case optInt:
        if n, err := strconv.Atoi(o.EditBuf); err == nil {
            o.Int = n
        }
    }
}

func (m *Model) toggleBool() {
    o := m.options[m.optionIndex]
    if o.Kind == optBool {
        o.Enabled = !o.Enabled
    }
}

func (m *Model) cycleMode(dir int) {
    o := m.options[m.optionIndex]
    if o.Kind != optMode {
        return
    }
    n := 4 // ModeRaw, ModeRawProbe, ModeRawWithDelay, ModeChunkedWithDelay
    o.Int = (o.Int + dir + n) % n
}

func (m *Model) syncOptionsToRunner() {
    o := core.SendOptions{}
    for _, row := range m.options {
        switch row.Label {
        case "Mode":
            o.Mode = core.Mode(row.Int)
        case "Pause":
            o.Pause = row.Dur
        case "Repeat":
            o.Repeat = row.Int
        case "Threshold":
            o.TimeThreshold = row.Dur
        case "Probe hard limit":
            o.ProbeHardLimit = row.Dur
		case "Follow-up request":
    		o.FollowUp = row.Enabled
		case "Add garbage data":
            o.Obfuscate = row.Enabled
        case "Update Content-Length":
            o.UpdateContentLength = row.Enabled
        }
    }
    m.runner.Options = o
}

func (m *Model) applyResult(res core.Result) {
    //m.lastResult = res
    //m.hasResult = true

    var sb strings.Builder

    if len(res.First) > 0 {
        sb.WriteString("── 0CL: before-body ──\n")
        sb.Write(res.First)
        sb.WriteString("\n")
    }
    if len(res.Second) > 0 {
        sb.WriteString("── 0CL: after-body ──\n")
        sb.Write(res.Second)
        sb.WriteString("\n")
    }

    if res.FollowUpErr != nil {
        sb.WriteString("\n── follow-up ERROR ──\n")
        sb.WriteString(res.FollowUpErr.Error())
    } else if res.FollowUp != nil {
        sb.WriteString(fmt.Sprintf("\n── follow-up ── status=%d dur=%v\n",
            res.FollowUpStatus, res.FollowUpDur))
        sb.Write(res.FollowUp)
    }

    if res.BaselineDetails != "" {
        sb.WriteString(fmt.Sprintf("\n\n── baseline ── matched=%v: %s\n",
            res.BaselineMatched, res.BaselineDetails))
    }

    if sb.Len() == 0 {
        sb.WriteString("(empty response)")
    }

    w := m.responseView.Width
    if w <= 0 {
        w = 80
    }
    m.responseView.SetContent(wordwrap.String(sb.String(), w))
    m.responseView.GotoTop()

    // status bar
    m.lastDur = res.TotalDur
    switch {
    case res.Err != nil:
        m.status = "Error: " + res.Err.Error()
    case res.BaselineMatched:
        m.status = fmt.Sprintf("SMUGGLED? 0CL=%v follow-up=%d (%v) — %s",
            res.TotalDur, res.FollowUpStatus, res.FollowUpDur, res.BaselineDetails)
    case res.Hit:
        m.status = fmt.Sprintf("HIT 0CL=%v, follow-up=%d", res.TotalDur, res.FollowUpStatus)
    default:
        m.status = fmt.Sprintf("MISS 0CL=%v, follow-up=%d", res.TotalDur, res.FollowUpStatus)
    }
}

/*
func (m *Model) applyResult(res core.Result) {
    var sb strings.Builder
    if len(res.First) > 0 {
        sb.WriteString("── first response ──\n")
        sb.Write(res.First)
        sb.WriteString("\n")
    }
    if len(res.Second) > 0 {
        sb.WriteString("── second response ──\n")
        sb.Write(res.Second)
    }
    if sb.Len() == 0 {
        sb.WriteString("(empty response)")
    }
    m.responseView.SetContent(sb.String())
    m.responseView.GotoTop()

    m.lastDur = res.TotalDur
    switch {
    case res.Err != nil:
        m.status = "Error: " + res.Err.Error()
    case res.Hit:
        m.status = "HIT  " + res.TotalDur.String()
    default:
        m.status = "MISS " + res.TotalDur.String()
    }
}
*/
func (m *Model) resize() {
    if m.width == 0 || m.height == 0 {
        return
    }

    const (
        panelBorder = 2
        panelTitle  = 1
        statusLines = 1
        helpLines   = 1
    )

    bottomH := panelBorder + panelTitle + len(m.options)
    topH := m.height - bottomH - statusLines - helpLines
    if topH < 6 {
        topH = 6
    }

    innerH := topH - panelBorder - panelTitle
    if innerH < 1 {
        innerH = 1
    }

    innerLeftW := m.width/2 - 4
    innerRightW := m.width - m.width/2 - 4
    if innerLeftW < 1 {
        innerLeftW = 1
    }
    if innerRightW < 1 {
        innerRightW = 1
    }
	//req
    m.requestInput.SetWidth(innerLeftW)
    m.requestInput.SetHeight(innerH)
	//logs
	m.logsView.Width = innerLeftW
    m.logsView.Height = innerH
    m.refreshLogsView() //questionable
	//resp
    m.responseView.Width = innerRightW
    m.responseView.Height = innerH
}

func (m *Model) applyFocus() {
	if m.focused == FocusRequest {
		m.requestInput.Focus()
	} else {
		m.requestInput.Blur()
	}
}