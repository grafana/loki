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

// sectionSpec describes one section to write into a test object, in append order.
type sectionSpec struct {
	kind string
	meta []byte
	data []byte
}

// buildObject returns the raw bytes of an object with the given sections, in order. Section metadata is
// written before any section data, so a spec with large metadata pushes the metadata region end out.
func buildObject(t *testing.T, secs ...sectionSpec) []byte {
	t.Helper()
	b := dataobj.NewBuilder(nil)
	for _, s := range secs {
		require.NoError(t, b.Append(fakeSectionBuilder{
			SectionType: dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: s.kind},
			FlushFunc: func(w dataobj.SectionWriter) (int64, error) {
				return w.WriteSection(&dataobj.WriteSectionOptions{Tenant: "t1"}, s.data, s.meta)
			},
		}))
	}

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

// TestFromBucket_MetadataCache_LogObject: an object with a logs section is detected as a log object, so the
// cache stores only up to the streams metadata (written first) and excludes the large logs-metadata tail.
func TestFromBucket_MetadataCache_LogObject(t *testing.T) {
	ctx := context.Background()
	streamsMeta := []byte("streams-meta")
	logsMeta := bytes.Repeat([]byte("L"), 40*1024) // large tail that must NOT be cached
	raw := buildObject(t,
		sectionSpec{kind: "streams", meta: streamsMeta, data: []byte("streams-data")},
		sectionSpec{kind: "logs", meta: logsMeta, data: []byte("logs-data")},
	)

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
	cb := &countingBucket{Bucket: inmem}
	cache := &fakeMetadataCache{m: map[string][]byte{}}

	// Miss: caches file+streams only, so the blob excludes the 40 KiB logs metadata.
	_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Equal(t, 1, cache.loads)
	// The region is file + a tiny streams section, well inside the 16 KiB head prefetch — so it excludes the
	// 40 KiB logs metadata and needs just the one head read.
	require.Less(t, len(cache.m["obj"]), 16*1024, "file+streams region fits the head prefetch")
	require.Equal(t, int64(1), cb.getRanges.Load(), "file+streams fits the head prefetch: one read")

	// Hit: opening reads nothing; the streams metadata is served from the cached region.
	cb.reset()
	obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Equal(t, 1, cache.loads, "second open is a hit")
	require.Zero(t, cb.getRanges.Load(), "opening from the cache reads nothing")

	streamsSec, logsSec := obj.Sections()[0], obj.Sections()[1]
	require.Equal(t, "streams", streamsSec.Type.Kind)
	require.Equal(t, "logs", logsSec.Type.Kind)

	gotStreams := readAll(t, func() (io.ReadCloser, error) {
		return streamsSec.Reader.MetadataRange(ctx, 0, streamsSec.Reader.MetadataSize())
	})
	require.Zero(t, cb.getRanges.Load(), "streams metadata comes from the cached region")
	require.Equal(t, streamsMeta, gotStreams)

	// The logs metadata is beyond the cached region, so it is read from object storage.
	gotLogs := readAll(t, func() (io.ReadCloser, error) {
		return logsSec.Reader.MetadataRange(ctx, 0, logsSec.Reader.MetadataSize())
	})
	require.Positive(t, cb.getRanges.Load(), "logs metadata is read from object storage")
	require.Equal(t, logsMeta, gotLogs)
}

// TestFromBucket_MetadataCache_IndexObject: an object with no logs section (streams + pointers) is detected
// as an index object, so the cache stores all metadata, including the large pointers metadata.
func TestFromBucket_MetadataCache_IndexObject(t *testing.T) {
	ctx := context.Background()
	pointersMeta := bytes.Repeat([]byte("P"), 40*1024) // large, but still cached in full for an index object
	raw := buildObject(t,
		sectionSpec{kind: "streams", meta: []byte("streams-meta"), data: []byte("streams-data")},
		sectionSpec{kind: "pointers", meta: pointersMeta, data: []byte("pointers-data")},
	)

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
	cb := &countingBucket{Bucket: inmem}
	cache := &fakeMetadataCache{m: map[string][]byte{}}

	// Miss: caches all metadata, so the blob includes the 40 KiB pointers metadata but not the data.
	_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Equal(t, 1, cache.loads)
	require.Greater(t, len(cache.m["obj"]), len(pointersMeta), "cached region includes all section metadata")
	require.Less(t, len(cache.m["obj"]), len(raw), "cached region excludes the section data")
	// The region exceeds the 16 KiB head prefetch, so it is read exactly after the head read.
	require.Equal(t, int64(2), cb.getRanges.Load())

	// Hit: opening reads nothing; even the pointers metadata is served from the cached region.
	cb.reset()
	obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Equal(t, 1, cache.loads, "second open is a hit")
	require.Zero(t, cb.getRanges.Load(), "opening from the cache reads nothing")

	pointersSec := obj.Sections()[1]
	require.Equal(t, "pointers", pointersSec.Type.Kind)
	gotPointers := readAll(t, func() (io.ReadCloser, error) {
		return pointersSec.Reader.MetadataRange(ctx, 0, pointersSec.Reader.MetadataSize())
	})
	require.Zero(t, cb.getRanges.Load(), "all metadata, including pointers, comes from the cached region")
	require.Equal(t, pointersMeta, gotPointers)
}

func TestFromBucket_MetadataCache_MatchesUncachedOpen(t *testing.T) {
	ctx := context.Background()
	raw := buildObject(t,
		sectionSpec{kind: "streams", meta: []byte("meta-0"), data: []byte("data-0")},
		sectionSpec{kind: "logs", meta: []byte("meta-1"), data: []byte("data-1")},
	)

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))

	direct, err := dataobj.FromBucket(ctx, inmem, "obj", 0) // no cache -> metadataDirect
	require.NoError(t, err)

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

		// Metadata: the region sizing is exactly what this feature changes, so compare it directly.
		wantMeta := readAll(t, func() (io.ReadCloser, error) {
			return direct.Sections()[i].Reader.MetadataRange(ctx, 0, direct.Sections()[i].Reader.MetadataSize())
		})
		gotMeta := readAll(t, func() (io.ReadCloser, error) {
			return cached.Sections()[i].Reader.MetadataRange(ctx, 0, cached.Sections()[i].Reader.MetadataSize())
		})
		require.Equal(t, wantMeta, gotMeta)

		wantData := readAll(t, func() (io.ReadCloser, error) {
			return direct.Sections()[i].Reader.DataRange(ctx, 0, direct.Sections()[i].Reader.DataSize())
		})
		gotData := readAll(t, func() (io.ReadCloser, error) {
			return cached.Sections()[i].Reader.DataRange(ctx, 0, cached.Sections()[i].Reader.DataSize())
		})
		require.Equal(t, wantData, gotData)
	}
}

// TestFromBucket_MetadataCache_LogObjectNoStreams: a log object with no streams section caches only the file
// metadata (streamsEnd stays at startOff), so no section metadata is cached at all.
func TestFromBucket_MetadataCache_LogObjectNoStreams(t *testing.T) {
	ctx := context.Background()
	logsMeta := bytes.Repeat([]byte("L"), 40*1024)
	raw := buildObject(t, sectionSpec{kind: "logs", meta: logsMeta, data: []byte("logs-data")})

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
	cb := &countingBucket{Bucket: inmem}
	cache := &fakeMetadataCache{m: map[string][]byte{}}

	// Miss: caches file metadata only, so the blob excludes the logs metadata; one head read covers it.
	_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Less(t, len(cache.m["obj"]), len(logsMeta), "no streams section: nothing but file metadata is cached")
	require.Equal(t, int64(1), cb.getRanges.Load())

	// Hit: opening reads nothing, but the logs metadata is beyond the cached region, so reading it hits storage.
	cb.reset()
	obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Equal(t, 1, cache.loads, "second open is a hit")
	require.Zero(t, cb.getRanges.Load(), "opening from the cache reads nothing")

	got := readAll(t, func() (io.ReadCloser, error) {
		return obj.Sections()[0].Reader.MetadataRange(ctx, 0, obj.Sections()[0].Reader.MetadataSize())
	})
	require.Positive(t, cb.getRanges.Load(), "logs metadata is beyond the cached region")
	require.Equal(t, logsMeta, got)
}

// TestFromBucket_MetadataCache_StreamsStraddlingLogs pins the offset-based, order-independent boundary: a
// streams section placed AFTER a large logs section must still be covered by the cached region. A boundary
// that summed streams-metadata lengths (rather than reading layout offsets), or kept only the first streams
// section, would stop short and read the trailing streams metadata from storage on every hit.
func TestFromBucket_MetadataCache_StreamsStraddlingLogs(t *testing.T) {
	ctx := context.Background()
	logsMeta := bytes.Repeat([]byte("L"), 40*1024) // pushes the second streams section well past the head
	raw := buildObject(t,
		sectionSpec{kind: "streams", meta: []byte("s0-meta"), data: []byte("s0-data")},
		sectionSpec{kind: "logs", meta: logsMeta, data: []byte("logs-data")},
		sectionSpec{kind: "streams", meta: []byte("s1-meta"), data: []byte("s1-data")},
	)

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
	cb := &countingBucket{Bucket: inmem}
	cache := &fakeMetadataCache{m: map[string][]byte{}}

	// The region reaches the last streams section (past the 40 KiB logs metadata), so the miss reads the head
	// and then the exact region.
	_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Greater(t, len(cache.m["obj"]), len(logsMeta), "region reaches the streams section after the logs tail")
	require.Equal(t, int64(2), cb.getRanges.Load(), "region exceeds the head prefetch: head read + exact read")

	// Hit: every streams section's metadata — including the one after the logs tail — is served from cache.
	cb.reset()
	obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err)
	require.Equal(t, 1, cache.loads, "second open is a hit")

	s0, s1 := obj.Sections()[0], obj.Sections()[2]
	require.Equal(t, "streams", s0.Type.Kind)
	require.Equal(t, "streams", s1.Type.Kind)
	require.Equal(t, []byte("s0-meta"), readAll(t, func() (io.ReadCloser, error) {
		return s0.Reader.MetadataRange(ctx, 0, s0.Reader.MetadataSize())
	}))
	require.Equal(t, []byte("s1-meta"), readAll(t, func() (io.ReadCloser, error) {
		return s1.Reader.MetadataRange(ctx, 0, s1.Reader.MetadataSize())
	}))
	require.Zero(t, cb.getRanges.Load(), "both streams sections' metadata come from the cached region")
}

func TestFromBucket_MetadataCache_CorruptEntryFallsBack(t *testing.T) {
	ctx := context.Background()
	raw := buildObject(t,
		sectionSpec{kind: "streams", meta: []byte("meta-0"), data: []byte("data-0")},
		sectionSpec{kind: "logs", meta: []byte("meta-1"), data: []byte("data-1")},
	)

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
	cb := &countingBucket{Bucket: inmem}
	// A corrupt cached entry for the object (not a valid data-object header).
	cache := &fakeMetadataCache{m: map[string][]byte{"obj": []byte("garbage-not-a-dataobj-header")}}

	obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(cache))
	require.NoError(t, err, "a corrupt cached entry falls back to a direct read instead of failing")
	require.Len(t, obj.Sections(), 2)
	require.Positive(t, cb.getRanges.Load(), "the fallback reads from object storage")

	sec := obj.Sections()[0]
	got := readAll(t, func() (io.ReadCloser, error) { return sec.Reader.DataRange(ctx, 0, sec.Reader.DataSize()) })
	require.Equal(t, []byte("data-0"), got)
}

// TestFromBucket_NoCache_ReadsMetadataAndData pins the default (no-cache) metadataDirect path to literal
// bytes, so a regression in the shared read/decode helper is caught even if it breaks both paths alike.
func TestFromBucket_NoCache_ReadsMetadataAndData(t *testing.T) {
	ctx := context.Background()
	raw := buildObject(t,
		sectionSpec{kind: "streams", meta: []byte("meta-0"), data: []byte("data-0")},
		sectionSpec{kind: "logs", meta: []byte("meta-1"), data: []byte("data-1")},
	)

	inmem := objstore.NewInMemBucket()
	require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))

	obj, err := dataobj.FromBucket(ctx, inmem, "obj", 0) // no cache -> metadataDirect
	require.NoError(t, err)
	require.Len(t, obj.Sections(), 2)

	sec := obj.Sections()[0]
	require.Equal(t, "streams", sec.Type.Kind)
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
