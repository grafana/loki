package dataobj

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
	"github.com/grafana/loki/v3/pkg/scratch"
)

var (
	magic = []byte("THOR")
)

const (
	fileFormatVersion = 0x1
)

// encoder encodes a data object. Data objects are hierarchical, split into
// distinct sections that contain their own hierarchy.
type encoder struct {
	startOffset int // Byte offset in the file where data starts after the header.
	totalBytes  int // Total bytes buffered in store so far.

	store    scratch.Store
	sections []sectionInfo // Sections buffered in store.

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

// newEncoder creates a new Encoder which writes a data object to the provided
// writer.
func newEncoder(store scratch.Store) *encoder {
	return &encoder{
		startOffset: len(magic),
		store:       store,
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

func (enc *encoder) Metadata() (*filemd.Metadata, error) {
	enc.initDictionary()

	offset := enc.startOffset

	// The data regions and metadata regions of all sections are encoded
	// contiguously. To represent this in the SectionInfo headers, we update
	// them in two passes, once for the data and once for the metadata.
	sections := make([]*filemd.SectionInfo, len(enc.sections))

	// Determine data region locations.
	for i, info := range enc.sections {
		sections[i] = &filemd.SectionInfo{
			TypeRef: enc.getTypeRef(info.Type),
			Layout: &filemd.SectionLayout{
				Data: &filemd.Region{
					Offset: uint64(offset),
					Length: uint64(info.Data.Size),
				},
			},

			ExtensionData: info.ExtensionData,
			TenantRef:     enc.getDictionaryKey(info.Tenant),
		}

		offset += info.Data.Size
	}

	// Determine metadta region location.
	for i, info := range enc.sections {
		// sections[i] is initialized in the previous loop, so we can directly
		// update the layout to include the metadata region.
		sections[i].Layout.Metadata = &filemd.Region{
			Offset: uint64(offset),
			Length: uint64(info.Metadata.Size),
		}

		offset += info.Metadata.Size
	}

	md := &filemd.Metadata{
		Sections:   sections,
		Dictionary: enc.dictionary,
		Types:      enc.rawTypes,

		// We need to temporarily set ObjectSize to a non-zero value to ensure
		// that the field is included when we compute the total object size.
		ObjectSize: math.MinInt64,
	}

	md.ObjectSize = enc.encodeSize(md)
	return md, nil
}

// encodeSize reports the final encoded size of the object.
func (enc *encoder) encodeSize(computedMetadata *filemd.Metadata) int64 {
	var size int64

	size += int64(len(magic)) // header

	// body
	for _, sec := range enc.sections {
		size += int64(sec.Data.Size)
		size += int64(sec.Metadata.Size)
	}

	// metadata
	size += int64(streamio.UvarintSize(fileFormatVersion))
	size += int64(protocodec.Size(computedMetadata))

	// tailer
	size += int64(4) // file metadata size (32 bits)
	size += int64(len(magic))

	return size
}

// Bytes returns the total number of bytes appended to the data object.
func (enc *encoder) Bytes() int { return enc.startOffset + enc.totalBytes }

// Flush converts all accumulated data into a [snapshot], allowing for streaming
// encoded bytes. [snapshot.Close] should be called when the snapshot is no
// longer needed to release sections.
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
	// body:
	//  [section data + metadata]
	// tailer:
	//  [file metadata version]
	//  [file metadata]
	//  [file metadata size (32 bits)]
	//  [magic]
	//
	// The file metadata size *must not* be varint since we need the last 8 bytes
	// of the file to consistently retrieve the tailer.

	var tailerBuffer bytes.Buffer

	metadata, err := enc.Metadata()
	if err != nil {
		return nil, fmt.Errorf("building metadata: %w", err)
	} else if err := streamio.WriteUvarint(&tailerBuffer, fileFormatVersion); err != nil {
		return nil, err
	} else if err := protocodec.Encode(&tailerBuffer, metadata); err != nil {
		return nil, err
	} else if err := binary.Write(&tailerBuffer, binary.LittleEndian, uint32(tailerBuffer.Len())); err != nil {
		return nil, fmt.Errorf("writing metadata size: %w", err)
	} else if _, err := tailerBuffer.Write(magic); err != nil {
		return nil, fmt.Errorf("writing magic tailer: %w", err)
	}

	// Convert our sections into regions for the snapshot to use. The order of
	// regions *must* match the order of offset+length written in
	// [encoder.Metadata]: all the data regions, followed by all the metadata
	// regions.
	regions := make([]sectionRegion, 0, len(enc.sections)*2)
	for _, sec := range enc.sections {
		regions = append(regions, sec.Data)
	}
	for _, sec := range enc.sections {
		regions = append(regions, sec.Metadata)
	}

	snapshot, err := newSnapshot(
		enc.store,
		magic, // header
		regions,
		tailerBuffer.Bytes(), // tailer
	)
	if err != nil {
		return nil, fmt.Errorf("creating snapshot: %w", err)
	} else if snapshot.Size() != metadata.ObjectSize {
		panic(fmt.Sprintf("snapshot size %d does not match computed metadata size %d", snapshot.Size(), metadata.ObjectSize))
	}
	return snapshot, nil
}

// Reset resets the Encoder to a fresh state.
func (enc *encoder) Reset() {
	enc.startOffset = len(magic)
	enc.totalBytes = 0

	enc.sections = nil

	enc.typesReady = false
	enc.dictionary = nil
	enc.dictionaryLookup = nil
	enc.rawTypes = nil
	enc.typeRefLookup = nil
}

type countingWriter struct {
	w     streamio.Writer
	count int64
}

func (cw *countingWriter) Write(p []byte) (n int, err error) {
	n, err = cw.w.Write(p)
	cw.count += int64(n)
	return n, err
}

func (cw *countingWriter) WriteByte(c byte) error {
	if err := cw.w.WriteByte(c); err != nil {
		return err
	}
	cw.count++
	return nil
}
