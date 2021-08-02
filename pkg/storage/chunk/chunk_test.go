package chunk

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/storage/chunk/encoding"
)

const userID = "userID"

func init() {
	encoding.DefaultEncoding = encoding.Varbit
}

var labelsForDummyChunks = labels.Labels{
	{Name: labels.MetricName, Value: "foo"},
	{Name: "bar", Value: "baz"},
	{Name: "toms", Value: "code"},
}

func dummyChunk(now model.Time) Chunk {
	return dummyChunkFor(now, labelsForDummyChunks)
}

func dummyChunkForEncoding(now model.Time, metric labels.Labels, enc encoding.Encoding, samples int) Chunk {
	c, _ := encoding.NewForEncoding(enc)
	chunkStart := now.Add(-time.Hour)

	for i := 0; i < samples; i++ {
		t := time.Duration(i) * 15 * time.Second
		nc, err := c.Add(model.SamplePair{Timestamp: chunkStart.Add(t), Value: model.SampleValue(i)})
		if err != nil {
			panic(err)
		}
		if nc != nil {
			panic("returned chunk was not nil")
		}
	}

	chunk := NewChunk(
		userID,
		client.Fingerprint(metric),
		metric,
		c,
		chunkStart,
		now,
	)
	// Force checksum calculation.
	err := chunk.Encode()
	if err != nil {
		panic(err)
	}
	return chunk
}

func dummyChunkFor(now model.Time, metric labels.Labels) Chunk {
	return dummyChunkForEncoding(now, metric, encoding.Varbit, 1)
}

func TestChunkCodec(t *testing.T) {
	dummy := dummyChunk(model.Now())
	decodeContext := NewDecodeContext()
	for i, c := range []struct {
		chunk Chunk
		err   error
		f     func(*Chunk, []byte)
	}{
		// Basic round trip
		{chunk: dummy},

		// Checksum should fail
		{
			chunk: dummy,
			err:   ErrInvalidChecksum,
			f:     func(_ *Chunk, buf []byte) { buf[4]++ },
		},

		// Checksum should fail
		{
			chunk: dummy,
			err:   ErrInvalidChecksum,
			f:     func(c *Chunk, _ []byte) { c.Checksum = 123 },
		},

		// Metadata test should fail
		{
			chunk: dummy,
			err:   ErrWrongMetadata,
			f:     func(c *Chunk, _ []byte) { c.Fingerprint++ },
		},

		// Metadata test should fail
		{
			chunk: dummy,
			err:   ErrWrongMetadata,
			f:     func(c *Chunk, _ []byte) { c.UserID = "foo" },
		},
	} {
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
			err := c.chunk.Encode()
			require.NoError(t, err)
			encoded, err := c.chunk.Encoded()
			require.NoError(t, err)

			have, err := ParseExternalKey(userID, c.chunk.ExternalKey())
			require.NoError(t, err)

			buf := make([]byte, len(encoded))
			copy(buf, encoded)
			if c.f != nil {
				c.f(&have, buf)
			}

			err = have.Decode(decodeContext, buf)
			require.Equal(t, c.err, errors.Cause(err))

			if c.err == nil {
				require.Equal(t, have, c.chunk)
			}
		})
	}
}

const fixedTimestamp = model.Time(1557654321000)

func TestChunkDecodeBackwardsCompatibility(t *testing.T) {
	// lets build a new chunk same as what was built using code at commit b1777a50ab19
	c, _ := encoding.NewForEncoding(encoding.Bigchunk)
	nc, err := c.Add(model.SamplePair{Timestamp: fixedTimestamp, Value: 0})
	require.NoError(t, err)
	require.Equal(t, nil, nc, "returned chunk should be nil")

	chunk := NewChunk(
		userID,
		client.Fingerprint(labelsForDummyChunks),
		labelsForDummyChunks,
		c,
		fixedTimestamp.Add(-time.Hour),
		fixedTimestamp,
	)
	// Force checksum calculation.
	require.NoError(t, chunk.Encode())

	// Chunk encoded using code at commit b1777a50ab19
	rawData := []byte("\x00\x00\x00\xb7\xff\x06\x00\x00sNaPpY\x01\xa5\x00\x00\x04\xc7a\xba{\"fingerprint\":18245339272195143978,\"userID\":\"userID\",\"from\":1557650721,\"through\":1557654321,\"metric\":{\"bar\":\"baz\",\"toms\":\"code\",\"__name__\":\"foo\"},\"encoding\":3}\n\x00\x00\x00\x15\x01\x00\x11\x00\x00\x01\xd0\xdd\xf5\xb6\xd5Z\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	decodeContext := NewDecodeContext()
	have, err := ParseExternalKey(userID, "userID/fd3477666dacf92a:16aab37c8e8:16aab6eb768:38eb373c")
	require.NoError(t, err)
	require.NoError(t, have.Decode(decodeContext, rawData))
	want := chunk
	// We can't just compare these two chunks, since the Bigchunk internals are different on construction and read-in.
	// Compare the serialised version instead
	require.NoError(t, have.Encode())
	require.NoError(t, want.Encode())
	haveEncoded, _ := have.Encoded()
	wantEncoded, _ := want.Encoded()
	require.Equal(t, haveEncoded, wantEncoded)
	require.Equal(t, have.ExternalKey(), want.ExternalKey())
}

func TestParseExternalKey(t *testing.T) {
	for _, c := range []struct {
		key   string
		chunk Chunk
		err   error
	}{
		{key: "2:1484661279394:1484664879394", chunk: Chunk{
			UserID:      userID,
			Fingerprint: model.Fingerprint(2),
			From:        model.Time(1484661279394),
			Through:     model.Time(1484664879394),
		}},

		{key: userID + "/2:270d8f00:270d8f00:f84c5745", chunk: Chunk{
			UserID:      userID,
			Fingerprint: model.Fingerprint(2),
			From:        model.Time(655200000),
			Through:     model.Time(655200000),
			ChecksumSet: true,
			Checksum:    4165752645,
		}},

		{key: "invalidUserID/2:270d8f00:270d8f00:f84c5745", chunk: Chunk{}, err: ErrWrongMetadata},
	} {
		chunk, err := ParseExternalKey(userID, c.key)
		require.Equal(t, c.err, errors.Cause(err))
		require.Equal(t, c.chunk, chunk)
	}
}

func TestChunksToMatrix(t *testing.T) {
	// Create 2 chunks which have the same metric
	now := model.Now()
	chunk1 := dummyChunkFor(now, labelsForDummyChunks)
	chunk1Samples, err := chunk1.Samples(chunk1.From, chunk1.Through)
	require.NoError(t, err)
	chunk2 := dummyChunkFor(now, labelsForDummyChunks)
	chunk2Samples, err := chunk2.Samples(chunk2.From, chunk2.Through)
	require.NoError(t, err)

	ss1 := &model.SampleStream{
		Metric: util.LabelsToMetric(chunk1.Metric),
		Values: util.MergeSampleSets(chunk1Samples, chunk2Samples),
	}

	// Create another chunk with a different metric
	otherMetric := labels.Labels{
		{Name: model.MetricNameLabel, Value: "foo2"},
		{Name: "bar", Value: "baz"},
		{Name: "toms", Value: "code"},
	}
	chunk3 := dummyChunkFor(now, otherMetric)
	chunk3Samples, err := chunk3.Samples(chunk3.From, chunk3.Through)
	require.NoError(t, err)

	ss2 := &model.SampleStream{
		Metric: util.LabelsToMetric(chunk3.Metric),
		Values: chunk3Samples,
	}

	for _, c := range []struct {
		chunks         []Chunk
		expectedMatrix model.Matrix
	}{
		{
			chunks:         []Chunk{},
			expectedMatrix: model.Matrix{},
		}, {
			chunks: []Chunk{
				chunk1,
				chunk2,
				chunk3,
			},
			expectedMatrix: model.Matrix{
				ss1,
				ss2,
			},
		},
	} {
		matrix, err := ChunksToMatrix(context.Background(), c.chunks, chunk1.From, chunk3.Through)
		require.NoError(t, err)

		sort.Sort(matrix)
		sort.Sort(c.expectedMatrix)
		require.Equal(t, c.expectedMatrix, matrix)
	}
}

func benchmarkChunk(now model.Time) Chunk {
	return dummyChunkFor(now, BenchmarkLabels)
}

func BenchmarkEncode(b *testing.B) {
	chunk := dummyChunk(model.Now())

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		chunk.encoded = nil
		err := chunk.Encode()
		require.NoError(b, err)
	}
}

func BenchmarkDecode1(b *testing.B)     { benchmarkDecode(b, 1) }
func BenchmarkDecode100(b *testing.B)   { benchmarkDecode(b, 100) }
func BenchmarkDecode10000(b *testing.B) { benchmarkDecode(b, 10000) }

func benchmarkDecode(b *testing.B, batchSize int) {
	chunk := benchmarkChunk(model.Now())
	err := chunk.Encode()
	require.NoError(b, err)
	buf, err := chunk.Encoded()
	require.NoError(b, err)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decodeContext := NewDecodeContext()
		b.StopTimer()
		chunks := make([]Chunk, batchSize)
		// Copy across the metadata so the check works out ok
		for j := 0; j < batchSize; j++ {
			chunks[j] = chunk
			chunks[j].Metric = nil
			chunks[j].Data = nil
		}
		b.StartTimer()
		for j := 0; j < batchSize; j++ {
			err := chunks[j].Decode(decodeContext, buf)
			require.NoError(b, err)
		}
	}
}

func TestChunk_Slice(t *testing.T) {
	chunkEndTime := model.Now()
	chunkStartTime := chunkEndTime.Add(-time.Hour)

	for _, tc := range []struct {
		name       string
		sliceRange model.Interval
		err        error
	}{
		{
			name:       "slice first 10 mins",
			sliceRange: model.Interval{Start: chunkStartTime, End: chunkStartTime.Add(10 * time.Minute)},
		},
		{
			name:       "slice last 10 mins",
			sliceRange: model.Interval{Start: chunkEndTime.Add(-10 * time.Minute), End: chunkEndTime},
		},
		{
			name:       "slice in the middle",
			sliceRange: model.Interval{Start: chunkStartTime.Add(20 * time.Minute), End: chunkEndTime.Add(-20 * time.Minute)},
		},
		{
			name:       "slice out of range",
			sliceRange: model.Interval{Start: chunkEndTime.Add(20 * time.Minute), End: chunkEndTime.Add(30 * time.Minute)},
			err:        ErrSliceOutOfRange,
		},
		{
			name:       "slice no data in range",
			sliceRange: model.Interval{Start: chunkStartTime.Add(time.Second), End: chunkStartTime.Add(10 * time.Second)},
			err:        encoding.ErrSliceNoDataInRange,
		},
		{
			name:       "slice interval not aligned with sample intervals",
			sliceRange: model.Interval{Start: chunkStartTime.Add(time.Second), End: chunkStartTime.Add(10 * time.Minute).Add(10 * time.Second)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// dummy chunk is created with time range chunkEndTime-1hour to chunkEndTime
			originalChunk := dummyChunkForEncoding(chunkEndTime, labelsForDummyChunks, encoding.DefaultEncoding, 241)

			newChunk, err := originalChunk.Slice(tc.sliceRange.Start, tc.sliceRange.End)
			if tc.err != nil {
				require.Equal(t, tc.err, err)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tc.sliceRange.Start, newChunk.From)
			require.Equal(t, tc.sliceRange.End, newChunk.Through)

			chunkItr := originalChunk.Data.NewIterator(nil)
			chunkItr.FindAtOrAfter(tc.sliceRange.Start)

			newChunkItr := newChunk.Data.NewIterator(nil)
			newChunkItr.Scan()

			for {
				require.Equal(t, chunkItr.Value(), newChunkItr.Value())

				originalChunksHasMoreSamples := chunkItr.Scan()
				newChunkHasMoreSamples := newChunkItr.Scan()

				// originalChunk and newChunk both should end at same time or newChunk should end before or at slice end time
				if !originalChunksHasMoreSamples || chunkItr.Value().Timestamp > tc.sliceRange.End {
					require.Equal(t, false, newChunkHasMoreSamples)
					break
				}

				require.Equal(t, true, newChunkHasMoreSamples)
			}
		})
	}
}

func Benchmark_ParseExternalKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := ParseExternalKey("fake", "fake/57f628c7f6d57aad:162c699f000:162c69a07eb:eb242d99")
		require.NoError(b, err)
	}
}
