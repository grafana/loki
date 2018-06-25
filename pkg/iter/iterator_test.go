package iter

import (
	"testing"
	"time"

	"github.com/grafana/logish/pkg/logproto"
	"github.com/stretchr/testify/assert"
)

const testSize = 100

func TestStreamIterator(t *testing.T) {
	iterator := mkStreamIterator(testSize, func(i int64) logproto.Entry {
		return logproto.Entry{
			Timestamp: time.Unix(-i, 0),
		}
	})
	testIterator(t, iterator, testSize, logproto.BACKWARD)
}

func TestHeapIteratorBackward(t *testing.T) {
	iterators := []EntryIterator{}
	for i := int64(0); i < 4; i++ {
		iterators = append(iterators, mkStreamIterator(testSize/4, func(j int64) logproto.Entry {
			return logproto.Entry{
				Timestamp: time.Unix(-j*4-i, 0),
			}
		}))
	}
	testIterator(t, NewHeapIterator(iterators, logproto.BACKWARD), testSize, logproto.BACKWARD)
}

func TestHeapIteratorForward(t *testing.T) {
	iterators := []EntryIterator{}
	for i := int64(0); i < 4; i++ {
		iterators = append(iterators, mkStreamIterator(testSize/4, func(j int64) logproto.Entry {
			return logproto.Entry{
				Timestamp: time.Unix(j*4+i, 0),
			}
		}))
	}
	testIterator(t, NewHeapIterator(iterators, logproto.FORWARD), testSize, logproto.FORWARD)
}

func mkStreamIterator(numEntries int64, f func(i int64) logproto.Entry) EntryIterator {
	entries := []logproto.Entry{}
	for i := int64(0); i < numEntries; i++ {
		entries = append(entries, f(i))
	}
	return newStreamIterator(&logproto.Stream{
		Entries: entries,
	})
}

func testIterator(t *testing.T, iterator EntryIterator,
	testSize int64, direction logproto.Direction) {
	i := int64(0)
	for ; i < testSize && iterator.Next(); i++ {
		switch direction {
		case logproto.BACKWARD:
			assert.Equal(t, -i, iterator.Entry().Timestamp.Unix())
		case logproto.FORWARD:
			assert.Equal(t, i, iterator.Entry().Timestamp.Unix())
		default:
			panic(direction)
		}
	}
	assert.Equal(t, i, int64(testSize))
	assert.NoError(t, iterator.Error())
	assert.NoError(t, iterator.Close())
}
