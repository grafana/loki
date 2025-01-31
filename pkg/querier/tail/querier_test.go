package tail

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
)

const queryTimeout = 1 * time.Hour

func mockInstanceDesc(addr string, state ring.InstanceState) ring.InstanceDesc {
	return ring.InstanceDesc{
		Addr:      addr,
		State:     state,
		Timestamp: time.Now().Unix(),
	}
}

func TestQuerier_Tail_QueryTimeoutConfigFlag(t *testing.T) {
	request := logproto.TailRequest{
		Query:    `{type="test"}`,
		DelayFor: 0,
		Limit:    10,
		Start:    time.Now(),
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{type="test"}`),
		},
	}

	// Setup mocks
	tailClient := newTailClientMock()
	tailClient.On("Recv").Return(mockTailResponse(logproto.Stream{
		Labels: `{type="test"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Now(), Line: "line 1"},
			{Timestamp: time.Now(), Line: "line 2"},
		},
	}), nil)

	// Setup ingester mock
	ingester := newMockTailIngester()
	clients := map[string]logproto.Querier_TailClient{
		"ingester-1": tailClient,
	}
	ingester.On("Tail", mock.Anything, &request).Return(clients, nil)
	ingester.On("TailersCount", mock.Anything).Return([]uint32{0}, nil)

	// Setup log selector mock
	logSelector := newMockTailLogSelector()
	logSelector.On("SelectLogs", mock.Anything, mock.Anything).Return(iter.NoopEntryIterator, nil)

	// Setup limits
	limits := &testutil.MockLimits{
		MaxQueryTimeoutVal:            queryTimeout,
		MaxStreamsMatchersPerQueryVal: 100,
		MaxConcurrentTailRequestsVal:  10,
	}

	// Create tail querier
	tailQuerier := NewQuerier(ingester, logSelector, newMockDeleteGettter("test", []deletion.DeleteRequest{}), limits, 7*24*time.Hour, NewMetrics(nil), log.NewNopLogger())

	// Run test
	ctx := user.InjectOrgID(context.Background(), "test")
	_, err := tailQuerier.Tail(ctx, &request, false)
	require.NoError(t, err)

	// Verify expectations
	ingester.AssertExpectations(t)
	logSelector.AssertExpectations(t)
}

func TestQuerier_concurrentTailLimits(t *testing.T) {
	request := logproto.TailRequest{
		Query:    "{type=\"test\"}",
		DelayFor: 0,
		Limit:    10,
		Start:    time.Now(),
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr("{type=\"test\"}"),
		},
	}

	t.Parallel()

	tests := map[string]struct {
		ringIngesters []ring.InstanceDesc
		expectedError error
		ingesterError error
		tailersCount  uint32
	}{
		"empty ring": {
			ringIngesters: []ring.InstanceDesc{},
			expectedError: httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found"),
			ingesterError: httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found"),
		},
		"ring containing one pending ingester": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.PENDING)},
			expectedError: httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found"),
			ingesterError: httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found"),
		},
		"ring containing one active ingester and 0 active tailers": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
		},
		"ring containing one active ingester and 1 active tailer": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
			tailersCount:  1,
		},
		"ring containing one pending and active ingester with 1 active tailer": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.PENDING), mockInstanceDesc("2.2.2.2", ring.ACTIVE)},
			tailersCount:  1,
		},
		"ring containing one active ingester and max active tailers": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
			expectedError: httpgrpc.Errorf(http.StatusBadRequest,
				"max concurrent tail requests limit exceeded, count > limit (%d > %d)", 6, 5),
			tailersCount: 5,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			ctx := user.InjectOrgID(context.Background(), "test")
			// Setup mocks
			tailClient := newTailClientMock()
			tailClient.On("Recv").Return(mockTailResponse(logproto.Stream{
				Labels: `{type="test"}`,
				Entries: []logproto.Entry{
					{Timestamp: time.Now(), Line: "line 1"},
					{Timestamp: time.Now(), Line: "line 2"},
				},
			}), nil)

			// Setup ingester mock
			ingester := newMockTailIngester()
			clients := map[string]logproto.Querier_TailClient{
				"ingester-1": tailClient,
			}
			ingester.On("Tail", mock.Anything, &request).Return(clients, testData.ingesterError)
			ingester.On("TailersCount", mock.Anything).Return([]uint32{testData.tailersCount}, testData.ingesterError)

			// Setup log selector mock
			logSelector := newMockTailLogSelector()
			logSelector.On("SelectLogs", mock.Anything, mock.Anything).Return(iter.NoopEntryIterator, nil)

			// Setup limits
			limits := &testutil.MockLimits{
				MaxConcurrentTailRequestsVal:  5,
				MaxStreamsMatchersPerQueryVal: 100,
			}

			// Create tail querier
			tailQuerier := NewQuerier(ingester, logSelector, newMockDeleteGettter("test", []deletion.DeleteRequest{}), limits, 7*24*time.Hour, NewMetrics(nil), log.NewNopLogger())

			// Run
			_, err := tailQuerier.Tail(ctx, &request, false)
			assert.Equal(t, testData.expectedError, err)

			// Verify expectations if we expect the request to succeed
			if testData.expectedError == nil {
				ingester.AssertExpectations(t)
				logSelector.AssertExpectations(t)
			}
		})
	}
}
