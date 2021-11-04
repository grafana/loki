package querier

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

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
				newReadRingMock(testData.ringIngesters),
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
