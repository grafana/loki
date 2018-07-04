package chunkenc

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math"
	"testing"

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
			ts:  9,
			str: "",
		},
	}

	for _, c := range cases {
		require.NoError(t, chk.Append(c.ts, c.str))
		if c.cut {
			require.NoError(t, chk.cut())
		}
	}

	it, err := chk.Iterator(0, math.MaxInt64)
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
		it, err := chk.Iterator(3, 7)
		require.NoError(t, err)

		idx := 2
		for it.Next() {
			e := it.Entry()
			require.Equal(t, cases[idx].ts, e.Timestamp.UnixNano())
			require.Equal(t, cases[idx].str, e.Line)
			idx++
		}
		require.NoError(t, it.Error())
		require.Equal(t, 7, idx)
	})
}

func TestGZIPCompression(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	b, err := ioutil.ReadFile("NASA_access_log_Aug95")
	if err != nil {
		t.SkipNow()
	}

	lines := bytes.Split(b, []byte("\n"))
	fmt.Println(len(lines))

	for _, blockSize := range []int{4 * 1024, 8 * 1024, 16 * 1024, 32 * 1024, 64 * 1024, 128 * 1024, 256 * 1024, 512 * 1024} {
		testName := fmt.Sprintf("%d", blockSize/1024)
		t.Run(testName, func(t *testing.T) {
			chk := NewMemChunk(EncGZIP)
			chk.blockSize = blockSize

			for i, l := range lines {
				require.NoError(t, chk.Append(int64(i), string(l)))
			}

			b2, err := chk.Bytes()
			require.NoError(t, err)
			fmt.Println(float64(len(b))/(1024*1024), float64(len(b2))/(1024*1024), float64(len(b2))/float64(len(chk.blocks)))

			it, err := chk.Iterator(0, math.MaxInt64)
			require.NoError(t, err)

			for i, l := range lines {
				require.True(t, it.Next())

				e := it.Entry()
				require.Equal(t, int64(i), e.Timestamp.UnixNano())
				require.Equal(t, string(l), e.Line)
			}
			require.NoError(t, it.Error())
		})
	}
}

func TestGZIPSerialisation(t *testing.T) {
	chk := NewMemChunk(EncGZIP)

	numSamples := 500000

	for i := 0; i < numSamples; i++ {
		require.NoError(t, chk.Append(int64(i), string(i)))
	}

	byt, err := chk.Bytes()
	require.NoError(t, err)

	bc, err := NewByteChunk(byt)
	require.NoError(t, err)

	it, err := bc.Iterator(0, math.MaxInt64)
	require.NoError(t, err)
	for i := 0; i < numSamples; i++ {
		require.True(t, it.Next())

		e := it.Entry()
		require.Equal(t, int64(i), e.Timestamp.UnixNano())
		require.Equal(t, string(i), e.Line)
	}

	require.NoError(t, it.Error())
}
