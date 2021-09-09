package stats

import (
	"context"
	"sync"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/dslog"
	jsoniter "github.com/json-iterator/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	ingesterDataKey = "ingester_data"
	chunkDataKey    = "chunk_data"
	storeDataKey    = "store_data"
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
		level.Warn(dslog.WithContext(ctx, util_log.Logger)).Log("msg", "failed to encode trailer", "err", err)
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

	storeData, ok := ctx.Value(storeKey).(*StoreData)
	if ok {
		data, err := jsoniter.MarshalToString(storeData)
		if err != nil {
			return meta, err
		}
		meta.Set(storeDataKey, data)
	}

	return meta, nil
}

func decodeTrailers(ctx context.Context) Result {
	var res Result
	collector, ok := ctx.Value(trailersKey).(*trailerCollector)
	if !ok {
		return res
	}
	collector.Lock()
	defer collector.Unlock()
	res.Ingester.TotalReached = int32(len(collector.trailers))
	for _, meta := range collector.trailers {
		ing := decodeTrailer(ctx, meta)
		res.Ingester.TotalChunksMatched += ing.Ingester.TotalChunksMatched
		res.Ingester.TotalBatches += ing.Ingester.TotalBatches
		res.Ingester.TotalLinesSent += ing.Ingester.TotalLinesSent
		res.Ingester.HeadChunkBytes += ing.Ingester.HeadChunkBytes
		res.Ingester.HeadChunkLines += ing.Ingester.HeadChunkLines
		res.Ingester.DecompressedBytes += ing.Ingester.DecompressedBytes
		res.Ingester.DecompressedLines += ing.Ingester.DecompressedLines
		res.Ingester.CompressedBytes += ing.Ingester.CompressedBytes
		res.Ingester.TotalDuplicates += ing.Ingester.TotalDuplicates
		res.Store.TotalChunksRef += ing.Store.TotalChunksRef
		res.Store.TotalChunksDownloaded += ing.Store.TotalChunksDownloaded
		res.Store.ChunksDownloadTime += ing.Store.ChunksDownloadTime
	}
	return res
}

func decodeTrailer(ctx context.Context, meta *metadata.MD) Result {
	logger := dslog.WithContext(ctx, util_log.Logger)
	var ingData IngesterData
	values := meta.Get(ingesterDataKey)
	if len(values) == 1 {
		if err := jsoniter.UnmarshalFromString(values[0], &ingData); err != nil {
			level.Warn(logger).Log("msg", "could not unmarshal ingester data", "err", err)
		}
	}
	var chunkData ChunkData
	values = meta.Get(chunkDataKey)
	if len(values) == 1 {
		if err := jsoniter.UnmarshalFromString(values[0], &chunkData); err != nil {
			level.Warn(logger).Log("msg", "could not unmarshal chunk data", "err", err)
		}
	}
	var storeData StoreData
	values = meta.Get(storeDataKey)
	if len(values) == 1 {
		if err := jsoniter.UnmarshalFromString(values[0], &storeData); err != nil {
			level.Warn(logger).Log("msg", "could not unmarshal chunk data", "err", err)
		}
	}
	return Result{
		Ingester: Ingester{
			TotalChunksMatched: ingData.TotalChunksMatched,
			TotalBatches:       ingData.TotalBatches,
			TotalLinesSent:     ingData.TotalLinesSent,
			HeadChunkBytes:     chunkData.HeadChunkBytes,
			HeadChunkLines:     chunkData.HeadChunkLines,
			DecompressedBytes:  chunkData.DecompressedBytes,
			DecompressedLines:  chunkData.DecompressedLines,
			CompressedBytes:    chunkData.CompressedBytes,
			TotalDuplicates:    chunkData.TotalDuplicates,
		},
		Store: Store{
			TotalChunksRef:        storeData.TotalChunksRef,
			TotalChunksDownloaded: storeData.TotalChunksDownloaded,
			ChunksDownloadTime:    storeData.ChunksDownloadTime.Seconds(),
		},
	}
}
