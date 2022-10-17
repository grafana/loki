package metadata

import (
	"context"
	"testing"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/stretchr/testify/require"
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
	JoinHeaders(ctx, h1)
	JoinHeaders(ctx, h2)
	JoinHeaders(ctx, h3)

	require.Equal(t, []*definitions.PrometheusResponseHeader{
		{Name: "Header1", Values: []string{"value"}},
		{Name: "Header2", Values: []string{"value1"}},
		{Name: "Header3", Values: []string{"value2"}},
	}, metadata.Headers())
}
