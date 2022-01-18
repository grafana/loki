package ingester

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/validation"
)

func TestStreamsMap(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)

	ss := []*stream{
		newStream(
			defaultConfig(),
			limiter,
			"fake",
			model.Fingerprint(1),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			true,
			NilMetrics,
		),
		newStream(
			defaultConfig(),
			limiter,
			"fake",
			model.Fingerprint(2),
			labels.Labels{
				{Name: "bar", Value: "foo"},
			},
			true,
			NilMetrics,
		),
	}
	var s *stream
	var loaded bool

	streams := newStreamsMap()

	require.Equal(t, 0, streams.Len())
	s, loaded = streams.Load(ss[0].labelsString)
	require.Nil(t, s)
	require.False(t, loaded)
	s, loaded = streams.LoadByFP(ss[1].fp)
	require.Nil(t, s)
	require.False(t, loaded)

	s, loaded, err = streams.LoadOrStoreNew(ss[0].labelsString, func() (*stream, error) {
		return ss[0], nil
	})
	require.Equal(t, s, ss[0])
	require.False(t, loaded)
	require.Nil(t, err)

	s, loaded, err = streams.LoadOrStoreNewByFP(ss[1].fp, func() (*stream, error) {
		return ss[1], nil
	})
	require.Equal(t, s, ss[1])
	require.False(t, loaded)
	require.Nil(t, err)

	require.Equal(t, len(ss), streams.Len())

	for _, st := range ss {
		s, loaded = streams.Load(st.labelsString)
		require.Equal(t, st, s)
		require.True(t, loaded)

		s, loaded = streams.LoadByFP(st.fp)
		require.Equal(t, st, s)
		require.True(t, loaded)
	}

	for _, st := range ss {
		deleted := streams.Delete(st)
		require.True(t, deleted)

		s, loaded = streams.Load(st.labelsString)
		require.Nil(t, s)
		require.False(t, loaded)

		s, loaded = streams.LoadByFP(st.fp)
		require.Nil(t, s)
		require.False(t, loaded)
	}
}
