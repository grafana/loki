package iter

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"

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
	return NewStreamIterator(&logproto.Stream{
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

func TestMostCommon(t *testing.T) {
	// First is most common.
	tuples := []tuple{
		{Entry: logproto.Entry{Line: "a"}},
		{Entry: logproto.Entry{Line: "b"}},
		{Entry: logproto.Entry{Line: "c"}},
		{Entry: logproto.Entry{Line: "a"}},
		{Entry: logproto.Entry{Line: "b"}},
		{Entry: logproto.Entry{Line: "c"}},
		{Entry: logproto.Entry{Line: "a"}},
	}
	require.Equal(t, "a", mostCommon(tuples).Entry.Line)

	tuples = []tuple{
		{Entry: logproto.Entry{Line: "a"}},
		{Entry: logproto.Entry{Line: "b"}},
		{Entry: logproto.Entry{Line: "b"}},
		{Entry: logproto.Entry{Line: "c"}},
		{Entry: logproto.Entry{Line: "c"}},
		{Entry: logproto.Entry{Line: "c"}},
		{Entry: logproto.Entry{Line: "d"}},
	}
	require.Equal(t, "c", mostCommon(tuples).Entry.Line)
}

func TestInsert(t *testing.T) {
	toInsert := []tuple{
		{Entry: logproto.Entry{Line: "a"}},
		{Entry: logproto.Entry{Line: "e"}},
		{Entry: logproto.Entry{Line: "c"}},
		{Entry: logproto.Entry{Line: "b"}},
		{Entry: logproto.Entry{Line: "d"}},
		{Entry: logproto.Entry{Line: "a"}},
		{Entry: logproto.Entry{Line: "c"}},
	}

	var ts []tuple
	for _, e := range toInsert {
		ts = insert(ts, e)
	}

	require.True(t, sort.SliceIsSorted(ts, func(i, j int) bool {
		return ts[i].Line < ts[j].Line
	}))
}

func TestReversableEntryIteratorImpls(t *testing.T) {
	// typecheck that these satisfy ReversableIterator
	checks := []EntryIterator{
		&streamIterator{},
		&nonOverlappingIterator{},
		&reversableIterator{},
	}

	for _, check := range checks {
		t.Run(fmt.Sprintf("%T", check), func(t *testing.T) {
			// Uncomment below for access to unsatiscactory implementation.
			// _ = check.(ReversableIterator)
			_, ok := check.(ReversableIterator)
			require.Equal(t, true, ok)
		})
	}
}

func TestReversableEntryIterator(t *testing.T) {
	oddStream := func() *logproto.Stream {
		return &logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{
					Timestamp: time.Unix(0, 0),
					Line:      "0",
				},
				{
					Timestamp: time.Unix(1, 0),
					Line:      "1",
				},
				{
					Timestamp: time.Unix(2, 0),
					Line:      "2",
				},
			},
		}
	}

	backwardsOddStream := func() *logproto.Stream {
		return &logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{
					Timestamp: time.Unix(2, 0),
					Line:      "2",
				},
				{
					Timestamp: time.Unix(1, 0),
					Line:      "1",
				},

				{
					Timestamp: time.Unix(0, 0),
					Line:      "0",
				},
			},
		}
	}

	evenStream := func() *logproto.Stream {
		stream := oddStream()
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp: time.Unix(3, 0),
			Line:      "3",
		})
		return stream
	}

	backwardsEvenStream := func() *logproto.Stream {
		stream := backwardsOddStream()
		stream.Entries = append([]logproto.Entry{
			{
				Timestamp: time.Unix(3, 0),
				Line:      "3",
			},
		}, stream.Entries...)
		return stream
	}

	for _, f := range []struct {
		name string
		fn   func(stream *logproto.Stream) EntryIterator
	}{
		{
			name: "streamIterator",
			fn: func(stream *logproto.Stream) EntryIterator {
				return NewStreamIterator(stream)
			},
		},
		{
			name: "reversedIterator",
			fn: func(stream *logproto.Stream) EntryIterator {
				iter := NewReversedIter(NewStreamIterator(stream), 0, false)
				iter.(ReversableIterator).Reverse()
				return iter
			},
		},
	} {
		for _, pair := range []struct {
			name     string
			in       func() *logproto.Stream
			reversed func() *logproto.Stream
		}{
			{
				name:     "evenStream",
				in:       evenStream,
				reversed: backwardsEvenStream,
			},
			{
				name:     "oddStream",
				in:       oddStream,
				reversed: backwardsOddStream,
			},
		} {
			t.Run(fmt.Sprintf("%s-%s", f.name, pair.name), func(t *testing.T) {
				// test constructor doesn't alter iter
				equalIters(t, NewStreamIterator(pair.in()), f.fn(pair.in()))
				// test reverse
				rev := f.fn(pair.in())
				rev.(ReversableIterator).Reverse()
				equalIters(t, NewStreamIterator(pair.reversed()), rev)
				// double reverse should noop
				noop := f.fn(pair.in())
				noop.(ReversableIterator).Reverse()
				noop.(ReversableIterator).Reverse()
				equalIters(t, NewStreamIterator(pair.in()), noop)
			})
		}
	}
}

func TestReversableNonOverlappingIter(t *testing.T) {
	first := func() EntryIterator { return mkStreamIterator(identity, `{foo="bar"}`) }
	second := func() EntryIterator { return mkStreamIterator(offset(testSize, identity), `{foo="bar"}`) }

	combined := func() EntryIterator {
		var combined []logproto.Entry
		for i := int64(0); i < testSize*2; i++ {
			combined = append(combined, identity(i))
		}
		return NewStreamIterator(&logproto.Stream{
			Entries: combined,
			Labels:  `{foo="bar"}`,
		})
	}

	overlapping := NewNonOverlappingIterator([]EntryIterator{first(), second()}, `{foo="bar"}`)
	equalIters(t, combined(), overlapping)

	// test reverse
	rev := NewNonOverlappingIterator([]EntryIterator{first(), second()}, `{foo="bar"}`)
	rev.(ReversableIterator).Reverse()
	equalIters(t, NewReversedIter(combined(), 0, false), rev)

	// test double reverse == identity
	dbl := NewNonOverlappingIterator([]EntryIterator{first(), second()}, `{foo="bar"}`)
	dbl.(ReversableIterator).Reverse()
	dbl.(ReversableIterator).Reverse()
	equalIters(t, combined(), dbl)
}

func TestReverseEntryIterator(t *testing.T) {
	itr1 := mkStreamIterator(inverse(offset(testSize, identity)), defaultLabels)
	itr2 := mkStreamIterator(inverse(offset(testSize, identity)), "{foobar: \"bazbar\"}")

	heapIterator := NewHeapIterator(context.Background(), []EntryIterator{itr1, itr2}, logproto.BACKWARD)
	reversedIter := NewReversedIter(heapIterator, testSize, false)

	for i := int64((testSize / 2) + 1); i <= testSize; i++ {
		assert.Equal(t, true, reversedIter.Next())
		assert.Equal(t, identity(i), reversedIter.Entry(), fmt.Sprintln("iteration", i))
		assert.Equal(t, reversedIter.Labels(), itr1.Labels())
		assert.Equal(t, true, reversedIter.Next())
		assert.Equal(t, identity(i), reversedIter.Entry(), fmt.Sprintln("iteration", i))
		assert.Equal(t, reversedIter.Labels(), itr2.Labels())
	}

	assert.Equal(t, false, reversedIter.Next())
	assert.Equal(t, nil, reversedIter.Error())
	assert.NoError(t, reversedIter.Close())
}

func TestReverseEntryIteratorUnlimited(t *testing.T) {
	itr1 := mkStreamIterator(offset(testSize, identity), defaultLabels)
	itr2 := mkStreamIterator(offset(testSize, identity), "{foobar: \"bazbar\"}")

	heapIterator := NewHeapIterator(context.Background(), []EntryIterator{itr1, itr2}, logproto.BACKWARD)
	reversedIter := NewReversedIter(heapIterator, 0, false)

	var ct int
	expected := 2 * testSize

	for reversedIter.Next() {
		ct++
	}
	require.Equal(t, expected, ct)
}

func Test_PeekingIterator(t *testing.T) {
	iter := NewPeekingIterator(NewStreamIterator(&logproto.Stream{
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
	stream := &logproto.Stream{
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
				NewStreamIterator(&logproto.Stream{
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
				NewStreamIterator(&logproto.Stream{
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
				NewStreamIterator(&logproto.Stream{
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
				NewStreamIterator(&logproto.Stream{
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

func equalIters(t *testing.T, a, b EntryIterator) {
	for a.Next() {
		require.Equal(t, true, b.Next())
		require.Equal(t, a.Labels(), b.Labels())
		require.Equal(t, a.Entry(), b.Entry())
	}
	require.Equal(t, b.Next(), false)
}
