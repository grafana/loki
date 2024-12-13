package deletion

import (
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type timeInterval struct {
	start, end time.Time
}

type DeleteRequest struct {
	RequestID string              `json:"request_id"`
	StartTime model.Time          `json:"start_time"`
	EndTime   model.Time          `json:"end_time"`
	Query     string              `json:"query"`
	Status    DeleteRequestStatus `json:"status"`
	CreatedAt model.Time          `json:"created_at"`

	UserID          string                 `json:"-"`
	SequenceNum     int64                  `json:"-"`
	matchers        []*labels.Matcher      `json:"-"`
	logSelectorExpr syntax.LogSelectorExpr `json:"-"`
	timeInterval    *timeInterval          `json:"-"`

	Metrics      *deleteRequestsManagerMetrics `json:"-"`
	DeletedLines int32                         `json:"-"`
}

func (d *DeleteRequest) SetQuery(logQL string) error {
	d.Query = logQL
	logSelectorExpr, err := parseDeletionQuery(logQL)
	if err != nil {
		return err
	}
	d.logSelectorExpr = logSelectorExpr
	d.matchers = logSelectorExpr.Matchers()
	return nil
}

// FilterFunction returns a filter function that returns true if the given line should be deleted based on the DeleteRequest
func (d *DeleteRequest) FilterFunction(lbls labels.Labels) (filter.Func, error) {
	// init d.timeInterval used to efficiently check log ts is within the bounds of delete request below in filter func
	// without having to do conversion of timestamps for each log line we check.
	if d.timeInterval == nil {
		d.timeInterval = &timeInterval{
			start: d.StartTime.Time(),
			end:   d.EndTime.Time(),
		}
	}

	if !allMatch(d.matchers, lbls) {
		return func(_ time.Time, _ string, _ ...labels.Label) bool {
			return false
		}, nil
	}

	// if delete request doesn't have a line filter, just do time based filtering
	if !d.logSelectorExpr.HasFilter() {
		return func(ts time.Time, _ string, _ ...labels.Label) bool {
			if ts.Before(d.timeInterval.start) || ts.After(d.timeInterval.end) {
				return false
			}

			return true
		}, nil
	}

	p, err := d.logSelectorExpr.Pipeline()
	if err != nil {
		return nil, err
	}

	f := p.ForStream(lbls).ProcessString
	return func(ts time.Time, s string, structuredMetadata ...labels.Label) bool {
		if ts.Before(d.timeInterval.start) || ts.After(d.timeInterval.end) {
			return false
		}

		result, _, skip := f(0, s, structuredMetadata...)
		if len(result) != 0 || skip {
			d.Metrics.deletedLinesTotal.WithLabelValues(d.UserID).Inc()
			d.DeletedLines++
			return true
		}
		return false
	}, nil
}

func allMatch(matchers []*labels.Matcher, labels labels.Labels) bool {
	for _, m := range matchers {
		if !m.Matches(labels.Get(m.Name)) {
			return false
		}
	}
	return true
}

// IsDeleted checks if the given ChunkEntry will be deleted by this DeleteRequest.
// It returns a filter.Func if the chunk is supposed to be deleted partially or the delete request contains line filters.
// If the filter.Func is nil, the whole chunk is supposed to be deleted.
func (d *DeleteRequest) IsDeleted(entry retention.ChunkEntry) (bool, filter.Func) {
	if d.UserID != unsafeGetString(entry.UserID) {
		return false, nil
	}

	if !intervalsOverlap(model.Interval{
		Start: entry.From,
		End:   entry.Through,
	}, model.Interval{
		Start: d.StartTime,
		End:   d.EndTime,
	}) {
		return false, nil
	}

	if d.logSelectorExpr == nil {
		err := d.SetQuery(d.Query)
		if err != nil {
			level.Error(util_log.Logger).Log(
				"msg", "failed to init log selector expr",
				"delete_request_id", d.RequestID,
				"user", d.UserID,
				"err", err,
			)
			return false, nil
		}
	}

	if !labels.Selector(d.matchers).Matches(entry.Labels) {
		return false, nil
	}

	if d.StartTime <= entry.From && d.EndTime >= entry.Through && !d.logSelectorExpr.HasFilter() {
		// Delete request covers the whole chunk and there are no line filters in the logSelectorExpr so the whole chunk will be deleted
		return true, nil
	}

	ff, err := d.FilterFunction(entry.Labels)
	if err != nil {
		// The query in the delete request is checked when added to the table.
		// So this error should not occur.
		level.Error(util_log.Logger).Log(
			"msg", "unexpected error getting filter function",
			"delete_request_id", d.RequestID,
			"user", d.UserID,
			"err", err,
		)
		return false, nil
	}

	return true, ff
}

func intervalsOverlap(interval1, interval2 model.Interval) bool {
	if interval1.Start > interval2.End || interval2.Start > interval1.End {
		return false
	}

	return true
}
