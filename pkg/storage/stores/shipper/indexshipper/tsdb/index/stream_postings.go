package index

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"sort"

	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/pool"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
)

// minPooledPostingsListBytes is the size below which a postings list's buffer
// is not pooled.
// Benchmarks show that a pool Get and Put cost more than they save for short
// postings lists.
const minPooledPostingsListBytes = 256

// postingsListBufferPool holds reusable []byte buffers for streaming postings lists.
var postingsListBufferPool = pool.New(minPooledPostingsListBytes, 16<<20, 2, func(sz int) interface{} {
	return make([]byte, 0, sz)
})

// getPostingsListBuffer returns a buffer of exactly n bytes.
// Buffers are returned dirty, so the caller is expected to overwrite any data in the buffer.
func getPostingsListBuffer(n int) []byte {
	if n < minPooledPostingsListBytes {
		return make([]byte, n)
	}
	return postingsListBufferPool.Get(n).([]byte)[:n]
}

func putPostingsListBuffer(buf []byte) {
	if len(buf) >= minPooledPostingsListBytes {
		postingsListBufferPool.Put(buf)
	}
}

func newPostingsListPostings(buf []byte) Postings {
	if len(buf) >= minPooledPostingsListBytes {
		return newPooledBigEndianPostings(buf)
	}
	return NewBigEndianPostings(buf)
}

// streamPostings is StreamReader's equivalent of ByteSliceReader's postings.
//
// It is used to read the postings-offset table of an index.
// On creation it builds a sparse offset table keeping every label name but only
// every symbolFactor-th value (plus the first and last per name).
// A postings lookup then binary-searches the sparse table and walks forward
// from there through the offset table to the target value(s).
type streamPostings struct {
	factory *streamenc.FilePoolDecbufFactory
	// off is the absolute file offset of the postings offset table.
	off int
	// postings maps a label name to its retained sparse postings offset table entries.
	postings map[string][]streamPostingOffset
}

type streamPostingOffset struct {
	labelValue string
	offset     int // relative offset of this entry within the postings offset table
}

// newStreamPostings scans the postings-offset table once, validating its CRC
// and capturing the sparse offset table.
func newStreamPostings(ctx context.Context, factory *streamenc.FilePoolDecbufFactory, off int) (*streamPostings, error) {
	p := &streamPostings{
		factory:  factory,
		off:      off,
		postings: map[string][]streamPostingOffset{},
	}
	if err := p.build(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *streamPostings) build(ctx context.Context) error {
	var (
		lastName   string
		lastValue  []byte // the previous entry's value, in a buffer every entry reuses
		haveLast   bool
		lastOff    int
		valueCount int
	)
	err := streamPostingsOffsetTable(ctx, p.factory, p.off, func(labelName, labelValue []byte, _ uint64, entryOffset int) error {
		if lastName != string(labelName) {
			// New label name
			if haveLast {
				// Always include the last value for the previous label name
				p.postings[lastName] = append(p.postings[lastName], streamPostingOffset{labelValue: string(lastValue), offset: lastOff})
				haveLast = false
			}
			lastName = string(labelName)
			valueCount = 0
		}
		if valueCount%symbolFactor == 0 {
			p.postings[lastName] = append(p.postings[lastName], streamPostingOffset{labelValue: string(labelValue), offset: entryOffset})
			haveLast = false
		} else {
			lastOff = entryOffset
			// This value is kept only if it turns out to be the last of its label name.
			// Copying it into a reused buffer defers allocating the string until we know it will be kept.
			lastValue = append(lastValue[:0], labelValue...)
			haveLast = true
		}
		valueCount++
		return nil
	})
	if err != nil {
		return err
	}
	if haveLast {
		p.postings[lastName] = append(p.postings[lastName], streamPostingOffset{labelValue: string(lastValue), offset: lastOff})
	}
	// Trim any extra space in the slices
	for k, v := range p.postings {
		trimmedSlice := make([]streamPostingOffset, len(v))
		copy(trimmedSlice, v)
		p.postings[k] = trimmedSlice
	}
	return nil
}

// isLabelName reports whether the given name is a label name held in the postings offset table.
func (p *streamPostings) isLabelName(name []byte) bool {
	if len(name) == 0 { // in postings, this is where the all-postings entry is stored, not a real label name
		return false
	}
	_, ok := p.postings[string(name)]
	return ok
}

// postingsFor returns a merged postings iterator over the series matching the
// given label name and values.
// It mirrors ByteSliceReader.Postings.
// Values must be provided in ascending order.
func (p *streamPostings) postingsFor(labelName string, labelValues ...string) (Postings, error) {
	ctx := context.Background()

	if len(labelValues) == 0 {
		return EmptyPostings(), nil
	}

	offsets, ok := p.postings[labelName]
	if !ok {
		return EmptyPostings(), nil
	}

	valueIndex := 0
	for valueIndex < len(labelValues) && labelValues[valueIndex] < offsets[0].labelValue {
		// Discard values before the start
		valueIndex++
	}
	if valueIndex == len(labelValues) || labelValues[valueIndex] > offsets[len(offsets)-1].labelValue {
		// If there are no values left, return early to avoid unnecessary IO
		return EmptyPostings(), nil
	}

	// Postings lists are read through one decbuf ranging over the whole file
	// so that it can benefit from the FileReader's read-ahead.
	// Opening it is deferred to the point that we need it, so that a lookup
	// which matches nothing doesn't pay the cost of opening a decbuf.
	var postingsDecbuf streamenc.Decbuf
	postingsDecbufOpen := false
	defer func() { _ = postingsDecbuf.Close() }()

	// Unchecked because we already checked CRC32 at startup
	offsetTableDecbuf := p.factory.NewDecbufAtUnchecked(ctx, p.off)
	defer func() { _ = offsetTableDecbuf.Close() }()
	if err := offsetTableDecbuf.Err(); err != nil {
		return nil, err
	}

	results := make([]Postings, 0, len(labelValues))
	skip := 0
	for valueIndex < len(labelValues) {
		targetLabelValue := labelValues[valueIndex]

		i := sort.Search(len(offsets), func(i int) bool { return offsets[i].labelValue >= targetLabelValue })
		if i == len(offsets) {
			// We're past the end
			break
		}
		if i > 0 && offsets[i].labelValue != targetLabelValue {
			// Need to look from the previous entry
			i--
		}

		offsetTableDecbuf.ResetAt(offsets[i].offset)

		// Iterate the offset table entries from the sparse position forward
		for offsetTableDecbuf.Err() == nil {
			if skip == 0 {
				// These are always the same number of bytes,
				// and it's faster to skip than parse.
				skip = offsetTableDecbuf.Len()
				offsetTableDecbuf.Uvarint()          // Key count
				offsetTableDecbuf.SkipUvarintBytes() // Label name
				skip -= offsetTableDecbuf.Len()
			} else {
				offsetTableDecbuf.Skip(skip)
			}
			currentLabelValue := offsetTableDecbuf.UvarintStr() // Label value
			postingsOffset := offsetTableDecbuf.Uvarint64()     // Absolute file offset to postings entry
			if err := offsetTableDecbuf.Err(); err != nil {
				return nil, err
			}
			for currentLabelValue >= targetLabelValue {
				if currentLabelValue == targetLabelValue {
					if !postingsDecbufOpen {
						postingsDecbuf = p.factory.NewRawDecbuf(ctx)
						postingsDecbufOpen = true
						if err := postingsDecbuf.Err(); err != nil {
							return nil, err
						}
					}
					postings, err := p.readPostingsList(&postingsDecbuf, postingsOffset)
					if err != nil {
						return nil, fmt.Errorf("decode postings: %w", err)
					}
					results = append(results, postings)
				}
				valueIndex++
				if valueIndex == len(labelValues) {
					break
				}
				targetLabelValue = labelValues[valueIndex]
			}
			if i+1 == len(offsets) || targetLabelValue >= offsets[i+1].labelValue || valueIndex == len(labelValues) {
				// Need to go to a later sparse entry, if there is one
				break
			}
		}
		if err := offsetTableDecbuf.Err(); err != nil {
			return nil, fmt.Errorf("get postings offset entry: %w", err)
		}
	}

	return Merge(results...), nil
}

// labelValuesFor returns every value stored for the given label name, in the
// ascending order they appear in the offset table.
// It mirrors ByteSliceReader.LabelValues.
func (p *streamPostings) labelValuesFor(labelName string) ([]string, error) {
	offsets, ok := p.postings[labelName]
	if !ok || len(offsets) == 0 {
		return nil, nil
	}
	labelValues := make([]string, 0, len(offsets)*symbolFactor)

	// Unchecked because we already checked CRC32 at startup
	decbuf := p.factory.NewDecbufAtUnchecked(context.Background(), p.off)
	defer decbuf.Close()
	if err := decbuf.Err(); err != nil {
		return nil, err
	}

	// The sparse table always retains a name's first and last value, so walking
	// forward from the first entry until the last value turns up covers every
	// value of this name and stops before the next name's entries.
	decbuf.ResetAt(offsets[0].offset)
	lastLabelValue := offsets[len(offsets)-1].labelValue

	skip := 0
	for decbuf.Err() == nil {
		if skip == 0 {
			// These are always the same number of bytes,
			// and it's faster to skip than parse.
			skip = decbuf.Len()
			decbuf.Uvarint()          // Key count
			decbuf.SkipUvarintBytes() // Label name
			skip -= decbuf.Len()
		} else {
			decbuf.Skip(skip)
		}
		currentLabelValue := decbuf.UvarintStr() // Label value
		if err := decbuf.Err(); err != nil {
			return nil, fmt.Errorf("get postings offset entry: %w", err)
		}
		labelValues = append(labelValues, currentLabelValue)
		if currentLabelValue == lastLabelValue {
			break
		}
		decbuf.Uvarint64() // Absolute file offset to postings entry
	}
	if err := decbuf.Err(); err != nil {
		return nil, fmt.Errorf("get postings offset entry: %w", err)
	}
	return labelValues, nil
}

// labelNames returns the sorted label names held in the offset table.
// It mirrors ByteSliceReader.LabelNames.
func (p *streamPostings) labelNames() []string {
	labelNames := make([]string, 0, len(p.postings))
	for name := range p.postings {
		if name == allPostingsKey.Name {
			// This isn't from any log.
			continue
		}
		labelNames = append(labelNames, name)
	}
	sort.Strings(labelNames)
	return labelNames
}

// readPostingsList reads the postings list stored at absolute file offset
// postingsOffset into memory and wraps it in a BigEndianPostings.
// decbuf is a raw Decbuf over the whole file, which is left positioned after
// the list.
// On disk, the list is a 4-byte big-endian content length, then the content:
// a 4-byte big-endian count N followed by N contiguous 4-byte big-endian series
// refs. Then comes a CRC32 of the content, which is checked here.
func (p *streamPostings) readPostingsList(decbuf *streamenc.Decbuf, postingsOffset uint64) (Postings, error) {
	decbuf.ResetAt(int(postingsOffset))
	contentLen := decbuf.Be32()
	n := decbuf.Be32()
	if err := decbuf.Err(); err != nil {
		return nil, err
	}
	// The count is read before the CRC is checked, so make sure it agrees with
	// the length and fits in the file before allocating for it.
	if uint64(contentLen) != 4+4*uint64(n) || 4*uint64(n)+crc32.Size > uint64(decbuf.Len()) {
		return nil, fmt.Errorf("postings list at %d: length %d for %d refs: %w", postingsOffset, contentLen, n, streamenc.ErrInvalidSize)
	}

	buf := getPostingsListBuffer(4 * int(n))
	decbuf.ReadInto(buf)
	expectedChecksum := decbuf.Be32()
	if err := decbuf.Err(); err != nil {
		putPostingsListBuffer(buf)
		return nil, err
	}

	var count [4]byte
	binary.BigEndian.PutUint32(count[:], n)
	crc := crc32.Update(0, castagnoliTable, count[:])
	if crc32.Update(crc, castagnoliTable, buf) != expectedChecksum {
		putPostingsListBuffer(buf)
		return nil, fmt.Errorf("postings list at %d: %w", postingsOffset, streamenc.ErrInvalidChecksum)
	}
	return newPostingsListPostings(buf), nil
}

// pooledBigEndianPostings wraps BigEndianPostings so the backing []byte
// buffer is returned to postingsListBufferPool once iteration drains (Next
// or Seek returns false).
// Callers that abandon the iterator mid-scan lose the pool benefit for
// that buffer but are otherwise unaffected — GC still reclaims it.
type pooledBigEndianPostings struct {
	BigEndianPostings
	buf    []byte // the buffer that we will return to the pool
	pooled bool   // tracks whether we've already returned the buffer to the pool
}

func newPooledBigEndianPostings(list []byte) *pooledBigEndianPostings {
	return &pooledBigEndianPostings{
		BigEndianPostings: BigEndianPostings{list: list},
		buf:               list,
		pooled:            true,
	}
}

func (p *pooledBigEndianPostings) Next() bool {
	ok := p.BigEndianPostings.Next()
	if !ok {
		p.release()
	}
	return ok
}

func (p *pooledBigEndianPostings) Seek(x storage.SeriesRef) bool {
	ok := p.BigEndianPostings.Seek(x)
	if !ok {
		p.release()
	}
	return ok
}

func (p *pooledBigEndianPostings) release() {
	if !p.pooled {
		return
	}
	p.pooled = false
	putPostingsListBuffer(p.buf)
}

// streamPostingsOffsetTable iterates the postings-offset table, running
// perEntryFn against each entry in the table.
// It is the streaming equivalent of ReadOffsetTable.
//
// labelName and labelValue are valid only for the duration of the perEntryFn
// call they are passed to: they each point into a scratch buffer that is
// overwritten with each call.
// A callback that keeps either must copy it, which converting to a string does.
// Since we only need to keep a small proportion of the names/values, this is
// a significant saving on allocations.
func streamPostingsOffsetTable(
	ctx context.Context,
	factory *streamenc.FilePoolDecbufFactory,
	postingsOffsetTableOffset int,
	perEntryFn func(
		labelName, labelValue []byte,
		postingsOffset uint64, // Absolute file offset
		entryOffset int, // Relative offset of this entry within the postings offset table
	) error,
) error {
	decbuf := factory.NewDecbufAtChecked(ctx, postingsOffsetTableOffset, castagnoliTable)
	defer decbuf.Close()
	if err := decbuf.Err(); err != nil {
		return err
	}

	var labelName []byte
	var labelValue []byte

	size := decbuf.Be32()
	for i := uint32(0); decbuf.Err() == nil && i < size; i++ {
		entryOffset := decbuf.Offset()
		if keyCount := decbuf.Uvarint(); keyCount != 2 {
			return fmt.Errorf("unexpected number of keys for postings offset table %d", keyCount)
		}
		labelName = append(labelName[:0], decbuf.UnsafeUvarintBytes()...)
		labelValue = append(labelValue[:0], decbuf.UnsafeUvarintBytes()...)
		postingsOffset := decbuf.Uvarint64()
		if err := decbuf.Err(); err != nil {
			return err
		}
		if err := perEntryFn(labelName, labelValue, postingsOffset, entryOffset); err != nil {
			return err
		}
	}
	return decbuf.Err()
}
