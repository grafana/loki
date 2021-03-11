package queryrange

import (
	"context"
	"sync"

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

	sharededQuerier *ShardedQuerier
}

// Querier implements Queryable
func (q *ShardedQueryable) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	q.sharededQuerier = &ShardedQuerier{Ctx: ctx, Req: q.Req, Handler: q.Handler, ResponseHeaders: map[string][]string{}}
	return q.sharededQuerier, nil
}

func (q *ShardedQueryable) getResponseHeaders() []*PrometheusResponseHeader {
	q.sharededQuerier.ResponseHeadersMtx.Lock()
	defer q.sharededQuerier.ResponseHeadersMtx.Unlock()

	return headersMapToPrometheusResponseHeaders(q.sharededQuerier.ResponseHeaders)
}

// ShardedQuerier is a an implementor of the Querier interface.
type ShardedQuerier struct {
	Ctx                context.Context
	Req                Request
	Handler            Handler
	ResponseHeaders    map[string][]string
	ResponseHeadersMtx sync.Mutex
}

// Select returns a set of series that matches the given label matchers.
// The bool passed is ignored because the series is always sorted.
func (q *ShardedQuerier) Select(_ bool, _ *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
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
		return storage.ErrSeriesSet(errors.Errorf(missingEmbeddedQueryMsg))

	}

	return storage.ErrSeriesSet(errors.Errorf(nonEmbeddedErrMsg))
}

// handleEmbeddedQuery defers execution of an encoded query to a downstream Handler
func (q *ShardedQuerier) handleEmbeddedQuery(encoded string) storage.SeriesSet {
	queries, err := astmapper.JSONCodec.Decode(encoded)
	if err != nil {
		return storage.ErrSeriesSet(err)
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
			q.setResponseHeaders(resp.(*PrometheusResponse).Headers)
			samplesCh <- streams
		}(query)
	}

	var samples []SampleStream

	for i := 0; i < len(queries); i++ {
		select {
		case err := <-errCh:
			return storage.ErrSeriesSet(err)
		case streams := <-samplesCh:
			samples = append(samples, streams...)
		}
	}

	return NewSeriesSet(samples)
}

func (q *ShardedQuerier) setResponseHeaders(headers []*PrometheusResponseHeader) {
	q.ResponseHeadersMtx.Lock()
	defer q.ResponseHeadersMtx.Unlock()

	for _, header := range headers {
		if _, ok := q.ResponseHeaders[header.Name]; !ok {
			q.ResponseHeaders[header.Name] = header.Values
		} else {
			q.ResponseHeaders[header.Name] = append(q.ResponseHeaders[header.Name], header.Values...)
		}
	}
}

// LabelValues returns all potential values for a label name.
func (q *ShardedQuerier) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
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

func headersMapToPrometheusResponseHeaders(headersMap map[string][]string) (prs []*PrometheusResponseHeader) {
	for h, v := range headersMap {
		prs = append(prs, &PrometheusResponseHeader{Name: h, Values: v})
	}

	return
}
