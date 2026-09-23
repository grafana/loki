package tsdb

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const postingsCodecVersion = "v1"

type postingsCache struct {
	cache.Cache
	logger  log.Logger
	metrics *postingsCacheMetrics
}

type postingsCacheMetrics struct {
	storeFailures  prometheus.Counter
	decodeFailures prometheus.Counter
	encodeFailures prometheus.Counter
}

func newPostingsCache(c cache.Cache, name string, reg prometheus.Registerer, logger log.Logger) *postingsCache {
	return &postingsCache{c, logger, newPostingsCacheMetrics(name, reg)}
}

func newPostingsCacheMetrics(name string, reg prometheus.Registerer) *postingsCacheMetrics {
	return &postingsCacheMetrics{
		storeFailures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        "loki_tsdb_postings_cache_store_failures_total",
			Help:        "Total number of failed postings cache Store calls.",
			ConstLabels: prometheus.Labels{"name": name},
		}),
		decodeFailures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        "loki_tsdb_postings_cache_decode_failures_total",
			Help:        "Total number of cached postings payloads that failed to decode.",
			ConstLabels: prometheus.Labels{"name": name},
		}),
		encodeFailures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        "loki_tsdb_postings_cache_encode_failures_total",
			Help:        "Total number of postings payloads that failed to encode for the cache.",
			ConstLabels: prometheus.Labels{"name": name},
		}),
	}
}

func objectIdentity(prefix, table, tenant, filename string) string {
	return encodeKeyParts(prefix, table, tenant, filename)
}

func encodeKeyParts(parts ...string) string {
	size := len(parts) * binary.MaxVarintLen64
	for _, part := range parts {
		size += len(part)
	}

	buf := make([]byte, 0, size)
	for _, part := range parts {
		buf = binary.AppendUvarint(buf, uint64(len(part)))
		buf = append(buf, part...)
	}
	return string(buf)
}

func postingsKey(identity string, fpFilter index.FingerprintFilter, matchers []*labels.Matcher) string {
	canonical := make([]labels.Matcher, len(matchers))
	for i, matcher := range matchers {
		canonical[i] = labels.Matcher{Type: matcher.Type, Name: matcher.Name, Value: matcher.Value}
	}
	slices.SortFunc(canonical, func(a, b labels.Matcher) int {
		if result := strings.Compare(a.Name, b.Name); result != 0 {
			return result
		}
		if result := int(a.Type - b.Type); result != 0 {
			return result
		}
		return strings.Compare(a.Value, b.Value)
	})

	const matcherTypeLen = 2
	size := 0
	for _, matcher := range canonical {
		size += len(matcher.Name) + matcherTypeLen + binary.Size(uint64(len(matcher.Value))) + len(matcher.Value)
	}
	canonicalMatchers := make([]byte, 0, size)
	for _, matcher := range canonical {
		canonicalMatchers = append(canonicalMatchers, matcher.Name...)
		canonicalMatchers = append(canonicalMatchers, matcher.Type.String()...)
		// A fixed-width value length prevents ambiguity with matcher operators.
		// Appending a uint64 cannot fail.
		canonicalMatchers, _ = binary.Append(canonicalMatchers, binary.BigEndian, uint64(len(matcher.Value)))
		canonicalMatchers = append(canonicalMatchers, matcher.Value...)
	}

	shard := "all"
	if fpFilter != nil {
		shardFrom, shardThrough := fpFilter.GetFromThrough()
		buf := make([]byte, 0, 2*binary.MaxVarintLen64)
		buf = binary.AppendUvarint(buf, uint64(shardFrom))
		buf = binary.AppendUvarint(buf, uint64(shardThrough))
		shard = string(buf)
	}
	return encodeKeyParts(identity, string(canonicalMatchers), shard)
}

func encodePostings(key string, refs []storage.SeriesRef) ([]byte, error) {
	// Most deltas encode near one byte, so reserve 1.25 bytes per reference.
	estimatedPostingsBytes := 5 * len(refs) / 4
	payload := make([]byte, 0, estimatedPostingsBytes)
	var previous storage.SeriesRef
	for _, ref := range refs {
		if ref < previous {
			return nil, fmt.Errorf("postings references are not sorted: %d follows %d", ref, previous)
		}
		payload = binary.AppendUvarint(payload, uint64(ref-previous))
		previous = ref
	}
	// Reserve the full output so Snappy can compress directly after the key.
	capacity := len(postingsCodecVersion) + binary.MaxVarintLen64 + len(key) + snappy.MaxEncodedLen(len(payload))
	encoded := make([]byte, capacity)
	offset := copy(encoded, postingsCodecVersion)
	offset += binary.PutUvarint(encoded[offset:], uint64(len(key)))
	offset += copy(encoded[offset:], key)

	compressed := snappy.Encode(encoded[offset:], payload)
	return encoded[:offset+len(compressed)], nil
}

func decodePostings(key string, encoded []byte) ([]storage.SeriesRef, error) {
	if !strings.HasPrefix(string(encoded), postingsCodecVersion) {
		return nil, fmt.Errorf("unsupported postings codec version")
	}
	encoded = encoded[len(postingsCodecVersion):]
	keyLen, n := binary.Uvarint(encoded)
	if n <= 0 || keyLen > uint64(len(encoded)-n) {
		return nil, fmt.Errorf("invalid postings cache key length")
	}
	encoded = encoded[n:]
	if string(encoded[:keyLen]) != key {
		return nil, fmt.Errorf("postings cache key mismatch")
	}
	payload, err := snappy.Decode(nil, encoded[keyLen:])
	if err != nil {
		return nil, fmt.Errorf("decompress postings: %w", err)
	}
	var refs []storage.SeriesRef
	var previous storage.SeriesRef
	for len(payload) > 0 {
		delta, n := binary.Uvarint(payload)
		if n <= 0 {
			return nil, fmt.Errorf("invalid postings delta varint")
		}
		previous += storage.SeriesRef(delta)
		refs = append(refs, previous)
		payload = payload[n:]
	}
	return refs, nil
}

func (c *postingsCache) cachedPostings(ctx context.Context, key string, compute func() (index.Postings, error)) (index.Postings, error) {
	found, bufs, _, err := c.Fetch(ctx, []string{cache.HashKey(key)})
	// Avoid a write after a failed fetch.
	writeCache := err == nil
	if err == nil && len(found) == 1 && len(bufs) == 1 {
		refs, err := decodePostings(key, bufs[0])
		if err == nil {
			return index.NewListPostings(refs), nil
		}
		c.metrics.decodeFailures.Inc()
		level.Warn(c.logger).Log("msg", "failed to decode cached postings", "err", err)
	}

	postings, err := compute()
	if err != nil {
		return nil, err
	}
	refs, err := index.ExpandPostings(postings)
	if err != nil {
		return nil, err
	}
	slices.Sort(refs)
	if writeCache {
		encoded, err := encodePostings(key, refs)
		if err != nil {
			c.metrics.encodeFailures.Inc()
			level.Warn(c.logger).Log("msg", "failed to encode postings for cache", "err", err)
			return index.NewListPostings(refs), nil
		}
		if err := c.Store(ctx, []string{cache.HashKey(key)}, [][]byte{encoded}); err != nil {
			c.metrics.storeFailures.Inc()
			level.Warn(c.logger).Log("msg", "failed to store postings in cache", "err", err)
		}
	}
	return index.NewListPostings(refs), nil
}
