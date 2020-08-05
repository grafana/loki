package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc/testdata"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
)

var testEncoding = []Encoding{
	EncNone,
	EncGZIP,
	EncLZ4_64k,
	EncLZ4_256k,
	EncLZ4_1M,
	EncLZ4_4M,
	EncSnappy,
}

var (
	testBlockSize  = 256 * 1024
	testTargetSize = 1500 * 1024
)

func TestBlocksInclusive(t *testing.T) {
	chk := NewMemChunk(EncNone, testBlockSize, testTargetSize)
	err := chk.Append(logprotoEntry(1, "1"))
	require.Nil(t, err)
	err = chk.cut()
	require.Nil(t, err)

	blocks := chk.Blocks(time.Unix(0, 1), time.Unix(0, 1))
	require.Equal(t, 1, len(blocks))
	require.Equal(t, 1, blocks[0].Entries())
}

func TestBlock(t *testing.T) {
	for _, enc := range testEncoding {
		t.Run(enc.String(), func(t *testing.T) {
			chk := NewMemChunk(enc, testBlockSize, testTargetSize)
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

			it, err := chk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, nil)
			require.NoError(t, err)

			idx := 0
			for it.Next() {
				e := it.Entry()
				require.Equal(t, cases[idx].ts, e.Timestamp.UnixNano())
				require.Equal(t, cases[idx].str, e.Line)
				idx++
			}

			require.NoError(t, it.Error())
			require.NoError(t, it.Close())
			require.Equal(t, len(cases), idx)

			sampleIt := chk.SampleIterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), nil, logql.ExtractCount)
			idx = 0
			for sampleIt.Next() {
				s := sampleIt.Sample()
				require.Equal(t, cases[idx].ts, s.Timestamp)
				require.Equal(t, 1., s.Value)
				require.NotEmpty(t, s.Hash)
				idx++
			}

			require.NoError(t, sampleIt.Error())
			require.NoError(t, sampleIt.Close())
			require.Equal(t, len(cases), idx)

			t.Run("bounded-iteration", func(t *testing.T) {
				it, err := chk.Iterator(context.Background(), time.Unix(0, 3), time.Unix(0, 7), logproto.FORWARD, nil)
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
		})
	}
}

func TestReadFormatV1(t *testing.T) {
	c := NewMemChunk(EncGZIP, testBlockSize, testTargetSize)
	fillChunk(c)
	// overrides default v2 format
	c.format = chunkFormatV1

	b, err := c.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	r, err := NewByteChunk(b, testBlockSize, testTargetSize)
	if err != nil {
		t.Fatal(err)
	}

	it, err := r.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, nil)
	if err != nil {
		t.Fatal(err)
	}

	i := int64(0)
	for it.Next() {
		require.Equal(t, i, it.Entry().Timestamp.UnixNano())
		require.Equal(t, testdata.LogString(i), it.Entry().Line)

		i++
	}
}

// Test all encodings by populating a memchunk, serializing it,
// re-loading with NewByteChunk, serializing it again, and re-loading into via NewByteChunk once more.
// This tests the integrity of transfer between the following:
// 1) memory populated chunks <-> []byte loaded chunks
// 2) []byte loaded chunks <-> []byte loaded chunks
func TestRoundtripV2(t *testing.T) {
	for _, enc := range testEncoding {
		t.Run(enc.String(), func(t *testing.T) {
			c := NewMemChunk(enc, testBlockSize, testTargetSize)
			populated := fillChunk(c)

			assertLines := func(c *MemChunk) {
				require.Equal(t, enc, c.Encoding())
				it, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, nil)
				if err != nil {
					t.Fatal(err)
				}

				i := int64(0)
				var data int64
				for it.Next() {
					require.Equal(t, i, it.Entry().Timestamp.UnixNano())
					require.Equal(t, testdata.LogString(i), it.Entry().Line)

					data += int64(len(it.Entry().Line))
					i++
				}
				require.Equal(t, populated, data)
			}

			assertLines(c)

			// test MemChunk -> NewByteChunk loading
			b, err := c.Bytes()
			if err != nil {
				t.Fatal(err)
			}

			r, err := NewByteChunk(b, testBlockSize, testTargetSize)
			if err != nil {
				t.Fatal(err)
			}
			assertLines(r)

			// test NewByteChunk -> NewByteChunk loading
			rOut, err := r.Bytes()
			require.Nil(t, err)

			loaded, err := NewByteChunk(rOut, testBlockSize, testTargetSize)
			require.Nil(t, err)

			assertLines(loaded)
		})

	}

}

func TestSerialization(t *testing.T) {
	for _, enc := range testEncoding {
		t.Run(enc.String(), func(t *testing.T) {
			chk := NewMemChunk(enc, testBlockSize, testTargetSize)

			numSamples := 50000

			for i := 0; i < numSamples; i++ {
				require.NoError(t, chk.Append(logprotoEntry(int64(i), string(i))))
			}

			byt, err := chk.Bytes()
			require.NoError(t, err)

			bc, err := NewByteChunk(byt, testBlockSize, testTargetSize)
			require.NoError(t, err)

			it, err := bc.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, nil)
			require.NoError(t, err)
			for i := 0; i < numSamples; i++ {
				require.True(t, it.Next())

				e := it.Entry()
				require.Equal(t, int64(i), e.Timestamp.UnixNano())
				require.Equal(t, string(i), e.Line)
			}
			require.NoError(t, it.Error())

			sampleIt := bc.SampleIterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), nil, logql.ExtractCount)
			for i := 0; i < numSamples; i++ {
				require.True(t, sampleIt.Next(), i)

				s := sampleIt.Sample()
				require.Equal(t, int64(i), s.Timestamp)
				require.Equal(t, 1., s.Value)
			}
			require.NoError(t, sampleIt.Error())

			byt2, err := chk.Bytes()
			require.NoError(t, err)

			require.True(t, bytes.Equal(byt, byt2))
		})
	}
}

func TestChunkFilling(t *testing.T) {
	for _, enc := range testEncoding {
		t.Run(enc.String(), func(t *testing.T) {
			chk := NewMemChunk(enc, testBlockSize, 0)
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

			it, err := chk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, 100), logproto.FORWARD, nil)
			require.NoError(t, err)
			i = 0
			for it.Next() {
				entry := it.Entry()
				require.Equal(t, i, entry.Timestamp.UnixNano())
				i++
			}

			require.Equal(t, int64(lines), i)
		})
	}
}

func TestGZIPChunkTargetSize(t *testing.T) {
	chk := NewMemChunk(EncGZIP, testBlockSize, testTargetSize)

	lineSize := 512
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      "",
	}

	// Use a random number to generate random log data, otherwise the gzip compression is way too good
	// and the following loop has to run waaayyyyy to many times
	// Using the same seed should guarantee the same random numbers and same test data.
	r := rand.New(rand.NewSource(99))

	i := int64(0)

	for ; chk.SpaceFor(entry) && i < 5000; i++ {
		logLine := make([]byte, lineSize)
		for j := range logLine {
			logLine[j] = byte(r.Int())
		}
		entry = &logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      string(logLine),
		}
		entry.Timestamp = time.Unix(0, i)
		require.NoError(t, chk.Append(entry))
	}

	// 5000 is a limit ot make sure the test doesn't run away, we shouldn't need this many log lines to make 1MB chunk
	require.NotEqual(t, 5000, i)

	require.NoError(t, chk.Close())

	require.Equal(t, 0, chk.head.size)

	// Even though the seed is static above and results should be deterministic,
	// we will allow +/- 10% variance
	minSize := int(float64(testTargetSize) * 0.9)
	maxSize := int(float64(testTargetSize) * 1.1)
	require.Greater(t, chk.CompressedSize(), minSize)
	require.Less(t, chk.CompressedSize(), maxSize)

	// Also verify our utilization is close to 1.0
	ut := chk.Utilization()
	require.Greater(t, ut, 0.99)
	require.Less(t, ut, 1.01)

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

			tester(t, NewMemChunk(EncGZIP, testBlockSize, testTargetSize))
		})
	}
}

func TestChunkSize(t *testing.T) {
	for _, enc := range testEncoding {
		t.Run(enc.String(), func(t *testing.T) {
			c := NewMemChunk(enc, testBlockSize, testTargetSize)
			inserted := fillChunk(c)
			b, err := c.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			t.Log("Chunk size", humanize.Bytes(uint64(len(b))))
			t.Log("characters ", inserted)
		})

	}
}

func TestChunkStats(t *testing.T) {
	c := NewMemChunk(EncSnappy, testBlockSize, 0)
	first := time.Now()
	entry := &logproto.Entry{
		Timestamp: first,
		Line:      `ts=2020-03-16T13:58:33.459Z caller=dedupe.go:112 component=remote level=debug remote_name=3ea44a url=https:/blan.goo.net/api/prom/push msg=QueueManager.updateShardsLoop lowerBound=45.5 desiredShards=56.724401194003136 upperBound=84.5`,
	}
	inserted := 0
	// fill the chunk with known data size.
	for {
		if !c.SpaceFor(entry) {
			break
		}
		if err := c.Append(entry); err != nil {
			t.Fatal(err)
		}
		inserted++
		entry.Timestamp = entry.Timestamp.Add(time.Nanosecond)
	}
	expectedSize := (inserted * len(entry.Line)) + (inserted * 2 * binary.MaxVarintLen64)
	ctx := stats.NewContext(context.Background())

	it, err := c.Iterator(ctx, first.Add(-time.Hour), entry.Timestamp.Add(time.Hour), logproto.BACKWARD, logql.LineFilterFunc(func(line []byte) bool { return false }))
	if err != nil {
		t.Fatal(err)
	}
	for it.Next() {

	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	// test on a chunk filling up
	s := stats.Snapshot(ctx, time.Since(first))
	require.Equal(t, int64(expectedSize), s.Summary.TotalBytesProcessed)
	require.Equal(t, int64(inserted), s.Summary.TotalLinesProcessed)

	require.Equal(t, int64(expectedSize), s.Store.DecompressedBytes)
	require.Equal(t, int64(inserted), s.Store.DecompressedLines)

	b, err := c.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	// test on a new chunk.
	cb, err := NewByteChunk(b, testBlockSize, testTargetSize)
	if err != nil {
		t.Fatal(err)
	}
	ctx = stats.NewContext(context.Background())
	it, err = cb.Iterator(ctx, first.Add(-time.Hour), entry.Timestamp.Add(time.Hour), logproto.BACKWARD, logql.LineFilterFunc(func(line []byte) bool { return false }))
	if err != nil {
		t.Fatal(err)
	}
	for it.Next() {

	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	s = stats.Snapshot(ctx, time.Since(first))
	require.Equal(t, int64(expectedSize), s.Summary.TotalBytesProcessed)
	require.Equal(t, int64(inserted), s.Summary.TotalLinesProcessed)

	require.Equal(t, int64(expectedSize), s.Store.DecompressedBytes)
	require.Equal(t, int64(inserted), s.Store.DecompressedLines)
}

func TestIteratorClose(t *testing.T) {
	for _, enc := range testEncoding {
		t.Run(enc.String(), func(t *testing.T) {
			for _, test := range []func(iter iter.EntryIterator, t *testing.T){
				func(iter iter.EntryIterator, t *testing.T) {
					// close without iterating
					if err := iter.Close(); err != nil {
						t.Fatal(err)
					}
				},
				func(iter iter.EntryIterator, t *testing.T) {
					// close after iterating
					for iter.Next() {
						_ = iter.Entry()
					}
					if err := iter.Close(); err != nil {
						t.Fatal(err)
					}
				},
				func(iter iter.EntryIterator, t *testing.T) {
					// close after a single iteration
					iter.Next()
					_ = iter.Entry()
					if err := iter.Close(); err != nil {
						t.Fatal(err)
					}
				},
			} {
				c := NewMemChunk(enc, testBlockSize, testTargetSize)
				inserted := fillChunk(c)
				iter, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, inserted), logproto.BACKWARD, nil)
				if err != nil {
					t.Fatal(err)
				}
				test(iter, t)
			}

		})
	}
}

var result []Chunk

func BenchmarkWrite(b *testing.B) {
	chunks := []Chunk{}

	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      testdata.LogString(0),
	}
	i := int64(0)

	for _, enc := range testEncoding {
		b.Run(enc.String(), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				c := NewMemChunk(enc, testBlockSize, testTargetSize)
				// adds until full so we trigger cut which serialize using gzip
				for c.SpaceFor(entry) {
					_ = c.Append(entry)
					entry.Timestamp = time.Unix(0, i)
					entry.Line = testdata.LogString(i)
					i++
				}
				chunks = append(chunks, c)
			}
			result = chunks
		})
	}

}

func BenchmarkRead(b *testing.B) {
	for _, enc := range testEncoding {
		b.Run(enc.String(), func(b *testing.B) {
			chunks, size := generateData(enc)
			b.ResetTimer()
			bytesRead := uint64(0)
			now := time.Now()
			for n := 0; n < b.N; n++ {
				for _, c := range chunks {
					// use forward iterator for benchmark -- backward iterator does extra allocations by keeping entries in memory
					iterator, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Now(), logproto.FORWARD, nil)
					if err != nil {
						panic(err)
					}
					for iterator.Next() {
						_ = iterator.Entry()
					}
					if err := iterator.Close(); err != nil {
						b.Fatal(err)
					}
				}
				bytesRead += size
			}
			b.Log("bytes per second ", humanize.Bytes(uint64(float64(bytesRead)/time.Since(now).Seconds())))
			b.Log("n=", b.N)
		})
	}
}

func BenchmarkBackwardIterator(b *testing.B) {
	b.ReportAllocs()
	c := NewMemChunk(EncSnappy, testBlockSize, testTargetSize)
	_ = fillChunk(c)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		iterator, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Now(), logproto.BACKWARD, nil)
		if err != nil {
			panic(err)
		}
		for iterator.Next() {
			_ = iterator.Entry()
		}
		if err := iterator.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGenerateDataSize(t *testing.T) {
	for _, enc := range testEncoding {
		t.Run(enc.String(), func(t *testing.T) {
			chunks, size := generateData(enc)

			bytesRead := uint64(0)
			for _, c := range chunks {
				// use forward iterator for benchmark -- backward iterator does extra allocations by keeping entries in memory
				iterator, err := c.Iterator(context.TODO(), time.Unix(0, 0), time.Now(), logproto.FORWARD, logql.LineFilterFunc(func(line []byte) bool {
					return true // return all
				}))
				if err != nil {
					panic(err)
				}
				for iterator.Next() {
					e := iterator.Entry()
					bytesRead += uint64(len(e.Line))
				}
				if err := iterator.Close(); err != nil {
					t.Fatal(err)
				}
			}

			require.Equal(t, size, bytesRead)
		})
	}
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
				iter := h.iterator(context.Background(), 0, math.MaxInt64, nil)

				for iter.Next() {
					_ = iter.Entry()
				}
			}
		})
	}
}

func TestMemChunk_IteratorBounds(t *testing.T) {

	var createChunk = func() *MemChunk {
		t.Helper()
		c := NewMemChunk(EncNone, 1e6, 1e6)

		if err := c.Append(&logproto.Entry{
			Timestamp: time.Unix(0, 1),
			Line:      "1",
		}); err != nil {
			t.Fatal(err)
		}
		if err := c.Append(&logproto.Entry{
			Timestamp: time.Unix(0, 2),
			Line:      "2",
		}); err != nil {
			t.Fatal(err)
		}
		return c
	}

	for _, tt := range []struct {
		mint, maxt time.Time
		direction  logproto.Direction
		expect     []bool // array of expected values for next call in sequence
	}{
		{time.Unix(0, 0), time.Unix(0, 1), logproto.FORWARD, []bool{false}},
		{time.Unix(0, 1), time.Unix(0, 1), logproto.FORWARD, []bool{true, false}},
		{time.Unix(0, 1), time.Unix(0, 2), logproto.FORWARD, []bool{true, false}},
		{time.Unix(0, 2), time.Unix(0, 2), logproto.FORWARD, []bool{true, false}},
		{time.Unix(0, 1), time.Unix(0, 3), logproto.FORWARD, []bool{true, true, false}},
		{time.Unix(0, 2), time.Unix(0, 3), logproto.FORWARD, []bool{true, false}},
		{time.Unix(0, 3), time.Unix(0, 3), logproto.FORWARD, []bool{false}},

		{time.Unix(0, 0), time.Unix(0, 1), logproto.BACKWARD, []bool{false}},
		{time.Unix(0, 1), time.Unix(0, 1), logproto.BACKWARD, []bool{true, false}},
		{time.Unix(0, 1), time.Unix(0, 2), logproto.BACKWARD, []bool{true, false}},
		{time.Unix(0, 2), time.Unix(0, 2), logproto.BACKWARD, []bool{true, false}},
		{time.Unix(0, 1), time.Unix(0, 3), logproto.BACKWARD, []bool{true, true, false}},
		{time.Unix(0, 2), time.Unix(0, 3), logproto.BACKWARD, []bool{true, false}},
		{time.Unix(0, 3), time.Unix(0, 3), logproto.BACKWARD, []bool{false}},
	} {
		t.Run(
			fmt.Sprintf("mint:%d,maxt:%d,direction:%s", tt.mint.UnixNano(), tt.maxt.UnixNano(), tt.direction),
			func(t *testing.T) {
				tt := tt
				c := createChunk()

				// testing headchunk
				it, err := c.Iterator(context.Background(), tt.mint, tt.maxt, tt.direction, nil)
				require.NoError(t, err)
				for i := range tt.expect {
					require.Equal(t, tt.expect[i], it.Next())
				}
				require.NoError(t, it.Close())

				// testing chunk blocks
				require.NoError(t, c.cut())
				it, err = c.Iterator(context.Background(), tt.mint, tt.maxt, tt.direction, nil)
				require.NoError(t, err)
				for i := range tt.expect {
					require.Equal(t, tt.expect[i], it.Next())
				}
				require.NoError(t, it.Close())
			})

	}

}

func TestMemchunkLongLine(t *testing.T) {
	for _, enc := range testEncoding {
		t.Run(enc.String(), func(t *testing.T) {
			c := NewMemChunk(enc, testBlockSize, testTargetSize)
			for i := 1; i <= 10; i++ {
				require.NoError(t, c.Append(&logproto.Entry{Timestamp: time.Unix(0, int64(i)), Line: strings.Repeat("e", 200000)}))
			}
			it, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, 100), logproto.FORWARD, nil)
			require.NoError(t, err)
			for i := 1; i <= 10; i++ {
				require.True(t, it.Next())
			}
			require.False(t, it.Next())
		})
	}
}
