package metastore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	metastorepb "github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
)

func TestMetastore_ListBlocksForQuery(t *testing.T) {
	block1, block2, block3 := &metastorepb.BlockMeta{
		Id:      "block1",
		MinTime: 0,
		MaxTime: 100,
		TenantStreams: []*metastorepb.TenantStreams{
			{
				TenantId: "tenant1",
				MinTime:  0,
				MaxTime:  50,
			},
		},
	}, &metastorepb.BlockMeta{
		Id:      "block2",
		MinTime: 100,
		MaxTime: 200,
		TenantStreams: []*metastorepb.TenantStreams{
			{
				TenantId: "tenant1",
				MinTime:  100,
				MaxTime:  150,
			},
		},
	}, &metastorepb.BlockMeta{
		Id:      "block3",
		MinTime: 200,
		MaxTime: 300,
		TenantStreams: []*metastorepb.TenantStreams{
			{
				TenantId: "tenant2",
				MinTime:  200,
				MaxTime:  250,
			},
			{
				TenantId: "tenant1",
				MinTime:  200,
				MaxTime:  250,
			},
		},
	}
	m := &Metastore{
		state: &metastoreState{
			segments: map[string]*metastorepb.BlockMeta{
				"block1": block1,
				"block2": block2,
				"block3": block3,
			},
		},
	}

	tests := []struct {
		name             string
		request          *metastorepb.ListBlocksForQueryRequest
		expectedResponse *metastorepb.ListBlocksForQueryResponse
	}{
		{
			name: "Matching tenant and time range",
			request: &metastorepb.ListBlocksForQueryRequest{
				TenantId:  "tenant1",
				StartTime: 0,
				EndTime:   100,
			},
			expectedResponse: &metastorepb.ListBlocksForQueryResponse{
				Blocks: []*metastorepb.BlockMeta{
					block1,
					block2,
				},
			},
		},
		{
			name: "Matching tenant but partial time range",
			request: &metastorepb.ListBlocksForQueryRequest{
				TenantId:  "tenant1",
				StartTime: 50,
				EndTime:   150,
			},
			expectedResponse: &metastorepb.ListBlocksForQueryResponse{
				Blocks: []*metastorepb.BlockMeta{
					block1,
					block2,
				},
			},
		},
		{
			name: "Non-matching tenant",
			request: &metastorepb.ListBlocksForQueryRequest{
				TenantId:  "tenant3",
				StartTime: 0,
				EndTime:   100,
			},
			expectedResponse: &metastorepb.ListBlocksForQueryResponse{},
		},
		{
			name: "Matching one tenant but not the other",
			request: &metastorepb.ListBlocksForQueryRequest{
				TenantId:  "tenant1",
				StartTime: 100,
				EndTime:   550,
			},
			expectedResponse: &metastorepb.ListBlocksForQueryResponse{
				Blocks: []*metastorepb.BlockMeta{
					block1,
					block2,
					block3,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, err := m.ListBlocksForQuery(context.Background(), test.request)
			require.NoError(t, err)
			require.Equal(t, test.expectedResponse, resp)
		})
	}
}
