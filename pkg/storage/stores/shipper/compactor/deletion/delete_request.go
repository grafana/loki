package deletion

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk/encoding"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
)

type DeleteRequest struct {
	RequestID string              `json:"request_id"`
	StartTime model.Time          `json:"start_time"`
	EndTime   model.Time          `json:"end_time"`
	Query     string              `json:"query"`
	Status    DeleteRequestStatus `json:"status"`
	CreatedAt model.Time          `json:"created_at"`

	UserID   string            `json:"-"`
	matchers []*labels.Matcher `json:"-"`
}

func (d *DeleteRequest) SetQuery(logQL string) error {
	d.Query = logQL
	matchers, err := parseDeletionQuery(logQL)
	if err != nil {
		return err
	}
	d.matchers = matchers
	return nil
}

// FilterFunction returns a dummy filter that doesn't remove any lines
func (d *DeleteRequest) FilterFunction() encoding.FilterFunc {
	return func(s string) bool {
		return false
	}
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
	ff := d.FilterFunction()

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
