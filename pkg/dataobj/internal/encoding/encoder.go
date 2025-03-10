package encoding

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

// TODO(rfratto): the memory footprint of [Encoder] can very slowly grow in
// memory as [bytesBufferPool] is filled with buffers with increasing capacity:
// each encoding pass has a different number of elements, shuffling which
// elements of the hierarchy get which pooled buffers.
//
// This means that elements that require more bytes will grow the capacity of
// the buffer and put the buffer back into the pool. Even if further encoding
// passes don't need that many bytes, the buffer is kept alive with its larger
// footprint. Given enough time, all buffers in the pool will have a large
// capacity.
//
// The bufpool package provides a solution to this (bucketing pools by
// capacity), but using bufpool properly requires knowing how many bytes are
// needed.
//
// Encoder can eventually be moved to the bufpool package by calculating a
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

	sections   []*filemd.SectionInfo
	curSection *filemd.SectionInfo

	data *bytes.Buffer
}

// NewEncoder creates a new Encoder which writes a data object to the provided
// writer.
func NewEncoder(w streamio.Writer) *Encoder {
	buf := bytesBufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	return &Encoder{
		w: w,

		startOffset: len(magic),

		data: buf,
	}
}

// OpenStreams opens a [StreamsEncoder]. OpenStreams fails if there is another
// open section.
func (enc *Encoder) OpenStreams() (*StreamsEncoder, error) {
	if enc.curSection != nil {
		return nil, ErrElementExist
	}

	// MetadtaOffset and MetadataSize aren't available until the section is
	// closed. We temporarily set these fields to the maximum values so they're
	// accounted for in the MetadataSize estimate.
	enc.curSection = &filemd.SectionInfo{
		Type:           filemd.SECTION_TYPE_STREAMS,
		MetadataOffset: math.MaxUint32,
		MetadataSize:   math.MaxUint32,
	}

	return newStreamsEncoder(
		enc,
		enc.startOffset+enc.data.Len(),
	), nil
}

// OpenLogs opens a [LogsEncoder]. OpenLogs fails if there is another open
// section.
func (enc *Encoder) OpenLogs() (*LogsEncoder, error) {
	if enc.curSection != nil {
		return nil, ErrElementExist
	}

	enc.curSection = &filemd.SectionInfo{
		Type:           filemd.SECTION_TYPE_LOGS,
		MetadataOffset: math.MaxUint32,
		MetadataSize:   math.MaxUint32,
	}

	return newLogsEncoder(
		enc,
		enc.startOffset+enc.data.Len(),
	), nil
}

// MetadataSize returns an estimate of the current size of the metadata for the
// data object. MetadataSize does not include the size of data appended. The
// estimate includes the currently open element.
func (enc *Encoder) MetadataSize() int { return elementMetadataSize(enc) }

func (enc *Encoder) metadata() proto.Message {
	sections := enc.sections[:len(enc.sections):cap(enc.sections)]
	if enc.curSection != nil {
		sections = append(sections, enc.curSection)
	}
	return &filemd.Metadata{Sections: sections}
}

// Flush flushes any buffered data to the underlying writer. After flushing,
// enc is reset. Flush fails if there is currently an open section.
func (enc *Encoder) Flush() error {
	if enc.curSection != nil {
		return ErrElementExist
	}

	metadataBuffer := bytesBufferPool.Get().(*bytes.Buffer)
	metadataBuffer.Reset()
	defer bytesBufferPool.Put(metadataBuffer)

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
	enc.data.Reset()
	enc.sections = nil
	enc.curSection = nil
	enc.w = w
	enc.startOffset = len(magic)
}

func (enc *Encoder) append(data, metadata []byte) error {
	if enc.curSection == nil {
		return errElementNoExist
	}

	if len(data) == 0 && len(metadata) == 0 {
		// Section was discarded.
		enc.curSection = nil
		return nil
	}

	enc.curSection.MetadataOffset = uint64(enc.startOffset + enc.data.Len() + len(data))
	enc.curSection.MetadataSize = uint64(len(metadata))

	// bytes.Buffer.Write never fails.
	enc.data.Grow(len(data) + len(metadata))
	_, _ = enc.data.Write(data)
	_, _ = enc.data.Write(metadata)

	enc.sections = append(enc.sections, enc.curSection)
	enc.curSection = nil
	return nil
}
