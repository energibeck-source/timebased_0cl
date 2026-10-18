package tui

import (
    "fmt"
    "strings"
    "sync"
    "time"
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

type LogLevel int

const (
    LogInfo LogLevel = iota
    LogWarn
    LogError
    LogHit
)
func (l LogLevel) String() string {
    switch l {
    case LogInfo:
        return "INFO"
    case LogWarn:
        return "WARN"
    case LogError:
        return "ERROR"
    case LogHit:
        return "HIT"
    }
    return "?"
}
type LogLine struct {
    At    time.Time
    Level LogLevel
    Text  string
}

type LogBuffer struct {
    mu       sync.Mutex
    lines    []LogLine
    cap      int

    onChange func()

	out 	io.Writer
}

func NewLogBuffer(capacity int, out io.Writer) *LogBuffer {
    return &LogBuffer{
        lines: make([]LogLine, 0, capacity),
        cap:   capacity,
        out:   out,
    }
}

func (lb *LogBuffer) SetOnChange(fn func()) {
    lb.mu.Lock()
    defer lb.mu.Unlock()
    lb.onChange = fn
}

func (lb *LogBuffer) Append(level LogLevel, format string, args ...any) {
    line := LogLine{
        At:    time.Now(),
        Level: level,
        Text:  fmt.Sprintf(format, args...),
    }

    lb.mu.Lock()
    if len(lb.lines) >= lb.cap {
        drop := lb.cap / 4
        if drop < 1 {
            drop = 1
        }
        lb.lines = append(lb.lines[:0], lb.lines[drop:]...)
    }
    lb.lines = append(lb.lines, line)

    if lb.out != nil {
        fmt.Fprintf(lb.out, "%s\t%s\t%s\n",
            line.At.Format("2006-01-02T15:04:05.000"),
            level.String(),
            line.Text,
        )
    }

    cb := lb.onChange
    lb.mu.Unlock()

    if cb != nil {
        cb()
    }
}

func (lb *LogBuffer) Snapshot() []LogLine {
    lb.mu.Lock()
    defer lb.mu.Unlock()
    out := make([]LogLine, len(lb.lines))
    copy(out, lb.lines)
    return out
}

var logStyles = map[LogLevel]lipgloss.Style{
    LogInfo:  lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
    LogWarn:  lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
    LogError: lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
    LogHit:   lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true),
}

func (lb *LogBuffer) Render(width int) string {
    lines := lb.Snapshot()
    var sb strings.Builder

    tsSample := "15:04:05.000"
    prefixW := len(tsSample) + 2
    textW := width - prefixW
    if textW < 10 {
        textW = 10
    }

    for _, l := range lines {
        ts := dimStyle.Render(l.At.Format("15:04:05.000"))
        wrapped := wordwrap.String(l.Text, textW)
        txt := logStyles[l.Level].Render(wrapped)
        sb.WriteString(ts + "  " + txt + "\n")
    }
    return sb.String()
}

func (lb *LogBuffer) Clear() {
    lb.mu.Lock()
    lb.lines = lb.lines[:0]
    cb := lb.onChange
    lb.mu.Unlock()

    if cb != nil {
        cb()
    }
}