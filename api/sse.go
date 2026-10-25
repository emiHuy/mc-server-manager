package api

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"time"
)

const (
	keepaliveInterval = 20 * time.Second
	sseWriteTimeout   = 10 * time.Second
)

// sseWriter writes Server-Sent Events to one client.
type sseWriter struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

// newSSEWriter sends the stream headers and flushes them, so the client sees the stream open immediately.
// After it returns, the status is committed and errors can no longer be reported as JSON.
func newSSEWriter(w http.ResponseWriter) (*sseWriter, error) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // stops nginx-style proxies from buffering the stream
	w.WriteHeader(http.StatusOK)

	s := &sseWriter{w: w, rc: http.NewResponseController(w)}
	if err := s.rc.Flush(); err != nil {
		return nil, err
	}
	return s, nil
}

// send writes v as one message.
// json.Marshal output has no raw newlines, so a single data line is always valid.
func (s *sseWriter) send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.write("data: " + string(data) + "\n\n")
}

// comment writes a line that clients ignore.
// It keeps idle streams alive and exposes dead connections through a failed write.
func (s *sseWriter) comment(text string) error {
	return s.write(": " + text + "\n\n")
}

func (s *sseWriter) write(msg string) error {
	// Bound each write: a client that stops reading must not pin this goroutine and its subscription forever.
	_ = s.rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))

	if _, err := io.WriteString(s.w, msg); err != nil {
		return err
	}
	return s.rc.Flush()
}

// serveStream forwards items from ch until the client goes away, ch closes, or a write fails.
func serveStream[T, R any](ctx context.Context, sse *sseWriter, ch <-chan T, convert func(T) R) {
	keepalive := time.NewTicker(keepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-ch:
			if !ok {
				return
			}
			if err := sse.send(convert(item)); err != nil {
				return
			}
		case <-keepalive.C:
			if err := sse.comment("keepalive"); err != nil {
				return
			}
		}
	}
}
