package dataset

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

var testStrings = []string{
	"hello",
	"world",
	"foo",
	"bar",
	"baz",
}

func Test_plainEncoder(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc = newPlainEncoder(&buf)
		dec = newPlainDecoder(&buf)
	)

	for _, v := range testStrings {
		require.NoError(t, enc.Encode(StringValue(v)))
	}

	var out []string

	for {
		str, err := dec.Decode()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		out = append(out, str.String())
	}

	require.Equal(t, testStrings, out)
}

func Test_plainEncoder_partialRead(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc = newPlainEncoder(&buf)
		dec = newPlainDecoder(&oneByteReader{&buf})
	)

	for _, v := range testStrings {
		require.NoError(t, enc.Encode(StringValue(v)))
	}

	var out []string

	for {
		str, err := dec.Decode()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		out = append(out, str.String())
	}

	require.Equal(t, testStrings, out)
}

func Benchmark_plainEncoder_Append(b *testing.B) {
	enc := newPlainEncoder(streamio.Discard)

	for i := 0; i < b.N; i++ {
		for _, v := range testStrings {
			_ = enc.Encode(StringValue(v))
		}
	}
}

func Benchmark_plainDecoder_Decode(b *testing.B) {
	buf := bytes.NewBuffer(make([]byte, 0, 1024)) // Large enough to avoid reallocations.

	var (
		enc = newPlainEncoder(buf)
		dec = newPlainDecoder(buf)
	)

	for _, v := range testStrings {
		require.NoError(b, enc.Encode(StringValue(v)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for {
			var err error
			_, err = dec.Decode()
			if errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// oneByteReader is like iotest.OneByteReader but it supports ReadByte.
type oneByteReader struct {
	r streamio.Reader
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return r.r.Read(p[0:1])
}

func (r *oneByteReader) ReadByte() (byte, error) {
	return r.r.ReadByte()
}
