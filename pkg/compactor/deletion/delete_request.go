package deletion

import (
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type timeInterval struct {
	start, end time.Time
}

type deleteRequest struct {
	deletionproto.DeleteRequest
	matchers        []*labels.Matcher      `json:"-"`
	logSelectorExpr syntax.LogSelectorExpr `json:"-"`
	timeInterval    *timeInterval          `json:"-"`

	TotalLinesDeletedMetric *prometheus.CounterVec `json:"-"`
	DeletedLines            atomic.Int32           `json:"-"`
}

func newDeleteRequest(protoReq deletionproto.DeleteRequest, totalLinesDeletedMetric *prometheus.CounterVec) (*deleteRequest, error) {
	d := deleteRequest{
		DeleteRequest:           protoReq,
		TotalLinesDeletedMetric: totalLinesDeletedMetric,
	}

	if err := d.SetQuery(d.Query); err != nil {
		return nil, err
	}

	// init d.timeInterval used to efficiently check log ts is within the bounds of delete request below in filter func
	// without having to do conversion of timestamps for each log line we check.
	d.timeInterval = &timeInterval{
		start: d.StartTime.Time(),
		end:   d.EndTime.Time(),
	}

	return &d, nil
}

func (d *deleteRequest) SetQuery(logQL string) error {
	d.Query = logQL
	logSelectorExpr, err := parseDeletionQuery(logQL)
	if err != nil {
		return err
	}
	d.logSelectorExpr = logSelectorExpr
	d.matchers = logSelectorExpr.Matchers()
	return nil
}

// FilterFunction returns a filter function that returns true if the given line should be deleted based on the DeleteRequest.
// The chunk the lines come from decides whether they are counted: a chunk indexed in several daily tables is filtered
// once per table, so only the pass over the last of those tables adds to the deleted line counters.
func (d *deleteRequest) FilterFunction(chunk retention.Chunk, tableInterval model.Interval, lbls labels.Labels) (filter.Func, error) {
	if !allMatch(d.matchers, lbls) {
		return func(_ time.Time, _ string, _ labels.Labels) bool {
			return false
		}, nil
	}

	// if delete request doesn't have a line filter, just do time based filtering
	if !d.logSelectorExpr.HasFilter() {
		return func(ts time.Time, _ string, _ labels.Labels) bool {
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

	countThisPass := d.countsChunk(chunk, tableInterval, lbls)
	f := p.ForStream(lbls).ProcessString
	return func(ts time.Time, s string, structuredMetadata labels.Labels) bool {
		if ts.Before(d.timeInterval.start) || ts.After(d.timeInterval.end) {
			return false
		}

		result, _, skip := f(0, s, structuredMetadata)
		if len(result) != 0 || skip {
			if countThisPass {
				d.TotalLinesDeletedMetric.WithLabelValues(d.UserID).Inc()
				d.DeletedLines.Add(1)
			}
			return true
		}
		return false
	}, nil
}

// countsChunk reports whether the deletes made while filtering this chunk belong in the line counters. Every daily
// table a chunk's bounds overlap runs the filter over it again, and each run reports the same lines as deleted, so
// counting each run would report the lines as many times as tables index the chunk. Only the last of those tables
// counts them. That makes the counters follow the chunk instead of the table, and re-running a table after a failure
// still counts it, since the answer depends only on which table is being processed.
func (d *deleteRequest) countsChunk(chunk retention.Chunk, tableInterval model.Interval, lbls labels.Labels) bool {
	// The whole chunk is deleted without a filter, so no line is ever visited and
	// there is nothing to count here; the caller accounts for the chunk instead.
	if d.StartTime <= chunk.From && d.EndTime >= chunk.Through && !d.logSelectorExpr.HasFilter() {
		return false
	}

	if !labels.Selector(d.matchers).Matches(lbls) {
		return false
	}

	// The last table indexing the chunk is the one holding its end.
	return chunk.Through <= tableInterval.End
}

func allMatch(matchers []*labels.Matcher, labels labels.Labels) bool {
	for _, m := range matchers {
		if !m.Matches(labels.Get(m.Name)) {
			return false
		}
	}
	return true
}

// IsDeleted checks if the given chunk entry would have data requested for deletion.
func (d *deleteRequest) IsDeleted(userID []byte, lbls labels.Labels, chunk retention.Chunk) bool {
	if d.UserID != unsafeGetString(userID) {
		return false
	}

	if !intervalsOverlap(model.Interval{
		Start: chunk.From,
		End:   chunk.Through,
	}, model.Interval{
		Start: d.StartTime,
		End:   d.EndTime,
	}) {
		return false
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
			return false
		}
	}

	if !labels.Selector(d.matchers).Matches(lbls) {
		return false
	}

	return true
}

// GetChunkFilter tells whether the chunk is covered by the DeleteRequest and
// optionally returns a filter.Func if the chunk is supposed to be deleted partially or the delete request has line filters.
func (d *deleteRequest) GetChunkFilter(userID []byte, lbls labels.Labels, chunk retention.Chunk, tableInterval model.Interval) (bool, filter.Func) {
	if !d.IsDeleted(userID, lbls, chunk) {
		return false, nil
	}

	if d.StartTime <= chunk.From && d.EndTime >= chunk.Through && !d.logSelectorExpr.HasFilter() {
		// Delete request covers the whole chunk and there are no line filters in the logSelectorExpr so the whole chunk will be deleted
		return true, nil
	}

	ff, err := d.FilterFunction(chunk, tableInterval, lbls)
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

func (d *deleteRequest) IsDuplicate(o *deleteRequest) (bool, error) {
	// we would never have duplicates from same request
	if d.RequestID == o.RequestID {
		return false, nil
	}
	if d.UserID != o.UserID || d.StartTime != o.StartTime || d.EndTime != o.EndTime {
		return false, nil
	}

	if d.logSelectorExpr == nil {
		if err := d.SetQuery(d.Query); err != nil {
			return false, errors.Wrapf(err, "failed to init log selector expr for request_id=%s, user_id=%s", d.RequestID, d.UserID)
		}
	}
	if o.logSelectorExpr == nil {
		if err := o.SetQuery(o.Query); err != nil {
			return false, errors.Wrapf(err, "failed to init log selector expr for request_id=%s, user_id=%s", o.RequestID, o.UserID)
		}
	}

	if d.logSelectorExpr.String() != o.logSelectorExpr.String() {
		return false, nil
	}

	return true, nil
}

func intervalsOverlap(interval1, interval2 model.Interval) bool {
	if interval1.Start > interval2.End || interval2.Start > interval1.End {
		return false
	}

	return true
}

// GetMatchers returns the string representation of the matchers
func (d *deleteRequest) GetMatchers() string {
	if len(d.matchers) == 0 {
		return ""
	}
	var result []string
	for _, m := range d.matchers {
		result = append(result, m.String())
	}
	return strings.Join(result, ",")
}
