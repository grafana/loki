package chunkcompat

import (
	"bytes"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/chunk"
	prom_chunk "github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
)

// StreamsToMatrix converts a slice of QueryStreamResponse to a model.Matrix.
func StreamsToMatrix(from, through model.Time, responses []*client.QueryStreamResponse) (model.Matrix, error) {
	result := model.Matrix{}
	for _, response := range responses {
		series, err := SeriesChunksToMatrix(from, through, response.Chunkseries)
		if err != nil {
			return nil, err
		}

		result = append(result, series...)
	}
	return result, nil
}

// SeriesChunksToMatrix converts slice of []client.TimeSeriesChunk to a model.Matrix.
func SeriesChunksToMatrix(from, through model.Time, serieses []client.TimeSeriesChunk) (model.Matrix, error) {
	if serieses == nil {
		return nil, nil
	}

	result := model.Matrix{}
	for _, series := range serieses {
		metric := cortexpb.FromLabelAdaptersToMetric(series.Labels)
		chunks, err := FromChunks("", cortexpb.FromLabelAdaptersToLabels(series.Labels), series.Chunks)
		if err != nil {
			return nil, err
		}

		samples := []model.SamplePair{}
		for _, chunk := range chunks {
			ss, err := chunk.Samples(from, through)
			if err != nil {
				return nil, err
			}
			samples = util.MergeSampleSets(samples, ss)
		}

		result = append(result, &model.SampleStream{
			Metric: metric,
			Values: samples,
		})
	}
	return result, nil
}

// FromChunks converts []client.Chunk to []chunk.Chunk.
func FromChunks(userID string, metric labels.Labels, in []client.Chunk) ([]chunk.Chunk, error) {
	out := make([]chunk.Chunk, 0, len(in))
	for _, i := range in {
		o, err := prom_chunk.NewForEncoding(prom_chunk.Encoding(byte(i.Encoding)))
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

		buf := bytes.NewBuffer(make([]byte, 0, prom_chunk.ChunkLen))
		if err := i.Data.Marshal(buf); err != nil {
			return nil, err
		}

		wireChunk.Data = buf.Bytes()
		out = append(out, wireChunk)
	}
	return out, nil
}
