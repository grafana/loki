package chunk

import (
	"time"

	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/mtime"
)

type schemaCaching struct {
	Schema

	cacheOlderThan time.Duration
}

func (s *schemaCaching) GetReadQueriesForMetric(from, through model.Time, userID string, metricName string) ([]IndexQuery, error) {
	cFrom, cThrough, from, through := splitTimesByCacheability(from, through, model.TimeFromUnix(mtime.Now().Add(-s.cacheOlderThan).Unix()))

	cacheableQueries, err := s.Schema.GetReadQueriesForMetric(cFrom, cThrough, userID, metricName)
	if err != nil {
		return nil, err
	}

	activeQueries, err := s.Schema.GetReadQueriesForMetric(from, through, userID, metricName)
	if err != nil {
		return nil, err
	}

	return mergeCacheableAndActiveQueries(cacheableQueries, activeQueries), nil
}

func (s *schemaCaching) GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName string, labelName string) ([]IndexQuery, error) {
	cFrom, cThrough, from, through := splitTimesByCacheability(from, through, model.TimeFromUnix(mtime.Now().Add(-s.cacheOlderThan).Unix()))

	cacheableQueries, err := s.Schema.GetReadQueriesForMetricLabel(cFrom, cThrough, userID, metricName, labelName)
	if err != nil {
		return nil, err
	}

	activeQueries, err := s.Schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, labelName)
	if err != nil {
		return nil, err
	}

	return mergeCacheableAndActiveQueries(cacheableQueries, activeQueries), nil
}

func (s *schemaCaching) GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	cFrom, cThrough, from, through := splitTimesByCacheability(from, through, model.TimeFromUnix(mtime.Now().Add(-s.cacheOlderThan).Unix()))

	cacheableQueries, err := s.Schema.GetReadQueriesForMetricLabelValue(cFrom, cThrough, userID, metricName, labelName, labelValue)
	if err != nil {
		return nil, err
	}

	activeQueries, err := s.Schema.GetReadQueriesForMetricLabelValue(from, through, userID, metricName, labelName, labelValue)
	if err != nil {
		return nil, err
	}

	return mergeCacheableAndActiveQueries(cacheableQueries, activeQueries), nil
}

// If the query resulted in series IDs, use this method to find chunks.
func (s *schemaCaching) GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error) {
	cFrom, cThrough, from, through := splitTimesByCacheability(from, through, model.TimeFromUnix(mtime.Now().Add(-s.cacheOlderThan).Unix()))

	cacheableQueries, err := s.Schema.GetChunksForSeries(cFrom, cThrough, userID, seriesID)
	if err != nil {
		return nil, err
	}

	activeQueries, err := s.Schema.GetChunksForSeries(from, through, userID, seriesID)
	if err != nil {
		return nil, err
	}

	return mergeCacheableAndActiveQueries(cacheableQueries, activeQueries), nil
}

func splitTimesByCacheability(from, through model.Time, cacheBefore model.Time) (model.Time, model.Time, model.Time, model.Time) {
	if from.After(cacheBefore) {
		return 0, 0, from, through
	}

	if through.Before(cacheBefore) {
		return from, through, 0, 0
	}

	return from, cacheBefore, cacheBefore, through
}

func mergeCacheableAndActiveQueries(cacheableQueries []IndexQuery, activeQueries []IndexQuery) []IndexQuery {
	finalQueries := make([]IndexQuery, 0, len(cacheableQueries)+len(activeQueries))

Outer:
	for _, cq := range cacheableQueries {
		for _, aq := range activeQueries {
			// When deduping, the bucket values only influence TableName and HashValue
			// and just checking those is enough.
			if cq.TableName == aq.TableName && cq.HashValue == aq.HashValue {
				continue Outer
			}
		}

		cq.Immutable = true
		finalQueries = append(finalQueries, cq)
	}

	finalQueries = append(finalQueries, activeQueries...)

	return finalQueries
}
