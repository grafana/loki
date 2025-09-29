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

const optimisticReadBytes = 16 * 1024

type decoder struct {
	rr   rangeReader
	size int64
}

func (d *decoder) Metadata(ctx context.Context) (*filemd.Metadata, error) {
	objectSize, err := d.objectSize(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading object size: %w", err)
	}

	readSize := min(objectSize, optimisticReadBytes)
	buf := bufpool.Get(int(readSize))
	defer bufpool.Put(buf)

	if err := d.readLastBytes(ctx, readSize, buf); err != nil {
		return nil, fmt.Errorf("reading last bytes: %w", err)
	}

	tailer, err := d.tailer(ctx, buf)
	if err != nil {
		return nil, fmt.Errorf("reading tailer: %w", err)
	}

	if tailer.MetadataSize+8 <= uint64(buf.Len()) {
		// Optimistic read was successful, so we can decode the metadata from the buffer
		rc := bytes.NewReader(buf.Bytes()[buf.Len()-int(tailer.MetadataSize)-8 : buf.Len()-8])
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

func (d *decoder) readLastBytes(ctx context.Context, readSize int64, buf *bytes.Buffer) error {
	objectSize, err := d.objectSize(ctx)
	if err != nil {
		return fmt.Errorf("reading object size: %w", err)
	}

	rc, err := d.rr.ReadRange(ctx, objectSize-readSize, readSize)
	if err != nil {
		return fmt.Errorf("reading last %d bytes: %w", readSize, err)
	}
	defer rc.Close()

	_, err = io.CopyN(buf, rc, readSize)
	if err != nil {
		return fmt.Errorf("copying last %d bytes: %w", readSize, err)
	}
	return nil
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

type tailer struct {
	MetadataSize uint64
	FileSize     uint64
}

func (d *decoder) tailer(ctx context.Context, tailData *bytes.Buffer) (tailer, error) {
	objectSize, err := d.objectSize(ctx)
	if err != nil {
		return tailer{}, fmt.Errorf("reading object size: %w", err)
	}

	br := bytes.NewReader(tailData.Bytes()[tailData.Len()-8:])

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
	return &sectionReader{rr: d.rr, md: metadata, sec: section, extensionData: extensionData}
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
