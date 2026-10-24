package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emiHuy/mc-server-manager/config"
)

func newAuthTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	s := New(newStoppedInstance(), config.APIConfig{Token: token})
	return httptest.NewTestServer(t, s.httpServer.Handler)
}

func doAuth(t *testing.T, server *httptest.Server, method, path, authHeader string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAuth(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	server := newAuthTestServer(t, token)

	tests := []struct {
		name   string
		method string
		path   string
		header string
		want   int
	}{
		{"valid token", http.MethodGet, "/api/server/status", "Bearer " + token, http.StatusOK},
		{"no header", http.MethodGet, "/api/server/status", "", http.StatusUnauthorized},
		{"wrong token", http.MethodGet, "/api/server/status", "Bearer nope", http.StatusUnauthorized},
		{"prefix of the token", http.MethodGet, "/api/server/status", "Bearer " + token[:10], http.StatusUnauthorized},
		{"token with extra suffix", http.MethodGet, "/api/server/status", "Bearer " + token + "x", http.StatusUnauthorized},
		{"wrong scheme", http.MethodGet, "/api/server/status", "Basic " + token, http.StatusUnauthorized},
		{"token without scheme", http.MethodGet, "/api/server/status", token, http.StatusUnauthorized},
		// Auth runs before routing, so callers learn nothing about routes.
		{"unknown route, no token", http.MethodGet, "/api/nope", "", http.StatusUnauthorized},
		{"wrong method, no token", http.MethodDelete, "/api/server/status", "", http.StatusUnauthorized},
		// 401, not the 409 the stopped server would give an authorized caller.
		{"action without token", http.MethodPost, "/api/server/stop", "", http.StatusUnauthorized},
		{"stream without token", http.MethodGet, "/api/server/events", "", http.StatusUnauthorized},
		// Authorized requests still get the normal routing errors.
		{"unknown route, valid token", http.MethodGet, "/api/nope", "Bearer " + token, http.StatusNotFound},
		{"action with token", http.MethodPost, "/api/server/stop", "Bearer " + token, http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doAuth(t, server, tt.method, tt.path, tt.header)

			if resp.StatusCode != tt.want {
				t.Fatalf("status code = %d, want %d", resp.StatusCode, tt.want)
			}
			if tt.want == http.StatusUnauthorized {
				if got := resp.Header.Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want %q", got, "Bearer")
				}
				if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
					t.Error("an unauthorized request must not start a stream")
				}
				body, _ := readJSON[errorResponse](t, resp)
				if body.Error != "unauthorized" {
					t.Errorf("error = %q, want %q", body.Error, "unauthorized")
				}
			}
		})
	}
}

func TestNoTokenMeansOpen(t *testing.T) {
	server := newAuthTestServer(t, "")

	resp := doAuth(t, server, http.MethodGet, "/api/server/status", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want 200 when no token is configured", resp.StatusCode)
	}
}
