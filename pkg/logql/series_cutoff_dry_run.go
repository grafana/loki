package logql

import (
	"context"
	"errors"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"

	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	logutil "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	cutoffDryRunAgreed       = "agreed"
	cutoffDryRunMismatch     = "mismatch"
	cutoffDryRunInconclusive = "inconclusive"
)

var seriesCutoffDryRunTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: constants.Loki,
	Name:      "logql_series_cutoff_dry_run_total",
	Help:      "Queries where the max_query_series cut-off would have fired, by whether the query then failed on the series limit anyway (agreed), succeeded (mismatch) or failed on something else (inconclusive).",
}, []string{"outcome"})

// cutoffTracker records that the early max_query_series cut-off would have failed a query, when
// the cut-off only runs as a dry run. It is shared by every iterator of the query, so it is safe
// for concurrent use.
type cutoffTracker struct {
	fired atomic.Bool
}

// newCutoffTracker returns a tracker that has not fired.
func newCutoffTracker() *cutoffTracker {
	return &cutoffTracker{}
}

// record notes that the cut-off would have failed the query. It is safe to call on a nil tracker.
func (t *cutoffTracker) record() {
	if t != nil {
		t.fired.Store(true)
	}
}

// outcome returns the outcome label for a query that finished with err, and false when the
// cut-off never fired and there is nothing to report.
//
// A query where the cut-off would have fired must still end on the series limit: the cut-off only
// fires where the output cannot be smaller than the limit. Success means the cut-off is wrong.
func (t *cutoffTracker) outcome(err error) (string, bool) {
	if t == nil || !t.fired.Load() {
		return "", false
	}
	switch {
	case err == nil:
		return cutoffDryRunMismatch, true
	case errors.Is(err, logqlerr.ErrLimit):
		return cutoffDryRunAgreed, true
	default:
		return cutoffDryRunInconclusive, true
	}
}

// report counts the outcome of a finished query and logs a mismatch, so a query where the
// cut-off disagrees with the real series limit can be found. It does nothing on a nil tracker.
func (t *cutoffTracker) report(ctx context.Context, logger log.Logger, params Params, err error) {
	outcome, ok := t.outcome(err)
	if !ok {
		return
	}
	seriesCutoffDryRunTotal.WithLabelValues(outcome).Inc()
	if outcome == cutoffDryRunMismatch {
		level.Warn(logutil.WithContext(ctx, logger)).Log(cutoffMismatchKeyvals(ctx, params)...)
	}
}

// cutoffMismatchKeyvals returns the fields that identify a query where the dry-run cut-off
// would have failed a query that succeeded. They match the "executing query" line, so the two
// can be joined on query_hash.
func cutoffMismatchKeyvals(ctx context.Context, params Params) []interface{} {
	keyvals := []interface{}{
		"msg", "max_query_series cut-off dry run would have failed a query that succeeded",
		"query", params.QueryString(),
		"query_hash", util.HashedQuery(params.QueryString()),
	}
	if GetRangeType(params) == InstantType {
		keyvals = append(keyvals, "type", "instant")
	} else {
		keyvals = append(keyvals, "type", "range", "length", params.End().Sub(params.Start()), "step", params.Step())
	}
	return append(keyvals, httpreq.TagsToKeyValues(httpreq.ExtractQueryTagsFromContext(ctx))...)
}

type cutoffTrackerKey struct{}

// contextWithCutoffTracker returns a context that carries t.
func contextWithCutoffTracker(ctx context.Context, t *cutoffTracker) context.Context {
	return context.WithValue(ctx, cutoffTrackerKey{}, t)
}

// cutoffTrackerFromContext returns the tracker of the query that runs under ctx, or nil when the
// cut-off is not a dry run.
func cutoffTrackerFromContext(ctx context.Context) *cutoffTracker {
	t, _ := ctx.Value(cutoffTrackerKey{}).(*cutoffTracker)
	return t
}
