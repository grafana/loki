package ingester

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func TestTailer_sendRaceConditionOnSendWhileClosing(t *testing.T) {
	runs := 100

	stream := logproto.Stream{
		Labels: `{type="test"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(int64(1), 0), Line: "line 1"},
			{Timestamp: time.Unix(int64(2), 0), Line: "line 2"},
		},
	}

	for run := 0; run < runs; run++ {
		tailer, err := newTailer("org-id", stream.Labels, nil, 10)
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
			tail, err := newTailer("foo", `{app="foo"} |= "foo"`, &fakeTailServer{}, maxDroppedStreams)
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

type fakeTailServer struct{}

func (f *fakeTailServer) Send(*logproto.TailResponse) error { return nil }
func (f *fakeTailServer) Context() context.Context          { return context.Background() }

func Test_TailerSendRace(t *testing.T) {
	tail, err := newTailer("foo", `{app="foo"} |= "foo"`, &fakeTailServer{}, 10)
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
