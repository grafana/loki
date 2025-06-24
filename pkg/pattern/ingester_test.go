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

	mockWriter.AssertCalled(
		t,
		"WriteEntry",
		now.Time(),
		aggregation.PatternEntry(
			now.Time(),
			1,
			"ts=<_> msg=hello",
			lbs,
		),
		labels.New(
			labels.Label{Name: constants.PatternLabel, Value: "test_service"},
		),
		[]logproto.LabelAdapter{
			{Name: constants.LevelLabel, Value: constants.LogLevelInfo},
		},
	)

	mockWriter.AssertCalled(
		t,
		"WriteEntry",
		now.Time(),
		aggregation.PatternEntry(
			now.Time(),
			5,
			"foo=bar num=<_>",
			lbs,
		),
		labels.New(
			labels.Label{Name: constants.PatternLabel, Value: "test_service"},
		),
		[]logproto.LabelAdapter{
			{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
		},
	)

	// writes a sample every 10s
	mockWriter.AssertCalled(
		t,
		"WriteEntry",
		now.Add(10*time.Second).Time(),
		aggregation.PatternEntry(
			now.Add(10*time.Second).Time(),
			5,
			"foo=bar num=<_>",
			lbs,
		),
		labels.New(
			labels.Label{Name: constants.PatternLabel, Value: "test_service"},
		),
		[]logproto.LabelAdapter{
			{Name: constants.LevelLabel, Value: constants.LogLevelUnknown},
		},
	)
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
	Limits
	metricAggregationEnabled bool
}

func (f *fakeLimits) PatternIngesterTokenizableJSONFields(_ string) []string {
	return []string{"log", "message", "msg", "msg_", "_msg", "content"}
}

func (f *fakeLimits) MetricAggregationEnabled(_ string) bool {
	return f.metricAggregationEnabled
}
