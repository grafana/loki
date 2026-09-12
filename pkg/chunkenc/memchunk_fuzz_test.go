package chunkenc

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
)

// Label names that all survive normalization, so reading the chunk the rewrite
// starts from always works. Several change shape on the way out ("a.b" ->
// "a_b", "9lives" -> "key_9lives"), which is what tells the form a block was
// encoded with apart from the form reads hand back. "a.b" and "a__b" normalize
// to the same name, so entries can collide on it.
var fuzzSymbolNames = []string{"app", "a.b", "a__b", "trace.id", "__reserved__", "level", "9lives", "x"}

// Values are stored and read back untouched, so these deliberately include
// strings that are not valid label names.
var fuzzSymbolValues = []string{"/", "-", "1", "", "value", "a/b/c", "alice@example.com"}

var fuzzBlockSizes = []int{1, 20, 60, 200, 1024}

// fuzzGen turns the fuzzer's bytes into a chunk layout: how big its blocks are,
// what structured metadata each entry carries and which entries the rewrite
// removes. It runs off the end of short inputs rather than failing, so every
// input describes some chunk.
type fuzzGen struct {
	data []byte
	pos  int
}

func (g *fuzzGen) next() byte {
	if g.pos >= len(g.data) {
		return 0
	}
	b := g.data[g.pos]
	g.pos++
	return b
}

func (g *fuzzGen) intn(n int) int {
	if n <= 1 {
		return 0
	}
	return int(g.next()) % n
}

func readEntriesErr(chk Chunk) ([]logproto.Entry, error) {
	itr, err := chk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.EmptyLabels()))
	if err != nil {
		return nil, err
	}

	var entries []logproto.Entry
	for itr.Next() {
		entries = append(entries, itr.At())
	}

	return entries, itr.Err()
}

func blankSymbols(c *MemChunk) int {
	n := 0
	for _, lbl := range c.symbolizer.labels {
		if lbl == "" {
			n++
		}
	}
	return n
}

func liveSymbols(c *MemChunk) []string {
	var live []string
	for _, lbl := range c.symbolizer.labels {
		if lbl != "" {
			live = append(live, lbl)
		}
	}
	return live
}

// FuzzMemChunkRewrite checks that rewriting a chunk to drop some of its entries
// leaves the rest exactly as they were.
//
// Blocks reference symbols by their position in the chunk's symbol table, and
// Rewrite copies the blocks it did not have to change straight over, so the
// table it builds has to keep those positions pointing at the same strings
// while still dropping what only the removed entries referenced. Which symbols
// that leaves live depends entirely on how the entries share metadata and where
// the removals land, which is what makes it worth fuzzing rather than enumerating.
//
// Run the seed corpus with the rest of the package, and explore with:
//
//	go test ./pkg/chunkenc/ -run=Fuzz -fuzz=FuzzMemChunkRewrite -fuzztime=5m
func FuzzMemChunkRewrite(f *testing.F) {
	// The generator reads the input as a script, so a seed is only as
	// interesting as the layout it decodes to. These were built backwards from
	// the layout wanted -- see the byte order in fuzzGen -- and cover removals
	// at the front, in the middle, at the end and adjacent, one entry per block
	// and several, both source shapes, and the metadata that makes the symbol
	// table interesting. Note that running off the end of the input yields
	// zeroes, which reads as "drop, with no metadata", so a seed that stops
	// early silently drops everything after that point.
	// Removals in the middle, one entry per block, source built in memory.
	f.Add([]byte{4, 0, 1, 0, 4, 1, 1, 0, 5, 1, 1, 0, 6, 0, 1, 0, 0, 1, 1, 0, 1, 1, 1})
	// Scattered removals, one entry per block, source read back from storage.
	f.Add([]byte{7, 0, 1, 3, 4, 1, 1, 3, 5, 0, 1, 3, 6, 1, 1, 3, 0, 1, 1, 3, 1, 0, 1, 3, 2, 1, 1, 3, 3, 0, 1, 3, 4, 1, 0})
	// A single removal from a chunk of three blocks, so one block is re-encoded
	// while both its neighbours are copied verbatim.
	f.Add([]byte{11, 2, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 2, 1, 1, 1, 3, 1, 1, 1, 4, 1, 1, 1, 5, 1, 1, 1, 6, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 3, 1, 1, 1, 4, 1, 0})
	// Adjacent removals.
	f.Add([]byte{5, 0, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 2, 0, 1, 1, 3, 0, 1, 1, 4, 1, 1, 1, 5, 1, 1})
	// Two pairs in one entry whose names normalize to the same thing, which the
	// read view collapses into one.
	f.Add([]byte{3, 2, 2, 1, 4, 2, 6, 1, 2, 1, 4, 2, 6, 0, 2, 1, 4, 2, 6, 1, 2, 1, 4, 2, 6, 1, 0})
	// Values that are not valid label names, including the empty one.
	f.Add([]byte{5, 0, 1, 0, 0, 0, 1, 1, 1, 1, 1, 2, 2, 1, 1, 3, 3, 0, 1, 4, 0, 1, 1, 5, 1, 1, 1})
	// Removing every entry, and an input that runs out immediately.
	f.Add([]byte{2, 0, 1, 0, 4, 0, 1, 0, 5, 0, 1, 0, 6, 0, 1})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		g := &fuzzGen{data: data}

		numEntries := 1 + g.intn(20)
		blockSize := fuzzBlockSizes[g.intn(len(fuzzBlockSizes))]

		originalChunk := NewMemChunk(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, testTargetSize)

		dropped := make(map[string]bool, numEntries)
		for i := 0; i < numEntries; i++ {
			line := fmt.Sprintf("line-%03d", i)

			var sm []logproto.LabelAdapter
			for pairs := g.intn(4); pairs > 0; pairs-- {
				sm = append(sm, logproto.LabelAdapter{
					Name:  fuzzSymbolNames[g.intn(len(fuzzSymbolNames))],
					Value: fuzzSymbolValues[g.intn(len(fuzzSymbolValues))],
				})
			}

			// Timestamps stay strictly increasing: iteration order is not what
			// this target is about.
			_, err := originalChunk.Append(&logproto.Entry{
				Timestamp:          time.Unix(0, int64(i+1)),
				Line:               line,
				StructuredMetadata: sm,
			})
			require.NoError(t, err)

			dropped[line] = g.intn(2) == 0
		}
		// Entries left in the head block are not part of any block yet, and
		// Rewrite only walks the cut ones.
		require.NoError(t, originalChunk.Close())

		// Half the time, rewrite the chunk the way the compactor gets it: read
		// back from storage, so its symbolizer is read-only and carries no
		// symbolsMap.
		if g.intn(2) == 0 {
			b, err := originalChunk.Bytes()
			require.NoError(t, err)
			originalChunk, err = NewByteChunk(b, blockSize, testTargetSize)
			require.NoError(t, err)
		}

		// Take what the chunk actually holds as the baseline rather than the
		// metadata handed to Append, so that whatever the write path did to it is
		// already accounted for and this only measures what the rewrite changed.
		original, err := readEntriesErr(originalChunk)
		require.NoError(t, err)

		var expected []logproto.Entry
		for _, e := range original {
			if !dropped[e.Line] {
				expected = append(expected, e)
			}
		}

		// Account for what each entry refers to by its stored symbols rather than
		// by the metadata a read returns: reads normalize label names, and two
		// pairs whose names normalize to the same thing collapse into one, so the
		// read view understates what an entry still holds.
		survivingValues := map[string]struct{}{}
		removedValues := map[string]struct{}{}
		for _, b := range originalChunk.blocks {
			itr := newBufferedIterator(context.Background(), compression.GetReaderPool(originalChunk.encoding), b.b, originalChunk.format, originalChunk.symbolizer)
			for itr.Next() {
				into := survivingValues
				if dropped[string(itr.currLine)] {
					into = removedValues
				}
				for _, sym := range itr.currSymbols() {
					into[originalChunk.symbolizer.lookup(sym.Value)] = struct{}{}
				}
			}
			require.NoError(t, itr.Err())
		}

		filterFunc := func(_ time.Time, s string, _ labels.Labels) bool { return dropped[s] }

		rewritten, err := originalChunk.Rewrite(filterFunc)
		if len(expected) == 0 {
			require.Equal(t, chunk.ErrRewriteNoDataLeft, err)
			return
		}
		require.NoError(t, err)
		newChunk := rewritten.(*MemChunk)

		// The entries that survived kept every byte of their metadata.
		actual, err := readEntriesErr(newChunk)
		require.NoError(t, err)
		requireEntriesEqual(t, expected, actual)

		// The rewritten chunk is what gets uploaded, so it has to survive the
		// encoding too.
		b, err := newChunk.Bytes()
		require.NoError(t, err)
		roundTripped, err := NewByteChunk(b, blockSize, testTargetSize)
		require.NoError(t, err)
		actual, err = readEntriesErr(roundTripped)
		require.NoError(t, err)
		requireEntriesEqual(t, expected, actual)

		// No surviving entry resolves a name to a blanked or out of range symbol.
		for _, e := range actual {
			for _, lbl := range e.StructuredMetadata {
				require.NotEmpty(t, lbl.Name, "entry %q resolved a name to a blanked symbol", e.Line)
			}
		}

		// The generator never damages the table and no name in its alphabet is
		// empty, so nothing here resolves a position the table does not hold and
		// the rewrite files no symbol of its own. Adding either would make this a
		// misleading failure rather than a real one.
		//
		// Keeping the positions stable must not grow the table or file a second
		// copy of a symbol: reads normalize label names, so re-encoding from what
		// a read returns rather than from what is stored would do both.
		require.Len(t, newChunk.symbolizer.labels, len(originalChunk.symbolizer.labels), "rewrite changed the size of the symbol table")

		live := liveSymbols(newChunk)
		seen := make(map[string]struct{}, len(live))
		for _, lbl := range live {
			_, dup := seen[lbl]
			require.False(t, dup, "symbol %q stored twice", lbl)
			seen[lbl] = struct{}{}
		}

		// Deleting an entry deletes its metadata: a value nothing surviving
		// refers to is gone from the chunk, not just unreferenced. The empty
		// string is what a blanked symbol holds, so it is not a value to chase.
		for value := range removedValues {
			if value == "" {
				continue
			}
			if _, ok := survivingValues[value]; ok {
				continue
			}
			require.NotContains(t, seen, value, "value %q of a removed entry is still in the chunk", value)
		}
	})
}
