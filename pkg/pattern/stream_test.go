package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/pkg/push"
)

func TestAddStream(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	mockWriter := &mockEntryWriter{}
	stream, err := newStream(model.Fingerprint(lbs.Hash()), lbs, newIngesterMetrics(nil, "test"), log.NewNopLogger(), drain.FormatUnknown, "123", drain.DefaultConfig(), &fakeLimits{}, mockWriter)
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
	mockWriter := &mockEntryWriter{}
	mockWriter.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	stream, err := newStream(model.Fingerprint(lbs.Hash()), lbs, newIngesterMetrics(nil, "test"), log.NewNopLogger(), drain.FormatUnknown, "123", drain.DefaultConfig(), &fakeLimits{}, mockWriter)
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

func TestStreamPatternPersistenceOnPrune(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "test", Value: "test"},
		labels.Label{Name: "service_name", Value: "test_service"},
	)
	mockWriter := &mockEntryWriter{}
	stream, err := newStream(model.Fingerprint(lbs.Hash()), lbs, newIngesterMetrics(nil, "test"), log.NewNopLogger(), drain.FormatUnknown, "123", drain.DefaultConfig(), &fakeLimits{}, mockWriter)
	require.NoError(t, err)

	// Push entries with old timestamps that will be pruned
	now := drain.TruncateTimestamp(model.TimeFromUnixNano(time.Now().UnixNano()), drain.TimeResolution).Time()
	oldTime := now.Add(-2 * time.Hour)
	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: oldTime,
			Line:      "ts=1 msg=hello",
		},
		{
			Timestamp: oldTime.Add(time.Minute),
			Line:      "ts=2 msg=hello",
		},
		{
			Timestamp: oldTime.Add(5 * time.Minute),
			Line:      "ts=3 msg=hello",
		},
	})
	require.NoError(t, err)

	// Push a newer entry to ensure the stream isn't completely pruned
	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: now,
			Line:      "ts=4 msg=hello",
		},
	})
	require.NoError(t, err)

	// We expect one pattern entry with aggregated count (3) and latest timestamp
	mockWriter.On("WriteEntry",
		oldTime.Add(5*time.Minute),
		aggregation.PatternEntry(oldTime.Add(5*time.Minute), 3, "ts=<_> msg=hello", lbs),
		labels.New(labels.Label{Name: constants.PatternLabel, Value: "test_service"}),
		[]logproto.LabelAdapter{
			{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
		},
	)

	// Prune old data - this should trigger pattern writing
	isEmpty := stream.prune(time.Hour)
	require.False(t, isEmpty) // Stream should not be empty due to newer entry

	// Verify the pattern was written
	mockWriter.AssertExpectations(t)
}
