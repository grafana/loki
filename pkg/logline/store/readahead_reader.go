package store

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/thanos-io/objstore"
)

const (
	// defaultReadAheadSize is the chunk size fetched per GetRange call.
	// Tuned to amortize GCS round-trip latency (~130ms) across many small
	// ReadAt calls during index merging. 32MB keeps memory bounded at
	// ~64MB × source_count (2 slots) while reducing request count by ~60x.
	defaultReadAheadSize = 32 << 20 // 32 MB

	// numSlots is the number of buffered chunks kept simultaneously.
	// The index merge alternates reads between two file regions (term blocks
	// and postings blocks), so 2 slots avoid thrashing. A third slot covers
	// the one-time metadata read at open without evicting a hot slot.
	numSlots = 3
)

// slot holds a single buffered chunk.
type slot struct {
	buf   []byte
	start int64 // byte offset of buf[0] in the remote object
	end   int64 // start + len(valid data)
	age   uint64
}

func (s *slot) contains(off, length int64) bool {
	return off >= s.start && off+length <= s.end
}

// readAheadReaderAt wraps a BucketReaderAt with a small set of read-ahead
// buffers. When a ReadAt falls outside all buffered regions, the
// least-recently-used slot is replaced with a new chunk starting at the
// requested offset.
//
// The merge iterator alternates between term blocks and postings blocks at
// different file offsets; keeping multiple slots avoids thrashing that a
// single buffer would cause.
type readAheadReaderAt struct {
	bucket   objstore.Bucket
	path     string
	ctx      context.Context
	fileSize int64
	chunkSz  int64

	mu    sync.Mutex
	slots [numSlots]slot
	clock uint64 // monotonic counter for LRU eviction
}

// NewReadAheadReaderAt returns an io.ReaderAt that prefetches chunks of
// chunkSize bytes from object storage. Subsequent ReadAt calls within the
// same chunk are served from the buffer without network I/O.
// If chunkSize is 0, defaultReadAheadSize is used.
func NewReadAheadReaderAt(ctx context.Context, bucket objstore.Bucket, path string, fileSize int64, chunkSize int) io.ReaderAt {
	cs := int64(chunkSize)
	if cs <= 0 {
		cs = defaultReadAheadSize
	}
	r := &readAheadReaderAt{
		bucket:   bucket,
		path:     path,
		ctx:      ctx,
		fileSize: fileSize,
		chunkSz:  cs,
	}
	// Mark all slots as empty.
	for i := range r.slots {
		r.slots[i].start = -1
		r.slots[i].end = -1
	}
	return r
}

func (r *readAheadReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	need := int64(len(p))

	// Request larger than chunk size: direct read, no buffering.
	if need > r.chunkSz {
		return r.directRead(p, off)
	}

	// Check all slots for a hit.
	for i := range r.slots {
		if r.slots[i].contains(off, need) {
			r.clock++
			r.slots[i].age = r.clock
			copy(p, r.slots[i].buf[off-r.slots[i].start:])
			return len(p), nil
		}
	}

	// Miss: evict the LRU slot and fill it.
	victim := r.lru()
	if err := r.fill(&r.slots[victim], off); err != nil {
		return 0, err
	}

	s := &r.slots[victim]
	avail := s.end - off
	if avail <= 0 {
		return 0, io.EOF
	}
	n := min(need, avail)
	copy(p, s.buf[off-s.start:off-s.start+n])
	if n < need {
		return int(n), io.EOF
	}
	return int(n), nil
}

// lru returns the index of the least-recently-used slot.
func (r *readAheadReaderAt) lru() int {
	minAge := r.slots[0].age
	minIdx := 0
	for i := 1; i < numSlots; i++ {
		if r.slots[i].age < minAge {
			minAge = r.slots[i].age
			minIdx = i
		}
	}
	return minIdx
}

// fill fetches a chunk starting at off into the given slot.
func (r *readAheadReaderAt) fill(s *slot, off int64) error {
	r.clock++
	s.age = r.clock

	end := min(off+r.chunkSz, r.fileSize)
	length := end - off
	if length <= 0 {
		s.start = off
		s.end = off
		return nil
	}

	rc, err := r.bucket.GetRange(r.ctx, r.path, off, length)
	if err != nil {
		return fmt.Errorf("readahead fill %s offset=%d len=%d: %w", r.path, off, length, err)
	}
	defer rc.Close()

	// Reuse buffer capacity when possible.
	if int64(cap(s.buf)) >= length {
		s.buf = s.buf[:length]
	} else {
		s.buf = make([]byte, length)
	}

	n, err := io.ReadFull(rc, s.buf)
	s.start = off
	s.end = off + int64(n)
	// EOF/UnexpectedEOF are expected when the chunk extends to or past end of file.
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return nil
	}
	return err
}

// directRead handles reads larger than the chunk size by issuing a single
// GetRange without buffering. Clamps to fileSize to avoid requesting
// bytes past the end of the object.
func (r *readAheadReaderAt) directRead(p []byte, off int64) (int, error) {
	length := int64(len(p))
	if off+length > r.fileSize {
		length = r.fileSize - off
	}
	if length <= 0 {
		return 0, io.EOF
	}
	rc, err := r.bucket.GetRange(r.ctx, r.path, off, length)
	if err != nil {
		return 0, fmt.Errorf("readahead direct read %s offset=%d len=%d: %w", r.path, off, length, err)
	}
	defer rc.Close()
	n, err := io.ReadFull(rc, p[:length])
	if err != nil {
		return n, err
	}
	if int64(n) < int64(len(p)) {
		return n, io.EOF
	}
	return n, nil
}
