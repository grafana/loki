package metadata

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase/definitions"
)

func TestHeaders(t *testing.T) {
	h1 := []*definitions.PrometheusResponseHeader{
		{
			Name:   "Header1",
			Values: []string{"value"},
		},
	}
	h2 := []*definitions.PrometheusResponseHeader{
		{
			Name:   "Header2",
			Values: []string{"value1"},
		},
	}
	h3 := []*definitions.PrometheusResponseHeader{
		{
			Name:   "Header3",
			Values: []string{"value2"},
		},
	}

	metadata, ctx := NewContext(context.Background())
	err := JoinHeaders(ctx, h1)
	require.Nil(t, err)
	err = JoinHeaders(ctx, h2)
	require.Nil(t, err)
	err = JoinHeaders(ctx, h3)
	require.Nil(t, err)

	require.Equal(t, []*definitions.PrometheusResponseHeader{
		{Name: "Header1", Values: []string{"value"}},
		{Name: "Header2", Values: []string{"value1"}},
		{Name: "Header3", Values: []string{"value2"}},
	}, metadata.Headers())
}

func TestHeadersNoKey(t *testing.T) {
	ctx := context.Background()
	err := JoinHeaders(ctx, []*definitions.PrometheusResponseHeader{
		{
			Name:   "Header1",
			Values: []string{"value"},
		},
	})

	require.True(t, errors.Is(err, ErrNoCtxData))
}
