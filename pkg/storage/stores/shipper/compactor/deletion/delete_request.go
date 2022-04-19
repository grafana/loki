package deletion

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	"github.com/grafana/loki/pkg/util/filter"
)

type DeleteRequest struct {
	RequestID string              `json:"request_id"`
	StartTime model.Time          `json:"start_time"`
	EndTime   model.Time          `json:"end_time"`
	Query     string              `json:"query"`
	Status    DeleteRequestStatus `json:"status"`
	CreatedAt model.Time          `json:"created_at"`

	UserID          string                 `json:"-"`
	matchers        []*labels.Matcher      `json:"-"`
	logSelectorExpr syntax.LogSelectorExpr `json:"-"`
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
func (d *DeleteRequest) FilterFunction(labels labels.Labels) (filter.Func, error) {
	if d.logSelectorExpr == nil {
		err := d.SetQuery(d.Query)
		if err != nil {
			return nil, err
		}
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
		return len(result) != 0 || skip
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
// It also returns the intervals of the ChunkEntry that will remain.
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

	if d.StartTime <= entry.From && d.EndTime >= entry.Through {
		return true, nil
	}

	intervals := make([]retention.IntervalFilter, 0, 2)
	ff, err := d.FilterFunction(entry.Labels)
	if err != nil {
		// TODO: log this? Handle it different?
		return false, nil
	}

	if d.StartTime > entry.From {
		intervals = append(intervals, retention.IntervalFilter{
			Interval: model.Interval{
				Start: entry.From,
				End:   d.StartTime - 1,
			},
			Filter: ff,
		})
	}

	if d.EndTime < entry.Through {
		intervals = append(intervals, retention.IntervalFilter{
			Interval: model.Interval{
				Start: d.EndTime + 1,
				End:   entry.Through,
			},
			Filter: ff,
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
