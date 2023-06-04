package tsdb

import (
	"bytes"
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

func NewCachedPostingsTSDBIndex(reader IndexReader) Index {
	return &cachedPostingsTSDBIndex{
		reader:        reader,
		Index:         NewTSDBIndex(reader),
		postingsCache: make(map[string]map[string]index.Postings),
	}
}

type cachedPostingsTSDBIndex struct {
	reader      IndexReader
	chunkFilter chunk.RequestChunkFilterer
	Index

	// postingsCache maps shards -> matchers -> postings.
	postingsCache map[string]map[string]index.Postings
}

func matchersToString(matchers []*labels.Matcher) string {
	var b bytes.Buffer

	b.WriteByte('{')
	for i, l := range matchers {
		if i > 0 {
			b.WriteByte(',')
			b.WriteByte(' ')
		}
		b.WriteString(l.String())
	}
	b.WriteByte('}')
	return b.String()
}

func (c *cachedPostingsTSDBIndex) ForSeries(ctx context.Context, shard *index.ShardAnnotation, from model.Time, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta), matchers ...*labels.Matcher) error {
	var ls labels.Labels
	chks := ChunkMetasPool.Get()
	defer ChunkMetasPool.Put(chks)

	var filterer chunk.Filterer
	if c.chunkFilter != nil {
		filterer = c.chunkFilter.ForRequest(ctx)
	}

	return c.forPostings(ctx, shard, from, through, matchers, func(p index.Postings) error {
		for p.Next() {
			hash, err := c.reader.Series(p.At(), int64(from), int64(through), &ls, &chks)
			if err != nil {
				return err
			}

			// skip series that belong to different shards
			if shard != nil && !shard.Match(model.Fingerprint(hash)) {
				continue
			}

			if filterer != nil && filterer.ShouldFilter(ls) {
				continue
			}

			fn(ls, model.Fingerprint(hash), chks)
		}
		return p.Err()
	})
}

func (c *cachedPostingsTSDBIndex) forPostings(
	ctx context.Context,
	shard *index.ShardAnnotation,
	from, through model.Time,
	matchers []*labels.Matcher,
	fn func(index.Postings) error,
) error {
	matchersStr := matchersToString(matchers)
	var shardStr string
	if shard != nil {
		shardStr = shard.String()
	}

	if _, ok := c.postingsCache[shardStr]; !ok {
		c.postingsCache[shardStr] = make(map[string]index.Postings)
	}

	if postings, ok := c.postingsCache[shardStr][matchersStr]; ok {
		return fn(postings)
	}

	p, err := PostingsForMatchers(c.reader, shard, matchers...)
	if err != nil {
		return err
	}
	c.postingsCache[shardStr][matchersStr] = p
	return fn(p)
}

func (c *cachedPostingsTSDBIndex) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, shard *index.ShardAnnotation, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	return c.forPostings(ctx, shard, from, through, matchers, func(p index.Postings) error {
		// TODO(owen-d): use pool
		var ls labels.Labels
		var filterer chunk.Filterer
		if c.chunkFilter != nil {
			filterer = c.chunkFilter.ForRequest(ctx)
		}

		for p.Next() {
			fp, stats, err := c.reader.ChunkStats(p.At(), int64(from), int64(through), &ls)
			if err != nil {
				return err
			}

			// skip series that belong to different shards
			if shard != nil && !shard.Match(model.Fingerprint(fp)) {
				continue
			}

			if filterer != nil && filterer.ShouldFilter(ls) {
				continue
			}

			if stats.Entries > 0 {
				// need to add stream
				acc.AddStream(model.Fingerprint(fp))
				acc.AddChunkStats(stats)
			}
		}
		return p.Err()
	})
}
