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

func generateData(enc Encoding) []Chunk {
	chunks := []Chunk{}
	i := int64(0)
	for n := 0; n < 50; n++ {
		entry := logprotoEntry(0, testdata.LogString(0))
		c := NewMemChunk(enc)
		for c.SpaceFor(entry) {
			_ = c.Append(entry)
			i++
			entry = logprotoEntry(i, testdata.LogString(i))
		}
		c.Close()
		chunks = append(chunks, c)
	}
	return chunks
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
	return inserted
}
