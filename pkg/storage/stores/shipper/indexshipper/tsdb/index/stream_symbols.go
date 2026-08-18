package index

import (
	"context"
	"fmt"
	"sync"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
)

const labelValueSymbolsCacheSize = 1024

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
	// labelNameSymbols caches label name symbols by ordinal, mirroring
	// ByteSliceReader.nameSymbols.
	// There are not many label names compared to label values, and they
	// make up half of all lookups, so holding them in memory is a good trade-off.
	labelNameSymbols map[uint32]string

	// labelValueSymbolsCache is a direct cache of label value symbols,
	// keyed on the low bits of the ordinal.
	// An entry answers for ordinal n only when it holds n and a non-empty
	// symbol, so the zero value is a miss and a collision costs a re-read
	// rather than the wrong symbol.
	labelValueSymbolsMtx   sync.Mutex
	labelValueSymbolsCache [labelValueSymbolsCacheSize]struct {
		ordinal uint32
		symbol  string
	}
}

// newStreamSymbols scans the symbol section once, validating its CRC,
// capturing the sparse offset table, and building labelNameSymbols.
func newStreamSymbols(ctx context.Context, factory *streamenc.FilePoolDecbufFactory, off int, isLabelName func(symbol string) bool) (*streamSymbols, error) {
	decbuf := factory.NewDecbufAtChecked(ctx, off, castagnoliTable)
	defer decbuf.Close()
	if err := decbuf.Err(); err != nil {
		return nil, err
	}

	size := decbuf.Be32int()
	if err := decbuf.Err(); err != nil {
		return nil, err
	}
	// Construct the sparse offset table
	s := &streamSymbols{
		factory:          factory,
		off:              off,
		size:             size,
		offsets:          make([]int, 0, 1+size/symbolFactor),
		labelNameSymbols: map[uint32]string{},
	}
	for i := 0; decbuf.Err() == nil && i < size; i++ {
		if i%symbolFactor == 0 {
			s.offsets = append(s.offsets, decbuf.Offset())
		}
		symbol := decbuf.UvarintStr()
		if isLabelName(symbol) {
			s.labelNameSymbols[uint32(i)] = symbol
		}
	}
	if err := decbuf.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// Lookup resolves ordinal n either from a file or a local cache.
func (s *streamSymbols) Lookup(n uint32) (string, error) {
	if symbol, ok := s.labelNameSymbols[n]; ok {
		return symbol, nil
	}

	if int(n) >= s.size {
		return "", fmt.Errorf("unknown symbol offset %d", n)
	}

	cacheIndex := n % labelValueSymbolsCacheSize
	s.labelValueSymbolsMtx.Lock()
	if entry := s.labelValueSymbolsCache[cacheIndex]; entry.ordinal == n && entry.symbol != "" {
		s.labelValueSymbolsMtx.Unlock()
		return entry.symbol, nil
	}
	s.labelValueSymbolsMtx.Unlock()

	symbol, err := s.lookup(n)
	if err != nil {
		return "", err
	}

	s.labelValueSymbolsMtx.Lock()
	s.labelValueSymbolsCache[cacheIndex].ordinal = n
	s.labelValueSymbolsCache[cacheIndex].symbol = symbol
	s.labelValueSymbolsMtx.Unlock()

	return symbol, nil
}

// lookup resolves ordinal n from the file.
func (s *streamSymbols) lookup(n uint32) (string, error) {
	decbuf := s.factory.NewDecbufAtUnchecked(context.Background(), s.off)
	defer decbuf.Close()
	if err := decbuf.Err(); err != nil {
		return "", err
	}

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
