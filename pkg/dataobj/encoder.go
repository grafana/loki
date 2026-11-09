package dataobj

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
	"github.com/grafana/loki/v3/pkg/scratch"
)

var magic = []byte("DOBJ")

const (
	fileFormatVersion = 0x1
)

// encoder encodes a data object. Data objects are hierarchical, split into
// distinct sections that contain their own hierarchy.
type encoder struct {
	totalBytes int // Total bytes buffered in store so far.

	store    scratch.Store
	sections []sectionInfo // Sections buffered in store.

	// prioritySections lists the section types that come first in the
	// object, in this order. Types match by [SectionType.Equals].
	prioritySections []SectionType

	// sizeLeveled is the info that [encoder.metadata] writes into the file
	// metadata of every object. If sizeLeveled is nil, the encoder writes no
	// compaction info.
	sizeLeveled *SizeLeveledCompaction

	typesReady       bool
	dictionary       []string
	dictionaryLookup map[string]uint32
	rawTypes         []*filemd.SectionType
	typeRefLookup    map[SectionType]uint32
}

type sectionInfo struct {
	Type SectionType

	Data     sectionRegion
	Metadata sectionRegion

	Tenant string // Owning tenant of the section, if any.

	// ExtensionData holds additional encoded info about the section, written to
	// the file-level metadata.
	ExtensionData []byte
}

// newEncoder returns an encoder that buffers sections in store and puts
// sections with a type in prioritySections first. If sizeLeveled is not nil,
// the encoder writes it into the file metadata of every object.
func newEncoder(store scratch.Store, prioritySections []SectionType, sizeLeveled *SizeLeveledCompaction) *encoder {
	return &encoder{
		store:            store,
		prioritySections: slices.Clone(prioritySections),
		sizeLeveled:      sizeLeveled,
	}
}

// AppendSection appends a section to the data object. AppendSection panics if
// typ is not SectionTypeLogs or SectionTypeStreams.
func (enc *encoder) AppendSection(typ SectionType, opts *WriteSectionOptions, data, metadata []byte) {
	var (
		dataHandle     = enc.store.Put(data)
		metadataHandle = enc.store.Put(metadata)
	)

	si := sectionInfo{
		Type: typ,

		Data:     sectionRegion{Handle: dataHandle, Size: len(data)},
		Metadata: sectionRegion{Handle: metadataHandle, Size: len(metadata)},
	}
	if opts != nil {
		si.Tenant = opts.Tenant
		si.ExtensionData = slices.Clone(opts.ExtensionData) // Avoid retaining references to caller memory.
	}

	enc.sections = append(enc.sections, si)
	enc.totalBytes += len(data) + len(metadata)
}

// getTypeRef returns the type reference for the given type or creates a new
// one.
func (enc *encoder) getTypeRef(typ SectionType) uint32 {
	if !enc.typesReady {
		enc.initTypeRefs()
	}

	ref, ok := enc.typeRefLookup[typ]
	if !ok {
		// Create a new type reference.
		enc.typeRefLookup[typ] = uint32(len(enc.rawTypes))
		enc.rawTypes = append(enc.rawTypes, &filemd.SectionType{
			NameRef: &filemd.SectionType_NameRef{
				NamespaceRef: enc.getDictionaryKey(typ.Namespace),
				KindRef:      enc.getDictionaryKey(typ.Kind),
			},
			Version: typ.Version,
		})
		return enc.typeRefLookup[typ]
	}
	return ref
}

func (enc *encoder) initTypeRefs() {
	enc.initDictionary()

	enc.rawTypes = []*filemd.SectionType{
		{NameRef: nil}, // Invalid type.
	}

	var invalidType SectionType // Zero value for SectionType is reserved for invalid types.
	enc.typeRefLookup = map[SectionType]uint32{invalidType: 0}

	enc.typesReady = true
}

func (enc *encoder) initDictionary() {
	if len(enc.dictionary) > 0 && len(enc.dictionaryLookup) > 0 {
		return // Already initialized.
	}

	// Reserve the zero index in the dictionary for an invalid entry. This is
	// only required for the type refs, but it's still easier to debug.
	enc.dictionary = []string{""}
	enc.dictionaryLookup = map[string]uint32{"": 0}
}

// getDictionaryKey returns the dictionary key for the given text or creates a
// new entry.
func (enc *encoder) getDictionaryKey(text string) uint32 {
	enc.initDictionary()

	key, ok := enc.dictionaryLookup[text]
	if ok {
		return key
	}

	key = uint32(len(enc.dictionary))
	enc.dictionary = append(enc.dictionary, text)
	enc.dictionaryLookup[text] = key
	return key
}

// sortSections orders enc.sections for writing. The section table, the
// metadata regions, and the data regions all use this order.
//
// Sections with a type in enc.prioritySections come first, grouped in the
// order of enc.prioritySections. All other sections follow.
//
// The sort is stable, so sections of one type keep their append order.
// Readers refer to a section by its index among sections of its type, such as
// the index of a logs section.
func (enc *encoder) sortSections() {
	rank := func(typ SectionType) int {
		if i := slices.IndexFunc(enc.prioritySections, typ.Equals); i >= 0 {
			return i
		}
		return len(enc.prioritySections)
	}

	slices.SortStableFunc(enc.sections, func(a, b sectionInfo) int {
		return cmp.Compare(rank(a.Type), rank(b.Type))
	})
}

// metadata returns the file metadata for the buffered sections, in the order
// of enc.sections. Call sortSections first.
func (enc *encoder) metadata() (*filemd.Metadata, error) {
	enc.initDictionary()

	// relativeOffset is the offset where regions are written relative to the
	// "body" of the file (e.g., after the header).
	//
	// Decoders use the known header size to reinterpret the offset into an
	// absolute offset.
	relativeOffset := 0

	// The metadata regions of all sections come first and are contiguous. The
	// data regions follow and are also contiguous. The first loop places the
	// metadata regions, and the second loop places the data regions.
	sections := make([]*filemd.SectionInfo, len(enc.sections))

	// Determine metadata region location.
	for i, info := range enc.sections {
		sections[i] = &filemd.SectionInfo{
			TypeRef: enc.getTypeRef(info.Type),
			Layout: &filemd.SectionLayout{
				Metadata: &filemd.Region{
					Offset: uint64(relativeOffset),
					Length: uint64(info.Metadata.Size),
				},
			},

			ExtensionData: info.ExtensionData,
			TenantRef:     enc.getDictionaryKey(info.Tenant),
		}

		relativeOffset += info.Metadata.Size
	}

	// Determine data region locations.
	for i, info := range enc.sections {
		// sections[i] is initialized in the previous loop, so we can directly
		// update the layout to include the data region.
		sections[i].Layout.Data = &filemd.Region{
			Offset: uint64(relativeOffset),
			Length: uint64(info.Data.Size),
		}

		relativeOffset += info.Data.Size
	}

	md := &filemd.Metadata{
		Sections:   sections,
		Dictionary: enc.dictionary,
		Types:      enc.rawTypes,
	}
	if enc.sizeLeveled != nil {
		md.Compaction = &filemd.CompactionInfo{
			SizeLeveled: &filemd.SizeLeveledCompaction{Level: enc.sizeLeveled.Level},
		}
	}
	return md, nil
}

// Bytes returns the total number of bytes appended to the data object.
func (enc *encoder) Bytes() int { return enc.totalBytes }

// Flush converts all accumulated data into a [snapshot], allowing for streaming
// encoded bytes. [snapshot.Close] should be called when the snapshot is no
// longer needed to release sections.
//
// The returned snapshot takes over the buffered sections: from that point on
// closing the snapshot, and not resetting the encoder, is what releases them
// from the scratch store.
//
// Callers must manually call [encoder.Reset] after calling Flush to prepare for
// the next encoding session. This is done to allow callers to ensure that the
// returned snapshot is complete before the encoder is reset.
func (enc *encoder) Flush() (*snapshot, error) {
	if len(enc.sections) == 0 {
		return nil, fmt.Errorf("empty Encoder")
	}

	// The overall structure is:
	//
	// header:
	//  [magic]
	//  [file metadata + version size (32 bits)]
	//  [file metadata version]
	//  [file metadata]
	// body:
	//  [metadata regions]
	//  [data regions]
	// tailer:
	//  [magic]

	var headerBuffer bytes.Buffer

	enc.sortSections()
	metadata, err := enc.metadata()
	if err != nil {
		return nil, fmt.Errorf("building metadata: %w", err)
	} else if _, err := headerBuffer.Write(magic); err != nil {
		return nil, fmt.Errorf("writing magic header: %w", err)
	}

	metadataSize := streamio.UvarintSize(fileFormatVersion) + protocodec.Size(metadata)

	if err := binary.Write(&headerBuffer, binary.LittleEndian, uint32(metadataSize)); err != nil {
		return nil, fmt.Errorf("writing metadata size: %w", err)
	} else if err := streamio.WriteUvarint(&headerBuffer, fileFormatVersion); err != nil {
		return nil, err
	} else if err := protocodec.Encode(&headerBuffer, metadata); err != nil {
		return nil, err
	}

	// Convert our sections into regions for the snapshot to use. The order of
	// regions *must* match the order of offset+length written in
	// [encoder.metadata]: all the metadata regions, followed by all the data
	// regions.
	//
	// We encode metadata regions first to allow a single prefetch of the start
	// of the file to also prefetch metadata regions.
	regions := make([]sectionRegion, 0, len(enc.sections)*2)
	for _, sec := range enc.sections {
		regions = append(regions, sec.Metadata)
	}
	for _, sec := range enc.sections {
		regions = append(regions, sec.Data)
	}

	snapshot, err := newSnapshot(
		enc.store,
		headerBuffer.Bytes(), // header
		regions,
		magic, // tailer
	)
	if err != nil {
		return nil, fmt.Errorf("creating snapshot: %w", err)
	}

	enc.sections = nil
	return snapshot, nil
}

// Reset resets the Encoder to a fresh state, removing any sections it still
// holds from the scratch store. Reset keeps the compaction info, so the next
// object gets the same info.
func (enc *encoder) Reset() {
	for _, section := range enc.sections {
		// A failed removal is not actionable: the handle is unreachable either
		// way, and [scratch.Store] implementations report their own failures.
		_ = enc.store.Remove(section.Data.Handle)
		_ = enc.store.Remove(section.Metadata.Handle)
	}

	enc.totalBytes = 0

	enc.sections = nil

	enc.typesReady = false
	enc.dictionary = nil
	enc.dictionaryLookup = nil
	enc.rawTypes = nil
	enc.typeRefLookup = nil
}
