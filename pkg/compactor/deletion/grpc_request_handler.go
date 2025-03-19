package deletion

import (
	"context"
	"sort"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type GRPCRequestHandler struct {
	deleteRequestsStore DeleteRequestsStore
	limits              Limits
}

func NewGRPCRequestHandler(deleteRequestsStore DeleteRequestsStore, limits Limits) *GRPCRequestHandler {
	return &GRPCRequestHandler{
		deleteRequestsStore: deleteRequestsStore,
		limits:              limits,
	}
}

func (g *GRPCRequestHandler) GetDeleteRequests(ctx context.Context, req *grpc.GetDeleteRequestsRequest) (*grpc.GetDeleteRequestsResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	hasDelete, err := validDeletionLimit(g.limits, userID)
	if err != nil {
		return nil, err
	}

	if !hasDelete {
		return nil, errors.New(deletionNotAvailableMsg)
	}

	deleteGroups, err := g.deleteRequestsStore.GetAllDeleteRequestsForUser(ctx, userID, req.ForQuerytimeFiltering)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
		return nil, err
	}

	deleteRequests := mergeDeletes(deleteGroups)

	sort.Slice(deleteRequests, func(i, j int) bool {
		return deleteRequests[i].CreatedAt < deleteRequests[j].CreatedAt
	})

	resp := grpc.GetDeleteRequestsResponse{
		DeleteRequests: make([]*grpc.DeleteRequest, len(deleteRequests)),
	}
	for i, dr := range deleteRequests {
		resp.DeleteRequests[i] = &grpc.DeleteRequest{
			RequestID: dr.RequestID,
			StartTime: int64(dr.StartTime),
			EndTime:   int64(dr.EndTime),
			Query:     dr.Query,
			Status:    string(dr.Status),
			CreatedAt: int64(dr.CreatedAt),
		}
	}

	return &resp, nil
}

func (g *GRPCRequestHandler) GetCacheGenNumbers(ctx context.Context, _ *grpc.GetCacheGenNumbersRequest) (*grpc.GetCacheGenNumbersResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	hasDelete, err := validDeletionLimit(g.limits, userID)
	if err != nil {
		return nil, err
	}

	if !hasDelete {
		return nil, errors.New(deletionNotAvailableMsg)
	}

	cacheGenNumber, err := g.deleteRequestsStore.GetCacheGenerationNumber(ctx, userID)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting cache generation number", "err", err)
		return nil, err
	}

	return &grpc.GetCacheGenNumbersResponse{ResultsCacheGen: cacheGenNumber}, nil
}
