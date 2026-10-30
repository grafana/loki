package parquet

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/parquet-go/parquet-go/variant"
)

// This file implements a columnar, cursor-based reader for VARIANT columns.
//
// Unlike the reconstruction path in variant_shredded_read.go — which rebuilds
// one variant.Value tree per row — VariantReader exposes the shredded layout
// of the file directly: every position in the shredding tree is a
// (typed_value, value) pair where at most one side is populated per row, and
// the API surfaces that duality as typed vectors plus a per-row location tag.
// A query engine evaluating a path expression such as $.a.b over a column
// shredded as INT64 runs a plain vector loop over Int64s()/Locs(), and only
// falls back to per-row work for rows where the value exists as residual
// variant binary.
//
// Navigation is total: a cursor exists for every path, whether or not the
// writer shredded it. Below a non-shredded position, cursors are backed by
// navigation of the nearest shredded ancestor's residual bytes, so location
// tags remain accurate through residuals: LocMissing means the path is
// missing even after looking inside ancestor residual values, and a type
// conflict anywhere along the path (a scalar where an object was shredded, a
// list where a scalar was expected) simply surfaces as variant.LocResidual
// with the residual value already navigated to the requested position.

// VariantCursorKind is the static (schema-derived) kind of a cursor
// position. It tells the engine which access pattern the position supports;
// the per-row runtime disposition is in Locs.
type VariantCursorKind uint8

const (
	// VariantCursorUnshredded is a position with no typed_value column:
	// either the schema stores only variant binary here, or the cursor
	// navigates below the shredded schema entirely. All present values are
	// variant.LocNull or variant.LocResidual.
	VariantCursorUnshredded VariantCursorKind = iota

	// VariantCursorLeaf is a position shredded as a primitive type; typed
	// values are read from the typed vector accessors.
	VariantCursorLeaf

	// VariantCursorObject is a position shredded as an object; descend into
	// shredded fields with Field.
	VariantCursorObject

	// VariantCursorList is a position shredded as a list; descend with
	// Elements and use ListOffsets.
	VariantCursorList
)

func (k VariantCursorKind) String() string {
	switch k {
	case VariantCursorUnshredded:
		return "unshredded"
	case VariantCursorLeaf:
		return "leaf"
	case VariantCursorObject:
		return "object"
	case VariantCursorList:
		return "list"
	default:
		return "unknown"
	}
}

// VariantReader reads one VARIANT column of a row group in columnar form.
//
// All cursors created from a reader share one row window, advanced with
// Next. Vectors and residual views returned by cursors are valid until the
// next call to Next or SeekToRow.
//
// Only the columns reachable from cursors that have been created are read:
// creating cursors is how an engine declares projection. Cursors may be
// created at any time; cursors created after a call to Next take effect on
// the following call.
type VariantReader struct {
	rowGroup   RowGroup
	group      *shreddedVariantGroup
	groupDef   int // definition level at which the variant group is present
	baseColumn int // first leaf column of the variant group
	numRows    int64
	rowOffset  int64
	leafInfo   []variantLeafInfo
	leaves     []*variantLeafReader // lazily created, indexed by relative column
	metaLeaf   *variantLeafReader
	root       *VariantCursor
	metaCache  map[string]variant.Metadata
	leafSet    map[*variantLeafReader]struct{} // reused by Next
	err        error

	// rootValueRequired is set for plain unshredded variant groups, whose
	// value field is required rather than optional.
	rootValueRequired bool
}

// unshreddedVariantGroupOf recognizes the plain unshredded variant group
// shape (required binary metadata and required binary value, nothing else)
// and returns its reconstruction tree. Groups with an optional value field
// are handled by buildShreddedVariantGroup instead.
func unshreddedVariantGroupOf(node Node) (*shreddedVariantGroup, bool) {
	g := &shreddedVariantGroup{metadataCol: -1, valueCol: -1}
	col := 0
	for _, f := range node.Fields() {
		if !f.Leaf() || f.Type().Kind() != ByteArray || f.Repeated() {
			return nil, false
		}
		switch f.Name() {
		case "metadata":
			if f.Optional() {
				return nil, false
			}
			g.metadataCol = col
		case "value":
			if f.Optional() {
				// An optional value with no typed_value is handled by
				// buildShreddedVariantGroup; this recognizer exists for
				// the required-value shape it rejects.
				return nil, false
			}
			g.valueCol = col
		default:
			return nil, false
		}
		col++
	}
	if g.metadataCol < 0 || g.valueCol < 0 {
		return nil, false
	}
	g.numCols = col
	return g, true
}

type variantLeafInfo struct {
	typ    Type
	maxDef byte
	maxRep byte
}

// variantColumnInfo is the result of resolving a variant column path in a
// schema, shared by NewVariantReader and NewVariantColumnWriter.
type variantColumnInfo struct {
	node          Node
	group         *shreddedVariantGroup
	valueRequired bool // plain unshredded shape (required binary value)
	col           int  // first leaf column of the variant group
	def           int  // definition level at which the group is present
	optional      bool // whether the group node itself is optional
}

// variantGroupOf builds the shredding tree of a variant group node,
// accepting both the shredded shapes (optional value) and the plain
// unshredded shape (required value) that VariantEncoding.md declares. The
// boolean result reports the latter.
func variantGroupOf(node Node) (*shreddedVariantGroup, bool, error) {
	g, err := buildShreddedVariantGroup(node, 0, 0, 0)
	if err != nil {
		if u, ok := unshreddedVariantGroupOf(node); ok {
			return u, true, nil
		}
		return nil, false, err
	}
	if g.metadataCol < 0 {
		return nil, false, fmt.Errorf("variant: variant group has no metadata field")
	}
	return g, false, nil
}

// resolveVariantColumn walks the schema to the variant group node named by
// path and builds its shredding tree.
func resolveVariantColumn(schema Node, path []string) (info variantColumnInfo, err error) {
	if len(path) == 0 {
		return info, fmt.Errorf("variant: a column path is required")
	}
	node := schema
	for _, name := range path {
		if node.Leaf() {
			return info, fmt.Errorf("variant: column %q not found: %q is a leaf", joinPath(path), name)
		}
		var next Node
		for _, f := range node.Fields() {
			if f.Name() == name {
				next = f
				break
			}
			info.col += int(numLeafColumnsOf(f))
		}
		if next == nil {
			return info, fmt.Errorf("variant: column %q not found: no field %q", joinPath(path), name)
		}
		if next.Repeated() {
			return info, fmt.Errorf("variant: column %q is beneath a repeated field, which the columnar variant APIs do not support", joinPath(path))
		}
		info.optional = next.Optional()
		if info.optional {
			info.def++
		}
		node = next
	}
	if node.Leaf() {
		return info, fmt.Errorf("variant: column %q is not a variant group", joinPath(path))
	}
	info.node = node
	info.group, info.valueRequired, err = variantGroupOf(node)
	if err != nil {
		return info, fmt.Errorf("variant: column %q: %w", joinPath(path), err)
	}
	return info, nil
}

// NewVariantReader creates a reader for the variant column at the given path
// in the row group. The path names the variant group node in the schema
// (e.g. "event" for a top-level column, "attrs", "v" for a nested one). The
// column may be unshredded (metadata, value) or shredded (metadata, value,
// typed_value) in any shape permitted by the Variant Shredding
// specification.
func NewVariantReader(rowGroup RowGroup, path ...string) (*VariantReader, error) {
	info, err := resolveVariantColumn(rowGroup.Schema(), path)
	if err != nil {
		return nil, err
	}
	group, node := info.group, info.node

	r := &VariantReader{
		rowGroup:   rowGroup,
		group:      group,
		groupDef:   info.def,
		baseColumn: info.col,
		numRows:    rowGroup.NumRows(),
		leafInfo:   make([]variantLeafInfo, group.numCols),
		leaves:     make([]*variantLeafReader, group.numCols),
		metaCache:  make(map[string]variant.Metadata),

		rootValueRequired: info.valueRequired,
	}

	// Types in leaf order; the traversal order matches the column numbering
	// of buildShreddedVariantGroup (both walk Fields() depth-first).
	i := 0
	forEachLeafColumnOf(node, func(leaf leafColumn) {
		if i < len(r.leafInfo) {
			r.leafInfo[i].typ = leaf.node.Type()
		}
		i++
	})
	if i != group.numCols {
		return nil, fmt.Errorf("variant: column %q has %d leaves but the shredded group spans %d", joinPath(path), i, group.numCols)
	}
	r.fillLeafLevels(group)

	for _, li := range r.leafInfo {
		switch li.typ.Kind() {
		case Boolean, Int32, Int64, Float, Double, ByteArray, FixedLenByteArray:
		default:
			return nil, fmt.Errorf("variant: unsupported leaf type %s in shredded variant group", li.typ)
		}
	}

	r.metaLeaf = r.leafFor(group.metadataCol)
	r.root = r.newNodeCursor(nil, group, "", false)
	return r, nil
}

func joinPath(path []string) string {
	return strings.Join(path, ".")
}

// fillLeafLevels computes the absolute max definition and repetition level
// of every leaf column of the variant subtree from the shredding tree.
func (r *VariantReader) fillLeafLevels(g *shreddedVariantGroup) {
	if g.metadataCol >= 0 {
		r.leafInfo[g.metadataCol].maxDef = byte(r.groupDef + g.defLevel)
		r.leafInfo[g.metadataCol].maxRep = byte(g.repDepth)
	}
	if g.valueCol >= 0 {
		valueDef := r.groupDef + g.defLevel + 1
		if g == r.group && r.rootValueRequired {
			valueDef = r.groupDef
		}
		r.leafInfo[g.valueCol].maxDef = byte(valueDef)
		r.leafInfo[g.valueCol].maxRep = byte(g.repDepth)
	}
	if t := g.typed; t != nil {
		switch t.kind {
		case shreddedTypedPrimitive:
			r.leafInfo[t.startCol].maxDef = byte(r.groupDef + t.defLevel)
			r.leafInfo[t.startCol].maxRep = byte(g.repDepth)
		case shreddedTypedList:
			r.fillLeafLevels(t.element)
		case shreddedTypedObject:
			for _, f := range t.fields {
				r.fillLeafLevels(f.group)
			}
		}
	}
}

func (r *VariantReader) leafFor(relCol int) *variantLeafReader {
	if l := r.leaves[relCol]; l != nil {
		return l
	}
	info := r.leafInfo[relCol]
	l := &variantLeafReader{
		reader:      r,
		relCol:      relCol,
		typ:         info.typ,
		kind:        info.typ.Kind(),
		maxDef:      info.maxDef,
		maxRep:      info.maxRep,
		pendingSeek: -1,
	}
	r.leaves[relCol] = l
	return l
}

// Root returns the cursor for the variant value itself.
func (r *VariantReader) Root() *VariantCursor { return r.root }

// Path returns the cursor for the object field path below the root,
// equivalent to chaining Field calls.
func (r *VariantReader) Path(path ...string) *VariantCursor {
	c := r.root
	for _, name := range path {
		c = c.Field(name)
	}
	return c
}

// NumRows returns the number of rows of the row group.
func (r *VariantReader) NumRows() int64 { return r.numRows }

// Next advances the shared row window by up to n rows and recomputes the
// window state of every cursor. It returns the number of rows in the new
// window, or (0, io.EOF) when the row group is exhausted.
func (r *VariantReader) Next(n int) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if n <= 0 {
		return 0, nil
	}
	remaining := r.numRows - r.rowOffset
	if remaining <= 0 {
		return 0, io.EOF
	}
	if int64(n) > remaining {
		n = int(remaining)
	}
	if r.leafSet == nil {
		r.leafSet = make(map[*variantLeafReader]struct{})
	}
	clear(r.leafSet)
	r.collectLeaves(r.root, r.leafSet)
	for l := range r.leafSet {
		if err := l.readWindow(n); err != nil {
			r.err = err
			return 0, err
		}
	}
	if err := r.root.process(n); err != nil {
		r.err = err
		return 0, err
	}
	r.rowOffset += int64(n)
	return n, nil
}

func (r *VariantReader) collectLeaves(c *VariantCursor, seen map[*variantLeafReader]struct{}) {
	if c == r.root {
		seen[r.metaLeaf] = struct{}{}
	}
	for _, l := range [...]*variantLeafReader{c.valueLeaf, c.typedLeaf, c.presenceLeaf} {
		if l != nil {
			seen[l] = struct{}{}
		}
	}
	for _, ch := range c.childList {
		r.collectLeaves(ch, seen)
	}
}

// SeekToRow positions the reader such that the next call to Next starts at
// the given row of the row group.
func (r *VariantReader) SeekToRow(rowIndex int64) error {
	if r.err != nil {
		return r.err
	}
	if rowIndex < 0 || rowIndex > r.numRows {
		return fmt.Errorf("variant: seek to row %d out of range [0, %d]", rowIndex, r.numRows)
	}
	r.rowOffset = rowIndex
	for _, l := range r.leaves {
		if l != nil && l.opened {
			l.pendingSeek = rowIndex
		}
	}
	return nil
}

// Close releases the page readers of all columns opened by the reader.
func (r *VariantReader) Close() error {
	var firstErr error
	for _, l := range r.leaves {
		if l == nil || !l.opened {
			continue
		}
		if l.page != nil {
			Release(l.page)
			l.page = nil
		}
		if err := l.pages.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		l.opened = false
	}
	return firstErr
}

func (r *VariantReader) metadataFor(row int32) (variant.Metadata, error) {
	w := &r.metaLeaf.win
	d := w.denseIdx[row]
	if d < 0 {
		return variant.Metadata{}, fmt.Errorf("variant: missing metadata for window row %d", row)
	}
	b := w.slab[w.offsets[d]:w.offsets[d+1]]
	if m, ok := r.metaCache[string(b)]; ok {
		return m, nil
	}
	m, err := variant.DecodeMetadata(b)
	if err != nil {
		return variant.Metadata{}, fmt.Errorf("variant metadata: %w", err)
	}
	// The cache key copies the bytes (they alias the window slab); cap the
	// cache so files with per-row unique metadata do not grow it unboundedly.
	if len(r.metaCache) < 4096 {
		r.metaCache[string(b)] = m
	}
	return m, nil
}

// VariantCursor is a position in the variant navigation tree: the root
// value, an object field, or a list element. See VariantReader.
type VariantCursor struct {
	reader *VariantReader
	parent *VariantCursor
	node   *shreddedVariantGroup // nil for cursors below the shredded schema
	kind   VariantCursorKind

	fieldName  string // step from parent, for field cursors
	isElements bool   // step from parent, for element cursors

	depth int // repetition depth within the variant subtree

	valueLeaf    *variantLeafReader
	typedLeaf    *variantLeafReader // primitive typed_value
	presenceLeaf *variantLeafReader // first leaf under an object/list typed_value
	valueMaxDef  byte
	typedMaxDef  byte
	typedDefAbs  byte // definition level at which typed_value is present
	elemsDefAbs  byte // definition level at which a typed list has elements

	children        map[string]*VariantCursor
	childList       []*VariantCursor
	elemsCursor     *VariantCursor
	hasVirtualChild bool

	// Window state. All slices are indexed by entry: one entry per row for
	// row-level cursors, one per list element for element cursors.
	//
	// Residual state is stored sparsely: entries holding residual bytes or
	// values are rare in shredded-heavy data, and per-entry storage of a
	// variant.Value (144 bytes) plus a byte-slice header dominated the
	// reader's memory footprint and window-reset cost.
	identityRows  bool // entry i is row i; rows stays nil
	identitySlots bool // entry i is slot group i; slotGroup stays nil
	locs          []variant.Loc
	slotGroup     []int32           // slot group in this node's own columns, -1 if none
	rows          []int32           // window row of each entry, nil means identity
	res           []variantResidual // residual state of tagged entries, ascending by entry
	typedRows     []int32
	listOffsets   []int32
	residualCount int
}

// variantResidual is the residual state of one entry: the raw bytes from the
// value column (nil for navigated entries, which are born decoded) and the
// lazily decoded value.
type variantResidual struct {
	bytes   []byte
	val     variant.Value
	entry   int32
	decoded bool
}

func (r *VariantReader) newNodeCursor(parent *VariantCursor, node *shreddedVariantGroup, name string, elements bool) *VariantCursor {
	c := &VariantCursor{
		reader:     r,
		parent:     parent,
		node:       node,
		fieldName:  name,
		isElements: elements,
		depth:      node.repDepth,
		children:   make(map[string]*VariantCursor),
	}
	c.identityRows = parent == nil || (!elements && parent.identityRows)
	c.identitySlots = parent == nil || (!elements && parent.identitySlots)
	if node.valueCol >= 0 {
		c.valueLeaf = r.leafFor(node.valueCol)
		c.valueMaxDef = r.leafInfo[node.valueCol].maxDef
	}
	if t := node.typed; t != nil {
		c.typedDefAbs = byte(r.groupDef + t.defLevel)
		switch t.kind {
		case shreddedTypedPrimitive:
			c.kind = VariantCursorLeaf
			c.typedLeaf = r.leafFor(t.startCol)
			c.typedMaxDef = byte(r.groupDef + t.defLevel)
		case shreddedTypedObject:
			c.kind = VariantCursorObject
			c.presenceLeaf = r.leafFor(t.startCol)
		case shreddedTypedList:
			c.kind = VariantCursorList
			c.presenceLeaf = r.leafFor(t.startCol)
			c.elemsDefAbs = byte(r.groupDef + t.defLevel + 1)
		}
	} else {
		c.kind = VariantCursorUnshredded
	}
	return c
}

func (r *VariantReader) newVirtualCursor(parent *VariantCursor, name string, elements bool) *VariantCursor {
	return &VariantCursor{
		reader:       r,
		parent:       parent,
		kind:         VariantCursorUnshredded,
		fieldName:    name,
		isElements:   elements,
		depth:        parent.depth,
		children:     make(map[string]*VariantCursor),
		identityRows: !elements && parent.identityRows,
		// Virtual cursors have no columns of their own, so their slot
		// groups are never read; leaving identitySlots set skips the
		// per-entry slotGroup appends.
		identitySlots: true,
	}
}

// Kind returns the static kind of the cursor position, derived from the
// shredded schema.
func (c *VariantCursor) Kind() VariantCursorKind { return c.kind }

// LeafType returns the parquet type of the typed_value column for cursors of
// kind VariantCursorLeaf, and nil otherwise. The type's logical type
// identifies the variant type per the shredded types table (e.g. a
// STRING-annotated BYTE_ARRAY holds variant strings).
func (c *VariantCursor) LeafType() Type {
	if c.typedLeaf != nil {
		return c.typedLeaf.typ
	}
	return nil
}

// Fields returns the names of the shredded fields at a cursor of kind
// VariantCursorObject, in schema order, and nil for other kinds. Residual
// fields of partially shredded objects are not listed; they are reached
// through Residual (or by name through Field).
func (c *VariantCursor) Fields() []string {
	if c.node == nil || c.node.typed == nil || c.node.typed.kind != shreddedTypedObject {
		return nil
	}
	names := make([]string, len(c.node.typed.fields))
	for i, f := range c.node.typed.fields {
		names[i] = f.name
	}
	return names
}

// Field returns the cursor for the object field with the given name below
// this cursor. Navigation is total: if the field is not part of the shredded
// schema at this position, the returned cursor is backed by per-row
// navigation of residual values.
func (c *VariantCursor) Field(name string) *VariantCursor {
	if ch := c.children[name]; ch != nil {
		return ch
	}
	var ch *VariantCursor
	if c.node != nil && c.node.typed != nil && c.node.typed.kind == shreddedTypedObject {
		if g := c.node.typed.fieldByName(name); g != nil {
			ch = c.reader.newNodeCursor(c, g, name, false)
		}
	}
	if ch == nil {
		ch = c.reader.newVirtualCursor(c, name, false)
		c.hasVirtualChild = true
	}
	c.children[name] = ch
	c.childList = append(c.childList, ch)
	return ch
}

// Elements returns the cursor for the elements of lists at this position.
// The element entry space concatenates, in row order, the elements of typed
// (shredded) lists and the elements of residual values that turn out to be
// arrays, so an engine can process mixed shredded/residual lists in one
// loop. ListOffsets on this cursor maps entries to element ranges.
func (c *VariantCursor) Elements() *VariantCursor {
	if c.elemsCursor != nil {
		return c.elemsCursor
	}
	var ch *VariantCursor
	if c.node != nil && c.node.typed != nil && c.node.typed.kind == shreddedTypedList {
		ch = c.reader.newNodeCursor(c, c.node.typed.element, "", true)
	} else {
		ch = c.reader.newVirtualCursor(c, "", true)
		c.hasVirtualChild = true
	}
	c.elemsCursor = ch
	c.childList = append(c.childList, ch)
	return ch
}

// Locs returns the location tag of every entry of the current window: one
// per row for row-level cursors, one per element for element cursors.
func (c *VariantCursor) Locs() []variant.Loc { return c.locs }

// Rows maps each entry of the current window to its row index within the
// window. For row-level cursors the mapping is the identity and Rows returns
// nil; for element cursors (and cursors below them) it returns one row index
// per entry, and several entries may map to one row.
func (c *VariantCursor) Rows() []int32 { return c.rows }

// ResidualCount returns the number of entries tagged variant.LocResidual in
// the current window. A zero count lets an engine skip per-entry residual
// decoding, though Residual may still return values for entries tagged
// variant.LocNull or variant.LocTypedObject (partial-object leftovers).
func (c *VariantCursor) ResidualCount() int { return c.residualCount }

// TypedRows maps each value of the typed vectors to its entry index in the
// current window: TypedRows()[i] is the entry holding the i-th typed value.
func (c *VariantCursor) TypedRows() []int32 { return c.typedRows }

// Booleans returns the dense typed values of a BOOLEAN leaf cursor.
func (c *VariantCursor) Booleans() []bool {
	if c.typedLeaf == nil {
		return nil
	}
	return c.typedLeaf.win.bools
}

// Int32s returns the dense typed values of an INT32-backed leaf cursor
// (int8, int16, int32, date, and decimal4 variant types).
func (c *VariantCursor) Int32s() []int32 {
	if c.typedLeaf == nil {
		return nil
	}
	return c.typedLeaf.win.i32
}

// Int64s returns the dense typed values of an INT64-backed leaf cursor
// (int64, timestamps, time, and decimal8 variant types).
func (c *VariantCursor) Int64s() []int64 {
	if c.typedLeaf == nil {
		return nil
	}
	return c.typedLeaf.win.i64
}

// Floats returns the dense typed values of a FLOAT leaf cursor.
func (c *VariantCursor) Floats() []float32 {
	if c.typedLeaf == nil {
		return nil
	}
	return c.typedLeaf.win.f32
}

// Doubles returns the dense typed values of a DOUBLE leaf cursor.
func (c *VariantCursor) Doubles() []float64 {
	if c.typedLeaf == nil {
		return nil
	}
	return c.typedLeaf.win.f64
}

// ByteArrays returns the dense typed values of a BYTE_ARRAY-backed leaf
// cursor (string, binary, and byte-array decimal variant types) as a shared
// slab plus len+1 offsets: value i is slab[offsets[i]:offsets[i+1]].
func (c *VariantCursor) ByteArrays() (slab []byte, offsets []uint32) {
	if c.typedLeaf == nil {
		return nil, nil
	}
	return c.typedLeaf.win.slab, c.typedLeaf.win.offsets
}

// FixedLenByteArrays returns the dense typed values of a
// FIXED_LEN_BYTE_ARRAY-backed leaf cursor (uuid and fixed decimal variant
// types) as a shared slab of size-byte values.
func (c *VariantCursor) FixedLenByteArrays() (slab []byte, size int) {
	if c.typedLeaf == nil {
		return nil, 0
	}
	return c.typedLeaf.win.flba, c.typedLeaf.win.flbaSize
}

// ListOffsets returns len(entries)+1 offsets into the entry space of the
// Elements cursor: the elements of entry e span
// Elements() entries [offsets[e], offsets[e+1]). It is computed only when
// Elements was called before the window was read.
func (c *VariantCursor) ListOffsets() []int32 { return c.listOffsets }

// Residual returns the residual variant value of entry i. It is valid for
// entries tagged variant.LocResidual (the value itself), variant.LocNull (the
// variant null value), and variant.LocTypedObject when the row is a partially
// shredded object (the leftover fields not covered by the shredded schema,
// as a variant object). The boolean result reports whether a residual value
// exists at the entry.
func (c *VariantCursor) Residual(i int) (variant.Value, bool, error) {
	if i < 0 || i >= len(c.locs) {
		return variant.Null(), false, fmt.Errorf("variant: residual index %d out of range", i)
	}
	switch c.locs[i] {
	case variant.LocNull:
		return variant.Null(), true, nil
	case variant.LocResidual, variant.LocTypedObject:
		r := c.residualAt(i)
		if r == nil {
			return variant.Null(), false, nil
		}
		if r.decoded {
			return r.val, true, nil
		}
		v, err := c.decodeResidual(r)
		if err != nil {
			return variant.Null(), false, err
		}
		return v, true, nil
	default:
		return variant.Null(), false, nil
	}
}

// residualAt returns the residual state of entry e, or nil if the entry has
// none. res is sorted by entry (windows are built in entry order).
func (c *VariantCursor) residualAt(e int) *variantResidual {
	i, ok := slices.BinarySearchFunc(c.res, int32(e), func(r variantResidual, e int32) int {
		return int(r.entry) - int(e)
	})
	if !ok {
		return nil
	}
	return &c.res[i]
}

// addResidual appends residual state for entry e, which must be greater than
// the entry of any state added to this window before it. The pointer is
// valid only until the next call to addResidual.
func (c *VariantCursor) addResidual(e int) *variantResidual {
	c.res = append(c.res, variantResidual{entry: int32(e)})
	return &c.res[len(c.res)-1]
}

func (c *VariantCursor) decodeResidual(r *variantResidual) (variant.Value, error) {
	m, err := c.reader.metadataFor(c.rowOf(int(r.entry)))
	if err != nil {
		return variant.Null(), err
	}
	v, err := variant.Decode(m, r.bytes)
	if err != nil {
		return variant.Null(), fmt.Errorf("variant: decoding residual value: %w", err)
	}
	r.val = v
	r.decoded = true
	return v, nil
}

func (c *VariantCursor) rowOf(i int) int32 {
	if c.rows == nil {
		return int32(i)
	}
	return c.rows[i]
}

// resetWindow clears the per-window state, retaining slice capacity. The
// residual entries are cleared so that references to window slabs and
// decoded values do not outlive the window.
func (c *VariantCursor) resetWindow() {
	c.locs = c.locs[:0]
	c.slotGroup = c.slotGroup[:0]
	c.rows = c.rows[:0]
	clear(c.res)
	c.res = c.res[:0]
	c.typedRows = c.typedRows[:0]
	c.listOffsets = c.listOffsets[:0]
	c.residualCount = 0
}

// growEntries bulk-extends the window to n entries, all tagged
// variant.LocMissing, for the flat window paths that fill locs in place.
func (c *VariantCursor) growEntries(n int) {
	c.locs = slices.Grow(c.locs, n)[:n]
	clear(c.locs)
}

func (c *VariantCursor) appendEntry(group, row int32) int {
	c.locs = append(c.locs, variant.LocMissing)
	if !c.identitySlots {
		c.slotGroup = append(c.slotGroup, group)
	}
	if !c.identityRows {
		c.rows = append(c.rows, row)
	}
	return len(c.locs) - 1
}

// groupOf returns the slot group of entry e in this node's own columns.
func (c *VariantCursor) groupOf(e int) int32 {
	if c.identitySlots {
		return int32(e)
	}
	return c.slotGroup[e]
}

func (c *VariantCursor) setNavigated(e int, v variant.Value) {
	if v.IsNull() {
		c.locs[e] = variant.LocNull
	} else {
		c.locs[e] = variant.LocResidual
		c.residualCount++
	}
	r := c.addResidual(e)
	r.val = v
	r.decoded = true
}

// process recomputes the cursor's window state from its leaf windows (or its
// parent's residuals), then recurses into child cursors.
func (c *VariantCursor) process(n int) error {
	c.resetWindow()
	var err error
	switch {
	case c.parent == nil:
		err = c.processRoot(n)
	case c.isElements:
		err = c.processElements()
	case c.node != nil:
		err = c.processShreddedField()
	default:
		err = c.processVirtualField()
	}
	if err != nil {
		return err
	}
	if len(c.childList) > 0 {
		if err := c.decodeResiduals(); err != nil {
			return err
		}
	}
	for _, ch := range c.childList {
		if err := ch.process(n); err != nil {
			return err
		}
	}
	return nil
}

// decodeResiduals eagerly decodes the residual values that child cursors
// navigate through: all variant.LocResidual entries, plus partial-object
// leftovers when a non-shredded field cursor exists below this one.
func (c *VariantCursor) decodeResiduals() error {
	for i := range c.res {
		r := &c.res[i]
		if r.decoded || r.bytes == nil {
			continue
		}
		if c.locs[r.entry] == variant.LocTypedObject && !c.hasVirtualChild {
			continue
		}
		if _, err := c.decodeResidual(r); err != nil {
			return err
		}
	}
	return nil
}

// flatWindow reports whether the cursor can use the bulk window path: no
// slot or row indirection, and one slot per entry in every leaf window it
// reads. The value and primitive typed_value leaves of a depth-0 cursor are
// never repeated, but the presence leaf can be: it is the first leaf under
// an object or list typed_value, which may sit below a repeated level (a
// list typed_value always does).
func (c *VariantCursor) flatWindow() bool {
	return c.depth == 0 && c.identitySlots && c.identityRows &&
		(c.presenceLeaf == nil || c.presenceLeaf.maxRep == 0)
}

func (c *VariantCursor) processRoot(n int) error {
	mw := &c.reader.metaLeaf.win
	if c.flatWindow() {
		c.growEntries(n)
		c.computeOwnFlat(n, mw.defs, byte(c.reader.groupDef), variant.LocNull)
		return nil
	}
	for row := range n {
		e := c.appendEntry(int32(row), int32(row))
		if mw.defs[row] < byte(c.reader.groupDef) {
			c.locs[e] = variant.LocMissing
		} else {
			c.computeOwn(e, int32(row), variant.LocNull)
		}
	}
	return nil
}

func (c *VariantCursor) processShreddedField() error {
	p := c.parent
	n := len(p.locs)
	if c.flatWindow() && p.residualCount == 0 {
		// No parent entry needs residual navigation, so every entry is
		// resolved from this node's own columns: entries below an absent
		// parent read as missing there too (their definition levels are
		// below the maxima).
		c.growEntries(n)
		c.computeOwnFlat(n, nil, 0, variant.LocMissing)
		return nil
	}
	for e := range n {
		g := p.groupOf(e)
		e2 := c.appendEntry(g, p.rowOf(e))
		switch p.locs[e] {
		case variant.LocTypedObject:
			// A typed object always has its own column slots (navigated
			// residual entries can only be tagged null or residual).
			c.computeOwn(e2, g, variant.LocMissing)
		case variant.LocResidual:
			if r := p.residualAt(e); r != nil && r.decoded {
				c.navigateField(e2, r.val)
			}
		}
	}
	return nil
}

func (c *VariantCursor) processVirtualField() error {
	p := c.parent
	n := len(p.locs)
	if c.identityRows && len(p.res) == 0 {
		// Virtual fields exist only inside residual values; with none in
		// the parent window every entry is missing.
		c.growEntries(n)
		return nil
	}
	for e := range n {
		e2 := c.appendEntry(-1, p.rowOf(e))
		switch p.locs[e] {
		case variant.LocResidual, variant.LocTypedObject:
			if r := p.residualAt(e); r != nil && r.decoded {
				c.navigateField(e2, r.val)
			}
		}
	}
	return nil
}

func (c *VariantCursor) navigateField(e int, pv variant.Value) {
	if pv.Basic() != variant.BasicObject {
		return // stays missing
	}
	for _, f := range pv.ObjectValue().Fields {
		if f.Name == c.fieldName {
			c.setNavigated(e, f.Value)
			return
		}
	}
}

func (c *VariantCursor) processElements() error {
	p := c.parent
	var startsP, startsE []int32
	var plw *variantLeafWindow
	h := 0
	if c.node != nil {
		plw = &p.presenceLeaf.win
		startsP = plw.starts(p.depth)
		startsE = plw.starts(c.depth)
	}
	p.listOffsets = append(p.listOffsets[:0], 0)
	for e := range p.locs {
		switch p.locs[e] {
		case variant.LocTypedList:
			g := p.groupOf(e)
			// g beyond the presence column's group count means the file
			// is corrupt (sibling columns must agree); skip the entry.
			if g < 0 || c.node == nil || int(g) >= len(startsP)-1 {
				break
			}
			gs, ge := startsP[g], startsP[g+1]
			for h < len(startsE)-1 && startsE[h] < gs {
				h++
			}
			if plw.defs[gs] >= p.elemsDefAbs {
				for h < len(startsE)-1 && startsE[h] < ge {
					e2 := c.appendEntry(int32(h), p.rowOf(e))
					c.computeOwn(e2, int32(h), variant.LocNull)
					h++
				}
			}
		case variant.LocResidual:
			if r := p.residualAt(e); r != nil && r.decoded && r.val.Basic() == variant.BasicArray {
				for _, elem := range r.val.ArrayValue().Elements {
					e2 := c.appendEntry(-1, p.rowOf(e))
					c.setNavigated(e2, elem)
				}
			}
		}
		p.listOffsets = append(p.listOffsets, int32(len(c.locs)))
	}
	return nil
}

// computeOwn resolves the location of entry e from the node's own value and
// typed_value columns at slot group g. missingLoc is the tag used when both
// sides are null: variant.LocMissing for object fields, variant.LocNull for
// the root and list elements (where both-null is invalid per the spec and
// read as variant null, matching the row-based reader).
func (c *VariantCursor) computeOwn(e int, g int32, missingLoc variant.Loc) {
	var vbytes []byte
	valuePresent := false
	if c.valueLeaf != nil {
		w := &c.valueLeaf.win
		if s, ok := w.slotOf(c.depth, g); ok && w.defs[s] == c.valueMaxDef {
			d := w.denseIdx[s]
			vbytes = w.slab[w.offsets[d]:w.offsets[d+1]]
			valuePresent = true
		}
	}
	if t := c.node.typed; t != nil {
		switch t.kind {
		case shreddedTypedPrimitive:
			w := &c.typedLeaf.win
			if s, ok := w.slotOf(c.depth, g); ok && w.defs[s] == c.typedMaxDef {
				// The spec requires value to be null when a primitive
				// typed_value is present; if a non-compliant writer set
				// both, the typed side wins (readers may assume data is
				// written correctly).
				c.locs[e] = variant.LocTyped
				c.typedRows = append(c.typedRows, int32(e))
				return
			}
		case shreddedTypedObject:
			w := &c.presenceLeaf.win
			if s, ok := w.slotOf(c.depth, g); ok && w.defs[s] >= c.typedDefAbs {
				c.locs[e] = variant.LocTypedObject
				if valuePresent {
					c.addResidual(e).bytes = vbytes // partially shredded object
				}
				return
			}
		case shreddedTypedList:
			w := &c.presenceLeaf.win
			if s, ok := w.slotOf(c.depth, g); ok && w.defs[s] >= c.typedDefAbs {
				c.locs[e] = variant.LocTypedList
				return
			}
		}
	}
	if valuePresent {
		if len(vbytes) == 0 || vbytes[0] == 0x00 {
			// Variant null is a one-byte value with a zero header. Empty
			// bytes are how the plain unshredded write path stores a zero
			// variant value; the row-based reader reads them as null too.
			c.locs[e] = variant.LocNull
		} else {
			c.locs[e] = variant.LocResidual
			c.addResidual(e).bytes = vbytes
			c.residualCount++
		}
		return
	}
	c.locs[e] = missingLoc
}

// computeOwnFlat is the bulk equivalent of computeOwn for flat cursors (see
// flatWindow): entry e reads slot e of every leaf window directly. Entries
// whose gate level (the metadata column for the root) is below gateDef stay
// variant.LocMissing; growEntries pre-tagged every entry that way, so the
// loops only write the other locations.
func (c *VariantCursor) computeOwnFlat(n int, gate []byte, gateDef byte, missingLoc variant.Loc) {
	locs := c.locs
	switch {
	case c.typedLeaf != nil: // primitive typed_value
		tdefs := c.typedLeaf.win.defs
		for e := range n {
			if gate != nil && gate[e] < gateDef {
				continue
			}
			if tdefs[e] == c.typedMaxDef {
				locs[e] = variant.LocTyped
				c.typedRows = append(c.typedRows, int32(e))
				continue
			}
			c.resolveValueFlat(e, missingLoc, false)
		}
	case c.presenceLeaf != nil: // object or list typed_value
		typedLoc := variant.LocTypedObject
		isObject := c.node.typed.kind == shreddedTypedObject
		if !isObject {
			typedLoc = variant.LocTypedList
		}
		pdefs := c.presenceLeaf.win.defs
		for e := range n {
			if gate != nil && gate[e] < gateDef {
				continue
			}
			if pdefs[e] >= c.typedDefAbs {
				locs[e] = typedLoc
				if isObject {
					// A present value column holds the leftover fields of
					// a partially shredded object.
					c.resolveValueFlat(e, typedLoc, true)
				}
				continue
			}
			c.resolveValueFlat(e, missingLoc, false)
		}
	default: // no typed_value column
		for e := range n {
			if gate != nil && gate[e] < gateDef {
				continue
			}
			c.resolveValueFlat(e, missingLoc, false)
		}
	}
}

// resolveValueFlat resolves entry e of a flat cursor from the value column
// alone: fallbackLoc when the column is null there, variant.LocNull or
// variant.LocResidual otherwise. With partialObject set (entry already
// tagged variant.LocTypedObject) a present value only records its leftover
// bytes.
func (c *VariantCursor) resolveValueFlat(e int, fallbackLoc variant.Loc, partialObject bool) {
	if c.valueLeaf != nil {
		w := &c.valueLeaf.win
		if w.defs[e] == c.valueMaxDef {
			d := w.denseIdx[e]
			vbytes := w.slab[w.offsets[d]:w.offsets[d+1]]
			switch {
			case partialObject:
				c.addResidual(e).bytes = vbytes
			case len(vbytes) == 0 || vbytes[0] == 0x00:
				// See computeOwn: zero or empty bytes read as variant null.
				c.locs[e] = variant.LocNull
			default:
				c.locs[e] = variant.LocResidual
				c.addResidual(e).bytes = vbytes
				c.residualCount++
			}
			return
		}
	}
	c.locs[e] = fallbackLoc
}

// variantLeafWindow holds the current window of one leaf column in columnar
// form: one definition level (and repetition level for repeated leaves) per
// slot, and the non-null values densely in typed buffers. All buffers are
// reused across windows.
type variantLeafWindow struct {
	defs     []byte
	reps     []byte  // nil when the leaf is not repeated
	denseIdx []int32 // slot -> index in the typed buffers, -1 for null slots
	dense    int

	// starts[d] lists the first slot of every repetition-depth-d group in
	// the window (plus a final sentinel of numSlots). Depth 0 groups are
	// rows. Computed lazily per depth per window.
	startsByDepth [][]int32
	startsValid   []bool

	bools    []bool
	i32      []int32
	i64      []int64
	f32      []float32
	f64      []float64
	slab     []byte
	offsets  []uint32 // ByteArray: len == dense+1
	flba     []byte
	flbaSize int
}

func (w *variantLeafWindow) reset() {
	w.defs = w.defs[:0]
	w.reps = w.reps[:0]
	w.denseIdx = w.denseIdx[:0]
	w.dense = 0
	for i := range w.startsValid {
		w.startsValid[i] = false
	}
	w.bools = w.bools[:0]
	w.i32 = w.i32[:0]
	w.i64 = w.i64[:0]
	w.f32 = w.f32[:0]
	w.f64 = w.f64[:0]
	w.slab = w.slab[:0]
	w.offsets = append(w.offsets[:0], 0)
	w.flba = w.flba[:0]
}

// slotOf returns the first slot of depth-d group g and whether the group
// exists in the window. Sibling columns of a valid file agree on group
// counts; a missing group means the file is corrupt, and the caller treats
// the position as missing rather than panicking.
func (w *variantLeafWindow) slotOf(depth int, g int32) (int32, bool) {
	if len(w.reps) == 0 {
		// Non-repeated window: every slot is its own group at every depth,
		// so the starts mapping is the identity.
		if int(g) >= len(w.defs) {
			return 0, false
		}
		return g, true
	}
	starts := w.starts(depth)
	if int(g) >= len(starts)-1 {
		return 0, false
	}
	return starts[g], true
}

func (w *variantLeafWindow) starts(depth int) []int32 {
	for len(w.startsByDepth) <= depth {
		w.startsByDepth = append(w.startsByDepth, nil)
		w.startsValid = append(w.startsValid, false)
	}
	if w.startsValid[depth] {
		return w.startsByDepth[depth]
	}
	s := w.startsByDepth[depth][:0]
	if len(w.reps) == 0 {
		for i := range w.defs {
			s = append(s, int32(i))
		}
	} else {
		for i, rp := range w.reps {
			if int(rp) <= depth {
				s = append(s, int32(i))
			}
		}
	}
	s = append(s, int32(len(w.defs)))
	w.startsByDepth[depth] = s
	w.startsValid[depth] = true
	return s
}

// variantLeafReader reads windows of one leaf column of the variant subtree
// from its pages, extracting levels and typed data without boxing values.
type variantLeafReader struct {
	reader *VariantReader
	relCol int
	typ    Type
	kind   Kind
	maxDef byte
	maxRep byte

	opened      bool
	pages       Pages
	pendingSeek int64

	// Current page state.
	page   Page
	pdefs  []byte
	preps  []byte
	pcount int // slots in the page
	ppos   int // slots consumed
	pdense int // non-null values consumed

	// Typed data of the current page (one of these per kind), or dictionary
	// indexes when the page is dictionary-encoded.
	pi32    []int32
	pi64    []int64
	pf32    []float32
	pf64    []float64
	pba     []byte
	pbaOff  []uint32
	pflba   []byte
	pflbaN  int
	pbools  []bool
	pidx    []int32
	dictSet bool
	di32    []int32
	di64    []int64
	df32    []float32
	df64    []float64
	dba     []byte
	dbaOff  []uint32
	dflba   []byte
	dflbaN  int

	win variantLeafWindow
}

func (l *variantLeafReader) open() error {
	chunks := l.reader.rowGroup.ColumnChunks()
	col := l.reader.baseColumn + l.relCol
	if col >= len(chunks) {
		return fmt.Errorf("variant: column %d out of range", col)
	}
	l.pages = chunks[col].Pages()
	l.opened = true
	if l.reader.rowOffset > 0 && l.pendingSeek < 0 {
		l.pendingSeek = l.reader.rowOffset
	}
	return nil
}

func (l *variantLeafReader) readWindow(nRows int) error {
	if !l.opened {
		if err := l.open(); err != nil {
			return err
		}
	}
	if l.pendingSeek >= 0 {
		if l.page != nil {
			Release(l.page)
			l.page = nil
		}
		if err := l.pages.SeekToRow(l.pendingSeek); err != nil {
			return err
		}
		l.pendingSeek = -1
	}
	w := &l.win
	w.reset()
	rowsDone := 0
	started := false
	for {
		if err := l.ensurePage(); err != nil {
			if err == io.EOF {
				if started {
					rowsDone++
				}
				if rowsDone == nRows {
					return nil
				}
				return fmt.Errorf("variant: column %d ended after %d of %d rows", l.reader.baseColumn+l.relCol, rowsDone, nRows)
			}
			return err
		}
		rep := byte(0)
		if l.preps != nil {
			rep = l.preps[l.ppos]
		}
		if rep == 0 {
			if started {
				rowsDone++
				if rowsDone == nRows {
					return nil
				}
			}
			started = true
		}
		l.consumeSlot(w, rep)
	}
}

func (l *variantLeafReader) ensurePage() error {
	for l.page == nil || l.ppos == l.pcount {
		if l.page != nil {
			Release(l.page)
			l.page = nil
		}
		p, err := l.pages.ReadPage()
		if err != nil {
			return err
		}
		if err := l.setPage(p); err != nil {
			// setPage stored the page in l.page; clear it so Close does
			// not release it a second time.
			l.page = nil
			Release(p)
			return err
		}
	}
	return nil
}

func (l *variantLeafReader) setPage(p Page) error {
	l.page = p
	l.pdefs = p.DefinitionLevels()
	l.preps = p.RepetitionLevels()
	if l.pdefs != nil {
		l.pcount = len(l.pdefs)
	} else {
		l.pcount = int(p.NumValues())
	}
	l.ppos, l.pdense = 0, 0
	l.pidx = nil

	if l.kind == Boolean {
		// Boolean page data is bit-packed with an internal bit offset that
		// Data() does not expose for sliced pages, so booleans go through
		// the page's value reader instead.
		if err := l.extractBooleans(p); err != nil {
			return err
		}
		return l.checkPageValues()
	}

	data := p.Data()
	if d := p.Dictionary(); d != nil {
		l.pidx = data.Int32()
		if !l.dictSet {
			dd := d.Page().Data()
			switch l.kind {
			case Int32:
				l.di32 = dd.Int32()
			case Int64:
				l.di64 = dd.Int64()
			case Float:
				l.df32 = dd.Float()
			case Double:
				l.df64 = dd.Double()
			case ByteArray:
				l.dba, l.dbaOff = dd.ByteArray()
			case FixedLenByteArray:
				l.dflba, l.dflbaN = dd.FixedLenByteArray()
			}
			l.dictSet = true
		}
		return l.checkPageValues()
	}
	switch l.kind {
	case Int32:
		l.pi32 = data.Int32()
	case Int64:
		l.pi64 = data.Int64()
	case Float:
		l.pf32 = data.Float()
	case Double:
		l.pf64 = data.Double()
	case ByteArray:
		l.pba, l.pbaOff = data.ByteArray()
	case FixedLenByteArray:
		l.pflba, l.pflbaN = data.FixedLenByteArray()
	default:
		return fmt.Errorf("variant: unsupported page kind %s", l.kind)
	}
	return l.checkPageValues()
}

// checkPageValues verifies that the page decoded at least as many non-null
// values as its definition levels declare, so that appendValue never indexes
// past the decoded data. The page decode layer does not enforce this for
// BYTE_ARRAY, dictionary-encoded, or data page v2 pages, where a corrupt
// value section can decode short without error.
func (l *variantLeafReader) checkPageValues() error {
	needed := l.pcount
	if l.pdefs != nil {
		needed = countLevelsEqual(l.pdefs, l.maxDef)
	}
	var have int
	switch {
	case l.pidx != nil:
		have = len(l.pidx)
	case l.kind == Boolean:
		have = len(l.pbools)
	case l.kind == Int32:
		have = len(l.pi32)
	case l.kind == Int64:
		have = len(l.pi64)
	case l.kind == Float:
		have = len(l.pf32)
	case l.kind == Double:
		have = len(l.pf64)
	case l.kind == ByteArray:
		if n := len(l.pbaOff); n > 0 {
			have = n - 1
		}
	case l.kind == FixedLenByteArray:
		if l.pflbaN > 0 {
			have = len(l.pflba) / l.pflbaN
		}
	}
	if have < needed {
		return fmt.Errorf("variant: column %d page declares %d non-null values but decoded %d: %w",
			l.reader.baseColumn+l.relCol, needed, have, ErrCorrupted)
	}
	return nil
}

func (l *variantLeafReader) extractBooleans(p Page) error {
	l.pbools = l.pbools[:0]
	vr := p.Values()
	var buf [128]Value
	for {
		n, err := vr.ReadValues(buf[:])
		for _, v := range buf[:n] {
			if !v.IsNull() {
				l.pbools = append(l.pbools, v.Boolean())
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("variant: boolean page reader made no progress")
		}
	}
}

func (l *variantLeafReader) consumeSlot(w *variantLeafWindow, rep byte) {
	def := l.maxDef
	if l.pdefs != nil {
		def = l.pdefs[l.ppos]
	}
	w.defs = append(w.defs, def)
	if l.maxRep > 0 {
		w.reps = append(w.reps, rep)
	}
	if def == l.maxDef {
		w.denseIdx = append(w.denseIdx, int32(w.dense))
		w.dense++
		l.appendValue(w)
	} else {
		w.denseIdx = append(w.denseIdx, -1)
	}
	l.ppos++
}

func (l *variantLeafReader) appendValue(w *variantLeafWindow) {
	i := l.pdense
	l.pdense++
	switch l.kind {
	case Boolean:
		w.bools = append(w.bools, l.pbools[i])
	case Int32:
		if l.pidx != nil {
			w.i32 = append(w.i32, l.di32[l.pidx[i]])
		} else {
			w.i32 = append(w.i32, l.pi32[i])
		}
	case Int64:
		if l.pidx != nil {
			w.i64 = append(w.i64, l.di64[l.pidx[i]])
		} else {
			w.i64 = append(w.i64, l.pi64[i])
		}
	case Float:
		if l.pidx != nil {
			w.f32 = append(w.f32, l.df32[l.pidx[i]])
		} else {
			w.f32 = append(w.f32, l.pf32[i])
		}
	case Double:
		if l.pidx != nil {
			w.f64 = append(w.f64, l.df64[l.pidx[i]])
		} else {
			w.f64 = append(w.f64, l.pf64[i])
		}
	case ByteArray:
		var b []byte
		if l.pidx != nil {
			j := l.pidx[i]
			b = l.dba[l.dbaOff[j]:l.dbaOff[j+1]]
		} else {
			b = l.pba[l.pbaOff[i]:l.pbaOff[i+1]]
		}
		w.slab = append(w.slab, b...)
		w.offsets = append(w.offsets, uint32(len(w.slab)))
	case FixedLenByteArray:
		if l.pidx != nil {
			j := int(l.pidx[i])
			w.flba = append(w.flba, l.dflba[j*l.dflbaN:(j+1)*l.dflbaN]...)
			w.flbaSize = l.dflbaN
		} else {
			w.flba = append(w.flba, l.pflba[i*l.pflbaN:(i+1)*l.pflbaN]...)
			w.flbaSize = l.pflbaN
		}
	}
}
