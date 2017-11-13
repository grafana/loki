package chunk

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/cortex/pkg/prom1/storage/local/chunk"
	"github.com/weaveworks/cortex/pkg/util"
)

const userID = "userID"

func dummyChunk() Chunk {
	return dummyChunkFor(model.Metric{
		model.MetricNameLabel: "foo",
		"bar":  "baz",
		"toms": "code",
	})
}

func dummyChunkFor(metric model.Metric) Chunk {
	now := model.Now()
	cs, _ := chunk.New().Add(model.SamplePair{Timestamp: now, Value: 0})
	chunk := NewChunk(
		userID,
		metric.Fingerprint(),
		metric,
		cs[0],
		now.Add(-time.Hour),
		now,
	)
	// Force checksum calculation.
	_, err := chunk.Encode()
	if err != nil {
		panic(err)
	}
	return chunk
}

func TestChunkCodec(t *testing.T) {
	for i, c := range []struct {
		chunk Chunk
		err   error
		f     func(*Chunk, []byte)
	}{
		// Basic round trip
		{chunk: dummyChunk()},

		// Checksum should fail
		{
			chunk: dummyChunk(),
			err:   ErrInvalidChecksum,
			f:     func(_ *Chunk, buf []byte) { buf[4]++ },
		},

		// Checksum should fail
		{
			chunk: dummyChunk(),
			err:   ErrInvalidChecksum,
			f:     func(c *Chunk, _ []byte) { c.Checksum = 123 },
		},

		// Metadata test should fail
		{
			chunk: dummyChunk(),
			err:   ErrWrongMetadata,
			f:     func(c *Chunk, _ []byte) { c.Fingerprint++ },
		},

		// Metadata test should fail
		{
			chunk: dummyChunk(),
			err:   ErrWrongMetadata,
			f:     func(c *Chunk, _ []byte) { c.UserID = "foo" },
		},
	} {
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
			buf, err := c.chunk.Encode()
			require.NoError(t, err)

			have, err := parseExternalKey(userID, c.chunk.ExternalKey())
			require.NoError(t, err)

			if c.f != nil {
				c.f(&have, buf)
			}

			err = have.Decode(buf)
			require.Equal(t, c.err, errors.Cause(err))

			if c.err == nil {
				require.Equal(t, have, c.chunk)
			}
		})
	}
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
		chunk, err := parseExternalKey(userID, c.key)
		require.Equal(t, c.err, errors.Cause(err))
		require.Equal(t, c.chunk, chunk)
	}
}

func TestChunksToMatrix(t *testing.T) {
	// Create 2 chunks which have the same metric
	metric := model.Metric{
		model.MetricNameLabel: "foo",
		"bar":  "baz",
		"toms": "code",
	}
	chunk1 := dummyChunkFor(metric)
	chunk1Samples, err := chunk1.Samples()
	require.NoError(t, err)
	chunk2 := dummyChunkFor(metric)
	chunk2Samples, err := chunk2.Samples()
	require.NoError(t, err)

	ss1 := &model.SampleStream{
		Metric: chunk1.Metric,
		Values: util.MergeSampleSets(chunk1Samples, chunk2Samples),
	}

	// Create another chunk with a different metric
	otherMetric := model.Metric{
		model.MetricNameLabel: "foo2",
		"bar":  "baz",
		"toms": "code",
	}
	chunk3 := dummyChunkFor(otherMetric)
	chunk3Samples, err := chunk3.Samples()
	require.NoError(t, err)

	ss2 := &model.SampleStream{
		Metric: chunk3.Metric,
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
		matrix, err := chunksToMatrix(c.chunks)
		require.NoError(t, err)

		sort.Sort(matrix)
		sort.Sort(c.expectedMatrix)
		require.Equal(t, c.expectedMatrix, matrix)
	}
}
