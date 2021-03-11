package querier

import (
	"archive/tar"
	"compress/gzip"
	"net/http"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"

	"github.com/cortexproject/cortex/pkg/querier/chunkstore"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
)

// ChunksHandler allows you to fetch a compressed tar of all the chunks for a
// given time range and set of matchers.
// Only works with the new unified chunk querier, which is enabled when you turn
// on ingester chunk query streaming.
func ChunksHandler(queryable storage.Queryable) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, err := tenant.TenantID(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mint, err := util.ParseTime(r.FormValue("start"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		maxt, err := util.ParseTime(r.FormValue("end"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		matchers, err := parser.ParseMetricSelector(r.FormValue("matcher"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		querier, err := queryable.Querier(r.Context(), mint, maxt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		store, ok := querier.(chunkstore.ChunkStore)
		if !ok {
			http.Error(w, "not supported", http.StatusServiceUnavailable)
			return
		}

		chunks, err := store.Get(r.Context(), userID, model.Time(mint), model.Time(maxt), matchers...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/tar+gzip")
		gw := gzip.NewWriter(w)
		defer gw.Close()

		writer := tar.NewWriter(gw)
		defer writer.Close()

		for _, chunk := range chunks {
			buf, err := chunk.Encoded()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if err := writer.WriteHeader(&tar.Header{
				Name: chunk.ExternalKey(),
				Size: int64(len(buf)),
				Mode: 0600,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if _, err := writer.Write(buf); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	})
}
