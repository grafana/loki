package deletion

import (
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/retention"
	"github.com/grafana/loki/pkg/util/filter"
	util_log "github.com/grafana/loki/pkg/util/log"
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

// FilterFunction returns a filter function that returns true if the given line matches
// Note: FilterFunction can be nil when the delete request does not have a line filter.
func (d *DeleteRequest) FilterFunction(labels labels.Labels) (filter.Func, error) {
	if d.logSelectorExpr == nil {
		err := d.SetQuery(d.Query)
		if err != nil {
			return nil, err
		}
	}

	// return a filter func if the delete request has a line filter
	if !d.logSelectorExpr.HasFilter() {
		return nil, nil
	}

	p, err := d.logSelectorExpr.Pipeline()
	if err != nil {
		return nil, err
	}

	if !allMatch(d.matchers, labels) {
		return func(_ time.Time, s string) bool {
			return false
		}, nil
	}

	f := p.ForStream(labels).ProcessString
	return func(_ time.Time, s string) bool {
		result, _, skip := f(0, s)
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
// It also returns the intervals of the ChunkEntry that will remain before filtering.
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

	if !labels.Selector(d.matchers).Matches(entry.Labels) {
		return false, nil
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

	if d.StartTime <= entry.From && d.EndTime >= entry.Through {
		// if the logSelectorExpr has a filter part return the chunk boundaries as intervals
		if d.logSelectorExpr.HasFilter() {
			level.Info(util_log.Logger).Log("msg", "deleting whole chunk with filter",
				"delete_request_id", d.RequestID,
				"chunk_id", string(entry.ChunkID),
				"chunk_start", entry.From.Time().UTC().String(),
				"chunk_end", entry.Through.Time().UTC().String(),
			)
			return true, ff
		}

		// No filter in the logSelectorExpr so the whole chunk will be deleted
		return true, nil
	}

	// delete request is without a line filter so initialize it to always return true to simplify the code below for partial chunk deletion
	if ff == nil {
		ff = func(_ time.Time, _ string) bool {
			return true
		}
	}

	// init d.timeInterval used to efficiently check log ts is within the bounds of delete request below in filter func
	// without having to do conversion of timestamps for each log line we check.
	if d.timeInterval == nil {
		d.timeInterval = &timeInterval{
			start: d.StartTime.Time(),
			end:   d.EndTime.Time(),
		}
	}

	return true, func(ts time.Time, s string) bool {
		if ts.Before(d.timeInterval.start) || ts.After(d.timeInterval.end) {
			return false
		}

		return ff(ts, s)
	}
}

func intervalsOverlap(interval1, interval2 model.Interval) bool {
	if interval1.Start > interval2.End || interval2.Start > interval1.End {
		return false
	}

	return true
}
