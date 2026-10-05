package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util"
)

// V5 and V6 keep the V4 chunk container (header, structured metadata symbols,
// block metas, checksums) and change only what a block holds.
//
// A V5 block, before compression with the chunk encoding:
//
//	uvarint numEntries
//	uvarint linesLen
//	lines          every entry's line, back to back
//	entry fields   see below
//
// A V6 block is stored as is. Only its token ids and entry fields are
// compressed:
//
//	uvarint numEntries
//	uvarint numTokens
//	uvarint len, token lengths (one uvarint per token)
//	uvarint len, dictionary (the tokens back to back, most frequent first)
//	uvarint linesLen
//	uvarint idsLen, uvarint len, compressed token ids (one uvarint per token)
//	uvarint fieldsLen, uvarint len, compressed entry fields
//
// The lines region of a V6 block is cut into tokens: maximal runs of word
// bytes, or maximal runs of other bytes. The token ids rebuild the region.
//
// Entry fields are, for every entry in order, a uvarint line length, then for
// every entry a varint timestamp, then for every entry the V4 structured
// metadata section (uvarint section length, uvarint symbol count, symbol pairs).

var errLayoutBlock = errors.New("invalid layout block")

// LayoutStats counts the work of V5 and V6 block iterators. It exists to
// compare the layouts.
var LayoutStats struct {
	// Blocks is the number of blocks iterated.
	Blocks atomic.Int64
	// BlocksDecompressed is the number of blocks whose compressed data was decompressed.
	BlocksDecompressed atomic.Int64
	// DecompressedBytes is the number of bytes the chunk encoding decompressed.
	DecompressedBytes atomic.Int64
	// DecodedLineBytes is the number of line bytes rebuilt from V6 token ids.
	DecodedLineBytes atomic.Int64
}

// ResetLayoutStats zeroes LayoutStats.
func ResetLayoutStats() {
	LayoutStats.Blocks.Store(0)
	LayoutStats.BlocksDecompressed.Store(0)
	LayoutStats.DecompressedBytes.Store(0)
	LayoutStats.DecodedLineBytes.Store(0)
}

// wordByte marks the bytes of word tokens. Every other byte belongs to
// separator tokens.
var wordByte = func() (t [256]bool) {
	for c := range t {
		t[c] = c >= 0x80 || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return t
}()

// tokenEnd returns the end of the token that starts at b[start].
func tokenEnd(b []byte, start int) int {
	w := wordByte[b[start]]
	end := start + 1
	for end < len(b) && wordByte[b[end]] == w {
		end++
	}
	return end
}

type layoutEntry struct {
	ts   int64
	line []byte
	// metadata is the entry's V4 structured metadata section, length included.
	metadata []byte
}

// ReencodeLayout rewrites every block of a V4 chunk in format, ChunkFormatV5
// or ChunkFormatV6. Entries, block boundaries and structured metadata symbols
// stay as they are.
func ReencodeLayout(c *MemChunk, format byte) error {
	if c.format != ChunkFormatV4 {
		return fmt.Errorf("re-encode chunk format %d: only V4 is supported", c.format)
	}
	if format != ChunkFormatV5 && format != ChunkFormatV6 {
		return fmt.Errorf("re-encode to chunk format %d: only V5 and V6 are supported", format)
	}
	writers := compression.GetWriterPool(c.encoding)
	var entries []layoutEntry
	c.cutBlockSize = 0
	for i, b := range c.blocks {
		entries = entries[:0]
		it := newBufferedIterator(context.Background(), compression.GetReaderPool(c.encoding), b.b, c.format, c.symbolizer)
		for it.Next() {
			entries = append(entries, layoutEntry{
				ts:       it.currTs,
				line:     slices.Clone(it.currLine),
				metadata: appendMetadataSection(nil, it.currSymbols()),
			})
		}
		if err := it.Close(); err != nil {
			return fmt.Errorf("block %d: %w", i, err)
		}
		if len(entries) != b.numEntries {
			return fmt.Errorf("block %d: decoded %d entries, want %d", i, len(entries), b.numEntries)
		}

		var (
			enc    []byte
			rawLen int
			err    error
		)
		if format == ChunkFormatV5 {
			enc, rawLen, err = encodeV5Block(entries, writers)
		} else {
			enc, rawLen, err = encodeV6Block(entries, writers)
		}
		if err != nil {
			return fmt.Errorf("block %d: %w", i, err)
		}
		c.blocks[i].b = enc
		c.blocks[i].uncompressedSize = rawLen
		c.cutBlockSize += len(enc)
	}
	c.format = format
	return nil
}

func appendMetadataSection(dst []byte, syms symbols) []byte {
	section := binary.AppendUvarint(nil, uint64(len(syms)))
	for _, s := range syms {
		section = binary.AppendUvarint(section, uint64(s.Name))
		section = binary.AppendUvarint(section, uint64(s.Value))
	}
	dst = binary.AppendUvarint(dst, uint64(len(section)))
	return append(dst, section...)
}

func appendEntryFields(dst []byte, entries []layoutEntry) []byte {
	for _, e := range entries {
		dst = binary.AppendUvarint(dst, uint64(len(e.line)))
	}
	for _, e := range entries {
		dst = binary.AppendVarint(dst, e.ts)
	}
	for _, e := range entries {
		dst = append(dst, e.metadata...)
	}
	return dst
}

func compressSection(pool compression.WriterPool, raw []byte) ([]byte, error) {
	var out bytes.Buffer
	w := pool.GetWriter(&out)
	defer pool.PutWriter(w)
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func encodeV5Block(entries []layoutEntry, writers compression.WriterPool) ([]byte, int, error) {
	linesLen := 0
	for _, e := range entries {
		linesLen += len(e.line)
	}
	raw := binary.AppendUvarint(nil, uint64(len(entries)))
	raw = binary.AppendUvarint(raw, uint64(linesLen))
	for _, e := range entries {
		raw = append(raw, e.line...)
	}
	raw = appendEntryFields(raw, entries)
	enc, err := compressSection(writers, raw)
	return enc, len(raw), err
}

func encodeV6Block(entries []layoutEntry, writers compression.WriterPool) ([]byte, int, error) {
	linesLen := 0
	for _, e := range entries {
		linesLen += len(e.line)
	}
	lines := make([]byte, 0, linesLen)
	for _, e := range entries {
		lines = append(lines, e.line...)
	}

	// Tokens in order of first use, how often each occurs, and the token
	// sequence that rebuilds lines.
	var (
		position = make(map[string]int, 1024)
		tokens   []string
		counts   []int
		sequence = make([]int32, 0, linesLen/4+1)
	)
	for start := 0; start < len(lines); {
		end := tokenEnd(lines, start)
		k, ok := position[string(lines[start:end])]
		if !ok {
			t := string(lines[start:end])
			k = len(tokens)
			position[t] = k
			tokens = append(tokens, t)
			counts = append(counts, 0)
		}
		counts[k]++
		sequence = append(sequence, int32(k))
		start = end
	}

	// The most frequent tokens get the shortest ids.
	order := make([]int, len(tokens))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return counts[order[a]] > counts[order[b]] })
	id := make([]int, len(tokens))
	var tokenLens, dict []byte
	for rank, k := range order {
		id[k] = rank
		tokenLens = binary.AppendUvarint(tokenLens, uint64(len(tokens[k])))
		dict = append(dict, tokens[k]...)
	}
	ids := make([]byte, 0, len(sequence)*2)
	for _, k := range sequence {
		ids = binary.AppendUvarint(ids, uint64(id[k]))
	}
	fields := appendEntryFields(nil, entries)

	idsEnc, err := compressSection(writers, ids)
	if err != nil {
		return nil, 0, err
	}
	fieldsEnc, err := compressSection(writers, fields)
	if err != nil {
		return nil, 0, err
	}

	out := binary.AppendUvarint(nil, uint64(len(entries)))
	out = binary.AppendUvarint(out, uint64(len(tokens)))
	out = binary.AppendUvarint(out, uint64(len(tokenLens)))
	out = append(out, tokenLens...)
	out = binary.AppendUvarint(out, uint64(len(dict)))
	out = append(out, dict...)
	out = binary.AppendUvarint(out, uint64(len(lines)))
	out = binary.AppendUvarint(out, uint64(len(ids)))
	out = binary.AppendUvarint(out, uint64(len(idsEnc)))
	out = append(out, idsEnc...)
	out = binary.AppendUvarint(out, uint64(len(fields)))
	out = binary.AppendUvarint(out, uint64(len(fieldsEnc)))
	out = append(out, fieldsEnc...)
	return out, len(lines) + len(fields), nil
}

// layoutBuf reads sizes and byte ranges from a block, recording the first error.
type layoutBuf struct {
	b   []byte
	bad bool
}

// size reads a uvarint that must fit in the rest of the block or be a count.
func (d *layoutBuf) size() int {
	if d.bad {
		return 0
	}
	v, n := binary.Uvarint(d.b)
	if n <= 0 || v > uint64(maxLineLength) {
		d.bad = true
		return 0
	}
	d.b = d.b[n:]
	return int(v)
}

func (d *layoutBuf) bytes(n int) []byte {
	if d.bad || n > len(d.b) {
		d.bad = true
		return nil
	}
	b := d.b[:n:n]
	d.b = d.b[n:]
	return b
}

// literalPlan finds a required literal in the lines of V5 and V6 blocks.
type literalPlan struct {
	literal []byte
	index   func([]byte) int

	// runs is the literal cut the way V6 cuts lines into tokens, and runIndex
	// searches for each run. Only set for V6.
	runs     [][]byte
	runIndex []func([]byte) int
}

func newLiteralPlan(literal []byte, format byte) *literalPlan {
	p := &literalPlan{literal: literal, index: log.NewLiteralIndex(literal)}
	if format == ChunkFormatV6 {
		for start := 0; start < len(literal); {
			end := tokenEnd(literal, start)
			p.runs = append(p.runs, literal[start:end])
			if start == 0 && end == len(literal) {
				p.runIndex = append(p.runIndex, p.index)
			} else {
				p.runIndex = append(p.runIndex, log.NewLiteralIndex(literal[start:end]))
			}
			start = end
		}
	}
	return p
}

// candidates appends the entries whose line contains the literal. offs holds
// the start of every line in lines, then the end of the last one. A match that
// runs from one line into the next does not count.
func (p *literalPlan) candidates(lines []byte, offs []int32, dst []int32) []int32 {
	n := len(p.literal)
	e := 0
	for pos := 0; pos+n <= len(lines); {
		k := p.index(lines[pos:])
		if k < 0 {
			break
		}
		start := pos + k
		for int(offs[e+1]) <= start {
			e++
		}
		end := int(offs[e+1])
		if start+n <= end {
			dst = append(dst, int32(e))
		}
		// Any later match starting in this line either is a duplicate or also
		// runs past its end.
		pos = end
	}
	return dst
}

const (
	runInside = iota // the run lies within a token
	runSuffix        // the run ends a token
	runPrefix        // the run starts a token
	runWhole         // the run is a whole token
)

// dictMayContain reports whether a lines region cut into the tokens of dict
// can contain the literal. A literal made of one run must lie inside one
// token. A longer literal must start with the end of a token, go on with whole
// tokens, and finish with the start of a token. When this returns false, no
// line of the block contains the literal.
func (p *literalPlan) dictMayContain(dict, tokenLens []byte) bool {
	last := len(p.runs) - 1
	if last == 0 {
		return p.dictHas(dict, tokenLens, 0, runInside)
	}
	for i := range p.runs {
		kind := runWhole
		switch i {
		case 0:
			kind = runSuffix
		case last:
			kind = runPrefix
		}
		if !p.dictHas(dict, tokenLens, i, kind) {
			return false
		}
	}
	return true
}

// dictHas reports whether run i occurs in some token of dict, placed as kind says.
func (p *literalPlan) dictHas(dict, tokenLens []byte, i, kind int) bool {
	run, index := p.runs[i], p.runIndex[i]
	n := len(run)
	tokStart, tokEnd := 0, 0
	for pos := 0; pos+n <= len(dict); {
		k := index(dict[pos:])
		if k < 0 {
			return false
		}
		start := pos + k
		end := start + n
		for tokEnd <= start {
			l, w := binary.Uvarint(tokenLens)
			if w <= 0 || l == 0 {
				// A malformed dictionary rules nothing out.
				return true
			}
			tokenLens = tokenLens[w:]
			tokStart, tokEnd = tokEnd, tokEnd+int(l)
		}
		var ok bool
		switch kind {
		case runInside:
			ok = end <= tokEnd
		case runSuffix:
			ok = end == tokEnd
		case runPrefix:
			ok = start == tokStart && end <= tokEnd
		case runWhole:
			ok = start == tokStart && end == tokEnd
		}
		if ok {
			return true
		}
		// Skip the starts in this token that cannot place the run as required.
		next := tokEnd
		if kind == runSuffix && end < tokEnd {
			next = max(start+1, tokEnd-n)
		}
		pos = next
	}
	return false
}

// layoutScratch holds the buffers used to decode one block.
type layoutScratch struct {
	reader    bytes.Reader
	raw       []byte // decompressed V5 block, or V6 entry fields
	ids       []byte // decompressed V6 token ids
	lines     []byte // V6 lines rebuilt from token ids
	dict      []byte // V6 dictionary with copy padding
	tokenOffs []int32
	offs      []int32
	ts        []int64
	cand      []int32
	symbols   []symbol
}

var layoutScratchPool = sync.Pool{New: func() any { return &layoutScratch{} }}

func resize(b []byte, n int) []byte {
	if cap(b) < n {
		return make([]byte, n)
	}
	return b[:n]
}

// decompress fills dst, which must have the decompressed length.
func (s *layoutScratch) decompress(pool compression.ReaderPool, src, dst []byte) error {
	s.reader.Reset(src)
	r, err := pool.GetReader(&s.reader)
	if err != nil {
		return err
	}
	defer pool.PutReader(r)
	if _, err := io.ReadFull(r, dst); err != nil {
		return err
	}
	LayoutStats.DecompressedBytes.Add(int64(len(dst)))
	return nil
}

// parseFields reads line offsets and timestamps into s and returns the
// structured metadata sections.
func (s *layoutScratch) parseFields(b []byte, n, linesLen int) ([]byte, error) {
	offs := append(s.offs[:0], 0)
	off := 0
	for range n {
		l, w := binary.Uvarint(b)
		if w <= 0 || l > uint64(linesLen-off) {
			return nil, errLayoutBlock
		}
		b = b[w:]
		off += int(l)
		offs = append(offs, int32(off))
	}
	if off != linesLen {
		return nil, errLayoutBlock
	}
	ts := s.ts[:0]
	for range n {
		t, w := binary.Varint(b)
		if w <= 0 {
			return nil, errLayoutBlock
		}
		b = b[w:]
		ts = append(ts, t)
	}
	s.offs, s.ts = offs, ts
	return b, nil
}

// rebuildLines expands V6 token ids into the lines region.
func (s *layoutScratch) rebuildLines(dict, tokenLens []byte, numTokens int, ids []byte, linesLen int) ([]byte, error) {
	offs := append(s.tokenOffs[:0], 0)
	off := 0
	for range numTokens {
		l, w := binary.Uvarint(tokenLens)
		if w <= 0 || l > uint64(len(dict)-off) {
			return nil, errLayoutBlock
		}
		tokenLens = tokenLens[w:]
		off += int(l)
		offs = append(offs, int32(off))
	}
	s.tokenOffs = offs
	if off != len(dict) {
		return nil, errLayoutBlock
	}
	// Tokens of up to copyPad bytes are copied as one fixed-size move, so the
	// dictionary and the output both carry copyPad bytes of slack.
	pd := resize(s.dict, len(dict)+copyPad)
	copy(pd, dict)
	s.dict = pd
	dst := resize(s.lines[:cap(s.lines)], linesLen+copyPad)
	s.lines = dst
	o := 0
	for i := 0; i < len(ids); {
		var id uint64
		if b := ids[i]; b < 0x80 {
			id = uint64(b)
			i++
		} else {
			v, w := binary.Uvarint(ids[i:])
			if w <= 0 {
				return nil, errLayoutBlock
			}
			id = v
			i += w
		}
		if id >= uint64(numTokens) {
			return nil, errLayoutBlock
		}
		st, en := int(offs[id]), int(offs[id+1])
		n := en - st
		if o+n > linesLen {
			return nil, errLayoutBlock
		}
		if n <= copyPad {
			*(*[copyPad]byte)(dst[o:]) = *(*[copyPad]byte)(pd[st:])
		} else {
			copy(dst[o:], pd[st:en])
		}
		o += n
	}
	if o != linesLen {
		return nil, errLayoutBlock
	}
	return dst[:linesLen:linesLen], nil
}

const copyPad = 16

// layoutCursor walks the entries of a V5 or V6 block. With a plan, it only
// visits entries whose line contains the plan's literal.
type layoutCursor struct {
	stats *stats.Context
	block encBlock
	plan  *literalPlan
	s     *layoutScratch

	loaded bool
	closed bool
	err    error

	n     int
	lines []byte
	sm    []byte // structured metadata sections, starting with entry smIdx
	smIdx int
	next  int // next index into s.cand with a plan, else the next entry

	currTs                 int64
	currLine               []byte
	currStructuredMetadata labels.Labels
}

func (c *layoutCursor) Next() bool {
	if c.closed {
		return false
	}
	if !c.loaded {
		c.loaded = true
		if err := c.load(); err != nil {
			c.err = err
			c.Close()
			return false
		}
	}
	var i int
	if c.plan != nil {
		if c.next >= len(c.s.cand) {
			c.Close()
			return false
		}
		i = int(c.s.cand[c.next])
	} else {
		if c.next >= c.n {
			c.Close()
			return false
		}
		i = c.next
	}
	c.next++

	start, end := c.s.offs[i], c.s.offs[i+1]
	c.currTs = c.s.ts[i]
	c.currLine = c.lines[start:end:end]
	lbls, err := c.structuredMetadata(i)
	if err != nil {
		c.err = err
		c.Close()
		return false
	}
	c.currStructuredMetadata = lbls
	return true
}

func (c *layoutCursor) load() error {
	LayoutStats.Blocks.Add(1)
	c.stats.AddCompressedBytes(int64(len(c.block.b)))
	c.s = layoutScratchPool.Get().(*layoutScratch)
	c.s.cand = c.s.cand[:0]
	readers := compression.GetReaderPool(c.block.enc)

	var fields []byte
	decompressed := 0
	switch c.block.format {
	case ChunkFormatV5:
		raw := resize(c.s.raw, c.block.uncompressedSize)
		c.s.raw = raw
		if err := c.s.decompress(readers, c.block.b, raw); err != nil {
			return err
		}
		decompressed = len(raw)
		d := layoutBuf{b: raw}
		c.n = d.size()
		c.lines = d.bytes(d.size())
		fields = d.b
		if d.bad {
			return errLayoutBlock
		}
	case ChunkFormatV6:
		d := layoutBuf{b: c.block.b}
		c.n = d.size()
		numTokens := d.size()
		tokenLens := d.bytes(d.size())
		dict := d.bytes(d.size())
		linesLen := d.size()
		idsLen := d.size()
		ids := d.bytes(d.size())
		fieldsLen := d.size()
		fieldsEnc := d.bytes(d.size())
		if d.bad {
			return errLayoutBlock
		}
		if c.plan != nil && !c.plan.dictMayContain(dict, tokenLens) {
			c.n = 0
			return nil
		}
		c.s.ids = resize(c.s.ids, idsLen)
		if err := c.s.decompress(readers, ids, c.s.ids); err != nil {
			return err
		}
		c.s.raw = resize(c.s.raw, fieldsLen)
		if err := c.s.decompress(readers, fieldsEnc, c.s.raw); err != nil {
			return err
		}
		lines, err := c.s.rebuildLines(dict, tokenLens, numTokens, c.s.ids, linesLen)
		if err != nil {
			return err
		}
		LayoutStats.DecodedLineBytes.Add(int64(linesLen))
		decompressed = idsLen + fieldsLen
		c.lines = lines
		fields = c.s.raw
	default:
		return fmt.Errorf("unsupported layout chunk format %d", c.block.format)
	}
	LayoutStats.BlocksDecompressed.Add(1)

	sm, err := c.s.parseFields(fields, c.n, len(c.lines))
	if err != nil {
		return err
	}
	c.sm = sm
	c.stats.AddDecompressedBytes(int64(decompressed))
	c.stats.AddDecompressedLines(int64(c.n))
	if c.plan != nil {
		c.s.cand = c.plan.candidates(c.lines, c.s.offs, c.s.cand)
	}
	return nil
}

// structuredMetadata resolves the labels of entry i. Entries must be asked for
// in increasing order.
func (c *layoutCursor) structuredMetadata(i int) (labels.Labels, error) {
	for ; c.smIdx < i; c.smIdx++ {
		l, w := binary.Uvarint(c.sm)
		if w <= 0 || l > uint64(len(c.sm)-w) {
			return labels.EmptyLabels(), errLayoutBlock
		}
		c.sm = c.sm[w+int(l):]
	}
	l, w := binary.Uvarint(c.sm)
	if w <= 0 || l > uint64(len(c.sm)-w) {
		return labels.EmptyLabels(), errLayoutBlock
	}
	section := c.sm[w : w+int(l)]
	c.sm = c.sm[w+int(l):]
	c.smIdx++

	nSymbols, w := binary.Uvarint(section)
	if w <= 0 {
		return labels.EmptyLabels(), errLayoutBlock
	}
	section = section[w:]
	syms := c.s.symbols[:0]
	for range nSymbols {
		name, nw := binary.Uvarint(section)
		if nw <= 0 {
			return labels.EmptyLabels(), errLayoutBlock
		}
		value, vw := binary.Uvarint(section[nw:])
		if vw <= 0 {
			return labels.EmptyLabels(), errLayoutBlock
		}
		section = section[nw+vw:]
		syms = append(syms, symbol{Name: uint32(name), Value: uint32(value)})
	}
	c.s.symbols = syms
	return c.block.symbolizer.Lookup(syms, nil)
}

func (c *layoutCursor) Err() error { return c.err }

func (c *layoutCursor) Close() error {
	if !c.closed {
		c.closed = true
		if c.s != nil {
			layoutScratchPool.Put(c.s)
			c.s = nil
		}
		c.lines, c.sm, c.currLine = nil, nil, nil
		c.currStructuredMetadata = labels.EmptyLabels()
	}
	return c.err
}

func newLayoutEntryIterator(ctx context.Context, b encBlock, pipeline log.StreamPipeline) iter.EntryIterator {
	it := &layoutEntryIterator{
		layoutCursor:   layoutCursor{stats: stats.FromContext(ctx), block: b},
		pipeline:       pipeline,
		skipProcessing: isProcessingDisabled(ctx),
	}
	if it.skipProcessing {
		it.currLabels = pipeline.BaseLabels()
	} else if lit := log.RequiredLiteral(pipeline); lit != nil {
		it.plan = newLiteralPlan(lit, b.format)
	}
	return it
}

type layoutEntryIterator struct {
	layoutCursor
	pipeline       log.StreamPipeline
	skipProcessing bool

	cur        logproto.Entry
	currLabels log.LabelsResult
}

func (e *layoutEntryIterator) Next() bool {
	for e.layoutCursor.Next() {
		if e.skipProcessing {
			e.cur.Timestamp = time.Unix(0, e.currTs)
			e.cur.Line = string(e.currLine)
			e.cur.StructuredMetadata = logproto.FromLabelsToLabelAdapters(e.currStructuredMetadata)
			return true
		}
		newLine, lbs, matches := e.pipeline.Process(e.currTs, e.currLine, e.currStructuredMetadata)
		if !matches {
			continue
		}
		e.stats.AddPostFilterLines(1)
		e.currLabels = lbs
		e.cur.Timestamp = time.Unix(0, e.currTs)
		e.cur.Line = string(newLine)
		e.cur.StructuredMetadata = logproto.FromLabelsToLabelAdapters(lbs.StructuredMetadata())
		e.cur.Parsed = logproto.FromLabelsToLabelAdapters(lbs.Parsed())
		return true
	}
	return false
}

func (e *layoutEntryIterator) At() logproto.Entry { return e.cur }

func (e *layoutEntryIterator) Labels() string { return e.currLabels.String() }

func (e *layoutEntryIterator) StreamHash() uint64 { return e.pipeline.BaseLabels().Hash() }

func (e *layoutEntryIterator) Close() error {
	if e.pipeline.ReferencedStructuredMetadata() {
		e.stats.SetQueryReferencedStructuredMetadata()
	}
	return e.layoutCursor.Close()
}

func newLayoutSampleIterator(ctx context.Context, b encBlock, extractor log.StreamSampleExtractor) iter.SampleIterator {
	if extractor == nil {
		return iter.NoopSampleIterator
	}
	it := &layoutSampleIterator{
		layoutCursor: layoutCursor{stats: stats.FromContext(ctx), block: b},
		extractor:    extractor,
	}
	if lit := log.RequiredSampleLiteral(extractor); lit != nil {
		it.plan = newLiteralPlan(lit, b.format)
	}
	return it
}

type layoutSampleIterator struct {
	layoutCursor
	extractor log.StreamSampleExtractor
	hasher    util.SampleHasher

	curr       logproto.Sample
	currLabels log.LabelsResult
}

func (e *layoutSampleIterator) Next() bool {
	for e.layoutCursor.Next() {
		sample, ok := e.extractor.Process(e.currTs, e.currLine, e.currStructuredMetadata)
		if !ok {
			continue
		}
		e.stats.AddPostFilterLines(1)
		lblString := sample.Labels.String()
		e.currLabels = sample.Labels
		e.curr = logproto.Sample{
			Timestamp: e.currTs,
			Value:     sample.Value,
			Hash:      e.hasher.Hash(lblString, e.currLine),
		}
		return true
	}
	return false
}

func (e *layoutSampleIterator) At() logproto.Sample { return e.curr }

func (e *layoutSampleIterator) Labels() string { return e.currLabels.String() }

func (e *layoutSampleIterator) StreamHash() uint64 { return e.extractor.BaseLabels().Hash() }

func (e *layoutSampleIterator) Close() error {
	if e.extractor.ReferencedStructuredMetadata() {
		e.stats.SetQueryReferencedStructuredMetadata()
	}
	return e.layoutCursor.Close()
}

// LayoutSectionSizes returns the total size of each part of a chunk's blocks,
// by name, to compare layouts.
func LayoutSectionSizes(c *MemChunk) (map[string]int, error) {
	sizes := map[string]int{}
	for _, b := range c.blocks {
		sizes["blocks"] += len(b.b)
		sizes["uncompressed"] += b.uncompressedSize
		switch c.format {
		case ChunkFormatV5:
			raw := make([]byte, b.uncompressedSize)
			var s layoutScratch
			if err := s.decompress(compression.GetReaderPool(c.encoding), b.b, raw); err != nil {
				return nil, err
			}
			d := layoutBuf{b: raw}
			d.size()
			sizes["lines"] += len(d.bytes(d.size()))
			sizes["fields"] += len(d.b)
		case ChunkFormatV6:
			d := layoutBuf{b: b.b}
			d.size()
			sizes["tokens"] += d.size()
			sizes["token_lengths"] += len(d.bytes(d.size()))
			sizes["dictionary"] += len(d.bytes(d.size()))
			sizes["lines"] += d.size()
			sizes["ids"] += d.size()
			sizes["ids_compressed"] += len(d.bytes(d.size()))
			sizes["fields"] += d.size()
			sizes["fields_compressed"] += len(d.bytes(d.size()))
			if d.bad {
				return nil, errLayoutBlock
			}
		}
	}
	return sizes, nil
}
