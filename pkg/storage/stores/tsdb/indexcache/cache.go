// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/thanos-io/thanos/blob/main/pkg/store/cache/cache.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Thanos Authors.

package indexcache

import (
	"encoding/base64"
	"reflect"
	"sort"
	"strings"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"golang.org/x/crypto/blake2b"
)

const (
	cacheTypePostings          = "Postings"
	cacheTypeSeriesForRef      = "SeriesForRef"
	cacheTypeExpandedPostings  = "ExpandedPostings"
	cacheTypeSeriesForPostings = "SeriesForPostings"
	cacheTypeLabelNames        = "LabelNames"
	cacheTypeLabelValues       = "LabelValues"
)

var (
	allCacheTypes = []string{
		cacheTypePostings,
		cacheTypeSeriesForRef,
		cacheTypeExpandedPostings,
		cacheTypeSeriesForPostings,
		cacheTypeLabelNames,
		cacheTypeLabelValues,
	}
)

type BytesResult interface {
	// Next should return a byte slice if there was a cache hit for the current key; otherwise Next should return nil.
	// Next should return false when there are no more keys in the result.
	Next() ([]byte, bool)

	// Remaining should return the number of keys left in the result.
	// There may or may not be an item for each key.
	Remaining() int

	// Size should return the size in bytes of the result.
	Size() int
}

type MapIterator[T comparable] struct {
	M    map[T][]byte
	Keys []T
}

func (l *MapIterator[T]) Next() ([]byte, bool) {
	if len(l.Keys) == 0 {
		return nil, false
	}
	b := l.M[l.Keys[0]]
	l.Keys = l.Keys[1:]
	return b, true
}

func (l *MapIterator[T]) Remaining() int {
	return len(l.Keys)
}

func (l *MapIterator[T]) Size() int {
	return sumBytes[T](l.M)
}

func sumBytes[T comparable](res map[T][]byte) int {
	sum := 0
	for _, v := range res {
		sum += len(v)
	}
	return sum
}

// PostingsKey represents a canonical key for a []storage.SeriesRef slice
type PostingsKey string

// CanonicalPostingsKey creates a canonical version of PostingsKey
func CanonicalPostingsKey(postings []storage.SeriesRef) PostingsKey {
	hashable := unsafeCastPostingsToBytes(postings)
	checksum := blake2b.Sum256(hashable)
	return PostingsKey(base64.RawURLEncoding.EncodeToString(checksum[:]))
}

const bytesPerPosting = int(unsafe.Sizeof(storage.SeriesRef(0)))

// unsafeCastPostingsToBytes returns the postings as a slice of bytes with minimal allocations.
// It casts the memory region of the underlying array to a slice of bytes. The resulting byte slice is only valid as long as the postings slice exists and is unmodified.
func unsafeCastPostingsToBytes(postings []storage.SeriesRef) []byte {
	byteSlice := make([]byte, 0)
	slicePtr := (*reflect.SliceHeader)(unsafe.Pointer(&byteSlice))
	slicePtr.Data = (*reflect.SliceHeader)(unsafe.Pointer(&postings)).Data
	slicePtr.Len = len(postings) * bytesPerPosting
	slicePtr.Cap = slicePtr.Len
	return byteSlice
}

// LabelMatchersKey represents a canonical key for a []*matchers.Matchers slice
type LabelMatchersKey string

// CanonicalLabelMatchersKey creates a canonical version of LabelMatchersKey
func CanonicalLabelMatchersKey(ms []*labels.Matcher) LabelMatchersKey {
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
	return LabelMatchersKey(sb.String())
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

func initLabelValuesForAllCacheTypes(vec *prometheus.MetricVec) {
	for _, typ := range allCacheTypes {
		_, err := vec.GetMetricWithLabelValues(typ)
		if err != nil {
			panic(err)
		}
	}
}
