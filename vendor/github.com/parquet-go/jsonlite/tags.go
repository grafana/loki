package jsonlite

// Object tag indexes are bump-allocated from blocks that grow geometrically,
// from tagBlockMin up to tagBlockMax.
//
// A fixed block size cannot serve both ends: sized for documents with many
// indexed objects it wastes most of a block on documents with one or two
// (a 512-byte block for the 28 tag bytes a cloud-logging record needs), and
// sized for the latter it stops amortizing. Doubling bounds the waste to the
// last block while keeping the number of allocations logarithmic.
//
// Tags cannot share an allocation with the []field slice they describe: the
// GC scans an allocation using the element type's pointer map, so raw tag
// bytes written into a pointer-marked word are a fatal bad pointer, and
// carving the fields out of a []byte instead would hide the key and value
// pointers from the GC entirely. Bump-allocating from a block is the next
// best thing: one allocation amortized across every indexed object in a
// document rather than one per object.
//
// A block is handed off to the values that reference it, never reused, so a
// retained object keeps its whole block alive. That is bounded by this
// constant and is negligible next to the cached JSON substring each object
// already retains.
const (
	tagBlockMin = 64
	tagBlockMax = 4096
)

// allocTags returns n bytes of tag storage from the parser's current block,
// starting a new block when the current one is exhausted. Objects wider than
// a block get an exact-size block of their own.
//
// The returned slice is full (len == cap), so the caller cannot extend it
// into the next object's tags, and the bytes are never written again once
// filled — which is what makes it safe to alias as an immutable string.
func (p *parser) allocTags(n int) []byte {
	if n > cap(p.tags)-len(p.tags) {
		grown := min(max(cap(p.tags)*2, tagBlockMin), tagBlockMax)
		p.tags = make([]byte, 0, max(n, grown))
	}
	off := len(p.tags)
	p.tags = p.tags[:off+n]
	return p.tags[off : off+n : off+n]
}
