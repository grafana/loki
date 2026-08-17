package index

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
)

// streamSymbols is StreamReader's equivalent of Symbols.
//
// It is used to read the symbols section of an index.
// On creation builds a sparse offset table capturing the position of every
// symbolFactor-th symbol.
// A symbol look-up then skips to a location specified by the sparse offset table
// and walks from there through up to symbolFactor symbols to the target symbol.
type streamSymbols struct {
	factory *streamenc.FilePoolDecbufFactory
	// off is the absolute file offset of the symbols section.
	off int
	// offsets contains the offset of each of every symbolFactor-th symbol relative to off.
	offsets []int
	// size is the total number of symbols in the section.
	size int
}

// newStreamSymbols scans the symbol section once, validating its CRC and
// capturing the sparse offset table.
func newStreamSymbols(ctx context.Context, factory *streamenc.FilePoolDecbufFactory, off int) (*streamSymbols, error) {
	decbuf := factory.NewDecbufAtChecked(ctx, off, castagnoliTable)
	if err := decbuf.Err(); err != nil {
		_ = decbuf.Close()
		return nil, err
	}
	defer func() { _ = decbuf.Close() }()

	size := decbuf.Be32int()
	if err := decbuf.Err(); err != nil {
		return nil, err
	}
	// Construct the sparse offset table
	s := &streamSymbols{
		factory: factory,
		off:     off,
		size:    size,
		offsets: make([]int, 0, 1+size/symbolFactor),
	}
	for i := 0; decbuf.Err() == nil && i < size; i++ {
		if i%symbolFactor == 0 {
			s.offsets = append(s.offsets, decbuf.Offset())
		}
		decbuf.SkipUvarintBytes()
	}
	if err := decbuf.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *streamSymbols) Lookup(n uint32) (string, error) {
	if int(n) >= s.size {
		return "", fmt.Errorf("unknown symbol offset %d", n)
	}

	decbuf := s.factory.NewDecbufAtUnchecked(context.Background(), s.off)
	if err := decbuf.Err(); err != nil {
		return "", err
	}
	defer func() { _ = decbuf.Close() }()

	// Use sparse offset table to jump right to the start of the bucket the symbol is in.
	decbuf.ResetAt(s.offsets[int(n/symbolFactor)])
	// Walk until we find the one we want.
	for i := n - (n / symbolFactor * symbolFactor); i > 0; i-- {
		decbuf.SkipUvarintBytes()
	}

	symbol := decbuf.UvarintStr()
	if err := decbuf.Err(); err != nil {
		return "", err
	}
	return symbol, nil
}
