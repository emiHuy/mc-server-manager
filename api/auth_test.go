package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuth(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	tests := []struct {
		name   string
		token  string
		path   string
		header string
		want   int
	}{
		{"disabled allows everything", "", "/api/server/status", "", 200},
		{"missing header", "secret", "/api/server/status", "", 401},
		{"wrong token", "secret", "/api/server/status", "Bearer nope", 401},
		{"wrong scheme", "secret", "/api/server/status", "Basic secret", 401},
		{"no prefix", "secret", "/api/server/status", "secret", 401},
		{"correct token", "secret", "/api/server/status", "Bearer secret", 200},
		{"non-api path skips auth", "secret", "/dashboard/", "", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{token: tt.token}
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			s.auth(ok).ServeHTTP(rec, req)

			require.Equal(t, tt.want, rec.Code)

			if tt.want == 401 {
				require.NotEmpty(t, rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}
