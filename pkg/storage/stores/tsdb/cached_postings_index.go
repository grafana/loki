package tsdb

import (
	"context"
	"encoding/binary"
	"sort"
	"strings"

	"github.com/dennwc/varint"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	promEncoding "github.com/prometheus/prometheus/tsdb/encoding"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/encoding"
)

type PostingsReader interface {
	ForPostings(ctx context.Context, matchers []*labels.Matcher, fn func(index.Postings) error) error
}

var sharedCacheClient cache.Cache

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
	key := CanonicalLabelMatchersKey(matchers)
	if postings, got := c.fetchPostings(ctx, key); got {
		return fn(postings)
	}

	p, err := PostingsForMatchers(c.reader, nil, matchers...)
	if err != nil {
		return err
	}

	expandedPosts, err := index.ExpandPostings(p)
	if err != nil {
		return err
	}

	if err := c.storePostings(ctx, expandedPosts, key); err != nil {
		level.Error(c.log).Log("msg", "failed to cache postings", "err", err, "matchers", key)
	}

	// `index.ExpandedPostings` makes the iterator to walk, so we have to reset it by instantiating a new NewListPostings.
	return fn(index.NewListPostings(expandedPosts))
}

// diffVarintEncodeNoHeader encodes postings into diff+varint representation.
// It doesn't add any header to the output bytes.
// Length argument is expected number of postings, used for preallocating buffer.
func diffVarintEncodeNoHeader(p []storage.SeriesRef, length int) ([]byte, error) {
	buf := encoding.Encbuf{}
	buf.PutUvarint64(uint64(length))

	// This encoding uses around ~1 bytes per posting, but let's use
	// conservative 1.25 bytes per posting to avoid extra allocations.
	if length > 0 {
		buf.B = make([]byte, 0, binary.MaxVarintLen64+5*length/4)
	}

	buf.PutUvarint64(uint64(length)) // first we put the postings length so we can use it when decoding.

	prev := storage.SeriesRef(0)
	for _, ref := range p {
		if ref < prev {
			return nil, errors.Errorf("postings entries must be in increasing order, current: %d, previous: %d", ref, prev)
		}

		// This is the 'diff' part -- compute difference from previous value.
		buf.PutUvarint64(uint64(ref - prev))
		prev = ref
	}

	return buf.B, nil
}

func decodeToPostings(b []byte) index.Postings {
	decoder := encoding.DecWrap(promEncoding.Decbuf{B: b})
	postingsLen := decoder.Uvarint64()
	refs := make([]storage.SeriesRef, 0, postingsLen)
	prev := storage.SeriesRef(0)

	for i := 0; i < int(postingsLen); i++ {
		v := storage.SeriesRef(decoder.Uvarint64())
		refs = append(refs, v+prev)
		prev = v
	}

	return index.NewListPostings(refs)
}

func encodedMatchersLen(matchers []*labels.Matcher) int {
	matchersLen := varint.UvarintSize(uint64(len(matchers)))
	for _, m := range matchers {
		matchersLen += varint.UvarintSize(uint64(len(m.Name)))
		matchersLen += len(m.Name)
		matchersLen++ // 1 byte for the type
		matchersLen += varint.UvarintSize(uint64(len(m.Value)))
		matchersLen += len(m.Value)
	}
	return matchersLen
}

func (c *cachedPostingsReader) storePostings(ctx context.Context, expandedPostings []storage.SeriesRef, canonicalMatchers string) error {
	dataToCache, err := diffVarintEncodeNoHeader(expandedPostings, len(expandedPostings))
	if err != nil {
		level.Warn(c.log).Log("msg", "couldn't encode postings", "err", err, "matchers", canonicalMatchers)
	}

	return c.cacheClient.Store(ctx, []string{canonicalMatchers}, [][]byte{dataToCache})
}

func (c *cachedPostingsReader) fetchPostings(ctx context.Context, key string) (index.Postings, bool) {
	found, bufs, _, err := c.cacheClient.Fetch(ctx, []string{key})

	if err != nil {
		level.Error(c.log).Log("msg", "error on fetching postings", "err", err, "matchers", key)
		return nil, false
	}

	if len(found) > 0 {
		var postings []index.Postings
		for _, b := range bufs {
			postings = append(postings, decodeToPostings(b))
		}

		return index.Merge(postings...), true
	}

	return nil, false
}

// CanonicalLabelMatchersKey creates a canonical version of LabelMatchersKey
func CanonicalLabelMatchersKey(ms []*labels.Matcher) string {
	sorted := make([]labels.Matcher, len(ms))
	for i := range ms {
		sorted[i] = labels.Matcher{Type: ms[i].Type, Name: ms[i].Name, Value: ms[i].Value}
	}
	sort.Sort(sortedLabelMatchers(sorted))

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
		sb.WriteByte(0)
	}
	return sb.String()
}

type sortedLabelMatchers []labels.Matcher

func (c sortedLabelMatchers) Less(i, j int) bool {
	if c[i].Name != c[j].Name {
		return c[i].Name < c[j].Name
	}
	if c[i].Type != c[j].Type {
		return c[i].Type < c[j].Type
	}
	return c[i].Value < c[j].Value
}

func (c sortedLabelMatchers) Len() int      { return len(c) }
func (c sortedLabelMatchers) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
