package fanout

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionCancelClosesChannelAndRemoves(t *testing.T) {
	h := New[int]("test", 1, 0)
	_, sub := h.Subscribe(0)

	sub.Cancel()

	_, ok := <-sub.C
	require.False(t, ok)
	require.Equal(t, 0, h.Len())
}

func TestSubscriptionCancelIsIdempotent(t *testing.T) {
	h := New[int]("test", 1, 0)
	_, sub := h.Subscribe(0)

	sub.Cancel()
	sub.Cancel()
}

func TestSubscriptionDropped(t *testing.T) {
	h := New[int]("test", 1, 0)
	_, sub := h.Subscribe(0)
	defer sub.Cancel()

	require.Equal(t, 0, sub.Dropped())

	h.Publish(1)
	h.Publish(2) // drops 1
	h.Publish(3) // drops 2

	require.Equal(t, 2, sub.Dropped())
}
