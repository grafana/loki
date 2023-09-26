// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package wal

import (
	"context"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"
)

func newTestStorage(walDir string) (*Storage, error) {
	metrics := NewMetrics(prometheus.DefaultRegisterer)
	return NewStorage(log.NewNopLogger(), metrics, nil, walDir)
}

func TestStorage_InvalidSeries(t *testing.T) {
	walDir := t.TempDir()

	s, err := newTestStorage(walDir)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, s.Close())
	}()

	app := s.Appender(context.Background())

	// Samples
	_, err = app.Append(0, labels.Labels{}, 0, 0)
	require.Error(t, err, "should reject empty labels")

	_, err = app.Append(0, labels.Labels{{Name: "a", Value: "1"}, {Name: "a", Value: "2"}}, 0, 0)
	require.Error(t, err, "should reject duplicate labels")

	// Sanity check: valid series
	sRef, err := app.Append(0, labels.Labels{{Name: "a", Value: "1"}}, 0, 0)
	require.NoError(t, err, "should not reject valid series")

	// Exemplars
	_, err = app.AppendExemplar(0, nil, exemplar.Exemplar{})
	require.Error(t, err, "should reject unknown series ref")

	e := exemplar.Exemplar{Labels: labels.Labels{{Name: "a", Value: "1"}, {Name: "a", Value: "2"}}}
	_, err = app.AppendExemplar(sRef, nil, e)
	require.ErrorIs(t, err, tsdb.ErrInvalidExemplar, "should reject duplicate labels")

	e = exemplar.Exemplar{Labels: labels.Labels{{Name: "a_somewhat_long_trace_id", Value: "nYJSNtFrFTY37VR7mHzEE/LIDt7cdAQcuOzFajgmLDAdBSRHYPDzrxhMA4zz7el8naI/AoXFv9/e/G0vcETcIoNUi3OieeLfaIRQci2oa"}}}
	_, err = app.AppendExemplar(sRef, nil, e)
	require.ErrorIs(t, err, storage.ErrExemplarLabelLength, "should reject too long label length")

	// Sanity check: valid exemplars
	e = exemplar.Exemplar{Labels: labels.Labels{{Name: "a", Value: "1"}}, Value: 20, Ts: 10, HasTs: true}
	_, err = app.AppendExemplar(sRef, nil, e)
	require.NoError(t, err, "should not reject valid exemplars")
}

func TestStorage(t *testing.T) {
	walDir := t.TempDir()

	s, err := newTestStorage(walDir)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, s.Close())
	}()

	app := s.Appender(context.Background())

	// Write some samples
	payload := buildSeries([]string{"foo", "bar", "baz"})
	for _, metric := range payload {
		metric.Write(t, app)
	}

	require.NoError(t, app.Commit())

	collector := walDataCollector{}
	replayer := walReplayer{w: &collector}
	require.NoError(t, replayer.Replay(s.wal.Dir()))

	names := []string{}
	for _, series := range collector.series {
		names = append(names, series.Labels.Get("__name__"))
	}
	require.Equal(t, payload.SeriesNames(), names)

	expectedSamples := payload.ExpectedSamples()
	actualSamples := collector.samples
	sort.Sort(byRefSample(actualSamples))
	require.Equal(t, expectedSamples, actualSamples)

	expectedExemplars := payload.ExpectedExemplars()
	actualExemplars := collector.exemplars
	sort.Sort(byRefExemplar(actualExemplars))
	require.Equal(t, expectedExemplars, actualExemplars)
}

func TestStorage_ExistingWAL(t *testing.T) {
	walDir := t.TempDir()

	s, err := newTestStorage(walDir)
	require.NoError(t, err)

	app := s.Appender(context.Background())
	payload := buildSeries([]string{"foo", "bar", "baz", "blerg"})

	// Write half of the samples.
	for _, metric := range payload[0 : len(payload)/2] {
		metric.Write(t, app)
	}

	require.NoError(t, app.Commit())
	require.NoError(t, s.Close())

	// We need to wait a little bit for the previous store to finish
	// flushing.
	time.Sleep(time.Millisecond * 150)

	// Create a new storage, write the other half of samples.
	s, err = newTestStorage(walDir)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, s.Close())
	}()

	// Verify that the storage picked up existing series when it
	// replayed the WAL.
	for series := range s.series.iterator().Channel() {
		require.Greater(t, series.lastTs, int64(0), "series timestamp not updated")
	}

	app = s.Appender(context.Background())

	for _, metric := range payload[len(payload)/2:] {
		metric.Write(t, app)
	}

	require.NoError(t, app.Commit())

	collector := walDataCollector{}
	replayer := walReplayer{w: &collector}
	require.NoError(t, replayer.Replay(s.wal.Dir()))

	names := []string{}
	for _, series := range collector.series {
		names = append(names, series.Labels.Get("__name__"))
	}
	require.Equal(t, payload.SeriesNames(), names)

	expectedSamples := payload.ExpectedSamples()
	actualSamples := collector.samples
	sort.Sort(byRefSample(actualSamples))
	require.Equal(t, expectedSamples, actualSamples)

	expectedExemplars := payload.ExpectedExemplars()
	actualExemplars := collector.exemplars
	sort.Sort(byRefExemplar(actualExemplars))
	require.Equal(t, expectedExemplars, actualExemplars)
}

func TestStorage_ExistingWAL_RefID(t *testing.T) {
	walDir := t.TempDir()

	s, err := newTestStorage(walDir)
	require.NoError(t, err)

	app := s.Appender(context.Background())
	payload := buildSeries([]string{"foo", "bar", "baz", "blerg"})

	// Write all the samples
	for _, metric := range payload {
		metric.Write(t, app)
	}
	require.NoError(t, app.Commit())

	// Truncate the WAL to force creation of a new segment.
	require.NoError(t, s.Truncate(0))
	require.NoError(t, s.Close())

	// Create a new storage and see what the ref ID is initialized to.
	s, err = newTestStorage(walDir)
	require.NoError(t, err)
	defer require.NoError(t, s.Close())

	require.Equal(t, uint64(len(payload)), s.ref.Load(), "cached ref ID should be equal to the number of series written")
}

func TestStorage_Truncate(t *testing.T) {
	// Same as before but now do the following:
	// after writing all the data, forcefully create 4 more segments,
	// then do a truncate of a timestamp for _some_ of the data.
	// then read data back in. Expect to only get the latter half of data.
	walDir := t.TempDir()

	s, err := newTestStorage(walDir)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, s.Close())
	}()

	app := s.Appender(context.Background())

	payload := buildSeries([]string{"foo", "bar", "baz", "blerg"})

	for _, metric := range payload {
		metric.Write(t, app)
	}

	require.NoError(t, app.Commit())

	// Forefully create a bunch of new segments so when we truncate
	// there's enough segments to be considered for truncation.
	for i := 0; i < 5; i++ {
		_, err := s.wal.NextSegmentSync()
		require.NoError(t, err)
	}

	// Truncate half of the samples, keeping only the second sample
	// per series.
	keepTs := payload[len(payload)-1].samples[0].ts + 1
	err = s.Truncate(keepTs)
	require.NoError(t, err)

	payload = payload.Filter(func(s sample) bool {
		return s.ts >= keepTs
	}, func(e exemplar.Exemplar) bool {
		return e.HasTs && e.Ts >= keepTs
	})
	expectedSamples := payload.ExpectedSamples()
	expectedExemplars := payload.ExpectedExemplars()

	// Read back the WAL, collect series and samples.
	collector := walDataCollector{}
	replayer := walReplayer{w: &collector}
	require.NoError(t, replayer.Replay(s.wal.Dir()))

	names := []string{}
	for _, series := range collector.series {
		names = append(names, series.Labels.Get("__name__"))
	}
	require.Equal(t, payload.SeriesNames(), names)

	actualSamples := collector.samples
	sort.Sort(byRefSample(actualSamples))
	require.Equal(t, expectedSamples, actualSamples)

	actualExemplars := collector.exemplars
	sort.Sort(byRefExemplar(actualExemplars))
	require.Equal(t, expectedExemplars, actualExemplars)
}

func TestStorage_WriteStalenessMarkers(t *testing.T) {
	walDir := t.TempDir()

	s, err := newTestStorage(walDir)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, s.Close())
	}()

	app := s.Appender(context.Background())

	// Write some samples
	payload := seriesList{
		{name: "foo", samples: []sample{{1, 10.0}, {10, 100.0}}},
		{name: "bar", samples: []sample{{2, 20.0}, {20, 200.0}}},
		{name: "baz", samples: []sample{{3, 30.0}, {30, 300.0}}},
	}
	for _, metric := range payload {
		metric.Write(t, app)
	}

	require.NoError(t, app.Commit())

	// Write staleness markers for every series
	require.NoError(t, s.WriteStalenessMarkers(func() int64 {
		// Pass math.MaxInt64 so it seems like everything was written already
		return math.MaxInt64
	}))

	// Read back the WAL, collect series and samples.
	collector := walDataCollector{}
	replayer := walReplayer{w: &collector}
	require.NoError(t, replayer.Replay(s.wal.Dir()))

	actual := collector.samples
	sort.Sort(byRefSample(actual))

	staleMap := map[chunks.HeadSeriesRef]bool{}
	for _, sample := range actual {
		if _, ok := staleMap[sample.Ref]; !ok {
			staleMap[sample.Ref] = false
		}
		if value.IsStaleNaN(sample.V) {
			staleMap[sample.Ref] = true
		}
	}

	for ref, v := range staleMap {
		require.True(t, v, "ref %d doesn't have stale marker", ref)
	}
}

func TestStorage_TruncateAfterClose(t *testing.T) {
	walDir := t.TempDir()

	s, err := newTestStorage(walDir)
	require.NoError(t, err)

	require.NoError(t, s.Close())
	require.Error(t, ErrWALClosed, s.Truncate(0))
}

type sample struct {
	ts  int64
	val float64
}

type series struct {
	name      string
	samples   []sample
	exemplars []exemplar.Exemplar

	ref *storage.SeriesRef
}

func (s *series) Write(t *testing.T, app storage.Appender) {
	t.Helper()

	lbls := labels.FromMap(map[string]string{"__name__": s.name})

	offset := 0
	if s.ref == nil {
		// Write first sample to get ref ID
		ref, err := app.Append(0, lbls, s.samples[0].ts, s.samples[0].val)
		require.NoError(t, err)

		s.ref = &ref
		offset = 1
	}

	// Write other data points with AddFast
	for _, sample := range s.samples[offset:] {
		_, err := app.Append(*s.ref, lbls, sample.ts, sample.val)
		require.NoError(t, err)
	}

	sRef := *s.ref
	for _, exemplar := range s.exemplars {
		var err error
		sRef, err = app.AppendExemplar(sRef, nil, exemplar)
		require.NoError(t, err)
	}
}

type seriesList []*series

// Filter creates a new seriesList with series filtered by a sample
// keep predicate function.
func (s seriesList) Filter(fn func(s sample) bool, fnExemplar func(e exemplar.Exemplar) bool) seriesList {
	var ret seriesList

	for _, entry := range s {
		var (
			samples   []sample
			exemplars []exemplar.Exemplar
		)

		for _, sample := range entry.samples {
			if fn(sample) {
				samples = append(samples, sample)
			}
		}

		for _, e := range entry.exemplars {
			if fnExemplar(e) {
				exemplars = append(exemplars, e)
			}
		}

		if len(samples) > 0 && len(exemplars) > 0 {
			ret = append(ret, &series{
				name:      entry.name,
				ref:       entry.ref,
				samples:   samples,
				exemplars: exemplars,
			})
		}
	}

	return ret
}

func (s seriesList) SeriesNames() []string {
	names := make([]string, 0, len(s))
	for _, series := range s {
		names = append(names, series.name)
	}
	return names
}

// ExpectedSamples returns the list of expected samples, sorted by ref ID and timestamp
func (s seriesList) ExpectedSamples() []record.RefSample {
	expect := []record.RefSample{}
	for _, series := range s {
		for _, sample := range series.samples {
			expect = append(expect, record.RefSample{
				Ref: chunks.HeadSeriesRef(*series.ref),
				T:   sample.ts,
				V:   sample.val,
			})
		}
	}
	sort.Sort(byRefSample(expect))
	return expect
}

// ExpectedExemplars returns the list of expected exemplars, sorted by ref ID and timestamp
func (s seriesList) ExpectedExemplars() []record.RefExemplar {
	expect := []record.RefExemplar{}
	for _, series := range s {
		for _, exemplar := range series.exemplars {
			expect = append(expect, record.RefExemplar{
				Ref:    chunks.HeadSeriesRef(*series.ref),
				T:      exemplar.Ts,
				V:      exemplar.Value,
				Labels: exemplar.Labels,
			})
		}
	}
	sort.Sort(byRefExemplar(expect))
	return expect
}

func buildSeries(nameSlice []string) seriesList {
	s := make(seriesList, 0, len(nameSlice))
	for i, n := range nameSlice {
		i++
		s = append(s, &series{
			name:    n,
			samples: []sample{{int64(i), float64(i * 10.0)}, {int64(i * 10), float64(i * 100.0)}},
			exemplars: []exemplar.Exemplar{
				{Labels: labels.Labels{{Name: "foobar", Value: "barfoo"}}, Value: float64(i * 10.0), Ts: int64(i), HasTs: true},
				{Labels: labels.Labels{{Name: "lorem", Value: "ipsum"}}, Value: float64(i * 100.0), Ts: int64(i * 10), HasTs: true},
			},
		})
	}
	return s
}

type byRefSample []record.RefSample

func (b byRefSample) Len() int      { return len(b) }
func (b byRefSample) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byRefSample) Less(i, j int) bool {
	if b[i].Ref == b[j].Ref {
		return b[i].T < b[j].T
	}
	return b[i].Ref < b[j].Ref
}

type byRefExemplar []record.RefExemplar

func (b byRefExemplar) Len() int      { return len(b) }
func (b byRefExemplar) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byRefExemplar) Less(i, j int) bool {
	if b[i].Ref == b[j].Ref {
		return b[i].T < b[j].T
	}
	return b[i].Ref < b[j].Ref
}
