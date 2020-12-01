package ingester

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
)

func TestMaxReturnedStreamsErrors(t *testing.T) {
	numLogs := 100

	tt := []struct {
		name       string
		limit      int
		expectErrs int
	}{
		{"10", 10, 10},
		{"unlimited", 0, numLogs},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.MaxReturnedErrors = tc.limit
			s := newStream(
				cfg,
				model.Fingerprint(0),
				labels.Labels{
					{Name: "foo", Value: "bar"},
				},
				NilMetrics,
			)

			err := s.Push(context.Background(), []logproto.Entry{
				{Timestamp: time.Unix(int64(numLogs), 0), Line: "log"},
			}, recordPool.GetRecord())
			require.NoError(t, err)

			newLines := make([]logproto.Entry, numLogs)
			for i := 0; i < numLogs; i++ {
				newLines[i] = logproto.Entry{Timestamp: time.Unix(int64(i), 0), Line: "log"}
			}

			var expected bytes.Buffer
			for i := 0; i < tc.expectErrs; i++ {
				fmt.Fprintf(&expected,
					"entry with timestamp %s ignored, reason: 'entry out of order' for stream: {foo=\"bar\"},\n",
					time.Unix(int64(i), 0).String(),
				)
			}

			fmt.Fprintf(&expected, "total ignored: %d out of %d", numLogs, numLogs)
			expectErr := httpgrpc.Errorf(http.StatusBadRequest, expected.String())

			err = s.Push(context.Background(), newLines, recordPool.GetRecord())
			require.Error(t, err)
			require.Equal(t, expectErr.Error(), err.Error())
		})
	}
}

func TestPushDeduplication(t *testing.T) {
	s := newStream(
		defaultConfig(),
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		NilMetrics,
	)

	err := s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "newer, better test"},
	}, recordPool.GetRecord())
	require.NoError(t, err)
	require.Len(t, s.chunks, 1)
	require.Equal(t, s.chunks[0].chunk.Size(), 2,
		"expected exact duplicate to be dropped and newer content with same timestamp to be appended")
}

func TestStreamIterator(t *testing.T) {
	const chunks = 3
	const entries = 100

	for _, chk := range []struct {
		name string
		new  func() *chunkenc.MemChunk
	}{
		{"gzipChunk", func() *chunkenc.MemChunk { return chunkenc.NewMemChunk(chunkenc.EncGZIP, 256*1024, 0) }},
	} {
		t.Run(chk.name, func(t *testing.T) {
			var s stream
			for i := int64(0); i < chunks; i++ {
				chunk := chk.new()
				for j := int64(0); j < entries; j++ {
					k := i*entries + j
					err := chunk.Append(&logproto.Entry{
						Timestamp: time.Unix(k, 0),
						Line:      fmt.Sprintf("line %d", k),
					})
					require.NoError(t, err)
				}
				s.chunks = append(s.chunks, chunkDesc{chunk: chunk})
			}

			for i := 0; i < 100; i++ {
				from := rand.Intn(chunks*entries - 1)
				len := rand.Intn(chunks*entries-from) + 1
				iter, err := s.Iterator(context.TODO(), nil, time.Unix(int64(from), 0), time.Unix(int64(from+len), 0), logproto.FORWARD, log.NewNoopPipeline().ForStream(s.labels))
				require.NotNil(t, iter)
				require.NoError(t, err)
				testIteratorForward(t, iter, int64(from), int64(from+len))
				_ = iter.Close()
			}

			for i := 0; i < 100; i++ {
				from := rand.Intn(entries - 1)
				len := rand.Intn(chunks*entries-from) + 1
				iter, err := s.Iterator(context.TODO(), nil, time.Unix(int64(from), 0), time.Unix(int64(from+len), 0), logproto.BACKWARD, log.NewNoopPipeline().ForStream(s.labels))
				require.NotNil(t, iter)
				require.NoError(t, err)
				testIteratorBackward(t, iter, int64(from), int64(from+len))
				_ = iter.Close()
			}
		})
	}

}

func Benchmark_PushStream(b *testing.B) {
	ls := labels.Labels{
		labels.Label{Name: "namespace", Value: "loki-dev"},
		labels.Label{Name: "cluster", Value: "dev-us-central1"},
		labels.Label{Name: "job", Value: "loki-dev/ingester"},
		labels.Label{Name: "container", Value: "ingester"},
	}
	s := newStream(&Config{}, model.Fingerprint(0), ls, NilMetrics)
	t, err := newTailer("foo", `{namespace="loki-dev"}`, &fakeTailServer{})
	require.NoError(b, err)

	go t.loop()
	defer t.close()

	s.tailers[1] = t
	ctx := context.Background()
	e := entries(100, time.Now())
	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		rec := recordPool.GetRecord()
		require.NoError(b, s.Push(ctx, e, rec))
		recordPool.PutRecord(rec)
	}
}
