package distributor

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestDistributorPushConcurrently(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	distributors, ingesters := prepare(t, 1, 5, limits, nil)

	numReq := 1
	var wg sync.WaitGroup
	for i := 0; i < numReq; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			request := makeWriteRequestWithLabels(100, 100,
				[]string{
					fmt.Sprintf(`{app="foo-%d"}`, n),
					fmt.Sprintf(`{instance="bar-%d"}`, n),
				}, false, false, false,
			)
			response, err := distributors[n%len(distributors)].Push(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, &logproto.PushResponse{}, response)
		}(i)
	}

	wg.Wait()
	// make sure the ingesters received the push requests
	time.Sleep(10 * time.Millisecond)

	counter := 0
	labels := make(map[string]int)

	for i := range ingesters {
		ingesters[i].mu.Lock()

		pushed := ingesters[i].pushed
		counter = counter + len(pushed)
		for _, pr := range pushed {
			for _, st := range pr.Streams {
				labels[st.Labels] = labels[st.Labels] + 1
			}
		}
		ingesters[i].mu.Unlock()
	}
	assert.Equal(t, numReq*3, counter) // RF=3
	// each stream is present 3 times
	for i := 0; i < numReq; i++ {
		l := fmt.Sprintf(`{instance="bar-%d"}`, i)
		assert.Equal(t, 3, labels[l], "stream %s expected 3 times, got %d", l, labels[l])
		l = fmt.Sprintf(`{app="foo-%d"}`, i)
		assert.Equal(t, 3, labels[l], "stream %s expected 3 times, got %d", l, labels[l])
	}
}

func TestDistributorPushErrors(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	t.Run("with RF=3 a single push can fail", func(t *testing.T) {
		distributors, ingesters := prepare(t, 1, 3, limits, nil)
		ingesters[0].failAfter = 5 * time.Millisecond
		ingesters[1].succeedAfter = 10 * time.Millisecond
		ingesters[2].succeedAfter = 15 * time.Millisecond

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			return len(ingesters[1].pushed) == 1 && len(ingesters[2].pushed) == 1
		}, time.Second, 10*time.Millisecond)

		require.Equal(t, 0, len(ingesters[0].pushed))
	})
	t.Run("with RF=3 two push failures result in error", func(t *testing.T) {
		distributors, ingesters := prepare(t, 1, 3, limits, nil)
		ingesters[0].failAfter = 5 * time.Millisecond
		ingesters[1].succeedAfter = 10 * time.Millisecond
		ingesters[2].failAfter = 15 * time.Millisecond

		request := makeWriteRequest(10, 64)
		_, err := distributors[0].Push(ctx, request)
		require.Error(t, err)

		require.Eventually(t, func() bool {
			return len(ingesters[1].pushed) == 1
		}, time.Second, 10*time.Millisecond)

		require.Equal(t, 0, len(ingesters[0].pushed))
		require.Equal(t, 0, len(ingesters[2].pushed))
	})
}
