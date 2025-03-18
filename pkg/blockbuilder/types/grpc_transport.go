package types

import (
	"context"
	"io"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/middleware"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types/proto"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

var _ BuilderTransport = &GRPCTransport{}

type grpcTransportMetrics struct {
	requestLatency *prometheus.HistogramVec
}

func newGRPCTransportMetrics(registerer prometheus.Registerer) *grpcTransportMetrics {
	return &grpcTransportMetrics{
		requestLatency: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "block_builder_grpc",
			Name:      "request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using the block builder grpc transport",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
	}
}

// GRPCTransport implements the Transport interface using gRPC
type GRPCTransport struct {
	grpc_health_v1.HealthClient
	io.Closer
	proto.SchedulerServiceClient
}

// NewGRPCTransportFromAddress creates a new gRPC transport instance from an address and dial options
func NewGRPCTransportFromAddress(
	address string,
	cfg grpcclient.Config,
	reg prometheus.Registerer,
) (*GRPCTransport, error) {
	metrics := newGRPCTransportMetrics(reg)
	dialOpts, err := cfg.DialOption(
		[]grpc.UnaryClientInterceptor{
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.UnaryClientInstrumentInterceptor(metrics.requestLatency),
		}, []grpc.StreamClientInterceptor{
			otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
			middleware.StreamClientInstrumentInterceptor(metrics.requestLatency),
		},
		middleware.NoOpInvalidClusterValidationReporter,
	)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(address, dialOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "new grpc pool dial")
	}

	return &GRPCTransport{
		Closer:                 conn,
		HealthClient:           grpc_health_v1.NewHealthClient(conn),
		SchedulerServiceClient: proto.NewSchedulerServiceClient(conn),
	}, nil
}

// SendGetJobRequest implements Transport
func (t *GRPCTransport) SendGetJobRequest(ctx context.Context, req *GetJobRequest) (*GetJobResponse, error) {
	protoReq := &proto.GetJobRequest{
		BuilderId: req.BuilderID,
	}

	resp, err := t.GetJob(ctx, protoReq)
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
		Success:   req.Success,
	}

	_, err := t.CompleteJob(ctx, protoReq)
	return err
}

// SendSyncJob implements Transport
func (t *GRPCTransport) SendSyncJob(ctx context.Context, req *SyncJobRequest) error {
	protoReq := &proto.SyncJobRequest{
		BuilderId: req.BuilderID,
		Job:       jobToProto(req.Job),
	}

	_, err := t.SyncJob(ctx, protoReq)
	return err
}

// protoToJob converts a proto Job to a types.Job
func protoToJob(p *proto.Job) *Job {
	if p == nil {
		return nil
	}
	return &Job{
		id:        p.GetId(),
		partition: p.GetPartition(),
		offsets: Offsets{
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
		Id:        j.ID(),
		Partition: j.Partition(),
		Offsets: &proto.Offsets{
			Min: j.offsets.Min,
			Max: j.offsets.Max,
		},
	}
}
