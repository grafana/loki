package chunkenc

import (
	"math/rand"
	"time"

	"github.com/grafana/loki/v3/pkg/chunkenc/testdata"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func logprotoEntry(ts int64, line string) *logproto.Entry {
	return &logproto.Entry{
		Timestamp: time.Unix(0, ts),
		Line:      line,
	}
}

func logprotoEntryWithStructuredMetadata(ts int64, line string, structuredMetadata []logproto.LabelAdapter) *logproto.Entry {
	return &logproto.Entry{
		Timestamp:          time.Unix(0, ts),
		Line:               line,
		StructuredMetadata: structuredMetadata,
	}
}

func generateData(enc Encoding, chunksCount, blockSize, targetSize int) ([]Chunk, uint64) {
	chunks := []Chunk{}
	i := int64(0)
	size := uint64(0)

	for n := 0; n < chunksCount; n++ {
		entry := logprotoEntry(0, testdata.LogString(0))
		c := NewMemChunk(ChunkFormatV4, enc, UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, targetSize)
		for c.SpaceFor(entry) {
			size += uint64(len(entry.Line))
			_, _ = c.Append(entry)
			i++
			entry = logprotoEntry(i, testdata.LogString(i))
		}
		c.Close()
		chunks = append(chunks, c)
	}
	return chunks, size
}

func fillChunk(c Chunk) int64 {
	return fillChunkClose(c, true)
}

func fillChunkClose(c Chunk, close bool) int64 {
	i := int64(0)
	inserted := int64(0)
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      testdata.LogString(i),
		StructuredMetadata: []logproto.LabelAdapter{
			{Name: "foo", Value: "bar"},
			{Name: "baz", Value: "buzz"},
			{Name: "qux", Value: "quux"},
			{Name: "corge", Value: "grault"},
			{Name: "garply", Value: "waldo"},
		},
	}
	for c.SpaceFor(entry) {
		_, err := c.Append(entry)
		if err != nil {
			panic(err)
		}
		i++
		inserted += int64(len(entry.Line))
		entry.Timestamp = time.Unix(0, i)
		entry.Line = testdata.LogString(i)

	}
	if close {
		_ = c.Close()
	}
	return inserted
}

func fillChunkRandomOrder(c Chunk, close bool) {
	ub := int64(1 << 30)
	i := int64(0)
	random := rand.New(rand.NewSource(42))
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      testdata.LogString(i),
	}

	for c.SpaceFor(entry) {
		_, err := c.Append(entry)
		if err != nil {
			panic(err)
		}
		i++
		entry.Timestamp = time.Unix(0, random.Int63n(ub))
		entry.Line = testdata.LogString(i)

	}
	if close {
		_ = c.Close()
	}
}
