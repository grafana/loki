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
	return s.splitTimesByCacheability(from, through, func(from, through model.Time) ([]IndexQuery, error) {
		return s.Schema.GetReadQueriesForMetric(from, through, userID, metricName)
	})
}

func (s *schemaCaching) GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName string, labelName string) ([]IndexQuery, error) {
	return s.splitTimesByCacheability(from, through, func(from, through model.Time) ([]IndexQuery, error) {
		return s.Schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, labelName)
	})
}

func (s *schemaCaching) GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	return s.splitTimesByCacheability(from, through, func(from, through model.Time) ([]IndexQuery, error) {
		return s.Schema.GetReadQueriesForMetricLabelValue(from, through, userID, metricName, labelName, labelValue)
	})
}

// If the query resulted in series IDs, use this method to find chunks.
func (s *schemaCaching) GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error) {
	return s.splitTimesByCacheability(from, through, func(from, through model.Time) ([]IndexQuery, error) {
		return s.Schema.GetChunksForSeries(from, through, userID, seriesID)
	})
}

func (s *schemaCaching) splitTimesByCacheability(from, through model.Time, f func(from, through model.Time) ([]IndexQuery, error)) ([]IndexQuery, error) {
	var (
		cacheableQueries []IndexQuery
		activeQueries    []IndexQuery
		err              error
		cacheBefore      = model.TimeFromUnix(mtime.Now().Add(-s.cacheOlderThan).Unix())
	)

	if from.After(cacheBefore) {
		activeQueries, err = f(from, through)
		if err != nil {
			return nil, err
		}
	} else if through.Before(cacheBefore) {
		cacheableQueries, err = f(from, through)
		if err != nil {
			return nil, err
		}
	} else {
		cacheableQueries, err = f(from, cacheBefore)
		if err != nil {
			return nil, err
		}

		activeQueries, err = f(cacheBefore, through)
		if err != nil {
			return nil, err
		}
	}

	return mergeCacheableAndActiveQueries(cacheableQueries, activeQueries), nil
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
