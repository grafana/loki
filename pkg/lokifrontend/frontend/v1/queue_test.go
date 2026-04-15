package v1

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v1/frontendv1pb"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

func setupFrontend(t *testing.T, config Config) *Frontend {
	logger := log.NewNopLogger()

	frontend, err := New(config, mockLimits{queriers: 3}, logger, nil, constants.Loki)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, services.StopAndAwaitTerminated(context.Background(), frontend))
	})
	return frontend
}

func testReq(ctx context.Context, reqID, user string) *request {
	return &request{
		originalCtx: ctx,
		err:         make(chan error, 1),
		request: &httpgrpc.HTTPRequest{
			// Good enough for testing.
			Method: user,
			Url:    reqID,
		},
		response: make(chan *httpgrpc.HTTPResponse, 1),
	}
}

func TestDequeuesExpiredRequests(t *testing.T) {
	var config Config
	flagext.DefaultValues(&config)
	config.MaxOutstandingPerTenant = 10
	userID := "1"

	f := setupFrontend(t, config)

	ctx := user.InjectOrgID(context.Background(), userID)
	expired, cancel := context.WithCancel(ctx)
	cancel()

	good := 0
	for i := 0; i < config.MaxOutstandingPerTenant; i++ {
		var err error
		if i%5 == 0 {
			good++
			err = f.queueRequest(ctx, testReq(ctx, fmt.Sprintf("good-%d", i), userID))
		} else {
			err = f.queueRequest(ctx, testReq(expired, fmt.Sprintf("expired-%d", i), userID))
		}

		require.Nil(t, err)
	}

	// Calling Process will only return when client disconnects or context is finished.
	// We use context timeout to stop Process call.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()

	m := &processServerMock{ctx: ctx2, querierID: "querier"}
	err := f.Process(m)
	require.EqualError(t, err, context.DeadlineExceeded.Error())

	// Verify that only non-expired requests were forwarded to querier.
	for _, r := range m.requests {
		require.True(t, strings.HasPrefix(r.Url, "good-"), r.Url)
	}
	require.Len(t, m.requests, good)
}

func TestRoundRobinQueues(t *testing.T) {
	var config Config
	flagext.DefaultValues(&config)

	const (
		requests = 100
		tenants  = 10
	)

	config.MaxOutstandingPerTenant = requests

	f := setupFrontend(t, config)

	for i := 0; i < requests; i++ {
		userID := fmt.Sprint(i / tenants)
		ctx := user.InjectOrgID(context.Background(), userID)

		err := f.queueRequest(ctx, testReq(ctx, fmt.Sprintf("%d", i), userID))
		require.NoError(t, err)
	}

	// Calling Process will only return when client disconnects or context is finished.
	// We use context timeout to stop Process call.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	m := &processServerMock{ctx: ctx, querierID: "querier"}
	err := f.Process(m)
	require.EqualError(t, err, context.DeadlineExceeded.Error())

	require.Len(t, m.requests, requests)
	for i, r := range m.requests {
		intUserID, err := strconv.Atoi(r.Method)
		require.NoError(t, err)

		require.Equal(t, i%tenants, intUserID)
	}
}

// This mock behaves as connected querier worker to frontend. It will remember each request
// that frontend sends, and reply with 200 HTTP status code.
type processServerMock struct {
	ctx       context.Context
	querierID string

	response *frontendv1pb.ClientToFrontend

	requests []*httpgrpc.HTTPRequest
}

func (p *processServerMock) Send(client *frontendv1pb.FrontendToClient) error {
	switch {
	case client.GetType() == frontendv1pb.GET_ID:
		p.response = &frontendv1pb.ClientToFrontend{ClientID: p.querierID}
		return nil

	case client.GetType() == frontendv1pb.HTTP_REQUEST:
		p.requests = append(p.requests, client.HttpRequest)
		p.response = &frontendv1pb.ClientToFrontend{HttpResponse: &httpgrpc.HTTPResponse{Code: 200}}
		return nil

	default:
		return errors.New("unknown message")
	}
}

func (p *processServerMock) Recv() (*frontendv1pb.ClientToFrontend, error) {
	if p.response != nil {
		m := p.response
		p.response = nil
		return m, nil
	}
	return nil, errors.New("no message")
}

func (p *processServerMock) SetHeader(_ metadata.MD) error  { return nil }
func (p *processServerMock) SendHeader(_ metadata.MD) error { return nil }
func (p *processServerMock) SetTrailer(_ metadata.MD)       {}
func (p *processServerMock) Context() context.Context       { return p.ctx }
func (p *processServerMock) SendMsg(_ interface{}) error    { return nil }
func (p *processServerMock) RecvMsg(_ interface{}) error    { return nil }
