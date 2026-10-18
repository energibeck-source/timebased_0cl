package core

import (
    "fmt"
    "net"
    "time"
    "net/http"
    "crypto/tls"
    "os"
    "log"
    "sync"
    "io"
    "errors"
)

type SimpleRequester struct {
    Timeout  time.Duration
    TLSConf  *tls.Config
    RateLimiter <-chan time.Time
}

func (r *SimpleRequester) SendNormal(t Target) (*http.Response, time.Duration, error) {

    transport := &http.Transport{}
    if t.TLS {
        transport.TLSClientConfig = &tls.Config{
            InsecureSkipVerify: true,
        }
    }

    client := &http.Client{
        Timeout:   r.Timeout,
        Transport: transport,
        //no redirects
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            return http.ErrUseLastResponse
        },
    }
    req, err := http.NewRequest("GET", t.URL.RawPath, nil)

    if err != nil {
        return nil, 0, err
    }

    start := time.Now()
    resp, err := client.Do(req)
    return resp, time.Since(start), err
}


func (r *SimpleRequester) dial(t Target) (net.Conn, error) {

    if !t.TLS {
        return net.DialTimeout("tcp", fmt.Sprintf("%s:%d", t.URL.Hostname(), t.Port), r.Timeout)
    }

    tlsConf := r.TLSConf
    if tlsConf == nil {
        tlsConf = &tls.Config{
            InsecureSkipVerify: true,
        }
    }

    if keyLogPath := os.Getenv("SSLKEYLOGFILE"); keyLogPath != "" { //for wireshark
        f, err := os.OpenFile(keyLogPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
        if err == nil {
            tlsConf.KeyLogWriter = f
        }
    }
    d := &net.Dialer{Timeout: r.Timeout}
    return tls.DialWithDialer(d, "tcp", fmt.Sprintf("%s:%d", t.URL.Hostname(), t.Port), tlsConf)
}

func (r *SimpleRequester) SendRaw(t Target, payload []byte) ([]byte, time.Duration, error) {

    conn, err := r.dial(t)
    if err != nil {
        return nil, 0, err
    }
    defer conn.Close()

    if r.Timeout > 0 {
        _ = conn.SetDeadline(time.Now().Add(r.Timeout))
    }
    start := time.Now() 
    if _, err := conn.Write(payload); err != nil {
        return nil, 0, err
    }

    
    buf := make([]byte, 65535)
    n, err := conn.Read(buf)
    return buf[:n], time.Since(start), err
}

func (r *SimpleRequester) SendRawProbe(t Target, payload []byte, hardLimit time.Duration) ([]byte, time.Duration, error) {
    conn, err := r.dial(t)
    if err != nil {
        return nil, 0, err
    }
    defer conn.Close()

    _ = conn.SetDeadline(time.Now().Add(hardLimit))

    start := time.Now()

    if _, err := conn.Write(payload); err != nil {
        return nil, 0, err
    }

    var (
        result []byte
        buf    = make([]byte, 32*1024)
    )

    for {
        n, readErr := conn.Read(buf)

        if n > 0 {
            result = append(result, buf[:n]...)
            log.Printf("[0CL] probe recieved: %d bytes, t=%v", n, time.Since(start))
        }

        if readErr != nil {
            dur := time.Since(start)
            if isTimeout(readErr) {
                return result, dur, nil
            }

            if errors.Is(readErr, io.EOF) {
                return result, dur, nil
            }

            return result, dur, readErr
        }

        if len(result) > 0 {
            return result, time.Since(start), nil
        }
    }
}


var finalChunk = []byte("0\r\n\r\n")

func (r *SimpleRequester) SendChunkedWithDelay(
    t Target,
    headers, body []byte,
    pause time.Duration,
    opts RawOptions,
) (
    headersResp, bodyResp []byte,
    headersDur, bodyDur time.Duration,
    err error,
) {
    if pause <= 0 {
        return nil, nil, 0, 0, fmt.Errorf("pause must be > 0")
    }

    gc := []byte("some_data")

    if opts.IsChunked {
        headers = forceChunkedHeaders(headers)
    } else if opts.UpdateContentLength {
        clBody := body
        if opts.Obfuscate {
            clBody = append(append([]byte(nil), gc...), body...)
        }
        headers = updateCL(headers, clBody)
    }

    conn, err := r.dial(t)
    if err != nil {
        return nil, nil, 0, 0, fmt.Errorf("dial: %w", err)
    }
    defer conn.Close()
    _ = conn.SetDeadline(time.Time{})

    done := make(chan struct{})

    var (
        mu             sync.Mutex
        allBytes       []byte
        bodySentAt     time.Time
        firstAfterBody time.Time 
    )

    wrappedOpts := opts
    wrappedOpts.OnData = func(chunk RawChunk) {
        mu.Lock()
        allBytes = append(allBytes, chunk.Data...)
        if !bodySentAt.IsZero() && firstAfterBody.IsZero() && !chunk.At.Before(bodySentAt) {
            firstAfterBody = chunk.At
        }
        mu.Unlock()
        if opts.OnData != nil {
            opts.OnData(chunk)
        }
    }

    readerErrCh := make(chan error, 1)
    go func() {
        _, err := readerLoop(conn, pause+r.Timeout+5*time.Second, wrappedOpts, done)
        readerErrCh <- err
    }()

    var offset int
    takeNew := func() []byte {
        mu.Lock()
        defer mu.Unlock()
        if offset >= len(allBytes) {
            return nil
        }
        out := make([]byte, len(allBytes)-offset)
        copy(out, allBytes[offset:])
        offset = len(allBytes)
        return out
    }

    stopReader := func() {
        select {
        case <-done:
        default:
            close(done)
        }
        if rerr := <-readerErrCh; rerr != nil && !errors.Is(rerr, io.EOF) {
            log.Printf("[0CL] reader stopped: %v", rerr)
        }
    }

    start := time.Now()

    if err := writeAll(conn, headers, r.Timeout); err != nil {
        stopReader()
        return nil, nil, 0, 0, fmt.Errorf("write headers: %w", err)
    }
    log.Printf("[0CL] headers sent: %d bytes, t=%v", len(headers), time.Since(start))

    pause1, pause2 := pause, time.Duration(0)
    if opts.Obfuscate {
        pause1 = pause / 4
        pause2 = pause - pause1
    }

    time.Sleep(pause1)
    headersResp = takeNew()
    headersDur = time.Since(start)
    log.Printf("[0CL] pause1 done (%v), headersResp=%d bytes", headersDur, len(headersResp))

    // garbage part
    if opts.Obfuscate {
        gOut := gc
        if opts.IsChunked {
            gOut = appendChunkSize(gc)
        }
        if err := writeAll(conn, gOut, r.Timeout); err != nil {
            stopReader()
            return headersResp, nil, headersDur, 0, fmt.Errorf("write garbage: %w", err)
        }
        log.Printf("[0CL] garbage sent: %d bytes, t=%v", len(gOut), time.Since(start))

        time.Sleep(pause2)
        garbageSnap := takeNew()
        log.Printf("[0CL] pause2 done (%v), garbageResp=%d bytes", time.Since(start), len(garbageSnap))
    }

    maxWait := opts.MaxWait
    if maxWait <= 0 {
        maxWait = 5 * time.Second
    }

    waitFirstAfterBody := func() (time.Duration, bool) {
        deadline := time.Now().Add(maxWait)
        for {
            mu.Lock()
            fb := firstAfterBody
            sent := bodySentAt
            mu.Unlock()
            if !fb.IsZero() {
                return fb.Sub(sent), true
            }
            if time.Now().After(deadline) {
                return maxWait, false
            }
            time.Sleep(2 * time.Millisecond)
        }
    }

    if len(body) > 0 {
        out := body
        if opts.IsChunked {
            out = appendChunkSize(body)
            out = append(out, finalChunk...)
        }
        if err := writeAll(conn, out, r.Timeout); err != nil {
            stopReader()
            return headersResp, nil, headersDur, 0, fmt.Errorf("write body: %w", err)
        }
        log.Printf("[0CL] body sent: %d bytes, t=%v", len(out), time.Since(start))
    }

    mu.Lock()
    bodySentAt = time.Now()
    mu.Unlock()

    latency, got := waitFirstAfterBody()
    bodyDur = latency
    bodyResp = takeNew()
    if got {
        log.Printf("[0CL] first byte after body in %v, read %d bytes", bodyDur, len(bodyResp))
    } else {
        log.Printf("[0CL] NO response %v after body (hang signal), read %d bytes", bodyDur, len(bodyResp))
    }

    followup := "GET / HTTP/1.1\r\nHost: " + t.URL.Host + "\r\nConnection: close\r\n\r\n"
    if err := writeAll(conn, []byte(followup), r.Timeout); err != nil {
        stopReader()
        return headersResp, bodyResp, headersDur, bodyDur,
            fmt.Errorf("write followup: %w", err)
    }
    log.Printf("[0CL] followup sent: %d bytes, t=%v", len(followup), time.Since(start))

    followResp := drainIdle(takeNew, 300*time.Millisecond, maxWait)
    log.Printf("[0CL] followup read: %d bytes", len(followResp))

    bodyResp = append(bodyResp, followResp...)

    stopReader()

    log.Printf("[0CL] done: headers=%d body=%d total=%v",
        len(headersResp), len(bodyResp), time.Since(start))

    return headersResp, bodyResp, headersDur, bodyDur, nil
}


func (r *SimpleRequester) SendRawWithDelay(
	t Target, headers, body []byte, pause time.Duration, opts RawOptions,
    ) (
	firstResp, secondResp []byte, firstDur, secondDur time.Duration, err error,
) {
	if pause < 0 {
		return nil, nil, 0, 0, fmt.Errorf("pause must not be negative")
	}

	conn, err := r.dial(t)
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	log.Println("=== beginning of new request ===")

	start := time.Now()

	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, nil, 0, 0, fmt.Errorf("clear deadline: %w", err)
	}

	if err := writeAll(conn, headers, r.Timeout); err != nil {
		return nil, nil, 0, 0, fmt.Errorf("write headers: %w", err)
	}

	log.Printf("[0CL] headers sent: %d bytes; waiting %v before body",
		len(headers), pause)

	var beforeBody []byte

	if pause > 0 {
		beforeBody, err = readForDuration(
			conn,
			pause,
			"before-body",
			opts.OnData,
		)
		if err != nil {
			return beforeBody, nil, time.Since(start), 0,
				fmt.Errorf("read before body: %w", err)
		}
	}

	firstResp = beforeBody
	firstDur = time.Since(start)

	log.Printf("[0CL] pause finished: received %d bytes in %v",
		len(firstResp), firstDur)

	bodyStart := time.Now()

	if len(body) > 0 {
		if err := writeAll(conn, body, r.Timeout); err != nil {
			return firstResp, nil, firstDur, time.Since(bodyStart),
				fmt.Errorf("write body: %w", err)
		}

		log.Printf("[0CL] body sent: %d bytes", len(body))
	}

	secondResp, readErr := readUntilTimeoutOrEOF(
		conn,
		r.Timeout,
		"after-body",
		opts,
	)

	secondDur = time.Since(bodyStart)

	if readErr != nil {
		return firstResp, secondResp, firstDur, secondDur, readErr
	}

	log.Printf("[0CL] after-body finished: received %d bytes in %v",
		len(secondResp), secondDur)

	return firstResp, secondResp, firstDur, secondDur, nil
}

type readResult struct {
    data []byte
    err  error
}           
