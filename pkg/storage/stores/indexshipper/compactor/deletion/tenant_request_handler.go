package deletion

import (
	"net/http"

	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/retention"

	"github.com/grafana/dskit/tenant"
)

func TenantMiddleware(limits retention.Limits, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID, err := tenant.TenantID(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		allLimits := limits.AllByUserID()
		userLimits, ok := allLimits[userID]
		if ok {
			if !userLimits.CompactorDeletionEnabled {
				http.Error(w, deletionNotAvailableMsg, http.StatusForbidden)
				return
			}
		} else {
			if !limits.DefaultLimits().CompactorDeletionEnabled {
				http.Error(w, deletionNotAvailableMsg, http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
