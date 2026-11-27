package api

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewSSEWriterHeaders(t *testing.T) {
	rec := httptest.NewRecorder()

	_, err := newSSEWriter(rec)
	require.NoError(t, err)

	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	require.True(t, rec.Flushed)
}

func TestServeStream(t *testing.T) {
	t.Run("forwards items until channel closes", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sse, err := newSSEWriter(rec)
		require.NoError(t, err)

		ch := make(chan string, 2)
		ch <- "one"
		ch <- "two"
		close(ch)

		serveStream(context.Background(), sse, ch, newConsoleLineResponse)

		want := "data: {\"line\":\"one\"}\n\ndata: {\"line\":\"two\"}\n\n"
		require.Equal(t, want, rec.Body.String())
	})

	t.Run("returns when context is cancelled", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sse, _ := newSSEWriter(rec)
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			serveStream(ctx, sse, make(chan string), newConsoleLineResponse)
			close(done)
		}()

		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("serveStream did not return after context cancel")
		}
	})
}
