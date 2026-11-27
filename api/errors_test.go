package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emiHuy/mc-server-manager/backup"
	"github.com/emiHuy/mc-server-manager/minecraft"
	"github.com/stretchr/testify/require"
)

func TestStatusFor(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{errNotFound, 404},
		{errMethodNotAllowed, 405},
		{fmt.Errorf("wrap: %w", minecraft.ErrInvalidState), 409},
		{minecraft.ErrPreflight, 409},
		{minecraft.ErrInvalidCommand, 400},
		{backup.ErrInProgress, 409},
		{backup.ErrServerBusy, 409},
		{backup.ErrChanged, 409},
		{fmt.Errorf("wrap: %w", backup.ErrNotFound), 404},
		{backup.ErrInvalidID, 400},
		{errors.New("boom"), 500},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, statusFor(tt.err))
	}
}

func TestWriteErrorHidesInternalDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), errors.New("secret db path /etc/foo"))

	require.Equal(t, 500, rec.Code)
	require.Equal(t, "internal error", errMsg(t, rec))
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}
