package core

import (
	"net"
	"net/url"
	"testing"
	"time"
)

func delayServer(t *testing.T, delay time.Duration) (addr string, stop func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 4096)
				for {
					_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
					n, err := conn.Read(buf)
					if n > 0 {
						time.Sleep(delay)
						_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: keep-alive\r\n\r\nhi"))
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestBodyDurTracksServerDelay(t *testing.T) {
	const serverDelay = 350 * time.Millisecond
	addr, stop := delayServer(t, serverDelay)
	defer stop()

	host, portStr, _ := net.SplitHostPort(addr)
	u := url.URL{Scheme: "http", Host: addr, Path: "/"}

	req := &SimpleRequester{Timeout: 2 * time.Second}
	tgt := Target{URL: u, TLS: false}

	var port int
	_, _ = timeParsePort(portStr, &port)
	tgt.Port = port
	_ = host

	runner := &Runner{
		Req:    req,
		Target: tgt,
		Options: SendOptions{
			Mode:          ModeChunkedWithDelay,
			Pause:         20 * time.Millisecond,
			Repeat:        3,
			TimeThreshold: 200 * time.Millisecond,
			MaxWait:       3 * time.Second,
			UpdateContentLength: true,
		},
	}

	payload := []byte("POST / HTTP/1.1\r\nHost: " + addr + "\r\nContent-Length: 4\r\n\r\ntest")
	res := runner.Run(payload)
	if res.Err != nil {
		t.Fatalf("run error: %v", res.Err)
	}

	if len(res.Samples) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(res.Samples))
	}

	if res.Median < 250*time.Millisecond || res.Median > 900*time.Millisecond {
		t.Fatalf("median %v not tracking server delay %v (sleep pollution?)", res.Median, serverDelay)
	}
	if !res.Hit {
		t.Fatalf("expected HIT: median %v >= threshold %v", res.Median, runner.Options.TimeThreshold)
	}
	if res.Stdev < 0 {
		t.Fatalf("bad stdev %v", res.Stdev)
	}
	t.Logf("median=%v mean=%v stdev=%v min=%v max=%v samples=%v",
		res.Median, res.Mean, res.Stdev, res.Min, res.Max, res.Samples)
}

func TestFastServerMisses(t *testing.T) {
	addr, stop := delayServer(t, 5*time.Millisecond)
	defer stop()

	u := url.URL{Scheme: "http", Host: addr, Path: "/"}
	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	_, _ = timeParsePort(portStr, &port)

	runner := &Runner{
		Req:    &SimpleRequester{Timeout: 2 * time.Second},
		Target: Target{URL: u, Port: port, TLS: false},
		Options: SendOptions{
			Mode:          ModeChunkedWithDelay,
			Pause:         20 * time.Millisecond,
			Repeat:        3,
			TimeThreshold: 300 * time.Millisecond,
			MaxWait:       3 * time.Second,
			UpdateContentLength: true,
		},
	}

	payload := []byte("POST / HTTP/1.1\r\nHost: " + addr + "\r\nContent-Length: 4\r\n\r\ntest")
	res := runner.Run(payload)
	if res.Err != nil {
		t.Fatalf("run error: %v", res.Err)
	}
	if res.Hit {
		t.Fatalf("expected MISS on fast server, median=%v", res.Median)
	}
}

func TestComputeStats(t *testing.T) {
	s := []time.Duration{10, 20, 30, 40, 100}
	median, mean, _, min, max := computeStats(s)
	if median != 30 {
		t.Fatalf("median = %v, want 30", median)
	}
	if mean != 40 {
		t.Fatalf("mean = %v, want 40", mean)
	}
	if min != 10 || max != 100 {
		t.Fatalf("min/max = %v/%v, want 10/100", min, max)
	}
}

func timeParsePort(s string, out *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return n, nil
}
