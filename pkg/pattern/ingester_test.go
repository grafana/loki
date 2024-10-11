package pattern

import (
	"context"
	"fmt"
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
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/v3/pkg/pattern/drain"

	loghttp_push "github.com/grafana/loki/v3/pkg/loghttp/push"

	"github.com/grafana/loki/pkg/push"
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
	)
	lbs2 := labels.New(
		labels.Label{Name: "foo", Value: "bar"},
		labels.Label{Name: "service_name", Value: "foo_service"},
	)
	lbs3 := labels.New(
		labels.Label{Name: "foo", Value: "baz"},
		labels.Label{Name: "service_name", Value: "baz_service"},
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
				// info level
				{
					Labels: lbs.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(20, 0),
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
				// unknown level
				{
					Labels: lbs2.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(20, 0),
							Line:      "ts=1 msg=hello",
						},
					},
				},
				// error level
				{
					Labels: lbs3.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(20, 0),
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
					// info
					{
						Labels: lbs.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(20, 0),
								Line:      "foo bar foo bar",
								StructuredMetadata: push.LabelsAdapter{
									push.LabelAdapter{
										Name:  constants.LevelLabel,
										Value: "info",
									},
								},
							},
						},
					},
					// error
					{
						Labels: lbs2.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(20, 0),
								Line:      "foo bar foo bar",
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
		inst, _ := setup()

		require.Len(t, inst.aggMetricsByStreamAndLevel, 3)

		require.Equal(
			t,
			uint64(14+(15*30)),
			inst.aggMetricsByStreamAndLevel[lbs.String()]["info"].bytes,
		)
		require.Equal(
			t,
			uint64(14),
			inst.aggMetricsByStreamAndLevel[lbs2.String()]["unknown"].bytes,
		)
		require.Equal(
			t,
			uint64(15*30),
			inst.aggMetricsByStreamAndLevel[lbs2.String()]["error"].bytes,
		)
		require.Equal(
			t,
			uint64(17),
			inst.aggMetricsByStreamAndLevel[lbs3.String()]["error"].bytes,
		)

		require.Equal(
			t,
			uint64(31),
			inst.aggMetricsByStreamAndLevel[lbs.String()]["info"].count,
		)
		require.Equal(
			t,
			uint64(1),
			inst.aggMetricsByStreamAndLevel[lbs2.String()]["unknown"].count,
		)
		require.Equal(
			t,
			uint64(30),
			inst.aggMetricsByStreamAndLevel[lbs2.String()]["error"].count,
		)
		require.Equal(
			t,
			uint64(1),
			inst.aggMetricsByStreamAndLevel[lbs3.String()]["error"].count,
		)
	},
	)

	t.Run("downsamples aggregated metrics", func(t *testing.T) {
		inst, mockWriter := setup()
		now := model.Now()
		inst.Downsample(now)

		expectedTestLine := fmt.Sprintf(
			`ts=%d bytes=%s count=%d detected_level="info" %s="test_service" test="test"`,
			now.UnixNano(),
			util.HumanizeBytes(uint64(14+(15*30))),
			uint64(31),
			loghttp_push.LabelServiceName,
		)

		mockWriter.AssertCalled(
			t,
			"WriteEntry",
			now.Time(),
			expectedTestLine,
			labels.New(
				labels.Label{Name: loghttp_push.AggregatedMetricLabel, Value: "test_service"},
				labels.Label{Name: "level", Value: "info"},
			),
		)

		expectedFooLine1 := fmt.Sprintf(
			`ts=%d bytes=%s count=%d detected_level="unknown" foo="bar" %s="foo_service"`,
			now.UnixNano(),
			util.HumanizeBytes(uint64(14)),
			uint64(1),
			loghttp_push.LabelServiceName,
		)
		expectedFooLine2 := fmt.Sprintf(
			`ts=%d bytes=%s count=%d detected_level="error" foo="bar" %s="foo_service"`,
			now.UnixNano(),
			util.HumanizeBytes(uint64(15*30)),
			uint64(30),
			loghttp_push.LabelServiceName,
		)

		mockWriter.AssertCalled(
			t,
			"WriteEntry",
			now.Time(),
			expectedFooLine1,
			labels.New(
				labels.Label{Name: loghttp_push.AggregatedMetricLabel, Value: "foo_service"},
				labels.Label{Name: "level", Value: "unknown"},
			),
		)
		mockWriter.AssertCalled(
			t,
			"WriteEntry",
			now.Time(),
			expectedFooLine2,
			labels.New(
				labels.Label{Name: loghttp_push.AggregatedMetricLabel, Value: "foo_service"},
				labels.Label{Name: "level", Value: "error"},
			),
		)

		expectedBazLine := fmt.Sprintf(
			`ts=%d bytes=%s count=%d detected_level="error" foo="baz" %s="baz_service"`,
			now.UnixNano(),
			util.HumanizeBytes(uint64(17)),
			uint64(1),
			loghttp_push.LabelServiceName,
		)

		mockWriter.AssertCalled(
			t,
			"WriteEntry",
			now.Time(),
			expectedBazLine,
			labels.New(
				labels.Label{Name: loghttp_push.AggregatedMetricLabel, Value: "baz_service"},
				labels.Label{Name: "level", Value: "error"},
			),
		)

		require.Equal(t, 0, len(inst.aggMetricsByStreamAndLevel))
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
