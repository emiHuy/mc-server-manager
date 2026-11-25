package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRouting(t *testing.T) {
	s := &Server{}

	tests := []struct {
		method, path string
		wantStatus   int
		wantAllow    string
	}{
		{"POST", "/api/server/status", 405, "GET"},
		{"GET", "/api/server/start", 405, "POST"},
		{"DELETE", "/api/backups", 405, "GET, POST"},
		{"GET", "/api/backups/abc", 405, "DELETE"},
		{"GET", "/api/nope", 404, ""},
		{"GET", "/", 404, ""},
		{"GET", "/docs", 301, ""},
		{"GET", "/dashboard", 301, ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := do(t, s, tt.method, tt.path, "", "")
			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantAllow, rec.Header().Get("Allow"))

			if tt.wantStatus == 404 || tt.wantStatus == 405 {
				require.NotEmpty(t, errMsg(t, rec))
			}
		})
	}
}

func TestAuthAppliesToRoutes(t *testing.T) {
	s := &Server{token: "secret"}

	rec := do(t, s, "GET", "/api/server/status", "", "")
	require.Equal(t, 401, rec.Code)

	// Unknown API paths are also protected, so they don't leak existence.
	rec = do(t, s, "GET", "/api/nope", "", "")
	require.Equal(t, 401, rec.Code)
}

func TestDashboardAndDocsURL(t *testing.T) {
	tests := []struct {
		addr, dashboard, docs string
	}{
		{":8080", "http://localhost:8080/dashboard", "http://localhost:8080/docs"},
		{"0.0.0.0:8080", "http://localhost:8080/dashboard", "http://localhost:8080/docs"},
		{"[::]:8080", "http://localhost:8080/dashboard", "http://localhost:8080/docs"},
		{"192.168.1.5:9000", "http://192.168.1.5:9000/dashboard", "http://192.168.1.5:9000/docs"},
		{"[::1]:9000", "http://[::1]:9000/dashboard", "http://[::1]:9000/docs"},
		{"garbage", "http://garbage", "http://garbage"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.dashboard, dashboardURL(tt.addr))
		require.Equal(t, tt.docs, docsURL(tt.addr))
	}
}
