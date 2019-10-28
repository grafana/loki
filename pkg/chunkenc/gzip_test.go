package chunkenc

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func TestGZIPBlock(t *testing.T) {
	chk := NewMemChunk(EncGZIP)

	cases := []struct {
		ts  int64
		str string
		cut bool
	}{
		{
			ts:  1,
			str: "hello, world!",
		},
		{
			ts:  2,
			str: "hello, world2!",
		},
		{
			ts:  3,
			str: "hello, world3!",
		},
		{
			ts:  4,
			str: "hello, world4!",
		},
		{
			ts:  5,
			str: "hello, world5!",
		},
		{
			ts:  6,
			str: "hello, world6!",
			cut: true,
		},
		{
			ts:  7,
			str: "hello, world7!",
		},
		{
			ts:  8,
			str: "hello, worl\nd8!",
		},
		{
			ts:  8,
			str: "hello, world 8, 2!",
		},
		{
			ts:  8,
			str: "hello, world 8, 3!",
		},
		{
			ts:  9,
			str: "",
		},
	}

	for _, c := range cases {
		require.NoError(t, chk.Append(logprotoEntry(c.ts, c.str)))
		if c.cut {
			require.NoError(t, chk.cut())
		}
	}

	it, err := chk.Iterator(time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, nil)
	require.NoError(t, err)

	idx := 0
	for it.Next() {
		e := it.Entry()
		require.Equal(t, cases[idx].ts, e.Timestamp.UnixNano())
		require.Equal(t, cases[idx].str, e.Line)
		idx++
	}

	require.NoError(t, it.Error())
	require.Equal(t, len(cases), idx)

	t.Run("bounded-iteration", func(t *testing.T) {
		it, err := chk.Iterator(time.Unix(0, 3), time.Unix(0, 7), logproto.FORWARD, nil)
		require.NoError(t, err)

		idx := 2
		for it.Next() {
			e := it.Entry()
			require.Equal(t, cases[idx].ts, e.Timestamp.UnixNano())
			require.Equal(t, cases[idx].str, e.Line)
			idx++
		}
		require.NoError(t, it.Error())
		require.Equal(t, 6, idx)
	})
}

func TestGZIPSerialisation(t *testing.T) {
	chk := NewMemChunk(EncGZIP)

	numSamples := 500000

	for i := 0; i < numSamples; i++ {
		require.NoError(t, chk.Append(logprotoEntry(int64(i), string(i))))
	}

	byt, err := chk.Bytes()
	require.NoError(t, err)

	bc, err := NewByteChunk(byt)
	require.NoError(t, err)

	it, err := bc.Iterator(time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, nil)
	require.NoError(t, err)
	for i := 0; i < numSamples; i++ {
		require.True(t, it.Next())

		e := it.Entry()
		require.Equal(t, int64(i), e.Timestamp.UnixNano())
		require.Equal(t, string(i), e.Line)
	}

	require.NoError(t, it.Error())

	byt2, err := chk.Bytes()
	require.NoError(t, err)

	require.True(t, bytes.Equal(byt, byt2))
}

func TestGZIPChunkFilling(t *testing.T) {
	chk := NewMemChunk(EncGZIP)
	chk.blockSize = 1024

	// We should be able to append only 10KB of logs.
	maxBytes := chk.blockSize * blocksPerChunk
	lineSize := 512
	lines := maxBytes / lineSize

	logLine := string(make([]byte, lineSize))
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      logLine,
	}

	i := int64(0)
	for ; chk.SpaceFor(entry) && i < 30; i++ {
		entry.Timestamp = time.Unix(0, i)
		require.NoError(t, chk.Append(entry))
	}

	require.Equal(t, int64(lines), i)

	it, err := chk.Iterator(time.Unix(0, 0), time.Unix(0, 100), logproto.FORWARD, nil)
	require.NoError(t, err)
	i = 0
	for it.Next() {
		entry := it.Entry()
		require.Equal(t, i, entry.Timestamp.UnixNano())
		i++
	}

	require.Equal(t, int64(lines), i)
}

func TestMemChunk_AppendOutOfOrder(t *testing.T) {
	t.Parallel()

	type tester func(t *testing.T, chk *MemChunk)

	tests := map[string]tester{
		"append out of order in the same block": func(t *testing.T, chk *MemChunk) {
			assert.NoError(t, chk.Append(logprotoEntry(5, "test")))
			assert.NoError(t, chk.Append(logprotoEntry(6, "test")))

			assert.EqualError(t, chk.Append(logprotoEntry(1, "test")), ErrOutOfOrder.Error())
		},
		"append out of order in a new block right after cutting the previous one": func(t *testing.T, chk *MemChunk) {
			assert.NoError(t, chk.Append(logprotoEntry(5, "test")))
			assert.NoError(t, chk.Append(logprotoEntry(6, "test")))
			assert.NoError(t, chk.cut())

			assert.EqualError(t, chk.Append(logprotoEntry(1, "test")), ErrOutOfOrder.Error())
		},
		"append out of order in a new block after multiple cuts": func(t *testing.T, chk *MemChunk) {
			assert.NoError(t, chk.Append(logprotoEntry(5, "test")))
			assert.NoError(t, chk.cut())

			assert.NoError(t, chk.Append(logprotoEntry(6, "test")))
			assert.NoError(t, chk.cut())

			assert.EqualError(t, chk.Append(logprotoEntry(1, "test")), ErrOutOfOrder.Error())
		},
	}

	for testName, tester := range tests {
		tester := tester

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			tester(t, NewMemChunk(EncGZIP))
		})
	}
}

var result []Chunk

func BenchmarkWriteGZIP(b *testing.B) {
	chunks := []Chunk{}

	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      RandString(512),
	}
	i := int64(0)

	for n := 0; n < b.N; n++ {
		c := NewMemChunk(EncGZIP)
		// adds until full so we trigger cut which serialize using gzip
		for c.SpaceFor(entry) {
			_ = c.Append(entry)
			entry.Timestamp = time.Unix(0, i)
			i++
		}
		chunks = append(chunks, c)
	}
	result = chunks
}

func BenchmarkReadGZIP(b *testing.B) {
	chunks := []Chunk{}
	i := int64(0)
	for n := 0; n < 50; n++ {
		entry := randSizeEntry(0)
		c := NewMemChunk(EncGZIP)
		// adds until full so we trigger cut which serialize using gzip
		for c.SpaceFor(entry) {
			_ = c.Append(entry)
			i++
			entry = randSizeEntry(i)
		}
		c.Close()
		chunks = append(chunks, c)
	}
	entries := []logproto.Entry{}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var wg sync.WaitGroup
		for _, c := range chunks {
			wg.Add(1)
			go func(c Chunk) {
				iterator, err := c.Iterator(time.Unix(0, 0), time.Now(), logproto.BACKWARD, nil)
				if err != nil {
					panic(err)
				}
				for iterator.Next() {
					entries = append(entries, iterator.Entry())
				}
				iterator.Close()
				wg.Done()
			}(c)
		}
		wg.Wait()
	}
}

func BenchmarkHeadBlockIterator(b *testing.B) {

	for _, j := range []int{100000, 50000, 15000, 10000} {
		b.Run(fmt.Sprintf("Size %d", j), func(b *testing.B) {

			h := headBlock{}

			for i := 0; i < j; i++ {
				h.append(int64(i), "this is the append string")
			}

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				iter := h.iterator(0, math.MaxInt64, nil)

				for iter.Next() {
					_ = iter.Entry()
				}
			}
		})
	}
}

func randSizeEntry(ts int64) *logproto.Entry {
	var line string
	switch ts % 10 {
	case 0:
		line = RandString(27000)
	case 1:
		line = RandString(10000)
	case 2, 3, 4, 5:
		line = RandString(2048)
	default:
		line = RandString(4096)
	}
	return &logproto.Entry{
		Timestamp: time.Unix(0, ts),
		Line:      line,
	}
}

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RandStringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset)-1)]
	}
	return string(b)
}

func RandString(length int) string {
	return RandStringWithCharset(length, charset)
}

func logprotoEntry(ts int64, line string) *logproto.Entry {
	return &logproto.Entry{
		Timestamp: time.Unix(0, ts),
		Line:      line,
	}
}
