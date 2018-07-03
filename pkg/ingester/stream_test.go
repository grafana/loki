package ingester

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/grafana/logish/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func TestStreamIterator(t *testing.T) {
	const chunks = 3
	const entries = 100

	chks := []chunkType{{"dumbChunk", newChunk}, {"compressedChunk", newCompressedChunk}}

	for _, c := range chks {
		t.Run(fmt.Sprintf("%s", c.name), func(t *testing.T) {
			var s stream
			for i := int64(0); i < chunks; i++ {
				chunk := c.chunkMaker()
				for j := int64(0); j < entries; j++ {
					k := i*entries + j
					err := chunk.Push(&logproto.Entry{
						Timestamp: time.Unix(k, 0),
						Line:      fmt.Sprintf("line %d", k),
					})
					require.NoError(t, err)
				}
				s.chunks = append([]Chunk{chunk}, s.chunks...)
			}

			for i := 0; i < 100; i++ {
				from := rand.Intn(chunks*entries - 1)
				len := rand.Intn(chunks*entries-from) + 1
				iter := s.Iterator(time.Unix(int64(from), 0), time.Unix(int64(from+len), 0), logproto.FORWARD)
				require.NotNil(t, iter)
				testIteratorForward(t, iter, int64(from), int64(from+len))
				iter.Close()
			}

			for i := 0; i < 100; i++ {
				from := rand.Intn(entries - 1)
				len := rand.Intn(chunks*entries-from) + 1
				iter := s.Iterator(time.Unix(int64(from), 0), time.Unix(int64(from+len), 0), logproto.BACKWARD)
				require.NotNil(t, iter)
				testIteratorBackward(t, iter, int64(from), int64(from+len))
				iter.Close()
			}
		})
	}
}
