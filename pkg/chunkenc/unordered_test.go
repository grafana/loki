package chunkenc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

func iterEq(t *testing.T, exp []entry, got iter.EntryIterator) {
	var i int
	for got.Next() {
		require.Equal(t, logproto.Entry{
			Timestamp: time.Unix(0, exp[i].t),
			Line:      exp[i].s,
		}, got.Entry())
		i++
	}
	require.Equal(t, i, len(exp))
}

func Test_forEntriesEarlyReturn(t *testing.T) {
	hb := newUnorderedHeadBlock()
	for i := 0; i < 10; i++ {
		require.Nil(t, hb.Append(int64(i), fmt.Sprint(i)))
	}

	// forward
	var forwardCt int
	var forwardStop int64
	err := hb.forEntries(
		context.Background(),
		logproto.FORWARD,
		0,
		math.MaxInt64,
		func(ts int64, line string) error {
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
		func(ts int64, line string) error {
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
	}{
		{
			desc: "simple forward",
			input: []entry{
				{0, "a"}, {1, "b"}, {2, "c"},
			},
			exp: []entry{
				{0, "a"}, {1, "b"}, {2, "c"},
			},
		},
		{
			desc: "simple backward",
			input: []entry{
				{0, "a"}, {1, "b"}, {2, "c"},
			},
			exp: []entry{
				{2, "c"}, {1, "b"}, {0, "a"},
			},
			dir: logproto.BACKWARD,
		},
		{
			desc: "unordered forward",
			input: []entry{
				{1, "b"}, {0, "a"}, {2, "c"},
			},
			exp: []entry{
				{0, "a"}, {1, "b"}, {2, "c"},
			},
		},
		{
			desc: "unordered backward",
			input: []entry{
				{1, "b"}, {0, "a"}, {2, "c"},
			},
			exp: []entry{
				{2, "c"}, {1, "b"}, {0, "a"},
			},
			dir: logproto.BACKWARD,
		},
		{
			desc: "ts collision forward",
			input: []entry{
				{0, "a"}, {0, "b"}, {1, "c"},
			},
			exp: []entry{
				{0, "a"}, {0, "b"}, {1, "c"},
			},
		},
		{
			desc: "ts collision backward",
			input: []entry{
				{0, "a"}, {0, "b"}, {1, "c"},
			},
			exp: []entry{
				{1, "c"}, {0, "b"}, {0, "a"},
			},
			dir: logproto.BACKWARD,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			hb := newUnorderedHeadBlock()
			for _, e := range tc.input {
				require.Nil(t, hb.Append(e.t, e.s))
			}

			itr := hb.Iterator(
				context.Background(),
				tc.dir,
				0,
				math.MaxInt64,
				noopStreamPipeline,
			)

			iterEq(t, tc.exp, itr)
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
				{0, "a"}, {1, "b"}, {2, "c"}, {3, "d"}, {4, "e"},
			},
			exp: []entry{
				{1, "b"}, {2, "c"}, {3, "d"},
			},
		},
		{
			desc: "simple backward",
			mint: 1,
			maxt: 4,
			input: []entry{
				{0, "a"}, {1, "b"}, {2, "c"}, {3, "d"}, {4, "e"},
			},
			exp: []entry{
				{3, "d"}, {2, "c"}, {1, "b"},
			},
			dir: logproto.BACKWARD,
		},
		{
			desc: "unordered",
			mint: 1,
			maxt: 4,
			input: []entry{
				{0, "a"}, {2, "c"}, {1, "b"}, {4, "e"}, {3, "d"},
			},
			exp: []entry{
				{1, "b"}, {2, "c"}, {3, "d"},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			hb := newUnorderedHeadBlock()
			for _, e := range tc.input {
				require.Nil(t, hb.Append(e.t, e.s))
			}

			itr := hb.Iterator(
				context.Background(),
				tc.dir,
				tc.mint,
				tc.maxt,
				noopStreamPipeline,
			)

			iterEq(t, tc.exp, itr)
		})
	}
}

func TestHeadBlockInterop(t *testing.T) {
	unordered, ordered := newUnorderedHeadBlock(), &headBlock{}
	for i := 0; i < 100; i++ {
		require.Nil(t, unordered.Append(int64(99-i), fmt.Sprint(99-i)))
		require.Nil(t, ordered.Append(int64(i), fmt.Sprint(i)))
	}

	// turn to bytes
	b1, err := ordered.CheckpointBytes(nil)
	require.Nil(t, err)
	b2, err := unordered.CheckpointBytes(nil)
	require.Nil(t, err)

	// Ensure we can recover ordered checkpoint into ordered headblock
	recovered, err := HeadFromCheckpoint(b1, OrderedHeadBlockFmt)
	require.Nil(t, err)
	require.Equal(t, ordered, recovered)

	// Ensure we can recover ordered checkpoint into unordered headblock
	recovered, err = HeadFromCheckpoint(b1, UnorderedHeadBlockFmt)
	require.Nil(t, err)
	require.Equal(t, unordered, recovered)

	// Ensure we can recover unordered checkpoint into ordered headblock
	recovered, err = HeadFromCheckpoint(b2, OrderedHeadBlockFmt)
	require.Nil(t, err)
	require.Equal(t, ordered, recovered)

	// Ensure we can recover unordered checkpoint into unordered headblock
	recovered, err = HeadFromCheckpoint(b2, UnorderedHeadBlockFmt)
	require.Nil(t, err)
	require.Equal(t, unordered, recovered)
}

// ensure backwards compatibility from when chunk format
// and head block format was split
func TestChunkBlockFmt(t *testing.T) {
	require.Equal(t, chunkFormatV3, byte(OrderedHeadBlockFmt))
}

func BenchmarkHeadBlockWrites(b *testing.B) {
	// ordered, ordered
	// unordered, ordered
	// unordered, unordered

	// current default block size of 256kb with 75b avg log lines =~ 5.2k lines/block
	var nWrites = (256 << 10) / 50

	headBlockFn := func() func(int64, string) {
		hb := &headBlock{}
		return func(ts int64, line string) {
			_ = hb.Append(ts, line)
		}
	}

	unorderedHeadBlockFn := func() func(int64, string) {
		hb := newUnorderedHeadBlock()
		return func(ts int64, line string) {
			_ = hb.Append(ts, line)
		}
	}

	for _, tc := range []struct {
		desc            string
		fn              func() func(int64, string)
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
		// build writes before we start benchmarking so random number generation, etc,
		// isn't included in our timing info
		writes := make([]entry, 0, nWrites)
		rnd := rand.NewSource(0)
		for i := 0; i < nWrites; i++ {
			if tc.unorderedWrites {
				ts := rnd.Int63()
				writes = append(writes, entry{
					t: ts,
					s: fmt.Sprint("line:", ts),
				})
			} else {
				writes = append(writes, entry{
					t: int64(i),
					s: fmt.Sprint("line:", i),
				})
			}
		}

		b.Run(tc.desc, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				writeFn := tc.fn()
				for _, w := range writes {
					writeFn(w.t, w.s)
				}
			}
		})
	}
}

func TestUnorderedChunkIterators(t *testing.T) {
	c := NewMemChunk(EncSnappy, UnorderedHeadBlockFmt, testBlockSize, testTargetSize)
	for i := 0; i < 100; i++ {
		// push in reverse order
		require.Nil(t, c.Append(&logproto.Entry{
			Timestamp: time.Unix(int64(99-i), 0),
			Line:      fmt.Sprint(99 - i),
		}))

		// ensure we have a mix of cut blocks + head block.
		if i%30 == 0 {
			require.Nil(t, c.cut())
		}
	}

	// ensure head block has data
	require.Equal(t, false, c.head.IsEmpty())

	forward, err := c.Iterator(
		context.Background(),
		time.Unix(0, 0),
		time.Unix(100, 0),
		logproto.FORWARD,
		noopStreamPipeline,
	)
	require.Nil(t, err)

	backward, err := c.Iterator(
		context.Background(),
		time.Unix(0, 0),
		time.Unix(100, 0),
		logproto.BACKWARD,
		noopStreamPipeline,
	)
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
		require.Equal(t, time.Unix(int64(i), 0), forward.Entry().Timestamp)
		require.Equal(t, time.Unix(int64(99-i), 0), backward.Entry().Timestamp)
		require.Equal(t, float64(1), smpl.Sample().Value)
		require.Equal(t, time.Unix(int64(i), 0).UnixNano(), smpl.Sample().Timestamp)
	}
	require.Equal(t, false, forward.Next())
	require.Equal(t, false, backward.Next())
}
