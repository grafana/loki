package dataobj_test

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

// countingBucket counts GetRange calls and the bytes they request, to prove which reads hit storage.
type countingBucket struct {
	objstore.Bucket
	getRanges  atomic.Int64
	rangeBytes atomic.Int64
}

func (b *countingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	b.getRanges.Add(1)
	b.rangeBytes.Add(length)
	return b.Bucket.GetRange(ctx, name, off, length)
}

func (b *countingBucket) reset() { b.getRanges.Store(0); b.rangeBytes.Store(0) }

// fakeMetadataCache is an in-memory MetadataCache that records how often it loads on a miss.
type fakeMetadataCache struct {
	m     map[string][]byte
	loads int
}

func (c *fakeMetadataCache) GetMetadata(ctx context.Context, key string, load func(context.Context) ([]byte, error)) ([]byte, error) {
	if b, ok := c.m[key]; ok {
		return b, nil
	}
	b, err := load(ctx)
	if err != nil {
		return nil, err
	}
	c.m[key] = b
	c.loads++
	return b, nil
}

// buildTwoSectionObject returns the raw bytes of an object with two sections. The first section's
// metadata is larger than the minimum prefetch window, exercising the exact-prefix read path.
func buildTwoSectionObject(t *testing.T, meta0, data0, meta1, data1 []byte) []byte {
	t.Helper()
	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(fakeSectionBuilder{
		SectionType: dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: "logs"},
		FlushFunc: func(w dataobj.SectionWriter) (int64, error) {
			return w.WriteSection(&dataobj.WriteSectionOptions{Tenant: "t1"}, data0, meta0)
		},
	}))
	require.NoError(t, b.Append(fakeSectionBuilder{
		SectionType: dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: "streams"},
		FlushFunc: func(w dataobj.SectionWriter) (int64, error) {
			return w.WriteSection(&dataobj.WriteSectionOptions{Tenant: "t1"}, data1, meta1)
		},
	}))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	rc, err := obj.Reader(context.Background())
	require.NoError(t, err)
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	require.NoError(t, err)
	return raw
}

func TestFromBucket_MetadataCache(t *testing.T) {
	ctx := context.Background()
	// Section 0 metadata exceeds the 16 KiB minimum prefetch, so the prefix must be read exactly.
	meta0 := bytes.Repeat([]byte("m"), 20*1024)
	data0 := []byte("data-of-section-0")
	meta1 := []byte("meta-1")
	data1 := []byte("data-1")
	raw := buildTwoSectionObject(t, meta0, data0, meta1, data1)

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
	cb := &countingBucket{Bucket: inmem}
	cache := &fakeMetadataCache{m: map[string][]byte{}}

	// First open is a miss: it loads and caches the metadata prefix.
	obj1, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Equal(t, 1, cache.loads)
	require.Len(t, cache.m, 1)
	require.Len(t, obj1.Sections(), 2)
	// The cached prefix is smaller than the whole object (it holds no data).
	require.Less(t, len(cache.m["obj"]), len(raw))
	require.Greater(t, len(cache.m["obj"]), 20*1024) // covers the large section-0 metadata
	// The miss reads the 16KiB prefetch, then the exact prefix (metadata exceeds the prefetch here).
	require.Equal(t, int64(2), cb.getRanges.Load())

	// Second open is a hit: no reload, and no object-storage read at all to open.
	cb.reset()
	obj2, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Equal(t, 1, cache.loads, "second open is served from the cache")
	require.Zero(t, cb.getRanges.Load(), "opening from the metadata cache reads nothing from object storage")

	sec0 := obj2.Sections()[0]
	require.Equal(t, "logs", sec0.Type.Kind)

	// Section metadata is served from the cached prefix — still no object-storage read.
	gotMeta := readAll(t, func() (io.ReadCloser, error) { return sec0.Reader.MetadataRange(ctx, 0, sec0.Reader.MetadataSize()) })
	require.Zero(t, cb.getRanges.Load(), "section metadata comes from the cached prefix")
	require.Equal(t, meta0, gotMeta)

	// Section data is beyond the cached prefix, so it does hit object storage and returns correctly.
	gotData := readAll(t, func() (io.ReadCloser, error) { return sec0.Reader.DataRange(ctx, 0, sec0.Reader.DataSize()) })
	require.Positive(t, cb.getRanges.Load(), "section data is read from object storage")
	require.Equal(t, data0, gotData)
}

func TestFromBucket_MetadataCache_MatchesUncachedOpen(t *testing.T) {
	ctx := context.Background()
	raw := buildTwoSectionObject(t, []byte("meta-0"), []byte("data-0"), []byte("meta-1"), []byte("data-1"))

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))

	// Uncached open (metadataDirect path).
	direct, err := dataobj.FromBucket(ctx, inmem, "obj", 0)
	require.NoError(t, err)

	// Cached open, warmed by a first miss.
	cache := &fakeMetadataCache{m: map[string][]byte{}}
	_, err = dataobj.FromBucket(ctx, inmem, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	cached, err := dataobj.FromBucket(ctx, inmem, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)

	require.Equal(t, len(direct.Sections()), len(cached.Sections()))
	require.Equal(t, direct.Tenants(), cached.Tenants())
	for i := range direct.Sections() {
		require.Equal(t, direct.Sections()[i].Type, cached.Sections()[i].Type)
		require.Equal(t, direct.Sections()[i].Tenant, cached.Sections()[i].Tenant)

		wantData := readAll(t, func() (io.ReadCloser, error) {
			return direct.Sections()[i].Reader.DataRange(ctx, 0, direct.Sections()[i].Reader.DataSize())
		})
		gotData := readAll(t, func() (io.ReadCloser, error) {
			return cached.Sections()[i].Reader.DataRange(ctx, 0, cached.Sections()[i].Reader.DataSize())
		})
		require.Equal(t, wantData, gotData)
	}
}

func TestFromBucket_MetadataCache_CorruptEntryFallsBack(t *testing.T) {
	ctx := context.Background()
	raw := buildTwoSectionObject(t, []byte("meta-0"), []byte("data-0"), []byte("meta-1"), []byte("data-1"))

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
	cb := &countingBucket{Bucket: inmem}
	// A corrupt cached entry for the object (not a valid data-object header).
	cache := &fakeMetadataCache{m: map[string][]byte{"obj": []byte("garbage-not-a-dataobj-header")}}

	obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err, "a corrupt cached entry falls back to a direct read instead of failing")
	require.Len(t, obj.Sections(), 2)
	require.Positive(t, cb.getRanges.Load(), "the fallback reads from object storage")

	// Data reads correctly via the fallback.
	sec := obj.Sections()[0]
	got := readAll(t, func() (io.ReadCloser, error) { return sec.Reader.DataRange(ctx, 0, sec.Reader.DataSize()) })
	require.Equal(t, []byte("data-0"), got)
}

// TestFromBucket_NoCache_ReadsMetadataAndData pins the default (no-cache) metadataDirect path to literal
// bytes, so a regression in the shared read/decode helper is caught even if it breaks both paths alike.
func TestFromBucket_NoCache_ReadsMetadataAndData(t *testing.T) {
	ctx := context.Background()
	raw := buildTwoSectionObject(t, []byte("meta-0"), []byte("data-0"), []byte("meta-1"), []byte("data-1"))

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))

	obj, err := dataobj.FromBucket(ctx, inmem, "obj", 0) // no cache -> metadataDirect
	require.NoError(t, err)
	require.Len(t, obj.Sections(), 2)

	sec := obj.Sections()[0]
	require.Equal(t, "logs", sec.Type.Kind)
	require.Equal(t, []byte("meta-0"), readAll(t, func() (io.ReadCloser, error) {
		return sec.Reader.MetadataRange(ctx, 0, sec.Reader.MetadataSize())
	}))
	require.Equal(t, []byte("data-0"), readAll(t, func() (io.ReadCloser, error) {
		return sec.Reader.DataRange(ctx, 0, sec.Reader.DataSize())
	}))
}

func readAll(t *testing.T, open func() (io.ReadCloser, error)) []byte {
	t.Helper()
	rc, err := open()
	require.NoError(t, err)
	defer rc.Close()
	b, err := io.ReadAll(rc)
	require.NoError(t, err)
	return b
}
