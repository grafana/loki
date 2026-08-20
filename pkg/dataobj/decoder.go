package dataobj

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

// minimumPrefetchBytes is the minimum number of bytes to prefetch before
// decoding.
const minimumPrefetchBytes int64 = 16 * 1024

type decoder struct {
	rr            rangeReader
	size          int64
	startOff      int64
	prefetchBytes int64

	prefetchedRangeReader rangeReader

	// metadataCache, when set, serves the metadata prefix (see fetchMetadataPrefix) so an open does not
	// read the metadata from object storage. metadataKey identifies the object in the cache.
	metadataCache MetadataCache
	metadataKey   string
}

func (d *decoder) Metadata(ctx context.Context) (*filemd.Metadata, error) {
	// An empty key would collide every object onto one cache entry, so treat it as no cache.
	if d.metadataCache != nil && d.metadataKey != "" {
		return d.metadataViaCache(ctx)
	}
	return d.metadataDirect(ctx)
}

// metadataDirect reads and decodes the file metadata straight from the range reader. It keeps the whole
// prefetch buffer as the prefetched window, so an over-sized prefetch also serves the first data reads.
func (d *decoder) metadataDirect(ctx context.Context) (*filemd.Metadata, error) {
	md, buf, metadataSize, err := d.fetchAndDecodeMetadata(ctx)
	if err != nil {
		return nil, err
	}
	d.setPrefetchedBytes(0, buf)
	d.startOff = int64(8) + int64(metadataSize)
	return md, nil
}

// metadataViaCache serves the metadata prefix from the cache (loading it on a miss), then decodes the
// file metadata from it. The prefetched window is exactly the prefix, so section-metadata reads hit it
// and only data reads go to object storage.
//
// A cached prefix that does not decode (truncated or corrupt) is not fatal: the read falls back to a
// direct read from object storage, so a poisoned entry never fails queries. It is left to expire by TTL.
func (d *decoder) metadataViaCache(ctx context.Context) (*filemd.Metadata, error) {
	blob, err := d.metadataCache.GetMetadata(ctx, d.metadataKey, d.fetchMetadataPrefix)
	if err != nil {
		return nil, err
	}

	md, metadataSize, ok := d.decodeMetadataFromPrefix(blob)
	if !ok {
		// The cached prefix is unusable (truncated or corrupt); fall back to a direct read.
		return d.metadataDirect(ctx)
	}
	d.setPrefetchedBytes(0, blob)
	d.startOff = int64(8) + int64(metadataSize)
	return md, nil
}

// decodeMetadataFromPrefix decodes the file metadata from a metadata prefix blob and returns the file metadata
// size from the header (which the caller uses to compute startOff). ok is false when the blob is too
// short or does not decode, so the caller can fall back.
func (d *decoder) decodeMetadataFromPrefix(blob []byte) (md *filemd.Metadata, metadataSize uint64, ok bool) {
	header, err := d.header(blob)
	if err != nil || uint64(len(blob)) < header.MetadataSize+8 {
		return nil, 0, false
	}
	md, err = decodeFileMetadata(bytes.NewReader(blob[8:]))
	if err != nil {
		return nil, 0, false
	}
	return md, header.MetadataSize, true
}

// fetchMetadataPrefix reads the contiguous [0, E) prefix that holds the header, the file metadata, and
// every section's metadata region, where E is the offset of the first data region. Reading exactly this
// prefix keeps the cached entry to the metadata alone.
func (d *decoder) fetchMetadataPrefix(ctx context.Context) ([]byte, error) {
	md, buf, metadataSize, err := d.fetchAndDecodeMetadata(ctx)
	if err != nil {
		return nil, err
	}

	// E = end of the last section metadata region = start of the first data region. Section metadata is
	// written contiguously right after the file metadata (see encoder.Flush).
	prefixEnd := int64(8) + int64(metadataSize)
	for _, sec := range md.Sections {
		prefixEnd += int64(sec.GetLayout().GetMetadata().GetLength())
	}

	// Return a right-sized copy, not a slice of buf. The copy is cached and kept as the prefetched window,
	// so it outlives buf. A slice would pin buf's larger backing array.
	if int64(len(buf)) >= prefixEnd {
		return bytes.Clone(buf[:prefixEnd]), nil
	}

	// The metadata is larger than the prefetch window; read the exact prefix.
	rc, err := d.rr.ReadRange(ctx, 0, prefixEnd)
	if err != nil {
		return nil, fmt.Errorf("reading metadata prefix: %w", err)
	}
	defer rc.Close()

	prefix := make([]byte, prefixEnd)
	if _, err := io.ReadFull(rc, prefix); err != nil {
		return nil, fmt.Errorf("reading metadata prefix: %w", err)
	}
	return prefix, nil
}

// fetchAndDecodeMetadata reads the prefetch window from offset 0 and decodes the file metadata. It returns
// the decoded metadata, the buffer it read (starting at offset 0, so callers can reuse it as the
// prefetched window), and the file metadata size from the header.
func (d *decoder) fetchAndDecodeMetadata(ctx context.Context) (*filemd.Metadata, []byte, uint64, error) {
	prefetchBytes := d.effectivePrefetchBytes()

	// A fresh buffer per call: [Object] has no Close, so there is no safe point to return a pooled buffer.
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
		// Optimistic read covered the file metadata; decode it from the buffer.
		md, err = decodeFileMetadata(bytes.NewReader(buf[8:]))
	} else {
		// Optimistic read was too small; read the file metadata fully.
		var rc io.ReadCloser
		rc, err = d.rr.ReadRange(ctx, int64(8), int64(header.MetadataSize))
		if err == nil {
			br := bufpool.GetReader(rc)
			md, err = decodeFileMetadata(br)
			bufpool.PutReader(br)
			_ = rc.Close()
		}
	}
	if err != nil {
		return nil, nil, 0, fmt.Errorf("decoding file metadata: %w", err)
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
