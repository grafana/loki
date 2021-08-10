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
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
)

var (
	countExtractor = func() log.StreamSampleExtractor {
		ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
		if err != nil {
			panic(err)
		}
		return ex.ForStream(labels.Labels{})
	}
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
				"fake",
				model.Fingerprint(0),
				labels.Labels{
					{Name: "foo", Value: "bar"},
				},
				true,
				NilMetrics,
			)

			_, err := s.Push(context.Background(), []logproto.Entry{
				{Timestamp: time.Unix(int64(numLogs), 0), Line: "log"},
			}, recordPool.GetRecord(), 0)
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

			_, err = s.Push(context.Background(), newLines, recordPool.GetRecord(), 0)
			require.Error(t, err)
			require.Equal(t, expectErr.Error(), err.Error())
		})
	}
}

func TestPushDeduplication(t *testing.T) {
	s := newStream(
		defaultConfig(),
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NilMetrics,
	)

	written, err := s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "newer, better test"},
	}, recordPool.GetRecord(), 0)
	require.NoError(t, err)
	require.Len(t, s.chunks, 1)
	require.Equal(t, s.chunks[0].chunk.Size(), 2,
		"expected exact duplicate to be dropped and newer content with same timestamp to be appended")
	require.Equal(t, len("test"+"newer, better test"), written)
}

func TestPushRejectOldCounter(t *testing.T) {
	s := newStream(
		defaultConfig(),
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NilMetrics,
	)

	// counter should be 2 now since the first line will be deduped
	_, err := s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "test"},
		{Timestamp: time.Unix(1, 0), Line: "newer, better test"},
	}, recordPool.GetRecord(), 0)
	require.NoError(t, err)
	require.Len(t, s.chunks, 1)
	require.Equal(t, s.chunks[0].chunk.Size(), 2,
		"expected exact duplicate to be dropped and newer content with same timestamp to be appended")

	// fail to push with a counter <= the streams internal counter
	_, err = s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, recordPool.GetRecord(), 2)
	require.Equal(t, ErrEntriesExist, err)

	// succeed with a greater counter
	_, err = s.Push(context.Background(), []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "test"},
	}, recordPool.GetRecord(), 3)
	require.Nil(t, err)

}

func TestStreamIterator(t *testing.T) {
	const chunks = 3
	const entries = 100

	for _, chk := range []struct {
		name string
		new  func() *chunkenc.MemChunk
	}{
		{"gzipChunk", func() *chunkenc.MemChunk {
			return chunkenc.NewMemChunk(chunkenc.EncGZIP, chunkenc.UnorderedHeadBlockFmt, 256*1024, 0)
		}},
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

func TestUnorderedPush(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	cfg.MaxChunkAge = 10 * time.Second
	s := newStream(
		&cfg,
		"fake",
		model.Fingerprint(0),
		labels.Labels{
			{Name: "foo", Value: "bar"},
		},
		true,
		NilMetrics,
	)

	for _, x := range []struct {
		cutBefore bool
		entries   []logproto.Entry
		err       bool
		written   int
	}{
		{
			entries: []logproto.Entry{
				{Timestamp: time.Unix(2, 0), Line: "x"},
				{Timestamp: time.Unix(1, 0), Line: "x"},
				{Timestamp: time.Unix(2, 0), Line: "x"},
				{Timestamp: time.Unix(2, 0), Line: "x"}, // duplicate ts/line is ignored
				{Timestamp: time.Unix(10, 0), Line: "x"},
			},
			written: 4, // 1 ignored
		},
		// highest ts is now 10, validity bound is (10-10/2) = 5
		{
			entries: []logproto.Entry{
				{Timestamp: time.Unix(4, 0), Line: "x"}, // ordering err, too far
				{Timestamp: time.Unix(8, 0), Line: "x"},
				{Timestamp: time.Unix(9, 0), Line: "x"},
			},
			err:     true,
			written: 2, // 1 ignored
		},
		// force a chunk cut and then push data overlapping with previous chunk.
		// This ultimately ensures the iterators implementation respects unordered chunks.
		{
			cutBefore: true,
			entries: []logproto.Entry{
				{Timestamp: time.Unix(11, 0), Line: "x"},
				{Timestamp: time.Unix(7, 0), Line: "x"},
			},
			written: 2,
		},
	} {
		if x.cutBefore {
			_ = s.cutChunk(context.Background())
		}
		written, err := s.Push(context.Background(), x.entries, recordPool.GetRecord(), 0)
		if x.err {
			require.NotNil(t, err)
		} else {
			require.Nil(t, err)
		}
		require.Equal(t, x.written, written)
	}

	require.Equal(t, 2, len(s.chunks))

	exp := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "x"},
		{Timestamp: time.Unix(2, 0), Line: "x"},
		// duplicate was allowed here b/c it wasnt written sequentially
		{Timestamp: time.Unix(2, 0), Line: "x"},
		{Timestamp: time.Unix(7, 0), Line: "x"},
		{Timestamp: time.Unix(8, 0), Line: "x"},
		{Timestamp: time.Unix(9, 0), Line: "x"},
		{Timestamp: time.Unix(10, 0), Line: "x"},
		{Timestamp: time.Unix(11, 0), Line: "x"},
	}

	itr, err := s.Iterator(context.Background(), nil, time.Unix(int64(0), 0), time.Unix(12, 0), logproto.FORWARD, log.NewNoopPipeline().ForStream(s.labels))
	require.Nil(t, err)
	iterEq(t, exp, itr)

	sItr, err := s.SampleIterator(context.Background(), nil, time.Unix(int64(0), 0), time.Unix(12, 0), countExtractor())
	require.Nil(t, err)
	for _, x := range exp {
		require.Equal(t, true, sItr.Next())
		require.Equal(t, x.Timestamp, time.Unix(0, sItr.Sample().Timestamp))
		require.Equal(t, float64(1), sItr.Sample().Value)
	}
	require.Equal(t, false, sItr.Next())
}

func iterEq(t *testing.T, exp []logproto.Entry, got iter.EntryIterator) {
	var i int
	for got.Next() {
		require.Equal(t, exp[i].Timestamp, got.Entry().Timestamp, "failed on the (%d) ts", i)
		require.Equal(t, exp[i].Line, got.Entry().Line)
		i++
	}
	require.Equal(t, i, len(exp), "incorrect number of entries expected")
}

func Benchmark_PushStream(b *testing.B) {
	ls := labels.Labels{
		labels.Label{Name: "namespace", Value: "loki-dev"},
		labels.Label{Name: "cluster", Value: "dev-us-central1"},
		labels.Label{Name: "job", Value: "loki-dev/ingester"},
		labels.Label{Name: "container", Value: "ingester"},
	}
	s := newStream(&Config{}, "fake", model.Fingerprint(0), ls, true, NilMetrics)
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
		_, err := s.Push(ctx, e, rec, 0)
		require.NoError(b, err)
		recordPool.PutRecord(rec)
	}
}
