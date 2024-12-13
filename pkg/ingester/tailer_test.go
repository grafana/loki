package ingester

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestTailer_RoundTrip(t *testing.T) {
	t.Parallel()
	server := &fakeTailServer{}

	lbs := makeRandomLabels()
	expr, err := syntax.ParseLogSelector(lbs.String(), true)
	require.NoError(t, err)
	tail, err := newTailer("org-id", expr, server, 10)
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		tail.loop()
		wg.Done()
	}()

	const numStreams = 1000
	var entries []logproto.Entry
	for i := 0; i < numStreams; i += 3 {
		var iterEntries []logproto.Entry
		for j := 0; j < 3; j++ {
			iterEntries = append(iterEntries, logproto.Entry{Timestamp: time.Unix(0, int64(i+j)), Line: fmt.Sprintf("line %d", i+j)})
		}
		entries = append(entries, iterEntries...)

		tail.send(logproto.Stream{
			Labels:  lbs.String(),
			Entries: iterEntries,
		}, lbs)

		// sleep a bit to allow the tailer to process the stream without dropping
		// This should take about 5 seconds to process all the streams
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for the stream to be received by the server.
	require.Eventually(t, func() bool {
		return len(server.GetResponses()) > 0
	}, 30*time.Second, 1*time.Second, "stream was not received")

	var processedEntries []logproto.Entry
	for _, response := range server.GetResponses() {
		processedEntries = append(processedEntries, response.Stream.Entries...)
	}
	require.ElementsMatch(t, entries, processedEntries)

	tail.close()
	wg.Wait()
}

func TestTailer_sendRaceConditionOnSendWhileClosing(t *testing.T) {
	t.Parallel()
	runs := 100

	stream := logproto.Stream{
		Labels: `{type="test"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(int64(1), 0), Line: "line 1"},
			{Timestamp: time.Unix(int64(2), 0), Line: "line 2"},
		},
	}

	for run := 0; run < runs; run++ {
		expr, err := syntax.ParseLogSelector(stream.Labels, true)
		require.NoError(t, err)
		tailer, err := newTailer("org-id", expr, nil, 10)
		require.NoError(t, err)
		require.NotNil(t, tailer)

		routines := sync.WaitGroup{}
		routines.Add(2)

		go assert.NotPanics(t, func() {
			defer routines.Done()
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Microsecond)
			tailer.send(stream, labels.Labels{{Name: "type", Value: "test"}})
		})

		go assert.NotPanics(t, func() {
			defer routines.Done()
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Microsecond)
			tailer.close()
		})

		routines.Wait()
	}
}

func Test_dropstream(t *testing.T) {
	t.Parallel()
	maxDroppedStreams := 10

	entry := logproto.Entry{Timestamp: time.Now(), Line: "foo"}

	cases := []struct {
		name     string
		drop     int
		expected int
	}{
		{
			name:     "less than maxDroppedStreams",
			drop:     maxDroppedStreams - 2,
			expected: maxDroppedStreams - 2,
		},
		{
			name:     "equal to maxDroppedStreams",
			drop:     maxDroppedStreams,
			expected: maxDroppedStreams,
		},
		{
			name:     "greater than maxDroppedStreams",
			drop:     maxDroppedStreams + 2,
			expected: 2, // should be bounded to maxDroppedStreams
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := syntax.ParseLogSelector(`{app="foo"} |= "foo"`, true)
			require.NoError(t, err)
			tail, err := newTailer("foo", expr, &fakeTailServer{}, maxDroppedStreams)
			require.NoError(t, err)

			for i := 0; i < c.drop; i++ {
				tail.dropStream(logproto.Stream{
					Entries: []logproto.Entry{
						entry,
					},
				})
			}
			assert.Equal(t, c.expected, len(tail.droppedStreams))
		})
	}
}

type fakeTailServer struct {
	responses   []logproto.TailResponse
	responsesMu sync.Mutex
}

func (f *fakeTailServer) Send(response *logproto.TailResponse) error {
	f.responsesMu.Lock()
	defer f.responsesMu.Unlock()
	f.responses = append(f.responses, *response)
	return nil

}

func (f *fakeTailServer) Context() context.Context { return context.Background() }

func cloneTailResponse(response logproto.TailResponse) logproto.TailResponse {
	var clone logproto.TailResponse
	if response.Stream != nil {
		clone.Stream = &logproto.Stream{}
		clone.Stream.Labels = response.Stream.Labels
		clone.Stream.Hash = response.Stream.Hash
		if response.Stream.Entries != nil {
			clone.Stream.Entries = make([]logproto.Entry, len(response.Stream.Entries))
			copy(clone.Stream.Entries, response.Stream.Entries)
		}
	}
	if response.DroppedStreams != nil {
		clone.DroppedStreams = make([]*logproto.DroppedStream, len(response.DroppedStreams))
		copy(clone.DroppedStreams, response.DroppedStreams)
	}

	return clone
}

func (f *fakeTailServer) GetResponses() []logproto.TailResponse {
	f.responsesMu.Lock()
	defer f.responsesMu.Unlock()
	clonedResponses := make([]logproto.TailResponse, len(f.responses))
	for i, resp := range f.responses {
		clonedResponses[i] = cloneTailResponse(resp)
	}
	return f.responses
}

func (f *fakeTailServer) Reset() {
	f.responsesMu.Lock()
	defer f.responsesMu.Unlock()
	f.responses = f.responses[:0]
}

func Test_TailerSendRace(t *testing.T) {
	expr, err := syntax.ParseLogSelector(`{app="foo"} |= "foo"`, true)
	require.NoError(t, err)
	tail, err := newTailer("foo", expr, &fakeTailServer{}, 10)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func() {
			lbs := makeRandomLabels()
			tail.send(logproto.Stream{
				Labels: lbs.String(),
				Entries: []logproto.Entry{
					{Timestamp: time.Unix(0, 1), Line: "1"},
					{Timestamp: time.Unix(0, 2), Line: "2"},
					{Timestamp: time.Unix(0, 3), Line: "3"},
				},
			}, lbs)
			wg.Done()
		}()
	}
	wg.Wait()
}

func Test_IsMatching(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		lbs      labels.Labels
		matchers []*labels.Matcher
		matches  bool
	}{
		{"not in lbs", labels.Labels{{Name: "job", Value: "foo"}}, []*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}, false},
		{"equal", labels.Labels{{Name: "job", Value: "foo"}}, []*labels.Matcher{{Type: labels.MatchEqual, Name: "job", Value: "foo"}}, true},
		{"regex", labels.Labels{{Name: "job", Value: "foo"}}, []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "job", ".+oo")}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.matches, isMatching(tt.lbs, tt.matchers))
		})
	}
}

func Test_StructuredMetadata(t *testing.T) {
	t.Parallel()
	lbs := makeRandomLabels()

	for _, tc := range []struct {
		name              string
		query             string
		sentStream        logproto.Stream
		expectedResponses []logproto.TailResponse
	}{
		{
			// Optimization will make the same stream to be returned regardless of structured metadata.
			name:  "noop pipeline",
			query: `{app="foo"}`,
			sentStream: logproto.Stream{
				Labels: lbs.String(),
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(0, 1),
						Line:      "foo=1",
					},
					{
						Timestamp:          time.Unix(0, 2),
						Line:               "foo=2",
						StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123")),
					},
				},
			},
			expectedResponses: []logproto.TailResponse{
				{
					Stream: &logproto.Stream{
						Labels: lbs.String(),
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(0, 1),
								Line:      "foo=1",
							},
							{
								Timestamp:          time.Unix(0, 2),
								Line:               "foo=2",
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123")),
							},
						},
					},
					DroppedStreams: nil,
				},
			},
		},
		{
			name:  "parse pipeline labels",
			query: `{app="foo"} | logfmt`,
			sentStream: logproto.Stream{
				Labels: lbs.String(),
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(0, 1),
						Line:      "foo=1",
					},
					{
						Timestamp:          time.Unix(0, 2),
						Line:               "foo=2",
						StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123")),
					},
				},
			},
			expectedResponses: []logproto.TailResponse{
				{
					Stream: &logproto.Stream{
						Labels: labels.NewBuilder(lbs).Set("foo", "1").Labels().String(),
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(0, 1),
								Line:      "foo=1",
								Parsed:    logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "1")),
							},
						},
					},
					DroppedStreams: nil,
				},
				{
					Stream: &logproto.Stream{
						Labels: labels.NewBuilder(lbs).Set("traceID", "123").Set("foo", "2").Labels().String(),
						Entries: []logproto.Entry{
							{
								Timestamp:          time.Unix(0, 2),
								Line:               "foo=2",
								StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123")),
								Parsed:             logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "2")),
							},
						},
					},
					DroppedStreams: nil,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var server fakeTailServer
			expr, err := syntax.ParseLogSelector(tc.query, true)
			require.NoError(t, err)
			tail, err := newTailer("foo", expr, &server, 10)
			require.NoError(t, err)

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				tail.loop()
				wg.Done()
			}()

			tail.send(tc.sentStream, lbs)

			// Wait for the stream to be received by the server.
			require.Eventually(t, func() bool {
				return len(server.GetResponses()) > 0
			}, 30*time.Second, 1*time.Second, "stream was not received")

			responses := server.GetResponses()
			require.ElementsMatch(t, tc.expectedResponses, responses)

			tail.close()
			wg.Wait()
		})
	}
}

func Benchmark_isClosed(t *testing.B) {
	var server fakeTailServer
	expr, err := syntax.ParseLogSelector(`{app="foo"}`, true)
	require.NoError(t, err)
	tail, err := newTailer("foo", expr, &server, 0)
	require.NoError(t, err)

	require.Equal(t, false, tail.isClosed())

	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		tail.isClosed()
	}

	tail.close()
	require.Equal(t, true, tail.isClosed())
}
