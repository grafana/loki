package dataobj

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"golang.org/x/sync/errgroup"

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
}

func (d *decoder) Metadata(ctx context.Context) (*filemd.Metadata, error) {
	prefetchBytes := d.effectivePrefetchBytes()

	// TODO(rfratto): If there was a Close method on [Object], we could use a
	// pool here to reduce allocations and return it to the pool when the object
	// is closed.
	buf := make([]byte, prefetchBytes)

	g, gctx := errgroup.WithContext(ctx)

	// We launch a separate goroutine to cache the object size in the background
	// as we read the header.
	//
	// This lowers the cost of the fallback case (one fewer round trip), and
	// allows [Object.Size] to work.
	if d.size == 0 {
		g.Go(func() error {
			if _, err := d.objectSize(gctx); err != nil {
				return fmt.Errorf("fetching object size: %w", err)
			}
			return nil
		})
	}

	g.Go(func() error {
		n, err := d.readFirstBytes(gctx, prefetchBytes, buf)
		if err != nil {
			return fmt.Errorf("reading first %d bytes: %w", prefetchBytes, err)
		}

		buf = buf[:n]
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	header, err := d.header(buf)
	if err != nil && errors.Is(err, errLegacyMagic) {
		// Fall back to legacy metadata, reusing the same prefetch buffer we
		// allocated already.
		buf = buf[:0]
		return d.legacyMetadata(ctx, buf)
	}

	d.setPrefetchedBytes(0, buf)
	d.startOff = int64(8) + int64(header.MetadataSize)

	if header.MetadataSize+8 <= uint64(len(buf)) {
		// Optimistic read was successful, so we can decode the metadata from
		// the buffer.
		rc := bytes.NewReader(buf[8:])
		return decodeFileMetadata(rc)
	}

	// Optimistic read was too small, so we need to read the metadata fully.
	rc, err := d.rr.ReadRange(ctx, int64(8), int64(header.MetadataSize))
	if err != nil {
		return nil, fmt.Errorf("getting metadata: %w", err)
	}
	defer rc.Close()

	br := bufpool.GetReader(rc)
	defer bufpool.PutReader(br)

	return decodeFileMetadata(br)
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

func (d *decoder) legacyMetadata(ctx context.Context, buf []byte) (*filemd.Metadata, error) {
	objectSize, err := d.objectSize(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading object size: %w", err)
	}

	readSize := min(objectSize, d.effectivePrefetchBytes())
	buf = slices.Grow(buf, int(readSize))
	buf = buf[:readSize]

	n, err := d.readLastBytes(ctx, readSize, buf)
	if err != nil {
		return nil, fmt.Errorf("reading last %d bytes: %w", readSize, err)
	}
	buf = buf[:n]

	d.setPrefetchedBytes(objectSize-readSize, buf)

	tailer, err := d.tailer(ctx, buf)
	if err != nil {
		return nil, fmt.Errorf("reading tailer: %w", err)
	}

	if tailer.MetadataSize+8 <= uint64(len(buf)) {
		// Optimistic read was successful, so we can decode the metadata from the buffer
		rc := bytes.NewReader(buf[len(buf)-int(tailer.MetadataSize)-8 : len(buf)-8])
		return decodeFileMetadata(rc)
	}

	// Optimistic read was too small, so we need to read the metadata fully
	rc, err := d.rr.ReadRange(ctx, int64(tailer.FileSize-tailer.MetadataSize-8), int64(tailer.MetadataSize))
	if err != nil {
		return nil, fmt.Errorf("getting metadata: %w", err)
	}
	defer rc.Close()

	br := bufpool.GetReader(rc)
	defer bufpool.PutReader(br)

	return decodeFileMetadata(br)
}

func (d *decoder) readLastBytes(ctx context.Context, readSize int64, buf []byte) (int, error) {
	objectSize, err := d.objectSize(ctx)
	if err != nil {
		return 0, fmt.Errorf("reading object size: %w", err)
	}

	// Truncate readSize to the object size so the range doesn't go negative.
	readSize = min(readSize, objectSize)

	rc, err := d.rr.ReadRange(ctx, objectSize-readSize, readSize)
	if err != nil {
		return 0, fmt.Errorf("reading last %d bytes: %w", readSize, err)
	}
	defer rc.Close()

	return io.ReadAtLeast(rc, buf, int(readSize))
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

type tailer struct {
	MetadataSize uint64
	FileSize     uint64
}

func (d *decoder) tailer(ctx context.Context, tailData []byte) (tailer, error) {
	objectSize, err := d.objectSize(ctx)
	if err != nil {
		return tailer{}, fmt.Errorf("reading object size: %w", err)
	}

	br := bytes.NewReader(tailData[len(tailData)-8:])

	metadataSize, err := decodeTailer(br)
	if err != nil {
		return tailer{}, fmt.Errorf("scanning tailer: %w", err)
	}

	return tailer{
		MetadataSize: uint64(metadataSize),
		FileSize:     uint64(objectSize),
	}, nil
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
