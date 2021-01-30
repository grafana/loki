package stats

import (
	"net/http"
	"time"
)

// WallTimeMiddleware tracks the wall time.
type WallTimeMiddleware struct{}

// NewWallTimeMiddleware makes a new WallTimeMiddleware.
func NewWallTimeMiddleware() WallTimeMiddleware {
	return WallTimeMiddleware{}
}

// Wrap implements middleware.Interface.
func (m WallTimeMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsEnabled(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}

		startTime := time.Now()
		next.ServeHTTP(w, r)

		stats := FromContext(r.Context())
		stats.AddWallTime(time.Since(startTime))
	})
}
