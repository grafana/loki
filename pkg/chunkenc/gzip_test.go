package chunkenc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGZIPBlock(t *testing.T) {
	b := NewMemChunk(EncGZIP)

	app, err := b.Appender()
	require.NoError(t, err)

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
			str: "hello, world8!",
		},
		{
			ts:  9,
			str: "",
		},
	}

	for _, c := range cases {
		require.NoError(t, app.Append(c.ts, c.str))
		if c.cut {
			require.NoError(t, b.app.cut())
			app, err = b.Appender()
			require.NoError(t, err)
		}
	}

	it := b.Iterator()
	idx := 0
	for it.Next() {
		ts, str := it.At()
		require.Equal(t, cases[idx].ts, ts)
		require.Equal(t, cases[idx].str, str)
		idx++
	}

	require.NoError(t, it.Err())
	require.Equal(t, len(cases), idx)
}
