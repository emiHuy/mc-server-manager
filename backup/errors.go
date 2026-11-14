package backup

import (
	"context"
	"errors"
)

var (
	ErrInProgress = errors.New("backup: another backup is in progress")
	ErrNotFound   = errors.New("backup: not found")
	ErrInvalidID  = errors.New("bacup: invalid id")
)

// failureReason is a short message that is safe to show to API clients.
// The full error, which may include file paths, only goes to the log.
func failureReason(err error) string {
	switch {
	case errors.Is(err, ErrFlushTimeout):
		return "timed out waiting for the world save"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "backup failed"
	}
}
