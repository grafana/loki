package distributor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/grafana/loki/pkg/push"
)

type mockedTee struct {
	mock.Mock
}

func (m *mockedTee) Duplicate(ctx context.Context, tenant string, streams []KeyedStream, pushTracker *PushTracker) {
	m.Called(ctx, tenant, streams, pushTracker)
}

func (m *mockedTee) Register(ctx context.Context, tenant string, streams []KeyedStream, pushTracker *PushTracker) {
	m.Called(ctx, tenant, streams, pushTracker)
}

func TestWrapTee(t *testing.T) {
	ctx := t.Context()
	tee1 := new(mockedTee)
	tee2 := new(mockedTee)
	tee3 := new(mockedTee)
	streams := []KeyedStream{
		{
			HashKey: 1,
			Stream:  push.Stream{},
		},
	}
	pushTracker := &PushTracker{
		done: make(chan struct{}, 1),
		err:  make(chan error, 1),
	}
	tee1.On("Duplicate", ctx, "1", streams, pushTracker).Once()
	tee1.On("Duplicate", ctx, "2", streams, pushTracker).Once()
	tee2.On("Duplicate", ctx, "2", streams, pushTracker).Once()
	tee1.On("Duplicate", ctx, "3", streams, pushTracker).Once()
	tee2.On("Duplicate", ctx, "3", streams, pushTracker).Once()
	tee3.On("Duplicate", ctx, "3", streams, pushTracker).Once()

	wrappedTee := WrapTee(nil, tee1)
	wrappedTee.Duplicate(ctx, "1", streams, pushTracker)

	wrappedTee = WrapTee(wrappedTee, tee2)
	wrappedTee.Duplicate(ctx, "2", streams, pushTracker)

	wrappedTee = WrapTee(wrappedTee, tee3)
	wrappedTee.Duplicate(ctx, "3", streams, pushTracker)

	tee1.AssertExpectations(t)
	tee2.AssertExpectations(t)
	tee3.AssertExpectations(t)
}
