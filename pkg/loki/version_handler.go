package loki

import (
	"encoding/json"
	"net/http"

	"github.com/grafana/loki/pkg/util/build"
)

type buildInfo struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Branch    string `json:"branch"`
	BuildUser string `json:"build_user"`
	BuildDate string `json:"build_date"`
}

func versionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := buildInfo{
			Version:   build.Version,
			Revision:  build.Revision,
			Branch:    build.Branch,
			BuildUser: build.BuildUser,
			BuildDate: build.BuildDate,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// We ignore errors here, because we cannot do anything about them.
		// Write will trigger sending Status code, so we cannot send a different status code afterwards.
		// Also this isn't internal error, but error communicating with client.
		_ = json.NewEncoder(w).Encode(info)
	}
}
