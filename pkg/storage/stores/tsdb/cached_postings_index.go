package tsdb

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

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

// diffVarintEncodeNoHeader encodes postings into diff+varint representation.
// It doesn't add any header to the output bytes.
// Length argument is expected number of postings, used for preallocating buffer.
func diffVarintEncodeNoHeader(p []storage.SeriesRef) ([]byte, error) {
	length := len(p)

	buf := encoding.Encbuf{}
	buf.PutUvarint32(uint32(length))

	// This encoding uses around ~1 bytes per posting, but let's use
	// conservative 1.25 bytes per posting to avoid extra allocations.
	if length > 0 {
		buf.B = make([]byte, 0, binary.MaxVarintLen32+5*length/4)
	}

	buf.PutUvarint32(uint32(length)) // first we put the postings length used when decoding.

	prev := storage.SeriesRef(0)
	for _, ref := range p {
		if ref < prev {
			return nil, errors.Errorf("postings entries must be in increasing order, current: %d, previous: %d", ref, prev)
		}

		// This is the 'diff' part -- compute difference from previous value.
		buf.PutUvarint32(uint32(ref - prev))
		prev = ref
	}

	return buf.Get(), nil
}

func decodeToPostings(b []byte) index.Postings {
	if len(b) <= 0 {
		return index.EmptyPostings()
	}

	decoder := encoding.DecWrap(promEncoding.Decbuf{B: b})
	postingsLen := decoder.Uvarint32()
	refs := make([]storage.SeriesRef, 0, postingsLen)
	prev := storage.SeriesRef(0)

	for i := 0; i < int(postingsLen); i++ {
		v := storage.SeriesRef(decoder.Uvarint32()) + prev
		refs = append(refs, v)
		prev = v
	}

	return index.NewListPostings(refs)
}

func (c *cachedPostingsReader) storePostings(ctx context.Context, expandedPostings []storage.SeriesRef, canonicalMatchers string) error {
	dataToCache, err := diffVarintEncodeNoHeader(expandedPostings)
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
		// we only use a single key so we only care about index=0.
		return decodeToPostings(bufs[0]), true
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
		sb.WriteByte(',')
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
