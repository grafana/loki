package dataobj

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

var (
	magic = []byte("THOR")
)

const (
	fileFormatVersion = 0x1
)

// (Temporarily) Hard-coded section types.
var (
	sectionTypeInvalid = SectionType{}
	sectionTypeStreams = SectionType{"github.com/grafana/loki", "streams"}
	sectionTypeLogs    = SectionType{"github.com/grafana/loki", "logs"}
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

// encoder encodes a data object. Data objects are hierarchical, split into
// distinct sections that contain their own hierarchy.
//
// To support hierarchical encoding, a set of Open* methods are provided to
// open a child element. Only one child element may be open at a given time;
// call Commit or Discard on a child element to close it.
type encoder struct {
	startOffset int // Byte offset in the file where data starts after the header.

	sections []*filemd.SectionInfo

	typesReady    bool
	dictionary    []string
	rawTypes      []*filemd.SectionType
	typeRefLookup map[SectionType]uint32

	data *bytes.Buffer
}

// newEncoder creates a new Encoder which writes a data object to the provided
// writer.
func newEncoder() *encoder {
	return &encoder{startOffset: len(magic)}
}

// AppendSection appends a section to the data object. AppendSection panics if
// typ is not SectionTypeLogs or SectionTypeStreams.
func (enc *encoder) AppendSection(typ SectionType, data, metadata []byte) {
	if enc.data == nil {
		// Lazily initialize enc.data. This allows an Encoder to persist for the
		// lifetime of a dataobj.Builder without holding onto memory when no data
		// is present.
		enc.data = bufpool.GetUnsized()
	}

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
func (enc *encoder) getTypeRef(typ SectionType) uint32 {
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

func (enc *encoder) initTypeRefs() {
	// Reserve the zero index in the dictionary for an invalid entry. This is
	// only required for the type refs, but it's still easier to debug.
	enc.dictionary = []string{"", "github.com/grafana/loki", "streams", "logs"}

	enc.rawTypes = []*filemd.SectionType{
		{NameRef: nil}, // Invalid type.
		{NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 2}}, // Streams.
		{NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 3}}, // Logs.
	}

	enc.typeRefLookup = map[SectionType]uint32{
		sectionTypeStreams: 1,
		sectionTypeLogs:    2,
	}

	enc.typesReady = true
}

// MetadataSize returns an estimate of the current size of the metadata for the
// data object. MetadataSize does not include the size of data appended or the
// currently open stream.
func (enc *encoder) MetadataSize() int { return proto.Size(enc.Metadata()) }

func (enc *encoder) Metadata() proto.Message {
	sections := enc.sections[:len(enc.sections):cap(enc.sections)]
	return &filemd.Metadata{
		Sections: sections,

		Dictionary: enc.dictionary,
		Types:      enc.rawTypes,
	}
}

// Flush flushes any buffered data to the underlying writer. After flushing,
// enc is reset.
func (enc *encoder) Flush(w streamio.Writer) (int64, error) {
	cw := countingWriter{w: w}

	if enc.data == nil {
		return cw.count, fmt.Errorf("empty Encoder")
	}

	metadataBuffer := bufpool.GetUnsized()
	defer bufpool.PutUnsized(metadataBuffer)

	// The file metadata should start with the version.
	if err := streamio.WriteUvarint(metadataBuffer, fileFormatVersion); err != nil {
		return cw.count, err
	} else if err := protocodec.Encode(metadataBuffer, enc.Metadata()); err != nil {
		return cw.count, err
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

	if _, err := cw.Write(magic); err != nil {
		return cw.count, fmt.Errorf("writing magic header: %w", err)
	} else if _, err := cw.Write(enc.data.Bytes()); err != nil {
		return cw.count, fmt.Errorf("writing data: %w", err)
	} else if _, err := cw.Write(metadataBuffer.Bytes()); err != nil {
		return cw.count, fmt.Errorf("writing metadata: %w", err)
	} else if err := binary.Write(&cw, binary.LittleEndian, uint32(metadataBuffer.Len())); err != nil {
		return cw.count, fmt.Errorf("writing metadata size: %w", err)
	} else if _, err := cw.Write(magic); err != nil {
		return cw.count, fmt.Errorf("writing magic tailer: %w", err)
	}

	enc.Reset()
	return cw.count, nil
}

// Reset resets the Encoder to a fresh state.
func (enc *encoder) Reset() {
	enc.startOffset = len(magic)

	enc.sections = nil

	enc.typesReady = false
	enc.dictionary = nil
	enc.rawTypes = nil
	enc.typeRefLookup = nil

	bufpool.PutUnsized(enc.data)
	enc.data = nil
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
