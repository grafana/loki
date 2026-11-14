package metadata

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/format"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

// errHeadTooShort means the head is too short to hold the header and the file metadata.
var errHeadTooShort = errors.New("the head is too short")

// unsupportedError means an object uses a feature that this version does not know. A newer writer can
// produce such an object.
type unsupportedError string

func (e unsupportedError) Error() string { return string(e) }

// decodeHeaderAndFileMetadata decodes the header and the file metadata from the first bytes of an
// object. startOffset is the offset where the section offsets start: the size of the header and the
// file metadata. It returns an error that wraps errHeadTooShort when head is too short to hold them.
func decodeHeaderAndFileMetadata(head []byte) (md *filemd.Metadata, startOffset int64, err error) {
	if len(head) < format.HeaderSize {
		return nil, 0, fmt.Errorf("%w: it has %d bytes, the header needs %d", errHeadTooShort, len(head), format.HeaderSize)
	}
	if magic := head[:len(format.FileMagic)]; string(magic) != format.FileMagic {
		return nil, 0, fmt.Errorf("unexpected magic %q", magic)
	}

	startOffset = format.HeaderSize + int64(binary.LittleEndian.Uint32(head[len(format.FileMagic):format.HeaderSize]))
	if int64(len(head)) < startOffset {
		return nil, 0, fmt.Errorf("%w: it has %d bytes, the header and the file metadata need %d", errHeadTooShort, len(head), startOffset)
	}

	body := head[format.HeaderSize:startOffset]
	version, n := binary.Uvarint(body)
	if n <= 0 {
		return nil, 0, errors.New("reading file format version: the file metadata is truncated")
	}
	if version != format.FileFormatVersion {
		return nil, 0, fmt.Errorf("unexpected file format version %d", version)
	}

	md = &filemd.Metadata{}
	if err := decodeProto(body[n:], md); err != nil {
		return nil, 0, fmt.Errorf("decoding file metadata: %w", err)
	}
	return md, startOffset, nil
}

// metadataRegionSize computes the size of the region from the file metadata alone: the bytes from
// offset 0 to the end of the metadata of all sections. The region holds no section data, because a
// section can have a lot of it. metadataRegionSize returns an error that wraps ErrNotPrefetchable
// when a layout is invalid.
func metadataRegionSize(md *filemd.Metadata, startOffset int64) (int64, error) {
	size := startOffset
	for i, sec := range md.Sections {
		metadataOffset, metadataLength, err := sectionRegionRange(sec.GetLayout().GetMetadata(), startOffset)
		if err != nil {
			return 0, fmt.Errorf("%w: section %d metadata: %w", ErrNotPrefetchable, i, err)
		}
		size = max(size, metadataOffset+metadataLength)
	}
	return size, nil
}

// splitMetadataRegionToGroups cuts region, the first bytes of the object, into the groups of
// prefetched ranges that the cache holds. The ranges alias region.
func splitMetadataRegionToGroups(md *filemd.Metadata, startOffset int64, region []byte) (prefetchedRangesGroups, error) {
	out := prefetchedRangesGroups{
		Groups:      map[prefetchedRangesID]PrefetchedRanges{},
		ColumnNames: map[string]struct{}{},
	}

	add := func(id prefetchedRangesID, offset, length int64) error {
		if !withinRegion(offset, length, region) {
			return fmt.Errorf("range of %d bytes at %d is outside the %d byte region", length, offset, len(region))
		}
		if length > 0 {
			out.Groups[id] = append(out.Groups[id], PrefetchedRange{Offset: offset, Data: region[offset : offset+length : offset+length]})
		}
		return nil
	}

	// The file metadata group holds the header and the file metadata. The core group holds the
	// metadata of every non-logs section, and from every logs section the section metadata and the
	// column metadata of the stream ID and timestamp columns. No group holds section data.
	if err := add(prefetchedRangesID{Kind: kindFileMetadata}, 0, startOffset); err != nil {
		return prefetchedRangesGroups{}, err
	}

	// The message and core groups exist even when empty, so a lookup can tell them from a miss.
	out.Groups[prefetchedRangesID{Kind: kindMessage}] = nil
	out.Groups[prefetchedRangesID{Kind: kindCore}] = nil

	for i, sec := range md.Sections {
		isLogs, err := isLogsSection(md, sec)
		if err != nil {
			return prefetchedRangesGroups{}, fmt.Errorf("section %d: %w", i, err)
		}

		metadataOffset, metadataLength, err := sectionRegionRange(sec.GetLayout().GetMetadata(), startOffset)
		if err != nil {
			return prefetchedRangesGroups{}, fmt.Errorf("section %d metadata: %w", i, err)
		}

		if isLogs {
			if err := splitLogsSectionMetadataToGroups(&out, sec, region, metadataOffset, add); err != nil {
				return prefetchedRangesGroups{}, fmt.Errorf("section %d: %w", i, err)
			}
		} else {
			if err := add(prefetchedRangesID{Kind: kindCore}, metadataOffset, metadataLength); err != nil {
				return prefetchedRangesGroups{}, fmt.Errorf("section %d: %w", i, err)
			}
		}
	}

	// The sections are not in offset order in every object, and a logs section adds its section
	// metadata before the column metadata that lies before it. So the ranges of a group can be out of
	// order. Sorted ranges that touch each other join into one range.
	var all PrefetchedRanges
	for id, ranges := range out.Groups {
		slices.SortFunc(ranges, func(a, b PrefetchedRange) int { return cmp.Compare(a.Offset, b.Offset) })
		out.Groups[id] = ranges.mergeAdjacent(region)
		all = append(all, out.Groups[id]...)
	}

	// A prefetch set combines groups, and a reader needs sorted ranges that do not overlap. Metadata
	// that overlaps other metadata is not valid, so the object is not prefetchable.
	slices.SortFunc(all, func(a, b PrefetchedRange) int { return cmp.Compare(a.Offset, b.Offset) })
	if !all.Valid() {
		return prefetchedRangesGroups{}, errInvalidRanges
	}

	return out, nil
}

// splitLogsSectionMetadataToGroups adds the ranges of one logs section to out. metadataOffset is the
// absolute offset of the metadata region of the section.
//
// It returns an error when a column has a type that it does not know. The caller then treats the
// object as not prefetchable, so a new column type cannot drop out of the groups without notice.
func splitLogsSectionMetadataToGroups(out *prefetchedRangesGroups, sec *filemd.SectionInfo, region []byte, metadataOffset int64, add func(prefetchedRangesID, int64, int64) error) error {
	var ext datasetmd.SectionInfoExtension
	if err := decodeProto(sec.ExtensionData, &ext); err != nil {
		return fmt.Errorf("decoding the section extension: %w", err)
	}

	sectionMetadataOffset := absoluteOffset(metadataOffset, ext.SectionMetadataOffset)
	sectionMetadataLength := int64(ext.SectionMetadataLength)
	if !withinRegion(sectionMetadataOffset, sectionMetadataLength, region) {
		return errors.New("the section metadata is outside the region")
	}

	// The core group holds the section metadata, which lists the columns. A reader needs it to open
	// the section.
	if err := add(prefetchedRangesID{Kind: kindCore}, sectionMetadataOffset, sectionMetadataLength); err != nil {
		return err
	}

	var sm datasetmd.SectionMetadata
	if err := decodeProto(region[sectionMetadataOffset:sectionMetadataOffset+sectionMetadataLength], &sm); err != nil {
		return fmt.Errorf("decoding the section metadata: %w", err)
	}

	dictionary := func(ref uint32) string {
		if ref == 0 || ref >= uint32(len(sm.Dictionary)) {
			return ""
		}
		return sm.Dictionary[ref]
	}
	for i, col := range sm.Columns {
		columnMetadataOffset := absoluteOffset(metadataOffset, col.ColumnMetadataOffset)
		columnMetadataLength := int64(col.ColumnMetadataLength)

		var id prefetchedRangesID
		switch logicalType := dictionary(col.GetType().GetLogicalRef()); logicalType {
		case format.LogsColumnStreamID, format.LogsColumnTimestamp:
			id = prefetchedRangesID{Kind: kindCore}
		case format.LogsColumnMessage:
			id = prefetchedRangesID{Kind: kindMessage}
		case format.LogsColumnMetadata:
			id = prefetchedRangesID{Kind: kindColumn, ColumnName: dictionary(col.TagRef)}
			out.ColumnNames[id.ColumnName] = struct{}{}
		default:
			return unsupportedError(fmt.Sprintf("column %d has an unknown type %q", i, logicalType))
		}
		if err := add(id, columnMetadataOffset, columnMetadataLength); err != nil {
			return err
		}
	}
	return nil
}

// decodeProto decodes a message that protocodec encoded at the start of b. It first checks that the
// size prefix fits in b, because protocodec.Decode allocates the size that the prefix names.
func decodeProto(b []byte, pb proto.Message) error {
	size, n := binary.Uvarint(b)
	if n <= 0 || size > uint64(len(b)-n) {
		return errors.New("the message is truncated")
	}
	return protocodec.Decode(bytes.NewReader(b), pb)
}

// sectionRegionRange returns the absolute offset and the length of a region of a section.
func sectionRegionRange(region *filemd.Region, startOffset int64) (offset, length int64, err error) {
	if region == nil {
		return 0, 0, errors.New("the section has no layout")
	}
	if region.Offset > math.MaxInt64 || region.Length > math.MaxInt64 {
		return 0, 0, errors.New("the region is too large")
	}
	offset = startOffset + int64(region.Offset)
	length = int64(region.Length)
	if offset < startOffset || offset+length < offset {
		return 0, 0, errors.New("the region overflows")
	}
	return offset, length, nil
}

// isLogsSection reports whether sec is a logs section. It returns an error when a type reference of
// sec is out of bounds.
func isLogsSection(md *filemd.Metadata, sec *filemd.SectionInfo) (bool, error) {
	if sec.TypeRef == 0 || sec.TypeRef >= uint32(len(md.Types)) {
		return false, fmt.Errorf("type reference %d is out of bounds", sec.TypeRef)
	}
	ref := md.Types[sec.TypeRef].GetNameRef()
	if ref.GetNamespaceRef() == 0 || ref.GetNamespaceRef() >= uint32(len(md.Dictionary)) ||
		ref.GetKindRef() == 0 || ref.GetKindRef() >= uint32(len(md.Dictionary)) {
		return false, errors.New("type name reference is out of bounds")
	}
	return md.Dictionary[ref.GetNamespaceRef()] == format.SectionNamespace && md.Dictionary[ref.GetKindRef()] == format.LogsSectionKind, nil
}

// absoluteOffset returns the absolute offset of an offset that is relative to base, or -1 when the sum
// does not fit in an int64. A negative offset is never inside a region.
func absoluteOffset(base int64, relative uint64) int64 {
	if relative > uint64(math.MaxInt64-base) {
		return -1
	}
	return base + int64(relative)
}

// withinRegion reports whether length bytes at offset lie inside region. It compares without adding,
// so untrusted offsets and lengths cannot overflow.
func withinRegion(offset, length int64, region []byte) bool {
	n := int64(len(region))
	return offset >= 0 && length >= 0 && offset <= n && length <= n-offset
}
