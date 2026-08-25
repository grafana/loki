package iter

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

const (
	testSize      = 10
	defaultLabels = "{foo=\"baz\"}"
)

func TestIterator(t *testing.T) {
	for i, tc := range []struct {
		iterator  EntryIterator
		generator generator
		length    int64
		labels    string
	}{
		// Test basic identity.
		{
			iterator:  mkStreamIterator(identity, defaultLabels),
			generator: identity,
			length:    testSize,
			labels:    defaultLabels,
		},

		// Test basic identity (backwards).
		{
			iterator:  mkStreamIterator(inverse(identity), defaultLabels),
			generator: inverse(identity),
			length:    testSize,
			labels:    defaultLabels,
		},

		// Test dedupe of overlapping iterators with the heap iterator.
		{
			iterator: NewMergeEntryIterator(context.Background(), []EntryIterator{
				mkStreamIterator(offset(0, identity), defaultLabels),
				mkStreamIterator(offset(testSize/2, identity), defaultLabels),
				mkStreamIterator(offset(testSize, identity), defaultLabels),
			}, logproto.FORWARD),
			generator: identity,
			length:    2 * testSize,
			labels:    defaultLabels,
		},

		// Test dedupe of overlapping iterators with the heap iterator (backward).
		{
			iterator: NewMergeEntryIterator(context.Background(), []EntryIterator{
				mkStreamIterator(inverse(offset(0, identity)), defaultLabels),
				mkStreamIterator(inverse(offset(-testSize/2, identity)), defaultLabels),
				mkStreamIterator(inverse(offset(-testSize, identity)), defaultLabels),
			}, logproto.BACKWARD),
			generator: inverse(identity),
			length:    2 * testSize,
			labels:    defaultLabels,
		},

		// Test dedupe of entries with the same timestamp but different entries.
		{
			iterator: NewMergeEntryIterator(context.Background(), []EntryIterator{
				mkStreamIterator(offset(0, constant(0)), defaultLabels),
				mkStreamIterator(offset(0, constant(0)), defaultLabels),
				mkStreamIterator(offset(testSize, constant(0)), defaultLabels),
			}, logproto.FORWARD),
			generator: constant(0),
			length:    2 * testSize,
			labels:    defaultLabels,
		},

		// Test basic identity with non-default labels.
		{
			iterator:  mkStreamIterator(identity, "{foobar: \"bazbar\"}"),
			generator: identity,
			length:    testSize,
			labels:    "{foobar: \"bazbar\"}",
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			for i := int64(0); i < tc.length; i++ {
				assert.Equal(t, true, tc.iterator.Next())
				assert.Equal(t, tc.generator(i), tc.iterator.At(), fmt.Sprintln("iteration", i))
				assert.Equal(t, tc.labels, tc.iterator.Labels(), fmt.Sprintln("iteration", i))
			}

			assert.Equal(t, false, tc.iterator.Next())
			assert.Equal(t, nil, tc.iterator.Err())
			assert.NoError(t, tc.iterator.Close())
		})
	}
}

func TestIteratorMultipleLabels(t *testing.T) {
	for i, tc := range []struct {
		iterator  EntryIterator
		generator generator
		length    int64
		labels    func(int64) string
	}{
		// Test merging with differing labels but same timestamps and values.
		{
			iterator: NewMergeEntryIterator(context.Background(), []EntryIterator{
				mkStreamIterator(identity, "{foobar: \"baz1\"}"),
				mkStreamIterator(identity, "{foobar: \"baz2\"}"),
			}, logproto.FORWARD),
			generator: func(i int64) logproto.Entry {
				return identity(i / 2)
			},
			length: testSize * 2,
			labels: func(i int64) string {
				if i%2 == 0 {
					return "{foobar: \"baz2\"}"
				}
				return "{foobar: \"baz1\"}"
			},
		},

		// Test merging with differing labels but all the same timestamps and different values.
		{
			iterator: NewMergeEntryIterator(context.Background(), []EntryIterator{
				mkStreamIterator(constant(0), "{foobar: \"baz1\"}"),
				mkStreamIterator(constant(0), "{foobar: \"baz2\"}"),
			}, logproto.FORWARD),
			generator: func(i int64) logproto.Entry {
				return constant(0)(i % testSize)
			},
			length: testSize * 2,
			labels: func(i int64) string {
				if i/testSize == 0 {
					return "{foobar: \"baz2\"}"
				}
				return "{foobar: \"baz1\"}"
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			for i := int64(0); i < tc.length; i++ {
				assert.Equal(t, true, tc.iterator.Next())
				assert.Equal(t, tc.generator(i), tc.iterator.At(), fmt.Sprintln("iteration", i))
				assert.Equal(t, tc.labels(i), tc.iterator.Labels(), fmt.Sprintln("iteration", i))
			}

			assert.Equal(t, false, tc.iterator.Next())
			assert.Equal(t, nil, tc.iterator.Err())
			assert.NoError(t, tc.iterator.Close())
		})
	}
}

func TestMergeIteratorPrefetch(t *testing.T) {
	t.Parallel()

	type tester func(t *testing.T, i MergeEntryIterator)

	tests := map[string]tester{
		"prefetch on IsEmpty() when called as first method": func(t *testing.T, i MergeEntryIterator) {
			assert.Equal(t, false, i.IsEmpty())
		},
		"prefetch on Peek() when called as first method": func(t *testing.T, i MergeEntryIterator) {
			assert.Equal(t, time.Unix(0, 0), i.Peek())
		},
		"prefetch on Next() when called as first method": func(t *testing.T, i MergeEntryIterator) {
			assert.True(t, i.Next())
			assert.Equal(t, logproto.Entry{Timestamp: time.Unix(0, 0), Line: "0"}, i.At())
		},
	}

	for testName, testFunc := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			i := NewMergeEntryIterator(context.Background(), []EntryIterator{
				mkStreamIterator(identity, "{foobar: \"baz1\"}"),
				mkStreamIterator(identity, "{foobar: \"baz2\"}"),
			}, logproto.FORWARD)

			testFunc(t, i)
		})
	}
}

type generator func(i int64) logproto.Entry

func mkStreamIterator(f generator, labels string) EntryIterator {
	entries := []logproto.Entry{}
	for i := int64(0); i < testSize; i++ {
		entries = append(entries, f(i))
	}
	return NewStreamIterator(logproto.Stream{
		Entries: entries,
		Labels:  labels,
		Hash:    hashLabels(labels),
	})
}

func hashLabels(lbs string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(lbs))
	return h.Sum64()
}

func identity(i int64) logproto.Entry {
	return logproto.Entry{
		Timestamp: time.Unix(i, 0),
		Line:      fmt.Sprintf("%d", i),
	}
}

func offset(j int64, g generator) generator {
	return func(i int64) logproto.Entry {
		return g(i + j)
	}
}

// nolint
func constant(t int64) generator {
	return func(i int64) logproto.Entry {
		return logproto.Entry{
			Timestamp: time.Unix(t, 0),
			Line:      fmt.Sprintf("%d", i),
		}
	}
}

func inverse(g generator) generator {
	return func(i int64) logproto.Entry {
		return g(-i)
	}
}

func TestMergeIteratorDeduplication(t *testing.T) {
	foo := logproto.Stream{
		Labels: `{app="foo"}`,
		Hash:   hashLabels(`{app="foo"}`),
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1), Line: "1"},
			{Timestamp: time.Unix(0, 2), Line: "2"},
			{Timestamp: time.Unix(0, 3), Line: "3"},
		},
	}
	bar := logproto.Stream{
		Labels: `{app="bar"}`,
		Hash:   hashLabels(`{app="bar"}`),
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1), Line: "1"},
			{Timestamp: time.Unix(0, 2), Line: "2"},
			{Timestamp: time.Unix(0, 3), Line: "3"},
		},
	}
	assertIt := func(it EntryIterator, reversed bool, length int) {
		for i := 0; i < length; i++ {
			j := i
			if reversed {
				j = length - 1 - i
			}
			require.True(t, it.Next())
			require.NoError(t, it.Err())
			require.Equal(t, bar.Labels, it.Labels())
			require.Equal(t, bar.Entries[j], it.At())

			require.True(t, it.Next())
			require.NoError(t, it.Err())
			require.Equal(t, foo.Labels, it.Labels())
			require.Equal(t, foo.Entries[j], it.At())

		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	}
	// forward iteration
	it := NewMergeEntryIterator(context.Background(), []EntryIterator{
		NewStreamIterator(foo),
		NewStreamIterator(bar),
		NewStreamIterator(foo),
		NewStreamIterator(bar),
		NewStreamIterator(foo),
		NewStreamIterator(bar),
		NewStreamIterator(foo),
	}, logproto.FORWARD)
	assertIt(it, false, len(foo.Entries))

	// backward iteration
	it = NewMergeEntryIterator(context.Background(), []EntryIterator{
		mustReverseStreamIterator(NewStreamIterator(foo)),
		mustReverseStreamIterator(NewStreamIterator(bar)),
		mustReverseStreamIterator(NewStreamIterator(foo)),
		mustReverseStreamIterator(NewStreamIterator(bar)),
		mustReverseStreamIterator(NewStreamIterator(foo)),
		mustReverseStreamIterator(NewStreamIterator(bar)),
		mustReverseStreamIterator(NewStreamIterator(foo)),
	}, logproto.BACKWARD)
	assertIt(it, true, len(foo.Entries))
}

func TestMergeIteratorWithoutLabels(t *testing.T) {
	foo := logproto.Stream{
		Labels: ``,
		Hash:   hashLabels(`{app="foo"}`),
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1), Line: "1"},
			{Timestamp: time.Unix(0, 2), Line: "2"},
			{Timestamp: time.Unix(0, 3), Line: "3"},
		},
	}
	bar := logproto.Stream{
		Labels: `{some="other"}`,
		Hash:   hashLabels(`{app="bar"}`),
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1), Line: "1"},
			{Timestamp: time.Unix(0, 2), Line: "2"},
			{Timestamp: time.Unix(0, 3), Line: "3"},
		},
	}

	// forward iteration
	it := NewMergeEntryIterator(context.Background(), []EntryIterator{
		NewStreamIterator(foo),
		NewStreamIterator(bar),
		NewStreamIterator(foo),
		NewStreamIterator(bar),
		NewStreamIterator(foo),
		NewStreamIterator(bar),
		NewStreamIterator(foo),
	}, logproto.FORWARD)

	for i := 0; i < 3; i++ {

		require.True(t, it.Next())
		require.NoError(t, it.Err())
		require.Equal(t, bar.Labels, it.Labels())
		require.Equal(t, bar.Entries[i], it.At())

		require.True(t, it.Next())
		require.NoError(t, it.Err())
		require.Equal(t, foo.Labels, it.Labels())
		require.Equal(t, foo.Entries[i], it.At())

	}
	require.False(t, it.Next())
	require.NoError(t, it.Err())
}

func mustReverseStreamIterator(it EntryIterator) EntryIterator {
	reversed, err := NewReversedIter(it, 0, true)
	if err != nil {
		panic(err)
	}
	return reversed
}

func TestReverseIterator(t *testing.T) {
	itr1 := mkStreamIterator(inverse(offset(testSize, identity)), defaultLabels)
	itr2 := mkStreamIterator(inverse(offset(testSize, identity)), "{foobar: \"bazbar\"}")

	mergeIterator := NewMergeEntryIterator(context.Background(), []EntryIterator{itr1, itr2}, logproto.BACKWARD)
	reversedIter, err := NewReversedIter(mergeIterator, testSize, false)
	require.NoError(t, err)

	for i := int64((testSize / 2) + 1); i <= testSize; i++ {
		assert.Equal(t, true, reversedIter.Next())
		assert.Equal(t, identity(i), reversedIter.At(), fmt.Sprintln("iteration", i))
		assert.Equal(t, reversedIter.Labels(), itr2.Labels())
		assert.Equal(t, true, reversedIter.Next())
		assert.Equal(t, identity(i), reversedIter.At(), fmt.Sprintln("iteration", i))
		assert.Equal(t, reversedIter.Labels(), itr1.Labels())
	}

	assert.Equal(t, false, reversedIter.Next())
	assert.Equal(t, nil, reversedIter.Err())
	assert.NoError(t, reversedIter.Close())
}

func TestReverseEntryIterator(t *testing.T) {
	itr1 := mkStreamIterator(identity, defaultLabels)

	reversedIter, err := NewEntryReversedIter(itr1)
	require.NoError(t, err)

	for i := int64(testSize - 1); i >= 0; i-- {
		assert.Equal(t, true, reversedIter.Next())
		assert.Equal(t, identity(i), reversedIter.At(), fmt.Sprintln("iteration", i))
		assert.Equal(t, reversedIter.Labels(), defaultLabels)
	}

	assert.Equal(t, false, reversedIter.Next())
	assert.Equal(t, nil, reversedIter.Err())
	assert.NoError(t, reversedIter.Close())
}

func TestReverseEntryIteratorUnlimited(t *testing.T) {
	itr1 := mkStreamIterator(offset(testSize, identity), defaultLabels)
	itr2 := mkStreamIterator(offset(testSize, identity), "{foobar: \"bazbar\"}")

	mergeIterator := NewMergeEntryIterator(context.Background(), []EntryIterator{itr1, itr2}, logproto.BACKWARD)
	reversedIter, err := NewReversedIter(mergeIterator, 0, false)
	require.NoError(t, err)

	var ct int
	expected := 2 * testSize

	for reversedIter.Next() {
		ct++
	}
	require.Equal(t, expected, ct)
}

func Test_PeekingIterator(t *testing.T) {
	iter := NewPeekingIterator(NewStreamIterator(logproto.Stream{
		Entries: []logproto.Entry{
			{
				Timestamp: time.Unix(0, 1),
			},
			{
				Timestamp: time.Unix(0, 2),
			},
			{
				Timestamp: time.Unix(0, 3),
			},
		},
	}))
	_, peek, ok := iter.Peek()
	if peek.Timestamp.UnixNano() != 1 {
		t.Fatal("wrong peeked time.")
	}
	if !ok {
		t.Fatal("should be ok.")
	}
	hasNext := iter.Next()
	if !hasNext {
		t.Fatal("should have next.")
	}
	if iter.At().Timestamp.UnixNano() != 1 {
		t.Fatal("wrong peeked time.")
	}

	_, peek, ok = iter.Peek()
	if peek.Timestamp.UnixNano() != 2 {
		t.Fatal("wrong peeked time.")
	}
	if !ok {
		t.Fatal("should be ok.")
	}
	hasNext = iter.Next()
	if !hasNext {
		t.Fatal("should have next.")
	}
	if iter.At().Timestamp.UnixNano() != 2 {
		t.Fatal("wrong peeked time.")
	}
	_, peek, ok = iter.Peek()
	if peek.Timestamp.UnixNano() != 3 {
		t.Fatal("wrong peeked time.")
	}
	if !ok {
		t.Fatal("should be ok.")
	}
	hasNext = iter.Next()
	if !hasNext {
		t.Fatal("should have next.")
	}
	if iter.At().Timestamp.UnixNano() != 3 {
		t.Fatal("wrong peeked time.")
	}
	_, _, ok = iter.Peek()
	if ok {
		t.Fatal("should not be ok.")
	}
}

func Test_DuplicateCount(t *testing.T) {
	stream := logproto.Stream{
		Entries: []logproto.Entry{
			{
				Timestamp: time.Unix(0, 1),
				Line:      "foo",
			},
			{
				Timestamp: time.Unix(0, 2),
				Line:      "foo",
			},
			{
				Timestamp: time.Unix(0, 3),
				Line:      "foo",
			},
		},
	}

	for _, test := range []struct {
		name               string
		iters              []EntryIterator
		direction          logproto.Direction
		expectedDuplicates int64
	}{
		{
			"empty b",
			[]EntryIterator{},
			logproto.BACKWARD,
			0,
		},
		{
			"empty f",
			[]EntryIterator{},
			logproto.FORWARD,
			0,
		},
		{
			"replication 2 b",
			[]EntryIterator{
				mustReverseStreamIterator(NewStreamIterator(stream)),
				mustReverseStreamIterator(NewStreamIterator(stream)),
			},
			logproto.BACKWARD,
			3,
		},
		{
			"replication 2 f",
			[]EntryIterator{
				NewStreamIterator(stream),
				NewStreamIterator(stream),
			},
			logproto.FORWARD,
			3,
		},
		{
			"replication 3 f",
			[]EntryIterator{
				NewStreamIterator(stream),
				NewStreamIterator(stream),
				NewStreamIterator(stream),
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 4),
							Line:      "bar",
						},
					},
				}),
			},
			logproto.FORWARD,
			6,
		},
		{
			"replication 3 b",
			[]EntryIterator{
				mustReverseStreamIterator(NewStreamIterator(stream)),
				mustReverseStreamIterator(NewStreamIterator(stream)),
				mustReverseStreamIterator(NewStreamIterator(stream)),
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 4),
							Line:      "bar",
						},
					},
				}),
			},
			logproto.BACKWARD,
			6,
		},
		{
			"single f",
			[]EntryIterator{
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 4),
							Line:      "bar",
						},
					},
				}),
			},
			logproto.FORWARD,
			0,
		},
		{
			"single b",
			[]EntryIterator{
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 4),
							Line:      "bar",
						},
					},
				}),
			},
			logproto.BACKWARD,
			0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, ctx := stats.NewContext(context.Background())
			it := NewMergeEntryIterator(ctx, test.iters, test.direction)
			defer it.Close()
			//nolint:revive
			for it.Next() {
			}
			require.Equal(t, test.expectedDuplicates, stats.FromContext(ctx).Result(0, 0, 0).TotalDuplicates())
		})
	}
}

func Test_timeRangedIterator_Next(t *testing.T) {
	tests := []struct {
		mint   time.Time
		maxt   time.Time
		expect []bool // array of expected values for next call in sequence
	}{
		{time.Unix(0, 0), time.Unix(0, 0), []bool{false}},
		{time.Unix(0, 0), time.Unix(0, 1), []bool{false}},
		{time.Unix(0, 1), time.Unix(0, 1), []bool{true, false}},
		{time.Unix(0, 1), time.Unix(0, 2), []bool{true, false}},
		{time.Unix(0, 1), time.Unix(0, 3), []bool{true, true, false}},
		{time.Unix(0, 3), time.Unix(0, 3), []bool{true, false}},
		{time.Unix(0, 4), time.Unix(0, 10), []bool{false}},
		{time.Unix(0, 1), time.Unix(0, 10), []bool{true, true, true, false}},
		{time.Unix(0, 0), time.Unix(0, 10), []bool{true, true, true, false}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("mint:%d maxt:%d", tt.mint.UnixNano(), tt.maxt.UnixNano()), func(t *testing.T) {
			it := NewTimeRangedIterator(
				NewStreamIterator(
					logproto.Stream{Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 1)},
						{Timestamp: time.Unix(0, 2)},
						{Timestamp: time.Unix(0, 3)},
					}}),
				tt.mint,
				tt.maxt,
			)
			for _, b := range tt.expect {
				require.Equal(t, b, it.Next())
			}
			require.NoError(t, it.Close())
		})
		t.Run(fmt.Sprintf("mint:%d maxt:%d_sample", tt.mint.UnixNano(), tt.maxt.UnixNano()), func(t *testing.T) {
			it := NewTimeRangedSampleIterator(
				NewSeriesIterator(
					logproto.Series{Samples: []logproto.Sample{
						sample(1),
						sample(2),
						sample(3),
					}}),
				tt.mint.UnixNano(),
				tt.maxt.UnixNano(),
			)
			for _, b := range tt.expect {
				require.Equal(t, b, it.Next())
			}
			require.NoError(t, it.Close())
		})
	}
}

type CloseTestingIterator struct {
	closed atomic.Bool
	e      logproto.Entry
}

func (i *CloseTestingIterator) Next() bool         { return true }
func (i *CloseTestingIterator) At() logproto.Entry { return i.e }
func (i *CloseTestingIterator) Labels() string     { return "" }
func (i *CloseTestingIterator) StreamHash() uint64 { return 0 }
func (i *CloseTestingIterator) Err() error         { return nil }
func (i *CloseTestingIterator) Close() error {
	i.closed.Store(true)
	return nil
}

func TestNonOverlappingClose(t *testing.T) {
	a, b := &CloseTestingIterator{}, &CloseTestingIterator{}
	itr := NewNonOverlappingIterator([]EntryIterator{a, b})

	// Ensure both itr.cur and itr.iterators are non nil
	itr.Next()

	require.NotNil(t, itr.(*nonOverlappingIterator).curr)

	itr.Close()

	require.Equal(t, true, a.closed.Load())
	require.Equal(t, true, b.closed.Load())
}

func BenchmarkSortIterator(b *testing.B) {
	var (
		ctx          = context.Background()
		streams      []logproto.Stream
		entriesCount = 10000
		streamsCount = 100
	)
	for i := 0; i < streamsCount; i++ {
		streams = append(streams, logproto.Stream{
			Labels: fmt.Sprintf(`{i="%d"}`, i),
		})
	}
	for i := 0; i < entriesCount; i++ {
		streams[i%streamsCount].Entries = append(streams[i%streamsCount].Entries, logproto.Entry{
			Timestamp: time.Unix(0, int64(streamsCount-i)),
			Line:      fmt.Sprintf("%d", i),
		})
	}
	rand.Shuffle(len(streams), func(i, j int) {
		streams[i], streams[j] = streams[j], streams[i]
	})

	b.Run("merge sort", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			var itrs []EntryIterator
			for i := 0; i < streamsCount; i++ {
				itrs = append(itrs, NewStreamIterator(streams[i]))
			}
			b.StartTimer()
			it := NewMergeEntryIterator(ctx, itrs, logproto.BACKWARD)
			for it.Next() {
				it.At()
			}
			it.Close()
		}
	})

	b.Run("merge sort dedupe", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			var itrs []EntryIterator
			for i := 0; i < streamsCount; i++ {
				itrs = append(itrs, NewStreamIterator(streams[i]))
				itrs = append(itrs, NewStreamIterator(streams[i]))
			}
			b.StartTimer()
			it := NewMergeEntryIterator(ctx, itrs, logproto.BACKWARD)
			for it.Next() {
				it.At()
			}
			it.Close()
		}
	})

	b.Run("sort", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			var itrs []EntryIterator
			for i := 0; i < streamsCount; i++ {
				itrs = append(itrs, NewStreamIterator(streams[i]))
			}
			b.StartTimer()
			it := NewSortEntryIterator(itrs, logproto.BACKWARD)
			for it.Next() {
				it.At()
			}
			it.Close()
		}
	})
}

func Test_EntrySortIterator(t *testing.T) {
	t.Run("backward", func(t *testing.T) {
		t.Parallel()
		it := NewSortEntryIterator(
			[]EntryIterator{
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 5)},
						{Timestamp: time.Unix(0, 3)},
						{Timestamp: time.Unix(0, 0)},
					},
					Labels: `{foo="bar"}`,
				}),
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 4)},
						{Timestamp: time.Unix(0, 2)},
						{Timestamp: time.Unix(0, 1)},
					},
					Labels: `{foo="buzz"}`,
				}),
			}, logproto.BACKWARD)
		var i int64 = 5
		defer it.Close()
		for it.Next() {
			require.Equal(t, time.Unix(0, i), it.At().Timestamp)
			i--
		}
	})
	t.Run("forward", func(t *testing.T) {
		t.Parallel()
		it := NewSortEntryIterator(
			[]EntryIterator{
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 0)},
						{Timestamp: time.Unix(0, 3)},
						{Timestamp: time.Unix(0, 5)},
					},
					Labels: `{foo="bar"}`,
				}),
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 1)},
						{Timestamp: time.Unix(0, 2)},
						{Timestamp: time.Unix(0, 4)},
					},
					Labels: `{foo="buzz"}`,
				}),
			}, logproto.FORWARD)
		var i int64
		defer it.Close()
		for it.Next() {
			require.Equal(t, time.Unix(0, i), it.At().Timestamp)
			i++
		}
	})
	t.Run("forward sort by stream", func(t *testing.T) {
		t.Parallel()
		it := NewSortEntryIterator(
			[]EntryIterator{
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 0)},
						{Timestamp: time.Unix(0, 3)},
						{Timestamp: time.Unix(0, 5)},
					},
					Labels: `b`,
				}),
				NewStreamIterator(logproto.Stream{
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 0)},
						{Timestamp: time.Unix(0, 1)},
						{Timestamp: time.Unix(0, 2)},
						{Timestamp: time.Unix(0, 4)},
					},
					Labels: `a`,
				}),
			}, logproto.FORWARD)
		// The first entry appears in both so we expect it to be sorted by Labels.
		require.True(t, it.Next())
		require.Equal(t, time.Unix(0, 0), it.At().Timestamp)
		require.Equal(t, `a`, it.Labels())

		var i int64
		defer it.Close()
		for it.Next() {
			require.Equal(t, time.Unix(0, i), it.At().Timestamp)
			i++
		}
	})
}

func TestDedupeMergeEntryIterator(t *testing.T) {
	it := NewMergeEntryIterator(context.Background(),
		[]EntryIterator{
			NewStreamIterator(logproto.Stream{
				Labels: ``,
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(1, 0),
						Line:      "0",
					},
					{
						Timestamp: time.Unix(1, 0),
						Line:      "2",
					},
					{
						Timestamp: time.Unix(2, 0),
						Line:      "3",
					},
				},
			}),
			NewStreamIterator(logproto.Stream{
				Labels: ``,
				Entries: []logproto.Entry{
					{
						Timestamp: time.Unix(1, 0),
						Line:      "1",
					},
					{
						Timestamp: time.Unix(1, 0),
						Line:      "0",
					},
					{
						Timestamp: time.Unix(1, 0),
						Line:      "2",
					},
				},
			}),
		}, logproto.FORWARD)
	require.True(t, it.Next())
	lines := []string{it.At().Line}
	require.Equal(t, time.Unix(1, 0), it.At().Timestamp)
	require.True(t, it.Next())
	lines = append(lines, it.At().Line)
	require.Equal(t, time.Unix(1, 0), it.At().Timestamp)
	require.True(t, it.Next())
	lines = append(lines, it.At().Line)
	require.Equal(t, time.Unix(1, 0), it.At().Timestamp)
	require.True(t, it.Next())
	lines = append(lines, it.At().Line)
	require.Equal(t, time.Unix(2, 0), it.At().Timestamp)
	// Two orderings are consistent with the inputs.
	if lines[0] == "1" {
		require.Equal(t, []string{"1", "0", "2", "3"}, lines)
	} else {
		require.Equal(t, []string{"0", "2", "1", "3"}, lines)
	}
}

func TestMergeIteratorNoDedupDifferentStructuredMetadata(t *testing.T) {
	// Entries with the same timestamp and line but different structured metadata
	// must NOT be deduplicated — they are distinct entries.
	const labels = `{app="foo"}`
	ts := time.Unix(1, 0)
	line := "same message"

	entry1 := logproto.Entry{
		Timestamp:          ts,
		Line:               line,
		StructuredMetadata: []logproto.LabelAdapter{{Name: "trace_id", Value: "aaa"}},
	}
	entry2 := logproto.Entry{
		Timestamp:          ts,
		Line:               line,
		StructuredMetadata: []logproto.LabelAdapter{{Name: "trace_id", Value: "bbb"}},
	}

	it := NewMergeEntryIterator(context.Background(),
		[]EntryIterator{
			NewStreamIterator(logproto.Stream{Labels: labels, Hash: hashLabels(labels), Entries: []logproto.Entry{entry1}}),
			NewStreamIterator(logproto.Stream{Labels: labels, Hash: hashLabels(labels), Entries: []logproto.Entry{entry2}}),
		}, logproto.FORWARD)

	var got []logproto.Entry
	for it.Next() {
		got = append(got, it.At())
	}
	require.NoError(t, it.Err())

	require.Len(t, got, 2, "entries with different structured metadata must not be deduplicated")
	require.Equal(t, ts, got[0].Timestamp)
	require.Equal(t, line, got[0].Line)
	require.Equal(t, ts, got[1].Timestamp)
	require.Equal(t, line, got[1].Line)
	require.NotEqual(t, got[0].StructuredMetadata, got[1].StructuredMetadata)
}

// TestNonOverlappingIterator_ShouldSurfaceErrors verifies the entry concatenation
// reports a sub-iterator failure through Err instead of treating it as normal
// exhaustion.
func TestNonOverlappingIterator_ShouldSurfaceErrors(t *testing.T) {
	line := func(ts int) logproto.Entry {
		return logproto.Entry{Timestamp: time.Unix(int64(ts), 0), Line: fmt.Sprintf("%d", ts)}
	}
	failing := func(ts int, labels string, err error) EntryIterator {
		return &erroringEntryIterator{entries: []logproto.Entry{line(ts)}, labels: labels, err: err}
	}
	healthy := func(ts int, labels string) EntryIterator {
		return NewStreamIterator(logproto.Stream{Labels: labels, Entries: []logproto.Entry{line(ts)}})
	}

	t.Run("error stops iteration and is surfaced", func(t *testing.T) {
		wantErr := errors.New("boom")
		it := NewNonOverlappingIterator([]EntryIterator{
			failing(1, `{app="a"}`, wantErr),
			healthy(2, `{app="b"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 1, got, "iteration stops at the failing stream; later streams are not played")
		require.ErrorIs(t, it.Err(), wantErr, "the error must be surfaced, not dropped as normal exhaustion")
	})

	t.Run("error in the last stream is surfaced", func(t *testing.T) {
		wantErr := errors.New("boom")
		it := NewNonOverlappingIterator([]EntryIterator{
			healthy(1, `{app="a"}`),
			failing(2, `{app="b"}`, wantErr),
		})

		for it.Next() { //nolint:revive
		}
		require.ErrorIs(t, it.Err(), wantErr)
	})

	t.Run("no error returns nil", func(t *testing.T) {
		it := NewNonOverlappingIterator([]EntryIterator{
			healthy(1, `{app="a"}`),
			healthy(2, `{app="b"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 2, got)
		require.NoError(t, it.Err())
	})

	t.Run("close error surfaces through Close, not Err", func(t *testing.T) {
		wantErr := errors.New("close boom")
		it := NewNonOverlappingIterator([]EntryIterator{
			&erroringEntryIterator{entries: []logproto.Entry{line(1)}, labels: `{app="a"}`, closeErr: wantErr},
			healthy(2, `{app="b"}`),
		})

		require.NoError(t, it.Err(), "a close error is not a read error")
		require.ErrorIs(t, it.Close(), wantErr, "Close must surface a sub-iterator close error")
	})

	t.Run("close error while iterating is not a read error", func(t *testing.T) {
		// The first iterator reads cleanly, then its Close fails while iterating.
		// That is a cleanup failure, not a read failure, so iteration continues
		// and Err stays nil.
		it := NewNonOverlappingIterator([]EntryIterator{
			&erroringEntryIterator{entries: []logproto.Entry{line(1)}, labels: `{app="a"}`, closeErr: errors.New("close boom")},
			healthy(2, `{app="b"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 2, got, "both streams play; a close failure does not stop iteration")
		require.NoError(t, it.Err(), "a close error during iteration must not become a read error")
	})

	t.Run("stream that errors before any entry stops immediately", func(t *testing.T) {
		wantErr := errors.New("open failed")
		it := NewNonOverlappingIterator([]EntryIterator{
			&erroringEntryIterator{labels: `{app="a"}`, err: wantErr}, // no entries
			healthy(2, `{app="b"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 0, got, "no entries are produced")
		require.ErrorIs(t, it.Err(), wantErr)
	})

	t.Run("error in a middle stream stops before later streams", func(t *testing.T) {
		wantErr := errors.New("boom")
		it := NewNonOverlappingIterator([]EntryIterator{
			healthy(1, `{app="a"}`),
			failing(2, `{app="b"}`, wantErr),
			healthy(3, `{app="c"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 2, got, "the stream after the failing one is not played")
		require.ErrorIs(t, it.Err(), wantErr)
	})

	t.Run("fail-fast leaves cleanup to Close", func(t *testing.T) {
		errored := &erroringEntryIterator{entries: []logproto.Entry{line(1)}, labels: `{app="a"}`, err: errors.New("boom")}
		later1 := &erroringEntryIterator{entries: []logproto.Entry{line(2)}, labels: `{app="b"}`}
		later2 := &erroringEntryIterator{entries: []logproto.Entry{line(3)}, labels: `{app="c"}`}

		it := NewNonOverlappingIterator([]EntryIterator{errored, later1, later2})
		for it.Next() { //nolint:revive
		}
		require.NoError(t, it.Close())

		require.Equal(t, 1, errored.closed, "the errored current iterator is closed once")
		require.Equal(t, 1, later1.closed, "an un-started iterator is closed once")
		require.Equal(t, 1, later2.closed, "an un-started iterator is closed once")
	})

	t.Run("close error replaying an already-surfaced read error is not reported again", func(t *testing.T) {
		// Some EntryIterator implementations (e.g. the chunk block iterator) return
		// their stored read error from Close too, as a fallback for callers that
		// only check the Close return value. That must not turn into a second,
		// spurious close error once the read error already surfaced through Err.
		wantErr := errors.New("boom")
		errored := &erroringEntryIterator{entries: []logproto.Entry{line(1)}, labels: `{app="a"}`, err: wantErr, closeErr: wantErr}

		it := NewNonOverlappingIterator([]EntryIterator{errored, healthy(2, `{app="b"}`)})
		for it.Next() { //nolint:revive
		}
		require.ErrorIs(t, it.Err(), wantErr)
		require.NoError(t, it.Close())
		require.Equal(t, 1, errored.closed)
	})

	t.Run("Close surfaces every close error", func(t *testing.T) {
		it := NewNonOverlappingIterator([]EntryIterator{
			&erroringEntryIterator{entries: []logproto.Entry{line(1)}, closeErr: errors.New("close a")},
			&erroringEntryIterator{entries: []logproto.Entry{line(2)}, closeErr: errors.New("close b")},
		})

		err := it.Close() // no iteration, so both remain for Close to close
		require.ErrorContains(t, err, "close a")
		require.ErrorContains(t, err, "close b")
	})

	t.Run("Err is stable after a read error", func(t *testing.T) {
		wantErr := errors.New("boom")
		it := NewNonOverlappingIterator([]EntryIterator{failing(1, `{app="a"}`, wantErr)})

		for it.Next() { //nolint:revive
		}
		require.ErrorIs(t, it.Err(), wantErr)

		require.False(t, it.Next(), "further Next calls keep returning false")
		require.ErrorIs(t, it.Err(), wantErr, "Err stays set")
	})
}

// erroringEntryIterator yields its entries, then fails: the Next after the last
// entry returns false with err set, modeling a stream that read some data and
// then failed mid-stream.
type erroringEntryIterator struct {
	entries  []logproto.Entry
	labels   string
	err      error // a read failure, returned by Err once the entries are exhausted
	closeErr error // a cleanup failure, returned by Close; kept separate from err
	closed   int   // number of times Close was called
	i        int
}

func (it *erroringEntryIterator) Next() bool {
	it.i++
	return it.i <= len(it.entries)
}
func (it *erroringEntryIterator) At() logproto.Entry { return it.entries[it.i-1] }
func (it *erroringEntryIterator) Labels() string     { return it.labels }
func (it *erroringEntryIterator) StreamHash() uint64 { return 0 }
func (it *erroringEntryIterator) Close() error       { it.closed++; return it.closeErr }
func (it *erroringEntryIterator) Err() error {
	if it.i > len(it.entries) {
		return it.err
	}
	return nil
}
