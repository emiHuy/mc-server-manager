package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/backup"
	"github.com/stretchr/testify/require"
)

func TestLinesParam(t *testing.T) {
	tests := []struct {
		query   string
		want    int
		wantErr bool
	}{
		{"", defaultConsoleLines, false},
		{"lines=1", 1, false},
		{"lines=1000", 1000, false},
		{"lines=0", 0, true},
		{"lines=1001", 0, true},
		{"lines=-5", 0, true},
		{"lines=abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)
			got, err := linesParam(r, 1)
			if tt.wantErr {
				require.ErrorIs(t, err, errInvalidLines)
				return
			}
			if err != nil {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

// These requests are rejected before the handler touches the instance or restarter.
func TestRequestValidation(t *testing.T) {
	s := &Server{}
	big := `{"command":"` + strings.Repeat("a", maxCommandBody) + `"}`

	tests := []struct {
		name, method, path, ct, body string
		want                         int
	}{
		{"command wrong content type", "POST", "/api/server/command", "text/plain", `{}`, 415},
		{"command missing content type", "POST", "/api/server/command", "", `{}`, 415},
		{"command malformed json", "POST", "/api/server/command", "application/json", `{`, 400},
		{"command body too large", "POST", "/api/server/command", "application/json", big, 400},
		{"auto-restart wrong content type", "POST", "/api/server/auto-restart", "text/plain", `{}`, 415},
		{"auto-restart missing enabled", "POST", "/api/server/auto-restart", "application/json", `{}`, 400},
		{"auto-restart malformed", "POST", "/api/server/auto-restart", "application/json", `nope`, 400},
		{"console invalid lines", "GET", "/api/server/console?lines=0", "", "", 400},
		{"console stream invalid lines", "GET", "/api/server/console/stream?lines=5000", "", "", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, s, tt.method, tt.path, tt.ct, tt.body)
			require.Equal(t, tt.want, rec.Code)
		})
	}
}

func TestBackupCreate(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("success", func(t *testing.T) {
		s := &Server{backups: fakeBackups{create: func(context.Context) (backup.Info, error) {
			return backup.Info{ID: "b1", CreatedAt: created, SizeBytes: 42}, nil
		}}}

		rec := do(t, s, "POST", "/api/backups", "", "")
		require.Equal(t, 201, rec.Code)

		var got struct {
			ID        string    `json:"id"`
			CreatedAt time.Time `json:"created_at"`
			SizeBytes int64     `json:"size_bytes"`
		}

		err := json.Unmarshal(rec.Body.Bytes(), &got)
		require.NoError(t, err)

		require.Equal(t, "b1", got.ID)
		require.Equal(t, 42, int(got.SizeBytes))
		require.Equal(t, created, got.CreatedAt)
	})

	t.Run("already in progress", func(t *testing.T) {
		s := &Server{backups: fakeBackups{create: func(context.Context) (backup.Info, error) {
			return backup.Info{}, backup.ErrInProgress
		}}}

		rec := do(t, s, "POST", "/api/backups", "", "")
		require.Equal(t, 409, rec.Code)
	})
}

func TestBackupList(t *testing.T) {
	t.Run("empty is [] not null", func(t *testing.T) {
		s := &Server{backups: fakeBackups{list: func() ([]backup.Info, error) { return nil, nil }}}

		rec := do(t, s, "GET", "/api/backups", "", "")
		require.Equal(t, 200, rec.Code)
		require.Contains(t, rec.Body.String(), `"backups":[]`)
	})

	t.Run("lists items", func(t *testing.T) {
		s := &Server{backups: fakeBackups{list: func() ([]backup.Info, error) {
			return []backup.Info{{ID: "a"}, {ID: "b"}}, nil
		}}}

		rec := do(t, s, "GET", "/api/backups", "", "")

		var got struct {
			Backups []struct {
				ID string `json:"id"`
			} `json:"backups"`
		}

		err := json.Unmarshal(rec.Body.Bytes(), &got)
		require.NoError(t, err)

		require.Len(t, got.Backups, 2)
		require.Equal(t, "a", got.Backups[0].ID)
	})

	t.Run("service error", func(t *testing.T) {
		s := &Server{backups: fakeBackups{list: func() ([]backup.Info, error) { return nil, errors.New("disk") }}}
		rec := do(t, s, "GET", "/api/backups", "", "")

		require.Equal(t, 500, rec.Code)
		require.Equal(t, "internal error", errMsg(t, rec))
	})
}

func TestBackupDelete(t *testing.T) {
	var deleted string
	s := &Server{backups: fakeBackups{delete: func(id string) error {
		deleted = id
		switch id {
		case "missing":
			return backup.ErrNotFound
		case "bad":
			return backup.ErrInvalidID
		}
		return nil
	}}}

	tests := []struct {
		id   string
		want int
	}{{"ok", 204}, {"missing", 404}, {"bad", 400}}
	for _, tt := range tests {
		rec := do(t, s, "DELETE", "/api/backups/"+tt.id, "", "")
		require.Equal(t, tt.want, rec.Code)
		require.Equal(t, tt.id, deleted)
	}
}

func TestBackupDownload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b1.zip")
	err := os.WriteFile(path, []byte("zipdata"), 0o600)
	require.NoError(t, err)

	s := &Server{backups: fakeBackups{exists: func(id string) (string, error) {
		if id == "b1" {
			return path, nil
		}
		return "", backup.ErrNotFound
	}}}

	t.Run("success", func(t *testing.T) {
		rec := do(t, s, "GET", "/api/backups/b1/download", "", "")
		require.Equal(t, 200, rec.Code)

		require.Equal(t, `attachment; filename="b1.zip"`, rec.Header().Get("Content-Disposition"))
		require.Equal(t, "zipdata", rec.Body.String())
	})

	t.Run("not found", func(t *testing.T) {
		rec := do(t, s, "GET", "/api/backups/nope/download", "", "")
		require.Equal(t, 404, rec.Code)
	})
}
