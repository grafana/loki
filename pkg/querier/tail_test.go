package querier

import (
	"errors"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/iter"
	loghttp "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	timeout  = 1 * time.Second
	throttle = 10 * time.Millisecond
)

func TestTailer(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		historicEntries iter.EntryIterator
		tailClient      *tailClientMock
		tester          func(t *testing.T, tailer *Tailer, tailClient *tailClientMock)
	}{
		"tail logs from historic entries only (no tail clients provided)": {
			historicEntries: mockStreamIterator(1, 2),
			tailClient:      nil,
			tester: func(t *testing.T, tailer *Tailer, tailClient *tailClientMock) {
				responses, err := readFromTailer(tailer, 2)
				require.NoError(t, err)

				actual := flattenStreamsFromResponses(responses)

				assert.Equal(t, []logproto.Stream{
					*mockStream(1, 1),
					*mockStream(2, 1),
				}, actual)
			},
		},
		"tail logs from tail clients only (no historic entries provided)": {
			historicEntries: mockStreamIterator(0, 0),
			tailClient:      newTailClientMock().mockRecvWithTrigger(mockTailResponse(mockStream(1, 1))),
			tester: func(t *testing.T, tailer *Tailer, tailClient *tailClientMock) {
				tailClient.triggerRecv()

				responses, err := readFromTailer(tailer, 1)
				require.NoError(t, err)

				actual := flattenStreamsFromResponses(responses)

				assert.Equal(t, []logproto.Stream{
					*mockStream(1, 1),
				}, actual)
			},
		},
		"tail logs both from historic entries and tail clients": {
			historicEntries: mockStreamIterator(1, 2),
			tailClient:      newTailClientMock().mockRecvWithTrigger(mockTailResponse(mockStream(3, 1))),
			tester: func(t *testing.T, tailer *Tailer, tailClient *tailClientMock) {
				tailClient.triggerRecv()

				responses, err := readFromTailer(tailer, 3)
				require.NoError(t, err)

				actual := flattenStreamsFromResponses(responses)

				assert.Equal(t, []logproto.Stream{
					*mockStream(1, 1),
					*mockStream(2, 1),
					*mockStream(3, 1),
				}, actual)
			},
		},
		"honor max entries per tail response": {
			historicEntries: mockStreamIterator(1, maxEntriesPerTailResponse+1),
			tailClient:      nil,
			tester: func(t *testing.T, tailer *Tailer, tailClient *tailClientMock) {
				responses, err := readFromTailer(tailer, maxEntriesPerTailResponse+1)
				require.NoError(t, err)

				require.Equal(t, 2, len(responses))
				assert.Equal(t, maxEntriesPerTailResponse, countEntriesInStreams(responses[0].Streams))
				assert.Equal(t, 1, countEntriesInStreams(responses[1].Streams))
				assert.Equal(t, 0, len(responses[1].DroppedEntries))
			},
		},
		"honor max buffered tail responses": {
			historicEntries: mockStreamIterator(1, (maxEntriesPerTailResponse*maxBufferedTailResponses)+5),
			tailClient:      newTailClientMock().mockRecvWithTrigger(mockTailResponse(mockStream(1, 1))),
			tester: func(t *testing.T, tailer *Tailer, tailClient *tailClientMock) {
				err := waitUntilTailerOpenStreamsHaveBeenConsumed(tailer)
				require.NoError(t, err)

				// Since the response channel is full/blocked, we do expect that all responses
				// are "full" and extra entries from historic entries have been dropped
				responses, err := readFromTailer(tailer, (maxEntriesPerTailResponse * maxBufferedTailResponses))
				require.NoError(t, err)

				require.Equal(t, maxBufferedTailResponses, len(responses))
				for i := 0; i < maxBufferedTailResponses; i++ {
					assert.Equal(t, maxEntriesPerTailResponse, countEntriesInStreams(responses[i].Streams))
					assert.Equal(t, 0, len(responses[1].DroppedEntries))
				}

				// Since we'll not receive dropped entries until the next tail response, we're now
				// going to trigger a Recv() from the tail client
				tailClient.triggerRecv()

				responses, err = readFromTailer(tailer, 1)
				require.NoError(t, err)

				require.Equal(t, 1, len(responses))
				assert.Equal(t, 1, countEntriesInStreams(responses[0].Streams))
				assert.Equal(t, 5, len(responses[0].DroppedEntries))
			},
		},
		"honor max dropped entries per tail response": {
			historicEntries: mockStreamIterator(1, (maxEntriesPerTailResponse*maxBufferedTailResponses)+maxDroppedEntriesPerTailResponse+5),
			tailClient:      newTailClientMock().mockRecvWithTrigger(mockTailResponse(mockStream(1, 1))),
			tester: func(t *testing.T, tailer *Tailer, tailClient *tailClientMock) {
				err := waitUntilTailerOpenStreamsHaveBeenConsumed(tailer)
				require.NoError(t, err)

				// Since the response channel is full/blocked, we do expect that all responses
				// are "full" and extra entries from historic entries have been dropped
				responses, err := readFromTailer(tailer, (maxEntriesPerTailResponse * maxBufferedTailResponses))
				require.NoError(t, err)

				require.Equal(t, maxBufferedTailResponses, len(responses))
				for i := 0; i < maxBufferedTailResponses; i++ {
					assert.Equal(t, maxEntriesPerTailResponse, countEntriesInStreams(responses[i].Streams))
					assert.Equal(t, 0, len(responses[1].DroppedEntries))
				}

				// Since we'll not receive dropped entries until the next tail response, we're now
				// going to trigger a Recv() from the tail client
				tailClient.triggerRecv()

				responses, err = readFromTailer(tailer, 1)
				require.NoError(t, err)

				require.Equal(t, 1, len(responses))
				assert.Equal(t, 1, countEntriesInStreams(responses[0].Streams))
				assert.Equal(t, maxDroppedEntriesPerTailResponse, len(responses[0].DroppedEntries))
			},
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			tailDisconnectedIngesters := func([]string) (map[string]logproto.Querier_TailClient, error) {
				return map[string]logproto.Querier_TailClient{}, nil
			}

			tailClients := map[string]logproto.Querier_TailClient{}
			if test.tailClient != nil {
				tailClients["test"] = test.tailClient
			}

			tailer := newTailer(0, tailClients, test.historicEntries, tailDisconnectedIngesters, timeout, throttle)
			defer tailer.close()

			test.tester(t, tailer, test.tailClient)
		})
	}
}

func readFromTailer(tailer *Tailer, maxEntries int) ([]*loghttp.TailResponse, error) {
	responses := make([]*loghttp.TailResponse, 0)
	entriesCount := 0

	// Ensure we do not wait indefinitely
	timeoutTicker := time.NewTicker(timeout)
	defer timeoutTicker.Stop()

	for !tailer.stopped && entriesCount < maxEntries {
		select {
		case <-timeoutTicker.C:
			return nil, errors.New("timeout expired while reading responses from Tailer")
		case response := <-tailer.getResponseChan():
			responses = append(responses, response)
			entriesCount += countEntriesInStreams(response.Streams)
		default:
			time.Sleep(throttle)
		}
	}

	return responses, nil
}

func waitUntilTailerOpenStreamsHaveBeenConsumed(tailer *Tailer) error {
	// Ensure we do not wait indefinitely
	timeoutTicker := time.NewTicker(timeout)
	defer timeoutTicker.Stop()

	for {
		if isTailerOpenStreamsConsumed(tailer) {
			return nil
		}

		select {
		case <-timeoutTicker.C:
			return errors.New("timeout expired while reading responses from Tailer")
		default:
			time.Sleep(throttle)
		}
	}
}

// isTailerOpenStreamsConsumed returns whether the input Tailer has fully
// consumed all streams from the openStreamIterator, which means the
// Tailer.loop() is now throttling
func isTailerOpenStreamsConsumed(tailer *Tailer) bool {
	tailer.streamMtx.Lock()
	defer tailer.streamMtx.Unlock()

	return tailer.openStreamIterator.Len() == 0 || tailer.openStreamIterator.Peek() == time.Unix(0, 0)
}

func countEntriesInStreams(streams []logproto.Stream) int {
	count := 0

	for _, stream := range streams {
		count += len(stream.Entries)
	}

	return count
}

// flattenStreamsFromResponses returns an array of streams each one containing
// one and only one entry from the input list of responses. This function is used
// to abstract away implementation details in the Tailer when testing for the output
// regardless how the responses have been generated (ie. multiple entries grouped
// into the same stream)
func flattenStreamsFromResponses(responses []*loghttp.TailResponse) []logproto.Stream {
	result := make([]logproto.Stream, 0)

	for _, response := range responses {
		for _, stream := range response.Streams {
			for _, entry := range stream.Entries {
				result = append(result, logproto.Stream{
					Entries: []logproto.Entry{entry},
					Labels:  stream.Labels,
				})
			}
		}
	}

	return result
}
