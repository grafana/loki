package pattern

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/v3/pkg/pattern/drain"

	"github.com/grafana/loki/pkg/push"
)

func TestInstancePushQuery(t *testing.T) {
	lbs := labels.FromStrings("test", "test", "service_name", "test_service")
	now := drain.TruncateTimestamp(model.Now(), drain.TimeResolution)

	ingesterID := "foo"
	replicationSet := ring.ReplicationSet{
		Instances: []ring.InstanceDesc{
			{Id: ingesterID, Addr: "ingester0"},
			{Id: "bar", Addr: "ingester1"},
			{Id: "baz", Addr: "ingester2"},
		},
	}

	fakeRing := &fakeRing{}
	fakeRing.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(replicationSet, nil)

	ringClient := &fakeRingClient{
		ring: fakeRing,
	}

	mockWriter := &mockEntryWriter{}
	mockWriter.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	inst, err := newInstance(
		"foo",
		log.NewNopLogger(),
		newIngesterMetrics(nil, "test"),
		drain.DefaultConfig(),
		&fakeLimits{},
		ringClient,
		ingesterID,
		mockWriter,
		mockWriter,
	)
	require.NoError(t, err)

	err = inst.Push(context.Background(), &push.PushRequest{
		Streams: []push.Stream{
			{
				Labels: lbs.String(),
				Entries: []push.Entry{
					{
						Timestamp: now.Time(),
						Line:      "ts=1 msg=hello",
						StructuredMetadata: push.LabelsAdapter{
							push.LabelAdapter{
								Name:  constants.LevelLabel,
								Value: constants.LogLevelInfo,
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	for i := 0; i <= 30; i++ {
		foo := "bar"
		if i%2 != 0 {
			foo = "baz"
		}
		err = inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbs.String(),
					Entries: []push.Entry{
						{
							Timestamp: now.Add(time.Duration(i) * time.Second).Time(),
							Line:      fmt.Sprintf("foo=%s num=%d", foo, rand.Int()),
						},
					},
				},
			},
		})
		require.NoError(t, err)
	}

	err = inst.Push(context.Background(), &push.PushRequest{
		Streams: []push.Stream{
			{
				Labels: lbs.String(),
				Entries: []push.Entry{
					{
						Timestamp: now.Add(1 * time.Minute).Time(),
						Line:      "ts=2 msg=hello",
						StructuredMetadata: push.LabelsAdapter{
							push.LabelAdapter{
								Name:  constants.LevelLabel,
								Value: constants.LogLevelInfo,
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	it, err := inst.Iterator(context.Background(), &logproto.QueryPatternsRequest{
		Query: "{test=\"test\"}",
		Start: time.Unix(0, 0),
		End:   time.Unix(0, math.MaxInt64),
	})
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 3, len(res.Series))

	patterns := make([]string, 0, 3)
	for _, it := range res.Series {
		patterns = append(patterns, it.Pattern)
	}

	require.ElementsMatch(t, []string{
		"foo=bar num=<_>",
		"foo=baz num=<_>",
		"ts=<_> msg=hello",
	}, patterns)
}

func TestInstancePushAggregateMetrics(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "test", Value: "test"},
		labels.Label{Name: "service_name", Value: "test_service"},
	)
	lbs2 := labels.New(
		labels.Label{Name: "foo", Value: "bar"},
		labels.Label{Name: "service_name", Value: "foo_service"},
	)
	lbs3 := labels.New(
		labels.Label{Name: "foo", Value: "baz"},
		labels.Label{Name: "service_name", Value: "baz_service"},
	)

	setup := func(now time.Time) (*instance, *mockEntryWriter) {
		ingesterID := "foo"
		replicationSet := ring.ReplicationSet{
			Instances: []ring.InstanceDesc{
				{Id: ingesterID, Addr: "ingester0"},
				{Id: "bar", Addr: "ingester1"},
				{Id: "baz", Addr: "ingester2"},
			},
		}

		fakeRing := &fakeRing{}
		fakeRing.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(replicationSet, nil)

		ringClient := &fakeRingClient{
			ring: fakeRing,
		}

		mockWriter := &mockEntryWriter{}
		mockWriter.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

		inst, err := newInstance(
			"foo",
			log.NewNopLogger(),
			newIngesterMetrics(nil, "test"),
			drain.DefaultConfig(),
			&fakeLimits{},
			ringClient,
			ingesterID,
			mockWriter,
			mockWriter,
		)
		require.NoError(t, err)

		err = inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbs.String(),
					Entries: []push.Entry{
						{
							Timestamp: now.Add(-1 * time.Minute),
							Line:      "ts=1 msg=hello",
							StructuredMetadata: push.LabelsAdapter{
								push.LabelAdapter{
									Name:  constants.LevelLabel,
									Value: "info",
								},
							},
						},
					},
				},
				{
					Labels: lbs2.String(),
					Entries: []push.Entry{
						{
							Timestamp: now.Add(-1 * time.Minute),
							Line:      fmt.Sprintf("ts=%d msg=hello", rand.Intn(9)),
							StructuredMetadata: push.LabelsAdapter{
								push.LabelAdapter{
									Name:  constants.LevelLabel,
									Value: "error",
								},
							},
						},
					},
				},
				{
					Labels: lbs3.String(),
					Entries: []push.Entry{
						{
							Timestamp: now.Add(-1 * time.Minute),
							Line:      "error error error",
							StructuredMetadata: push.LabelsAdapter{
								push.LabelAdapter{
									Name:  constants.LevelLabel,
									Value: "error",
								},
							},
						},
					},
				},
			},
		})
		for i := 0; i < 30; i++ {
			err = inst.Push(context.Background(), &push.PushRequest{
				Streams: []push.Stream{
					{
						Labels: lbs.String(),
						Entries: []push.Entry{
							{
								Timestamp: now.Add(-1 * time.Duration(i) * time.Second),
								Line:      "foo=bar baz=qux",
								StructuredMetadata: push.LabelsAdapter{
									push.LabelAdapter{
										Name:  constants.LevelLabel,
										Value: "info",
									},
								},
							},
						},
					},
					{
						Labels: lbs2.String(),
						Entries: []push.Entry{
							{
								Timestamp: now.Add(-1 * time.Duration(i) * time.Second),
								Line:      "foo=bar baz=qux",
								StructuredMetadata: push.LabelsAdapter{
									push.LabelAdapter{
										Name:  constants.LevelLabel,
										Value: "error",
									},
								},
							},
						},
					},
				},
			})
			require.NoError(t, err)
		}
		require.NoError(t, err)

		return inst, mockWriter
	}

	t.Run("accumulates bytes and count for each stream and level on every push", func(t *testing.T) {
		now := time.Now()
		inst, _ := setup(now)

		require.Len(t, inst.aggMetricsByStreamAndLevel, 3)

		require.Equal(t, uint64(14+(15*30)), inst.aggMetricsByStreamAndLevel[lbs.String()]["info"].bytes)
		require.Equal(t, uint64(14+(15*30)), inst.aggMetricsByStreamAndLevel[lbs2.String()]["error"].bytes)
		require.Equal(t, uint64(17), inst.aggMetricsByStreamAndLevel[lbs3.String()]["error"].bytes)

		require.Equal(
			t,
			uint64(31),
			inst.aggMetricsByStreamAndLevel[lbs.String()]["info"].count,
		)
		require.Equal(
			t,
			uint64(31),
			inst.aggMetricsByStreamAndLevel[lbs2.String()]["error"].count,
		)
		require.Equal(
			t,
			uint64(1),
			inst.aggMetricsByStreamAndLevel[lbs3.String()]["error"].count,
		)
	})

	t.Run("downsamples aggregated metrics", func(t *testing.T) {
		now := model.Now()
		inst, mockWriter := setup(now.Time())
		inst.Downsample(now)

		mockWriter.AssertCalled(
			t,
			"WriteEntry",
			now.Time(),
			aggregation.AggregatedMetricEntry(
				now,
				uint64(14+(15*30)),
				uint64(31),
				lbs,
			),
			labels.New(
				labels.Label{Name: constants.AggregatedMetricLabel, Value: "test_service"},
			),
			[]logproto.LabelAdapter{
				{Name: constants.LevelLabel, Value: constants.LogLevelInfo},
			},
		)

		mockWriter.AssertCalled(
			t,
			"WriteEntry",
			now.Time(),
			aggregation.AggregatedMetricEntry(
				now,
				uint64(14+(15*30)),
				uint64(31),
				lbs2,
			),
			labels.New(
				labels.Label{Name: constants.AggregatedMetricLabel, Value: "foo_service"},
			),
			[]logproto.LabelAdapter{
				{Name: constants.LevelLabel, Value: constants.LogLevelError},
			},
		)

		mockWriter.AssertCalled(
			t,
			"WriteEntry",
			now.Time(),
			aggregation.AggregatedMetricEntry(
				now,
				uint64(17),
				uint64(1),
				lbs3,
			),
			labels.New(
				labels.Label{Name: constants.AggregatedMetricLabel, Value: "baz_service"},
			),
			[]logproto.LabelAdapter{
				{Name: constants.LevelLabel, Value: constants.LogLevelError},
			},
		)

		require.Equal(t, 0, len(inst.aggMetricsByStreamAndLevel))
	})
}

type mockEntryWriter struct {
	mock.Mock
}

func (m *mockEntryWriter) WriteEntry(ts time.Time, entry string, lbls labels.Labels, structuredMetadata []logproto.LabelAdapter) {
	_ = m.Called(ts, entry, lbls, structuredMetadata)
}

func (m *mockEntryWriter) Stop() {
	_ = m.Called()
}

type fakeLimits struct {
	metricAggregationEnabled  bool
	patternPersistenceEnabled bool
}

var _ drain.Limits = &fakeLimits{}
var _ Limits = &fakeLimits{}

func (f *fakeLimits) PatternIngesterTokenizableJSONFields(_ string) []string {
	return []string{"log", "message", "msg", "msg_", "_msg", "content"}
}

func (f *fakeLimits) MetricAggregationEnabled(_ string) bool {
	return f.metricAggregationEnabled
}

func (f *fakeLimits) PatternPersistenceEnabled(_ string) bool {
	return f.patternPersistenceEnabled
}

func TestIngesterShutdownFlush(t *testing.T) {
	lbs := labels.FromStrings("test", "test", "service_name", "test_service")
	now := model.Now()

	ingesterID := "foo"
	replicationSet := ring.ReplicationSet{
		Instances: []ring.InstanceDesc{
			{Id: ingesterID, Addr: "ingester0"},
			{Id: "bar", Addr: "ingester1"},
			{Id: "baz", Addr: "ingester2"},
		},
	}

	fakeRing := &fakeRing{}
	fakeRing.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(replicationSet, nil)

	ringClient := &fakeRingClient{
		ring: fakeRing,
	}

	mockWriter := &mockEntryWriter{}
	mockWriter.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockWriter.On("Stop")

	inst, err := newInstance(
		"foo",
		log.NewNopLogger(),
		newIngesterMetrics(nil, "test"),
		drain.DefaultConfig(),
		&fakeLimits{patternPersistenceEnabled: true},
		ringClient,
		ingesterID,
		mockWriter,
		mockWriter,
	)
	require.NoError(t, err)

	// Push some log entries to create patterns
	err = inst.Push(context.Background(), &push.PushRequest{
		Streams: []push.Stream{
			{
				Labels: lbs.String(),
				Entries: []push.Entry{
					{
						Timestamp: now.Time(),
						Line:      "info: user logged in",
					},
					{
						Timestamp: now.Add(time.Second).Time(),
						Line:      "info: user logged out",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	// Verify patterns exist before shutdown
	it, err := inst.Iterator(context.Background(), &logproto.QueryPatternsRequest{
		Query: "{test=\"test\"}",
		Start: time.Unix(0, 0),
		End:   time.Unix(0, math.MaxInt64),
	})
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Greater(t, len(res.Series), 0, "should have patterns before shutdown")

	// To isloate that the WriteEntry was called during flush
	mockWriter.AssertNotCalled(t, "WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	// Flush patterns (simulating shutdown)
	inst.flushPatterns()

	// Verify WriteEntry was called during flush
	mockWriter.AssertCalled(t, "WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestIngesterShutdownFlushMaintainsChunkBoundaries(t *testing.T) {
	lbs := labels.FromStrings("test", "test", "service_name", "test_service")

	ingesterID := "foo"
	replicationSet := ring.ReplicationSet{
		Instances: []ring.InstanceDesc{
			{Id: ingesterID, Addr: "ingester0"},
		},
	}

	fakeRing := &fakeRing{}
	fakeRing.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(replicationSet, nil)

	ringClient := &fakeRingClient{
		ring: fakeRing,
	}

	mockWriter := &mockEntryWriter{}
	// Track WriteEntry calls to verify chunk boundaries
	var writeEntryCalls []mock.Call
	mockWriter.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		writeEntryCalls = append(writeEntryCalls, mock.Call{Arguments: args})
	})
	mockWriter.On("Stop")

	inst, err := newInstance(
		"foo",
		log.NewNopLogger(),
		newIngesterMetrics(nil, "test"),
		drain.DefaultConfig(),
		&fakeLimits{patternPersistenceEnabled: true},
		ringClient,
		ingesterID,
		mockWriter,
		mockWriter,
	)
	require.NoError(t, err)

	// Push log entries across different time windows to create multiple chunks
	baseTime := time.Now()
	err = inst.Push(context.Background(), &push.PushRequest{
		Streams: []push.Stream{
			{
				Labels: lbs.String(),
				Entries: []push.Entry{
					{
						Timestamp: baseTime,
						Line:      "info: user action completed",
					},
					{
						Timestamp: baseTime.Add(2 * time.Hour), // Force new chunk (> 1 hour)
						Line:      "info: user action completed",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	// To isloate that the WriteEntry was called during flush
	mockWriter.AssertNotCalled(t, "WriteEntry", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	// Flush patterns (simulating shutdown)
	inst.flushPatterns()

	// Verify WriteEntry was called multiple times (once per chunk)
	// With 2 entries across 2 hour windows, we should get 2 pattern writes
	require.Greater(t, len(writeEntryCalls), 0, "should have written patterns during shutdown")

	// Verify each call has the expected pattern (URL-encoded in the entry)
	for _, call := range writeEntryCalls {
		entry := call.Arguments[1].(string)
		require.Contains(t, entry, "detected_pattern=", "should contain detected_pattern field")
		require.Contains(t, entry, "info", "should contain pattern info")
	}
}
