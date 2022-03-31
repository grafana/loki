package stores

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk/encoding"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/stores/errors"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/util/validation"
)

var _ Store = &compositeStore{}

type StoreLimits interface {
	MaxChunksPerQueryFromStore(userID string) int
	MaxQueryLength(userID string) time.Duration
}

type Index interface {
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.ChunkRef, error)
	GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.SeriesIdentifier, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error)
}

type ChunkWriter interface {
	Put(ctx context.Context, chunks []encoding.Chunk) error
	PutOne(ctx context.Context, from, through model.Time, chunk encoding.Chunk) error
}

type compositeStoreEntry struct {
	start  model.Time
	limits StoreLimits

	stop    []func()
	fetcher *fetcher.Fetcher
	index   Index
	ChunkWriter
}

func (c *compositeStoreEntry) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([][]encoding.Chunk, []*fetcher.Fetcher, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	log, ctx := spanlogger.New(ctx, "GetChunkRefs")
	defer log.Span.Finish()

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, nil, err
	} else if shortcut {
		return nil, nil, nil
	}

	refs, err := c.index.GetChunkRefs(ctx, userID, from, through, allMatchers...)

	chunks := make([]encoding.Chunk, len(refs))
	for i, ref := range refs {
		chunks[i] = encoding.Chunk{
			ChunkRef: ref,
		}
	}

	return [][]encoding.Chunk{chunks}, []*fetcher.Fetcher{c.fetcher}, err
}

func (c *compositeStoreEntry) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.SeriesIdentifier, error) {
	return c.index.GetSeries(ctx, userID, from, through, matchers...)
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c *compositeStoreEntry) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.LabelNamesForMetricName")
	defer log.Span.Finish()

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}
	level.Debug(log).Log("metric", metricName)

	return c.index.LabelNamesForMetricName(ctx, userID, from, through, metricName)
}

func (c *compositeStoreEntry) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.LabelValuesForMetricName")
	defer log.Span.Finish()

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}

	return c.index.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName, matchers...)
}

func (c *compositeStoreEntry) validateQueryTimeRange(ctx context.Context, userID string, from *model.Time, through *model.Time) (bool, error) {
	//nolint:ineffassign,staticcheck //Leaving ctx even though we don't currently use it, we want to make it available for when we might need it and hopefully will ensure us using the correct context at that time

	if *through < *from {
		return false, errors.QueryError(fmt.Sprintf("invalid query, through < from (%s < %s)", through, from))
	}

	maxQueryLength := c.limits.MaxQueryLength(userID)
	if maxQueryLength > 0 && (*through).Sub(*from) > maxQueryLength {
		return false, errors.QueryError(fmt.Sprintf(validation.ErrQueryTooLong, (*through).Sub(*from), maxQueryLength))
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

func (c *compositeStoreEntry) GetChunkFetcher(tm model.Time) *fetcher.Fetcher {
	return c.fetcher
}

func (c *compositeStoreEntry) Stop() {
	for _, stop := range c.stop {
		stop()
	}
}
