package manager

import (
	"context"
	"errors"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
)

const AlertForStateMetricName = "ALERTS_FOR_STATE"

func (m *MemStore) Appender() storage.Appender { return m }

// Add implements storage.Appener by filtering only the ALERTS_FOR_STATE series and mapping to a rule-specific appender.
// This is used when a distinct rule group is loaded to see if it had been firing previously.
func (m *MemStore) Add(ls labels.Labels, t int64, v float64) (uint64, error) {
	var name string

	for _, l := range ls {
		if l.Name == labels.AlertName {
			name = l.Value
		}
		if l.Name == labels.MetricName && l.Value != AlertForStateMetricName {
			// This is not an ALERTS_FOR_STATE metric, skip
			return 0, nil
		}
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	app, ok := m.appenders[name]
	if !ok {
		app = NewForStateAppender(m.metrics)
		m.appenders[name] = app
	}

	return app.Add(ls, t, v)
}

func (m *MemStore) AddFast(ref uint64, t int64, v float64) error {
	return errors.New("unimplemented")
}

func (m *MemStore) Commit() error { return nil }

func (m *MemStore) Rollback() error { return nil }

// implement storage.Queryable
func (m *MemStore) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return &MemStoreQuerier{
		mint:     mint,
		maxt:     maxt,
		MemStore: m,
		ctx:      ctx,
	}, nil

}

type MemStoreQuerier struct {
	mint, maxt int64
	ctx        context.Context
	*MemStore
}

func (m *MemStoreQuerier) Select(sortSeries bool, params *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	var ruleKey string
	for _, matcher := range matchers {
		if matcher.Name == labels.AlertName && matcher.Type == labels.MatchEqual {
			ruleKey = matcher.Value
		}
	}
	if ruleKey == "" {
		return storage.NoopSeriesSet()
	}

	m.MemStore.mtx.Lock()
	defer m.MemStore.mtx.Unlock()

	app, ok := m.MemStore.appenders[ruleKey]
	if !ok {
		return storage.NoopSeriesSet()
	}

	return app.Querier(m.ctx, m.mint, m.maxt).Select(sortSeries, params, matchers...)
}

// LabelValues returns all potential values for a label name.
func (*MemStoreQuerier) LabelValues(name string) ([]string, storage.Warnings, error) {
	return nil, nil, errors.New("unimplemented")
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (*MemStoreQuerier) LabelNames() ([]string, storage.Warnings, error) {
	return nil, nil, errors.New("unimplemented")
}

// Close releases the resources of the Querier.
func (*MemStoreQuerier) Close() error { return nil }
