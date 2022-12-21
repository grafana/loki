package deletion

import (
	"net/http"

	"github.com/grafana/dskit/tenant"
)

func TenantMiddleware(limits Limits, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID, err := tenant.TenantID(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		hasDelete, err := validDeletionLimit(limits, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if !hasDelete {
			http.Error(w, deletionNotAvailableMsg, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
