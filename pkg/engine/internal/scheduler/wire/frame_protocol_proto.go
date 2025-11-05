package wire

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
)

// ProtobufProtocol implements a protobuf-based protocol for frames.
// Messages are length-prefixed: [4-byte length][protobuf payload]
type ProtobufProtocol struct {
	mapper            *protoMapper
	maxFrameSizeBytes uint32
}

var _ FrameProtocol = (*ProtobufProtocol)(nil)

const DefaultMaxFrameSizeBytes = 100 * 1024 * 1024 // 100MB
var DefaultProtobufProtocol = NewProtobufProtocol(memory.NewGoAllocator(), DefaultMaxFrameSizeBytes)

func NewProtobufProtocol(allocator memory.Allocator, maxFrameSizeBytes uint32) *ProtobufProtocol {
	return &ProtobufProtocol{
		mapper:            &protoMapper{allocator},
		maxFrameSizeBytes: maxFrameSizeBytes,
	}
}

// WriteFrame encodes a frame as protobuf and writes it to the writer.
// Format: [4-byte length (big-endian)][protobuf payload]
func (p *ProtobufProtocol) WriteFrame(w io.Writer, frame Frame) error {
	// Convert wire.Frame to protobuf
	pbFrame, err := p.mapper.FrameToPbFrame(frame)
	if err != nil {
		return fmt.Errorf("failed to convert frame to protobuf: %w", err)
	}

	// Marshal to bytes
	data, err := proto.Marshal(pbFrame)
	if err != nil {
		return fmt.Errorf("failed to marshal protobuf: %w", err)
	}

	// Write length prefix (4 bytes, big-endian)
	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("failed to write length prefix: %w", err)
	}

	// Write payload
	n, err := w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write payload: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("incomplete write: wrote %d bytes, expected %d", n, len(data))
	}

	return nil
}

// ReadFrame reads and decodes a frame from the bound reader.
// Format: [4-byte length (big-endian)][protobuf payload]
func (p *ProtobufProtocol) ReadFrame(r io.Reader) (Frame, error) {
	// Read length prefix (4 bytes, big-endian)
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read length prefix: %w", err)
	}

	// Sanity check: prevent excessive allocations
	if length > p.maxFrameSizeBytes {
		return nil, fmt.Errorf("frame size %d exceeds maximum %d", length, p.maxFrameSizeBytes)
	}

	// Read payload
	data := make([]byte, length)
	n, err := io.ReadFull(r, data)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}
	if n != int(length) {
		return nil, fmt.Errorf("incomplete read: read %d bytes, expected %d", n, length)
	}

	// Unmarshal protobuf
	pbFrame := &wirepb.Frame{}
	if err := proto.Unmarshal(data, pbFrame); err != nil {
		return nil, fmt.Errorf("failed to unmarshal protobuf: %w", err)
	}

	// Convert protobuf to wire.Frame
	frame, err := p.mapper.FrameFromPbFrame(pbFrame)
	if err != nil {
		return nil, fmt.Errorf("failed to convert protobuf to frame: %w", err)
	}

	return frame, nil
}
