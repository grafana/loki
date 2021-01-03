package chunkenc

import (
	"time"

	"github.com/grafana/loki/pkg/chunkenc/testdata"
	"github.com/grafana/loki/pkg/logproto"
)

func logprotoEntry(ts int64, line string) *logproto.Entry {
	return &logproto.Entry{
		Timestamp: time.Unix(0, ts),
		Line:      line,
	}
}

func generateData(enc Encoding, chunksCount int) ([]Chunk, uint64) {
	chunks := []Chunk{}
	i := int64(0)
	size := uint64(0)

	for n := 0; n < chunksCount; n++ {
		entry := logprotoEntry(0, testdata.LogString(0))
		c := NewMemChunk(enc, testBlockSize, testTargetSize)
		for c.SpaceFor(entry) {
			size += uint64(len(entry.Line))
			_ = c.Append(entry)
			i++
			entry = logprotoEntry(i, testdata.LogString(i))
		}
		c.Close()
		chunks = append(chunks, c)
	}
	return chunks, size
}

func fillChunk(c Chunk) int64 {
	i := int64(0)
	inserted := int64(0)
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      testdata.LogString(i),
	}
	for c.SpaceFor(entry) {
		err := c.Append(entry)
		if err != nil {
			panic(err)
		}
		i++
		inserted += int64(len(entry.Line))
		entry.Timestamp = time.Unix(0, i)
		entry.Line = testdata.LogString(i)

	}
	_ = c.Close()
	return inserted
}
