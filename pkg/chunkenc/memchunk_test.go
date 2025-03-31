package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash"
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

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/chunkenc/testdata"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/filter"
)

var testEncodings = []compression.Codec{
	compression.None,
	compression.GZIP,
	compression.LZ4_64k,
	compression.LZ4_256k,
	compression.LZ4_1M,
	compression.LZ4_4M,
	compression.Snappy,
	compression.Flate,
	compression.Zstd,
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
	bytesExtractor = func() log.StreamSampleExtractor {
		ex, err := log.NewLineSampleExtractor(log.BytesExtractor, nil, nil, false, false)
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
			headBlockFmt: UnorderedWithStructuredMetadataHeadBlockFmt,
			chunkFormat:  ChunkFormatV4,
		},
	}
)

const (
	DefaultTestHeadBlockFmt = UnorderedWithStructuredMetadataHeadBlockFmt
	lblPing                 = "ping"
	lblPong                 = "pong"
)

func TestBlocksInclusive(t *testing.T) {
	for _, enc := range testEncodings {
		for _, format := range allPossibleFormats {
			chunkfmt, headfmt := format.chunkFormat, format.headBlockFmt
			chk := NewMemChunk(chunkfmt, enc, headfmt, testBlockSize, testTargetSize)
			dup, err := chk.Append(logprotoEntry(1, "1"))
			require.False(t, dup)
			require.Nil(t, err)
			err = chk.cut()
			require.Nil(t, err)

			blocks := chk.Blocks(time.Unix(0, 1), time.Unix(0, 1))
			require.Equal(t, 1, len(blocks))
			require.Equal(t, 1, blocks[0].Entries())
		}
	}
}

func TestBlock(t *testing.T) {
	for _, enc := range testEncodings {
		for _, format := range allPossibleFormats {
			chunkFormat, headBlockFmt := format.chunkFormat, format.headBlockFmt
			t.Run(fmt.Sprintf("encoding:%v chunkFormat:%v headBlockFmt:%v", enc, chunkFormat, headBlockFmt), func(t *testing.T) {
				t.Parallel()
				chk := newMemChunkWithFormat(chunkFormat, enc, headBlockFmt, testBlockSize, testTargetSize)
				cases := []struct {
					ts    int64
					str   string
					bytes float64
					lbs   []logproto.LabelAdapter
					cut   bool
				}{
					{
						ts:    1,
						str:   "hello, world!",
						bytes: float64(len("hello, world!")),
					},
					{
						ts:    2,
						str:   "hello, world2!",
						bytes: float64(len("hello, world2!")),
						lbs: []logproto.LabelAdapter{
							{Name: "app", Value: "myapp"},
						},
					},
					{
						ts:    3,
						str:   "hello, world3!",
						bytes: float64(len("hello, world3!")),
						lbs: []logproto.LabelAdapter{
							{Name: "a", Value: "a"},
							{Name: "b", Value: "b"},
						},
					},
					{
						ts:    4,
						str:   "hello, world4!",
						bytes: float64(len("hello, world4!")),
					},
					{
						ts:    5,
						str:   "hello, world5!",
						bytes: float64(len("hello, world5!")),
					},
					{
						ts:    6,
						str:   "hello, world6!",
						bytes: float64(len("hello, world6!")),
						cut:   true,
					},
					{
						ts:    7,
						str:   "hello, world7!",
						bytes: float64(len("hello, world7!")),
					},
					{
						ts:    8,
						str:   "hello, worl\nd8!",
						bytes: float64(len("hello, worl\nd8!")),
					},
					{
						ts:    8,
						str:   "hello, world 8, 2!",
						bytes: float64(len("hello, world 8, 2!")),
					},
					{
						ts:    8,
						str:   "hello, world 8, 3!",
						bytes: float64(len("hello, world 8, 3!")),
					},
					{
						ts:    9,
						str:   "",
						bytes: float64(len("")),
					},
					{
						ts:    10,
						str:   "hello, world10!",
						bytes: float64(len("hello, world10!")),
						lbs: []logproto.LabelAdapter{
							{Name: "a", Value: "a2"},
							{Name: "b", Value: "b"},
						},
					},
				}

				for _, c := range cases {
					dup, err := chk.Append(logprotoEntryWithStructuredMetadata(c.ts, c.str, c.lbs))
					require.False(t, dup)
					require.NoError(t, err)
					if c.cut {
						require.NoError(t, chk.cut())
					}
				}

				noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})

				it, err := chk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline)
				require.NoError(t, err)

				idx := 0
				for it.Next() {
					e := it.At()
					require.Equal(t, cases[idx].ts, e.Timestamp.UnixNano())
					require.Equal(t, cases[idx].str, e.Line)
					if chunkFormat < ChunkFormatV4 {
						require.Equal(t, labels.EmptyLabels().String(), it.Labels())
						require.Empty(t, e.StructuredMetadata)
					} else {
						if len(cases[idx].lbs) > 0 {
							require.Equal(t, push.LabelsAdapter(cases[idx].lbs), e.StructuredMetadata)
						}

						expectedLabels := logproto.FromLabelAdaptersToLabels(cases[idx].lbs).String()
						require.Equal(t, expectedLabels, it.Labels())
					}
					idx++
				}

				require.NoError(t, it.Err())
				require.NoError(t, it.Close())
				require.Equal(t, len(cases), idx)

				sampleIt := chk.SampleIterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), countExtractor)
				idx = 0
				for sampleIt.Next() {
					s := sampleIt.At()
					require.Equal(t, cases[idx].ts, s.Timestamp)
					require.Equal(t, 1., s.Value)
					require.NotEmpty(t, s.Hash)
					idx++
				}

				require.NoError(t, sampleIt.Err())
				require.NoError(t, sampleIt.Close())
				require.Equal(t, len(cases), idx)
				t.Run("multi-extractor", func(t *testing.T) {
					// Wrap extractors in variant extractors so they get a variant index we can use later for differentiating counts and bytes
					extractors := []log.StreamSampleExtractor{
						log.NewVariantsStreamSampleExtractorWrapper(0, countExtractor),
						log.NewVariantsStreamSampleExtractorWrapper(1, bytesExtractor),
					}
					sampleIt = chk.SampleIterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), extractors...)
					idx = 0

					// variadic arguments can't guarantee order, so we're going to store the expected and actual values
					// and do an ElementsMatch on them.
					var actualCounts = make([]float64, 0, len(cases))
					var actualBytes = make([]float64, 0, len(cases))

					var expectedCounts = make([]float64, 0, len(cases))
					var expectedBytes = make([]float64, 0, len(cases))
					for _, c := range cases {
						expectedCounts = append(expectedCounts, 1.)
						expectedBytes = append(expectedBytes, c.bytes)
					}

					// 2 extractors, expect 2 samples per original timestamp
					for sampleIt.Next() {
						s := sampleIt.At()
						require.Equal(t, cases[idx].ts, s.Timestamp)
						require.NotEmpty(t, s.Hash)
						lbls := sampleIt.Labels()
						if strings.Contains(lbls, `__variant__="0"`) {
							actualCounts = append(actualCounts, s.Value)
						} else {
							actualBytes = append(actualBytes, s.Value)
						}

						require.True(t, sampleIt.Next())
						s = sampleIt.At()
						require.Equal(t, cases[idx].ts, s.Timestamp)
						require.NotEmpty(t, s.Hash)
						lbls = sampleIt.Labels()
						if strings.Contains(lbls, `__variant__="0"`) {
							actualCounts = append(actualCounts, s.Value)
						} else {
							actualBytes = append(actualBytes, s.Value)
						}

						idx++
					}

					require.ElementsMatch(t, expectedCounts, actualCounts)
					require.ElementsMatch(t, expectedBytes, actualBytes)

					require.NoError(t, sampleIt.Err())
					require.NoError(t, sampleIt.Close())
					require.Equal(t, len(cases), idx)
				})

				t.Run("bounded-iteration", func(t *testing.T) {
					it, err := chk.Iterator(context.Background(), time.Unix(0, 3), time.Unix(0, 7), logproto.FORWARD, noopStreamPipeline)
					require.NoError(t, err)

					idx := 2
					for it.Next() {
						e := it.At()
						require.Equal(t, cases[idx].ts, e.Timestamp.UnixNano())
						require.Equal(t, cases[idx].str, e.Line)
						idx++
					}
					require.NoError(t, it.Err())
					require.Equal(t, 6, idx)
				})
			})

		}
	}
}

func TestCorruptChunk(t *testing.T) {
	for _, enc := range testEncodings {
		for _, format := range allPossibleFormats {
			chunkfmt, headfmt := format.chunkFormat, format.headBlockFmt

			t.Run(enc.String(), func(t *testing.T) {
				t.Parallel()

				chk := NewMemChunk(chunkfmt, enc, headfmt, testBlockSize, testTargetSize)
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
					noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
					it, err := chk.Iterator(ctx, start, end, logproto.FORWARD, noopStreamPipeline)
					require.NoError(t, err, "case %d", i)

					idx := 0
					for it.Next() {
						idx++
					}
					require.Error(t, it.Err(), "case %d", i)
					require.NoError(t, it.Close())
				}
			})
		}
	}
}

func TestReadFormatV1(t *testing.T) {
	t.Parallel()

	c := NewMemChunk(ChunkFormatV3, compression.GZIP, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize)
	fillChunk(c)
	// overrides to v1 for testing that specific version.
	c.format = ChunkFormatV1

	b, err := c.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	r, err := NewByteChunk(b, testBlockSize, testTargetSize)
	if err != nil {
		t.Fatal(err)
	}

	noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
	it, err := r.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline)
	if err != nil {
		t.Fatal(err)
	}

	i := int64(0)
	for it.Next() {
		require.Equal(t, i, it.At().Timestamp.UnixNano())
		require.Equal(t, testdata.LogString(i), it.At().Line)

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
		for _, enc := range testEncodings {
			t.Run(testNameWithFormats(enc, testData.chunkFormat, testData.headBlockFmt), func(t *testing.T) {
				t.Parallel()

				c := newMemChunkWithFormat(testData.chunkFormat, enc, testData.headBlockFmt, testBlockSize, testTargetSize)
				populated := fillChunk(c)

				assertLines := func(c *MemChunk) {
					require.Equal(t, enc, c.Encoding())
					noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
					it, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline)
					if err != nil {
						t.Fatal(err)
					}

					i := int64(0)
					var data int64
					for it.Next() {
						require.Equal(t, i, it.At().Timestamp.UnixNano())
						require.Equal(t, testdata.LogString(i), it.At().Line)

						data += int64(len(it.At().Line))
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

func testNameWithFormats(enc compression.Codec, chunkFormat byte, headBlockFmt HeadBlockFmt) string {
	return fmt.Sprintf("encoding:%v chunkFormat:%v headBlockFmt:%v", enc, chunkFormat, headBlockFmt)
}

func TestRoundtripV3(t *testing.T) {
	for _, enc := range testEncodings {
		for _, format := range allPossibleFormats {
			chunkfmt, headfmt := format.chunkFormat, format.headBlockFmt
			t.Run(fmt.Sprintf("%v-%v", format, enc), func(t *testing.T) {
				t.Parallel()

				c := NewMemChunk(chunkfmt, enc, headfmt, testBlockSize, testTargetSize)
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
		for _, enc := range testEncodings {
			// run tests with and without structured metadata since it is optional
			for _, appendWithStructuredMetadata := range []bool{false, true} {
				testName := testNameWithFormats(enc, testData.chunkFormat, testData.headBlockFmt)
				if appendWithStructuredMetadata {
					testName = fmt.Sprintf("%s - append structured metadata", testName)
				} else {
					testName = fmt.Sprintf("%s - without structured metadata", testName)
				}
				t.Run(testName, func(t *testing.T) {
					t.Parallel()

					chk := NewMemChunk(testData.chunkFormat, enc, testData.headBlockFmt, testBlockSize, testTargetSize)
					chk.format = testData.chunkFormat
					numSamples := 50000
					var entry *logproto.Entry

					for i := 0; i < numSamples; i++ {
						entry = logprotoEntry(int64(i), strconv.Itoa(i))
						if appendWithStructuredMetadata {
							entry.StructuredMetadata = []logproto.LabelAdapter{{Name: "foo", Value: strconv.Itoa(i)}}
						}
						dup, err := chk.Append(entry)
						require.False(t, dup)
						require.NoError(t, err)
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

						e := it.At()
						require.Equal(t, int64(i), e.Timestamp.UnixNano())
						require.Equal(t, strconv.Itoa(i), e.Line)
						if appendWithStructuredMetadata && testData.chunkFormat >= ChunkFormatV4 {
							require.Equal(t, labels.FromStrings("foo", strconv.Itoa(i)).String(), it.Labels())
							require.Equal(t, labels.FromStrings("foo", strconv.Itoa(i)), logproto.FromLabelAdaptersToLabels(e.StructuredMetadata))
						} else {
							require.Equal(t, labels.EmptyLabels().String(), it.Labels())
							require.Nil(t, e.StructuredMetadata)
						}
					}
					require.NoError(t, it.Err())

					countExtractor := func() log.StreamSampleExtractor {
						ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
						if err != nil {
							panic(err)
						}
						return ex.ForStream(labels.Labels{})
					}()
					extractors := []log.StreamSampleExtractor{countExtractor, countExtractor}

					sampleIt := bc.SampleIterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), extractors...)
					for i := 0; i < numSamples; i++ {
						require.True(t, sampleIt.Next(), i)

						s := sampleIt.At()
						require.Equal(t, int64(i), s.Timestamp)
						require.Equal(t, 1., s.Value)
						if appendWithStructuredMetadata && testData.chunkFormat >= ChunkFormatV4 {
							require.Equal(t, labels.FromStrings("foo", strconv.Itoa(i)).String(), sampleIt.Labels())
						} else {
							require.Equal(t, labels.EmptyLabels().String(), sampleIt.Labels())
						}

						// check that the second extractor is returning samples as well
						require.True(t, sampleIt.Next())
						s = sampleIt.At()
						require.Equal(t, int64(i), s.Timestamp)
						require.Equal(t, 1., s.Value)
					}
					require.NoError(t, sampleIt.Err())

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
		for _, enc := range testEncodings {
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
					dup, err := chk.Append(entry)
					require.False(t, dup)
					require.NoError(t, err)
				}

				require.Equal(t, int64(lines), i)

				noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
				it, err := chk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, 100), logproto.FORWARD, noopStreamPipeline)
				require.NoError(t, err)
				i = 0
				for it.Next() {
					entry := it.At()
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

	chk := NewMemChunk(ChunkFormatV3, compression.GZIP, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize)

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
		dup, err := chk.Append(entry)
		require.False(t, dup)
		require.NoError(t, err)
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
			dup, err := chk.Append(logprotoEntry(5, "test"))
			assert.False(t, dup)
			assert.NoError(t, err)
			dup, err = chk.Append(logprotoEntry(6, "test"))
			assert.False(t, dup)
			assert.NoError(t, err)

			if chk.headFmt == OrderedHeadBlockFmt {
				dup, err = chk.Append(logprotoEntry(1, "test"))
				assert.EqualError(t, err, ErrOutOfOrder.Error())
				assert.False(t, dup)
			} else {
				dup, err = chk.Append(logprotoEntry(1, "test"))
				assert.False(t, dup)
				assert.NoError(t, err)
			}
		},
		"append out of order in a new block right after cutting the previous one": func(t *testing.T, chk *MemChunk) {
			dup, err := chk.Append(logprotoEntry(5, "test"))
			assert.False(t, dup)
			assert.NoError(t, err)
			dup, err = chk.Append(logprotoEntry(6, "test"))
			assert.False(t, dup)
			assert.NoError(t, err)
			assert.NoError(t, chk.cut())

			if chk.headFmt == OrderedHeadBlockFmt {
				dup, err = chk.Append(logprotoEntry(1, "test"))
				assert.False(t, dup)
				assert.EqualError(t, err, ErrOutOfOrder.Error())
			} else {
				dup, err = chk.Append(logprotoEntry(1, "test"))
				assert.False(t, dup)
				assert.NoError(t, err)
			}
		},
		"append out of order in a new block after multiple cuts": func(t *testing.T, chk *MemChunk) {
			dup, err := chk.Append(logprotoEntry(5, "test"))
			assert.False(t, dup)
			assert.NoError(t, err)
			assert.NoError(t, chk.cut())

			dup, err = chk.Append(logprotoEntry(6, "test"))
			assert.False(t, dup)
			assert.NoError(t, err)
			assert.NoError(t, chk.cut())

			if chk.headFmt == OrderedHeadBlockFmt {
				dup, err = chk.Append(logprotoEntry(1, "test"))
				assert.False(t, dup)
				assert.EqualError(t, err, ErrOutOfOrder.Error())
			} else {
				dup, err = chk.Append(logprotoEntry(1, "test"))
				assert.False(t, dup)
				assert.NoError(t, err)
			}
		},
	}

	for _, f := range HeadBlockFmts {
		for testName, tester := range tests {
			t.Run(testName, func(t *testing.T) {
				t.Parallel()

				tester(t, NewMemChunk(ChunkFormatV3, compression.GZIP, f, testBlockSize, testTargetSize))
			})
		}
	}
}

func BenchmarkEncodingsAndChunkSize(b *testing.B) {
	type res struct {
		name           string
		count          uint64
		size           uint64
		compressedSize uint64
		ratio          float64
	}
	var result []res

	resBuffer := make([]byte, 0, 50*1024*1024)
	for _, enc := range testEncodings {
		for _, bs := range testBlockSizes {
			for fi, f := range allPossibleFormats {
				name := fmt.Sprintf("%s_block_size_%s_format_%d", enc.String(), humanize.Bytes(uint64(bs)), fi)
				b.Run(name, func(b *testing.B) {
					var insertedTotal, compressedTotal, count uint64
					for range b.N {
						c := newMemChunkWithFormat(f.chunkFormat, enc, f.headBlockFmt, bs, testTargetSize)
						inserted := fillChunk(c)
						insertedTotal += uint64(inserted)
						cb, err := c.BytesWith(resBuffer)
						if err != nil {
							b.Fatal(err)
						}
						compressedTotal += uint64(len(cb))
						count++
					}

					averageRatio := float64(insertedTotal) / float64(compressedTotal)
					result = append(result, res{
						name:           name,
						count:          count,
						size:           insertedTotal,
						compressedSize: compressedTotal,
						ratio:          averageRatio,
					})
					b.ReportMetric(averageRatio, "compression_ratio")
					b.ReportMetric(float64(insertedTotal)/float64(count*1024), "avg_size_kb")
					b.ReportMetric(float64(compressedTotal)/float64(count*1024), "avg_compressed_size_kb")
				})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ratio > result[j].ratio
	})
	fmt.Printf("%s\t%s\t%s\t%s\t%s\n", "name", "count", "uncompressed", "compressed", "ratio")
	for _, r := range result {
		fmt.Printf("%s\t(count %d)\n%s\t%s\t%f\n", r.name, r.count, humanize.Bytes(r.size/r.count), humanize.Bytes(r.compressedSize/r.count), r.ratio)
	}
}

func TestChunkStats(t *testing.T) {
	c := NewMemChunk(ChunkFormatV4, compression.Snappy, DefaultTestHeadBlockFmt, testBlockSize, 0)
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
		if _, err := c.Append(entry); err != nil {
			t.Fatal(err)
		}
		inserted++
		entry.Timestamp = entry.Timestamp.Add(time.Nanosecond)
	}
	// For each entry: timestamp <varint>, line size <varint>, line <bytes>, num of labels in structured metadata <varint>
	expectedSize := inserted * (len(entry.Line) + 3*binary.MaxVarintLen64)
	statsCtx, ctx := stats.NewContext(context.Background())

	noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
	it, err := c.Iterator(ctx, first.Add(-time.Hour), entry.Timestamp.Add(time.Hour), logproto.BACKWARD, noopStreamPipeline)
	if err != nil {
		t.Fatal(err)
	}
	//nolint:revive
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
	//nolint:revive
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
		for _, enc := range testEncodings {
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
							_ = iter.At()
						}
						if err := iter.Close(); err != nil {
							t.Fatal(err)
						}
					},
					func(iter iter.EntryIterator, t *testing.T) {
						// close after a single iteration
						iter.Next()
						_ = iter.At()
						if err := iter.Close(); err != nil {
							t.Fatal(err)
						}
					},
				} {
					c := newMemChunkWithFormat(f.chunkFormat, enc, f.headBlockFmt, testBlockSize, testTargetSize)
					inserted := fillChunk(c)
					noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
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
		for _, enc := range testEncodings {
			for _, withStructuredMetadata := range []bool{false, true} {
				name := fmt.Sprintf("%v-%v", f, enc)
				if withStructuredMetadata {
					name += "-withStructuredMetadata"
				}
				b.Run(name, func(b *testing.B) {
					uncompressedBytes, compressedBytes := 0, 0
					for n := 0; n < b.N; n++ {
						c := NewMemChunk(ChunkFormatV3, enc, f, testBlockSize, testTargetSize)
						// adds until full so we trigger cut which serialize using gzip
						for c.SpaceFor(entry) {
							_, _ = c.Append(entry)
							entry.Timestamp = time.Unix(0, i)
							entry.Line = testdata.LogString(i)
							if withStructuredMetadata {
								entry.StructuredMetadata = []logproto.LabelAdapter{
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

func (nomatchPipeline) ReferencedStructuredMetadata() bool {
	return false
}

func BenchmarkRead(b *testing.B) {
	for _, bs := range testBlockSizes {
		for _, enc := range testEncodings {
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
							_ = iterator.At()
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
		for _, enc := range testEncodings {
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
							_ = iterator.At()
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

type noopTestPipeline struct{}

func (noopTestPipeline) BaseLabels() log.LabelsResult { return log.EmptyLabelsResult }
func (noopTestPipeline) Process(_ int64, line []byte, _ ...labels.Label) ([]byte, log.LabelsResult, bool) {
	return line, nil, false
}

func (noopTestPipeline) ProcessString(_ int64, line string, _ ...labels.Label) (string, log.LabelsResult, bool) {
	return line, nil, false
}

func (noopTestPipeline) ReferencedStructuredMetadata() bool {
	return false
}

func BenchmarkBackwardIterator(b *testing.B) {
	for _, bs := range testBlockSizes {
		b.Run(humanize.Bytes(uint64(bs)), func(b *testing.B) {
			b.ReportAllocs()
			c := NewMemChunk(ChunkFormatV4, compression.Snappy, DefaultTestHeadBlockFmt, bs, testTargetSize)
			_ = fillChunk(c)
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				noop := noopTestPipeline{}
				iterator, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Now(), logproto.BACKWARD, noop)
				if err != nil {
					panic(err)
				}
				for iterator.Next() {
					_ = iterator.At()
				}
				if err := iterator.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestGenerateDataSize(t *testing.T) {
	for _, enc := range testEncodings {
		t.Run(enc.String(), func(t *testing.T) {
			chunks, size := generateData(enc, 50, testBlockSize, testTargetSize)

			bytesRead := uint64(0)
			for _, c := range chunks {
				noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
				// use forward iterator for benchmark -- backward iterator does extra allocations by keeping entries in memory
				iterator, err := c.Iterator(context.TODO(), time.Unix(0, 0), time.Now(), logproto.FORWARD, noopStreamPipeline)
				if err != nil {
					panic(err)
				}
				for iterator.Next() {
					e := iterator.At()
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
		for _, withStructuredMetadata := range []bool{false, true} {
			b.Run(fmt.Sprintf("size=%d structuredMetadata=%v", j, withStructuredMetadata), func(b *testing.B) {
				h := headBlock{}

				var structuredMetadata labels.Labels
				if withStructuredMetadata {
					structuredMetadata = labels.Labels{{Name: "foo", Value: "foo"}}
				}

				for i := 0; i < j; i++ {
					if _, err := h.Append(int64(i), "this is the append string", structuredMetadata); err != nil {
						b.Fatal(err)
					}
				}

				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
					iter := h.Iterator(context.Background(), logproto.BACKWARD, 0, math.MaxInt64, noopStreamPipeline)

					for iter.Next() {
						_ = iter.At()
					}
				}
			})
		}
	}
}

func BenchmarkHeadBlockSampleIterator(b *testing.B) {
	for _, j := range []int{20000, 10000, 8000, 5000} {
		for _, withStructuredMetadata := range []bool{false, true} {
			b.Run(fmt.Sprintf("size=%d structuredMetadata=%v", j, withStructuredMetadata), func(b *testing.B) {
				h := headBlock{}

				var structuredMetadata labels.Labels
				if withStructuredMetadata {
					structuredMetadata = labels.Labels{{Name: "foo", Value: "foo"}}
				}

				for i := 0; i < j; i++ {
					if _, err := h.Append(int64(i), "this is the append string", structuredMetadata); err != nil {
						b.Fatal(err)
					}
				}

				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					iter := h.SampleIterator(context.Background(), 0, math.MaxInt64, countExtractor)

					for iter.Next() {
						_ = iter.At()
					}
					iter.Close()
				}
			})
		}
	}
}

func BenchmarkHeadBlockSampleIterator_WithMultipleExtractors(b *testing.B) {
	for _, j := range []int{20000, 10000, 8000, 5000} {
		for _, withStructuredMetadata := range []bool{false, true} {
			b.Run(fmt.Sprintf("size=%d structuredMetadata=%v", j, withStructuredMetadata), func(b *testing.B) {
				h := headBlock{}

				var structuredMetadata labels.Labels
				if withStructuredMetadata {
					structuredMetadata = labels.Labels{{Name: "foo", Value: "foo"}}
				}

				for i := 0; i < j; i++ {
					if _, err := h.Append(int64(i), "this is the append string", structuredMetadata); err != nil {
						b.Fatal(err)
					}
				}

				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					iter := h.SampleIterator(context.Background(), 0, math.MaxInt64, countExtractor, bytesExtractor)

					for iter.Next() {
						_ = iter.At()
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
		c := NewMemChunk(ChunkFormatV3, compression.None, DefaultTestHeadBlockFmt, 1e6, 1e6)

		if _, err := c.Append(&logproto.Entry{
			Timestamp: time.Unix(0, 1),
			Line:      "1",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Append(&logproto.Entry{
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
				c := createChunk()

				noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
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
	for _, enc := range testEncodings {
		t.Run(enc.String(), func(t *testing.T) {
			t.Parallel()

			c := NewMemChunk(ChunkFormatV3, enc, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize)
			for i := 1; i <= 10; i++ {
				dup, err := c.Append(&logproto.Entry{Timestamp: time.Unix(0, int64(i)), Line: strings.Repeat("e", 200000)})
				require.False(t, dup)
				require.NoError(t, err)
			}
			noopStreamPipeline := log.NewNoopPipeline().ForStream(labels.Labels{})
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

	exp, err := NewMemChunk(ChunkFormatV3, compression.None, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize).BytesWith(nil)
	require.Nil(t, err)
	out, err := NewMemChunk(ChunkFormatV3, compression.None, DefaultTestHeadBlockFmt, testBlockSize, testTargetSize).BytesWith([]byte{1, 2, 3})
	require.Nil(t, err)

	require.Equal(t, exp, out)
}

func TestCheckpointEncoding(t *testing.T) {
	t.Parallel()

	blockSize, targetSize := 256*1024, 1500*1024
	for _, f := range allPossibleFormats {
		t.Run(testNameWithFormats(compression.Snappy, f.chunkFormat, f.headBlockFmt), func(t *testing.T) {
			c := newMemChunkWithFormat(f.chunkFormat, compression.Snappy, f.headBlockFmt, blockSize, targetSize)

			// add a few entries
			for i := 0; i < 5; i++ {
				entry := &logproto.Entry{
					Timestamp: time.Unix(int64(i), 0),
					Line:      fmt.Sprintf("hi there - %d", i),
					StructuredMetadata: push.LabelsAdapter{{
						Name:  fmt.Sprintf("name%d", i),
						Value: fmt.Sprintf("val%d", i),
					}},
				}
				require.Equal(t, true, c.SpaceFor(entry))
				dup, err := c.Append(entry)
				require.False(t, dup)
				require.Nil(t, err)
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
				dup, err := c.Append(entry)
				require.False(t, dup)
				require.Nil(t, err)
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
			c := NewMemChunk(ChunkFormatV3, compression.Snappy, f, testBlockSize, testTargetSize)
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
								streams = append(streams, logproto.Stream{Labels: it.Labels(), Entries: []logproto.Entry{it.At()}})
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
					ex, err := expr.Extractors()
					if err != nil {
						b.Fatal(err)
					}
					var iters []iter.SampleIterator
					for _, lbs := range labelsSet {
						streamExtractors := make([]log.StreamSampleExtractor, 0, len(ex))
						for _, extractor := range ex {
							streamExtractors = append(streamExtractors, extractor.ForStream(lbs))
						}
						iters = append(
							iters,
							c.SampleIterator(
								context.Background(),
								time.Unix(0, 0),
								time.Now(),
								streamExtractors...),
						)
					}
					b.ResetTimer()
					for n := 0; n < b.N; n++ {
						for _, it := range iters {
							for it.Next() {
								series = append(series, logproto.Series{Labels: it.Labels(), Samples: []logproto.Sample{it.At()}})
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
		t.Run(testNameWithFormats(compression.Snappy, testData.chunkFormat, testData.headBlockFmt), func(t *testing.T) {
			c := newMemChunkWithFormat(testData.chunkFormat, compression.Snappy, testData.headBlockFmt, testBlockSize, testTargetSize)
			genEntry := func(i int64) *logproto.Entry {
				return &logproto.Entry{
					Timestamp: time.Unix(0, i),
					Line:      fmt.Sprintf(`msg="%d"`, i),
				}
			}
			var i int64
			for e := genEntry(i); c.SpaceFor(e); e, i = genEntry(i+1), i+1 {
				dup, err := c.Append(e)
				require.False(t, dup)
				require.NoError(t, err)
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
					require.Equal(t, total, it.At().Timestamp.UnixNano())
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

				require.Equal(t, originalChunkItr.At(), newChunkItr.At())
			}
		})
	}
}

func buildTestMemChunk(t *testing.T, from, through time.Time) *MemChunk {
	chk := NewMemChunk(ChunkFormatV3, compression.GZIP, DefaultTestHeadBlockFmt, defaultBlockSize, 0)
	for ; from.Before(through); from = from.Add(time.Second) {
		_, err := chk.Append(&logproto.Entry{
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

	for _, tc := range []struct {
		name          string
		testMemChunk  *MemChunk
		filterFunc    filter.Func
		err           error
		nrMatching    int
		nrNotMatching int
	}{
		{
			name:         "no matches",
			testMemChunk: buildFilterableTestMemChunk(t, chkFrom, chkThrough, nil, nil, false),
			filterFunc: func(_ time.Time, in string, _ ...labels.Label) bool {
				return strings.HasPrefix(in, "matching")
			},
			nrMatching:    0,
			nrNotMatching: 10,
		},
		{
			name:         "some lines removed",
			testMemChunk: buildFilterableTestMemChunk(t, chkFrom, chkThrough, &chkFrom, &chkFromPlus5, false),
			filterFunc: func(_ time.Time, in string, _ ...labels.Label) bool {
				return strings.HasPrefix(in, "matching")
			},
			nrMatching:    5,
			nrNotMatching: 5,
		},
		{
			name:         "all lines match",
			testMemChunk: buildFilterableTestMemChunk(t, chkFrom, chkThrough, &chkFrom, &chkThroughPlus1, false),
			filterFunc: func(_ time.Time, in string, _ ...labels.Label) bool {
				return strings.HasPrefix(in, "matching")
			},
			err: chunk.ErrSliceNoDataInRange,
		},

		// Test cases with structured metadata
		{
			name:         "no matches - chunk without structured metadata",
			testMemChunk: buildFilterableTestMemChunk(t, chkFrom, chkThrough, &chkFrom, &chkThroughPlus1, false),
			filterFunc: func(_ time.Time, _ string, structuredMetadata ...labels.Label) bool {
				return labels.Labels(structuredMetadata).Get(lblPing) == lblPong
			},
			nrMatching:    0,
			nrNotMatching: 10,
		},
		{
			name:         "structured metadata not matching",
			testMemChunk: buildFilterableTestMemChunk(t, chkFrom, chkThrough, &chkFrom, &chkThroughPlus1, true),
			filterFunc: func(_ time.Time, _ string, structuredMetadata ...labels.Label) bool {
				return labels.Labels(structuredMetadata).Get("ding") == "dong"
			},
			nrMatching:    0,
			nrNotMatching: 10,
		},
		{
			name:         "some lines removed - with structured metadata",
			testMemChunk: buildFilterableTestMemChunk(t, chkFrom, chkThrough, &chkFrom, &chkFromPlus5, true),
			filterFunc: func(_ time.Time, _ string, structuredMetadata ...labels.Label) bool {
				return labels.Labels(structuredMetadata).Get(lblPing) == lblPong
			},
			nrMatching:    5,
			nrNotMatching: 5,
		},
		{
			name:         "all lines match -  with structured metadata",
			testMemChunk: buildFilterableTestMemChunk(t, chkFrom, chkThrough, &chkFrom, &chkThroughPlus1, true),
			filterFunc: func(_ time.Time, in string, structuredMetadata ...labels.Label) bool {
				return labels.Labels(structuredMetadata).Get(lblPing) == lblPong && strings.HasPrefix(in, "matching")
			},
			err: chunk.ErrSliceNoDataInRange,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalChunk := tc.testMemChunk
			newChunk, err := originalChunk.Rebound(chkFrom, chkThrough, tc.filterFunc)
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

func buildFilterableTestMemChunk(t *testing.T, from, through time.Time, matchingFrom, matchingTo *time.Time, withStructuredMetadata bool) *MemChunk {
	chk := NewMemChunk(ChunkFormatV4, compression.GZIP, DefaultTestHeadBlockFmt, defaultBlockSize, 0)
	t.Logf("from   : %v", from.String())
	t.Logf("through: %v", through.String())
	var structuredMetadata push.LabelsAdapter
	if withStructuredMetadata {
		structuredMetadata = push.LabelsAdapter{{Name: lblPing, Value: lblPong}}
	}
	for from.Before(through) {
		// If a line is between matchingFrom and matchingTo add the prefix "matching"
		if matchingFrom != nil && matchingTo != nil &&
			(from.Equal(*matchingFrom) || (from.After(*matchingFrom) && (from.Before(*matchingTo)))) {
			t.Logf("%v matching line", from.String())
			_, err := chk.Append(&logproto.Entry{
				Line:               fmt.Sprintf("matching %v", from.String()),
				Timestamp:          from,
				StructuredMetadata: structuredMetadata,
			})
			require.NoError(t, err)
		} else {
			t.Logf("%v non-match line", from.String())
			var structuredMetadata push.LabelsAdapter
			if withStructuredMetadata {
				structuredMetadata = push.LabelsAdapter{{Name: "ding", Value: "dong"}}
			}
			_, err := chk.Append(&logproto.Entry{
				Line:               from.String(),
				Timestamp:          from,
				StructuredMetadata: structuredMetadata,
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
			desc:         "entry fits with structured metadata",
			targetSize:   10,
			headSize:     0,
			cutBlockSize: 0,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      strings.Repeat("a", 2),
				StructuredMetadata: []logproto.LabelAdapter{
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
			desc:         "entry too big because structured metadata",
			targetSize:   10,
			headSize:     0,
			cutBlockSize: 0,
			entry: logproto.Entry{
				Timestamp: time.Unix(0, 0),
				Line:      strings.Repeat("a", 5),
				StructuredMetadata: []logproto.LabelAdapter{
					{Name: "foo", Value: strings.Repeat("a", 5)},
				},
			},

			expectFunc: func(chunkFormat byte, _ HeadBlockFmt) bool {
				// Succeed unless we're using chunk format v4, which should
				// take the structured metadata into account.
				return chunkFormat < ChunkFormatV4
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			for _, format := range allPossibleFormats {
				t.Run(fmt.Sprintf("chunk_v%d_head_%s", format.chunkFormat, format.headBlockFmt), func(t *testing.T) {
					chk := newMemChunkWithFormat(format.chunkFormat, compression.None, format.headBlockFmt, 1024, tc.targetSize)

					chk.blocks = make([]block, tc.nBlocks)
					chk.cutBlockSize = tc.cutBlockSize
					for i := 0; i < tc.headSize; i++ {
						dup, err := chk.head.Append(int64(i), "a", nil)
						require.False(t, dup)
						require.NoError(t, err)
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

func TestMemChunk_IteratorWithStructuredMetadata(t *testing.T) {
	for _, enc := range testEncodings {
		t.Run(enc.String(), func(t *testing.T) {
			streamLabels := labels.Labels{
				{Name: "job", Value: "fake"},
			}
			chk := newMemChunkWithFormat(ChunkFormatV4, enc, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
			dup, err := chk.Append(logprotoEntryWithStructuredMetadata(1, "lineA", []logproto.LabelAdapter{
				{Name: "traceID", Value: "123"},
				{Name: "user", Value: "a"},
			}))
			require.False(t, dup)
			require.NoError(t, err)
			dup, err = chk.Append(logprotoEntryWithStructuredMetadata(2, "lineB", []logproto.LabelAdapter{
				{Name: "traceID", Value: "456"},
				{Name: "user", Value: "b"},
			}))
			require.False(t, dup)
			require.NoError(t, err)
			require.NoError(t, chk.cut())
			dup, err = chk.Append(logprotoEntryWithStructuredMetadata(3, "lineC", []logproto.LabelAdapter{
				{Name: "traceID", Value: "789"},
				{Name: "user", Value: "c"},
			}))
			require.False(t, dup)
			require.NoError(t, err)
			dup, err = chk.Append(logprotoEntryWithStructuredMetadata(4, "lineD", []logproto.LabelAdapter{
				{Name: "traceID", Value: "123"},
				{Name: "user", Value: "d"},
			}))
			require.False(t, dup)
			require.NoError(t, err)

			// The expected bytes is the sum of bytes decompressed and bytes read from the head chunk.
			// First we add the bytes read from the store (aka decompressed). That's
			// structuredMetadataBytes = n. lines * (n. labels <int> + (2 * n. structuredMetadataSymbols * symbol <int>))
			// lineBytes = n. lines * (ts <int> + line length <int> + line)
			expectedStructuredMetadataBytes := 2 * (binary.MaxVarintLen64 + (2 * 2 * binary.MaxVarintLen64))
			lineBytes := 2 * (2*binary.MaxVarintLen64 + len("lineA"))
			// Now we add the bytes read from the head chunk. That's
			// structuredMetadataBytes = n. lines * (2 * n. structuredMetadataSymbols * symbol <uint32>)
			// lineBytes = n. lines * (line)
			expectedStructuredMetadataBytes += 2 * (2 * 2 * 4)
			lineBytes += 2 * (len("lineC"))
			// Finally, the expected total bytes is the line bytes + structured metadata bytes
			expectedBytes := lineBytes + expectedStructuredMetadataBytes

			for _, tc := range []struct {
				name                       string
				query                      string
				expectedLines              []string
				expectedStreams            []string
				expectedStructuredMetadata [][]logproto.LabelAdapter
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
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123", "user", "a")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "456", "user", "b")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "789", "user", "c")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123", "user", "d")),
					},
				},
				{
					name:          "filter",
					query:         `{job="fake"} | traceID="789"`,
					expectedLines: []string{"lineC"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "traceID", "789", "user", "c").String(),
					},
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "789", "user", "c")),
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
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "456", "user", "b")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "789", "user", "c")),
					},
				},
				{
					name:          "filter-regex-contains",
					query:         `{job="fake"} | traceID=~".*5.*"`,
					expectedLines: []string{"lineB"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "traceID", "456", "user", "b").String(),
					},
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "456", "user", "b")),
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
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123", "user", "a")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123", "user", "d")),
					},
				},
				{
					name:          "multiple-filters",
					query:         `{job="fake"} | traceID="123" | user="d"`,
					expectedLines: []string{"lineD"},
					expectedStreams: []string{
						labels.FromStrings("job", "fake", "traceID", "123", "user", "d").String(),
					},
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123", "user", "d")),
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
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "a")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "b")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "c")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "d")),
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
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "b")),
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
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "a")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "b")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "c")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "d")),
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
					expectedStructuredMetadata: [][]logproto.LabelAdapter{
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "a")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "456", "user", "b")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "789", "user", "c")),
						logproto.FromLabelsToLabelAdapters(labels.FromStrings("user", "d")),
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
							var structuredMetadata [][]logproto.LabelAdapter
							for it.Next() {
								require.NoError(t, it.Err())
								e := it.At()
								lines = append(lines, e.Line)
								streams = append(streams, it.Labels())

								if len(e.StructuredMetadata) > 0 {
									structuredMetadata = append(structuredMetadata, e.StructuredMetadata)
								}
								require.Empty(t, e.Parsed)
							}
							assert.ElementsMatch(t, tc.expectedLines, lines)
							assert.ElementsMatch(t, tc.expectedStreams, streams)
							assert.ElementsMatch(t, tc.expectedStructuredMetadata, structuredMetadata)

							resultStats := sts.Result(0, 0, len(lines))
							require.Equal(t, int64(expectedBytes), resultStats.Summary.TotalBytesProcessed)
							require.Equal(t, int64(expectedStructuredMetadataBytes), resultStats.Summary.TotalStructuredMetadataBytesProcessed)
						}
					})

					t.Run("metric", func(t *testing.T) {
						query := fmt.Sprintf(`count_over_time(%s [1d])`, tc.query)
						expr, err := syntax.ParseSampleExpr(query)
						require.NoError(t, err)

						extractors, err := expr.Extractors()
						require.NoError(t, err)

						// We will run the test twice so the iterator will be created twice.
						// This is to ensure that the iterator is correctly closed.
						for i := 0; i < 2; i++ {
							sts, ctx := stats.NewContext(context.Background())

							streamExtractors := make(
								[]log.StreamSampleExtractor,
								0,
								len(extractors),
							)
							for _, extractor := range extractors {
								streamExtractors = append(
									streamExtractors,
									extractor.ForStream(streamLabels),
								)
							}
							it := chk.SampleIterator(
								ctx,
								time.Unix(0, 0),
								time.Unix(0, math.MaxInt64),
								streamExtractors...)

							var sumValues int
							var streams []string
							for it.Next() {
								require.NoError(t, it.Err())
								e := it.At()
								sumValues += int(e.Value)
								streams = append(streams, it.Labels())
							}
							require.Equal(t, len(tc.expectedLines), sumValues)
							assert.ElementsMatch(t, tc.expectedStreams, streams)

							resultStats := sts.Result(0, 0, 0)
							require.Equal(t, int64(expectedBytes), resultStats.Summary.TotalBytesProcessed)
							require.Equal(t, int64(expectedStructuredMetadataBytes), resultStats.Summary.TotalStructuredMetadataBytesProcessed)
						}
					})
				})
			}
		})
	}
}

func TestDecodeChunkIncorrectBlockOffset(t *testing.T) {
	// use small block size to build multiple blocks in the test chunk
	blockSize := 10

	for _, format := range allPossibleFormats {
		t.Run(fmt.Sprintf("chunkFormat:%v headBlockFmt:%v", format.chunkFormat, format.headBlockFmt), func(t *testing.T) {
			for incorrectOffsetBlockNum := 0; incorrectOffsetBlockNum < 3; incorrectOffsetBlockNum++ {
				t.Run(fmt.Sprintf("inorrect offset block: %d", incorrectOffsetBlockNum), func(t *testing.T) {
					chk := NewMemChunk(format.chunkFormat, compression.None, format.headBlockFmt, blockSize, testTargetSize)
					ts := time.Now().Unix()
					for i := 0; i < 3; i++ {
						dup, err := chk.Append(&logproto.Entry{
							Timestamp: time.Now(),
							Line:      fmt.Sprintf("%d-%d", ts, i),
							StructuredMetadata: []logproto.LabelAdapter{
								{Name: "foo", Value: fmt.Sprintf("%d-%d", ts, i)},
							},
						})
						require.NoError(t, err)
						require.False(t, dup)
					}

					require.Len(t, chk.blocks, 3)

					b, err := chk.Bytes()
					require.NoError(t, err)

					metasOffset := binary.BigEndian.Uint64(b[len(b)-8:])

					w := bytes.NewBuffer(nil)
					eb := EncodeBufferPool.Get().(*encbuf)
					defer EncodeBufferPool.Put(eb)

					crc32Hash := crc32HashPool.Get().(hash.Hash32)
					defer crc32HashPool.Put(crc32Hash)

					crc32Hash.Reset()
					eb.reset()

					// BEGIN - code copied from writeTo func starting from encoding of block metas to change offset of a block
					eb.putUvarint(len(chk.blocks))

					for i, b := range chk.blocks {
						eb.putUvarint(b.numEntries)
						eb.putVarint64(b.mint)
						eb.putVarint64(b.maxt)
						// change offset of one block
						blockOffset := b.offset
						if i == incorrectOffsetBlockNum {
							blockOffset += 5
						}
						eb.putUvarint(blockOffset)
						if chk.format >= ChunkFormatV3 {
							eb.putUvarint(b.uncompressedSize)
						}
						eb.putUvarint(len(b.b))
					}
					metasLen := len(eb.get())
					eb.putHash(crc32Hash)

					_, err = w.Write(eb.get())
					require.NoError(t, err)

					if chk.format >= ChunkFormatV4 {
						// Write structured metadata offset and length
						eb.reset()

						eb.putBE64int(int(binary.BigEndian.Uint64(b[len(b)-32:])))
						eb.putBE64int(int(binary.BigEndian.Uint64(b[len(b)-24:])))
						_, err = w.Write(eb.get())
						require.NoError(t, err)
					}

					// Write the metasOffset.
					eb.reset()
					if chk.format >= ChunkFormatV4 {
						eb.putBE64int(metasLen)
					}
					eb.putBE64int(int(metasOffset))
					_, err = w.Write(eb.get())
					require.NoError(t, err)
					// END - code copied from writeTo func

					// build chunk using pre-block meta section + rewritten remainder of the chunk with incorrect offset for a block
					chkWithIncorrectOffset := make([]byte, int(metasOffset)+w.Len())
					copy(chkWithIncorrectOffset, b[:metasOffset])
					copy(chkWithIncorrectOffset[metasOffset:], w.Bytes())

					// decoding the problematic chunk should succeed
					decodedChkWithIncorrectOffset, err := newByteChunk(chkWithIncorrectOffset, blockSize, testTargetSize, false)
					require.NoError(t, err)

					require.Len(t, decodedChkWithIncorrectOffset.blocks, len(chk.blocks))

					// both chunks should have same log lines
					origChunkItr, err := chk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
					require.NoError(t, err)

					corruptChunkItr, err := decodedChkWithIncorrectOffset.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
					require.NoError(t, err)

					numEntriesFound := 0
					for origChunkItr.Next() {
						numEntriesFound++
						require.True(t, corruptChunkItr.Next())
						require.Equal(t, origChunkItr.At(), corruptChunkItr.At())
					}

					require.False(t, corruptChunkItr.Next())
					require.Equal(t, 3, numEntriesFound)
				})
			}
		})
	}
}
