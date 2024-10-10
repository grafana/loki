package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"

	"github.com/grafana/loki/pkg/push"
)

func TestAddStream(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	stream, err := newStream(
		model.Fingerprint(lbs.Hash()),
		lbs,
		newIngesterMetrics(nil, "test"),
		log.NewNopLogger(),
		drain.FormatUnknown,
		"123",
		drain.DefaultConfig(),
	)
	require.NoError(t, err)

	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=1 msg=hello",
		},
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=2 msg=hello",
		},
		{
			Timestamp: time.Unix(10, 0),
			Line:      "ts=3 msg=hello", // this should be ignored because it's older than the last entry
		},
	})
	require.NoError(t, err)
	it, err := stream.Iterator(context.Background(), model.Earliest, model.Latest, model.Time(time.Second))
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 1, len(res.Series))
	require.Equal(t, int64(2), res.Series[0].Samples[0].Value)
}

func TestPruneStream(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	stream, err := newStream(
		model.Fingerprint(lbs.Hash()),
		lbs,
		newIngesterMetrics(nil, "test"),
		log.NewNopLogger(),
		drain.FormatUnknown,
		"123",
		drain.DefaultConfig(),
	)
	require.NoError(t, err)

	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=1 msg=hello",
		},
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=2 msg=hello",
		},
	})
	require.NoError(t, err)
	require.Equal(t, true, stream.prune(time.Hour))

	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Now(),
			Line:      "ts=1 msg=hello",
		},
	})
	require.NoError(t, err)
	require.Equal(t, false, stream.prune(time.Hour))
	it, err := stream.Iterator(context.Background(), model.Earliest, model.Latest, model.Time(time.Second))
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 1, len(res.Series))
	require.Equal(t, int64(1), res.Series[0].Samples[0].Value)
}
