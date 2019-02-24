package iter

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/assert"
)

const testSize = 10
const defaultLabels = "{foo: \"baz\"}"

func TestIterator(t *testing.T) {
	for i, tc := range []struct {
		iterator  EntryIterator
		generator generator
		length    int64
		labels    string
	}{
		// Test basic identity.
		{
			iterator:  mkStreamIterator(testSize, identity, defaultLabels),
			generator: identity,
			length:    testSize,
			labels:    defaultLabels,
		},

		// Test basic identity (backwards).
		{
			iterator:  mkStreamIterator(testSize, inverse(identity), defaultLabels),
			generator: inverse(identity),
			length:    testSize,
			labels:    defaultLabels,
		},

		// Test dedupe of overlapping iterators with the heap iterator.
		{
			iterator: NewHeapIterator([]EntryIterator{
				mkStreamIterator(testSize, offset(0, identity), defaultLabels),
				mkStreamIterator(testSize, offset(testSize/2, identity), defaultLabels),
				mkStreamIterator(testSize, offset(testSize, identity), defaultLabels),
			}, logproto.FORWARD),
			generator: identity,
			length:    2 * testSize,
			labels:    defaultLabels,
		},

		// Test dedupe of overlapping iterators with the heap iterator (backward).
		{
			iterator: NewHeapIterator([]EntryIterator{
				mkStreamIterator(testSize, inverse(offset(0, identity)), defaultLabels),
				mkStreamIterator(testSize, inverse(offset(-testSize/2, identity)), defaultLabels),
				mkStreamIterator(testSize, inverse(offset(-testSize, identity)), defaultLabels),
			}, logproto.BACKWARD),
			generator: inverse(identity),
			length:    2 * testSize,
			labels:    defaultLabels,
		},

		// Test dedupe of entries with the same timestamp but different entries.
		{
			iterator: NewHeapIterator([]EntryIterator{
				mkStreamIterator(testSize, offset(0, constant(0)), defaultLabels),
				mkStreamIterator(testSize, offset(0, constant(0)), defaultLabels),
				mkStreamIterator(testSize, offset(testSize, constant(0)), defaultLabels),
			}, logproto.FORWARD),
			generator: constant(0),
			length:    2 * testSize,
			labels:    defaultLabels,
		},

		// Test basic identity with non-default labels.
		{
			iterator:  mkStreamIterator(testSize, identity, "{foobar: \"bazbar\"}"),
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
			iterator: NewHeapIterator([]EntryIterator{
				mkStreamIterator(testSize, identity, "{foobar: \"baz1\"}"),
				mkStreamIterator(testSize, identity, "{foobar: \"baz2\"}"),
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
			iterator: NewHeapIterator([]EntryIterator{
				mkStreamIterator(testSize, constant(0), "{foobar: \"baz1\"}"),
				mkStreamIterator(testSize, constant(0), "{foobar: \"baz2\"}"),
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

type generator func(i int64) logproto.Entry

func mkStreamIterator(numEntries int64, f generator, labels string) EntryIterator {
	entries := []logproto.Entry{}
	for i := int64(0); i < numEntries; i++ {
		entries = append(entries, f(i))
	}
	return newStreamIterator(&logproto.Stream{
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

	// Last is most common
	tuples = []tuple{
		{Entry: logproto.Entry{Line: "a"}},
		{Entry: logproto.Entry{Line: "b"}},
		{Entry: logproto.Entry{Line: "c"}},
		{Entry: logproto.Entry{Line: "b"}},
		{Entry: logproto.Entry{Line: "c"}},
		{Entry: logproto.Entry{Line: "c"}},
	}
	require.Equal(t, "c", mostCommon(tuples).Entry.Line)
}
