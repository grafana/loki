package deletion

import (
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/retention"
	"github.com/grafana/loki/pkg/util/filter"
	util_log "github.com/grafana/loki/pkg/util/log"
)

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
		return func(s string) bool {
			return false
		}, nil
	}

	f := p.ForStream(labels).ProcessString
	return func(s string) bool {
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
func (d *DeleteRequest) IsDeleted(entry retention.ChunkEntry) (bool, []retention.IntervalFilter) {
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
			return true, []retention.IntervalFilter{
				{
					Interval: model.Interval{
						Start: entry.From,
						End:   entry.Through,
					},
					Filter: ff,
				},
			}
		}

		// No filter in the logSelectorExpr so the whole chunk will be deleted
		return true, nil
	}

	intervals := make([]retention.IntervalFilter, 0, 2)

	// chunk partially deleted from the end
	if d.StartTime > entry.From {
		// Add the deleted part with Filter func
		if ff != nil {
			intervals = append(intervals, retention.IntervalFilter{
				Interval: model.Interval{
					Start: d.StartTime,
					End:   entry.Through,
				},
				Filter: ff,
			})
		}

		// Add non-deleted part without Filter func
		intervals = append(intervals, retention.IntervalFilter{
			Interval: model.Interval{
				Start: entry.From,
				End:   d.StartTime - 1,
			},
		})
	}

	// chunk partially deleted from the beginning
	if d.EndTime < entry.Through {
		// Add the deleted part with Filter func
		if ff != nil {
			intervals = append(intervals, retention.IntervalFilter{
				Interval: model.Interval{
					Start: entry.From,
					End:   d.EndTime,
				},
				Filter: ff,
			})
		}

		// Add non-deleted part without Filter func
		intervals = append(intervals, retention.IntervalFilter{
			Interval: model.Interval{
				Start: d.EndTime + 1,
				End:   entry.Through,
			},
		})
	}

	return true, intervals
}

func intervalsOverlap(interval1, interval2 model.Interval) bool {
	if interval1.Start > interval2.End || interval2.Start > interval1.End {
		return false
	}

	return true
}
