package client

import (
	"context"
	"sync"

	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/rateservice/proto"
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
func (c *MockClient) AllRealms(_ context.Context, in *proto.AllRealmsRequest, _ ...grpc.CallOption) (proto.RateService_AllRealmsClient, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.allRealms = append(c.allRealms, in)
	return nil, nil
}

// GetRealm implements [proto.RateServiceClient].
func (c *MockClient) GetRealm(_ context.Context, in *proto.GetRealmRequest, _ ...grpc.CallOption) (*proto.GetRealmResponse, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.getRealms = append(c.getRealms, in)
	return &proto.GetRealmResponse{}, nil
}

// UpdateRealm implements [proto.RateServiceClient].
func (c *MockClient) UpdateRealm(_ context.Context, in *proto.UpdateRealmRequest, _ ...grpc.CallOption) (*proto.UpdateRealmResponse, error) {
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
