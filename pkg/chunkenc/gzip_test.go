package chunkenc

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGZIPBlock(t *testing.T) {
	b := NewMemChunk(EncGZIP)

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
		require.NoError(t, b.Append(c.ts, c.str))
		if c.cut {
			require.NoError(t, b.app.cut())
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

func TestGZIPCompression(t *testing.T) {
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

			it := chk.Iterator()

			for i, l := range lines {
				require.True(t, it.Next())

				ts, str := it.At()
				require.Equal(t, int64(i), ts)
				require.Equal(t, string(l), str)
			}
			require.NoError(t, it.Err())
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

	it := bc.Iterator()
	for i := 0; i < numSamples; i++ {
		require.True(t, it.Next())

		ts, str := it.At()
		require.Equal(t, int64(i), ts)
		require.Equal(t, string(i), str)
	}

	require.NoError(t, it.Err())
}
