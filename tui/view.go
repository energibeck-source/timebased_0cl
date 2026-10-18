package tui

import (
    "fmt"
    "strings"

    "github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
    if !m.ready {
        return "Loading..."
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

    // left
    leftStyle := panelStyle
    if m.focused == FocusRequest {
        leftStyle = focusedPanelStyle
    }
    left := leftStyle.
        Width(m.width/2 - 2).
        Render(
            panelTitleStyle.Render("▌Request") + "\n" +
                m.requestInput.View(),
        )

    // right
    rightStyle := panelStyle
    if m.focused == FocusResponse {
        rightStyle = focusedPanelStyle
    }
	var rightTitle, rightBody string
	switch m.paneMode {
	case PaneResponse:
		rightTitle = "Response"
		rightBody = m.responseView.View()
		if rightBody == "" {
			rightBody = dimStyle.Render("(no response yet)")
		}
	
	case PaneLogs:
		rightTitle = "Logs"
		rightBody = m.logsView.View()
	}
	modeIndicator := " [Ctrl+L: Logs] "
    if m.paneMode == PaneLogs {
        modeIndicator = " [Ctrl+L: Response] "
    }
    right := rightStyle.
        Width(m.width - m.width/2 - 2).
        Render(
            panelTitleStyle.Render(rightTitle) +
                dimStyle.Render(modeIndicator) + "\n" +
                rightBody,
        )

    top := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	//clampHeight(lipgloss.JoinHorizontal(lipgloss.Top, left, right), topH)

    // bottom — options
    bottomStyle := panelStyle
    if m.focused == FocusOptions {
        bottomStyle = focusedPanelStyle
    }
    var lines []string
    for i, o := range m.options {
        cursor := "  "
        if i == m.optionIndex && m.focused == FocusOptions {
            cursor = "> "
        }
        lines = append(lines, cursor+formatOption(o))
    }
    bottom := bottomStyle.
        Width(m.width - 2).
        Render(panelTitleStyle.Render("▌Options") + "\n" +
            strings.Join(lines, "\n"))

    status := statusBarStyle.Render(m.status)
    help := helpStyle.Render(
        "Tab: focus • Ctrl+R: send • ↑/↓: options • ←/→: mode • Enter: edit • Ctrl+C: quit",
    )

    return fmt.Sprintf("%s\n%s\n%s\n%s", top, bottom, status, help)
}

func formatOption(o *OptionRow) string {
    if o.Editing {
        return fmt.Sprintf("%-20s [%s_]", o.Label, o.EditBuf)
    }
    switch o.Kind {
    case optBool:
        box := "[ ]"
        if o.Enabled {
            box = "[x]"
        }
        return fmt.Sprintf("%-20s %s", o.Label, box)
    case optDuration:
        return fmt.Sprintf("%-20s %s", o.Label, o.Dur.String())
    case optInt:
        return fmt.Sprintf("%-20s %d", o.Label, o.Int)
    case optMode:
        return fmt.Sprintf("%-20s %s", o.Label, modeName(o.Int))
    }
    return o.Label
}

func modeName(i int) string {
    switch i {
    case 0:
        return "Raw"
    case 1:
        return "RawProbe"
    case 2:
        return "RawWithDelay (0CL)"
	case 3:
		return "ChunkedWithDelay (TE)"
    }
    return "?"
}