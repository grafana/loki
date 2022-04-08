package index

import (
	"time"

	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/mtime"
)

type schemaCaching struct {
	SeriesStoreSchema

	cacheOlderThan time.Duration
}

func NewSchemaCaching(schema SeriesStoreSchema, cacheOlderThan time.Duration) SeriesStoreSchema {
	return &schemaCaching{
		SeriesStoreSchema: schema,
		cacheOlderThan:    cacheOlderThan,
	}
}

func (s *schemaCaching) GetReadQueriesForMetric(from, through model.Time, userID string, metricName string) ([]Query, error) {
	queries, err := s.SeriesStoreSchema.GetReadQueriesForMetric(from, through, userID, metricName)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

func (s *schemaCaching) GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName string, labelName string) ([]Query, error) {
	queries, err := s.SeriesStoreSchema.GetReadQueriesForMetricLabel(from, through, userID, metricName, labelName)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

func (s *schemaCaching) GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName string, labelName string, labelValue string) ([]Query, error) {
	queries, err := s.SeriesStoreSchema.GetReadQueriesForMetricLabelValue(from, through, userID, metricName, labelName, labelValue)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

// If the query resulted in series IDs, use this method to find chunks.
func (s *schemaCaching) GetChunksForSeries(from, through model.Time, userID string, seriesID []byte) ([]Query, error) {
	queries, err := s.SeriesStoreSchema.GetChunksForSeries(from, through, userID, seriesID)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

func (s *schemaCaching) GetLabelNamesForSeries(from, through model.Time, userID string, seriesID []byte) ([]Query, error) {
	queries, err := s.SeriesStoreSchema.GetLabelNamesForSeries(from, through, userID, seriesID)
	if err != nil {
		return nil, err
	}
	return s.setImmutability(from, through, queries), nil
}

func (s *schemaCaching) setImmutability(_, through model.Time, queries []Query) []Query {
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
