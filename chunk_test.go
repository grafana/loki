package chunk

import (
	"context"
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
	decodeContext := NewDecodeContext()
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

			err = have.Decode(decodeContext, buf)
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
		matrix, err := chunksToMatrix(context.Background(), c.chunks)
		require.NoError(t, err)

		sort.Sort(matrix)
		sort.Sort(c.expectedMatrix)
		require.Equal(t, c.expectedMatrix, matrix)
	}
}

func benchmarkChunk() Chunk {
	// This is a real example from Kubernetes' embedded cAdvisor metrics, lightly obfuscated
	return dummyChunkFor(model.Metric{
		model.MetricNameLabel:              "container_cpu_usage_seconds_total",
		"beta_kubernetes_io_arch":          "amd64",
		"beta_kubernetes_io_instance_type": "c3.somesize",
		"beta_kubernetes_io_os":            "linux",
		"container_name":                   "some-name",
		"cpu":                              "cpu01",
		"failure_domain_beta_kubernetes_io_region": "somewhere-1",
		"failure_domain_beta_kubernetes_io_zone":   "somewhere-1b",
		"id":       "/kubepods/burstable/pod6e91c467-e4c5-11e7-ace3-0a97ed59c75e/a3c8498918bd6866349fed5a6f8c643b77c91836427fb6327913276ebc6bde28",
		"image":    "registry/organisation/name@sha256:dca3d877a80008b45d71d7edc4fd2e44c0c8c8e7102ba5cbabec63a374d1d506",
		"instance": "ip-111-11-1-11.ec2.internal",
		"job":      "kubernetes-cadvisor",
		"kubernetes_io_hostname": "ip-111-11-1-11",
		"monitor":                "prod",
		"name":                   "k8s_some-name_some-other-name-5j8s8_kube-system_6e91c467-e4c5-11e7-ace3-0a97ed59c75e_0",
		"namespace":              "kube-system",
		"pod_name":               "some-other-name-5j8s8",
	})
}

func BenchmarkEncode(b *testing.B) {
	chunk := dummyChunk()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		chunk.Encode()
	}
}

func BenchmarkDecode1(b *testing.B)     { benchmarkDecode(b, 1) }
func BenchmarkDecode100(b *testing.B)   { benchmarkDecode(b, 100) }
func BenchmarkDecode10000(b *testing.B) { benchmarkDecode(b, 10000) }

func benchmarkDecode(b *testing.B, batchSize int) {
	chunk := benchmarkChunk()
	buf, err := chunk.Encode()
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
