package ingester

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testIteratorForward(t *testing.T, iter iter.EntryIterator, from, through int64) {
	i := from
	for iter.Next() {
		entry := iter.Entry()
		assert.Equal(t, time.Unix(i, 0), entry.Timestamp)
		assert.Equal(t, fmt.Sprintf("line %d", i), entry.Line)
		i++
	}
	assert.Equal(t, through, i)
	assert.NoError(t, iter.Error())
}

func testIteratorBackward(t *testing.T, iter iter.EntryIterator, from, through int64) {
	i := through - 1
	for iter.Next() {
		entry := iter.Entry()
		assert.Equal(t, time.Unix(i, 0), entry.Timestamp)
		assert.Equal(t, fmt.Sprintf("line %d", i), entry.Line)
		i--
	}
	assert.Equal(t, from-1, i)
	assert.NoError(t, iter.Error())
}

func TestIterator(t *testing.T) {
	const entries = 100

	for _, chk := range []struct {
		name string
		new  func() chunkenc.Chunk
	}{
		{"dumbChunk", chunkenc.NewDumbChunk},
		{"gzipChunk", func() chunkenc.Chunk { return chunkenc.NewMemChunk(chunkenc.EncGZIP) }},
	} {
		t.Run(chk.name, func(t *testing.T) {
			chunk := chk.new()
			for i := int64(0); i < entries; i++ {
				err := chunk.Append(&logproto.Entry{
					Timestamp: time.Unix(i, 0),
					Line:      fmt.Sprintf("line %d", i),
				})
				require.NoError(t, err)
			}

			for i := 0; i < entries; i++ {
				from := rand.Intn(entries - 1)
				len := rand.Intn(entries-from) + 1
				iter, err := chunk.Iterator(time.Unix(int64(from), 0), time.Unix(int64(from+len), 0), logproto.FORWARD, nil)
				require.NoError(t, err)
				testIteratorForward(t, iter, int64(from), int64(from+len))
				_ = iter.Close()
			}

			for i := 0; i < entries; i++ {
				from := rand.Intn(entries - 1)
				len := rand.Intn(entries-from) + 1
				iter, err := chunk.Iterator(time.Unix(int64(from), 0), time.Unix(int64(from+len), 0), logproto.BACKWARD, nil)
				require.NoError(t, err)
				testIteratorBackward(t, iter, int64(from), int64(from+len))
				_ = iter.Close()
			}
		})
	}
}
