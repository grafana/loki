package iter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/stats"
)

const testSize = 10
const defaultLabels = "{foo=\"baz\"}"

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
			iterator: NewHeapIterator(context.Background(), []EntryIterator{
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
			iterator: NewHeapIterator(context.Background(), []EntryIterator{
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
			iterator: NewHeapIterator(context.Background(), []EntryIterator{
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
				assert.Equal(t, tc.generator(i), tc.iterator.Entry(), fmt.Sprintln("iteration", i))
				assert.Equal(t, tc.labels, tc.iterator.Labels(), fmt.Sprintln("iteration", i))
			}

			assert.Equal(t, false, tc.iterator.Next())
			assert.Equal(t, nil, tc.iterator.Error())
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
			iterator: NewHeapIterator(context.Background(), []EntryIterator{
				mkStreamIterator(identity, "{foobar: \"baz1\"}"),
				mkStreamIterator(identity, "{foobar: \"baz2\"}"),
			}, logproto.FORWARD),
			generator: func(i int64) logproto.Entry {
				return identity(i / 2)
			},
			length: testSize * 2,
			labels: func(i int64) string {
				if i%2 == 0 {
					return "{foobar: \"baz1\"}"
				}
				return "{foobar: \"baz2\"}"
			},
		},

		// Test merging with differing labels but all the same timestamps and different values.
		{
			iterator: NewHeapIterator(context.Background(), []EntryIterator{
				mkStreamIterator(constant(0), "{foobar: \"baz1\"}"),
				mkStreamIterator(constant(0), "{foobar: \"baz2\"}"),
			}, logproto.FORWARD),
			generator: func(i int64) logproto.Entry {
				return constant(0)(i % testSize)
			},
			length: testSize * 2,
			labels: func(i int64) string {
				if i/testSize == 0 {
					return "{foobar: \"baz1\"}"
				}
				return "{foobar: \"baz2\"}"
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			for i := int64(0); i < tc.length; i++ {
				assert.Equal(t, true, tc.iterator.Next())
				assert.Equal(t, tc.generator(i), tc.iterator.Entry(), fmt.Sprintln("iteration", i))
				assert.Equal(t, tc.labels(i), tc.iterator.Labels(), fmt.Sprintln("iteration", i))
			}

			assert.Equal(t, false, tc.iterator.Next())
			assert.Equal(t, nil, tc.iterator.Error())
			assert.NoError(t, tc.iterator.Close())
		})
	}
}

func TestHeapIteratorPrefetch(t *testing.T) {
	t.Parallel()

	type tester func(t *testing.T, i HeapIterator)

	tests := map[string]tester{
		"prefetch on Len() when called as first method": func(t *testing.T, i HeapIterator) {
			assert.Equal(t, 2, i.Len())
		},
		"prefetch on Peek() when called as first method": func(t *testing.T, i HeapIterator) {
			assert.Equal(t, time.Unix(0, 0), i.Peek())
		},
		"prefetch on Next() when called as first method": func(t *testing.T, i HeapIterator) {
			assert.True(t, i.Next())
			assert.Equal(t, logproto.Entry{Timestamp: time.Unix(0, 0), Line: "0"}, i.Entry())
		},
	}

	for testName, testFunc := range tests {
		testFunc := testFunc

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			i := NewHeapIterator(context.Background(), []EntryIterator{
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
	})
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

func TestHeapIteratorDeduplication(t *testing.T) {
	foo := logproto.Stream{
		Labels: `{app="foo"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1), Line: "1"},
			{Timestamp: time.Unix(0, 2), Line: "2"},
			{Timestamp: time.Unix(0, 3), Line: "3"},
		},
	}
	bar := logproto.Stream{
		Labels: `{app="bar"}`,
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
			require.NoError(t, it.Error())
			require.Equal(t, bar.Labels, it.Labels())
			require.Equal(t, bar.Entries[j], it.Entry())

			require.True(t, it.Next())
			require.NoError(t, it.Error())
			require.Equal(t, foo.Labels, it.Labels())
			require.Equal(t, foo.Entries[j], it.Entry())

		}
		require.False(t, it.Next())
		require.NoError(t, it.Error())
	}
	// forward iteration
	it := NewHeapIterator(context.Background(), []EntryIterator{
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
	it = NewHeapIterator(context.Background(), []EntryIterator{
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

	heapIterator := NewHeapIterator(context.Background(), []EntryIterator{itr1, itr2}, logproto.BACKWARD)
	reversedIter, err := NewReversedIter(heapIterator, testSize, false)
	require.NoError(t, err)

	for i := int64((testSize / 2) + 1); i <= testSize; i++ {
		assert.Equal(t, true, reversedIter.Next())
		assert.Equal(t, identity(i), reversedIter.Entry(), fmt.Sprintln("iteration", i))
		assert.Equal(t, reversedIter.Labels(), itr2.Labels())
		assert.Equal(t, true, reversedIter.Next())
		assert.Equal(t, identity(i), reversedIter.Entry(), fmt.Sprintln("iteration", i))
		assert.Equal(t, reversedIter.Labels(), itr1.Labels())
	}

	assert.Equal(t, false, reversedIter.Next())
	assert.Equal(t, nil, reversedIter.Error())
	assert.NoError(t, reversedIter.Close())
}

func TestReverseEntryIterator(t *testing.T) {
	itr1 := mkStreamIterator(identity, defaultLabels)

	reversedIter, err := NewEntryReversedIter(itr1)
	require.NoError(t, err)

	for i := int64(testSize - 1); i >= 0; i-- {
		assert.Equal(t, true, reversedIter.Next())
		assert.Equal(t, identity(i), reversedIter.Entry(), fmt.Sprintln("iteration", i))
		assert.Equal(t, reversedIter.Labels(), defaultLabels)
	}

	assert.Equal(t, false, reversedIter.Next())
	assert.Equal(t, nil, reversedIter.Error())
	assert.NoError(t, reversedIter.Close())
}

func TestReverseEntryIteratorUnlimited(t *testing.T) {
	itr1 := mkStreamIterator(offset(testSize, identity), defaultLabels)
	itr2 := mkStreamIterator(offset(testSize, identity), "{foobar: \"bazbar\"}")

	heapIterator := NewHeapIterator(context.Background(), []EntryIterator{itr1, itr2}, logproto.BACKWARD)
	reversedIter, err := NewReversedIter(heapIterator, 0, false)
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
	if iter.Entry().Timestamp.UnixNano() != 1 {
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
	if iter.Entry().Timestamp.UnixNano() != 2 {
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
	if iter.Entry().Timestamp.UnixNano() != 3 {
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
				NewStreamIterator(stream),
				NewStreamIterator(stream),
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
					}}),
			},
			logproto.FORWARD,
			6,
		},
		{
			"replication 3 b",
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
					}}),
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
					}}),
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
					}}),
			},
			logproto.BACKWARD,
			0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = stats.NewContext(ctx)
			it := NewHeapIterator(ctx, test.iters, test.direction)
			defer it.Close()
			for it.Next() {
			}
			require.Equal(t, test.expectedDuplicates, stats.GetChunkData(ctx).TotalDuplicates)
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
