package stores

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/go-kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/errors"
	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type StoreLimits interface {
	MaxChunksPerQueryFromStore(string) int
	MaxQueryLength(context.Context, string) time.Duration
}

type compositeStoreEntry struct {
	start model.Time
	Store
}

type storeEntry struct {
	limits      StoreLimits
	stop        func()
	fetcher     *fetcher.Fetcher
	indexReader index.Reader
	ChunkWriter
}

func (c *storeEntry) GetChunks(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, nil, err
	} else if shortcut {
		return nil, nil, nil
	}

	refs, err := c.indexReader.GetChunkRefs(ctx, userID, from, through, predicate)

	chunks := make([]chunk.Chunk, len(refs))
	for i, ref := range refs {
		chunks[i] = chunk.Chunk{
			ChunkRef: ref,
		}
	}

	return [][]chunk.Chunk{chunks}, []*fetcher.Fetcher{c.fetcher}, err
}

func (c *storeEntry) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	return c.indexReader.GetSeries(ctx, userID, from, through, matchers...)
}

func (c *storeEntry) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	c.indexReader.SetChunkFilterer(chunkFilter)
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c *storeEntry) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "SeriesStore.LabelNamesForMetricName")
	defer sp.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Span.Finish()

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}
	level.Debug(log).Log("metric", metricName)

	return c.indexReader.LabelNamesForMetricName(ctx, userID, from, through, metricName)
}

func (c *storeEntry) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "SeriesStore.LabelValuesForMetricName")
	defer sp.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Span.Finish()

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}

	return c.indexReader.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName, matchers...)
}

func (c *storeEntry) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}

	return c.indexReader.Stats(ctx, userID, from, through, matchers...)
}

func (c *storeEntry) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "SeriesStore.Volume")
	defer sp.Finish()

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}

	sp.LogKV(
		"user", userID,
		"from", from.Time(),
		"through", through.Time(),
		"matchers", syntax.MatchersString(matchers),
		"err", err,
		"limit", limit,
		"aggregateBy", aggregateBy,
	)

	return c.indexReader.Volume(ctx, userID, from, through, limit, targetLabels, aggregateBy, matchers...)
}

func (c *storeEntry) GetShards(
	ctx context.Context,
	userID string,
	from, through model.Time,
	targetBytesPerShard uint64,
	predicate chunk.Predicate,
) (*logproto.ShardsResponse, error) {
	_, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	}

	return c.indexReader.GetShards(ctx, userID, from, through, targetBytesPerShard, predicate)
}

func (c *storeEntry) HasForSeries(from, through model.Time) (sharding.ForSeries, bool) {
	return c.indexReader.HasForSeries(from, through)
}

func (c *storeEntry) validateQueryTimeRange(ctx context.Context, userID string, from *model.Time, through *model.Time) (bool, error) {
	//nolint:ineffassign,staticcheck //Leaving ctx even though we don't currently use it, we want to make it available for when we might need it and hopefully will ensure us using the correct context at that time

	if *through < *from {
		return false, errors.QueryError(fmt.Sprintf("invalid query, through < from (%s < %s)", through, from))
	}

	maxQueryLength := c.limits.MaxQueryLength(ctx, userID)
	if maxQueryLength > 0 && (*through).Sub(*from) > maxQueryLength {
		return false, errors.QueryError(fmt.Sprintf(validation.ErrQueryTooLong, model.Duration((*through).Sub(*from)), model.Duration(maxQueryLength)))
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

func (c *storeEntry) GetChunkFetcher(_ model.Time) *fetcher.Fetcher {
	return c.fetcher
}

func (c *storeEntry) Stop() {
	if c.stop != nil {
		c.stop()
	}
}
