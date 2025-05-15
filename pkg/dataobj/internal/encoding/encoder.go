package encoding

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

// TODO(rfratto): the memory footprint of [Encoder] can very slowly grow in
// memory as [bufpool] is filled with buffers with increasing capacity:
// each encoding pass has a different number of elements, shuffling which
// elements of the hierarchy get which pooled buffers.
//
// This means that elements that require more bytes will grow the capacity of
// the buffer and put the buffer back into the pool. Even if further encoding
// passes don't need that many bytes, the buffer is kept alive with its larger
// footprint. Given enough time, all buffers in the pool will have a large
// capacity.
//
// The bufpool package provides bucketed pools as a solution to, but this
// requires knowing how many bytes are needed.
//
// Encoder can eventually be moved to the bucketed pools by calculating a
// rolling maximum of encoding size used per element across usages of an
// Encoder instance. This would then allow larger buffers to be eventually
// reclaimed regardless of how often encoding is done.

// Encoder encodes a data object. Data objects are hierarchical, split into
// distinct sections that contain their own hierarchy.
//
// To support hierarchical encoding, a set of Open* methods are provided to
// open a child element. Only one child element may be open at a given time;
// call Commit or Discard on a child element to close it.
type Encoder struct {
	w streamio.Writer

	startOffset int // Byte offset in the file where data starts after the header.

	sections       []*filemd.SectionInfo
	curSectionType SectionType

	typesReady    bool
	dictionary    []string
	rawTypes      []*filemd.SectionType
	typeRefLookup map[SectionType]uint32

	data *bytes.Buffer
}

// NewEncoder creates a new Encoder which writes a data object to the provided
// writer.
func NewEncoder(w streamio.Writer) *Encoder {
	buf := bufpool.GetUnsized()

	return &Encoder{
		w: w,

		startOffset: len(magic),

		data: buf,
	}
}

// AppendSection appends a section to the data object. AppendSection panics if
// typ is not SectionTypeLogs or SectionTypeStreams.
func (enc *Encoder) AppendSection(typ SectionType, data, metadata []byte) {
	info := &filemd.SectionInfo{
		TypeRef: enc.getTypeRef(typ),
		Layout: &filemd.SectionLayout{
			Data: &filemd.Region{
				Offset: uint64(enc.startOffset + enc.data.Len()),
				Length: uint64(len(data)),
			},

			Metadata: &filemd.Region{
				Offset: uint64(enc.startOffset + enc.data.Len() + len(data)),
				Length: uint64(len(metadata)),
			},
		},
	}

	// bytes.Buffer.Write never fails.
	enc.data.Grow(len(data) + len(metadata))
	_, _ = enc.data.Write(data)
	_, _ = enc.data.Write(metadata)

	enc.sections = append(enc.sections, info)
}

// getTypeRef returns the type reference for the given type.
//
// getTypeRef panics if typ is not SectionTypeLogs or SectionTypeStreams.
func (enc *Encoder) getTypeRef(typ SectionType) uint32 {
	// TODO(rfratto): support arbitrary SectionType values.
	if !enc.typesReady {
		enc.initTypeRefs()
	}

	ref, ok := enc.typeRefLookup[typ]
	if !ok {
		panic(fmt.Sprintf("unknown type reference for %s", typ))
	}
	return ref
}

func (enc *Encoder) initTypeRefs() {
	// Reserve the zero index in the dictionary for an invalid entry. This is
	// only required for the type refs, but it's still easier to debug.
	enc.dictionary = []string{"", "github.com/grafana/loki", "streams", "logs"}

	enc.rawTypes = []*filemd.SectionType{
		{NameRef: nil}, // Invalid type.
		{NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 2}}, // Streams.
		{NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 3}}, // Logs.
	}

	enc.typeRefLookup = map[SectionType]uint32{
		SectionTypeStreams: 1,
		SectionTypeLogs:    2,
	}

	enc.typesReady = true
}

// OpenLogs opens a [LogsEncoder]. OpenLogs fails if there is another open
// section.
func (enc *Encoder) OpenLogs() (*LogsEncoder, error) {
	if enc.curSectionType.Valid() {
		return nil, ErrElementExist
	}

	enc.curSectionType = SectionTypeLogs
	return newLogsEncoder(enc), nil
}

// MetadataSize returns an estimate of the current size of the metadata for the
// data object. MetadataSize does not include the size of data appended or the
// currently open stream.
func (enc *Encoder) MetadataSize() int { return elementMetadataSize(enc) }

func (enc *Encoder) metadata() proto.Message {
	sections := enc.sections[:len(enc.sections):cap(enc.sections)]
	return &filemd.Metadata{
		Sections: sections,

		Dictionary: enc.dictionary,
		Types:      enc.rawTypes,
	}
}

// Flush flushes any buffered data to the underlying writer. After flushing,
// enc is reset. Flush fails if there is currently an open section.
func (enc *Encoder) Flush() error {
	if enc.curSectionType.Valid() {
		return ErrElementExist
	}

	metadataBuffer := bufpool.GetUnsized()
	defer bufpool.PutUnsized(metadataBuffer)

	// The file metadata should start with the version.
	if err := streamio.WriteUvarint(metadataBuffer, fileFormatVersion); err != nil {
		return err
	} else if err := elementMetadataWrite(enc, metadataBuffer); err != nil {
		return err
	}

	// The overall structure is:
	//
	// header:
	//  [magic]
	// body:
	//  [data]
	//  [metadata]
	// tailer:
	//  [file metadata size (32 bits)]
	//  [magic]
	//
	// The file metadata size *must not* be varint since we need the last 8 bytes
	// of the file to consistently retrieve the tailer.

	if _, err := enc.w.Write(magic); err != nil {
		return fmt.Errorf("writing magic header: %w", err)
	} else if _, err := enc.w.Write(enc.data.Bytes()); err != nil {
		return fmt.Errorf("writing data: %w", err)
	} else if _, err := enc.w.Write(metadataBuffer.Bytes()); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	} else if err := binary.Write(enc.w, binary.LittleEndian, uint32(metadataBuffer.Len())); err != nil {
		return fmt.Errorf("writing metadata size: %w", err)
	} else if _, err := enc.w.Write(magic); err != nil {
		return fmt.Errorf("writing magic tailer: %w", err)
	}

	enc.sections = nil
	enc.data.Reset()
	return nil
}

func (enc *Encoder) Reset(w streamio.Writer) {
	enc.w = w

	enc.startOffset = len(magic)

	enc.sections = nil
	enc.curSectionType = SectionTypeInvalid

	enc.typesReady = false
	enc.dictionary = nil
	enc.rawTypes = nil
	enc.typeRefLookup = nil

	enc.data.Reset()
}

func (enc *Encoder) append(data, metadata []byte) error {
	if !enc.curSectionType.Valid() {
		return errElementNoExist
	}

	if len(data) == 0 && len(metadata) == 0 {
		// Section was discarded.
		enc.curSectionType = SectionTypeInvalid
		return nil
	}

	enc.AppendSection(enc.curSectionType, data, metadata)
	enc.curSectionType = SectionTypeInvalid
	return nil
}
