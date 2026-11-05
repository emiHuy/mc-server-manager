package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
)

func (s *Server) auth(next http.Handler) http.Handler {
	if s.token == "" {
		return next // auth disabled
	}

	want := sha256.Sum256([]byte(s.token))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(got))

		if !ok || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			slog.Warn("unauthorized request", "path", r.URL.Path, "remote", r.RemoteAddr)
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcsm"`)
			writeError(w, r, errUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
