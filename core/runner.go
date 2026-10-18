package core

import (
    "fmt"
    "time"
    "log"
    "net/http"
    "crypto/tls"
    "io"
)

type Runner struct {
    Req     *SimpleRequester
    Target  Target
    Options SendOptions
}

func (r *Runner) Run(payload []byte) Result {
    reps := r.Options.Repeat
    if reps < 1 {
        reps = 1
    }

    var (
        last    Result
        samples []time.Duration
    )

    for i := 0; i < reps; i++ {
        res := r.runOnce(payload)
        last = res
        if res.Err != nil {
            break
        }
        sig := res.SecondDur
        if sig <= 0 {
            sig = res.TotalDur
        }
        samples = append(samples, sig)
    }

    if len(samples) > 0 {
        median, mean, stdev, min, max := computeStats(samples)
        last.Samples = samples
        last.Median = median
        last.Mean = mean
        last.Stdev = stdev
        last.Min = min
        last.Max = max
        last.TotalDur = median
        last.Hit = median >= r.Options.TimeThreshold
    }
    return last
}

func (r *Runner) runOnce(payload []byte) Result {
    payload = normalizeCRLF(payload)
    
    switch r.Options.Mode {

    case ModeRaw:
        body, dur, err := r.Req.SendRaw(r.Target, payload)
        res := Result{
            Second:   body,
            SecondDur: dur,
            TotalDur: dur,
            Err:      err,
        }
        r.maybeFollowUp(&res)
        return res

    case ModeRawProbe:
        headers, _ := splitRequest(payload)
        body, dur, err := r.Req.SendRawProbe(r.Target, headers, r.Options.ProbeHardLimit)
        res := Result{
            Second:   body,
            SecondDur: dur,
            TotalDur: dur,
            Err:      err,
        }
        r.maybeFollowUp(&res)
        return res

    case ModeRawWithDelay:
        headers, body := splitRequest(payload)
        options := RawOptions{
			OnData: func(chunk RawChunk) {
				log.Printf(
					"[live][%s][%s] %d bytes: %q",
					chunk.Phase,
					chunk.At.Format(time.RFC3339Nano),
					len(chunk.Data),
					chunk.Data,
				)
			},
			AcceptEOF: true,
            IsChunked:   false,
            Obfuscate: r.Options.Obfuscate,
            UpdateContentLength: r.Options.UpdateContentLength,
            MaxWait: r.Options.MaxWait,
        }

        first, second, d1, d2, err := r.Req.SendChunkedWithDelay(
            r.Target, headers, body, r.Options.Pause, options,
        )
        res := Result{
            First:     first,
            Second:    second,
            FirstDur:  d1,
            SecondDur: d2,
            TotalDur:  d1 + d2,
            Err:       err,
        }
        r.maybeFollowUp(&res)
        return res

    case ModeChunkedWithDelay:
        headers, body := splitRequest(payload)
        options := RawOptions{
			OnData: func(chunk RawChunk) {
				log.Printf(
					"[live][%s][%s] %d bytes: %q",
					chunk.Phase,
					chunk.At.Format(time.RFC3339Nano),
					len(chunk.Data),
					chunk.Data,
				)
			},
			AcceptEOF: true,
            IsChunked:   true,
            Obfuscate: r.Options.Obfuscate,
            UpdateContentLength: r.Options.UpdateContentLength,
            MaxWait: r.Options.MaxWait,
        }

        first, second, d1, d2, err := r.Req.SendChunkedWithDelay(
            r.Target, headers, body, r.Options.Pause, options,
        )
        res := Result{
            First:     first,
            Second:    second,
            FirstDur:  d1,
            SecondDur: d2,
            TotalDur:  d1 + d2,
            Err:       err,
        }
        r.maybeFollowUp(&res)
        return res
    }

    return Result{Err: fmt.Errorf("unknown mode: %d", r.Options.Mode)}
}

func (r *Runner) maybeFollowUp(res *Result) {
    if !r.Options.FollowUp {
        return
    }
    time.Sleep(300*time.Millisecond)

    req, err := http.NewRequest("GET", r.Target.URL.String(), nil)
    if err != nil {
        res.FollowUpErr = err
        return
    }
    req.Header.Set("Connection", "close")

    client := &http.Client{
        Timeout: 10 * time.Second,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
        CheckRedirect: func(*http.Request, []*http.Request) error {
            return http.ErrUseLastResponse
        },
    }

    start := time.Now()
    resp, err := client.Do(req)
    res.FollowUpDur = time.Since(start)
    if err != nil {
        res.FollowUpErr = err
        return
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    res.FollowUp = body
    res.FollowUpStatus = resp.StatusCode

    if b := r.Options.Baseline; b != nil {
        matched, details := compareBaseline(b, resp.StatusCode, len(body))
        res.BaselineMatched = matched
        res.BaselineDetails = details
    }
}
