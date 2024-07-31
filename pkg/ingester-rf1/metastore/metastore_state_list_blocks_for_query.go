package metastore

import (
	"context"
	"slices"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
)

func (m *Metastore) ListBlocksForQuery(
	ctx context.Context,
	request *metastorepb.ListBlocksForQueryRequest,
) (
	*metastorepb.ListBlocksForQueryResponse, error,
) {
	return m.state.listBlocksForQuery(ctx, request)
}

func (m *metastoreState) listBlocksForQuery(
	_ context.Context,
	request *metastorepb.ListBlocksForQueryRequest,
) (
	*metastorepb.ListBlocksForQueryResponse, error,
) {
	if len(request.TenantId) == 0 {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	if request.StartTime > request.EndTime {
		return nil, status.Error(codes.InvalidArgument, "start_time must be less than or equal to end_time")
	}
	var resp metastorepb.ListBlocksForQueryResponse
	m.segmentsMutex.Lock()
	defer m.segmentsMutex.Unlock()

	for _, segment := range m.segments {
		for _, tenants := range segment.TenantStreams {
			if tenants.TenantId == request.TenantId && inRange(segment.MinTime, segment.MaxTime, request.StartTime, request.EndTime) {
				resp.Blocks = append(resp.Blocks, cloneBlockForQuery(segment))
				break
			}
		}
	}
	slices.SortFunc(resp.Blocks, func(a, b *metastorepb.BlockMeta) int {
		return strings.Compare(a.Id, b.Id)
	})
	return &resp, nil
}

func inRange(blockStart, blockEnd, queryStart, queryEnd int64) bool {
	return blockStart <= queryEnd && blockEnd >= queryStart
}

func cloneBlockForQuery(b *metastorepb.BlockMeta) *metastorepb.BlockMeta {
	res := &metastorepb.BlockMeta{
		Id:              b.Id,
		MinTime:         b.MinTime,
		MaxTime:         b.MaxTime,
		CompactionLevel: b.CompactionLevel,
		FormatVersion:   b.FormatVersion,
		IndexRef: metastorepb.DataRef{
			Offset: b.IndexRef.Offset,
			Length: b.IndexRef.Length,
		},
		TenantStreams: make([]*metastorepb.TenantStreams, 0, len(b.TenantStreams)),
	}
	for _, svc := range b.TenantStreams {
		res.TenantStreams = append(res.TenantStreams, &metastorepb.TenantStreams{
			TenantId: svc.TenantId,
			MinTime:  svc.MinTime,
			MaxTime:  svc.MaxTime,
		})
	}
	return res
}
