package log

type VectorInt []int64

type VectorString struct {
	Offsets VectorInt
	Lines   []byte
}

// TODO: we might want an interface to support different types of batches. https://github.com/jeschkies/loki/blob/065a34a1afb765e45d15430c143ac522d0308646/pkg/logql/vectorized.go#L54
type Batch struct {
	Timestamps VectorInt
	Entries    VectorString
	Selection  []int
	// TODO: Add selection
}

// Returns the timestamp and line for index i or false
func (b *Batch) Get(i int) (int64, []byte, bool) {
	if i < 0 || i >= len(b.Timestamps) {
		return 0, nil, false
	}

	prevOffset := 0
	if i > 0 {
		prevOffset = int(b.Entries.Offsets[i-1])
	}
	return b.Timestamps[i], b.Entries.Lines[prevOffset:b.Entries.Offsets[i]], true
}

func (b *Batch) Iter(yield func(int64, []byte) bool) {
	prevOffset := 0
	for i, ts := range b.Timestamps {
		if i > 0 {
			prevOffset = int(b.Entries.Offsets[i-1])
		}
		line := b.Entries.Lines[prevOffset:b.Entries.Offsets[i]]
		if !yield(ts, line) {
			return
		}
	}
}

func (b *Batch) Append(ts int64, line []byte) {
	b.Timestamps = append(b.Timestamps, ts)
	b.Entries.Offsets = append(b.Entries.Offsets, int64(len(b.Entries.Lines)))
	b.Entries.Lines = append(b.Entries.Lines, line...)
}
