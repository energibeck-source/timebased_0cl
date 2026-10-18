package main

import (
    "fmt"
    "os"
    "time"
    "log"
    "path/filepath"

    "timebased_0cl/core"
    "timebased_0cl/tui"

    tea "github.com/charmbracelet/bubbletea"
)

func main() {
    target := core.ParseOptions()
    requester := &core.SimpleRequester{
        Timeout: 10 * time.Second,
    }

    runner := &core.Runner{
        Req:     requester,
        Target:  target,
    }

    logFile := filepath.Join(os.TempDir(), "timebased_0cl.log")
    f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        log.Fatal(err)
    }
    defer f.Close()
    log.SetOutput(f)
    m := tui.New(runner, target.CurrentReq, f)
    p := tea.NewProgram(m, tea.WithAltScreen())
    m.SetProgram(p)

    if _, err := p.Run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}