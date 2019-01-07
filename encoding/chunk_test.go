// Copyright 2016 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Note: this file has tests for code in both delta.go and doubledelta.go --
// it may make sense to split those out later, but given that the tests are
// near-identical and share a helper, this feels simpler for now.

package encoding

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestLen(t *testing.T) {
	chunks := []Chunk{}
	for _, encoding := range []Encoding{Delta, DoubleDelta, Varbit} {
		c, err := NewForEncoding(encoding)
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, c)
	}

	for _, c := range chunks {
		for i := 0; i <= 10; i++ {
			if c.Len() != i {
				t.Errorf("chunk type %s should have %d samples, had %d", c.Encoding(), i, c.Len())
			}

			cs, _ := c.Add(model.SamplePair{
				Timestamp: model.Time(i),
				Value:     model.SampleValue(i),
			})
			c = cs[0]
		}
	}
}

var step = int(15 * time.Second / time.Millisecond)

func TestChunk(t *testing.T) {
	alwaysMarshalFullsizeChunks = false
	for _, tc := range []struct {
		encoding   Encoding
		maxSamples int
	}{
		{DoubleDelta, 989},
		{Varbit, 2048},
		{Bigchunk, 4096},
	} {
		for samples := tc.maxSamples / 10; samples < tc.maxSamples; samples += tc.maxSamples / 10 {

			// DoubleDelta doesn't support zero length chunks.
			if tc.encoding == DoubleDelta && samples == 0 {
				continue
			}

			t.Run(fmt.Sprintf("testChunkEncoding/%s/%d", tc.encoding.String(), samples), func(t *testing.T) {
				testChunkEncoding(t, tc.encoding, samples)
			})

			t.Run(fmt.Sprintf("testChunkSeek/%s/%d", tc.encoding.String(), samples), func(t *testing.T) {
				testChunkSeek(t, tc.encoding, samples)
			})

			t.Run(fmt.Sprintf("testChunkSeekForward/%s/%d", tc.encoding.String(), samples), func(t *testing.T) {
				testChunkSeekForward(t, tc.encoding, samples)
			})

			t.Run(fmt.Sprintf("testChunkBatch/%s/%d", tc.encoding.String(), samples), func(t *testing.T) {
				testChunkBatch(t, tc.encoding, samples)
			})
		}
	}
}

func mkChunk(t *testing.T, encoding Encoding, samples int) Chunk {
	chunk, err := NewForEncoding(encoding)
	require.NoError(t, err)

	for i := 0; i < samples; i++ {
		chunks, err := chunk.Add(model.SamplePair{
			Timestamp: model.Time(i * step),
			Value:     model.SampleValue(i),
		})
		require.NoError(t, err)
		require.Len(t, chunks, 1)
		chunk = chunks[0]
	}

	return chunk
}

// testChunkEncoding checks chunks roundtrip and contain all their samples.
func testChunkEncoding(t *testing.T, encoding Encoding, samples int) {
	chunk := mkChunk(t, encoding, samples)

	var buf bytes.Buffer
	err := chunk.Marshal(&buf)
	require.NoError(t, err)

	bs1 := buf.Bytes()
	chunk, err = NewForEncoding(encoding)
	err = chunk.UnmarshalFromBuf(bs1)
	require.NoError(t, err)

	// Check all the samples are in there.
	iter := chunk.NewIterator()
	for i := 0; i < samples; i++ {
		require.True(t, iter.Scan())
		sample := iter.Value()
		require.EqualValues(t, model.Time(i*step), sample.Timestamp)
		require.EqualValues(t, model.SampleValue(i), sample.Value)
	}
	require.False(t, iter.Scan())
	require.NoError(t, iter.Err())

	// Check the byte representation after another Marshall is the same.
	buf = bytes.Buffer{}
	err = chunk.Marshal(&buf)
	require.NoError(t, err)
	bs2 := buf.Bytes()

	require.Equal(t, bs1, bs2)
}

// testChunkSeek checks seek works as expected.
// This version of the test will seek backwards.
func testChunkSeek(t *testing.T, encoding Encoding, samples int) {
	chunk := mkChunk(t, encoding, samples)

	iter := chunk.NewIterator()
	for i := 0; i < samples; i += samples / 10 {
		require.True(t, iter.FindAtOrAfter(model.Time(i*step)))
		sample := iter.Value()
		require.EqualValues(t, model.Time(i*step), sample.Timestamp)
		require.EqualValues(t, model.SampleValue(i), sample.Value)

		j := i + 1
		for ; j < samples; j++ {
			require.True(t, iter.Scan())
			sample := iter.Value()
			require.EqualValues(t, model.Time(j*step), sample.Timestamp)
			require.EqualValues(t, model.SampleValue(j), sample.Value)
		}
		require.False(t, iter.Scan())
		require.NoError(t, iter.Err())
	}
}

func testChunkSeekForward(t *testing.T, encoding Encoding, samples int) {
	chunk := mkChunk(t, encoding, samples)

	iter := chunk.NewIterator()
	for i := 0; i < samples; i += samples / 10 {
		require.True(t, iter.FindAtOrAfter(model.Time(i*step)))
		sample := iter.Value()
		require.EqualValues(t, model.Time(i*step), sample.Timestamp)
		require.EqualValues(t, model.SampleValue(i), sample.Value)

		j := i + 1
		for ; j < (i+samples/10) && j < samples; j++ {
			require.True(t, iter.Scan())
			sample := iter.Value()
			require.EqualValues(t, model.Time(j*step), sample.Timestamp)
			require.EqualValues(t, model.SampleValue(j), sample.Value)
		}
	}
	require.False(t, iter.Scan())
	require.NoError(t, iter.Err())
}

func testChunkBatch(t *testing.T, encoding Encoding, samples int) {
	chunk := mkChunk(t, encoding, samples)

	// Check all the samples are in there.
	iter := chunk.NewIterator()
	for i := 0; i < samples; {
		require.True(t, iter.Scan())
		batch := iter.Batch(BatchSize)
		for j := 0; j < batch.Length; j++ {
			require.EqualValues(t, int64((i+j)*step), batch.Timestamps[j])
			require.EqualValues(t, float64(i+j), batch.Values[j])
		}
		i += batch.Length
	}
	require.False(t, iter.Scan())
	require.NoError(t, iter.Err())
}
