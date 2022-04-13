package chunkcompat

import (
	"bytes"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/storage/chunk"
)

// FromChunks converts []client.Chunk to []chunk.Chunk.
func FromChunks(userID string, metric labels.Labels, in []client.Chunk) ([]chunk.Chunk, error) {
	out := make([]chunk.Chunk, 0, len(in))
	for _, i := range in {
		o, err := chunk.NewForEncoding(chunk.Encoding(byte(i.Encoding)))
		if err != nil {
			return nil, err
		}

		if err := o.UnmarshalFromBuf(i.Data); err != nil {
			return nil, err
		}

		firstTime, lastTime := model.Time(i.StartTimestampMs), model.Time(i.EndTimestampMs)
		// As the lifetime of this chunk is scopes to this request, we don't need
		// to supply a fingerprint.
		out = append(out, chunk.NewChunk(userID, 0, metric, o, firstTime, lastTime))
	}
	return out, nil
}

// ToChunks converts []chunk.Chunk to []client.Chunk.
func ToChunks(in []chunk.Chunk) ([]client.Chunk, error) {
	out := make([]client.Chunk, 0, len(in))
	for _, i := range in {
		wireChunk := client.Chunk{
			StartTimestampMs: int64(i.From),
			EndTimestampMs:   int64(i.Through),
			Encoding:         int32(i.Data.Encoding()),
		}

		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		if err := i.Data.Marshal(buf); err != nil {
			return nil, err
		}

		wireChunk.Data = buf.Bytes()
		out = append(out, wireChunk)
	}
	return out, nil
}
