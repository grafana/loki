package client

import (
	"context"
	"sync"

	"github.com/grafana/loki/v3/pkg/rateservice/proto"
	"google.golang.org/grpc"
)

// A MockClient implements [proto.RateServiceClient]. It is used in tests.
type MockClient struct {
	allRealms    []*proto.AllRealmsRequest
	getRealms    []*proto.GetRealmRequest
	updateRealms []*proto.UpdateRealmRequest
	mtx          sync.Mutex
}

// NewMockClient returns a new MockClient.
func NewMockClient() *MockClient {
	return &MockClient{}
}

// GetRealm implements [proto.RateServiceClient].
func (c *MockClient) AllRealms(ctx context.Context, in *proto.AllRealmsRequest, opts ...grpc.CallOption) (proto.RateService_AllRealmsClient, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.allRealms = append(c.allRealms, in)
	return nil, nil
}

// GetRealm implements [proto.RateServiceClient].
func (c *MockClient) GetRealm(ctx context.Context, in *proto.GetRealmRequest, opts ...grpc.CallOption) (*proto.GetRealmResponse, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.getRealms = append(c.getRealms, in)
	return &proto.GetRealmResponse{}, nil
}

// UpdateRealm implements [proto.RateServiceClient].
func (c *MockClient) UpdateRealm(ctx context.Context, in *proto.UpdateRealmRequest, opts ...grpc.CallOption) (*proto.UpdateRealmResponse, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.updateRealms = append(c.updateRealms, in)
	return &proto.UpdateRealmResponse{}, nil
}

// UpdateRealmRequests returns all [proto.UpdateRealmRequests] received since
// the start of the mock, and in the order received.
func (c *MockClient) UpdateRealmRequests() []*proto.UpdateRealmRequest {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.updateRealms
}
