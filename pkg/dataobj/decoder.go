package dataobj

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

// errCannotCacheMetadata signals that a metadata region could not be produced to cache.
// metadataViaCache treats it as non-fatal: it falls back to a direct read instead of failing the open.
var errCannotCacheMetadata = errors.New("cannot cache data-object metadata region")

// minimumPrefetchBytes is the minimum number of bytes to prefetch before
// decoding.
const minimumPrefetchBytes int64 = 16 * 1024

type decoder struct {
	rr            rangeReader
	size          int64
	startOff      int64
	prefetchBytes int64

	prefetchedRangeReader rangeReader

	// metadataCache, when set, serves the metadata region so a warm cache avoids re-reading the
	// metadata from object storage on every open. metadataKey identifies the object in the cache.
	metadataCache MetadataCache
	metadataKey   string

	// logger reports otherwise-invisible degradations, such as a cached metadata entry that does not
	// decode. FromBucket and FromReaderAt default it to a no-op logger, so it is never nil here.
	logger log.Logger
}

func (d *decoder) Metadata(ctx context.Context) (*filemd.Metadata, error) {
	// An empty key would collide every object onto one cache entry, so treat it as no cache.
	if d.metadataCache != nil && d.metadataKey != "" {
		return d.metadataViaCache(ctx)
	}
	return d.metadataDirect(ctx)
}

// metadataDirect reads and decodes the file metadata straight from the range reader. It keeps the
// whole prefetch buffer as the prefetched window, so an over-sized prefetch also serves the first data
// reads.
func (d *decoder) metadataDirect(ctx context.Context) (*filemd.Metadata, error) {
	md, buf, metadataSize, err := d.fetchAndDecodeMetadata(ctx)
	if err != nil {
		return nil, err
	}
	d.setPrefetchedBytes(0, buf)
	d.startOff = int64(8) + int64(metadataSize)
	return md, nil
}

// metadataViaCache serves the metadata region from the cache (loading it on a miss), then decodes the
// file metadata from it. It prefetches whatever region was cached, so a later section-metadata read
// falling inside that region is served from memory instead of a fresh read from object storage.
//
// Two things can go wrong without being fatal: a cached region that does not decode (truncated or
// corrupt), and a load that cannot produce a cacheable region at all (see errCannotCacheMetadata). Both
// fall back to a direct read, so enabling the cache never makes an object less openable than the
// uncached path would have.
//
// The two cases differ in how long they last. A corrupt entry clears itself once the cache's TTL or
// eviction policy expires it. An uncacheable region recurs on every open of that object, since nothing
// is ever stored for it to expire.
func (d *decoder) metadataViaCache(ctx context.Context) (*filemd.Metadata, error) {
	blob, err := d.metadataCache.GetOrLoadMetadataRegion(ctx, d.metadataKey, d.fetchMetadataRegion)
	if err != nil {
		if errors.Is(err, errCannotCacheMetadata) {
			level.Warn(d.logger).Log("msg", "data-object metadata cannot be cached; reading directly", "key", d.metadataKey, "err", err)
			return d.metadataDirect(ctx)
		}
		return nil, err
	}

	md, metadataSize, decodeErr := d.decodeMetadataRegion(blob)
	if decodeErr != nil {
		level.Warn(d.logger).Log("msg", "cached data-object metadata region did not decode; falling back to a direct read",
			"key", d.metadataKey, "blob_bytes", len(blob), "err", decodeErr)
		return d.metadataDirect(ctx)
	}

	d.setPrefetchedBytes(0, blob)
	d.startOff = int64(8) + int64(metadataSize)
	return md, nil
}

// decodeMetadataRegion decodes the file metadata from a cached metadata region and returns the
// file metadata size from the header (which the caller uses to compute startOff). A non-nil error
// means the blob is too short or does not decode, so the caller can fall back and report why.
func (d *decoder) decodeMetadataRegion(blob []byte) (md *filemd.Metadata, metadataSize uint64, err error) {
	header, err := d.header(blob)
	if err != nil {
		return nil, 0, fmt.Errorf("scanning header: %w", err)
	}
	if uint64(len(blob)) < header.MetadataSize+8 {
		return nil, 0, fmt.Errorf("blob is shorter than the file metadata: have %d bytes, need at least %d", len(blob), header.MetadataSize+8)
	}
	md, err = decodeFileMetadata(bytes.NewReader(blob[8:]))
	if err != nil {
		return nil, 0, fmt.Errorf("decoding file metadata: %w", err)
	}
	return md, header.MetadataSize, nil
}

// fetchMetadataRegion reads the metadata region its caller should cache: the file metadata plus, per
// extendedMetadataRegionEnd, every non-logs section's own metadata.
//
// A failure after the file metadata decodes (computing the region, or reading it) is wrapped in
// errCannotCacheMetadata, so metadataViaCache can fall back to a direct read. A failure decoding the
// file metadata itself is returned unwrapped: metadataDirect would hit that same failure, so there is
// no direct read left to fall back to.
func (d *decoder) fetchMetadataRegion(ctx context.Context) ([]byte, error) {
	md, buf, metadataSize, err := d.fetchAndDecodeMetadata(ctx)
	if err != nil {
		return nil, err
	}

	// MaxItemBytes is the cache backend's own size limit: asking upfront, before reading or allocating
	// anything, avoids doing that work for a region the backend would reject anyway. MetadataCache
	// guarantees a positive value, so there is nothing to default here.
	startOff := int64(8) + int64(metadataSize)
	regionEnd, err := d.extendedMetadataRegionEnd(md, startOff, d.metadataCache.MaxItemBytes())
	if err != nil {
		return nil, err
	}

	// Return a right-sized copy, not a slice of buf. The copy is cached and kept as the prefetched
	// window, so it outlives buf. A slice would pin buf's larger backing array.
	if int64(len(buf)) >= regionEnd {
		return bytes.Clone(buf[:regionEnd]), nil
	}

	// The region exceeds the prefetch window; read only the missing tail and append it, instead of
	// re-reading the bytes buf already holds. If the file metadata itself also exceeded the prefetch
	// window, fetchAndDecodeMetadata already issued its own separate read for that; this tail read
	// adds to that read rather than replacing it.
	rc, err := d.rr.ReadRange(ctx, int64(len(buf)), regionEnd-int64(len(buf)))
	if err != nil {
		return nil, fmt.Errorf("reading metadata region: %w: %w", errCannotCacheMetadata, err)
	}
	defer rc.Close()

	region := make([]byte, regionEnd)
	copy(region, buf)
	if _, err := io.ReadFull(rc, region[len(buf):]); err != nil {
		return nil, fmt.Errorf("reading metadata region: %w: %w", errCannotCacheMetadata, err)
	}
	return region, nil
}

// extendedMetadataRegionEnd returns the end offset of the region to fetch and cache for md: startOff
// extended as far as possible, without exceeding maxBytes, to also cover non-logs sections' own
// metadata. A logs section's metadata is always excluded: it can grow arbitrarily large with schema
// width, which is exactly the cost this function exists to avoid caching.
//
// The dataobj layout this relies on (see encoder.Flush):
//
//	+--------+---------------+----------------------+------------------+-------+
//	| header | file metadata |   metadata regions   |   data regions   | magic |
//	+--------+---------------+----------------------+------------------+-------+
//	                         ^ startOff             ^ data regions begin
//	                         |----------------------|  extended up to maxBytes (non-logs sections only)
//
// Every section's metadata sits in one contiguous block, followed by every section's data in a
// second block. The cached blob is always the prefix [0, regionEnd), so section data is never
// cached, however large maxBytes is. A logs section's own metadata is never what grows the region,
// though it can still be swept in incidentally when it sits before an included non-logs section.
//
// A non-logs section that alone would exceed maxBytes is skipped rather than aborting the whole
// extension, so the result is the largest offset achievable. This depends only on each section's own
// layout offset, not its position in md.Sections, since sections are not guaranteed to be listed in
// on-disk offset order.
//
// maxBytes also guards against a corrupt layout driving a huge allocation in fetchMetadataRegion.
// Only startOff itself exceeding maxBytes fails outright, since then nothing is left to cache, not
// even the file metadata.
func (d *decoder) extendedMetadataRegionEnd(md *filemd.Metadata, startOff, maxBytes int64) (int64, error) {
	if startOff > maxBytes {
		return 0, fmt.Errorf("%w: file metadata alone is %d bytes, exceeding the %d byte limit", errCannotCacheMetadata, startOff, maxBytes)
	}

	// Duplicates an identity pkg/dataobj/sections/logs owns, to avoid an import cycle; the kind string
	// is part of the on-disk format, not an internal detail that could drift.
	logsSectionType := SectionType{Namespace: "github.com/grafana/loki", Kind: "logs"}

	regionEnd := startOff

	for i, sec := range md.Sections {
		// Not wrapped in errCannotCacheMetadata: Object.init calls getSectionType on every section right
		// after Metadata returns, on both the cached and uncached paths, so a bad section type fails
		// the open identically either way. Falling back to metadataDirect first would only delay that
		// same failure by one doomed attempt.
		typ, err := getSectionType(md, sec)
		if err != nil {
			return 0, fmt.Errorf("getting section %d type: %w", i, err)
		}
		if typ.Equals(logsSectionType) {
			// Its layout is skipped too, not just its contribution: since it is excluded regardless, a
			// corrupt layout here must not block caching every other section that does fit.
			continue
		}

		region := sec.GetLayout().GetMetadata()
		if region.GetOffset() > math.MaxInt64 || region.GetLength() > math.MaxInt64 {
			return 0, fmt.Errorf("%w: section %d metadata region is invalid: offset=%d length=%d", errCannotCacheMetadata, i, region.GetOffset(), region.GetLength())
		}

		end := startOff + int64(region.GetOffset()) + int64(region.GetLength())
		if end < startOff {
			return 0, fmt.Errorf("%w: section %d metadata region overflows: offset=%d length=%d", errCannotCacheMetadata, i, region.GetOffset(), region.GetLength())
		}

		if end <= maxBytes {
			regionEnd = max(regionEnd, end)
		}
	}

	return regionEnd, nil
}

// fetchAndDecodeMetadata reads the prefetch window from offset 0 and decodes the file metadata. It
// returns the decoded metadata, the buffer it read (starting at offset 0, so callers can reuse it as
// the prefetched window), and the file metadata size from the header.
func (d *decoder) fetchAndDecodeMetadata(ctx context.Context) (*filemd.Metadata, []byte, uint64, error) {
	prefetchBytes := d.effectivePrefetchBytes()

	// TODO(rfratto): If there was a Close method on [Object], we could use a
	// pool here to reduce allocations and return it to the pool when the object
	// is closed.
	buf := make([]byte, prefetchBytes)

	n, err := d.readFirstBytes(ctx, prefetchBytes, buf)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("reading first %d bytes: %w", prefetchBytes, err)
	}
	buf = buf[:n]

	header, err := d.header(buf)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("reading header: %w", err)
	}

	var md *filemd.Metadata
	if header.MetadataSize+8 <= uint64(len(buf)) {
		// Optimistic read was successful, so we can decode the metadata from
		// the buffer.
		md, err = decodeFileMetadata(bytes.NewReader(buf[8:]))
		if err != nil {
			return nil, nil, 0, fmt.Errorf("decoding file metadata: %w", err)
		}
	} else {
		// Optimistic read was too small, so we need to read the metadata fully.
		rc, err := d.rr.ReadRange(ctx, int64(8), int64(header.MetadataSize))
		if err != nil {
			return nil, nil, 0, fmt.Errorf("getting metadata: %w", err)
		}
		defer rc.Close()

		br := bufpool.GetReader(rc)
		defer bufpool.PutReader(br)

		md, err = decodeFileMetadata(br)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("decoding file metadata: %w", err)
		}
	}

	return md, buf, header.MetadataSize, nil
}

func (d *decoder) readFirstBytes(ctx context.Context, readSize int64, buf []byte) (int, error) {
	rc, err := d.rr.ReadRange(ctx, 0, readSize)
	if err != nil {
		return 0, fmt.Errorf("reading data: %w", err)
	}
	defer rc.Close()

	// readSize may be bigger than the actual file, but we'll read as much as
	// possible and let the decoders decide if the file is missing data.
	n, err := io.ReadAtLeast(rc, buf, int(readSize))
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return n, err
	}
	return n, nil
}

func (d *decoder) objectSize(ctx context.Context) (int64, error) {
	if d.size == 0 {
		size, err := d.rr.Size(ctx)
		if err != nil {
			return 0, fmt.Errorf("reading size: %w", err)
		}
		d.size = size
	}
	return d.size, nil
}

type header struct {
	MetadataSize uint64
}

func (d *decoder) header(headData []byte) (header, error) {
	off := min(int64(len(headData)), 8)

	br := bytes.NewReader(headData[:off])

	metadataSize, err := decodeHeader(br)
	if err != nil {
		return header{}, fmt.Errorf("scanning header: %w", err)
	}

	return header{MetadataSize: uint64(metadataSize)}, nil
}

func (d *decoder) SectionReader(metadata *filemd.Metadata, section *filemd.SectionInfo, extensionData []byte) SectionReader {
	rr := d.rr
	if d.prefetchedRangeReader != nil {
		rr = d.prefetchedRangeReader
	}

	return &sectionReader{
		rr:  rr,
		md:  metadata,
		sec: section,

		startOff: d.startOff,

		extensionData: extensionData,
	}
}

func (d *decoder) effectivePrefetchBytes() int64 {
	return max(minimumPrefetchBytes, d.prefetchBytes)
}

func (d *decoder) setPrefetchedBytes(offset int64, data []byte) {
	if len(data) == 0 {
		return
	}

	d.prefetchedRangeReader = &prefetchedRangeReader{
		inner:          d.rr,
		prefetchOffset: offset,
		prefetched:     data,
	}
}

var errMissingSectionType = errors.New("missing section type")

// getSectionType returns the [SectionType] for the given section.
func getSectionType(md *filemd.Metadata, section *filemd.SectionInfo) (SectionType, error) {
	if section.TypeRef == 0 || section.TypeRef >= uint32(len(md.Types)) {
		return SectionType{}, fmt.Errorf("%w: typeRef %d out of bounds [1, %d)", errMissingSectionType, section.TypeRef, len(md.Types))
	}

	var (
		rawType = md.Types[section.TypeRef]

		namespaceRef = rawType.NameRef.NamespaceRef
		kindRef      = rawType.NameRef.KindRef
	)

	// Validate the namespace and kind references.
	if namespaceRef == 0 || namespaceRef >= uint32(len(md.Dictionary)) {
		return SectionType{}, fmt.Errorf("%w: namespaceRef %d out of bounds [1, %d)", errMissingSectionType, namespaceRef, len(md.Dictionary))
	} else if kindRef == 0 || kindRef >= uint32(len(md.Dictionary)) {
		return SectionType{}, fmt.Errorf("%w: kindRef %d out of bounds [1, %d)", errMissingSectionType, kindRef, len(md.Dictionary))
	}

	return SectionType{
		Namespace: md.Dictionary[namespaceRef],
		Kind:      md.Dictionary[kindRef],
		Version:   rawType.Version,
	}, nil
}
