// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/thanos-io/thanos/blob/main/pkg/store/postings_codec.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Thanos Authors.

package tsdb

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type PostingsReader interface {
	ForPostings(ctx context.Context, matchers []*labels.Matcher, fn func(index.Postings) error) error
}

// NewCachedPostingsReader uses the cache defined by `index_read_cache` to store and read Postings.
//
// The cache key is stored/read as `matchers:reader_checksum`.
//
// The cache value is stored as: `[n, refs...]`, where n is how many series references this entry has, and refs is
// a sequence of series references encoded as the diff between the current series and the previous one.
//
// Example: if the postings for stream "app=kubernetes,env=production" is `[1,7,30,50]` and its reader has `checksum=12345`:
// - The cache key for the entry will be: `app=kubernetes,env=production:12345`
// - The cache value for the entry will be: [4, 1, 6, 23, 20].
func NewCachedPostingsReader(reader IndexReader, logger log.Logger, cacheClient cache.Cache) PostingsReader {
	return &cachedPostingsReader{
		reader:      reader,
		cacheClient: cacheClient,
		log:         logger,
	}
}

type cachedPostingsReader struct {
	reader IndexReader

	cacheClient cache.Cache

	log log.Logger
}

func (c *cachedPostingsReader) ForPostings(ctx context.Context, matchers []*labels.Matcher, fn func(index.Postings) error) error {
	checksum := c.reader.Checksum()
	key := fmt.Sprintf("%s:%d", CanonicalLabelMatchersKey(matchers), checksum)
	if postings, got := c.fetchPostings(ctx, key); got {
		return fn(postings)
	}

	p, err := PostingsForMatchers(c.reader, nil, matchers...)
	if err != nil {
		return fmt.Errorf("failed to evaluate postings for matchers: %w", err)
	}

	expandedPosts, err := index.ExpandPostings(p)
	if err != nil {
		return fmt.Errorf("failed to expand postings: %w", err)
	}

	if err := c.storePostings(ctx, expandedPosts, key); err != nil {
		level.Error(c.log).Log("msg", "failed to cache postings", "err", err, "matchers", key)
	}

	// because `index.ExpandPostings` walks the iterator, we have to reset it current index by instantiating a new ListPostings.
	return fn(index.NewListPostings(expandedPosts))
}

func (c *cachedPostingsReader) storePostings(ctx context.Context, expandedPostings []storage.SeriesRef, canonicalMatchers string) error {
	buf, err := diffVarintSnappyEncode(index.NewListPostings(expandedPostings), len(expandedPostings))
	if err != nil {
		return fmt.Errorf("couldn't encode postings: %w", err)
	}

	return c.cacheClient.Store(ctx, []string{canonicalMatchers}, [][]byte{buf})
}

func (c *cachedPostingsReader) fetchPostings(ctx context.Context, key string) (index.Postings, bool) {
	found, bufs, _, err := c.cacheClient.Fetch(ctx, []string{key})

	if err != nil {
		level.Error(c.log).Log("msg", "error on fetching postings", "err", err, "matchers", key)
		return nil, false
	}

	if len(found) > 0 {
		// we only use a single key so we only care about index=0.
		p, err := decodeToPostings(bufs[0])
		if err != nil {
			level.Error(c.log).Log("msg", "failed to fetch postings", "err", err)
			return nil, false
		}
		return p, true
	}

	return nil, false
}

func decodeToPostings(b []byte) (index.Postings, error) {
	p, err := diffVarintSnappyDecode(b)
	if err != nil {
		return nil, fmt.Errorf("couldn't decode postings: %w", err)
	}

	return p, nil
}

// CanonicalLabelMatchersKey creates a canonical version of LabelMatchersKey
func CanonicalLabelMatchersKey(ms []*labels.Matcher) string {
	sorted := make([]labels.Matcher, len(ms))
	for i := range ms {
		sorted[i] = labels.Matcher{Type: ms[i].Type, Name: ms[i].Name, Value: ms[i].Value}
	}
	sort.Sort(sorteableLabelMatchers(sorted))

	const (
		typeLen = 2
		sepLen  = 1
	)
	var size int
	for _, m := range sorted {
		size += len(m.Name) + len(m.Value) + typeLen + sepLen
	}
	sb := strings.Builder{}
	sb.Grow(size)
	for _, m := range sorted {
		sb.WriteString(m.Name)
		sb.WriteString(m.Type.String())
		sb.WriteString(m.Value)
		sb.WriteByte(',')
	}
	return sb.String()
}

type sorteableLabelMatchers []labels.Matcher

func (c sorteableLabelMatchers) Less(i, j int) bool {
	if c[i].Name != c[j].Name {
		return c[i].Name < c[j].Name
	}
	if c[i].Type != c[j].Type {
		return c[i].Type < c[j].Type
	}
	return c[i].Value < c[j].Value
}

func (c sorteableLabelMatchers) Len() int      { return len(c) }
func (c sorteableLabelMatchers) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
