package deletion

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
)

type DeleteRequest struct {
	RequestID string              `json:"request_id"`
	StartTime model.Time          `json:"start_time"`
	EndTime   model.Time          `json:"end_time"`
	Queries   []string            `json:"queries"`
	Status    DeleteRequestStatus `json:"status"`
	CreatedAt model.Time          `json:"created_at"`

	UserID   string            `json:"-"`
	matchers []*labels.Matcher `json:"-"`
}

func (d *DeleteRequest) AddQueries(queries []string) error {
	d.Queries = queries
	for _, query := range queries {
		matchers, err := parseDeletionQuery(query)
		if err != nil {
			return err
		}

		d.matchers = append(d.matchers, matchers...)
	}

	return nil
}

func (d *DeleteRequest) IsDeleted(entry retention.ChunkEntry) (bool, []model.Interval) {
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

	intervals := make([]model.Interval, 0, 2)

	if d.StartTime > entry.From {
		intervals = append(intervals, model.Interval{
			Start: entry.From,
			End:   d.StartTime - 1,
		})
	}

	if d.EndTime < entry.Through {
		intervals = append(intervals, model.Interval{
			Start: d.EndTime + 1,
			End:   entry.Through,
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
