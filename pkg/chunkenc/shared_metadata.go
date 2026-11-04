package chunkenc

import (
	"slices"
	"strings"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// SharedMetadata interns one group's attributes for the chunk currently receiving
// it. It is scoped to a push, must not be shared by concurrent writers, and never
// adds a request cache to the chunk. Attribute slices supplied by callers are never mutated.
type SharedMetadata struct {
	attrs []logproto.LabelAdapter
	table *symbolizer
	syms  symbols
	valid []bool
	own   []logproto.LabelAdapter
}

// NewSharedMetadata copies and sorts a group's resolved resource/scope attributes.
func NewSharedMetadata(attrs []logproto.LabelAdapter) *SharedMetadata {
	sorted := slices.Clone(attrs)
	slices.SortFunc(sorted, func(a, b logproto.LabelAdapter) int { return strings.Compare(a.Name, b.Name) })
	return &SharedMetadata{attrs: sorted}
}

// AppendWithShared appends an entry with entry attributes taking precedence over
// shared attributes, using the existing chunk encoding and duplicate detection.
func (c *MemChunk) AppendWithShared(entry *logproto.Entry, shared *SharedMetadata) (bool, error) {
	if shared == nil || len(shared.attrs) == 0 || c.format < ChunkFormatV4 {
		return c.Append(entry)
	}
	hb, ok := c.head.(*unorderedHeadBlock)
	if !ok || hb.format < UnorderedWithStructuredMetadataHeadBlockFmt {
		return c.Append(entry)
	}
	syms, err := shared.symbolsFor(c.symbolizer, entry.StructuredMetadata)
	if err != nil {
		return false, err
	}
	dup, err := hb.appendWithSymbols(entry.Timestamp.UnixNano(), entry.Line, syms)
	if err != nil {
		return dup, err
	}
	if c.head.UncompressedSize() >= c.blockSize {
		return false, c.cut()
	}
	return dup, nil
}

func (m *SharedMetadata) symbolsFor(table *symbolizer, own []logproto.LabelAdapter) (symbols, error) {
	if table.readOnly {
		return nil, errSymbolizerReadOnly
	}
	if m.table != table {
		m.table = table
		// Previously returned symbols may still be referenced by an older chunk.
		m.syms = make(symbols, len(m.attrs))
		m.valid = make([]bool, len(m.attrs))
	}
	if len(own) == 0 {
		for i := range m.attrs {
			m.intern(table, i)
		}
		return m.syms, nil
	}
	m.own = append(m.own[:0], own...)
	slices.SortFunc(m.own, func(a, b logproto.LabelAdapter) int { return strings.Compare(a.Name, b.Name) })
	result := make(symbols, 0, len(own)+len(m.attrs))
	for i, j := 0, 0; i < len(m.own) || j < len(m.attrs); {
		if i < len(m.own) && (j == len(m.attrs) || m.own[i].Name <= m.attrs[j].Name) {
			for j < len(m.attrs) && m.own[i].Name == m.attrs[j].Name {
				j++
			}
			result = append(result, symbol{Name: table.add(m.own[i].Name), Value: table.add(m.own[i].Value)})
			i++
		} else {
			result = append(result, m.intern(table, j))
			j++
		}
	}
	return result, nil
}

func (m *SharedMetadata) intern(s *symbolizer, i int) symbol {
	if !m.valid[i] {
		m.syms[i] = symbol{Name: s.add(m.attrs[i].Name), Value: s.add(m.attrs[i].Value)}
		m.valid[i] = true
	}
	return m.syms[i]
}

// SpaceForWithMetadataSize preserves SpaceFor's chunk-cut accounting without
// materializing an entry's effective metadata. metadataSize includes overrides.
func (c *MemChunk) SpaceForWithMetadataSize(entry *logproto.Entry, metadataSize int) bool {
	return c.spaceFor(entry, metadataSize)
}
