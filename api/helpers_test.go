package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emiHuy/mc-server-manager/backup"
	"github.com/stretchr/testify/require"
)

// do sends a request through the full auth+routes stack.
func do(t *testing.T, s *Server, method, path, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	s.auth(s.routes()).ServeHTTP(rec, req)
	return rec
}

func errMsg(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var e errorResponse
	err := json.Unmarshal(rec.Body.Bytes(), &e)
	require.NoError(t, err)

	return e.Error
}

// fakeBackups is a configurable backupService.
type fakeBackups struct {
	create func(ctx context.Context) (backup.Info, error)
	list   func() ([]backup.Info, error)
	delete func(id string) error
	exists func(id string) (string, error)
}

func (f fakeBackups) Create(ctx context.Context) (backup.Info, error) { return f.create(ctx) }
func (f fakeBackups) List() ([]backup.Info, error)                    { return f.list() }
func (f fakeBackups) Delete(id string) error                          { return f.delete(id) }
func (f fakeBackups) BackupExists(id string) (string, error)          { return f.exists(id) }
