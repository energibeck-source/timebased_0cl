package core

import (
    "time"   
    "net/url"
)

type Options struct {
    RawUrl          string
    URL             url.URL
    RequestFile     string
    Port        int
}

type Target struct {
    URL         url.URL
    Port        int
    TLS         bool
    CurrentReq  []byte
}


type Mode int

const (
    ModeRaw          Mode = iota 
    ModeRawProbe
    ModeRawWithDelay
    ModeChunkedWithDelay
)

type SendOptions struct {
    Mode Mode

    ProbeHardLimit time.Duration
    Pause time.Duration
    Repeat int
    TimeThreshold time.Duration
    FollowUp bool
    Baseline *Baseline
    Obfuscate   bool
    UpdateContentLength bool
    MaxWait time.Duration
}
type Baseline struct {
    StatusCode    int
    ContentLength int
    Body          []byte
}

func DefaultSendOptions() SendOptions {
    return SendOptions{
        Mode:          ModeRawWithDelay,
        ProbeHardLimit: 5 * time.Second,
        Pause:         1500 * time.Millisecond,
        Repeat:        5,
        TimeThreshold: 1000 * time.Millisecond,
        Obfuscate: false,
        UpdateContentLength: true,
        MaxWait:       5 * time.Second,
    }
}

type Result struct {
    First       []byte
    Second      []byte
    FirstDur    time.Duration
    SecondDur   time.Duration
    TotalDur    time.Duration
    Err         error
    Hit         bool

    FollowUp       []byte
    FollowUpDur    time.Duration
    FollowUpStatus int
    FollowUpErr    error

    BaselineMatched bool
    BaselineDetails string

    Samples []time.Duration
    Median  time.Duration
    Mean    time.Duration
    Stdev   time.Duration
    Min     time.Duration
    Max     time.Duration
}


type RawChunk struct {
	Phase string
	Data  []byte
	At    time.Time
}

type RawOptions struct {
	OnData func(RawChunk)

	AcceptEOF bool
    IsChunked    bool
    Obfuscate   bool
    UpdateContentLength bool

    MaxWait time.Duration
}