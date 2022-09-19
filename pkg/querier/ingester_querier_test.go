package querier

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

func TestIngesterQuerier_earlyExitOnQuorum(t *testing.T) {
	ringIngesters := []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("2.2.2.2", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE)}

	t.Parallel()
	t.Run("label call should return early on reaching quorum", func(t *testing.T) {
		cnt := 0
		wg := sync.WaitGroup{}
		wait := make(chan struct{})

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("Label", mock.Anything, mock.Anything, mock.Anything).Return(new(logproto.LabelResponse), nil).Run(
			func(args mock.Arguments) {
				wg.Done()

				ctx := args[0].(context.Context)
				for {
					select {
					case <-ctx.Done():
						// expected to be cancelled once the first two replicas return
						require.ErrorIs(t, ctx.Err(), context.Canceled)
						return
					case <-wait:
						cnt++
						return
					}
				}
			},
		)
		ingesterQuerier, err := newIngesterQuerier(
			mockIngesterClientConfig(),
			newReadRingMock(ringIngesters, 1),
			mockQuerierConfig().ExtraQueryDelay,
			newIngesterClientMockFactory(ingesterClient),
		)
		require.NoError(t, err)

		// Ensure the testcase completes by timing out incase the context doesn't get cancelled
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		wg.Add(3)
		go func() {
			// wait for all 3 replicas to get called before returning response
			wg.Wait()

			// return response from 2 of the 3 replicas
			wait <- struct{}{}
			wait <- struct{}{}
		}()

		_, err = ingesterQuerier.Label(ctx, new(logproto.LabelRequest))
		ingesterClient.AssertNumberOfCalls(t, "Label", 3)
		require.Equal(t, 2, cnt)
		require.NoError(t, err)
	})

	t.Run("select logs should not return early on reaching quorum", func(t *testing.T) {
		cnt := 0
		wg := sync.WaitGroup{}
		wait := make(chan struct{})

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(newQueryClientMock(), nil).Run(
			func(args mock.Arguments) {
				wg.Done()
				ctx := args[0].(context.Context)
				for {
					select {
					case <-ctx.Done():
						// should not be cancelled by the tracker
						require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
						return
					case <-wait:
						cnt++
						return
					}
				}
			},
		)

		ingesterQuerier, err := newIngesterQuerier(
			mockIngesterClientConfig(),
			newReadRingMock(ringIngesters, 1),
			mockQuerierConfig().ExtraQueryDelay,
			newIngesterClientMockFactory(ingesterClient),
		)
		require.NoError(t, err)

		wg.Add(3)
		go func() {
			// wait for all 3 replicas to get called before returning response
			wg.Wait()

			// return response from 2 out of the 3 replicas
			wait <- struct{}{}
			wait <- struct{}{}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_, err = ingesterQuerier.SelectLogs(ctx, logql.SelectLogParams{
			QueryRequest: new(logproto.QueryRequest),
		})
		ingesterClient.AssertNumberOfCalls(t, "Query", 3)
		require.NoError(t, err)
		require.Equal(t, 2, cnt)
	})
}

func TestQuerier_tailDisconnectedIngesters(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		connectedIngestersAddr []string
		ringIngesters          []ring.InstanceDesc
		expectedClientsAddr    []string
	}{
		"no connected ingesters and empty ring": {
			connectedIngestersAddr: []string{},
			ringIngesters:          []ring.InstanceDesc{},
			expectedClientsAddr:    []string{},
		},
		"no connected ingesters and ring containing new ingesters": {
			connectedIngestersAddr: []string{},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
			expectedClientsAddr:    []string{"1.1.1.1"},
		},
		"connected ingesters and ring contain the same ingesters": {
			connectedIngestersAddr: []string{"1.1.1.1", "2.2.2.2"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("2.2.2.2", ring.ACTIVE), mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
			expectedClientsAddr:    []string{},
		},
		"ring contains new ingesters compared to the connected one": {
			connectedIngestersAddr: []string{"1.1.1.1"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("2.2.2.2", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE)},
			expectedClientsAddr:    []string{"2.2.2.2", "3.3.3.3"},
		},
		"connected ingesters contain ingesters not in the ring anymore": {
			connectedIngestersAddr: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE)},
			expectedClientsAddr:    []string{},
		},
		"connected ingesters contain ingesters not in the ring anymore and the ring contains new ingesters too": {
			connectedIngestersAddr: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE), mockInstanceDesc("4.4.4.4", ring.ACTIVE)},
			expectedClientsAddr:    []string{"4.4.4.4"},
		},
		"ring contains ingester in LEAVING state not listed in the connected ingesters": {
			connectedIngestersAddr: []string{"1.1.1.1"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("2.2.2.2", ring.LEAVING)},
			expectedClientsAddr:    []string{},
		},
		"ring contains ingester in PENDING state not listed in the connected ingesters": {
			connectedIngestersAddr: []string{"1.1.1.1"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("2.2.2.2", ring.PENDING)},
			expectedClientsAddr:    []string{},
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			req := logproto.TailRequest{
				Query:    "{type=\"test\"}",
				DelayFor: 0,
				Limit:    10,
				Start:    time.Now(),
			}

			// For this test's purpose, whenever a new ingester client needs to
			// be created, the factory will always return the same mock instance
			ingesterClient := newQuerierClientMock()
			ingesterClient.On("Tail", mock.Anything, &req, mock.Anything).Return(newTailClientMock(), nil)

			ingesterQuerier, err := newIngesterQuerier(
				mockIngesterClientConfig(),
				newReadRingMock(testData.ringIngesters, 0),
				mockQuerierConfig().ExtraQueryDelay,
				newIngesterClientMockFactory(ingesterClient),
			)
			require.NoError(t, err)

			actualClients, err := ingesterQuerier.TailDisconnectedIngesters(context.Background(), &req, testData.connectedIngestersAddr)
			require.NoError(t, err)

			actualClientsAddr := make([]string, 0, len(actualClients))
			for addr, client := range actualClients {
				actualClientsAddr = append(actualClientsAddr, addr)

				// The returned map of clients should never contain nil values
				assert.NotNil(t, client)
			}

			assert.ElementsMatch(t, testData.expectedClientsAddr, actualClientsAddr)
		})
	}
}

func TestConvertMatchersToString(t *testing.T) {
	for _, tc := range []struct {
		name     string
		matchers []*labels.Matcher
		expected string
	}{
		{
			name:     "empty matchers",
			matchers: []*labels.Matcher{},
			expected: "{}",
		},
		{
			name: "with matchers",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "foo", "equal"),
				labels.MustNewMatcher(labels.MatchNotEqual, "bar", "not-equal"),
			},
			expected: "{foo=\"equal\",bar!=\"not-equal\"}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, convertMatchersToString(tc.matchers))
		})
	}
}
