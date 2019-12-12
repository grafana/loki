package chunkenc

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dustin/go-humanize"
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

var encodingTests = []Encoding{EncGZIPBestSpeed, EncGZIP, EncLZ4, EncSnappy, EncSnappyV2}

func TestChunkSize(t *testing.T) {
	for _, enc := range encodingTests {
		t.Run(enc.String(), func(t *testing.T) {
			i := int64(0)
			c := NewMemChunk(enc)
			entry := &logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      RandString(512),
			}
			for c.SpaceFor(entry) {
				err := c.Append(entry)
				if err != nil {
					t.Fatal(err)
				}
				entry.Timestamp = time.Unix(0, i)
				i++
			}
			b, err := c.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			t.Log("Chunk size", humanize.Bytes(uint64(len(b))))
			t.Log("Lines", i)
			t.Log("characters ", i*int64(len(entry.Line)))

		})

	}
}

var result []Chunk

func BenchmarkWrite(b *testing.B) {
	chunks := []Chunk{}

	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      RandString(512),
	}
	i := int64(0)

	for _, enc := range encodingTests {
		b.Run(enc.String(), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				c := NewMemChunk(enc)
				// adds until full so we trigger cut which serialize using gzip
				for c.SpaceFor(entry) {
					_ = c.Append(entry)
					entry.Timestamp = time.Unix(0, i)
					i++
				}
				chunks = append(chunks, c)
			}
			result = chunks
		})
	}

}

var entries = []logproto.Entry{}

func BenchmarkRead(b *testing.B) {
	for _, enc := range encodingTests {
		b.Run(enc.String(), func(b *testing.B) {
			chunks := generateData(enc)
			b.ResetTimer()
			bytesRead := int64(0)
			//now := time.Now()
			for n := 0; n < b.N; n++ {
				for _, c := range chunks {
					// use forward iterator for benchmark -- backward iterator does extra allocations by keeping entries in memory
					iterator, err := c.Iterator(time.Unix(0, 0), time.Now(), logproto.FORWARD, nil)
					if err != nil {
						panic(err)
					}
					for iterator.Next() {
						e := iterator.Entry()
						bytesRead += int64(len(e.Line))
						// entries = append(entries, e) // adds extra allocation, we don't need this
					}
					if err := iterator.Close(); err != nil {
						b.Fatal(err)
					}
				}
			}
			//b.Log("bytes per second ", humanize.Bytes(uint64(float64(bytesRead)/time.Since(now).Seconds())))
			//b.Log("n=", b.N)
		})
	}
}

func generateData(enc Encoding) []Chunk {
	chunks := []Chunk{}
	i := int64(0)
	for n := 0; n < 50; n++ {
		entry := randSizeEntry(0)
		c := NewMemChunk(enc)
		for c.SpaceFor(entry) {
			_ = c.Append(entry)
			i++
			entry = randSizeEntry(i)
		}
		c.Close()
		chunks = append(chunks, c)
	}
	return chunks
}

func BenchmarkHeadBlockIterator(b *testing.B) {

	for _, j := range []int{100000, 50000, 15000, 10000} {
		b.Run(fmt.Sprintf("Size %d", j), func(b *testing.B) {

			h := headBlock{}

			for i := 0; i < j; i++ {
				if err := h.append(int64(i), "this is the append string"); err != nil {
					b.Fatal(err)
				}
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
	return fmt.Sprintf(
		`level=error ts=2019-12-11T21:22:17.913Z caller=klog.go:%d component=k8s_client_runtime func=ErrorDepth msg="/app/discovery/kubernetes/kubernetes.go:263: Failed to list *v1.Endpoints: endpoints is forbidden: User \"system:serviceaccount:malcolm-default:prometheus-one\" cannot list resource \"endpoints\" in API group \"\" at the cluster scope"`,
		rand.Int(),
	)
	// return RandStringWithCharset(length, charset)
}

func logprotoEntry(ts int64, line string) *logproto.Entry {
	return &logproto.Entry{
		Timestamp: time.Unix(0, ts),
		Line:      line,
	}
}
