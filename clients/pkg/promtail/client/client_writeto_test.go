package client

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestClientWriter_LogEntriesAreReconstructedAndForwardedCorrectly(t *testing.T) {
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	ch := make(chan api.Entry)
	defer close(ch)

	var mu sync.Mutex
	var receivedEntries []api.Entry

	go func() {
		for e := range ch {
			mu.Lock()
			receivedEntries = append(receivedEntries, e)
			mu.Unlock()
		}
	}()

	var lines = []string{
		"some entry",
		"some other entry",
		"this is a song",
		"about entries",
		"I'm in a starbucks",
	}

	writeTo := newClientWriteTo(ch, logger)
	testAppLabelsRef := chunks.HeadSeriesRef(1)
	writeTo.StoreSeries([]record.RefSeries{
		{
			Ref: testAppLabelsRef,
			Labels: []labels.Label{
				{
					Name:  "app",
					Value: "test",
				},
			},
		},
	}, 1)

	for _, line := range lines {
		_ = writeTo.AppendEntries(wal.RefEntries{
			Ref: testAppLabelsRef,
			Entries: []logproto.Entry{
				{
					Timestamp: time.Now(),
					Line:      line,
				},
			},
		})
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(receivedEntries) == len(lines)
	}, time.Second*10, time.Second)
	mu.Lock()
	for _, receivedEntry := range receivedEntries {
		require.Contains(t, lines, receivedEntry.Line, "entry line was not expected")
		require.Equal(t, model.LabelValue("test"), receivedEntry.Labels["app"])
	}
	mu.Unlock()
}

func TestClientWriter_LogEntriesWithoutMatchingSeriesAreIgnored(t *testing.T) {
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	ch := make(chan api.Entry)
	defer close(ch)

	var receivedEntries []api.Entry

	go func() {
		for e := range ch {
			receivedEntries = append(receivedEntries, e)
		}
	}()

	var lines = []string{
		"some entry",
		"some other entry",
		"this is a song",
		"about entries",
		"I'm in a starbucks",
	}

	writeTo := newClientWriteTo(ch, logger)
	testAppLabelsRef := chunks.HeadSeriesRef(1)
	writeTo.StoreSeries([]record.RefSeries{
		{
			Ref: testAppLabelsRef,
			Labels: []labels.Label{
				{
					Name:  "app",
					Value: "test",
				},
			},
		},
	}, 1)

	for _, line := range lines {
		_ = writeTo.AppendEntries(wal.RefEntries{
			Ref: chunks.HeadSeriesRef(61324),
			Entries: []logproto.Entry{
				{
					Timestamp: time.Now(),
					Line:      line,
				},
			},
		})
	}

	time.Sleep(time.Second * 2)
	require.Empty(t, receivedEntries, "no entry should have arrived")
}

func BenchmarkClientWriteTo(b *testing.B) {
	type testCase struct {
		numWriters int
		totalLines int
	}
	for _, tc := range []testCase{
		{numWriters: 5, totalLines: 1_000},
		{numWriters: 10, totalLines: 1_000},
		{numWriters: 5, totalLines: 10_000},
		{numWriters: 10, totalLines: 10_000},
		{numWriters: 5, totalLines: 100_000},
		{numWriters: 10, totalLines: 100_000},
		{numWriters: 10, totalLines: 1_000_000},
	} {
		b.Run(fmt.Sprintf("num writers %d, with %d lines written in total", tc.numWriters, tc.totalLines),
			func(b *testing.B) {
				require.True(b, tc.totalLines%tc.numWriters == 0, "total lines must be divisible by num of writers")
				for n := 0; n < b.N; n++ {
					bench(tc.numWriters, tc.totalLines, b)
				}
			})
	}
}

// bench is a single execution of the ClientWriteTo benchmark. During the execution of the benchmark, numWriters routines
// will run, writing totalLines in total. After the writers have finished, the execution will block until the number of
// expected written entries is reached, or it times out.
func bench(numWriters, totalLines int, b *testing.B) {
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	readerWG := sync.WaitGroup{}
	ch := make(chan api.Entry)

	b.Log("starting reader routine")
	readerWG.Add(1)
	var totalReceived atomic.Int64
	go func() {
		defer readerWG.Done()
		// discard received entries
		for range ch {
			totalReceived.Inc()
		}
	}()

	writeTo := newClientWriteTo(ch, logger)

	// spin up the numWriters routines
	writersWG := sync.WaitGroup{}
	for n := 0; n < numWriters; n++ {
		writersWG.Add(1)
		go func(n int) {
			defer writersWG.Done()
			b.Logf("starting writer routine %d", n)
			// run as writing from segment n+w, and after finishing reset series up to n. That way we are only causing blocking,
			// and not deleting actually used series
			startWriter(numWriters+n, n, writeTo, totalLines/numWriters, record.RefSeries{
				Ref: chunks.HeadSeriesRef(n),
				Labels: labels.Labels{
					{Name: "n", Value: fmt.Sprint(n)},
				},
			}, time.Millisecond*500)
		}(n)
	}

	b.Log("waiting for writers to finish")
	writersWG.Wait()

	b.Log("waiting for reader to finish")
	require.Eventually(b, func() bool {
		var read = totalReceived.Load()
		b.Logf("checking, value read: %d", read)
		return read == int64(totalLines)
	}, time.Second*10, time.Second)
	close(ch)
	readerWG.Wait()
	require.Equal(b, int64(totalLines), totalReceived.Load(), "some lines where not read")
}

// startWriter orchestrates each writer routine that bench launches. Each routine will execute the following actions:
// 1. Add some jitter to the whole routine execution by waiting between 0 and maxInitialSleep.
// 2. Store the series that all written entries will use.
// 3. Write the lines entries, adding a small jitter between 0 and 1 ms after each write operation.
// 4. After all are written, call a SeriesReset. This will block the entire series map and will hopefully block
// some other writing routine.
func startWriter(segmentNum, seriesToReset int, target *clientWriteTo, lines int, series record.RefSeries, maxInitialSleep time.Duration) {
	randomSleepMax := func(maxVal time.Duration) {
		// random sleep to add some jitter
		s := int64(rand.Uint64()) % int64(maxVal)
		time.Sleep(time.Duration(s))
	}
	// random sleep to add some jitter
	randomSleepMax(maxInitialSleep)

	// store series
	target.StoreSeries([]record.RefSeries{series}, segmentNum)

	// write lines with that series
	for i := 0; i < lines; i++ {
		_ = target.AppendEntries(wal.RefEntries{
			Ref: series.Ref,
			Entries: []logproto.Entry{
				{
					Timestamp: time.Now(),
					Line:      fmt.Sprintf("%d - %d - hellooo", segmentNum, i),
				},
			},
		})
		// add some jitter between writes
		randomSleepMax(time.Millisecond * 1)
	}

	// reset segment
	target.SeriesReset(seriesToReset)
}
