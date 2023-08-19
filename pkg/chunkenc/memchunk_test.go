package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc/testdata"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/pkg/storage/chunk"
)

var testEncoding = []Encoding{
	EncNone,
	EncGZIP,
	EncLZ4_64k,
	EncLZ4_256k,
	EncLZ4_1M,
	EncLZ4_4M,
	EncSnappy,
	EncFlate,
	EncZstd,
}

var (
	testBlockSize  = 256 * 1024
	testTargetSize = 1500 * 1024
	testBlockSizes = []int{64 * 1024, 256 * 1024, 512 * 1024}
	countExtractor = func() log.StreamSampleExtractor {
		ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
		if err != nil {
			panic(err)
		}
		return ex.ForStream(labels.Labels{})
	}()
	allPossibleFormats = []struct {
		headBlockFmt HeadBlockFmt
		chunkFormat  byte
	}{
		{
			headBlockFmt: OrderedHeadBlockFmt,
			chunkFormat:  ChunkFormatV2,
		},
		{
			headBlockFmt: OrderedHeadBlockFmt,
			chunkFormat:  ChunkFormatV3,
		},
		{
			headBlockFmt: UnorderedHeadBlockFmt,
			chunkFormat:  ChunkFormatV3,
		},
		{
			headBlockFmt: UnorderedWithNonIndexedLabelsHeadBlockFmt,
			chunkFormat:  ChunkFormatV4,
		},
	}
)

const DefaultTestHeadBlockFmt = DefaultHeadBlockFmt

func TestBlocksInclusive(t *testing.T) {
	chk := NewMemChunk(ChunkFormatV3, EncNone, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize)
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
		enc := enc
		for _, format := range allPossibleFormats {
			chunkFormat, headBlockFmt := format.chunkFormat, format.headBlockFmt
			t.Run(fmt.Sprintf("encoding:%v chunkFormat:%v headBlockFmt:%v", enc, chunkFormat, headBlockFmt), func(t *testing.T) {
				t.Parallel()
				chk := newMemChunkWithFormat(chunkFormat, enc, headBlockFmt, testBlockSize, testTargetSize)
				cases := []struct {
					ts  int64
					str string
					lbs []logproto.LabelAdapter
					cut bool
				}{
					{
						ts:  1,
						str: "hello, world!",
					},
					{
						ts:  2,
						str: "hello, world2!",
						lbs: []logproto.LabelAdapter{
							{Name: "app", Value: "myapp"},
						},
					},
					{
						ts:  3,
						str: "hello, world3!",
						lbs: []logproto.LabelAdapter{
							{Name: "a", Value: "a"},
							{Name: "b", Value: "b"},
						},
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
					{
						ts:  10,
						str: "hello, world10!",
						lbs: []logproto.LabelAdapter{
							{Name: "a", Value: "a2"},
							{Name: "b", Value: "b"},
						},
					},
				}

				for _, c := range cases {
					require.NoError(t, chk.Append(logprotoEntryWithNonIndexedLabels(c.ts, c.str, c.lbs)))
					if c.cut {
						require.NoError(t, chk.cut())
					}
				}

				var noopStreamPipeline = log.NewNoopPipeline().ForStream(labels.Labels{})

				it, err := chk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline)
				require.NoError(t, err)

				idx := 0
				for it.Next() {
					e := it.Entry()
					require.Equal(t, cases[idx].ts, e.Timestamp.UnixNano())
					require.Equal(t, cases[idx].str, e.Line)
					require.Empty(t, e.NonIndexedLabels)
					if chunkFormat < ChunkFormatV4 {
						require.Equal(t, labels.EmptyLabels().String(), it.Labels())
					} else {
						expectedLabels := logproto.FromLabelAdaptersToLabels(cases[idx].lbs).String()
						require.Equal(t, expectedLabels, it.Labels())
					}
					idx++
				}

				require.NoError(t, it.Error())
				require.NoError(t, it.Close())
				require.Equal(t, len(cases), idx)

				countExtractor = func() log.StreamSampleExtractor {
					ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
					if err != nil {
						panic(err)
					}
					return ex.ForStream(labels.Labels{})
				}()

				sampleIt := chk.SampleIterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), countExtractor)
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
					it, err := chk.Iterator(context.Background(), time.Unix(0, 3), time.Unix(0, 7), logproto.FORWARD, noopStreamPipeline)
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
}

func TestCorruptChunk(t *testing.T) {
	for _, enc := range testEncoding {
		enc := enc
		t.Run(enc.String(), func(t *testing.T) {
			t.Parallel()

			chk := NewMemChunk(ChunkFormatV3, enc, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize)
			cases := []struct {
				data []byte
			}{
				// Data that should not decode as lines from a chunk in any encoding.
				{data: []byte{0}},
				{data: []byte{1}},
				{data: []byte("asdfasdfasdfqwyteqwtyeq")},
			}

			ctx, start, end := context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64)
			for i, c := range cases {
				chk.blocks = []block{{b: c.data}}
				it, err := chk.Iterator(ctx, start, end, logproto.FORWARD, noopStreamPipeline)
				require.NoError(t, err, "case %d", i)

				idx := 0
				for it.Next() {
					idx++
				}
				require.Error(t, it.Error(), "case %d", i)
				require.NoError(t, it.Close())
			}
		})
	}
}

func TestReadFormatV1(t *testing.T) {
	t.Parallel()

	c := NewMemChunk(ChunkFormatV3, EncGZIP, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize)
	fillChunk(c)
	// overrides default v2 format
	c.format = ChunkFormatV1

	b, err := c.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	r, err := NewByteChunk(b, testBlockSize, testTargetSize)
	if err != nil {
		t.Fatal(err)
	}

	it, err := r.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline)
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
	for _, testData := range allPossibleFormats {
		for _, enc := range testEncoding {
			enc := enc
			t.Run(testNameWithFormats(enc, testData.chunkFormat, testData.headBlockFmt), func(t *testing.T) {
				t.Parallel()

				c := newMemChunkWithFormat(testData.chunkFormat, enc, testData.headBlockFmt, testBlockSize, testTargetSize)
				populated := fillChunk(c)

				assertLines := func(c *MemChunk) {
					require.Equal(t, enc, c.Encoding())
					it, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline)
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
}

func testNameWithFormats(enc Encoding, chunkFormat byte, headBlockFmt HeadBlockFmt) string {
	return fmt.Sprintf("encoding:%v chunkFormat:%v headBlockFmt:%v", enc, chunkFormat, headBlockFmt)
}

func TestRoundtripV3(t *testing.T) {
	for _, f := range HeadBlockFmts {
		for _, enc := range testEncoding {
			enc := enc
			t.Run(fmt.Sprintf("%v-%v", f, enc), func(t *testing.T) {
				t.Parallel()

				c := NewMemChunk(ChunkFormatV3, enc, f, testBlockSize, testTargetSize)
				c.format = ChunkFormatV3
				_ = fillChunk(c)

				b, err := c.Bytes()
				require.Nil(t, err)
				r, err := NewByteChunk(b, testBlockSize, testTargetSize)
				require.Nil(t, err)

				b2, err := r.Bytes()
				require.Nil(t, err)
				require.Equal(t, b, b2)
			})
		}
	}
}

func TestSerialization(t *testing.T) {
	for _, testData := range allPossibleFormats {
		for _, enc := range testEncoding {
			enc := enc
			// run tests with and without non-indexed labels set since it is optional
			for _, appendWithNonIndexedLabels := range []bool{false, true} {
				appendWithNonIndexedLabels := appendWithNonIndexedLabels
				testName := testNameWithFormats(enc, testData.chunkFormat, testData.headBlockFmt)
				if appendWithNonIndexedLabels {
					testName = fmt.Sprintf("%s - append non-indexed labels", testName)
				} else {
					testName = fmt.Sprintf("%s - without non-indexed labels", testName)
				}
				t.Run(testName, func(t *testing.T) {
					t.Parallel()

					chk := NewMemChunk(testData.chunkFormat, enc, testData.headBlockFmt, testBlockSize, testTargetSize)
					chk.format = testData.chunkFormat
					numSamples := 50000
					var entry *logproto.Entry

					for i := 0; i < numSamples; i++ {
						entry = logprotoEntry(int64(i), strconv.Itoa(i))
						if appendWithNonIndexedLabels {
							entry.NonIndexedLabels = []logproto.LabelAdapter{{Name: "foo", Value: strconv.Itoa(i)}}
						}
						require.NoError(t, chk.Append(entry))
					}
					require.NoError(t, chk.Close())

					byt, err := chk.Bytes()
					require.NoError(t, err)

					bc, err := NewByteChunk(byt, testBlockSize, testTargetSize)
					require.NoError(t, err)

					it, err := bc.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
					require.NoError(t, err)
					for i := 0; i < numSamples; i++ {
						require.True(t, it.Next())

						e := it.Entry()
						require.Equal(t, int64(i), e.Timestamp.UnixNano())
						require.Equal(t, strconv.Itoa(i), e.Line)
						require.Nil(t, e.NonIndexedLabels)
						if appendWithNonIndexedLabels && testData.chunkFormat >= ChunkFormatV4 {
							require.Equal(t, labels.FromStrings("foo", strconv.Itoa(i)).String(), it.Labels())
						} else {
							require.Equal(t, labels.EmptyLabels().String(), it.Labels())
						}
					}
					require.NoError(t, it.Error())

					countExtractor = func() log.StreamSampleExtractor {
						ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
						if err != nil {
							panic(err)
						}
						return ex.ForStream(labels.Labels{})
					}()

					sampleIt := bc.SampleIterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), countExtractor)
					for i := 0; i < numSamples; i++ {
						require.True(t, sampleIt.Next(), i)

						s := sampleIt.Sample()
						require.Equal(t, int64(i), s.Timestamp)
						require.Equal(t, 1., s.Value)
						if appendWithNonIndexedLabels && testData.chunkFormat >= ChunkFormatV4 {
							require.Equal(t, labels.FromStrings("foo", strconv.Itoa(i)).String(), sampleIt.Labels())
						} else {
							require.Equal(t, labels.EmptyLabels().String(), sampleIt.Labels())
						}
					}
					require.NoError(t, sampleIt.Error())

					byt2, err := chk.Bytes()
					require.NoError(t, err)

					require.True(t, bytes.Equal(byt, byt2))
				})
			}
		}
	}
}

func TestChunkFilling(t *testing.T) {
	for _, testData := range allPossibleFormats {
		for _, enc := range testEncoding {
			enc := enc
			t.Run(testNameWithFormats(enc, testData.chunkFormat, testData.headBlockFmt), func(t *testing.T) {
				t.Parallel()

				chk := newMemChunkWithFormat(testData.chunkFormat, enc, testData.headBlockFmt, testBlockSize, 0)
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

				it, err := chk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, 100), logproto.FORWARD, noopStreamPipeline)
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
}

func TestGZIPChunkTargetSize(t *testing.T) {
	t.Parallel()

	chk := NewMemChunk(ChunkFormatV3, EncGZIP, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize)

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

	require.Equal(t, 0, chk.head.UncompressedSize())

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

			if chk.headFmt == OrderedHeadBlockFmt {
				assert.EqualError(t, chk.Append(logprotoEntry(1, "test")), ErrOutOfOrder.Error())
			} else {
				assert.NoError(t, chk.Append(logprotoEntry(1, "test")))
			}
		},
		"append out of order in a new block right after cutting the previous one": func(t *testing.T, chk *MemChunk) {
			assert.NoError(t, chk.Append(logprotoEntry(5, "test")))
			assert.NoError(t, chk.Append(logprotoEntry(6, "test")))
			assert.NoError(t, chk.cut())

			if chk.headFmt == OrderedHeadBlockFmt {
				assert.EqualError(t, chk.Append(logprotoEntry(1, "test")), ErrOutOfOrder.Error())
			} else {
				assert.NoError(t, chk.Append(logprotoEntry(1, "test")))
			}
		},
		"append out of order in a new block after multiple cuts": func(t *testing.T, chk *MemChunk) {
			assert.NoError(t, chk.Append(logprotoEntry(5, "test")))
			assert.NoError(t, chk.cut())

			assert.NoError(t, chk.Append(logprotoEntry(6, "test")))
			assert.NoError(t, chk.cut())

			if chk.headFmt == OrderedHeadBlockFmt {
				assert.EqualError(t, chk.Append(logprotoEntry(1, "test")), ErrOutOfOrder.Error())
			} else {
				assert.NoError(t, chk.Append(logprotoEntry(1, "test")))
			}
		},
	}

	for _, f := range HeadBlockFmts {
		for testName, tester := range tests {
			tester := tester

			t.Run(testName, func(t *testing.T) {
				t.Parallel()

				tester(t, NewMemChunk(ChunkFormatV3, EncGZIP, f, testBlockSize, testTargetSize))
			})
		}
	}
}

func TestChunkSize(t *testing.T) {
	type res struct {
		name           string
		size           uint64
		compressedSize uint64
		ratio          float64
	}
	var result []res
	for _, bs := range testBlockSizes {
		for _, f := range allPossibleFormats {
			for _, enc := range testEncoding {
				name := fmt.Sprintf("%s_%s", enc.String(), humanize.Bytes(uint64(bs)))
				t.Run(name, func(t *testing.T) {
					c := newMemChunkWithFormat(f.chunkFormat, enc, f.headBlockFmt, bs, testTargetSize)
					inserted := fillChunk(c)
					b, err := c.Bytes()
					if err != nil {
						t.Fatal(err)
					}
					result = append(result, res{
						name:           name,
						size:           uint64(inserted),
						compressedSize: uint64(len(b)),
						ratio:          float64(inserted) / float64(len(b)),
					})
				})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ratio > result[j].ratio
	})
	fmt.Printf("%s\t%s\t%s\t%s\n", "name", "uncompressed", "compressed", "ratio")
	for _, r := range result {
		fmt.Printf("%s\t%s\t%s\t%f\n", r.name, humanize.Bytes(r.size), humanize.Bytes(r.compressedSize), r.ratio)
	}
}

func TestChunkStats(t *testing.T) {
	c := NewMemChunk(ChunkFormatV3, EncSnappy, DefaultTestHeadBlockFmt, testBlockSize, 0)
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
	// For each entry: timestamp <varint>, line size <varint>, line <bytes>, num of non-indexed labels <varint>
	expectedSize := inserted * (len(entry.Line) + 3*binary.MaxVarintLen64)
	statsCtx, ctx := stats.NewContext(context.Background())

	it, err := c.Iterator(ctx, first.Add(-time.Hour), entry.Timestamp.Add(time.Hour), logproto.BACKWARD, noopStreamPipeline)
	if err != nil {
		t.Fatal(err)
	}
	for it.Next() {
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	// test on a chunk filling up
	s := statsCtx.Result(time.Since(first), 0, 0)
	require.Equal(t, int64(expectedSize), s.Summary.TotalBytesProcessed)
	require.Equal(t, int64(inserted), s.Summary.TotalLinesProcessed)

	require.Equal(t, int64(expectedSize), s.TotalDecompressedBytes())
	require.Equal(t, int64(inserted), s.TotalDecompressedLines())

	b, err := c.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	// test on a new chunk.
	cb, err := NewByteChunk(b, testBlockSize, testTargetSize)
	if err != nil {
		t.Fatal(err)
	}
	statsCtx, ctx = stats.NewContext(context.Background())
	it, err = cb.Iterator(ctx, first.Add(-time.Hour), entry.Timestamp.Add(time.Hour), logproto.BACKWARD, noopStreamPipeline)
	if err != nil {
		t.Fatal(err)
	}
	for it.Next() {
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	s = statsCtx.Result(time.Since(first), 0, 0)
	require.Equal(t, int64(expectedSize), s.Summary.TotalBytesProcessed)
	require.Equal(t, int64(inserted), s.Summary.TotalLinesProcessed)

	require.Equal(t, int64(expectedSize), s.TotalDecompressedBytes())
	require.Equal(t, int64(inserted), s.TotalDecompressedLines())
}

func TestIteratorClose(t *testing.T) {
	for _, f := range allPossibleFormats {
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
					c := newMemChunkWithFormat(f.chunkFormat, enc, f.headBlockFmt, testBlockSize, testTargetSize)
					inserted := fillChunk(c)
					iter, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, inserted), logproto.BACKWARD, noopStreamPipeline)
					if err != nil {
						t.Fatal(err)
					}
					test(iter, t)
				}
			})
		}
	}
}

func BenchmarkWrite(b *testing.B) {
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      testdata.LogString(0),
	}
	i := int64(0)

	for _, f := range HeadBlockFmts {
		for _, enc := range testEncoding {
			for _, withNonIndexedLabels := range []bool{false, true} {
				name := fmt.Sprintf("%v-%v", f, enc)
				if withNonIndexedLabels {
					name += "-withNonIndexedLabels"
				}
				b.Run(name, func(b *testing.B) {
					uncompressedBytes, compressedBytes := 0, 0
					for n := 0; n < b.N; n++ {
						c := NewMemChunk(ChunkFormatV3, enc, f, testBlockSize, testTargetSize)
						// adds until full so we trigger cut which serialize using gzip
						for c.SpaceFor(entry) {
							_ = c.Append(entry)
							entry.Timestamp = time.Unix(0, i)
							entry.Line = testdata.LogString(i)
							if withNonIndexedLabels {
								entry.NonIndexedLabels = []logproto.LabelAdapter{
									{Name: "foo", Value: fmt.Sprint(i)},
								}
							}
							i++
						}
						uncompressedBytes += c.UncompressedSize()
						compressedBytes += c.CompressedSize()
					}
					b.SetBytes(int64(uncompressedBytes) / int64(b.N))
					b.ReportMetric(float64(compressedBytes)/float64(uncompressedBytes)*100, "%compressed")
				})
			}
		}
	}
}

type nomatchPipeline struct{}

func (nomatchPipeline) BaseLabels() log.LabelsResult { return log.EmptyLabelsResult }
func (nomatchPipeline) Process(_ int64, line []byte, _ ...labels.Label) ([]byte, log.LabelsResult, bool) {
	return line, nil, false
}
func (nomatchPipeline) ProcessString(_ int64, line string, _ ...labels.Label) (string, log.LabelsResult, bool) {
	return line, nil, false
}

func BenchmarkRead(b *testing.B) {
	for _, bs := range testBlockSizes {
		for _, enc := range testEncoding {
			name := fmt.Sprintf("%s_%s", enc.String(), humanize.Bytes(uint64(bs)))
			b.Run(name, func(b *testing.B) {
				chunks, size := generateData(enc, 5, bs, testTargetSize)
				_, ctx := stats.NewContext(context.Background())
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					for _, c := range chunks {
						// use forward iterator for benchmark -- backward iterator does extra allocations by keeping entries in memory
						iterator, err := c.Iterator(ctx, time.Unix(0, 0), time.Now(), logproto.FORWARD, nomatchPipeline{})
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
				b.SetBytes(int64(size))
			})
		}
	}

	for _, bs := range testBlockSizes {
		for _, enc := range testEncoding {
			name := fmt.Sprintf("sample_%s_%s", enc.String(), humanize.Bytes(uint64(bs)))
			b.Run(name, func(b *testing.B) {
				chunks, size := generateData(enc, 5, bs, testTargetSize)
				_, ctx := stats.NewContext(context.Background())
				b.ResetTimer()
				bytesRead := uint64(0)
				for n := 0; n < b.N; n++ {
					for _, c := range chunks {
						iterator := c.SampleIterator(ctx, time.Unix(0, 0), time.Now(), countExtractor)
						for iterator.Next() {
							_ = iterator.Sample()
						}
						if err := iterator.Close(); err != nil {
							b.Fatal(err)
						}
					}
					bytesRead += size
				}
				b.SetBytes(int64(bytesRead) / int64(b.N))
			})
		}
	}
}

func BenchmarkBackwardIterator(b *testing.B) {
	for _, bs := range testBlockSizes {
		b.Run(humanize.Bytes(uint64(bs)), func(b *testing.B) {
			b.ReportAllocs()
			c := NewMemChunk(ChunkFormatV3, EncSnappy, DefaultTestHeadBlockFmt, bs, testTargetSize)
			_ = fillChunk(c)
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				iterator, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Now(), logproto.BACKWARD, noopStreamPipeline)
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
		})
	}
}

func TestGenerateDataSize(t *testing.T) {
	for _, enc := range testEncoding {
		t.Run(enc.String(), func(t *testing.T) {
			chunks, size := generateData(enc, 50, testBlockSize, testTargetSize)

			bytesRead := uint64(0)
			for _, c := range chunks {
				// use forward iterator for benchmark -- backward iterator does extra allocations by keeping entries in memory
				iterator, err := c.Iterator(context.TODO(), time.Unix(0, 0), time.Now(), logproto.FORWARD, noopStreamPipeline)
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
		for _, withNonIndexedLabels := range []bool{false, true} {
			b.Run(fmt.Sprintf("size=%d nonIndexedLabels=%v", j, withNonIndexedLabels), func(b *testing.B) {
				h := headBlock{}

				var nonIndexedLabels labels.Labels
				if withNonIndexedLabels {
					nonIndexedLabels = labels.Labels{{Name: "foo", Value: "foo"}}
				}

				for i := 0; i < j; i++ {
					if err := h.Append(int64(i), "this is the append string", nonIndexedLabels); err != nil {
						b.Fatal(err)
					}
				}

				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					iter := h.Iterator(context.Background(), logproto.BACKWARD, 0, math.MaxInt64, noopStreamPipeline)

					for iter.Next() {
						_ = iter.Entry()
					}
				}
			})
		}
	}
}

func BenchmarkHeadBlockSampleIterator(b *testing.B) {
	for _, j := range []int{20000, 10000, 8000, 5000} {
		for _, withNonIndexedLabels := range []bool{false, true} {
			b.Run(fmt.Sprintf("size=%d nonIndexedLabels=%v", j, withNonIndexedLabels), func(b *testing.B) {
				h := headBlock{}

				var nonIndexedLabels labels.Labels
				if withNonIndexedLabels {
					nonIndexedLabels = labels.Labels{{Name: "foo", Value: "foo"}}
				}

				for i := 0; i < j; i++ {
					if err := h.Append(int64(i), "this is the append string", nonIndexedLabels); err != nil {
						b.Fatal(err)
					}
				}

				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					iter := h.SampleIterator(context.Background(), 0, math.MaxInt64, countExtractor)

					for iter.Next() {
						_ = iter.Sample()
					}
					iter.Close()
				}
			})
		}
	}
}

func TestMemChunk_IteratorBounds(t *testing.T) {
	createChunk := func() *MemChunk {
		t.Helper()
		c := NewMemChunk(ChunkFormatV3, EncNone, DefaultTestHeadBlockFmt, 1e6, 1e6)

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
		{time.Unix(0, 1), time.Unix(0, 2), logproto.FORWARD, []bool{true, false}},
		{time.Unix(0, 1), time.Unix(0, 3), logproto.FORWARD, []bool{true, true, false}},
		{time.Unix(0, 2), time.Unix(0, 3), logproto.FORWARD, []bool{true, false}},

		{time.Unix(0, 0), time.Unix(0, 1), logproto.BACKWARD, []bool{false}},
		{time.Unix(0, 1), time.Unix(0, 2), logproto.BACKWARD, []bool{true, false}},
		{time.Unix(0, 1), time.Unix(0, 3), logproto.BACKWARD, []bool{true, true, false}},
		{time.Unix(0, 2), time.Unix(0, 3), logproto.BACKWARD, []bool{true, false}},
	} {
		t.Run(
			fmt.Sprintf("mint:%d,maxt:%d,direction:%s", tt.mint.UnixNano(), tt.maxt.UnixNano(), tt.direction),
			func(t *testing.T) {
				tt := tt
				c := createChunk()

				// testing headchunk
				it, err := c.Iterator(context.Background(), tt.mint, tt.maxt, tt.direction, noopStreamPipeline)
				require.NoError(t, err)
				for idx, expected := range tt.expect {
					require.Equal(t, expected, it.Next(), "idx: %s", idx)
				}
				require.NoError(t, it.Close())

				// testing chunk blocks
				require.NoError(t, c.cut())
				it, err = c.Iterator(context.Background(), tt.mint, tt.maxt, tt.direction, noopStreamPipeline)
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
		enc := enc
		t.Run(enc.String(), func(t *testing.T) {
			t.Parallel()

			c := NewMemChunk(ChunkFormatV3, enc, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize)
			for i := 1; i <= 10; i++ {
				require.NoError(t, c.Append(&logproto.Entry{Timestamp: time.Unix(0, int64(i)), Line: strings.Repeat("e", 200000)}))
			}
			it, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, 100), logproto.FORWARD, noopStreamPipeline)
			require.NoError(t, err)
			for i := 1; i <= 10; i++ {
				require.True(t, it.Next())
			}
			require.False(t, it.Next())
		})
	}
}

// Ensure passing a reusable []byte doesn't affect output
func TestBytesWith(t *testing.T) {
	t.Parallel()

	exp, err := NewMemChunk(ChunkFormatV3, EncNone, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize).BytesWith(nil)
	require.Nil(t, err)
	out, err := NewMemChunk(ChunkFormatV3, EncNone, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize).BytesWith([]byte{1, 2, 3})
	require.Nil(t, err)

	require.Equal(t, exp, out)
}

func TestCheckpointEncoding(t *testing.T) {
	t.Parallel()

	blockSize, targetSize := 256*1024, 1500*1024
	for _, f := range allPossibleFormats {
		t.Run(testNameWithFormats(EncSnappy, f.chunkFormat, f.headBlockFmt), func(t *testing.T) {
			c := newMemChunkWithFormat(f.chunkFormat, EncSnappy, f.headBlockFmt, blockSize, targetSize)

			// add a few entries
			for i := 0; i < 5; i++ {
				entry := &logproto.Entry{
					Timestamp: time.Unix(int64(i), 0),
					Line:      fmt.Sprintf("hi there - %d", i),
					NonIndexedLabels: push.LabelsAdapter{{
						Name:  fmt.Sprintf("name%d", i),
						Value: fmt.Sprintf("val%d", i),
					}},
				}
				require.Equal(t, true, c.SpaceFor(entry))
				require.Nil(t, c.Append(entry))
			}

			// cut it
			require.Nil(t, c.cut())

			// ensure we have cut a block and head block is empty
			require.Equal(t, 1, len(c.blocks))
			require.True(t, c.head.IsEmpty())

			// check entries with empty head
			var chk, head bytes.Buffer
			var err error
			var cpy *MemChunk
			err = c.SerializeForCheckpointTo(&chk, &head)
			require.Nil(t, err)

			cpy, err = MemchunkFromCheckpoint(chk.Bytes(), head.Bytes(), f.headBlockFmt, blockSize, targetSize)
			require.Nil(t, err)

			if f.chunkFormat <= ChunkFormatV2 {
				for i := range c.blocks {
					c.blocks[i].uncompressedSize = 0
				}
			}

			require.Equal(t, c, cpy)

			// add a few more to head
			for i := 5; i < 10; i++ {
				entry := &logproto.Entry{
					Timestamp: time.Unix(int64(i), 0),
					Line:      fmt.Sprintf("hi there - %d", i),
				}
				require.Equal(t, true, c.SpaceFor(entry))
				require.Nil(t, c.Append(entry))
			}

			// ensure new blocks are not cut
			require.Equal(t, 1, len(c.blocks))

			chk.Reset()
			head.Reset()
			err = c.SerializeForCheckpointTo(&chk, &head)
			require.Nil(t, err)

			cpy, err = MemchunkFromCheckpoint(chk.Bytes(), head.Bytes(), f.headBlockFmt, blockSize, targetSize)
			require.Nil(t, err)

			if f.chunkFormat <= ChunkFormatV2 {
				for i := range c.blocks {
					c.blocks[i].uncompressedSize = 0
				}
			}

			require.Equal(t, c, cpy)
		})
	}
}

var (
	streams = []logproto.Stream{}
	series  = []logproto.Series{}
)

func BenchmarkBufferedIteratorLabels(b *testing.B) {
	for _, f := range HeadBlockFmts {
		b.Run(f.String(), func(b *testing.B) {
			c := NewMemChunk(ChunkFormatV3, EncSnappy, f, testBlockSize, testTargetSize)
			_ = fillChunk(c)

			labelsSet := []labels.Labels{
				{
					{Name: "cluster", Value: "us-central1"},
					{Name: "stream", Value: "stdout"},
					{Name: "filename", Value: "/var/log/pods/loki-prod_query-frontend-6894f97b98-89q2n_eac98024-f60f-44af-a46f-d099bc99d1e7/query-frontend/0.log"},
					{Name: "namespace", Value: "loki-dev"},
					{Name: "job", Value: "loki-prod/query-frontend"},
					{Name: "container", Value: "query-frontend"},
					{Name: "pod", Value: "query-frontend-6894f97b98-89q2n"},
				},
				{
					{Name: "cluster", Value: "us-central2"},
					{Name: "stream", Value: "stderr"},
					{Name: "filename", Value: "/var/log/pods/loki-prod_querier-6894f97b98-89q2n_eac98024-f60f-44af-a46f-d099bc99d1e7/query-frontend/0.log"},
					{Name: "namespace", Value: "loki-dev"},
					{Name: "job", Value: "loki-prod/querier"},
					{Name: "container", Value: "querier"},
					{Name: "pod", Value: "querier-6894f97b98-89q2n"},
				},
			}
			for _, test := range []string{
				`{app="foo"}`,
				`{app="foo"} != "foo"`,
				`{app="foo"} != "foo" | logfmt `,
				`{app="foo"} != "foo" | logfmt | duration > 10ms`,
				`{app="foo"} != "foo" | logfmt | duration > 10ms and component="tsdb"`,
			} {
				b.Run(test, func(b *testing.B) {
					b.ReportAllocs()
					expr, err := syntax.ParseLogSelector(test, true)
					if err != nil {
						b.Fatal(err)
					}
					p, err := expr.Pipeline()
					if err != nil {
						b.Fatal(err)
					}
					var iters []iter.EntryIterator
					for _, lbs := range labelsSet {
						it, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Now(), logproto.FORWARD, p.ForStream(lbs))
						if err != nil {
							b.Fatal(err)
						}
						iters = append(iters, it)
					}
					b.ResetTimer()
					for n := 0; n < b.N; n++ {
						for _, it := range iters {
							for it.Next() {
								streams = append(streams, logproto.Stream{Labels: it.Labels(), Entries: []logproto.Entry{it.Entry()}})
							}
						}
					}
					streams = streams[:0]
				})
			}

			for _, test := range []string{
				`rate({app="foo"}[1m])`,
				`sum by (cluster) (rate({app="foo"}[10s]))`,
				`sum by (cluster) (rate({app="foo"} != "foo" [10s]))`,
				`sum by (cluster) (rate({app="foo"} != "foo" | logfmt[10s]))`,
				`sum by (caller) (rate({app="foo"} != "foo" | logfmt[10s]))`,
				`sum by (cluster) (rate({app="foo"} != "foo" | logfmt | duration > 10ms[10s]))`,
				`sum by (cluster) (rate({app="foo"} != "foo" | logfmt | duration > 10ms and component="tsdb"[1m]))`,
			} {
				b.Run(test, func(b *testing.B) {
					b.ReportAllocs()
					expr, err := syntax.ParseSampleExpr(test)
					if err != nil {
						b.Fatal(err)
					}
					ex, err := expr.Extractor()
					if err != nil {
						b.Fatal(err)
					}
					var iters []iter.SampleIterator
					for _, lbs := range labelsSet {
						iters = append(iters, c.SampleIterator(context.Background(), time.Unix(0, 0), time.Now(), ex.ForStream(lbs)))
					}
					b.ResetTimer()
					for n := 0; n < b.N; n++ {
						for _, it := range iters {
							for it.Next() {
								series = append(series, logproto.Series{Labels: it.Labels(), Samples: []logproto.Sample{it.Sample()}})
							}
						}
					}
					series = series[:0]
				})
			}
		})
	}
}

func Test_HeadIteratorReverse(t *testing.T) {
	for _, testData := range allPossibleFormats {
		t.Run(testNameWithFormats(EncSnappy, testData.chunkFormat, testData.headBlockFmt), func(t *testing.T) {
			c := newMemChunkWithFormat(testData.chunkFormat, EncSnappy, testData.headBlockFmt, testBlockSize, testTargetSize)
			genEntry := func(i int64) *logproto.Entry {
				return &logproto.Entry{
					Timestamp: time.Unix(0, i),
					Line:      fmt.Sprintf(`msg="%d"`, i),
				}
			}
			var i int64
			for e := genEntry(i); c.SpaceFor(e); e, i = genEntry(i+1), i+1 {
				require.NoError(t, c.Append(e))
			}

			assertOrder := func(t *testing.T, total int64) {
				expr, err := syntax.ParseLogSelector(`{app="foo"} | logfmt`, true)
				require.NoError(t, err)
				p, err := expr.Pipeline()
				require.NoError(t, err)
				it, err := c.Iterator(context.TODO(), time.Unix(0, 0), time.Unix(0, i), logproto.BACKWARD, p.ForStream(labels.Labels{{Name: "app", Value: "foo"}}))
				require.NoError(t, err)
				for it.Next() {
					total--
					require.Equal(t, total, it.Entry().Timestamp.UnixNano())
				}
			}

			assertOrder(t, i)
			// let's try again without the headblock.
			require.NoError(t, c.cut())
			assertOrder(t, i)
		})
	}
}

func TestMemChunk_Rebound(t *testing.T) {
	chkFrom := time.Unix(0, 0)
	chkThrough := chkFrom.Add(time.Hour)
	originalChunk := buildTestMemChunk(t, chkFrom, chkThrough)

	for _, tc := range []struct {
		name               string
		sliceFrom, sliceTo time.Time
		err                error
	}{
		{
			name:      "slice whole chunk",
			sliceFrom: chkFrom,
			sliceTo:   chkThrough,
		},
		{
			name:      "slice first half",
			sliceFrom: chkFrom,
			sliceTo:   chkFrom.Add(30 * time.Minute),
		},
		{
			name:      "slice second half",
			sliceFrom: chkFrom.Add(30 * time.Minute),
			sliceTo:   chkThrough,
		},
		{
			name:      "slice in the middle",
			sliceFrom: chkFrom.Add(15 * time.Minute),
			sliceTo:   chkFrom.Add(45 * time.Minute),
		},
		{
			name:      "slice interval not aligned with sample intervals",
			sliceFrom: chkFrom.Add(time.Second),
			sliceTo:   chkThrough.Add(-time.Second),
		},
		{
			name:      "slice out of bounds without overlap",
			err:       chunk.ErrSliceNoDataInRange,
			sliceFrom: chkThrough.Add(time.Minute),
			sliceTo:   chkThrough.Add(time.Hour),
		},
		{
			name:      "slice out of bounds with overlap",
			sliceFrom: chkFrom.Add(10 * time.Minute),
			sliceTo:   chkThrough.Add(10 * time.Minute),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newChunk, err := originalChunk.Rebound(tc.sliceFrom, tc.sliceTo, nil)
			if tc.err != nil {
				require.Equal(t, tc.err, err)
				return
			}
			require.NoError(t, err)

			// iterate originalChunk from slice start to slice end + nanosecond. Adding a nanosecond here to be inclusive of sample at end time.
			originalChunkItr, err := originalChunk.Iterator(context.Background(), tc.sliceFrom, tc.sliceTo.Add(time.Nanosecond), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
			require.NoError(t, err)

			// iterate newChunk for whole chunk interval which should include all the samples in the chunk and hence align it with expected values.
			newChunkItr, err := newChunk.Iterator(context.Background(), chkFrom, chkThrough, logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
			require.NoError(t, err)

			for {
				originalChunksHasMoreSamples := originalChunkItr.Next()
				newChunkHasMoreSamples := newChunkItr.Next()

				// either both should have samples or none of them
				require.Equal(t, originalChunksHasMoreSamples, newChunkHasMoreSamples)
				if !originalChunksHasMoreSamples {
					break
				}

				require.Equal(t, originalChunkItr.Entry(), newChunkItr.Entry())
			}
		})
	}
}

func buildTestMemChunk(t *testing.T, from, through time.Time) *MemChunk {
	chk := NewMemChunk(ChunkFormatV3, EncGZIP, DefaultTestHeadBlockFmt, defaultBlockSize, 0)
	for ; from.Before(through); from = from.Add(time.Second) {
		err := chk.Append(&logproto.Entry{
			Line:      from.String(),
			Timestamp: from,
		})
		require.NoError(t, err)
	}

	return chk
}

func TestMemChunk_ReboundAndFilter_with_filter(t *testing.T) {
	chkFrom := time.Unix(1, 0) // headBlock.Append treats Unix time 0 as not set so we have to use a later time
	chkFromPlus5 := chkFrom.Add(5 * time.Second)
	chkThrough := chkFrom.Add(10 * time.Second)
	chkThroughPlus1 := chkThrough.Add(1 * time.Second)

	filterFunc := func(_ time.Time, in string) bool {
		return strings.HasPrefix(in, "matching")
	}

	for _, tc := range []struct {
		name                               string
		matchingSliceFrom, matchingSliceTo *time.Time
		err                                error
		nrMatching                         int
		nrNotMatching                      int
	}{
		{
			name:          "no matches",
			nrMatching:    0,
			nrNotMatching: 10,
		},
		{
			name:              "some lines removed",
			matchingSliceFrom: &chkFrom,
			matchingSliceTo:   &chkFromPlus5,
			nrMatching:        5,
			nrNotMatching:     5,
		},
		{
			name:              "all lines match",
			err:               chunk.ErrSliceNoDataInRange,
			matchingSliceFrom: &chkFrom,
			matchingSliceTo:   &chkThroughPlus1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalChunk := buildFilterableTestMemChunk(t, chkFrom, chkThrough, tc.matchingSliceFrom, tc.matchingSliceTo)
			newChunk, err := originalChunk.Rebound(chkFrom, chkThrough, filterFunc)
			if tc.err != nil {
				require.Equal(t, tc.err, err)
				return
			}
			require.NoError(t, err)

			// iterate originalChunk from slice start to slice end + nanosecond. Adding a nanosecond here to be inclusive of sample at end time.
			originalChunkItr, err := originalChunk.Iterator(context.Background(), chkFrom, chkThrough.Add(time.Nanosecond), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
			require.NoError(t, err)
			originalChunkSamples := 0
			for originalChunkItr.Next() {
				originalChunkSamples++
			}
			require.Equal(t, tc.nrMatching+tc.nrNotMatching, originalChunkSamples)

			// iterate newChunk for whole chunk interval which should include all the samples in the chunk and hence align it with expected values.
			newChunkItr, err := newChunk.Iterator(context.Background(), chkFrom, chkThrough.Add(time.Nanosecond), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
			require.NoError(t, err)
			newChunkSamples := 0
			for newChunkItr.Next() {
				newChunkSamples++
			}
			require.Equal(t, tc.nrNotMatching, newChunkSamples)
		})
	}
}

func buildFilterableTestMemChunk(t *testing.T, from, through time.Time, matchingFrom, matchingTo *time.Time) *MemChunk {
	chk := NewMemChunk(ChunkFormatV3, EncGZIP, DefaultTestHeadBlockFmt, defaultBlockSize, 0)
	t.Logf("from   : %v", from.String())
	t.Logf("through: %v", through.String())
	for from.Before(through) {
		// If a line is between matchingFrom and matchingTo add the prefix "matching"
		if matchingFrom != nil && matchingTo != nil &&
			(from.Equal(*matchingFrom) || (from.After(*matchingFrom) && (from.Before(*matchingTo)))) {
			t.Logf("%v matching line", from.String())
			err := chk.Append(&logproto.Entry{
				Line:      fmt.Sprintf("matching %v", from.String()),
				Timestamp: from,
			})
			require.NoError(t, err)
		} else {
			t.Logf("%v non-match line", from.String())
			err := chk.Append(&logproto.Entry{
				Line:      from.String(),
				Timestamp: from,
			})
			require.NoError(t, err)
		}
		from = from.Add(time.Second)
	}

	return chk
}

func TestMemChunk_SpaceFor(t *testing.T) {
	for _, tc := range []struct {
		desc string

		nBlocks      int
		targetSize   int
		headSize     int
		cutBlockSize int
		entry        logproto.Entry

		expect     bool
		expectFunc func(chunkFormat byte, headFmt HeadBlockFmt) bool
	}{
		{
			desc:    "targetSize not defined",
			nBlocks: blocksPerChunk - 1,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      "a",
			},
			expect: true,
		},
		{
			desc:    "targetSize not defined and too many blocks",
			nBlocks: blocksPerChunk + 1,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      "a",
			},
			expect: false,
		},
		{
			desc:         "head too big",
			targetSize:   10,
			headSize:     100,
			cutBlockSize: 0,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      "a",
			},
			expect: false,
		},
		{
			desc:         "cut blocks too big",
			targetSize:   10,
			headSize:     0,
			cutBlockSize: 100,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      "a",
			},
			expect: false,
		},
		{
			desc:         "entry fits",
			targetSize:   10,
			headSize:     0,
			cutBlockSize: 0,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      strings.Repeat("a", 9),
			},
			expect: true,
		},
		{
			desc:         "entry fits with non-indexed labels",
			targetSize:   10,
			headSize:     0,
			cutBlockSize: 0,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      strings.Repeat("a", 2),
				NonIndexedLabels: []logproto.LabelAdapter{
					{Name: "foo", Value: strings.Repeat("a", 2)},
				},
			},
			expect: true,
		},
		{
			desc:         "entry too big",
			targetSize:   10,
			headSize:     0,
			cutBlockSize: 0,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      strings.Repeat("a", 100),
			},
			expect: false,
		},
		{
			desc:         "entry too big because non-indexed labels",
			targetSize:   10,
			headSize:     0,
			cutBlockSize: 0,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      strings.Repeat("a", 5),
				NonIndexedLabels: []logproto.LabelAdapter{
					{Name: "foo", Value: strings.Repeat("a", 5)},
				},
			},

			expectFunc: func(chunkFormat byte, _ HeadBlockFmt) bool {
				// Succeed unless we're using chunk format v4, which should
				// take the non-indexed labels into account.
				return chunkFormat < ChunkFormatV4
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			for _, format := range allPossibleFormats {
				t.Run(fmt.Sprintf("chunk_v%d_head_%s", format.chunkFormat, format.headBlockFmt), func(t *testing.T) {
					chk := newMemChunkWithFormat(format.chunkFormat, EncNone, format.headBlockFmt, 1024, tc.targetSize)

					chk.blocks = make([]block, tc.nBlocks)
					chk.cutBlockSize = tc.cutBlockSize
					for i := 0; i < tc.headSize; i++ {
						require.NoError(t, chk.head.Append(int64(i), "a", nil))
					}

					expect := tc.expect
					if tc.expectFunc != nil {
						expect = tc.expectFunc(format.chunkFormat, format.headBlockFmt)
					}

					require.Equal(t, expect, chk.SpaceFor(&tc.entry))
				})
			}

		})
	}
}

func TestMemChunk_IteratorWithNonIndexedLabels(t *testing.T) {
	for _, enc := range testEncoding {
		enc := enc
		t.Run(enc.String(), func(t *testing.T) {
			streamLabels := labels.Labels{
				{Name: "job", Value: "fake"},
			}
			chk := newMemChunkWithFormat(ChunkFormatV4, enc, UnorderedWithNonIndexedLabelsHeadBlockFmt, testBlockSize, testTargetSize)
			require.NoError(t, chk.Append(logprotoEntryWithNonIndexedLabels(1, "lineA", []logproto.LabelAdapter{
				{Name: "traceID", Value: "123"},
				{Name: "user", Value: "a"},
			})))
			require.NoError(t, chk.Append(logprotoEntryWithNonIndexedLabels(2, "lineB", []logproto.LabelAdapter{
				{Name: "traceID", Value: "456"},
				{Name: "user", Value: "b"},
			})))
			require.NoError(t, chk.cut())
			require.NoError(t, chk.Append(logprotoEntryWithNonIndexedLabels(3, "lineC", []logproto.LabelAdapter{
				{Name: "traceID", Value: "789"},
				{Name: "user", Value: "c"},
			})))
			require.NoError(t, chk.Append(logprotoEntryWithNonIndexedLabels(4, "lineD", []logproto.LabelAdapter{
				{Name: "traceID", Value: "123"},
				{Name: "user", Value: "d"},
			})))

			// The expected bytes is the sum of bytes decompressed and bytes read from the head chunk.
			// First we add the bytes read from the store (aka decompressed). That's
			// nonIndexedLabelsBytes = n. lines * (n. labels <int> + (2 * n. nonIndexedLabelsSymbols * symbol <int>))
			// lineBytes = n. lines * (ts <int> + line length <int> + line)
			expectedNonIndexedLabelsBytes := 2 * (binary.MaxVarintLen64 + (2 * 2 * binary.MaxVarintLen64))
			lineBytes := 2 * (2*binary.MaxVarintLen64 + len("lineA"))
			// Now we add the bytes read from the head chunk. That's
			// nonIndexedLabelsBytes = n. lines * (2 * n. nonIndexedLabelsSymbols * symbol <uint32>)
			// lineBytes = n. lines * (line)
			expectedNonIndexedLabelsBytes += 2 * (2 * 2 * 4)
			lineBytes += 2 * (len("lineC"))
			// Finally, the expected total bytes is the line bytes + non-indexed labels bytes
			expectedBytes := lineBytes + expectedNonIndexedLabelsBytes

			for _, tc := range []struct {
				name            string
				query           string
				expectedLines   []string
				expectedStreams []string
			}{
				{
					name:          "no-filter",
					query:         `{job="fake"}`,
					expectedLines: []string{"lineA", "lineB", "lineC", "lineD"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "traceID", "123", "user", "a").String(),
						labels.FromStrings("job", "fake", "traceID", "456", "user", "b").String(),
						labels.FromStrings("job", "fake", "traceID", "789", "user", "c").String(),
						labels.FromStrings("job", "fake", "traceID", "123", "user", "d").String(),
					},
				},
				{
					name:          "filter",
					query:         `{job="fake"} | traceID="789"`,
					expectedLines: []string{"lineC"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "traceID", "789", "user", "c").String(),
					},
				},
				{
					name:          "filter-regex-or",
					query:         `{job="fake"} | traceID=~"456|789"`,
					expectedLines: []string{"lineB", "lineC"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "traceID", "456", "user", "b").String(),
						labels.FromStrings("job", "fake", "traceID", "789", "user", "c").String(),
					},
				},
				{
					name:          "filter-regex-contains",
					query:         `{job="fake"} | traceID=~".*5.*"`,
					expectedLines: []string{"lineB"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "traceID", "456", "user", "b").String(),
					},
				},
				{
					name:          "filter-regex-complex",
					query:         `{job="fake"} | traceID=~"^[0-9]2.*"`,
					expectedLines: []string{"lineA", "lineD"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "traceID", "123", "user", "a").String(),
						labels.FromStrings("job", "fake", "traceID", "123", "user", "d").String(),
					},
				},
				{
					name:          "multiple-filters",
					query:         `{job="fake"} | traceID="123" | user="d"`,
					expectedLines: []string{"lineD"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "traceID", "123", "user", "d").String(),
					},
				},
				{
					name:          "keep",
					query:         `{job="fake"} | keep job, user`,
					expectedLines: []string{"lineA", "lineB", "lineC", "lineD"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "user", "a").String(),
						labels.FromStrings("job", "fake", "user", "b").String(),
						labels.FromStrings("job", "fake", "user", "c").String(),
						labels.FromStrings("job", "fake", "user", "d").String(),
					},
				},
				{
					name:          "keep-filter",
					query:         `{job="fake"} | keep job, user="b"`,
					expectedLines: []string{"lineA", "lineB", "lineC", "lineD"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake").String(),
						labels.FromStrings("job", "fake", "user", "b").String(),
						labels.FromStrings("job", "fake").String(),
						labels.FromStrings("job", "fake").String(),
					},
				},
				{
					name:          "drop",
					query:         `{job="fake"} | drop traceID`,
					expectedLines: []string{"lineA", "lineB", "lineC", "lineD"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "user", "a").String(),
						labels.FromStrings("job", "fake", "user", "b").String(),
						labels.FromStrings("job", "fake", "user", "c").String(),
						labels.FromStrings("job", "fake", "user", "d").String(),
					},
				},
				{
					name:          "drop-filter",
					query:         `{job="fake"} | drop traceID="123"`,
					expectedLines: []string{"lineA", "lineB", "lineC", "lineD"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "user", "a").String(),
						labels.FromStrings("job", "fake", "traceID", "456", "user", "b").String(),
						labels.FromStrings("job", "fake", "traceID", "789", "user", "c").String(),
						labels.FromStrings("job", "fake", "user", "d").String(),
					},
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Run("log", func(t *testing.T) {
						expr, err := syntax.ParseLogSelector(tc.query, true)
						require.NoError(t, err)

						pipeline, err := expr.Pipeline()
						require.NoError(t, err)

						// We will run the test twice so the iterator will be created twice.
						// This is to ensure that the iterator is correctly closed.
						for i := 0; i < 2; i++ {
							sts, ctx := stats.NewContext(context.Background())
							it, err := chk.Iterator(ctx, time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, pipeline.ForStream(streamLabels))
							require.NoError(t, err)

							var lines []string
							var streams []string
							for it.Next() {
								require.NoError(t, it.Error())
								e := it.Entry()
								lines = append(lines, e.Line)
								streams = append(streams, it.Labels())

								// We don't want to send back the non-indexed labels since
								// they are already part of the returned labels.
								require.Empty(t, e.NonIndexedLabels)
							}
							assert.ElementsMatch(t, tc.expectedLines, lines)
							assert.ElementsMatch(t, tc.expectedStreams, streams)

							resultStats := sts.Result(0, 0, len(lines))
							require.Equal(t, int64(expectedBytes), resultStats.Summary.TotalBytesProcessed)
							require.Equal(t, int64(expectedNonIndexedLabelsBytes), resultStats.Summary.TotalNonIndexedLabelsBytesProcessed)
						}
					})

					t.Run("metric", func(t *testing.T) {
						query := fmt.Sprintf(`count_over_time(%s [1d])`, tc.query)
						expr, err := syntax.ParseSampleExpr(query)
						require.NoError(t, err)

						extractor, err := expr.Extractor()
						require.NoError(t, err)

						// We will run the test twice so the iterator will be created twice.
						// This is to ensure that the iterator is correctly closed.
						for i := 0; i < 2; i++ {
							sts, ctx := stats.NewContext(context.Background())
							it := chk.SampleIterator(ctx, time.Unix(0, 0), time.Unix(0, math.MaxInt64), extractor.ForStream(streamLabels))

							var sumValues int
							var streams []string
							for it.Next() {
								require.NoError(t, it.Error())
								e := it.Sample()
								sumValues += int(e.Value)
								streams = append(streams, it.Labels())
							}
							require.Equal(t, len(tc.expectedLines), sumValues)
							assert.ElementsMatch(t, tc.expectedStreams, streams)

							resultStats := sts.Result(0, 0, 0)
							require.Equal(t, int64(expectedBytes), resultStats.Summary.TotalBytesProcessed)
							require.Equal(t, int64(expectedNonIndexedLabelsBytes), resultStats.Summary.TotalNonIndexedLabelsBytesProcessed)
						}
					})
				})
			}
		})
	}
}

func TestMemChunk_IteratorOptions(t *testing.T) {
	chk := newMemChunkWithFormat(ChunkFormatV4, EncNone, UnorderedWithNonIndexedLabelsHeadBlockFmt, testBlockSize, testTargetSize)
	require.NoError(t, chk.Append(logprotoEntryWithNonIndexedLabels(0, "0", logproto.FromLabelsToLabelAdapters(
		labels.FromStrings("a", "0"),
	))))
	require.NoError(t, chk.Append(logprotoEntryWithNonIndexedLabels(1, "1", logproto.FromLabelsToLabelAdapters(
		labels.FromStrings("a", "1"),
	))))
	require.NoError(t, chk.cut())
	require.NoError(t, chk.Append(logprotoEntryWithNonIndexedLabels(2, "2", logproto.FromLabelsToLabelAdapters(
		labels.FromStrings("a", "2"),
	))))
	require.NoError(t, chk.Append(logprotoEntryWithNonIndexedLabels(3, "3", logproto.FromLabelsToLabelAdapters(
		labels.FromStrings("a", "3"),
	))))

	for _, tc := range []struct {
		name                   string
		options                []iter.EntryIteratorOption
		expectNonIndexedLabels bool
	}{
		{
			name:                   "No options",
			expectNonIndexedLabels: false,
		},
		{
			name: "WithKeepNonIndexedLabels",
			options: []iter.EntryIteratorOption{
				iter.WithKeepNonIndexedLabels(),
			},

			expectNonIndexedLabels: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, err := chk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline, tc.options...)
			require.NoError(t, err)

			var idx int64
			for it.Next() {
				expectedLabels := labels.FromStrings("a", fmt.Sprintf("%d", idx))
				expectedEntry := logproto.Entry{
					Timestamp: time.Unix(0, idx),
					Line:      fmt.Sprintf("%d", idx),
				}

				if tc.expectNonIndexedLabels {
					expectedEntry.NonIndexedLabels = logproto.FromLabelsToLabelAdapters(expectedLabels)
				}

				require.Equal(t, expectedEntry, it.Entry())
				require.Equal(t, expectedLabels.String(), it.Labels())
				idx++
			}
		})
	}
}
