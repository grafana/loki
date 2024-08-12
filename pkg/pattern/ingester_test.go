package pattern

import (
	"context"
	"math"
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

	"github.com/grafana/loki/v3/pkg/pattern/drain"

	"github.com/grafana/loki/pkg/push"
	loghttp_push "github.com/grafana/loki/v3/pkg/loghttp/push"
)

func TestInstancePushQuery(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})

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
	mockWriter.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything)

	inst, err := newInstance(
		"foo",
		log.NewNopLogger(),
		newIngesterMetrics(nil, "test"),
		drain.DefaultConfig(),
		ringClient,
		ingesterID,
		mockWriter,
	)
	require.NoError(t, err)

	err = inst.Push(context.Background(), &push.PushRequest{
		Streams: []push.Stream{
			{
				Labels: lbs.String(),
				Entries: []push.Entry{
					{
						Timestamp: time.Unix(20, 0),
						Line:      "ts=1 msg=hello",
					},
				},
			},
		},
	})
	for i := 0; i <= 30; i++ {
		err = inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbs.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(20, 0),
							Line:      "foo bar foo bar",
						},
					},
				},
			},
		})
		require.NoError(t, err)
	}
	require.NoError(t, err)
	it, err := inst.Iterator(context.Background(), &logproto.QueryPatternsRequest{
		Query: "{test=\"test\"}",
		Start: time.Unix(0, 0),
		End:   time.Unix(0, math.MaxInt64),
	})
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 2, len(res.Series))
}

func TestInstancePushAggregateMetrics(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "test", Value: "test"},
		labels.Label{Name: "service_name", Value: "test_service"},
		labels.Label{Name: "level", Value: "info"},
	)
	lbs2 := labels.New(
		labels.Label{Name: "foo", Value: "bar"},
		labels.Label{Name: "service_name", Value: "foo_service"},
		labels.Label{Name: "level", Value: "error"},
	)

	setup := func() (*instance, *mockEntryWriter) {
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
		mockWriter.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything)

		inst, err := newInstance(
			"foo",
			log.NewNopLogger(),
			newIngesterMetrics(nil, "test"),
			drain.DefaultConfig(),
			ringClient,
			ingesterID,
			mockWriter,
		)
		require.NoError(t, err)

		err = inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbs.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(20, 0),
							Line:      "ts=1 msg=hello",
						},
					},
				},
				{
					Labels: lbs2.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(20, 0),
							Line:      "ts=1 msg=hello",
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
								Timestamp: time.Unix(20, 0),
								Line:      "foo bar foo bar",
							},
						},
					},
					{
						Labels: lbs2.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(20, 0),
								Line:      "foo bar foo bar",
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

	t.Run("accumulates bytes and count on every push", func(t *testing.T) {
		inst, _ := setup()

		require.Len(t, inst.aggMetricsByStream, 2)

		require.Equal(t, uint64(14+(15*30)), inst.aggMetricsByStream[lbs.String()].bytes)
		require.Equal(t, uint64(14+(15*30)), inst.aggMetricsByStream[lbs2.String()].bytes)

		require.Equal(t, uint64(31), inst.aggMetricsByStream[lbs.String()].count)
		require.Equal(t, uint64(31), inst.aggMetricsByStream[lbs2.String()].count)
	})

	t.Run("downsamples aggregated metrics", func(t *testing.T) {
		inst, mockWriter := setup()
		now := model.Now()
		inst.Downsample(now)

		mockWriter.AssertCalled(
			t,
			"WriteEntry",
			now.Time(),
			aggregation.AggregatedMetricEntry(
				now,
				uint64(14+(15*30)),
				uint64(31),
				"test_service",
				lbs,
			),
			labels.New(
				labels.Label{Name: loghttp_push.AggregatedMetricLabel, Value: "test_service"},
				labels.Label{Name: "level", Value: "info"},
			),
		)

		mockWriter.AssertCalled(
			t,
			"WriteEntry",
			now.Time(),
			aggregation.AggregatedMetricEntry(
				now,
				uint64(14+(15*30)),
				uint64(31),
				"foo_service",
				lbs2,
			),
			labels.New(
				labels.Label{Name: loghttp_push.AggregatedMetricLabel, Value: "foo_service"},
				labels.Label{Name: "level", Value: "error"},
			),
		)

		require.Equal(t, 0, len(inst.aggMetricsByStream))
	})
}

type mockEntryWriter struct {
	mock.Mock
}

func (m *mockEntryWriter) WriteEntry(ts time.Time, entry string, lbls labels.Labels) {
	_ = m.Called(ts, entry, lbls)
}

func (m *mockEntryWriter) Stop() {
	_ = m.Called()
}
