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

func (m *mockedTee) Duplicate(ctx context.Context, tenant string, streams []KeyedStream) {
	m.Called(ctx, tenant, streams)
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
	tee1.On("Duplicate", ctx, "1", streams).Once()
	tee1.On("Duplicate", ctx, "2", streams).Once()
	tee2.On("Duplicate", ctx, "2", streams).Once()
	tee1.On("Duplicate", ctx, "3", streams).Once()
	tee2.On("Duplicate", ctx, "3", streams).Once()
	tee3.On("Duplicate", ctx, "3", streams).Once()

	wrappedTee := WrapTee(nil, tee1)
	wrappedTee.Duplicate(ctx, "1", streams)

	wrappedTee = WrapTee(wrappedTee, tee2)
	wrappedTee.Duplicate(ctx, "2", streams)

	wrappedTee = WrapTee(wrappedTee, tee3)
	wrappedTee.Duplicate(ctx, "3", streams)

	tee1.AssertExpectations(t)
	tee2.AssertExpectations(t)
	tee3.AssertExpectations(t)
}
