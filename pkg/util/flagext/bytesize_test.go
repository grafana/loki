package flagext

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func Test_ByteSize(t *testing.T) {
	for _, tc := range []struct {
		in  string
		err bool
		out int
	}{
		{
			in:  "abc",
			err: true,
		},
		{
			in:  "",
			err: false,
			out: 0,
		},
		{
			in:  "0",
			err: false,
			out: 0,
		},
		{
			in:  "1b",
			err: false,
			out: 1,
		},
		{
			in:  "100kb",
			err: false,
			out: 100 << 10,
		},
		{
			in:  "100 KB",
			err: false,
			out: 100 << 10,
		},
		{
			// ensure lowercase works
			in:  "50mb",
			err: false,
			out: 50 << 20,
		},
		{
			// ensure mixed capitalization works
			in:  "50Mb",
			err: false,
			out: 50 << 20,
		},
		{
			in:  "256GB",
			err: false,
			out: 256 << 30,
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			var bs ByteSize

			err := bs.Set(tc.in)
			if tc.err {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
				require.Equal(t, tc.out, bs.Get().(int))
			}

		})
	}
}

func Test_ByteSizeYAML(t *testing.T) {
	for _, tc := range []struct {
		in  string
		err bool
		out ByteSize
	}{
		{
			in:  "256GB",
			out: ByteSize(256 << 30),
		},
		{
			in:  "abc",
			err: true,
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			var out ByteSize
			err := yaml.Unmarshal([]byte(tc.in), &out)
			if tc.err {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
				require.Equal(t, tc.out, out)
			}
		})
	}
}
