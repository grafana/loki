package chunkenc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

func iterEq(t *testing.T, exp []entry, got iter.EntryIterator) {
	var i int
	for got.Next() {
		require.Equal(t, logproto.Entry{
			Timestamp:          time.Unix(0, exp[i].t),
			Line:               exp[i].s,
			StructuredMetadata: logproto.FromLabelsToLabelAdapters(exp[i].structuredMetadata),
		}, got.At())
		require.Equal(t, exp[i].structuredMetadata.String(), got.Labels())
		i++
	}
	require.Equal(t, i, len(exp))
}

func Test_forEntriesEarlyReturn(t *testing.T) {
	hb := newUnorderedHeadBlock(UnorderedHeadBlockFmt, newSymbolizer())
	for i := 0; i < 10; i++ {
		dup, err := hb.Append(int64(i), fmt.Sprint(i), labels.Labels{{Name: "i", Value: fmt.Sprint(i)}})
		require.False(t, dup)
		require.Nil(t, err)
	}

	// forward
	var forwardCt int
	var forwardStop int64
	err := hb.forEntries(
		context.Background(),
		logproto.FORWARD,
		0,
		math.MaxInt64,
		func(_ *stats.Context, ts int64, _ string, _ symbols) error {
			forwardCt++
			forwardStop = ts
			if ts == 5 {
				return errors.New("err")
			}
			return nil
		},
	)
	require.Error(t, err)
	require.Equal(t, int64(5), forwardStop)
	require.Equal(t, 6, forwardCt)

	// backward
	var backwardCt int
	var backwardStop int64
	err = hb.forEntries(
		context.Background(),
		logproto.BACKWARD,
		0,
		math.MaxInt64,
		func(_ *stats.Context, ts int64, _ string, _ symbols) error {
			backwardCt++
			backwardStop = ts
			if ts == 5 {
				return errors.New("err")
			}
			return nil
		},
	)
	require.Error(t, err)
	require.Equal(t, int64(5), backwardStop)
	require.Equal(t, 5, backwardCt)
}

func Test_Unordered_InsertRetrieval(t *testing.T) {
	for _, tc := range []struct {
		desc       string
		input, exp []entry
		dir        logproto.Direction
		hasDup     bool
	}{
		{
			desc: "simple forward",
			input: []entry{
				{0, "a", nil}, {1, "b", nil}, {2, "c", labels.Labels{{Name: "a", Value: "b"}}},
			},
			exp: []entry{
				{0, "a", nil}, {1, "b", nil}, {2, "c", labels.Labels{{Name: "a", Value: "b"}}},
			},
		},
		{
			desc: "simple backward",
			input: []entry{
				{0, "a", nil}, {1, "b", nil}, {2, "c", labels.Labels{{Name: "a", Value: "b"}}},
			},
			exp: []entry{
				{2, "c", labels.Labels{{Name: "a", Value: "b"}}}, {1, "b", nil}, {0, "a", nil},
			},
			dir: logproto.BACKWARD,
		},
		{
			desc: "unordered forward",
			input: []entry{
				{1, "b", nil}, {0, "a", nil}, {2, "c", labels.Labels{{Name: "a", Value: "b"}}},
			},
			exp: []entry{
				{0, "a", nil}, {1, "b", nil}, {2, "c", labels.Labels{{Name: "a", Value: "b"}}},
			},
		},
		{
			desc: "unordered backward",
			input: []entry{
				{1, "b", nil}, {0, "a", nil}, {2, "c", labels.Labels{{Name: "a", Value: "b"}}},
			},
			exp: []entry{
				{2, "c", labels.Labels{{Name: "a", Value: "b"}}}, {1, "b", nil}, {0, "a", nil},
			},
			dir: logproto.BACKWARD,
		},
		{
			desc: "ts collision forward",
			input: []entry{
				{0, "a", labels.Labels{{Name: "a", Value: "b"}}}, {0, "b", labels.Labels{{Name: "a", Value: "b"}}}, {1, "c", nil},
			},
			exp: []entry{
				{0, "a", labels.Labels{{Name: "a", Value: "b"}}}, {0, "b", labels.Labels{{Name: "a", Value: "b"}}}, {1, "c", nil},
			},
		},
		{
			desc: "ts collision backward",
			input: []entry{
				{0, "a", labels.Labels{{Name: "a", Value: "b"}}}, {0, "b", nil}, {1, "c", nil},
			},
			exp: []entry{
				{1, "c", nil}, {0, "b", nil}, {0, "a", labels.Labels{{Name: "a", Value: "b"}}},
			},
			dir: logproto.BACKWARD,
		},
		{
			desc: "ts remove exact dupe forward",
			input: []entry{
				{0, "a", nil}, {0, "b", nil}, {1, "c", nil}, {0, "b", labels.Labels{{Name: "a", Value: "b"}}},
			},
			exp: []entry{
				{0, "a", nil}, {0, "b", nil}, {1, "c", nil},
			},
			dir:    logproto.FORWARD,
			hasDup: true,
		},
		{
			desc: "ts remove exact dupe backward",
			input: []entry{
				{0, "a", nil}, {0, "b", nil}, {1, "c", nil}, {0, "b", labels.Labels{{Name: "a", Value: "b"}}},
			},
			exp: []entry{
				{1, "c", nil}, {0, "b", nil}, {0, "a", nil},
			},
			dir:    logproto.BACKWARD,
			hasDup: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			for _, format := range []HeadBlockFmt{
				UnorderedHeadBlockFmt,
				UnorderedWithStructuredMetadataHeadBlockFmt,
			} {
				t.Run(format.String(), func(t *testing.T) {
					hb := newUnorderedHeadBlock(format, newSymbolizer())
					dup := false
					for _, e := range tc.input {
						tmpdup, err := hb.Append(e.t, e.s, e.structuredMetadata)
						if !dup { // only set dup if it's not already true
							if tmpdup { // can't examine duplicates until we start getting all the data
								dup = true
							}
						}
						require.Nil(t, err)
					}
					require.Equal(t, tc.hasDup, dup)

					itr := hb.Iterator(
						context.Background(),
						tc.dir,
						0,
						math.MaxInt64,
						noopStreamPipeline,
					)

					expected := make([]entry, len(tc.exp))
					copy(expected, tc.exp)
					if format < UnorderedWithStructuredMetadataHeadBlockFmt {
						for i := range expected {
							expected[i].structuredMetadata = nil
						}
					}

					iterEq(t, expected, itr)
				})
			}
		})
	}
}

func Test_UnorderedBoundedIter(t *testing.T) {
	for _, tc := range []struct {
		desc       string
		mint, maxt int64
		dir        logproto.Direction
		input      []entry
		exp        []entry
	}{
		{
			desc: "simple",
			mint: 1,
			maxt: 4,
			input: []entry{
				{0, "a", nil}, {1, "b", labels.Labels{{Name: "a", Value: "b"}}}, {2, "c", nil}, {3, "d", nil}, {4, "e", nil},
			},
			exp: []entry{
				{1, "b", labels.Labels{{Name: "a", Value: "b"}}}, {2, "c", nil}, {3, "d", nil},
			},
		},
		{
			desc: "simple backward",
			mint: 1,
			maxt: 4,
			input: []entry{
				{0, "a", nil}, {1, "b", labels.Labels{{Name: "a", Value: "b"}}}, {2, "c", nil}, {3, "d", nil}, {4, "e", nil},
			},
			exp: []entry{
				{3, "d", nil}, {2, "c", nil}, {1, "b", labels.Labels{{Name: "a", Value: "b"}}},
			},
			dir: logproto.BACKWARD,
		},
		{
			desc: "unordered",
			mint: 1,
			maxt: 4,
			input: []entry{
				{0, "a", nil}, {2, "c", nil}, {1, "b", labels.Labels{{Name: "a", Value: "b"}}}, {4, "e", nil}, {3, "d", nil},
			},
			exp: []entry{
				{1, "b", labels.Labels{{Name: "a", Value: "b"}}}, {2, "c", nil}, {3, "d", nil},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			for _, format := range []HeadBlockFmt{
				UnorderedHeadBlockFmt,
				UnorderedWithStructuredMetadataHeadBlockFmt,
			} {
				t.Run(format.String(), func(t *testing.T) {
					hb := newUnorderedHeadBlock(format, newSymbolizer())
					for _, e := range tc.input {
						dup, err := hb.Append(e.t, e.s, e.structuredMetadata)
						require.False(t, dup)
						require.Nil(t, err)
					}

					itr := hb.Iterator(
						context.Background(),
						tc.dir,
						tc.mint,
						tc.maxt,
						noopStreamPipeline,
					)

					expected := make([]entry, len(tc.exp))
					copy(expected, tc.exp)
					if format < UnorderedWithStructuredMetadataHeadBlockFmt {
						for i := range expected {
							expected[i].structuredMetadata = nil
						}
					}

					iterEq(t, expected, itr)
				})
			}
		})
	}
}

func TestHeadBlockInterop(t *testing.T) {
	unordered, ordered := newUnorderedHeadBlock(UnorderedHeadBlockFmt, nil), &headBlock{}
	unorderedWithStructuredMetadata := newUnorderedHeadBlock(UnorderedWithStructuredMetadataHeadBlockFmt, newSymbolizer())
	for i := 0; i < 100; i++ {
		metaLabels := labels.Labels{{Name: "foo", Value: fmt.Sprint(99 - i)}}
		dup, err := unordered.Append(int64(99-i), fmt.Sprint(99-i), metaLabels)
		require.False(t, dup)
		require.Nil(t, err)
		dup, err = unorderedWithStructuredMetadata.Append(int64(99-i), fmt.Sprint(99-i), metaLabels)
		require.False(t, dup)
		require.Nil(t, err)
		dup, err = ordered.Append(int64(i), fmt.Sprint(i), labels.Labels{{Name: "foo", Value: fmt.Sprint(i)}})
		require.False(t, dup)
		require.Nil(t, err)
	}

	// turn to bytes
	orderedCheckpointBytes, err := ordered.CheckpointBytes(nil)
	require.Nil(t, err)
	unorderedCheckpointBytes, err := unordered.CheckpointBytes(nil)
	require.Nil(t, err)
	unorderedWithStructuredMetadataCheckpointBytes, err := unorderedWithStructuredMetadata.CheckpointBytes(nil)
	require.Nil(t, err)

	// Ensure we can recover ordered checkpoint into ordered headblock
	recovered, err := HeadFromCheckpoint(orderedCheckpointBytes, OrderedHeadBlockFmt, nil)
	require.Nil(t, err)
	require.Equal(t, ordered, recovered)

	// Ensure we can recover ordered checkpoint into unordered headblock
	recovered, err = HeadFromCheckpoint(orderedCheckpointBytes, UnorderedHeadBlockFmt, nil)
	require.Nil(t, err)
	require.Equal(t, unordered, recovered)

	// Ensure we can recover ordered checkpoint into unordered headblock with structured metadata
	recovered, err = HeadFromCheckpoint(orderedCheckpointBytes, UnorderedWithStructuredMetadataHeadBlockFmt, nil)
	require.NoError(t, err)
	require.Equal(t, &unorderedHeadBlock{
		format: UnorderedWithStructuredMetadataHeadBlockFmt,
		rt:     unordered.rt,
		lines:  unordered.lines,
		size:   unordered.size,
		mint:   unordered.mint,
		maxt:   unordered.maxt,
	}, recovered)

	// Ensure we can recover unordered checkpoint into unordered headblock
	recovered, err = HeadFromCheckpoint(unorderedCheckpointBytes, UnorderedHeadBlockFmt, nil)
	require.Nil(t, err)
	require.Equal(t, unordered, recovered)

	// Ensure trying to recover unordered checkpoint into unordered with structured metadata keeps it in unordered format
	recovered, err = HeadFromCheckpoint(unorderedCheckpointBytes, UnorderedWithStructuredMetadataHeadBlockFmt, nil)
	require.NoError(t, err)
	require.Equal(t, unordered, recovered)

	// Ensure trying to recover unordered with structured metadata checkpoint into ordered headblock keeps it in unordered with structured metadata format
	recovered, err = HeadFromCheckpoint(unorderedWithStructuredMetadataCheckpointBytes, OrderedHeadBlockFmt, unorderedWithStructuredMetadata.symbolizer)
	require.Nil(t, err)
	require.Equal(t, unorderedWithStructuredMetadata, recovered) // we compare the data with unordered because unordered head block does not contain metaLabels.

	// Ensure trying to recover unordered with structured metadata checkpoint into unordered headblock keeps it in unordered with structured metadata format
	recovered, err = HeadFromCheckpoint(unorderedWithStructuredMetadataCheckpointBytes, UnorderedHeadBlockFmt, unorderedWithStructuredMetadata.symbolizer)
	require.Nil(t, err)
	require.Equal(t, unorderedWithStructuredMetadata, recovered) // we compare the data with unordered because unordered head block does not contain metaLabels.

	// Ensure we can recover unordered with structured metadata checkpoint into unordered with structured metadata headblock
	recovered, err = HeadFromCheckpoint(unorderedWithStructuredMetadataCheckpointBytes, UnorderedWithStructuredMetadataHeadBlockFmt, unorderedWithStructuredMetadata.symbolizer)
	require.Nil(t, err)
	require.Equal(t, unorderedWithStructuredMetadata, recovered)
}

// ensure backwards compatibility from when chunk format
// and head block format was split
func TestChunkBlockFmt(t *testing.T) {
	require.Equal(t, ChunkFormatV3, byte(OrderedHeadBlockFmt))
}

func BenchmarkHeadBlockWrites(b *testing.B) {
	// ordered, ordered
	// unordered, ordered
	// unordered, unordered

	// current default block size of 256kb with 75b avg log lines =~ 5.2k lines/block
	nWrites := (256 << 10) / 50

	headBlockFn := func() func(int64, string, labels.Labels) {
		hb := &headBlock{}
		return func(ts int64, line string, metaLabels labels.Labels) {
			_, _ = hb.Append(ts, line, metaLabels)
		}
	}

	unorderedHeadBlockFn := func() func(int64, string, labels.Labels) {
		hb := newUnorderedHeadBlock(UnorderedHeadBlockFmt, nil)
		return func(ts int64, line string, metaLabels labels.Labels) {
			_, _ = hb.Append(ts, line, metaLabels)
		}
	}

	for _, tc := range []struct {
		desc            string
		fn              func() func(int64, string, labels.Labels)
		unorderedWrites bool
	}{
		{
			desc: "ordered headblock ordered writes",
			fn:   headBlockFn,
		},
		{
			desc: "unordered headblock ordered writes",
			fn:   unorderedHeadBlockFn,
		},
		{
			desc:            "unordered headblock unordered writes",
			fn:              unorderedHeadBlockFn,
			unorderedWrites: true,
		},
	} {
		for _, withStructuredMetadata := range []bool{false, true} {
			// build writes before we start benchmarking so random number generation, etc,
			// isn't included in our timing info
			writes := make([]entry, 0, nWrites)
			rnd := rand.NewSource(0)
			for i := 0; i < nWrites; i++ {
				ts := int64(i)
				if tc.unorderedWrites {
					ts = rnd.Int63()
				}

				var structuredMetadata labels.Labels
				if withStructuredMetadata {
					structuredMetadata = labels.Labels{{Name: "foo", Value: fmt.Sprint(ts)}}
				}

				writes = append(writes, entry{
					t:                  ts,
					s:                  fmt.Sprint("line:", i),
					structuredMetadata: structuredMetadata,
				})
			}

			name := tc.desc
			if withStructuredMetadata {
				name += " with structured metadata"
			}
			b.Run(name, func(b *testing.B) {
				for n := 0; n < b.N; n++ {
					writeFn := tc.fn()
					for _, w := range writes {
						writeFn(w.t, w.s, w.structuredMetadata)
					}
				}
			})
		}
	}
}

func TestUnorderedChunkIterators(t *testing.T) {
	c := NewMemChunk(ChunkFormatV4, compression.Snappy, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
	for i := 0; i < 100; i++ {
		// push in reverse order
		dup, err := c.Append(&logproto.Entry{
			Timestamp: time.Unix(int64(99-i), 0),
			Line:      fmt.Sprint(99 - i),
		})
		require.False(t, dup)
		require.Nil(t, err)

		// ensure we have a mix of cut blocks + head block.
		if i%30 == 0 {
			require.Nil(t, c.cut())
		}
	}

	// ensure head block has data
	require.Equal(t, false, c.head.IsEmpty())

	forward, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(100, 0), logproto.FORWARD, noopStreamPipeline)
	require.Nil(t, err)

	backward, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(100, 0), logproto.BACKWARD, noopStreamPipeline)
	require.Nil(t, err)

	smpl := c.SampleIterator(
		context.Background(),
		time.Unix(0, 0),
		time.Unix(100, 0),
		countExtractor,
	)

	for i := 0; i < 100; i++ {
		require.Equal(t, true, forward.Next())
		require.Equal(t, true, backward.Next())
		require.Equal(t, true, smpl.Next())
		require.Equal(t, time.Unix(int64(i), 0), forward.At().Timestamp)
		require.Equal(t, time.Unix(int64(99-i), 0), backward.At().Timestamp)
		require.Equal(t, float64(1), smpl.At().Value)
		require.Equal(t, time.Unix(int64(i), 0).UnixNano(), smpl.At().Timestamp)
	}
	require.Equal(t, false, forward.Next())
	require.Equal(t, false, backward.Next())
}

func BenchmarkUnorderedRead(b *testing.B) {
	legacy := NewMemChunk(ChunkFormatV3, compression.Snappy, OrderedHeadBlockFmt, testBlockSize, testTargetSize)
	fillChunkClose(legacy, false)
	ordered := NewMemChunk(ChunkFormatV3, compression.Snappy, UnorderedHeadBlockFmt, testBlockSize, testTargetSize)
	fillChunkClose(ordered, false)
	unordered := NewMemChunk(ChunkFormatV3, compression.Snappy, UnorderedHeadBlockFmt, testBlockSize, testTargetSize)
	fillChunkRandomOrder(unordered, false)

	tcs := []struct {
		desc string
		c    *MemChunk
	}{
		{
			desc: "ordered+legacy hblock",
			c:    legacy,
		},
		{
			desc: "ordered+unordered hblock",
			c:    ordered,
		},
		{
			desc: "unordered+unordered hblock",
			c:    unordered,
		},
	}

	b.Run("itr", func(b *testing.B) {
		for _, tc := range tcs {
			b.Run(tc.desc, func(b *testing.B) {
				for n := 0; n < b.N; n++ {
					iterator, err := tc.c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline)
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
	})

	b.Run("smpl", func(b *testing.B) {
		for _, tc := range tcs {
			b.Run(tc.desc, func(b *testing.B) {
				for n := 0; n < b.N; n++ {
					iterator := tc.c.SampleIterator(
						context.Background(),
						time.Unix(0, 0),
						time.Unix(0, math.MaxInt64),
						countExtractor,
					)
					for iterator.Next() {
						_ = iterator.At()
					}
					if err := iterator.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})
}

func TestUnorderedIteratorCountsAllEntries(t *testing.T) {
	c := NewMemChunk(ChunkFormatV4, compression.Snappy, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
	fillChunkRandomOrder(c, false)

	ct := 0
	var i int64
	iterator, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline)
	if err != nil {
		panic(err)
	}
	for iterator.Next() {
		next := iterator.At().Timestamp.UnixNano()
		require.GreaterOrEqual(t, next, i)
		i = next
		ct++
	}
	if err := iterator.Close(); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, c.Size(), ct)

	ct = 0
	i = 0
	smpl := c.SampleIterator(
		context.Background(),
		time.Unix(0, 0),
		time.Unix(0, math.MaxInt64),
		countExtractor,
	)
	for smpl.Next() {
		next := smpl.At().Timestamp
		require.GreaterOrEqual(t, next, i)
		i = next
		ct += int(smpl.At().Value)
	}
	require.Equal(t, c.Size(), ct)

	if err := iterator.Close(); err != nil {
		t.Fatal(err)
	}
}

func chunkFrom(xs []logproto.Entry) ([]byte, error) {
	c := NewMemChunk(ChunkFormatV4, compression.Snappy, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
	for _, x := range xs {
		if _, err := c.Append(&x); err != nil {
			return nil, err
		}
	}

	if err := c.Close(); err != nil {
		return nil, err
	}
	return c.Bytes()
}

func TestReorder(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		input    []logproto.Entry
		expected []logproto.Entry
	}{
		{
			desc: "unordered",
			input: []logproto.Entry{
				{
					Timestamp: time.Unix(4, 0),
					Line:      "x",
				},
				{
					Timestamp: time.Unix(2, 0),
					Line:      "x",
				},
				{
					Timestamp: time.Unix(3, 0),
					Line:      "x",
				},
				{
					Timestamp: time.Unix(1, 0),
					Line:      "x",
				},
			},
			expected: []logproto.Entry{
				{
					Timestamp: time.Unix(1, 0),
					Line:      "x",
				},
				{
					Timestamp: time.Unix(2, 0),
					Line:      "x",
				},
				{
					Timestamp: time.Unix(3, 0),
					Line:      "x",
				},
				{
					Timestamp: time.Unix(4, 0),
					Line:      "x",
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			c := NewMemChunk(ChunkFormatV4, compression.Snappy, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
			for _, x := range tc.input {
				dup, err := c.Append(&x)
				require.False(t, dup)
				require.Nil(t, err)
			}
			require.Nil(t, c.Close())
			b, err := c.Bytes()
			require.Nil(t, err)

			exp, err := chunkFrom(tc.expected)
			require.Nil(t, err)

			require.Equal(t, exp, b)
		})
	}
}

func TestReorderAcrossBlocks(t *testing.T) {
	c := NewMemChunk(ChunkFormatV4, compression.Snappy, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
	for _, batch := range [][]int{
		// ensure our blocks have overlapping bounds and must be reordered
		// before closing.
		{1, 5},
		{3, 7},
	} {
		for _, x := range batch {
			dup, err := c.Append(&logproto.Entry{
				Timestamp: time.Unix(int64(x), 0),
				Line:      fmt.Sprint(x),
			})
			require.False(t, dup)
			require.Nil(t, err)
		}
		require.Nil(t, c.cut())
	}
	// get bounds before it's reordered
	from, to := c.Bounds()
	require.Nil(t, c.Close())

	itr, err := c.Iterator(context.Background(), from, to.Add(time.Nanosecond), logproto.FORWARD, log.NewNoopPipeline().ForStream(nil))
	require.Nil(t, err)

	exp := []entry{
		{
			t: time.Unix(1, 0).UnixNano(),
			s: "1",
		},
		{
			t: time.Unix(3, 0).UnixNano(),
			s: "3",
		},
		{
			t: time.Unix(5, 0).UnixNano(),
			s: "5",
		},
		{
			t: time.Unix(7, 0).UnixNano(),
			s: "7",
		},
	}
	iterEq(t, exp, itr)
}

func Test_HeadIteratorHash(t *testing.T) {
	lbs := labels.Labels{labels.Label{Name: "foo", Value: "bar"}}
	countEx, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
	require.NoError(t, err)
	bytesEx, err := log.NewLineSampleExtractor(log.BytesExtractor, nil, nil, false, false)
	require.NoError(t, err)

	for name, b := range map[string]HeadBlock{
		"unordered":                          newUnorderedHeadBlock(UnorderedHeadBlockFmt, nil),
		"unordered with structured metadata": newUnorderedHeadBlock(UnorderedWithStructuredMetadataHeadBlockFmt, newSymbolizer()),
		"ordered":                            &headBlock{},
	} {
		t.Run(fmt.Sprintf("%s SampleIterator", name), func(t *testing.T) {
			dup, err := b.Append(1, "foo", labels.Labels{{Name: "foo", Value: "bar"}})
			require.False(t, dup)
			require.NoError(t, err)
			eit := b.Iterator(context.Background(), logproto.BACKWARD, 0, 2, log.NewNoopPipeline().ForStream(lbs))

			for eit.Next() {
				require.Equal(t, lbs.Hash(), eit.StreamHash())
			}

			sit := b.SampleIterator(context.TODO(), 0, 2, countEx.ForStream(lbs))
			for sit.Next() {
				require.Equal(t, lbs.Hash(), sit.StreamHash())
			}
		})

		t.Run(fmt.Sprintf("%s SampleIterator with multiple extractors", name), func(t *testing.T) {
			dup, err := b.Append(1, "bar", labels.Labels{{Name: "bar", Value: "foo"}})
			require.False(t, dup)
			require.NoError(t, err)
			eit := b.Iterator(
				context.Background(),
				logproto.BACKWARD,
				0,
				2,
				log.NewNoopPipeline().ForStream(lbs),
			)

			for eit.Next() {
				require.Equal(t, lbs.Hash(), eit.StreamHash())
			}

			sit := b.SampleIterator(
				context.TODO(),
				0,
				2,
				countEx.ForStream(lbs),
				bytesEx.ForStream(lbs),
			)
			for sit.Next() {
				require.Equal(t, lbs.Hash(), sit.StreamHash())
			}
		})
	}
}
