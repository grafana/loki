package dataobj_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metadatacache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/test"
)

var (
	logsSectionType     = dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: "logs"}
	streamsSectionType  = dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: "streams"}
	pointersSectionType = dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: "pointers"}
)

func TestFromBucket_MetadataCache(t *testing.T) {
	t.Run("a cold open populates the cache and a warm open serves entirely from it", func(t *testing.T) {
		ctx := context.Background()
		raw := buildObject(t, sectionSpec{typ: streamsSectionType, meta: []byte("streams-meta"), data: []byte("streams-data")})

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		metadataCache := metadatacache.New(cache.NewMockCache(), 0, nil, nil)
		t.Cleanup(metadataCache.Stop)

		_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Positive(t, cb.getRanges.Load(), "the cold open reads from object storage")

		cb.reset()
		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Zero(t, cb.getRanges.Load(), "the warm open is served entirely from the cache")

		sec := obj.Sections()[0]
		gotMeta := readAll(t, func() (io.ReadCloser, error) { return sec.Reader.MetadataRange(ctx, 0, sec.Reader.MetadataSize()) })
		require.Equal(t, []byte("streams-meta"), gotMeta)
	})

	t.Run("a cached open decodes the same sections tenants and data as an uncached open", func(t *testing.T) {
		ctx := context.Background()
		raw := buildObject(t,
			sectionSpec{typ: logsSectionType, meta: []byte("meta-0"), data: []byte("data-0")},
			sectionSpec{typ: streamsSectionType, meta: []byte("meta-1"), data: []byte("data-1")},
		)

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))

		direct, err := dataobj.FromBucket(ctx, inmem, "obj", 0)
		require.NoError(t, err)

		metadataCache := metadatacache.New(cache.NewMockCache(), 0, nil, nil)
		t.Cleanup(metadataCache.Stop)

		_, err = dataobj.FromBucket(ctx, inmem, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		cached, err := dataobj.FromBucket(ctx, inmem, "obj", 0, dataobj.WithMetadataCache(metadataCache))
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
	})

	t.Run("an object with a logs section appended after the streams section caches the streams section metadata but not the logs section metadata", func(t *testing.T) {
		ctx := context.Background()
		bigLogsMeta := bytes.Repeat([]byte("m"), 20*1024) // exceeds the 16 KiB minimum prefetch
		raw := buildObject(t,
			sectionSpec{typ: streamsSectionType, meta: []byte("streams-meta"), data: []byte("streams-data")},
			sectionSpec{typ: logsSectionType, meta: bigLogsMeta, data: []byte("logs-data")},
		)

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		memoryCache := cache.NewMockCache()
		metadataCache := metadatacache.New(memoryCache, 0, nil, nil)
		t.Cleanup(metadataCache.Stop)

		_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Less(t, len(onlyCachedBlob(t, memoryCache)), 20*1024, "the cached region excludes the logs section's metadata")
		require.Less(t, len(onlyCachedBlob(t, memoryCache)), len(raw))

		cb.reset()
		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Zero(t, cb.getRanges.Load(), "opening from the metadata cache reads nothing from object storage")

		streamsSec := sectionByKind(t, obj.Sections(), "streams")
		logsSec := sectionByKind(t, obj.Sections(), "logs")

		streamsMeta := readAll(t, func() (io.ReadCloser, error) {
			return streamsSec.Reader.MetadataRange(ctx, 0, streamsSec.Reader.MetadataSize())
		})
		require.Zero(t, cb.getRanges.Load(), "streams metadata is served from the cached region")
		require.Equal(t, []byte("streams-meta"), streamsMeta)

		logsMeta := readAll(t, func() (io.ReadCloser, error) {
			return logsSec.Reader.MetadataRange(ctx, 0, logsSec.Reader.MetadataSize())
		})
		require.Positive(t, cb.getRanges.Load(), "logs metadata is outside the cached region and falls through to storage")
		require.Equal(t, bigLogsMeta, logsMeta)
	})

	t.Run("an object with a logs section appended before the streams section caches both sections' metadata", func(t *testing.T) {
		ctx := context.Background()
		logsMeta := bytes.Repeat([]byte("m"), 20*1024)
		streamsMeta := []byte("streams-meta")
		raw := buildObject(t,
			sectionSpec{typ: logsSectionType, meta: logsMeta, data: []byte("logs-data")},
			sectionSpec{typ: streamsSectionType, meta: streamsMeta, data: []byte("streams-data")},
		)

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		memoryCache := cache.NewMockCache()
		metadataCache := metadatacache.New(memoryCache, 0, nil, nil)
		t.Cleanup(metadataCache.Stop)

		_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Greater(t, len(onlyCachedBlob(t, memoryCache)), len(logsMeta), "the cutoff must span past the logs section to reach the streams section's metadata")

		cb.reset()
		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Zero(t, cb.getRanges.Load())

		logsSec := sectionByKind(t, obj.Sections(), "logs")
		gotLogsMeta := readAll(t, func() (io.ReadCloser, error) {
			return logsSec.Reader.MetadataRange(ctx, 0, logsSec.Reader.MetadataSize())
		})
		require.Zero(t, cb.getRanges.Load(), "the logs metadata is served from the cached region too, since the region has to span past it anyway")
		require.Equal(t, logsMeta, gotLogsMeta)

		streamsSec := sectionByKind(t, obj.Sections(), "streams")
		gotStreamsMeta := readAll(t, func() (io.ReadCloser, error) {
			return streamsSec.Reader.MetadataRange(ctx, 0, streamsSec.Reader.MetadataSize())
		})
		require.Zero(t, cb.getRanges.Load(), "the streams metadata is served from the cached region even though it is appended after the logs section")
		require.Equal(t, streamsMeta, gotStreamsMeta)
	})

	t.Run("an object with only a logs section caches just the file metadata", func(t *testing.T) {
		ctx := context.Background()
		logsMeta := []byte("logs-meta")
		raw := buildObject(t, sectionSpec{typ: logsSectionType, meta: logsMeta, data: []byte("logs-data")})

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		memoryCache := cache.NewMockCache()
		metadataCache := metadatacache.New(memoryCache, 0, nil, nil)
		t.Cleanup(metadataCache.Stop)

		_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Less(t, len(onlyCachedBlob(t, memoryCache)), len(raw), "the cached region excludes the only section's metadata")

		cb.reset()
		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Zero(t, cb.getRanges.Load())

		sec := obj.Sections()[0]
		gotMeta := readAll(t, func() (io.ReadCloser, error) {
			return sec.Reader.MetadataRange(ctx, 0, sec.Reader.MetadataSize())
		})
		require.Positive(t, cb.getRanges.Load(), "the only section is a logs section, so its metadata always falls through")
		require.Equal(t, logsMeta, gotMeta)
	})

	t.Run("an object with logs sections appended before and after the streams section caches up to the streams section metadata but not the last logs section's metadata", func(t *testing.T) {
		ctx := context.Background()
		streamsMeta := []byte("streams-meta")
		lastLogsMeta := bytes.Repeat([]byte("b"), 8*1024)
		raw := buildObject(t,
			sectionSpec{typ: logsSectionType, meta: bytes.Repeat([]byte("a"), 8*1024), data: []byte("logs-data-1")},
			sectionSpec{typ: streamsSectionType, meta: streamsMeta, data: []byte("streams-data")},
			sectionSpec{typ: logsSectionType, meta: lastLogsMeta, data: []byte("logs-data-2")},
		)

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		memoryCache := cache.NewMockCache()
		metadataCache := metadatacache.New(memoryCache, 0, nil, nil)
		t.Cleanup(metadataCache.Stop)

		_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Greater(t, len(onlyCachedBlob(t, memoryCache)), 8*1024, "the cutoff must span past the first logs section to reach the streams section's metadata")
		require.Less(t, len(onlyCachedBlob(t, memoryCache)), len(raw), "the cutoff must not include the last logs section's metadata")

		cb.reset()
		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Zero(t, cb.getRanges.Load())

		streamsSec := sectionByKind(t, obj.Sections(), "streams")
		gotStreamsMeta := readAll(t, func() (io.ReadCloser, error) {
			return streamsSec.Reader.MetadataRange(ctx, 0, streamsSec.Reader.MetadataSize())
		})
		require.Zero(t, cb.getRanges.Load(), "the streams metadata is served from the cached region despite sitting between two logs sections")
		require.Equal(t, streamsMeta, gotStreamsMeta)

		// obj.Sections()[2], not sectionByKind, since there are two logs sections and this must be the
		// last one specifically: a bug that special-cases only the first logs section it sees would
		// wrongly include this one's metadata in the cached region too.
		lastLogsSec := obj.Sections()[2]
		gotLastLogsMeta := readAll(t, func() (io.ReadCloser, error) {
			return lastLogsSec.Reader.MetadataRange(ctx, 0, lastLogsSec.Reader.MetadataSize())
		})
		require.Positive(t, cb.getRanges.Load(), "the last logs section's metadata is outside the cached region and falls through to storage")
		require.Equal(t, lastLogsMeta, gotLastLogsMeta)
	})

	t.Run("a cached region larger than the prefetch window is completed with an exact read of the missing tail", func(t *testing.T) {
		ctx := context.Background()
		bigMeta := bytes.Repeat([]byte("m"), 20*1024) // exceeds the 16 KiB minimum prefetch
		raw := buildObject(t,
			sectionSpec{typ: streamsSectionType, meta: []byte("streams-meta"), data: []byte("streams-data")},
			sectionSpec{typ: pointersSectionType, meta: bigMeta, data: []byte("pointers-data")},
		)

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		memoryCache := cache.NewMockCache()
		metadataCache := metadatacache.New(memoryCache, 0, nil, nil)
		t.Cleanup(metadataCache.Stop)

		_, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		cachedBlob := onlyCachedBlob(t, memoryCache)
		require.Greater(t, len(cachedBlob), 16*1024, "the cached region exceeds the optimistic prefetch window")
		require.GreaterOrEqual(t, cb.getRanges.Load(), int64(2), "an oversized region needs the optimistic prefetch plus an exact read of the missing tail")
		// Reading only the missing tail (not re-reading the prefetched bytes) means the total bytes
		// requested equals the cached region exactly, not the region plus a second full-length read.
		require.Equal(t, int64(len(cachedBlob)), cb.rangeBytes.Load(), "the miss must not re-read bytes the prefetch already holds")

		cb.reset()
		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Zero(t, cb.getRanges.Load(), "a cache hit still reads nothing from object storage")

		pointersSec := sectionByKind(t, obj.Sections(), "pointers")
		gotMeta := readAll(t, func() (io.ReadCloser, error) {
			return pointersSec.Reader.MetadataRange(ctx, 0, pointersSec.Reader.MetadataSize())
		})
		require.Zero(t, cb.getRanges.Load())
		require.Equal(t, bigMeta, gotMeta)
	})

	// maxItemBytes is a custom limit, not metadatacache.Cache's default, so this also proves MaxItemBytes
	// is genuinely plumbed through fetchMetadataRegion, not just the built-in 64 MiB default.
	t.Run("a metadata region above the cache size cap still caches the file metadata but not the oversized section's metadata", func(t *testing.T) {
		ctx := context.Background()
		const maxItemBytes = 4096
		oversizedMeta := bytes.Repeat([]byte("m"), maxItemBytes+1) // just over maxItemBytes
		raw := buildObject(t, sectionSpec{typ: pointersSectionType, meta: oversizedMeta, data: []byte("data")})

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		memoryCache := cache.NewMockCache()
		metadataCache := metadatacache.New(memoryCache, maxItemBytes, nil, nil)
		t.Cleanup(metadataCache.Stop)
		logger := &test.CapturingLogger{}

		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache), dataobj.WithLogger(logger))
		require.NoError(t, err)
		require.Empty(t, logger.Entries(), "caching only the file metadata is not a degradation, so nothing is logged")
		require.Len(t, obj.Sections(), 1)
		require.Less(t, len(onlyCachedBlob(t, memoryCache)), len(oversizedMeta), "the section's oversized metadata must never be part of the cached blob")
		require.Equal(t, int64(1), cb.getRanges.Load(), "caching the file metadata costs a single optimistic prefetch")

		cb.reset()
		reopened, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.NoError(t, err)
		require.Zero(t, cb.getRanges.Load(), "the second open is served entirely from the cached file metadata")

		sec := reopened.Sections()[0]
		gotMeta := readAll(t, func() (io.ReadCloser, error) { return sec.Reader.MetadataRange(ctx, 0, sec.Reader.MetadataSize()) })
		require.Positive(t, cb.getRanges.Load(), "the section's own metadata is outside the cached region and falls through to storage")
		require.Equal(t, oversizedMeta, gotMeta, "section metadata round-trips through the direct read")
	})

	// extendedMetadataRegionEnd's cap bounds the absolute region end, not a size relative to startOff.
	// startOff is itself derived from the object's header, which decodeFileMetadata's self-delimiting
	// protobuf parse never validates against the header's claimed size — so a corrupted header inflates
	// startOff without affecting decoding. A cap checked only against regionEnd-startOff would miss
	// this entirely and let fetchMetadataRegion attempt an allocation sized by the corrupted value.
	//
	// The object must exceed the prefetch window: a smaller one's tail read fails on EOF for an
	// unrelated reason (the object is simply too short), which would let this test pass even under the
	// exact bug it targets. The two file-metadata reads below (1 optimistic prefetch, 1 full read of
	// the corruptly large declared size) happen twice — once for the failed cache attempt, once for the
	// fallback's direct read — and the getRanges assertion pins that neither ever attempts a tail read
	// or allocation sized by the corrupted value.
	t.Run("a corrupted header that inflates the region end still falls back to a direct read instead of failing", func(t *testing.T) {
		ctx := context.Background()
		bigMeta := bytes.Repeat([]byte("m"), 20*1024) // exceeds the 16 KiB minimum prefetch
		raw := buildObject(t,
			sectionSpec{typ: streamsSectionType, meta: []byte("streams-meta"), data: []byte("streams-data")},
			sectionSpec{typ: pointersSectionType, meta: bigMeta, data: []byte("pointers-data")},
		)

		corrupted := append([]byte{}, raw...)
		binary.LittleEndian.PutUint32(corrupted[4:8], 128<<20) // claims 128 MiB of file metadata

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(corrupted)))
		cb := &countingBucket{Bucket: inmem}
		memoryCache := cache.NewMockCache()
		metadataCache := metadatacache.New(memoryCache, 0, nil, nil)
		t.Cleanup(metadataCache.Stop)
		logger := &test.CapturingLogger{}

		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache), dataobj.WithLogger(logger))
		require.NoError(t, err, "a corrupted header must degrade to a direct read, not fail the open")
		require.Empty(t, memoryCache.GetInternal(), "a region derived from a corrupted header must never be cached")
		require.Len(t, logger.Entries(), 1, "the fallback logs exactly once")
		require.Equal(t, int64(4), cb.getRanges.Load(),
			"1 optimistic prefetch + 1 full read of the corruptly large declared size, for both the failed cache attempt and the fallback's direct read")
		require.Len(t, obj.Sections(), 2)
	})

	t.Run("an object truncated shorter than its section layout claims falls back to a direct read", func(t *testing.T) {
		ctx := context.Background()
		bigMeta := bytes.Repeat([]byte("m"), 20*1024) // forces the exact-range tail read in fetchMetadataRegion
		raw := buildObject(t,
			sectionSpec{typ: streamsSectionType, meta: []byte("streams-meta"), data: []byte("streams-data")},
			sectionSpec{typ: pointersSectionType, meta: bigMeta, data: []byte("pointers-data")},
		)
		truncated := raw[:len(raw)/2]

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(truncated)))
		memoryCache := cache.NewMockCache()
		metadataCache := metadatacache.New(memoryCache, 0, nil, nil)
		t.Cleanup(metadataCache.Stop)
		logger := &test.CapturingLogger{}

		direct, err := dataobj.FromBucket(ctx, inmem, "obj", 0)
		require.NoError(t, err, "sanity: the uncached path must open the truncated object")

		obj, err := dataobj.FromBucket(ctx, inmem, "obj", 0, dataobj.WithMetadataCache(metadataCache), dataobj.WithLogger(logger))
		require.NoError(t, err, "the cached path must open it too, not regress relative to the uncached path")
		require.Empty(t, memoryCache.GetInternal(), "a region that failed to read must never be cached")
		require.Len(t, logger.Entries(), 1, "the fallback logs exactly once")
		require.Equal(t, len(direct.Sections()), len(obj.Sections()))
	})

	t.Run("a load error from object storage fails the open and leaves nothing cached", func(t *testing.T) {
		ctx := context.Background()
		wantErr := errors.New("object storage unavailable")
		bucket := &failingBucket{err: wantErr}
		memoryCache := cache.NewMockCache()
		metadataCache := metadatacache.New(memoryCache, 0, nil, nil)
		t.Cleanup(metadataCache.Stop)

		_, err := dataobj.FromBucket(ctx, bucket, "obj", 0, dataobj.WithMetadataCache(metadataCache))
		require.Error(t, err)
		require.ErrorIs(t, err, wantErr)
		require.Empty(t, memoryCache.GetInternal(), "a failed load must not poison the cache")
	})

	t.Run("a corrupt cached entry falls back to a direct read and is logged", func(t *testing.T) {
		ctx := context.Background()
		raw := buildObject(t,
			sectionSpec{typ: logsSectionType, meta: []byte("meta-0"), data: []byte("data-0")},
			sectionSpec{typ: streamsSectionType, meta: []byte("meta-1"), data: []byte("data-1")},
		)

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		metadataCache := metadatacache.New(cache.NewMockCache(), 0, nil, nil)
		t.Cleanup(metadataCache.Stop)
		// A corrupt cached entry for the object (not a valid data-object header).
		seedCache(t, ctx, metadataCache, "obj", []byte("garbage-not-a-dataobj-header"))
		logger := &test.CapturingLogger{}

		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache), dataobj.WithLogger(logger))
		require.NoError(t, err, "a corrupt cached entry falls back to a direct read instead of failing")
		require.Len(t, obj.Sections(), 2)
		require.Positive(t, cb.getRanges.Load(), "the fallback reads from object storage")
		require.Len(t, logger.Entries(), 1, "the fallback logs exactly once")
		require.Contains(t, logger.Entries()[0], "obj", "the log identifies which key was affected")

		sec := obj.Sections()[0]
		got := readAll(t, func() (io.ReadCloser, error) { return sec.Reader.DataRange(ctx, 0, sec.Reader.DataSize()) })
		require.Equal(t, []byte("data-0"), got)
	})

	t.Run("a cached entry truncated before its declared metadata length falls back to a direct read", func(t *testing.T) {
		ctx := context.Background()
		raw := buildObject(t, sectionSpec{typ: logsSectionType, meta: []byte("meta-0"), data: []byte("data-0")})

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		metadataCache := metadatacache.New(cache.NewMockCache(), 0, nil, nil)
		t.Cleanup(metadataCache.Stop)
		// Real magic and a real, correctly encoded metadataSize field, but the blob stops well short of
		// that declared length.
		truncatedBlob := append([]byte{}, raw[:12]...)
		seedCache(t, ctx, metadataCache, "obj", truncatedBlob)
		logger := &test.CapturingLogger{}

		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache), dataobj.WithLogger(logger))
		require.NoError(t, err, "a truncated cached entry falls back to a direct read instead of failing")
		require.Positive(t, cb.getRanges.Load(), "the fallback reads from object storage")
		require.Len(t, logger.Entries(), 1)
		require.Len(t, obj.Sections(), 1)
	})

	t.Run("a cached entry whose payload is not valid protobuf falls back to a direct read", func(t *testing.T) {
		ctx := context.Background()
		raw := buildObject(t, sectionSpec{typ: logsSectionType, meta: []byte("meta-0"), data: []byte("data-0")})

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))
		cb := &countingBucket{Bucket: inmem}
		metadataCache := metadatacache.New(cache.NewMockCache(), 0, nil, nil)
		t.Cleanup(metadataCache.Stop)
		// Real magic, a header claiming a 4-byte file metadata that the blob's length does satisfy, but
		// those 4 bytes (all varint continuation bits set, no terminator) are not a valid encoded message.
		badBlob := append([]byte{}, raw[:8]...)
		binary.LittleEndian.PutUint32(badBlob[4:8], 4)
		badBlob = append(badBlob, 0xFF, 0xFF, 0xFF, 0xFF)
		seedCache(t, ctx, metadataCache, "obj", badBlob)
		logger := &test.CapturingLogger{}

		obj, err := dataobj.FromBucket(ctx, cb, "obj", 0, dataobj.WithMetadataCache(metadataCache), dataobj.WithLogger(logger))
		require.NoError(t, err, "a cached entry with an invalid payload falls back to a direct read instead of failing")
		require.Positive(t, cb.getRanges.Load(), "the fallback reads from object storage")
		require.Len(t, logger.Entries(), 1)
		require.Len(t, obj.Sections(), 1)
	})

	t.Run("passing a nil MetadataCache interface behaves the same as omitting the option", func(t *testing.T) {
		ctx := context.Background()
		raw := buildObject(t, sectionSpec{typ: logsSectionType, meta: []byte("meta-0"), data: []byte("data-0")})

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))

		noOption, err := dataobj.FromBucket(ctx, inmem, "obj", 0)
		require.NoError(t, err)
		nilCache, err := dataobj.FromBucket(ctx, inmem, "obj", 0, dataobj.WithMetadataCache(nil))
		require.NoError(t, err)

		require.Equal(t, len(noOption.Sections()), len(nilCache.Sections()))
		sec := noOption.Sections()[0]
		nilSec := nilCache.Sections()[0]
		require.Equal(t, readAll(t, func() (io.ReadCloser, error) { return sec.Reader.DataRange(ctx, 0, sec.Reader.DataSize()) }),
			readAll(t, func() (io.ReadCloser, error) { return nilSec.Reader.DataRange(ctx, 0, nilSec.Reader.DataSize()) }))
	})

	t.Run("passing a typed nil concrete cache behaves the same as omitting the option", func(t *testing.T) {
		ctx := context.Background()
		raw := buildObject(t, sectionSpec{typ: logsSectionType, meta: []byte("meta-0"), data: []byte("data-0")})

		inmem := objstore.NewInMemBucket()
		require.NoError(t, inmem.Upload(ctx, "obj", bytes.NewReader(raw)))

		noOption, err := dataobj.FromBucket(ctx, inmem, "obj", 0)
		require.NoError(t, err)

		// typedNil is *metadatacache.Cache, the real concrete type callers pass, not a test-only
		// stand-in: this proves WithMetadataCache's typed-nil normalization covers it specifically,
		// rather than panicking on first use the way a direct method call on it would.
		var typedNil *metadatacache.Cache
		typedNilCache, err := dataobj.FromBucket(ctx, inmem, "obj", 0, dataobj.WithMetadataCache(typedNil))
		require.NoError(t, err)

		require.Equal(t, len(noOption.Sections()), len(typedNilCache.Sections()))
		sec := noOption.Sections()[0]
		typedNilSec := typedNilCache.Sections()[0]
		require.Equal(t, readAll(t, func() (io.ReadCloser, error) { return sec.Reader.DataRange(ctx, 0, sec.Reader.DataSize()) }),
			readAll(t, func() (io.ReadCloser, error) {
				return typedNilSec.Reader.DataRange(ctx, 0, typedNilSec.Reader.DataSize())
			}))
	})
}

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

// onlyCachedBlob returns the single blob stored in memoryCache, failing the test if there isn't exactly one.
// metadatacache.Cache prefixes and versions its keys internally, so tests read the cache back this
// way instead of assuming a literal key.
func onlyCachedBlob(t *testing.T, memoryCache cache.MockCache) []byte {
	t.Helper()
	entries := memoryCache.GetInternal()
	require.Len(t, entries, 1)
	for _, blob := range entries {
		return blob
	}
	return nil
}

// seedCache stores blob in c under key, as if an earlier load had already produced it. blob need not
// be valid dataobj content, so this also seeds a deliberately corrupt entry.
func seedCache(t *testing.T, ctx context.Context, c dataobj.MetadataCache, key string, blob []byte) {
	t.Helper()
	_, err := c.GetOrLoadMetadataRegion(ctx, key, func(context.Context) ([]byte, error) { return blob, nil })
	require.NoError(t, err)
}

// failingBucket fails every GetRange with a fixed error, to exercise a load failure through the cache.
type failingBucket struct {
	objstore.Bucket
	err error
}

func (b *failingBucket) GetRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, b.err
}

type sectionSpec struct {
	typ  dataobj.SectionType
	meta []byte
	data []byte
}

// buildObject returns the raw bytes of an object with one section per spec, appended in order.
func buildObject(t *testing.T, specs ...sectionSpec) []byte {
	t.Helper()
	b := dataobj.NewBuilder(nil)
	for _, spec := range specs {
		require.NoError(t, b.Append(fakeSectionBuilder{
			SectionType: spec.typ,
			FlushFunc: func(w dataobj.SectionWriter) (int64, error) {
				return w.WriteSection(&dataobj.WriteSectionOptions{Tenant: "t1"}, spec.data, spec.meta)
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

func sectionByKind(t *testing.T, sections dataobj.Sections, kind string) *dataobj.Section {
	t.Helper()
	for _, sec := range sections {
		if sec.Type.Kind == kind {
			return sec
		}
	}
	t.Fatalf("no section with kind %q", kind)
	return nil
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
