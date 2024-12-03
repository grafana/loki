package types

import (
	"context"

	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types/proto"
)

var _ Transport = &GRPCTransport{}

// GRPCTransport implements the Transport interface using gRPC
type GRPCTransport struct {
	client proto.BlockBuilderServiceClient
}

// NewGRPCTransport creates a new gRPC transport instance
func NewGRPCTransport(conn *grpc.ClientConn) *GRPCTransport {
	return &GRPCTransport{
		client: proto.NewBlockBuilderServiceClient(conn),
	}
}

// SendGetJobRequest implements Transport
func (t *GRPCTransport) SendGetJobRequest(ctx context.Context, req *GetJobRequest) (*GetJobResponse, error) {
	protoReq := &proto.GetJobRequest{
		BuilderId: req.BuilderID,
	}

	resp, err := t.client.GetJob(ctx, protoReq)
	if err != nil {
		return nil, err
	}

	return &GetJobResponse{
		Job: protoToJob(resp.GetJob()),
		OK:  resp.GetOk(),
	}, nil
}

// SendCompleteJob implements Transport
func (t *GRPCTransport) SendCompleteJob(ctx context.Context, req *CompleteJobRequest) error {
	protoReq := &proto.CompleteJobRequest{
		BuilderId: req.BuilderID,
		Job:       jobToProto(req.Job),
	}

	_, err := t.client.CompleteJob(ctx, protoReq)
	return err
}

// SendSyncJob implements Transport
func (t *GRPCTransport) SendSyncJob(ctx context.Context, req *SyncJobRequest) error {
	protoReq := &proto.SyncJobRequest{
		BuilderId: req.BuilderID,
		Job:       jobToProto(req.Job),
	}

	_, err := t.client.SyncJob(ctx, protoReq)
	return err
}

// protoToJob converts a proto Job to a types.Job
func protoToJob(p *proto.Job) *Job {
	if p == nil {
		return nil
	}
	return &Job{
		ID:        p.GetId(),
		Partition: int(p.GetPartition()),
		Offsets: Offsets{
			Min: p.GetOffsets().GetMin(),
			Max: p.GetOffsets().GetMax(),
		},
	}
}

// jobToProto converts a types.Job to a proto Job
func jobToProto(j *Job) *proto.Job {
	if j == nil {
		return nil
	}
	return &proto.Job{
		Id:        j.ID,
		Partition: int32(j.Partition),
		Offsets: &proto.Offsets{
			Min: j.Offsets.Min,
			Max: j.Offsets.Max,
		},
	}
}
