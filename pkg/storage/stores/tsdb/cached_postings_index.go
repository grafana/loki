package tsdb

import (
	"context"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/indexcache"
)

var cacheClient cache.Cache

func NewCachedPostingsClient(reader IndexReader) PostingsClient {
	return &cachedPostingsClient{
		reader:      reader,
		cacheClient: cacheClient,
	}
}

type PostingsClient interface {
	ForPostings(ctx context.Context, matchers []*labels.Matcher, fn func(index.Postings) error) error
}

type cachedPostingsClient struct {
	reader      IndexReader
	chunkFilter chunk.RequestChunkFilterer

	cacheClient      cache.Cache
	IndexCacheClient indexcache.Client
}

func (c *cachedPostingsClient) ForPostings(ctx context.Context, matchers []*labels.Matcher, fn func(index.Postings) error) error {
	if postings, got := c.IndexCacheClient.FetchPostings(matchers); got {
		return fn(postings)
	}

	p, err := PostingsForMatchers(c.reader, nil, matchers...)
	if err != nil {
		return err
	}

	c.IndexCacheClient.StorePostings(matchers, p)
	return fn(p)
}
