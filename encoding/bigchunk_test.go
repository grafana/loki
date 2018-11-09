package encoding

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestSliceBiggerChunk(t *testing.T) {
	var c Chunk = newBigchunk()
	for i := 0; i < 12*3600/15; i++ {
		cs, err := c.Add(model.SamplePair{
			Timestamp: model.Time(i * step),
			Value:     model.SampleValue(i),
		})
		require.NoError(t, err)
		c = cs[0]
	}

	// Test for when the slice aligns perfectly with the sub-chunk boundaries.

	for i := 0; i < (12*3600/15)-480; i += 120 {
		s := c.Slice(model.Time(i*step), model.Time((i+479)*step))
		iter := s.NewIterator()
		for j := i; j < i+480; j++ {
			require.True(t, iter.Scan())
			sample := iter.Value()
			require.Equal(t, sample.Timestamp, model.Time(j*step))
			require.Equal(t, sample.Value, model.SampleValue(j))
		}
		require.False(t, iter.Scan())
		require.NoError(t, iter.Err())
	}

	// Test for when the slice does not align perfectly with the sub-chunk boundaries.
	for i := 0; i < (12*3600/15)-500; i += 100 {
		s := c.Slice(model.Time(i*step), model.Time((i+500)*step))
		iter := s.NewIterator()

		// Consume some samples until we get to where we want to be.
		for {
			require.True(t, iter.Scan())
			sample := iter.Value()
			if sample.Timestamp == model.Time(i*step) {
				break
			}
		}

		for j := i; j < i+500; j++ {
			sample := iter.Value()
			require.Equal(t, sample.Timestamp, model.Time(j*step))
			require.Equal(t, sample.Value, model.SampleValue(j))
			require.True(t, iter.Scan())
		}
	}
}

func BenchmarkBiggerChunkMemory(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var c Chunk = newBigchunk()
		for i := 0; i < 12*3600/15; i++ {
			cs, err := c.Add(model.SamplePair{
				Timestamp: model.Time(i * step),
				Value:     model.SampleValue(i),
			})
			require.NoError(b, err)
			c = cs[0]
		}

		c.(*bigchunk).printSize()
	}
}

// printSize calculates various sizes of the chunk when encoded, and in memory.
func (b *bigchunk) printSize() {
	var buf bytes.Buffer
	b.Marshal(&buf)

	var size, allocd int
	for _, c := range b.chunks {
		size += len(c.Bytes())
		allocd += cap(c.Bytes())
	}

	fmt.Println("encodedlen =", len(buf.Bytes()), "subchunks =", len(b.chunks), "len =", size, "cap =", allocd)
}
