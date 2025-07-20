package types

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types/proto"
)

// schedulerServer implements proto.SchedulerServiceServer by delegating to a SchedulerHandler
type schedulerServer struct {
	handler SchedulerHandler
}

// NewSchedulerServer creates a new gRPC server that delegates to the provided handler
func NewSchedulerServer(handler SchedulerHandler) proto.SchedulerServiceServer {
	return &schedulerServer{handler: handler}
}

// GetJob implements proto.SchedulerServiceServer
func (s *schedulerServer) GetJob(ctx context.Context, _ *proto.GetJobRequest) (*proto.GetJobResponse, error) {
	job, ok, err := s.handler.HandleGetJob(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !ok {
		return &proto.GetJobResponse{
			Job: nil,
			Ok:  false,
		}, nil
	}
	return &proto.GetJobResponse{
		Job: jobToProto(job),
		Ok:  true,
	}, nil
}

// CompleteJob implements proto.SchedulerServiceServer
func (s *schedulerServer) CompleteJob(ctx context.Context, req *proto.CompleteJobRequest) (*proto.CompleteJobResponse, error) {
	if err := s.handler.HandleCompleteJob(ctx, protoToJob(req.Job), req.Success); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &proto.CompleteJobResponse{}, nil
}

// SyncJob implements proto.SchedulerServiceServer
func (s *schedulerServer) SyncJob(ctx context.Context, req *proto.SyncJobRequest) (*proto.SyncJobResponse, error) {
	if err := s.handler.HandleSyncJob(ctx, protoToJob(req.Job)); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &proto.SyncJobResponse{}, nil
}
