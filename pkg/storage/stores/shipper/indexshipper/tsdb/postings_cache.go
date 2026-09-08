package tsdb

import (
	"context"
	"encoding/binary"
	"slices"
	"strings"

	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const (
	postingsCacheVersion     = "postings-v1"
	maxPostingsCacheBytes    = 16 << 20
	maxPostingsCacheKeyBytes = 64 << 10
	maxPostingsCacheRefs     = 1 << 20
)

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

	const (
		matcherTypeLen   = 2
		matcherSeparator = 1
	)
	size := 0
	for _, matcher := range canonical {
		size += len(matcher.Name) + matcherTypeLen + binary.MaxVarintLen64 + len(matcher.Value) + matcherSeparator
	}
	canonicalMatchers := make([]byte, 0, size)
	for _, matcher := range canonical {
		canonicalMatchers = append(canonicalMatchers, matcher.Name...)
		canonicalMatchers = append(canonicalMatchers, matcher.Type.String()...)
		canonicalMatchers = append(canonicalMatchers, 0)
		canonicalMatchers = binary.AppendUvarint(canonicalMatchers, uint64(len(matcher.Value)))
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
	return encodeKeyParts(postingsCacheVersion, identity, string(canonicalMatchers), shard)
}

func encodePostings(key string, refs []storage.SeriesRef) []byte {
	if len(key) > maxPostingsCacheKeyBytes || len(refs) > maxPostingsCacheRefs {
		return nil
	}

	// Most deltas encode near one byte, so reserve 1.25 bytes per reference.
	estimatedPostingsBytes := 5 * len(refs) / 4
	capacity := 1 + 2*binary.MaxVarintLen64 + len(key) + estimatedPostingsBytes
	payload := make([]byte, 0, capacity)
	payload = append(payload, 1)
	payload = binary.AppendUvarint(payload, uint64(len(key)))
	payload = append(payload, key...)
	payload = binary.AppendUvarint(payload, uint64(len(refs)))
	var previous storage.SeriesRef
	for _, ref := range refs {
		if ref < previous {
			return nil
		}
		payload = binary.AppendUvarint(payload, uint64(ref-previous))
		if len(payload) > maxPostingsCacheBytes {
			return nil
		}
		previous = ref
	}
	encoded := snappy.Encode(nil, payload)
	if len(encoded) > maxPostingsCacheBytes {
		return nil
	}
	return encoded
}

func decodePostings(key string, encoded []byte, maxRef storage.SeriesRef) ([]storage.SeriesRef, bool) {
	if len(key) > maxPostingsCacheKeyBytes || len(encoded) == 0 || len(encoded) > maxPostingsCacheBytes {
		return nil, false
	}
	decodedLen, err := snappy.DecodedLen(encoded)
	if err != nil || decodedLen > maxPostingsCacheBytes {
		return nil, false
	}
	payload, err := snappy.Decode(nil, encoded)
	if err != nil || len(payload) == 0 || payload[0] != 1 {
		return nil, false
	}
	payload = payload[1:]
	keyLen, n := binary.Uvarint(payload)
	if n <= 0 || keyLen > uint64(len(payload)-n) {
		return nil, false
	}
	payload = payload[n:]
	if string(payload[:keyLen]) != key {
		return nil, false
	}
	payload = payload[keyLen:]
	count, n := binary.Uvarint(payload)
	if n <= 0 || count > maxPostingsCacheRefs || count > uint64(len(payload)-n) {
		return nil, false
	}
	payload = payload[n:]
	refs := make([]storage.SeriesRef, 0, count)
	var previous storage.SeriesRef
	for range count {
		delta, n := binary.Uvarint(payload)
		if n <= 0 || delta > uint64(^storage.SeriesRef(0)-previous) {
			return nil, false
		}
		previous += storage.SeriesRef(delta)
		if previous > maxRef {
			return nil, false
		}
		refs = append(refs, previous)
		payload = payload[n:]
	}
	return refs, len(payload) == 0
}

func cachedPostings(ctx context.Context, c cache.Cache, key string, maxRef storage.SeriesRef, compute func() (index.Postings, error)) (index.Postings, error) {
	if c != nil && len(key) <= maxPostingsCacheKeyBytes {
		found, bufs, _, err := c.Fetch(ctx, []string{cache.HashKey(key)})
		if err == nil && len(found) == 1 && len(bufs) == 1 {
			if refs, ok := decodePostings(key, bufs[0], maxRef); ok {
				return index.NewListPostings(refs), nil
			}
		}
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
	if c != nil {
		if encoded := encodePostings(key, refs); encoded != nil {
			_ = c.Store(ctx, []string{cache.HashKey(key)}, [][]byte{encoded})
		}
	}
	return index.NewListPostings(refs), nil
}
