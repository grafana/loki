package chunk

import (
	"fmt"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/config"
)

const userID = "userID"

var labelsForDummyChunks = labels.Labels{
	{Name: labels.MetricName, Value: "foo"},
	{Name: "bar", Value: "baz"},
	{Name: "toms", Value: "code"},
}

func dummyChunk(now model.Time) Chunk {
	return dummyChunkFor(now, labelsForDummyChunks)
}

func dummyChunkForEncoding(now model.Time, metric labels.Labels, samples int) Chunk {
	c, _ := NewForEncoding(Bigchunk)
	chunkStart := now.Add(-time.Hour)

	for i := 0; i < samples; i++ {
		t := time.Duration(i) * 15 * time.Second
		nc, err := c.(*bigchunk).Add(model.SamplePair{Timestamp: chunkStart.Add(t), Value: model.SampleValue(i)})
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
	return dummyChunkForEncoding(now, metric, 1)
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

			s := config.SchemaConfig{
				Configs: []config.PeriodConfig{
					{
						From:      config.DayTime{Time: 0},
						Schema:    "v11",
						RowShards: 16,
					},
				},
			}

			have, err := ParseExternalKey(userID, s.ExternalKey(c.chunk.ChunkRef))
			require.NoError(t, err)

			buf := make([]byte, len(encoded))
			copy(buf, encoded)
			if c.f != nil {
				c.f(&have, buf)
			}

			err = have.Decode(decodeContext, buf)
			require.Equal(t, c.err, errors.Cause(err))

			if c.err == nil {
				require.Equal(t, have.encoded, c.chunk.encoded)
			}
		})
	}
}

const fixedTimestamp = model.Time(1557654321000)

func TestChunkDecodeBackwardsCompatibility(t *testing.T) {
	// lets build a new chunk same as what was built using code at commit b1777a50ab19
	c, _ := NewForEncoding(Bigchunk)
	nc, err := c.(*bigchunk).Add(model.SamplePair{Timestamp: fixedTimestamp, Value: 0})
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
	rawData := []byte("\x00\x00\x00\xb7\xff\x06\x00\x00sNaPpY\x01\xa5\x00\x00\xfcB\xb4\xc9{\"fingerprint\":18245339272195143978,\"userID\":\"userID\",\"from\":1557650721,\"through\":1557654321,\"metric\":{\"__name__\":\"foo\",\"bar\":\"baz\",\"toms\":\"code\"},\"encoding\":0}\n\x00\x00\x00\x15\x01\x00\x11\x00\x00\x01\xd0\xdd\xf5\xb6\xd5Z\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	decodeContext := NewDecodeContext()
	have, err := ParseExternalKey(userID, "userID/fd3477666dacf92a:16aab37c8e8:16aab6eb768:70b431bb")
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

	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:      config.DayTime{Time: 0},
				Schema:    "v11",
				RowShards: 16,
			},
		},
	}
	require.Equal(t, s.ExternalKey(have.ChunkRef), s.ExternalKey(want.ChunkRef))
}

func TestParseExternalKey(t *testing.T) {
	for _, c := range []struct {
		key   string
		chunk Chunk
		err   error
	}{
		{key: userID + "/2:270d8f00:270d8f00:f84c5745", chunk: Chunk{
			ChunkRef: logproto.ChunkRef{
				UserID:      userID,
				Fingerprint: uint64(2),
				From:        model.Time(655200000),
				Through:     model.Time(655200000),
				Checksum:    4165752645,
			},
		}},

		{key: userID + "/2/270d8f00:270d8f00:f84c5745", chunk: Chunk{
			ChunkRef: logproto.ChunkRef{
				UserID:      userID,
				Fingerprint: uint64(2),
				From:        model.Time(655200000),
				Through:     model.Time(655200000),
				Checksum:    4165752645,
			},
		}},

		{key: "invalidUserID/2:270d8f00:270d8f00:f84c5745", chunk: Chunk{}, err: ErrWrongMetadata},
	} {
		chunk, err := ParseExternalKey(userID, c.key)
		require.Equal(t, c.err, errors.Cause(err))
		require.Equal(t, c.chunk, chunk)
	}
}

// BenchmarkLabels is a real example from Kubernetes' embedded cAdvisor metrics, lightly obfuscated
var BenchmarkLabels = labels.Labels{
	{Name: model.MetricNameLabel, Value: "container_cpu_usage_seconds_total"},
	{Name: "beta_kubernetes_io_arch", Value: "amd64"},
	{Name: "beta_kubernetes_io_instance_type", Value: "c3.somesize"},
	{Name: "beta_kubernetes_io_os", Value: "linux"},
	{Name: "container_name", Value: "some-name"},
	{Name: "cpu", Value: "cpu01"},
	{Name: "failure_domain_beta_kubernetes_io_region", Value: "somewhere-1"},
	{Name: "failure_domain_beta_kubernetes_io_zone", Value: "somewhere-1b"},
	{Name: "id", Value: "/kubepods/burstable/pod6e91c467-e4c5-11e7-ace3-0a97ed59c75e/a3c8498918bd6866349fed5a6f8c643b77c91836427fb6327913276ebc6bde28"},
	{Name: "image", Value: "registry/organisation/name@sha256:dca3d877a80008b45d71d7edc4fd2e44c0c8c8e7102ba5cbabec63a374d1d506"},
	{Name: "instance", Value: "ip-111-11-1-11.ec2.internal"},
	{Name: "job", Value: "kubernetes-cadvisor"},
	{Name: "kubernetes_io_hostname", Value: "ip-111-11-1-11"},
	{Name: "monitor", Value: "prod"},
	{Name: "name", Value: "k8s_some-name_some-other-name-5j8s8_kube-system_6e91c467-e4c5-11e7-ace3-0a97ed59c75e_0"},
	{Name: "namespace", Value: "kube-system"},
	{Name: "pod_name", Value: "some-other-name-5j8s8"},
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

func TestChunkKeys(t *testing.T) {
	for _, tc := range []struct {
		name      string
		chunk     Chunk
		schemaCfg config.SchemaConfig
	}{
		{
			name: "Legacy key (pre-checksum)",
			chunk: Chunk{
				ChunkRef: logproto.ChunkRef{
					Fingerprint: 100,
					UserID:      "fake",
					From:        model.TimeFromUnix(1000),
					Through:     model.TimeFromUnix(5000),
					Checksum:    12345,
				},
			},
			schemaCfg: config.SchemaConfig{
				Configs: []config.PeriodConfig{
					{
						From:      config.DayTime{Time: 0},
						Schema:    "v11",
						RowShards: 16,
					},
				},
			},
		},
		{
			name: "Newer key (post-v12)",
			chunk: Chunk{
				ChunkRef: logproto.ChunkRef{
					Fingerprint: 100,
					UserID:      "fake",
					From:        model.TimeFromUnix(1000),
					Through:     model.TimeFromUnix(5000),
					Checksum:    12345,
				},
			},
			schemaCfg: config.SchemaConfig{
				Configs: []config.PeriodConfig{
					{
						From:      config.DayTime{Time: 0},
						Schema:    "v12",
						RowShards: 16,
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := tc.schemaCfg.ExternalKey(tc.chunk.ChunkRef)
			newChunk, err := ParseExternalKey("fake", key)
			require.NoError(t, err)
			require.Equal(t, tc.chunk, newChunk)
			require.Equal(t, key, tc.schemaCfg.ExternalKey(newChunk.ChunkRef))
		})
	}
}

func BenchmarkParseNewerExternalKey(b *testing.B) {
	benchmarkParseExternalKey(b, "fake/57f628c7f6d57aad/162c699f000:162c69a07eb:eb242d99")
}

func BenchmarkParseNewExternalKey(b *testing.B) {
	benchmarkParseExternalKey(b, "fake/57f628c7f6d57aad:162c699f000:162c69a07eb:eb242d99")
}

func BenchmarkParseLegacyExternalKey(b *testing.B) {
	benchmarkParseExternalKey(b, "2:1484661279394:1484664879394")
}

func BenchmarkRootParseNewExternalKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := parseNewExternalKey("fake", "fake/57f628c7f6d57aad:162c699f000:162c69a07eb:eb242d99")
		require.NoError(b, err)
	}
}

func BenchmarkRootParseNewerExternalKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := parseNewerExternalKey("fake", "fake/57f628c7f6d57aad/162c699f000:162c69a07eb:eb242d99")
		require.NoError(b, err)
	}
}

func benchmarkParseExternalKey(b *testing.B, key string) {
	for i := 0; i < b.N; i++ {
		_, err := ParseExternalKey("fake", key)
		require.NoError(b, err)
	}
}
