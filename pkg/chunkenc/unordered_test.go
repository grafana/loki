package chunkenc

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func iterEq(t *testing.T, exp []entry, got iter.EntryIterator, dir logproto.Direction) {
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
				hb.append(e.t, e.s)
			}

			itr := hb.iterator(
				context.Background(),
				tc.dir,
				0,
				math.MaxInt64,
				noopStreamPipeline,
			)

			iterEq(t, tc.exp, itr, tc.dir)
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
				hb.append(e.t, e.s)
			}

			itr := hb.iterator(
				context.Background(),
				tc.dir,
				tc.mint,
				tc.maxt,
				noopStreamPipeline,
			)

			iterEq(t, tc.exp, itr, tc.dir)
		})
	}
}
