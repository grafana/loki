package iter

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/assert"
)

const testSize = 100

func TestIterator(t *testing.T) {
	for i, tc := range []struct {
		iterator  EntryIterator
		generator generator
		length    int64
	}{
		// Test basic identity.
		{
			iterator:  mkStreamIterator(testSize, identity),
			generator: identity,
			length:    testSize,
		},

		// Test basic identity (backwards).
		{
			iterator:  mkStreamIterator(testSize, inverse(identity)),
			generator: inverse(identity),
			length:    testSize,
		},

		// Test dedupe of overlapping iterators with the heap iterator.
		{
			iterator: NewHeapIterator([]EntryIterator{
				mkStreamIterator(testSize, offset(0)),
				mkStreamIterator(testSize, offset(testSize/2)),
				mkStreamIterator(testSize, offset(testSize)),
			}, logproto.FORWARD),
			generator: identity,
			length:    2 * testSize,
		},

		// Test dedupe of overlapping iterators with the heap iterator (backward).
		{
			iterator: NewHeapIterator([]EntryIterator{
				mkStreamIterator(testSize, inverse(offset(0))),
				mkStreamIterator(testSize, inverse(offset(-testSize/2))),
				mkStreamIterator(testSize, inverse(offset(-testSize))),
			}, logproto.BACKWARD),
			generator: inverse(identity),
			length:    2 * testSize,
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			for i := int64(0); i < tc.length; i++ {
				assert.Equal(t, true, tc.iterator.Next())
				assert.Equal(t, tc.generator(i), tc.iterator.Entry(), fmt.Sprintln("iteration", i))
			}

			assert.Equal(t, false, tc.iterator.Next())
			assert.Equal(t, nil, tc.iterator.Error())
			assert.NoError(t, tc.iterator.Close())
		})
	}
}

type generator func(i int64) logproto.Entry

func mkStreamIterator(numEntries int64, f generator) EntryIterator {
	entries := []logproto.Entry{}
	for i := int64(0); i < numEntries; i++ {
		entries = append(entries, f(i))
	}
	return newStreamIterator(&logproto.Stream{
		Entries: entries,
	})
}

func identity(i int64) logproto.Entry {
	return logproto.Entry{
		Timestamp: time.Unix(i, 0),
		Line:      fmt.Sprintf("%d", i),
	}
}

func offset(j int64) generator {
	return func(i int64) logproto.Entry {
		return logproto.Entry{
			Timestamp: time.Unix(i+j, 0),
			Line:      fmt.Sprintf("%d", i+j),
		}
	}
}

func inverse(g generator) generator {
	return func(i int64) logproto.Entry {
		return g(-i)
	}
}
