package api

import (
	"context"

	"github.com/gogo/status"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

func translateError(err error) error {
	if err == nil {
		return err
	}

	// vendor/github.com/prometheus/prometheus/web/api/v1/api.go, respondError function only accepts
	// *apiError types.
	// Translation of error to *apiError happens in vendor/github.com/prometheus/prometheus/web/api/v1/api.go, returnAPIError method.
	// It only supports:
	// promql.ErrQueryCanceled, mapped to 503
	// promql.ErrQueryTimeout, mapped to 503
	// promql.ErrStorage mapped to 500
	// anything else is mapped to 422

	switch errors.Cause(err).(type) {
	case promql.ErrStorage, promql.ErrTooManySamples, promql.ErrQueryCanceled, promql.ErrQueryTimeout:
		// Don't translate those, just in case we use them internally.
		return err
	case chunk.QueryError, validation.LimitError:
		// This will be returned with status code 422 by Prometheus API.
		return err
	default:
		if errors.Is(err, context.Canceled) {
			return err // 422
		}

		s, ok := status.FromError(err)

		if !ok {
			s, ok = status.FromError(errors.Cause(err))
		}

		if ok {
			code := s.Code()

			// Treat these as HTTP status codes, even though they are supposed to be grpc codes.
			if code >= 400 && code < 500 {
				// Return directly, will be mapped to 422
				return err
			} else if code >= 500 && code < 599 {
				// Wrap into ErrStorage for mapping to 500
				return promql.ErrStorage{Err: err}
			}
		}

		// All other errors will be returned as 500.
		return promql.ErrStorage{Err: err}
	}
}

type errorTranslateQueryable struct {
	q storage.SampleAndChunkQueryable
}

func (e errorTranslateQueryable) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	q, err := e.q.Querier(ctx, mint, maxt)
	return errorTranslateQuerier{q: q}, translateError(err)
}

func (e errorTranslateQueryable) ChunkQuerier(ctx context.Context, mint, maxt int64) (storage.ChunkQuerier, error) {
	q, err := e.q.ChunkQuerier(ctx, mint, maxt)
	return errorTranslateChunkQuerier{q: q}, translateError(err)
}

type errorTranslateQuerier struct {
	q storage.Querier
}

func (e errorTranslateQuerier) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	values, warnings, err := e.q.LabelValues(name, matchers...)
	return values, warnings, translateError(err)
}

func (e errorTranslateQuerier) LabelNames() ([]string, storage.Warnings, error) {
	values, warnings, err := e.q.LabelNames()
	return values, warnings, translateError(err)
}

func (e errorTranslateQuerier) Close() error {
	return translateError(e.q.Close())
}

func (e errorTranslateQuerier) Select(sortSeries bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	s := e.q.Select(sortSeries, hints, matchers...)
	return errorTranslateSeriesSet{s}
}

type errorTranslateChunkQuerier struct {
	q storage.ChunkQuerier
}

func (e errorTranslateChunkQuerier) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	values, warnings, err := e.q.LabelValues(name, matchers...)
	return values, warnings, translateError(err)
}

func (e errorTranslateChunkQuerier) LabelNames() ([]string, storage.Warnings, error) {
	values, warnings, err := e.q.LabelNames()
	return values, warnings, translateError(err)
}

func (e errorTranslateChunkQuerier) Close() error {
	return translateError(e.q.Close())
}

func (e errorTranslateChunkQuerier) Select(sortSeries bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.ChunkSeriesSet {
	s := e.q.Select(sortSeries, hints, matchers...)
	return errorTranslateChunkSeriesSet{s}
}

type errorTranslateSeriesSet struct {
	s storage.SeriesSet
}

func (e errorTranslateSeriesSet) Next() bool {
	return e.s.Next()
}

func (e errorTranslateSeriesSet) At() storage.Series {
	return e.s.At()
}

func (e errorTranslateSeriesSet) Err() error {
	return translateError(e.s.Err())
}

func (e errorTranslateSeriesSet) Warnings() storage.Warnings {
	return e.s.Warnings()
}

type errorTranslateChunkSeriesSet struct {
	s storage.ChunkSeriesSet
}

func (e errorTranslateChunkSeriesSet) Next() bool {
	return e.s.Next()
}

func (e errorTranslateChunkSeriesSet) At() storage.ChunkSeries {
	return e.s.At()
}

func (e errorTranslateChunkSeriesSet) Err() error {
	return translateError(e.s.Err())
}

func (e errorTranslateChunkSeriesSet) Warnings() storage.Warnings {
	return e.s.Warnings()
}
