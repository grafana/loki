package chunk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/util/validation"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type indexStore struct {
	index  IndexClient
	limits StoreLimits
	schema BaseSchema
}

func newIndexStore(index IndexClient, limits StoreLimits, schema BaseSchema) *indexStore {
	return &indexStore{
		index:  index,
		limits: limits,
		schema: schema,
	}
}

func (c *indexStore) validateQueryTimeRange(ctx context.Context, userID string, from *model.Time, through *model.Time) (bool, error) {
	//nolint:ineffassign,staticcheck //Leaving ctx even though we don't currently use it, we want to make it available for when we might need it and hopefully will ensure us using the correct context at that time

	if *through < *from {
		return false, QueryError(fmt.Sprintf("invalid query, through < from (%s < %s)", through, from))
	}

	maxQueryLength := c.limits.MaxQueryLength(userID)
	if maxQueryLength > 0 && (*through).Sub(*from) > maxQueryLength {
		return false, QueryError(fmt.Sprintf(validation.ErrQueryTooLong, (*through).Sub(*from), maxQueryLength))
	}

	now := model.Now()

	if from.After(now) {
		// time-span start is in future ... regard as legal
		level.Info(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "whole timerange in future, yield empty resultset", "through", through, "from", from, "now", now)
		return true, nil
	}

	if through.After(now.Add(5 * time.Minute)) {
		// time-span end is in future ... regard as legal
		level.Info(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "adjusting end timerange from future to now", "old_through", through, "new_through", now)
		*through = now // Avoid processing future part - otherwise some schemas could fail with eg non-existent table gripes
	}

	return false, nil
}

func (c *indexStore) lookupEntriesByQueries(ctx context.Context, queries []IndexQuery) ([]IndexEntry, error) {
	// Nothing to do if there are no queries.
	if len(queries) == 0 {
		return nil, nil
	}

	var lock sync.Mutex
	var entries []IndexEntry
	err := c.index.QueryPages(ctx, queries, func(query IndexQuery, resp ReadBatch) bool {
		iter := resp.Iterator()
		lock.Lock()
		for iter.Next() {
			entries = append(entries, IndexEntry{
				TableName:  query.TableName,
				HashValue:  query.HashValue,
				RangeValue: iter.RangeValue(),
				Value:      iter.Value(),
			})
		}
		lock.Unlock()
		return true
	})
	if err != nil {
		level.Error(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "error querying storage", "err", err)
	}
	return entries, err
}

func (c *indexStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "ChunkStore.LabelValues")
	defer log.Span.Finish()
	level.Debug(log).Log("from", from, "through", through, "metricName", metricName, "labelName", labelName)

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}

	if len(matchers) == 0 {
		queries, err := c.schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, labelName)
		if err != nil {
			return nil, err
		}

		entries, err := c.lookupEntriesByQueries(ctx, queries)
		if err != nil {
			return nil, err
		}

		var result UniqueStrings
		for _, entry := range entries {
			_, labelValue, err := parseChunkTimeRangeValue(entry.RangeValue, entry.Value)
			if err != nil {
				return nil, err
			}
			result.Add(string(labelValue))
		}
		return result.Strings(), nil
	}

	// NOTE
	return nil, errors.New("unimplemented: Matchers are not supported by chunk store")

}
