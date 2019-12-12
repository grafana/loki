package ingester

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util/validation"
)

var defaultFactory = func() chunkenc.Chunk {
	return chunkenc.NewMemChunkSize(chunkenc.EncGZIP, 512, 0)
}

func TestLabelsCollisions(t *testing.T) {
	o, err := validation.NewOverrides(validation.Limits{MaxStreamsPerUser: 1000})
	require.NoError(t, err)

	i := newInstance("test", defaultFactory, o)

	// avoid entries from the future.
	tt := time.Now().Add(-5 * time.Minute)

	// Notice how labels aren't sorted.
	err = i.Push(context.Background(), &logproto.PushRequest{Streams: []*logproto.Stream{
		// both label sets have FastFingerprint=e002a3a451262627
		{Labels: "{app=\"l\",uniq0=\"0\",uniq1=\"1\"}", Entries: entries(5, tt.Add(time.Minute))},
		{Labels: "{uniq0=\"1\",app=\"m\",uniq1=\"1\"}", Entries: entries(5, tt)},

		// e002a3a451262247
		{Labels: "{app=\"l\",uniq0=\"1\",uniq1=\"0\"}", Entries: entries(5, tt.Add(time.Minute))},
		{Labels: "{uniq1=\"0\",app=\"m\",uniq0=\"0\"}", Entries: entries(5, tt)},

		// e002a2a4512624f4
		{Labels: "{app=\"l\",uniq0=\"0\",uniq1=\"0\"}", Entries: entries(5, tt.Add(time.Minute))},
		{Labels: "{uniq0=\"1\",uniq1=\"0\",app=\"m\"}", Entries: entries(5, tt)},
	}})
	require.NoError(t, err)
}

func TestConcurrentPushes(t *testing.T) {
	o, err := validation.NewOverrides(validation.Limits{MaxStreamsPerUser: 1000})
	require.NoError(t, err)

	inst := newInstance("test", defaultFactory, o)

	const (
		concurrent          = 10
		iterations          = 100
		entriesPerIteration = 100
	)

	uniqueLabels := map[string]bool{}
	startChannel := make(chan struct{})

	wg := sync.WaitGroup{}
	for i := 0; i < concurrent; i++ {
		l := makeRandomLabels()
		for uniqueLabels[l] {
			l = makeRandomLabels()
		}
		uniqueLabels[l] = true

		wg.Add(1)
		go func(labels string) {
			defer wg.Done()

			<-startChannel

			tt := time.Now().Add(-5 * time.Minute)

			for i := 0; i < iterations; i++ {
				err := inst.Push(context.Background(), &logproto.PushRequest{Streams: []*logproto.Stream{
					{Labels: labels, Entries: entries(entriesPerIteration, tt)},
				}})

				require.NoError(t, err)

				tt = tt.Add(entriesPerIteration * time.Nanosecond)
			}
		}(l)
	}

	time.Sleep(100 * time.Millisecond) // ready
	close(startChannel)                // go!

	wg.Wait()
	// test passes if no goroutine reports error
}

func entries(n int, t time.Time) []logproto.Entry {
	var result []logproto.Entry
	for i := 0; i < n; i++ {
		result = append(result, logproto.Entry{Timestamp: t, Line: fmt.Sprintf("hello %d", i)})
		t = t.Add(time.Nanosecond)
	}
	return result
}

var labelNames = []string{"app", "instance", "namespace", "user", "cluster"}

func makeRandomLabels() string {
	ls := labels.NewBuilder(nil)
	for _, ln := range labelNames {
		ls.Set(ln, fmt.Sprintf("%d", rand.Int31()))
	}
	return ls.Labels().String()
}
