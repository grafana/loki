package queryrange

import (
	"context"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
)

const (
	missingEmbeddedQueryMsg = "missing embedded query"
	nonEmbeddedErrMsg       = "DownstreamQuerier cannot handle a non-embedded query"
)

// ShardedQueryable is an implementor of the Queryable interface.
type ShardedQueryable struct {
	Req     Request
	Handler Handler
}

// Querier implements Queryable
func (q *ShardedQueryable) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return &ShardedQuerier{ctx, q.Req, q.Handler}, nil
}

// ShardedQuerier is a an implementor of the Querier interface.
type ShardedQuerier struct {
	Ctx     context.Context
	Req     Request
	Handler Handler
}

func (q *ShardedQuerier) Select(sp *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, storage.Warnings, error) {
	return q.SelectSorted(sp, matchers...)
}

// Select returns a set of series that matches the given label matchers.
func (q *ShardedQuerier) SelectSorted(_ *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, storage.Warnings, error) {
	var embeddedQuery string
	var isEmbedded bool
	for _, matcher := range matchers {
		if matcher.Name == labels.MetricName && matcher.Value == astmapper.EmbeddedQueriesMetricName {
			isEmbedded = true
		}

		if matcher.Name == astmapper.QueryLabel {
			embeddedQuery = matcher.Value
		}
	}

	if isEmbedded {
		if embeddedQuery != "" {
			return q.handleEmbeddedQuery(embeddedQuery)
		}
		return nil, nil, errors.Errorf(missingEmbeddedQueryMsg)

	}

	return nil, nil, errors.Errorf(nonEmbeddedErrMsg)
}

// handleEmbeddedQuery defers execution of an encoded query to a downstream Handler
func (q *ShardedQuerier) handleEmbeddedQuery(encoded string) (storage.SeriesSet, storage.Warnings, error) {
	queries, err := astmapper.JSONCodec.Decode(encoded)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(q.Ctx)
	defer cancel()

	// buffer channels to length of queries to prevent leaking memory due to sending to unbuffered channels after cancel/err
	errCh := make(chan error, len(queries))
	samplesCh := make(chan []SampleStream, len(queries))
	// TODO(owen-d): impl unified concurrency controls, not per middleware
	for _, query := range queries {
		go func(query string) {
			resp, err := q.Handler.Do(ctx, q.Req.WithQuery(query))
			if err != nil {
				errCh <- err
				return
			}
			streams, err := ResponseToSamples(resp)
			if err != nil {
				errCh <- err
				return
			}
			samplesCh <- streams
		}(query)
	}

	var samples []SampleStream

	for i := 0; i < len(queries); i++ {
		select {
		case err := <-errCh:
			return nil, nil, err
		case streams := <-samplesCh:
			samples = append(samples, streams...)
		}
	}

	return NewSeriesSet(samples), nil, err
}

// LabelValues returns all potential values for a label name.
func (q *ShardedQuerier) LabelValues(name string) ([]string, storage.Warnings, error) {
	return nil, nil, errors.Errorf("unimplemented")
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (q *ShardedQuerier) LabelNames() ([]string, storage.Warnings, error) {
	return nil, nil, errors.Errorf("unimplemented")
}

// Close releases the resources of the Querier.
func (q *ShardedQuerier) Close() error {
	return nil
}
