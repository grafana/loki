package dataobj

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/gogo/protobuf/proto"

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
func (enc *encoder) AppendSection(typ SectionType, data, metadata []byte, extension []byte) {
	var (
		dataHandle     = enc.store.Put(data)
		metadataHandle = enc.store.Put(metadata)
	)

	enc.sections = append(enc.sections, sectionInfo{
		Type: typ,

		Data:     dataHandle,
		Metadata: metadataHandle,

		DataSize:     len(data),
		MetadataSize: len(metadata),

		ExtensionData: extension,
	})

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
	// Reserve the zero index in the dictionary for an invalid entry. This is
	// only required for the type refs, but it's still easier to debug.
	enc.dictionary = []string{""}
	enc.dictionaryLookup = map[string]uint32{"": 0}

	enc.rawTypes = []*filemd.SectionType{
		{NameRef: nil}, // Invalid type.
	}

	var invalidType SectionType // Zero value for SectionType is reserved for invalid types.
	enc.typeRefLookup = map[SectionType]uint32{invalidType: 0}

	enc.typesReady = true
}

// getDictionaryKey returns the dictionary key for the given text or creates a
// new entry.
func (enc *encoder) getDictionaryKey(text string) uint32 {
	if enc.dictionaryLookup == nil {
		enc.dictionaryLookup = make(map[string]uint32)
	}

	key, ok := enc.dictionaryLookup[text]
	if ok {
		return key
	}

	key = uint32(len(enc.dictionary))
	enc.dictionary = append(enc.dictionary, text)
	enc.dictionaryLookup[text] = key
	return key
}

func (enc *encoder) Metadata() (proto.Message, error) {
	sections := make([]*filemd.SectionInfo, len(enc.sections))

	offset := enc.startOffset

	for i, info := range enc.sections {
		dataOffset := offset                     // Data starts at the current total offset
		metaOffset := dataOffset + info.DataSize // Metadata starts right after data

		sections[i] = &filemd.SectionInfo{
			TypeRef: enc.getTypeRef(info.Type),
			Layout: &filemd.SectionLayout{
				Data: &filemd.Region{
					Offset: uint64(dataOffset),
					Length: uint64(info.DataSize),
				},
				Metadata: &filemd.Region{
					Offset: uint64(metaOffset),
					Length: uint64(info.MetadataSize),
				},
			},

			ExtensionData: info.ExtensionData,
		}

		offset += info.DataSize + info.MetadataSize
	}

	return &filemd.Metadata{
		Sections:   sections,
		Dictionary: enc.dictionary,
		Types:      enc.rawTypes,
	}, nil
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

	snapshot, err := newSnapshot(
		enc.store,
		magic, // header
		slices.Clone(enc.sections),
		tailerBuffer.Bytes(), // tailer
	)
	if err != nil {
		return nil, fmt.Errorf("creating snapshot: %w", err)
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
