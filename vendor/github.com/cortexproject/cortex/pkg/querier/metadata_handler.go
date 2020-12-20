package querier

import (
	"net/http"

	"github.com/cortexproject/cortex/pkg/util"
)

type metricMetadata struct {
	Type string `json:"type"`
	Help string `json:"help"`
	Unit string `json:"unit"`
}

const (
	statusSuccess = "success"
	statusError   = "error"
)

type metadataResult struct {
	Status string                      `json:"status"`
	Data   map[string][]metricMetadata `json:"data,omitempty"`
	Error  string                      `json:"error,omitempty"`
}

// MetadataHandler returns metric metadata held by Cortex for a given tenant.
// It is kept and returned as a set.
func MetadataHandler(d Distributor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := d.MetricsMetadata(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			util.WriteJSONResponse(w, metadataResult{Status: statusError, Error: err.Error()})
			return
		}

		// Put all the elements of the pseudo-set into a map of slices for marshalling.
		metrics := map[string][]metricMetadata{}
		for _, m := range resp {
			ms, ok := metrics[m.Metric]
			if !ok {
				// Most metrics will only hold 1 copy of the same metadata.
				ms = make([]metricMetadata, 0, 1)
				metrics[m.Metric] = ms
			}
			metrics[m.Metric] = append(ms, metricMetadata{Type: string(m.Type), Help: m.Help, Unit: m.Unit})
		}

		util.WriteJSONResponse(w, metadataResult{Status: statusSuccess, Data: metrics})
	})
}
