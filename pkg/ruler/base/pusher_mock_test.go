package base

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type pusherMock struct {
	mock.Mock
}

func newPusherMock() *pusherMock {
	return &pusherMock{}
}

func (m *pusherMock) Push(ctx context.Context, req *logproto.WriteRequest) (*logproto.WriteResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*logproto.WriteResponse), args.Error(1)
}

func (m *pusherMock) MockPush(res *logproto.WriteResponse, err error) {
	m.On("Push", mock.Anything, mock.Anything).Return(res, err)
}
