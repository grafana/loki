package loki

import (
	"encoding/json"
	"net/http"

	"github.com/grafana/loki/v3/pkg/util/build"
)

func versionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// We ignore errors here, because we cannot do anything about them.
		// Write will trigger sending Status code, so we cannot send a different status code afterwards.
		// Also this isn't internal error, but error communicating with client.
		_ = json.NewEncoder(w).Encode(build.GetVersion())
	}
}
