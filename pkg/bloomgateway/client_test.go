package bloomgateway

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

type errorMockPool struct {
	*JumpHashClientPool
}

func (p *errorMockPool) Addr(_ string) (string, error) {
	return "", errors.New("no server found")
}

func TestBloomGatewayClient(t *testing.T) {
	logger := log.NewNopLogger()

	limits := newLimits()

	cfg := ClientConfig{}
	flagext.DefaultValues(&cfg)

	t.Run("FilterChunks returns response", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		c, err := NewClient(cfg, limits, reg, logger, nil, false)
		require.NoError(t, err)
		expr, err := syntax.ParseExpr(`{foo="bar"}`)
		require.NoError(t, err)
		res, err := c.FilterChunks(context.Background(), "tenant", bloomshipper.NewInterval(0, 0), nil, plan.QueryPlan{AST: expr})
		require.NoError(t, err)
		require.Equal(t, 0, len(res))
	})

	t.Run("pool error is suppressed and returns full list of chunks", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		c, err := NewClient(cfg, limits, reg, logger, nil, false)
		require.NoError(t, err)
		c.pool = &errorMockPool{}

		expected := []*logproto.GroupedChunkRefs{
			{Fingerprint: 0x00, Refs: []*logproto.ShortRef{shortRef(0, 1, 1)}},
			{Fingerprint: 0x9f, Refs: []*logproto.ShortRef{shortRef(0, 1, 2)}},
			{Fingerprint: 0xa0, Refs: []*logproto.ShortRef{shortRef(0, 1, 3)}},
			{Fingerprint: 0xff, Refs: []*logproto.ShortRef{shortRef(0, 1, 4)}},
		}

		blocks := []blockWithSeries{
			{block: mkBlockRef(0x00, 0x9f), series: expected[0:2]},
			{block: mkBlockRef(0xa0, 0xff), series: expected[2:4]},
		}
		expr, err := syntax.ParseExpr(`{foo="bar"}`)
		require.NoError(t, err)

		res, err := c.FilterChunks(context.Background(), "tenant", bloomshipper.NewInterval(0, 0), blocks, plan.QueryPlan{AST: expr})
		require.NoError(t, err)
		require.Equal(t, 4, len(res))

		require.Equal(t, expected, res)
	})
}

func shortRef(f, t model.Time, c uint32) *logproto.ShortRef {
	return &logproto.ShortRef{
		From:     f,
		Through:  t,
		Checksum: c,
	}
}

func TestGatewayClient_MergeSeries(t *testing.T) {
	inputs := [][]*logproto.GroupedChunkRefs{
		// response 1 -- sorted
		{
			{Fingerprint: 0x00, Refs: []*logproto.ShortRef{shortRef(0, 1, 1), shortRef(1, 2, 2)}}, // not overlapping
			{Fingerprint: 0x01, Refs: []*logproto.ShortRef{shortRef(0, 1, 3), shortRef(1, 2, 4)}}, // fully overlapping chunks
			{Fingerprint: 0x02, Refs: []*logproto.ShortRef{shortRef(0, 1, 5), shortRef(1, 2, 6)}}, // partially overlapping chunks
		},
		// response 2 -- not sorted
		{
			{Fingerprint: 0x03, Refs: []*logproto.ShortRef{shortRef(0, 1, 8), shortRef(1, 2, 9)}}, // not overlapping
			{Fingerprint: 0x01, Refs: []*logproto.ShortRef{shortRef(0, 1, 3), shortRef(1, 2, 4)}}, // fully overlapping chunks
			{Fingerprint: 0x02, Refs: []*logproto.ShortRef{shortRef(1, 2, 6), shortRef(2, 3, 7)}}, // partially overlapping chunks
		},
	}

	expected := []*logproto.GroupedChunkRefs{
		{Fingerprint: 0x00, Refs: []*logproto.ShortRef{shortRef(0, 1, 1), shortRef(1, 2, 2)}},                    // not overlapping
		{Fingerprint: 0x01, Refs: []*logproto.ShortRef{shortRef(0, 1, 3), shortRef(1, 2, 4)}},                    // fully overlapping chunks
		{Fingerprint: 0x02, Refs: []*logproto.ShortRef{shortRef(0, 1, 5), shortRef(1, 2, 6), shortRef(2, 3, 7)}}, // partially overlapping chunks
		{Fingerprint: 0x03, Refs: []*logproto.ShortRef{shortRef(0, 1, 8), shortRef(1, 2, 9)}},                    // not overlapping
	}

	result, _ := mergeSeries(inputs, nil)
	require.Equal(t, expected, result)
}

func TestGatewayClient_MergeChunkSets(t *testing.T) {
	inp1 := []*logproto.ShortRef{
		shortRef(1, 3, 1),
		shortRef(2, 3, 2),
		shortRef(4, 5, 3),
	}
	inp2 := []*logproto.ShortRef{
		shortRef(2, 3, 2),
		shortRef(3, 4, 4),
		shortRef(5, 6, 5),
	}

	expected := []*logproto.ShortRef{
		shortRef(1, 3, 1),
		shortRef(2, 3, 2),
		shortRef(3, 4, 4),
		shortRef(4, 5, 3),
		shortRef(5, 6, 5),
	}

	result := mergeChunkSets(inp1, inp2)
	require.Equal(t, expected, result)
}
