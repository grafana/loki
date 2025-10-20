package deletion

import (
	"context"
	"sort"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
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

	deleteRequests, err := g.deleteRequestsStore.GetAllDeleteRequestsForUser(ctx, userID, req.ForQuerytimeFiltering)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
		return nil, err
	}

	sort.Slice(deleteRequests, func(i, j int) bool {
		return deleteRequests[i].CreatedAt < deleteRequests[j].CreatedAt
	})

	resp := grpc.GetDeleteRequestsResponse{
		DeleteRequests: make([]*deletionproto.DeleteRequest, len(deleteRequests)),
	}
	for i := range deleteRequests {
		resp.DeleteRequests[i] = &deleteRequests[i]
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
