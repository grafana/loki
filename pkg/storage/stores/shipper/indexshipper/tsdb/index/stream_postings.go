package index

import (
	"context"
	"fmt"
	"sort"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
)

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
		lastName, lastValue string
		haveLast            bool
		lastOff             int
		valueCount          int
	)
	err := streamPostingsOffsetTable(ctx, p.factory, p.off, func(labelName, labelValue string, _ uint64, entryOffset int) error {
		if _, ok := p.postings[labelName]; !ok {
			// New label name
			p.postings[labelName] = []streamPostingOffset{}
			if haveLast {
				// Always include the last value for the previous label name
				p.postings[lastName] = append(p.postings[lastName], streamPostingOffset{labelValue: lastValue, offset: lastOff})
			}
			valueCount = 0
		}
		if valueCount%symbolFactor == 0 {
			p.postings[labelName] = append(p.postings[labelName], streamPostingOffset{labelValue: labelValue, offset: entryOffset})
			haveLast = false
		} else {
			lastName, lastValue, lastOff = labelName, labelValue, entryOffset
			haveLast = true
		}
		valueCount++
		return nil
	})
	if err != nil {
		return err
	}
	if haveLast {
		p.postings[lastName] = append(p.postings[lastName], streamPostingOffset{labelValue: lastValue, offset: lastOff})
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
func (p *streamPostings) isLabelName(name string) bool {
	if name == "" { // in postings, this is where the all-postings entry is stored, not a real label name
		return false
	}
	_, ok := p.postings[name]
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

	results := make([]Postings, 0, len(labelValues))
	skip := 0
	valueIndex := 0
	for valueIndex < len(labelValues) && labelValues[valueIndex] < offsets[0].labelValue {
		// Discard values before the start
		valueIndex++
	}
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

		// Unchecked because we already checked CRC32 at startup
		decbuf := p.factory.NewDecbufAtUnchecked(ctx, p.off)
		if err := decbuf.Err(); err != nil {
			_ = decbuf.Close()
			return nil, err
		}
		decbuf.ResetAt(offsets[i].offset)

		// Iterate the offset table entries from the sparse position forward
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
			postingsOffset := decbuf.Uvarint64()     // Absolute file offset to postings entry
			if err := decbuf.Err(); err != nil {
				_ = decbuf.Close()
				return nil, err
			}
			for currentLabelValue >= targetLabelValue {
				if currentLabelValue == targetLabelValue {
					postings, err := p.readPostingsList(postingsOffset)
					if err != nil {
						_ = decbuf.Close()
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
		if err := decbuf.Err(); err != nil {
			_ = decbuf.Close()
			return nil, fmt.Errorf("get postings offset entry: %w", err)
		}
		_ = decbuf.Close()
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
	if err := decbuf.Err(); err != nil {
		return nil, err
	}
	defer func() { _ = decbuf.Close() }()

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
// On disk, the list is a 4-byte big-endian count N followed by N contiguous
// 4-byte big-endian series refs (then the section CRC, which NewDecbufAtChecked
// validates while opening).
func (p *streamPostings) readPostingsList(postingsOffset uint64) (Postings, error) {
	decbuf := p.factory.NewDecbufAtChecked(context.Background(), int(postingsOffset), castagnoliTable)
	defer func() { _ = decbuf.Close() }()
	if err := decbuf.Err(); err != nil {
		return nil, err
	}

	n := decbuf.Be32int()
	if err := decbuf.Err(); err != nil {
		return nil, err
	}

	buf := make([]byte, 4*n)
	decbuf.ReadInto(buf)
	if err := decbuf.Err(); err != nil {
		return nil, err
	}
	return NewBigEndianPostings(buf), nil
}

// streamPostingsOffsetTable iterates the postings-offset table, running
// perEntryFn against each entry in the table.
// It is the streaming equivalent of ReadOffsetTable.
func streamPostingsOffsetTable(
	ctx context.Context,
	factory *streamenc.FilePoolDecbufFactory,
	postingsOffsetTableOffset int,
	perEntryFn func(
		labelName, labelValue string,
		postingsOffset uint64, // Absolute file offset
		entryOffset int, // Relative offset of this entry within the postings offset table
	) error,
) error {
	decbuf := factory.NewDecbufAtChecked(ctx, postingsOffsetTableOffset, castagnoliTable)
	defer func() { _ = decbuf.Close() }()
	if err := decbuf.Err(); err != nil {
		return err
	}

	size := decbuf.Be32()
	for i := uint32(0); decbuf.Err() == nil && i < size; i++ {
		entryOffset := decbuf.Offset()
		if keyCount := decbuf.Uvarint(); keyCount != 2 {
			return fmt.Errorf("unexpected number of keys for postings offset table %d", keyCount)
		}
		labelName := decbuf.UvarintStr()
		labelValue := decbuf.UvarintStr()
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
