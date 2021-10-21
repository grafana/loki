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
	for _, encoding := range []Encoding{Bigchunk, PrometheusXorChunk} {
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

			cs, err := c.Add(model.SamplePair{
				Timestamp: model.Time(i),
				Value:     model.SampleValue(i),
			})
			require.NoError(t, err)
			require.Nil(t, cs)
		}
	}
}

var step = int(15 * time.Second / time.Millisecond)

func TestChunk(t *testing.T) {
	for _, tc := range []struct {
		encoding   Encoding
		maxSamples int
	}{

		{Bigchunk, 4096},
		{PrometheusXorChunk, 2048},
	} {
		for samples := tc.maxSamples / 10; samples < tc.maxSamples; samples += tc.maxSamples / 10 {

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

			if tc.encoding != PrometheusXorChunk {
				t.Run(fmt.Sprintf("testChunkRebound/%s/%d", tc.encoding.String(), samples), func(t *testing.T) {
					testChunkRebound(t, tc.encoding, samples)
				})
			}
		}
	}
}

func mkChunk(t *testing.T, encoding Encoding, samples int) Chunk {
	chunk, err := NewForEncoding(encoding)
	require.NoError(t, err)

	for i := 0; i < samples; i++ {
		newChunk, err := chunk.Add(model.SamplePair{
			Timestamp: model.Time(i * step),
			Value:     model.SampleValue(i),
		})
		require.NoError(t, err)
		require.Nil(t, newChunk)
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
	require.NoError(t, err)

	err = chunk.UnmarshalFromBuf(bs1)
	require.NoError(t, err)

	// Check all the samples are in there.
	iter := chunk.NewIterator(nil)
	for i := 0; i < samples; i++ {
		require.True(t, iter.Scan())
		sample := iter.Value()
		require.EqualValues(t, model.Time(i*step), sample.Timestamp)
		require.EqualValues(t, model.SampleValue(i), sample.Value)
	}
	require.False(t, iter.Scan())
	require.NoError(t, iter.Err())

	// Check seek works after unmarshal
	iter = chunk.NewIterator(iter)
	for i := 0; i < samples; i += samples / 10 {
		require.True(t, iter.FindAtOrAfter(model.Time(i*step)))
	}

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

	iter := chunk.NewIterator(nil)
	for i := 0; i < samples; i += samples / 10 {
		if i > 0 {
			// Seek one millisecond before the actual time
			require.True(t, iter.FindAtOrAfter(model.Time(i*step-1)), "1ms before step %d not found", i)
			sample := iter.Value()
			require.EqualValues(t, model.Time(i*step), sample.Timestamp)
			require.EqualValues(t, model.SampleValue(i), sample.Value)
		}
		// Now seek to exactly the right time
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
	// Check seek past the end of the chunk returns failure
	require.False(t, iter.FindAtOrAfter(model.Time(samples*step+1)))
}

func testChunkSeekForward(t *testing.T, encoding Encoding, samples int) {
	chunk := mkChunk(t, encoding, samples)

	iter := chunk.NewIterator(nil)
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
	iter := chunk.NewIterator(nil)
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

func testChunkRebound(t *testing.T, encoding Encoding, samples int) {
	for _, tc := range []struct {
		name               string
		sliceFrom, sliceTo model.Time
		err                error
	}{
		{
			name:      "slice first half",
			sliceFrom: 0,
			sliceTo:   model.Time((samples / 2) * step),
		},
		{
			name:      "slice second half",
			sliceFrom: model.Time((samples / 2) * step),
			sliceTo:   model.Time((samples - 1) * step),
		},
		{
			name:      "slice in the middle",
			sliceFrom: model.Time(int(float64(samples)*0.25) * step),
			sliceTo:   model.Time(int(float64(samples)*0.75) * step),
		},
		{
			name:      "slice no data in range",
			err:       ErrSliceNoDataInRange,
			sliceFrom: model.Time((samples + 1) * step),
			sliceTo:   model.Time(samples * 2 * step),
		},
		{
			name:      "slice interval not aligned with sample intervals",
			sliceFrom: model.Time(0 + step/2),
			sliceTo:   model.Time(samples * step).Add(time.Duration(-step / 2)),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalChunk := mkChunk(t, encoding, samples)

			newChunk, err := originalChunk.Rebound(tc.sliceFrom, tc.sliceTo)
			if tc.err != nil {
				require.Equal(t, tc.err, err)
				return
			}
			require.NoError(t, err)

			chunkItr := originalChunk.NewIterator(nil)
			chunkItr.FindAtOrAfter(tc.sliceFrom)

			newChunkItr := newChunk.NewIterator(nil)
			newChunkItr.Scan()

			for {
				require.Equal(t, chunkItr.Value(), newChunkItr.Value())

				originalChunksHasMoreSamples := chunkItr.Scan()
				newChunkHasMoreSamples := newChunkItr.Scan()

				// originalChunk and newChunk both should end at same time or newChunk should end before or at slice end time
				if !originalChunksHasMoreSamples || chunkItr.Value().Timestamp > tc.sliceTo {
					require.False(t, newChunkHasMoreSamples)
					break
				}

				require.True(t, newChunkHasMoreSamples)
			}
		})
	}
}
