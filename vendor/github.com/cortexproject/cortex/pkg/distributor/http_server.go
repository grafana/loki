package distributor

import (
	"net/http"

	"github.com/go-kit/kit/log/level"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
)

// PushHandler is a http.Handler which accepts WriteRequests.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	compressionType := util.CompressionTypeFor(r.Header.Get("X-Prometheus-Remote-Write-Version"))
	var req client.PreallocWriteRequest
	req.Source = client.API
	buf, err := util.ParseProtoReader(r.Context(), r.Body, int(r.ContentLength), d.cfg.MaxRecvMsgSize, &req, compressionType)
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

	if _, err := d.Push(r.Context(), &req.WriteRequest); err != nil {
		resp, ok := httpgrpc.HTTPResponseFromError(err)
		if !ok {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if resp.GetCode() != 202 {
			level.Error(logger).Log("msg", "push error", "err", err)
		}
		http.Error(w, string(resp.Body), int(resp.Code))
	}
}

// UserStats models ingestion statistics for one user.
type UserStats struct {
	IngestionRate     float64 `json:"ingestionRate"`
	NumSeries         uint64  `json:"numSeries"`
	APIIngestionRate  float64 `json:"APIIngestionRate"`
	RuleIngestionRate float64 `json:"RuleIngestionRate"`
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
