package dataobj

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

type decoder struct {
	rr rangeReader
}

func (d *decoder) Metadata(ctx context.Context) (*filemd.Metadata, error) {
	tailer, err := d.tailer(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading tailer: %w", err)
	}

	rc, err := d.rr.ReadRange(ctx, int64(tailer.FileSize-tailer.MetadataSize-8), int64(tailer.MetadataSize))
	if err != nil {
		return nil, fmt.Errorf("getting metadata: %w", err)
	}
	defer rc.Close()

	br := bufpool.GetReader(rc)
	defer bufpool.PutReader(br)

	return decodeFileMetadata(br)
}

type tailer struct {
	MetadataSize uint64
	FileSize     uint64
}

func (d *decoder) tailer(ctx context.Context) (tailer, error) {
	size, err := d.rr.Size(ctx)
	if err != nil {
		return tailer{}, fmt.Errorf("reading attributes: %w", err)
	}

	// Read the last 8 bytes of the object to get the metadata size and magic.
	rc, err := d.rr.ReadRange(ctx, size-8, 8)
	if err != nil {
		return tailer{}, fmt.Errorf("getting file tailer: %w", err)
	}
	defer rc.Close()

	br := bufpool.GetReader(rc)
	defer bufpool.PutReader(br)

	metadataSize, err := decodeTailer(br)
	if err != nil {
		return tailer{}, fmt.Errorf("scanning tailer: %w", err)
	}

	return tailer{
		MetadataSize: uint64(metadataSize),
		FileSize:     uint64(size),
	}, nil
}

func (d *decoder) SectionReader(metadata *filemd.Metadata, section *filemd.SectionInfo) SectionReader {
	return &sectionReader{rr: d.rr, md: metadata, sec: section}
}

var (
	errInvalidSectionType = errors.New("invalid section type")
	errMissingSectionType = errors.New("missing section type")

	legacyKindMapping = map[filemd.SectionKind]SectionType{ //nolint:staticcheck // Ignore deprecation warning
		filemd.SECTION_KIND_UNSPECIFIED: legacySectionTypeInvalid,
		filemd.SECTION_KIND_STREAMS:     legacySectionTypeStreams,
		filemd.SECTION_KIND_LOGS:        legacySectionTypeLogs,
	}
)

// getSectionType returns the [SectionType] for the given section.
// getSectionType is backwards compatible with the old section kind system. The
// md argument is used to look up the type in the metadata.
//
// getSectionType returns an error if the section incorrectly specifies both
// the legacy kind and the new type reference.
func getSectionType(md *filemd.Metadata, section *filemd.SectionInfo) (SectionType, error) {
	var (
		deprecatedKind = section.Kind //nolint:staticcheck // Ignore deprecation warning
	)

	switch {
	case deprecatedKind != filemd.SECTION_KIND_UNSPECIFIED && section.TypeRef != 0:
		return legacySectionTypeInvalid, errors.New("section specifies both legacy kind and new type reference")

	case deprecatedKind != filemd.SECTION_KIND_UNSPECIFIED:
		typ, ok := legacyKindMapping[deprecatedKind]
		if !ok {
			return legacySectionTypeInvalid, errInvalidSectionType
		}
		return typ, nil

	case section.TypeRef != 0:
		if section.TypeRef >= uint32(len(md.Types)) {
			return legacySectionTypeInvalid, fmt.Errorf("%w: typeRef %d out of bounds [1, %d)", errMissingSectionType, section.TypeRef, len(md.Types))
		}

		var (
			rawType = md.Types[section.TypeRef]

			namespaceRef = rawType.NameRef.NamespaceRef
			kindRef      = rawType.NameRef.KindRef
		)

		// Validate the namespace and kind references.
		if namespaceRef == 0 || namespaceRef >= uint32(len(md.Dictionary)) {
			return legacySectionTypeInvalid, fmt.Errorf("%w: namespaceRef %d out of bounds [1, %d)", errMissingSectionType, namespaceRef, len(md.Dictionary))
		} else if kindRef == 0 || kindRef >= uint32(len(md.Dictionary)) {
			return legacySectionTypeInvalid, fmt.Errorf("%w: kindRef %d out of bounds [1, %d)", errMissingSectionType, kindRef, len(md.Dictionary))
		}

		return SectionType{
			Namespace: md.Dictionary[namespaceRef],
			Kind:      md.Dictionary[kindRef],
		}, nil
	}

	return legacySectionTypeInvalid, errMissingSectionType
}
