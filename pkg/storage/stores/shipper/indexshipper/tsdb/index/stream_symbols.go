// SPDX-License-Identifier: AGPL-3.0-only
//
// streamSymbols mirrors the Symbols type in this package but reads through a
// streamenc.DecbufFactory rather than a ByteSlice. Offsets are stored relative
// to the symbol section's file offset (i.e., relative to Decbuf base) so
// lookup can go via BufReader.ResetAt.

package index

import (
	"context"
	"sort"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
)

// streamSymbols is the file-streaming counterpart to Symbols.
type streamSymbols struct {
	factory *streamenc.FilePoolDecbufFactory
	version int
	// symOff is the file offset of the symbols section (the 4-byte length
	// prefix). Decbufs opened at this offset position their cursor at the
	// start of content, i.e. file offset symOff+4.
	symOff int

	// offsets stores the position (relative to Decbuf base = symOff) of
	// every symbolFactor-th symbol. offsets[0] points at the first symbol,
	// i.e. Decbuf position 8 (past the section length prefix at 0 and the
	// count uint32 at 4). Subsequent entries move forward.
	offsets []int
	// seen is the total number of symbols.
	seen int
}

// newStreamSymbols reads through the symbols section once at StreamReader
// construction time, capturing sparse offsets for O(1) lookup with a bounded
// scan.
func newStreamSymbols(ctx context.Context, factory *streamenc.FilePoolDecbufFactory, version, off int) (*streamSymbols, error) {
	d := factory.NewDecbufAtChecked(ctx, off, castagnoliTable)
	if err := d.Err(); err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()

	cnt := d.Be32int()
	if err := d.Err(); err != nil {
		return nil, err
	}

	s := &streamSymbols{
		factory: factory,
		version: version,
		symOff:  off,
		offsets: make([]int, 0, 1+cnt/symbolFactor),
	}
	for d.Err() == nil && s.seen < cnt {
		if s.seen%symbolFactor == 0 {
			s.offsets = append(s.offsets, d.Offset())
		}
		d.SkipUvarintBytes()
		s.seen++
	}
	if err := d.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// Lookup returns the symbol at ordinal o. On V2+ indexes, o is the symbol's
// zero-based ordinal in the symbol table; on V1 it's the file offset of the
// symbol relative to the section base — same convention as the existing
// Symbols type.
func (s *streamSymbols) Lookup(o uint32) (string, error) {
	d := s.factory.NewDecbufAtUnchecked(context.Background(), s.symOff)
	if err := d.Err(); err != nil {
		return "", err
	}
	defer func() { _ = d.Close() }()

	if s.version >= FormatV2 {
		if int(o) >= s.seen {
			return "", errors.Errorf("unknown symbol offset %d", o)
		}
		d.ResetAt(s.offsets[int(o/symbolFactor)])
		for i := o - (o/symbolFactor)*symbolFactor; i > 0; i-- {
			d.SkipUvarintBytes()
		}
	} else {
		d.ResetAt(int(o))
	}
	sym := d.UvarintStr()
	if err := d.Err(); err != nil {
		return "", err
	}
	return sym, nil
}

// ReverseLookup returns the ordinal (V2+) or file offset (V1) of sym. Used
// at reader construction to warm the nameSymbols cache; each call re-opens
// the file, so this is not a query hot path.
func (s *streamSymbols) ReverseLookup(sym string) (uint32, error) {
	if len(s.offsets) == 0 {
		return 0, errors.Errorf("unknown symbol %q - no symbols", sym)
	}
	i := sort.Search(len(s.offsets), func(i int) bool {
		// Any error inside the closure is swallowed by sort.Search; if it
		// hits it's re-surfaced when we do the linear scan below.
		d := s.factory.NewDecbufAtUnchecked(context.Background(), s.symOff)
		defer func() { _ = d.Close() }()
		d.ResetAt(s.offsets[i])
		return d.UvarintStr() > sym
	})
	d := s.factory.NewDecbufAtUnchecked(context.Background(), s.symOff)
	if err := d.Err(); err != nil {
		return 0, err
	}
	defer func() { _ = d.Close() }()

	if i > 0 {
		i--
	}
	d.ResetAt(s.offsets[i])
	res := i * symbolFactor
	var lastSymbol string
	// For V1, we also need to know the position (in bytes remaining) so we
	// can compute the file offset of the found symbol.
	var lastLen int
	for d.Err() == nil && res <= s.seen {
		lastLen = d.Len()
		lastSymbol = d.UvarintStr()
		if lastSymbol >= sym {
			break
		}
		res++
	}
	if err := d.Err(); err != nil {
		return 0, err
	}
	if lastSymbol != sym {
		return 0, errors.Errorf("unknown symbol %q", sym)
	}
	if s.version >= FormatV2 {
		return uint32(res), nil
	}
	// V1: return the file offset of the found symbol. lastLen is the Decbuf
	// remaining length just before reading the found symbol. Total section
	// content length = original 4+content+4 - 4 (CRC) — but for V1 we need
	// absolute file offset of the symbol's varint length prefix, matching
	// the existing Symbols.ReverseLookup semantics. In the ByteSlice version
	// that was `bs.Len() - lastLen`; here the ByteSlice equivalent is the
	// file size, so we return int64(r.size) - int64(lastLen). Callers on V1
	// are legacy — Loki writes V3/V4 today — so we accept the small
	// overhead of not caching r.size here.
	//
	// Note: this branch is untested in Loki CI (no V1 fixtures written by
	// Loki tooling); revisit if V1 support becomes load-bearing.
	return uint32(s.decbufFileLen() - lastLen), nil
}

// decbufFileLen returns the effective ByteSlice length the V1 branch of
// ReverseLookup would have seen. For a streaming reader that value is the
// file size; we fetch it via the factory.
func (s *streamSymbols) decbufFileLen() int {
	// The old Symbols.ReverseLookup used bs.Len(); the byte slice was the
	// whole index file. Match that.
	if size, err := s.factory.FileSize(); err == nil {
		return int(size)
	}
	return 0
}

// Size mirrors Symbols.Size — the on-heap footprint of the sparse offset
// table, in bytes.
func (s *streamSymbols) Size() int { return len(s.offsets) * 8 }

// Iter returns an iterator over every symbol in the section, in order.
func (s *streamSymbols) Iter() StringIter {
	d := s.factory.NewDecbufAtChecked(context.Background(), s.symOff, castagnoliTable)
	if err := d.Err(); err != nil {
		return errStringIter{err: err}
	}
	cnt := d.Be32int()
	if err := d.Err(); err != nil {
		_ = d.Close()
		return errStringIter{err: err}
	}
	return &streamSymbolsIter{
		d:   d,
		cnt: cnt,
	}
}

// streamSymbolsIter is the streaming counterpart to symbolsIter. Consumers
// must eventually stop iterating; the underlying file handle is returned to
// the pool when Err reports the terminal state (either exhaustion or a
// decode error). We keep the interface identical to StringIter so no caller
// changes are needed.
type streamSymbolsIter struct {
	d   streamenc.Decbuf
	cnt int
	cur string
	err error
	// closed guards against double-close from callers that call Next() past
	// the end and then perform manual cleanup.
	closed bool
}

func (it *streamSymbolsIter) Next() bool {
	if it.err != nil || it.cnt <= 0 {
		it.closeOnce()
		return false
	}
	s := it.d.UvarintStr()
	if err := it.d.Err(); err != nil {
		it.err = err
		it.closeOnce()
		return false
	}
	it.cur = s
	it.cnt--
	if it.cnt == 0 {
		it.closeOnce()
	}
	return true
}

func (it *streamSymbolsIter) At() string { return it.cur }
func (it *streamSymbolsIter) Err() error { return it.err }

func (it *streamSymbolsIter) closeOnce() {
	if it.closed {
		return
	}
	it.closed = true
	_ = it.d.Close()
}
