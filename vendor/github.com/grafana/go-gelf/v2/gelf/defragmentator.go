package gelf

import (
	"bytes"
	"time"
)

// defragmentator provides GELF message de-chunking.
type defragmentator struct {
	deadline   time.Time
	chunksData [][]byte
	processed  int
	totalBytes int
}

// newDefragmentator returns empty defragmentator with maximum message size and duration
// specified.
func newDefragmentator(timeout time.Duration) (res *defragmentator) {
	res = new(defragmentator)
	res.deadline = time.Now().Add(timeout)
	return
}

// bytes returns message bytes, not nessesarily fully defragmentated.
func (a *defragmentator) bytes() []byte {
	return bytes.Join(a.chunksData, nil)
}

// expired returns true if first chunk is too old.
func (a *defragmentator) expired() bool {
	return time.Now().After(a.deadline)
}

// update feeds the byte chunk to defragmentator, returns true when the message is
// complete.
func (a *defragmentator) update(chunk []byte) bool {
	// each message contains index of current chunk and total count of chunks
	chunkIndex, count := int(chunk[10]), int(chunk[11])
	if a.chunksData == nil {
		a.chunksData = make([][]byte, count)
	}
	if count != len(a.chunksData) || chunkIndex >= count {
		return false
	}
	body := chunk[chunkedHeaderLen:]
	if a.chunksData[chunkIndex] == nil {
		chunkData := make([]byte, 0, len(body))
		a.totalBytes += len(body)
		a.chunksData[chunkIndex] = append(chunkData, body...)
		a.processed++
	}
	return a.processed == len(a.chunksData)
}
