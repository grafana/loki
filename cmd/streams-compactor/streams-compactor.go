package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"sort"
	"time"

	"github.com/grafana/dskit/concurrency"
	"github.com/pkg/errors"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

var logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

const (
	resultsFileLocation = "path to json file with results from streams-inspector"
	desiredChunkSizeKB  = 6 * 1024
)

func main() {
	stats := IndexStats{}
	level.Info(logger).Log("msg", "reading file content")
	content, err := os.ReadFile(resultsFileLocation)
	if err != nil {
		level.Error(logger).Log("msg", "error while reading the file", "err", err)
		return
	}
	level.Info(logger).Log("msg", "unmarshalling the json")
	err = json.Unmarshal(content, &stats)
	if err != nil {
		level.Error(logger).Log("msg", "error while unmarshalling", "err", err)
		return
	}

	err = assertCorrect(stats)
	if err != nil {
		level.Error(logger).Log("msg", "results are incorrect. can not continue", "err", err)
		return
	}

	level.Info(logger).Log("msg", "results are correct")
	computeMetrics(&stats)
	printStats(stats, "before compaction")
	level.Info(logger).Log("msg", "start compaction")
	compacted, err := compact(stats, desiredChunkSizeKB)
	if err != nil {
		level.Error(logger).Log("msg", "error while compacting", "err", err)
		return
	}
	level.Info(logger).Log("msg", "complete compaction")
	computeMetrics(&compacted)
	printStats(compacted, "after compaction")
}

func assertCorrect(stats IndexStats) error {
	if len(stats.Streams) != int(stats.StreamsCount) {
		level.Error(logger).Log("msg", "streams count mismatch", "streams_count", stats.StreamsCount, "actual_streams_count", len(stats.Streams))
		return errors.New("streams count mismatch")
	}
	actualSizeKB := uint32(0)
	actualChunksCount := uint32(0)
	for _, stream := range stats.Streams {
		actualChunksCount += uint32(len(stream.Chunks))
		for _, chunk := range stream.Chunks {
			actualSizeKB += chunk.SizeKB
		}
	}
	if actualSizeKB != stats.SizeKB {
		level.Error(logger).Log("msg", "sizeKB mismatch", "size_kb", stats.SizeKB, "actual_size_kb", actualSizeKB)
		return errors.New("sizeKB mismatch")
	}

	if actualChunksCount != stats.ChunksCount {
		level.Error(logger).Log("msg", "chunks count mismatch", "chunks_count", stats.ChunksCount, "actual_chunks_count", actualChunksCount)
		return errors.New("chunks count mismatch")
	}
	return nil
}

//func truncate(stats IndexStats, content []byte, err error) {
//	updated := stats.Streams[0:20]
//	sizeKb := uint32(0)
//	chunksCount := uint32(0)
//	for _, streamStats := range updated {
//		chunksCount += streamStats.ChunksCount
//		for _, chunk := range streamStats.Chunks {
//			sizeKb += chunk.SizeKB
//		}
//	}
//	truncated := IndexStats{
//		Streams:      updated,
//		StreamsCount: 20,
//		SizeKB:       sizeKb,
//		ChunksCount:  chunksCount,
//	}
//	content, err = json.MarshalIndent(truncated, "  ", "  ")
//	if err != nil {
//		panic(err)
//		return
//	}
//	err = os.WriteFile("path to truncated file", content, 0644)
//	if err != nil {
//		panic(err)
//		return
//	}
//}

func printStats(stats IndexStats, msg string) {
	level.Info(logger).Log(
		"msg", msg,
		"streams_count", stats.StreamsCount,
		"chunks_count", stats.ChunksCount,
		"size_kb", stats.SizeKB,
		"chunk_avg_size_kb", stats.ChunkAvgSizeKB,
		"chunk_med_size_kb", stats.ChunkMedSizeKB,
		"min_chunk_size_kb", stats.MinChunkSizeKB,
		"max_chunk_size_kb", stats.MaxChunkSizeKB,
	)
}

func compact(stats IndexStats, desiredChunkSizeKB uint32) (IndexStats, error) {
	compactedStreamStats := make([]StreamStats, len(stats.Streams))
	err := concurrency.ForEachJob(context.Background(), len(stats.Streams), 100, func(ctx context.Context, idx int) error {
		stream := stats.Streams[idx]
		compactedStream := StreamStats{Stream: stream.Stream, Chunks: make([]ChunkStats, 0, len(stream.Chunks))}
		lastNotCompactedChunkIdx := -1
		currentCompactedChunkSizeKb := uint32(0)
		for i := 0; i < len(stream.Chunks); i++ {
			currentChunk := stream.Chunks[i]
			// if we reach desired size
			if currentChunk.SizeKB+currentCompactedChunkSizeKb > desiredChunkSizeKB ||
				// or if it's the last chunk
				i == len(stream.Chunks)-1 ||
				// or if the size of the next chunk is greater than desired size
				stream.Chunks[i+1].SizeKB > desiredChunkSizeKB {
				compactedChunkStart := currentChunk.Start
				compactedChunkEnd := currentChunk.End
				if lastNotCompactedChunkIdx > -1 {
					if stream.Chunks[lastNotCompactedChunkIdx].Start.Before(currentChunk.Start) {
						compactedChunkStart = stream.Chunks[lastNotCompactedChunkIdx].Start
					}
					if stream.Chunks[lastNotCompactedChunkIdx].End.After(currentChunk.End) {
						compactedChunkEnd = stream.Chunks[lastNotCompactedChunkIdx].End
					}
				}
				compactedStream.Chunks = append(compactedStream.Chunks, ChunkStats{
					Start:  compactedChunkStart,
					End:    compactedChunkEnd,
					SizeKB: currentChunk.SizeKB + currentCompactedChunkSizeKb,
				})
				compactedStream.ChunksCount++
				compactedStream.SizeKB += currentChunk.SizeKB + currentCompactedChunkSizeKb
				//reset
				lastNotCompactedChunkIdx = -1
				currentCompactedChunkSizeKb = 0
			} else {
				currentCompactedChunkSizeKb += currentChunk.SizeKB
				lastNotCompactedChunkIdx = i
			}
		}
		compactedStreamStats[idx] = compactedStream
		return nil
	})
	if err != nil {
		return IndexStats{}, errors.Wrap(err, "error while compacting the streams")
	}
	compacted := IndexStats{Streams: compactedStreamStats, StreamsCount: uint32(len(compactedStreamStats))}
	for _, sst := range compactedStreamStats {
		compacted.ChunksCount += sst.ChunksCount
		compacted.SizeKB += sst.SizeKB
	}
	return compacted, nil
}

func computeMetrics(stats *IndexStats) {
	chunksCountZeroKB := 0
	chunksCount := int(stats.ChunksCount)
	stats.ChunkAvgSizeKB = stats.SizeKB / stats.ChunksCount
	sizesKB := make([]int, 0, chunksCount)
	min := uint32(math.MaxUint32)
	max := uint32(0)
	for _, stream := range stats.Streams {
		for _, chunk := range stream.Chunks {
			if chunk.SizeKB < min {
				min = chunk.SizeKB
			}
			if chunk.SizeKB > max {
				max = chunk.SizeKB
			}
			if chunk.SizeKB == 0 {
				chunksCountZeroKB++
			}
			sizesKB = append(sizesKB, int(chunk.SizeKB))
		}
	}
	level.Info(logger).Log("msg", "chunks with ZERO size", "count", chunksCountZeroKB)
	sort.Ints(sizesKB)
	stats.ChunkMedSizeKB = uint32(sizesKB[len(sizesKB)/2])
	stats.MinChunkSizeKB = min
	stats.MaxChunkSizeKB = max
}

type ChunkStats struct {
	Start  time.Time
	End    time.Time
	SizeKB uint32
}

type StreamStats struct {
	Stream      string
	Chunks      []ChunkStats
	ChunksCount uint32
	SizeKB      uint32
}

type IndexStats struct {
	Streams      []StreamStats
	StreamsCount uint32
	ChunksCount  uint32
	SizeKB       uint32
	//computed
	ChunkAvgSizeKB uint32
	ChunkMedSizeKB uint32
	MinChunkSizeKB uint32
	MaxChunkSizeKB uint32
}
