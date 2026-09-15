package arrowcodec

import (
	"bytes"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// DefaultArrowCodec is the package-level default codec using the default allocator.
var DefaultArrowCodec = &ArrowCodec{Allocator: memory.DefaultAllocator}

// ArrowCodec serializes and deserializes Arrow record batches using IPC format.
type ArrowCodec struct {
	Allocator memory.Allocator
}

// SerializeArrowRecord serializes an Arrow record batch to bytes using IPC format.
func (c *ArrowCodec) SerializeArrowRecord(record arrow.RecordBatch) ([]byte, error) {
	if record == nil {
		return nil, errors.New("nil arrow record")
	}

	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf,
		ipc.WithSchema(record.Schema()),
		ipc.WithAllocator(c.Allocator),
	)
	defer writer.Close()

	if err := writer.Write(record); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// DeserializeArrowRecord deserializes an Arrow record batch from bytes using IPC format.
func (c *ArrowCodec) DeserializeArrowRecord(data []byte) (arrow.RecordBatch, error) {
	if len(data) == 0 {
		return nil, errors.New("empty arrow data")
	}

	reader, err := ipc.NewReader(
		bytes.NewReader(data),
		ipc.WithAllocator(c.Allocator),
	)
	if err != nil {
		return nil, err
	}

	if !reader.Next() {
		if err := reader.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("no record in arrow data")
	}

	rec := reader.RecordBatch()
	return rec, nil
}
