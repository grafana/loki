package encoding

import (
	"io"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

type element interface {
	metadata() proto.Message
}

func elementMetadataSize(e element) int {
	return proto.Size(e.metadata())
}

func elementMetadataWrite(e element, w streamio.Writer) error {
	buf := protoBufferPool.Get().(*proto.Buffer)
	buf.Reset()
	defer protoBufferPool.Put(buf)

	if err := buf.Marshal(e.metadata()); err != nil {
		return err
	}

	// Every protobuf message is always prepended with its size as a uvarint.
	messageSize := len(buf.Bytes())
	if err := streamio.WriteUvarint(w, uint64(messageSize)); err != nil {
		return err
	}

	sz, err := w.Write(buf.Bytes())
	if err != nil {
		return err
	} else if sz != messageSize {
		return io.ErrShortWrite
	}

	return nil
}
