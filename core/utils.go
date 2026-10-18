package core

import (
	"fmt"
	"bytes"
	"net"
	"time"
	"errors"
	"strconv"
	"io"
	"sort"
	"math"
	"sync"
)

// computeStats returns the median, mean, standard deviation, min and max
// over a set of durations. For time-based decisions the median is more
// robust than the mean: a single network outlier won't move the verdict.
func computeStats(samples []time.Duration) (median, mean, stdev, min, max time.Duration) {
	if len(samples) == 0 {
		return 0, 0, 0, 0, 0
	}

	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	min, max = sorted[0], sorted[len(sorted)-1]

	n := len(sorted)
	if n%2 == 1 {
		median = sorted[n/2]
	} else {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	}

	var sum time.Duration
	for _, s := range sorted {
		sum += s
	}
	mean = sum / time.Duration(n)

	var variance float64
	for _, s := range sorted {
		d := float64(s - mean)
		variance += d * d
	}
	variance /= float64(n)
	stdev = time.Duration(math.Sqrt(variance))

	return median, mean, stdev, min, max
}

func normalizeCRLF(payload []byte) []byte {
    payload = bytes.ReplaceAll(payload, []byte("\r\n"), []byte("\n"))
    return bytes.ReplaceAll(payload, []byte("\n"), []byte("\r\n"))
}


// splitRequest splits a raw request into headers and body.
// Everything up to and including \r\n\r\n is headers, the rest is body.

func splitRequest(raw []byte) (headers, body []byte) {
    idx := bytes.Index(raw, []byte("\r\n\r\n"))
    if idx < 0 {
        return raw, nil
    }
    cut := idx + 4
    return raw[:cut], raw[cut:]
}

func compareBaseline(b *Baseline, status, length int) (bool, string) {
    if b.StatusCode != status {
        return true, fmt.Sprintf("status %d → %d (changed)", b.StatusCode, status)
    }
    if b.ContentLength != length {
        return true, fmt.Sprintf("content-length %d → %d (changed)", b.ContentLength, length)
    }
    return false, "unchanged"
}

func appendChunkSize(chunk []byte) []byte {
    if len(chunk) == 0 {
        return []byte("0\r\n\r\n")
    }

    var buf bytes.Buffer
    buf.WriteString(strconv.FormatInt(int64(len(chunk)), 16))
    buf.WriteString("\r\n")
    buf.Write(chunk)
    buf.WriteString("\r\n")
    return buf.Bytes()
}

// forceChunkedHeaders replaces the "Content-Length: N" line with
// "Transfer-Encoding: chunked". If there is no CL line, it adds TE
// after the request line.
func forceChunkedHeaders(headers []byte) []byte {
    // process line by line, preserving \r\n
    lines := bytes.Split(headers, []byte("\r\n"))
    found := false

    for i, line := range lines {
        if len(line) >= len("Content-Length:") &&
            bytes.EqualFold(line[:len("Content-Length:")], []byte("Content-Length:")) {
            lines[i] = []byte("Transfer-Encoding: chunked")
            found = true
        }
    }

    out := bytes.Join(lines, []byte("\r\n"))

    if !found {
        idx := bytes.Index(out, []byte("\r\n"))
        if idx < 0 {
            out = append(out, []byte("\r\nTransfer-Encoding: chunked")...)
        } else {
            insert := idx + 2
            out = append(out[:insert],
                append([]byte("Transfer-Encoding: chunked\r\n"), out[insert:]...)...)
        }
    }

    return out
}

func updateCL(headers, body []byte) []byte {
    lines := bytes.Split(headers, []byte("\r\n"))

    prefix := []byte("Content-Length:")
    newLine := []byte(fmt.Sprintf("Content-Length: %d", len(body)))

    found := false
    for i, line := range lines {
        if len(line) >= len(prefix) &&
            bytes.EqualFold(line[:len(prefix)], prefix) {
            if !found {
                lines[i] = newLine
                found = true
            } else {
                // duplicate - drop it
                lines[i] = nil
            }
        }
    }

    var clean [][]byte
    for _, l := range lines {
        if l != nil {
            clean = append(clean, l)
        }
    }
    out := bytes.Join(clean, []byte("\r\n"))

    if !found {
        idx := bytes.Index(out, []byte("\r\n"))
        if idx < 0 {
            out = append(append([]byte(nil), out...), []byte("\r\n")...)
            out = append(out, newLine...)
        } else {
            insert := idx + 2
            head := append([]byte(nil), out[:insert]...)
            tail := append([]byte(nil), out[insert:]...)
            out = append(head, newLine...)
            out = append(out, []byte("\r\n")...)
            out = append(out, tail...)
        }
    }

    return out
}

func drainIdle(takeNew func() []byte, idle, hardLimit time.Duration) []byte {
	var out []byte
	start := time.Now()
	lastData := time.Now()
	for {
		chunk := takeNew()
		if len(chunk) > 0 {
			out = append(out, chunk...)
			lastData = time.Now()
		}
		if time.Since(lastData) >= idle {
			return out
		}
		if hardLimit > 0 && time.Since(start) >= hardLimit {
			return out
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func writeAll(conn net.Conn, data []byte, timeout time.Duration) error {
	if len(data) == 0 {
		return nil
	}

	if timeout > 0 {
		if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			return err
		}
	} else {
		if err := conn.SetWriteDeadline(time.Time{}); err != nil {
			return err
		}
	}

	for len(data) > 0 {
		n, err := conn.Write(data)

		if n > 0 {
			data = data[n:]
		}

		if err != nil {
			return err
		}

		if n == 0 {
			return io.ErrShortWrite
		}
	}

	return nil
}

func readForDuration(
	conn net.Conn, 
	duration time.Duration, 
	phase string, 
	onData func(RawChunk),
	) ([]byte, error) {
	deadline := time.Now().Add(duration)

	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, err
	}

	var result []byte
	buf := make([]byte, 32*1024)

	for {
		n, err := conn.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			result = append(result, chunk...)

			if onData != nil {
				onData(RawChunk{
					Phase: phase,
					Data:  chunk,
					At:    time.Now(),
				})
			}
		}

		if err != nil {
			if isTimeout(err) {
				return result, nil
			}

			if errors.Is(err, io.EOF) {
				return result, nil
			}

			return result, err
		}
	}
}

func readUntilIdle(conn net.Conn, idle time.Duration, hardLimit time.Duration, phase string, opts RawOptions) ([]byte, error) {
    var result []byte
    buf := make([]byte, 32*1024)
    start := time.Now()

    for {
        _ = conn.SetReadDeadline(time.Now().Add(idle))

        n, err := conn.Read(buf)
        if n > 0 {
            chunk := append([]byte(nil), buf[:n]...)
            result = append(result, chunk...)
            if opts.OnData != nil {
                opts.OnData(RawChunk{Phase: phase, Data: chunk, At: time.Now()})
            }
        }

        if err != nil {
            if isTimeout(err) {
                return result, nil
            }
            if errors.Is(err, io.EOF) {
                return result, nil
            }
            return result, err
        }

        if hardLimit > 0 && time.Since(start) > hardLimit {
            return result, nil
        }
    }
}
func readUntilTimeoutOrEOF(conn net.Conn, timeout time.Duration, phase string, opts RawOptions) ([]byte, error) {
	if timeout > 0 {
		if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return nil, err
		}
	} else {
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			return nil, err
		}
	}

	var result []byte
	buf := make([]byte, 32*1024)

	for {
		n, err := conn.Read(buf)

		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			result = append(result, chunk...)

			if opts.OnData != nil {
				opts.OnData(RawChunk{
					Phase: phase,
					Data:  chunk,
					At:    time.Now(),
				})
			}
		}

		if err != nil {
			if isTimeout(err) {
				return result, nil
			}

			if errors.Is(err, io.EOF) {
				if opts.AcceptEOF {
					return result, nil
				}
				return result, io.EOF
			}

			return result, err
		}
	}
}

func readerLoop(conn net.Conn, hardLimit time.Duration, opts RawOptions, done <-chan struct{}) ([]byte, error) {
    var mu sync.Mutex
    var result []byte
    buf := make([]byte, 32*1024)
    start := time.Now()

    for {
        select {
        case <-done:
            return result, nil
        default:
        }

        if hardLimit > 0 && time.Since(start) > hardLimit {
            return result, nil
        }

        _ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))

        n, err := conn.Read(buf)
        if n > 0 {
            chunk := append([]byte(nil), buf[:n]...)

            mu.Lock()
            result = append(result, chunk...)
            mu.Unlock()

            if opts.OnData != nil {
                opts.OnData(RawChunk{
                    Phase: "stream",
                    Data:  chunk,
                    At:    time.Now(),
                })
            }
        }

        if err != nil {
            if isTimeout(err) {
                continue
            }
            if errors.Is(err, io.EOF) {
                return result, nil
            }
            return result, err
        }
    }
}