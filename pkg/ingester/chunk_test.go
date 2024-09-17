package ingester

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

func testIteratorForward(t *testing.T, iter iter.EntryIterator, from, through int64) {
	i := from
	for iter.Next() {
		entry := iter.At()
		require.Equal(t, time.Unix(i, 0).Unix(), entry.Timestamp.Unix())
		require.Equal(t, fmt.Sprintf("line %d", i), entry.Line)
		i++
	}
	require.Equal(t, through, i)
	require.NoError(t, iter.Err())
}

func testIteratorBackward(t *testing.T, iter iter.EntryIterator, from, through int64) {
	i := through - 1
	for iter.Next() {
		entry := iter.At()
		require.Equal(t, time.Unix(i, 0).Unix(), entry.Timestamp.Unix())
		require.Equal(t, fmt.Sprintf("line %d", i), entry.Line)
		i--
	}
	require.Equal(t, from-1, i)
	require.NoError(t, iter.Err())
}

func TestIterator(t *testing.T) {
	const entries = 100

	for _, chk := range []struct {
		name string
		new  func() chunkenc.Chunk
	}{
		{"dumbChunk", chunkenc.NewDumbChunk},
		{"gzipChunk", func() chunkenc.Chunk {
			return chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncGZIP, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, 256*1024, 0)
		}},
	} {
		t.Run(chk.name, func(t *testing.T) {
			chunk := chk.new()
			for i := int64(0); i < entries; i++ {
				dup, err := chunk.Append(&logproto.Entry{
					Timestamp: time.Unix(i, 0),
					Line:      fmt.Sprintf("line %d", i),
				})
				require.False(t, dup)
				require.NoError(t, err)
			}

			for i := 0; i < entries; i++ {
				from := rand.Intn(entries - 1)
				length := rand.Intn(entries-from) + 1
				iter, err := chunk.Iterator(context.TODO(), time.Unix(int64(from), 0), time.Unix(int64(from+length), 0), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
				require.NoError(t, err)
				testIteratorForward(t, iter, int64(from), int64(from+length))
				_ = iter.Close()
			}

			for i := 0; i < entries; i++ {
				from := rand.Intn(entries - 1)
				length := rand.Intn(entries-from) + 1
				iter, err := chunk.Iterator(context.TODO(), time.Unix(int64(from), 0), time.Unix(int64(from+length), 0), logproto.BACKWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
				require.NoError(t, err)
				testIteratorBackward(t, iter, int64(from), int64(from+length))
				_ = iter.Close()
			}
		})
	}
}
