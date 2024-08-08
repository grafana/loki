package ingester

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
)

func TestStreamsMap(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, &ringCountMock{count: 1}, 1)
	chunkfmt, headfmt := defaultChunkFormat(t)

	ss := []*stream{
		newStream(
			chunkfmt,
			headfmt,
			defaultConfig(),
			limiter,
			"fake",
			model.Fingerprint(1),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			true,
			NewStreamRateCalculator(),
			NilMetrics,
			nil,
			nil,
		),
		newStream(
			chunkfmt,
			headfmt,
			defaultConfig(),
			limiter,
			"fake",
			model.Fingerprint(2),
			labels.Labels{
				{Name: "bar", Value: "foo"},
			},
			true,
			NewStreamRateCalculator(),
			NilMetrics,
			nil,
			nil,
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

	// Test LoadOrStoreNew
	s, loaded, err = streams.LoadOrStoreNew(ss[0].labelsString, func() (*stream, error) {
		return ss[0], nil
	}, nil)
	require.Equal(t, s, ss[0])
	require.False(t, loaded)
	require.Nil(t, err)

	// Test LoadOrStoreNewByFP
	s, loaded, err = streams.LoadOrStoreNewByFP(ss[1].fp, func() (*stream, error) {
		return ss[1], nil
	}, nil)
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

	// Test Delete
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

	require.Equal(t, 0, streams.Len())

	// Test Store
	streams.Store(ss[0].labelsString, ss[0])

	s, loaded = streams.Load(ss[0].labelsString)
	require.Equal(t, ss[0], s)
	require.True(t, loaded)

	s, loaded = streams.LoadByFP(ss[0].fp)
	require.Equal(t, ss[0], s)
	require.True(t, loaded)

	// Test StoreByFP
	streams.StoreByFP(ss[1].fp, ss[1])

	s, loaded = streams.Load(ss[1].labelsString)
	require.Equal(t, ss[1], s)
	require.True(t, loaded)

	s, loaded = streams.LoadByFP(ss[1].fp)
	require.Equal(t, ss[1], s)
	require.True(t, loaded)

	require.Equal(t, len(ss), streams.Len())
}
