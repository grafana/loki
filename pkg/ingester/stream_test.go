package ingester

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func TestStreamIterator(t *testing.T) {
	const chunks = 3
	const entries = 100

	for _, chk := range []struct {
		name string
		new  func() chunkenc.Chunk
	}{
		{"dumbChunk", chunkenc.NewDumbChunk},
		{"gzipChunk", func() chunkenc.Chunk { return chunkenc.NewMemChunk(chunkenc.EncGZIP) }},
	} {
		t.Run(chk.name, func(t *testing.T) {
			var s stream
			for i := int64(0); i < chunks; i++ {
				chunk := chk.new()
				for j := int64(0); j < entries; j++ {
					k := i*entries + j
					err := chunk.Append(&logproto.Entry{
						Timestamp: time.Unix(k, 0),
						Line:      fmt.Sprintf("line %d", k),
					})
					require.NoError(t, err)
				}
				s.chunks = append(s.chunks, chunkDesc{chunk: chunk})
			}

			for i := 0; i < 100; i++ {
				from := rand.Intn(chunks*entries - 1)
				len := rand.Intn(chunks*entries-from) + 1
				iter, err := s.Iterator(time.Unix(int64(from), 0), time.Unix(int64(from+len), 0), logproto.FORWARD, nil)
				require.NotNil(t, iter)
				require.NoError(t, err)
				testIteratorForward(t, iter, int64(from), int64(from+len))
				_ = iter.Close()
			}

			for i := 0; i < 100; i++ {
				from := rand.Intn(entries - 1)
				len := rand.Intn(chunks*entries-from) + 1
				iter, err := s.Iterator(time.Unix(int64(from), 0), time.Unix(int64(from+len), 0), logproto.BACKWARD, nil)
				require.NotNil(t, iter)
				require.NoError(t, err)
				testIteratorBackward(t, iter, int64(from), int64(from+len))
				_ = iter.Close()
			}
		})
	}

}
