package base

import (
	"context"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/stretchr/testify/mock"
)

type pusherMock struct {
	mock.Mock
}

func newPusherMock() *pusherMock {
	return &pusherMock{}
}

func (m *pusherMock) Push(ctx context.Context, req *cortexpb.WriteRequest) (*cortexpb.WriteResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*cortexpb.WriteResponse), args.Error(1)
}

func (m *pusherMock) MockPush(res *cortexpb.WriteResponse, err error) {
	m.On("Push", mock.Anything, mock.Anything).Return(res, err)
}
