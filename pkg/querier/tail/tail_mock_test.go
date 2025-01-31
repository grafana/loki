package tail

import (
	"context"
	"time"

	grpc_metadata "google.golang.org/grpc/metadata"

	"github.com/stretchr/testify/mock"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/util"
)

// mockTailIngester implements tailIngester interface for testing
type mockTailIngester struct {
	mock.Mock
}

func newMockTailIngester() *mockTailIngester {
	return &mockTailIngester{}
}

func (m *mockTailIngester) TailDisconnectedIngesters(ctx context.Context, req *logproto.TailRequest, connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
	args := m.Called(ctx, req, connectedIngestersAddr)
	return args.Get(0).(map[string]logproto.Querier_TailClient), args.Error(1)
}

func (m *mockTailIngester) TailersCount(ctx context.Context) ([]uint32, error) {
	args := m.Called(ctx)
	return args.Get(0).([]uint32), args.Error(1)
}

func (m *mockTailIngester) Tail(ctx context.Context, req *logproto.TailRequest) (map[string]logproto.Querier_TailClient, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(map[string]logproto.Querier_TailClient), args.Error(1)
}

// mockTailLogSelector implements tailLogSelector interface for testing
type mockTailLogSelector struct {
	mock.Mock
}

func newMockTailLogSelector() *mockTailLogSelector {
	return &mockTailLogSelector{}
}

func (m *mockTailLogSelector) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(iter.EntryIterator), args.Error(1)
}

func mockTailResponse(stream logproto.Stream) *logproto.TailResponse {
	return &logproto.TailResponse{
		Stream:         &stream,
		DroppedStreams: []*logproto.DroppedStream{},
	}
}

// tailClientMock is mockable version of Querier_TailClient
type tailClientMock struct {
	util.ExtendedMock
	logproto.Querier_TailClient
	recvTrigger chan time.Time
}

func newTailClientMock() *tailClientMock {
	return &tailClientMock{
		recvTrigger: make(chan time.Time, 10),
	}
}

func (c *tailClientMock) Recv() (*logproto.TailResponse, error) {
	args := c.Called()
	return args.Get(0).(*logproto.TailResponse), args.Error(1)
}

func (c *tailClientMock) Header() (grpc_metadata.MD, error) {
	return nil, nil
}

func (c *tailClientMock) Trailer() grpc_metadata.MD {
	return nil
}

func (c *tailClientMock) CloseSend() error {
	return nil
}

func (c *tailClientMock) Context() context.Context {
	return context.Background()
}

func (c *tailClientMock) SendMsg(_ interface{}) error {
	return nil
}

func (c *tailClientMock) RecvMsg(_ interface{}) error {
	return nil
}

func (c *tailClientMock) mockRecvWithTrigger(response *logproto.TailResponse) *tailClientMock {
	c.On("Recv").WaitUntil(c.recvTrigger).Return(response, nil)
	return c
}

func (c *tailClientMock) triggerRecv() {
	c.recvTrigger <- time.Now()
}

type mockDeleteGettter struct {
	user    string
	results []deletion.DeleteRequest
}

func newMockDeleteGettter(user string, results []deletion.DeleteRequest) *mockDeleteGettter {
	return &mockDeleteGettter{
		user:    user,
		results: results,
	}
}

func (d *mockDeleteGettter) GetAllDeleteRequestsForUser(_ context.Context, userID string) ([]deletion.DeleteRequest, error) {
	d.user = userID
	return d.results, nil
}
