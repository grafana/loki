package chunkenc

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// LayoutLiteralCheck counts how the V5 or V6 pushdown for one literal compared
// with searching every line on its own.
type LayoutLiteralCheck struct {
	Blocks          int // blocks checked
	BlocksWithMatch int // blocks with a line containing the literal
	DictPass        int // blocks the V6 dictionary check lets through; every block for V5
	MatchingLines   int // lines containing the literal
}

// CheckLayoutLiterals decodes every block of a V5 or V6 chunk and, for each
// literal, compares the lines the pushdown keeps with bytes.Contains on every
// line. It fails on any line kept or dropped wrongly, and on any block the V6
// dictionary check would skip although a line in it contains the literal.
func CheckLayoutLiterals(c *MemChunk, literals [][]byte, out []LayoutLiteralCheck) error {
	if c.format != ChunkFormatV5 && c.format != ChunkFormatV6 {
		return fmt.Errorf("chunk format %d has no layout blocks", c.format)
	}
	for bi, b := range c.blocks {
		cur := layoutCursor{stats: stats.FromContext(context.Background()), block: encBlock{c.encoding, c.format, c.symbolizer, b}}
		if err := cur.load(); err != nil {
			return fmt.Errorf("block %d: %w", bi, err)
		}
		lines, offs := cur.lines, cur.s.offs
		var dict, tokenLens []byte
		if c.format == ChunkFormatV6 {
			d := layoutBuf{b: b.b}
			d.size()
			d.size()
			tokenLens = d.bytes(d.size())
			dict = d.bytes(d.size())
			if d.bad {
				return fmt.Errorf("block %d: %w", bi, errLayoutBlock)
			}
		}
		for li, lit := range literals {
			var want []int32
			for i := range cur.n {
				if bytes.Contains(lines[offs[i]:offs[i+1]], lit) {
					want = append(want, int32(i))
				}
			}
			p := newLiteralPlan(lit, c.format)
			if got := p.candidates(lines, offs, nil); !slices.Equal(got, want) {
				return fmt.Errorf("block %d literal %q: pushdown keeps lines %v, want %v", bi, lit, got, want)
			}
			pass := true
			if c.format == ChunkFormatV6 {
				pass = p.dictMayContain(dict, tokenLens)
			}
			if len(want) > 0 && !pass {
				return fmt.Errorf("block %d literal %q: dictionary check skips a block with %d matching lines", bi, lit, len(want))
			}
			o := &out[li]
			o.Blocks++
			o.MatchingLines += len(want)
			if len(want) > 0 {
				o.BlocksWithMatch++
			}
			if pass {
				o.DictPass++
			}
		}
		cur.Close()
	}
	return nil
}
