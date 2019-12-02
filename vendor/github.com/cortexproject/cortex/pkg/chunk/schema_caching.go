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
	queries, err := s.Schema.GetReadQueriesForMetric(from, through, userID, metricName)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

func (s *schemaCaching) GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName string, labelName string) ([]IndexQuery, error) {
	queries, err := s.Schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, labelName)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

func (s *schemaCaching) GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName string, labelName string, labelValue string) ([]IndexQuery, error) {
	queries, err := s.Schema.GetReadQueriesForMetricLabelValue(from, through, userID, metricName, labelName, labelValue)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

// If the query resulted in series IDs, use this method to find chunks.
func (s *schemaCaching) GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error) {
	queries, err := s.Schema.GetChunksForSeries(from, through, userID, seriesID)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

func (s *schemaCaching) GetLabelNamesForSeries(from, through model.Time, userID string, seriesID []byte) ([]IndexQuery, error) {
	queries, err := s.Schema.GetLabelNamesForSeries(from, through, userID, seriesID)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

func (s *schemaCaching) setImmutability(from, through model.Time, queries []IndexQuery) []IndexQuery {
	cacheBefore := model.TimeFromUnix(mtime.Now().Add(-s.cacheOlderThan).Unix())

	// If the entire query is cacheable then cache it.
	// While not super effective stand-alone, when combined with query-frontend and splitting,
	// old queries will mostly be all behind boundary.
	// To cleanly split cacheable and non-cacheable ranges, we'd need bucket start and end times
	// which we don't know.
	// See: https://github.com/cortexproject/cortex/issues/1698
	if through.Before(cacheBefore) {
		for i := range queries {
			queries[i].Immutable = true
		}
	}

	return queries
}
