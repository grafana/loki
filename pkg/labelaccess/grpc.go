package labelaccess

import (
	"context"
	"strings"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	lowerLBACHeaderName = "x-prom-label-policy"
)

// injectIntoGRPCRequest takes the LBAC config from the context and puts it in the GRPC metadata
func injectIntoGRPCRequest(ctx context.Context) (context.Context, error) {
	labelPolicySet, err := ExtractLabelMatchersContext(ctx)
	if err != nil {
		return ctx, nil
	}

	if len(labelPolicySet) > 0 {
		trace.SpanFromContext(ctx).SetAttributes(
			attribute.String("inject x-prom-label-policy", labelPolicySet.String()),
		)
	}

	// inject into GRPC metadata
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md = md.Copy()
	var headerValues []string
	for instanceName, policies := range labelPolicySet {
		for _, policy := range policies {
			encoded, err := policyToHeaderValue(instanceName, policy)
			if err != nil {
				return ctx, err
			}
			headerValues = append(headerValues, encoded)
		}
	}
	md.Set(lowerLBACHeaderName, headerValues...)
	newCtx := metadata.NewOutgoingContext(ctx, md)

	return newCtx, nil
}

// ClientLBACInterceptor propagates the LBAC headers from the context to gRPC metadata, which eventually ends up as a HTTP2 header.
func ClientLBACInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx, err := injectIntoGRPCRequest(ctx)
	if err != nil {
		return err
	}

	return invoker(ctx, method, req, reply, cc, opts...)
}

// StreamClientLBACInterceptor propagates the LBAC headers from the context to gRPC metadata, which eventually ends up as a HTTP2 header.
// For streaming gRPC requests.
func StreamClientLBACInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx, err := injectIntoGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return streamer(ctx, desc, cc, method, opts...)
}

// extractFromGRPCRequest takes the LBAC config from the GRPC metadata and puts it in the context
func extractFromGRPCRequest(ctx context.Context) (context.Context, error) {

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// No metadata, just return as is
		return ctx, nil
	}

	headerValues, ok := md[lowerLBACHeaderName]
	if !ok {
		// No LBAC metadata found, just return context
		return ctx, nil
	}

	instancePolicyMap := LabelPolicySet{}
	for _, headerValue := range headerValues {
		for _, v := range strings.Split(headerValue, ",") {
			instanceName, policy, err := policyFromHeaderValue(v)
			if err != nil {
				return ctx, err
			}

			currentPolicies, exists := instancePolicyMap[instanceName]
			if exists {
				instancePolicyMap[instanceName] = append(currentPolicies, policy)
			} else {
				instancePolicyMap[instanceName] = []*types.LabelPolicy{policy}
			}
		}
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("extracted x-prom-label-policy", instancePolicyMap.String()),
	)

	return InjectLabelMatchersContext(ctx, instancePolicyMap), nil
}

// ServerLBACInterceptor propagates the LBAC headers from the gRPC metadata back to our context.
func ServerLBACInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	ctx, err := extractFromGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

// StreamServerLBACInterceptor propagates the LBAC headers from the gRPC metadata back to our context.
func StreamServerLBACInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := extractFromGRPCRequest(ss.Context())
	if err != nil {
		return err
	}

	return handler(srv, serverStream{
		ctx:          ctx,
		ServerStream: ss,
	})
}

type serverStream struct {
	ctx context.Context
	grpc.ServerStream
}

func (ss serverStream) Context() context.Context {
	return ss.ctx
}
