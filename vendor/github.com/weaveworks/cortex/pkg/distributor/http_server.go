package distributor

import (
	"fmt"
	"net/http"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/promql"

	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/cortex/pkg/ingester/client"
	"github.com/weaveworks/cortex/pkg/util"
)

// PushHandler is a http.Handler which accepts WriteRequests.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	compressionType := util.CompressionTypeFor(r.Header.Get("X-Prometheus-Remote-Write-Version"))
	var req client.WriteRequest
	buf, err := util.ParseProtoRequest(r.Context(), r, &req, compressionType)
	logger := util.WithContext(r.Context(), util.Logger)
	if err != nil {
		level.Error(logger).Log("err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if d.cfg.EnableBilling {
		var samples int64
		for _, ts := range req.Timeseries {
			samples += int64(len(ts.Samples))
		}
		if err := d.emitBillingRecord(r.Context(), buf, samples); err != nil {
			level.Error(logger).Log("msg", "error emitting billing record", "err", err)
		}
	}

	if _, err := d.Push(r.Context(), &req); err != nil {
		if httpResp, ok := httpgrpc.HTTPResponseFromError(err); ok {
			level.Error(logger).Log("msg", "push error", "err", err)
			http.Error(w, string(httpResp.Body), int(httpResp.Code))
		} else {
			level.Error(logger).Log("msg", "push error", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
}

// UserStats models ingestion statistics for one user.
type UserStats struct {
	IngestionRate float64 `json:"ingestionRate"`
	NumSeries     uint64  `json:"numSeries"`
}

// UserStatsHandler handles user stats to the Distributor.
func (d *Distributor) UserStatsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := d.UserStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	util.WriteJSONResponse(w, stats)
}

// ValidateExprHandler validates a PromQL expression.
func (d *Distributor) ValidateExprHandler(w http.ResponseWriter, r *http.Request) {
	_, err := promql.ParseExpr(r.FormValue("expr"))

	// We mimick the response format of Prometheus's official API here for
	// consistency, but unfortunately its private types (string consts etc.)
	// aren't reusable.
	if err == nil {
		util.WriteJSONResponse(w, map[string]string{
			"status": "success",
		})
		return
	}

	parseErr, ok := err.(*promql.ParseErr)
	if !ok {
		// This should always be a promql.ParseErr.
		http.Error(w, fmt.Sprintf("unexpected error returned from PromQL parser: %v", err), http.StatusInternalServerError)
		return
	}

	// If the parsing input was a single line, parseErr.Line is 0
	// and the generated error string omits the line entirely. But we
	// want to report line numbers consistently, no matter how many
	// lines there are (starting at 1).
	if parseErr.Line == 0 {
		parseErr.Line = 1
	}
	w.WriteHeader(http.StatusBadRequest)
	util.WriteJSONResponse(w, map[string]interface{}{
		"status":    "error",
		"errorType": "bad_data",
		"error":     err.Error(),
		"location": map[string]int{
			"line": parseErr.Line,
			"pos":  parseErr.Pos,
		},
	})
}
