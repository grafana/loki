package stats

import (
	"context"
	"sync"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	jsoniter "github.com/json-iterator/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	ingesterDataKey = "ingester_data"
	chunkDataKey    = "chunk_data"
)

type trailerCollector struct {
	trailers []*metadata.MD
	sync.Mutex
}

func (c *trailerCollector) addTrailer() *metadata.MD {
	c.Lock()
	defer c.Unlock()
	meta := metadata.MD{}
	c.trailers = append(c.trailers, &meta)
	return &meta
}

func injectTrailerCollector(ctx context.Context) context.Context {
	return context.WithValue(ctx, trailersKey, &trailerCollector{})
}

// CollectTrailer register a new trailer that can be collected by the engine.
func CollectTrailer(ctx context.Context) grpc.CallOption {
	d, ok := ctx.Value(trailersKey).(*trailerCollector)
	if !ok {
		return grpc.EmptyCallOption{}

	}
	return grpc.Trailer(d.addTrailer())
}

func SendAsTrailer(ctx context.Context, stream grpc.ServerStream) {
	trailer, err := encodeTrailer(ctx)
	if err != nil {
		level.Warn(util.Logger).Log("msg", "failed to encode trailer", "err", err)
		return
	}
	stream.SetTrailer(trailer)
}

func encodeTrailer(ctx context.Context) (metadata.MD, error) {
	meta := metadata.MD{}
	ingData, ok := ctx.Value(ingesterKey).(*IngesterData)
	if ok {
		data, err := jsoniter.MarshalToString(ingData)
		if err != nil {
			return meta, err
		}
		meta.Set(ingesterDataKey, data)
	}
	chunkData, ok := ctx.Value(chunksKey).(*ChunkData)
	if ok {
		data, err := jsoniter.MarshalToString(chunkData)
		if err != nil {
			return meta, err
		}
		meta.Set(chunkDataKey, data)
	}
	return meta, nil
}

func decodeTrailers(ctx context.Context) Ingester {
	var res Ingester
	collector, ok := ctx.Value(trailersKey).(*trailerCollector)
	if !ok {
		return res
	}
	res.TotalReached = len(collector.trailers)
	for _, meta := range collector.trailers {
		ing := decodeTrailer(meta)
		res.TotalChunksMatched += ing.TotalChunksMatched
		res.TotalBatches += ing.TotalBatches
		res.TotalLinesSent += ing.TotalLinesSent
		res.BytesUncompressed += ing.BytesUncompressed
		res.LinesUncompressed += ing.LinesUncompressed
		res.BytesDecompressed += ing.BytesDecompressed
		res.LinesDecompressed += ing.LinesDecompressed
		res.BytesCompressed += ing.BytesCompressed
		res.TotalDuplicates += ing.TotalDuplicates
	}
	return res
}

func decodeTrailer(meta *metadata.MD) Ingester {
	var res Ingester
	values := meta.Get(ingesterDataKey)
	if len(values) == 1 {
		if err := jsoniter.UnmarshalFromString(values[0], &res.IngesterData); err != nil {
			level.Warn(util.Logger).Log("msg", "could not unmarshal ingester data", "err", err)
		}
	}
	values = meta.Get(chunkDataKey)
	if len(values) == 1 {
		if err := jsoniter.UnmarshalFromString(values[0], &res.ChunkData); err != nil {
			level.Warn(util.Logger).Log("msg", "could not unmarshal chunk data", "err", err)
		}
	}
	return res
}
