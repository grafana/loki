package encoder

import (
	"context"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/jsonstring"
	"github.com/goccy/go-json/internal/runtime"
)

type compileContext struct {
	opcodeIndex uint32
	ptrIndex    int
	indent      uint32
	// escapeKey and escapeJSKey are whether the names of the fields are escaped for HTML and for JavaScript, and
	// jsKey is whether a name has a character which the escape for JavaScript escapes ( see KeyEscapeOptions ).
	escapeKey         bool
	escapeJSKey       bool
	jsKey             bool
	structTypeToCodes map[uintptr]Opcodes
	recursiveCodes    *Opcodes
	// hiddenNames are the names which the code of a recursive struct leaves out, by the opcode which jumps to it
	// from a struct it is embedded in ( see StructCode.hiddenNames ).
	hiddenNames map[*Opcode][]string
	// recursiveKeys are the keys of the structs which the recursive codes jump to, by the opcodes which jump, where
	// the key is not the type ( see StructCode.key ).
	recursiveKeys map[*Opcode]uintptr
}

func (c *compileContext) incIndent() {
	c.indent++
}

func (c *compileContext) decIndent() {
	c.indent--
}

func (c *compileContext) incIndex() {
	c.incOpcodeIndex()
	c.incPtrIndex()
}

func (c *compileContext) decIndex() {
	c.decOpcodeIndex()
	c.decPtrIndex()
}

func (c *compileContext) incOpcodeIndex() {
	c.opcodeIndex++
}

func (c *compileContext) decOpcodeIndex() {
	c.opcodeIndex--
}

func (c *compileContext) incPtrIndex() {
	c.ptrIndex++
}

func (c *compileContext) decPtrIndex() {
	c.ptrIndex--
}

const (
	bufSize = 1024
)

var (
	runtimeContextPool = sync.Pool{
		New: func() any {
			ctx := &RuntimeContext{
				Buf:   make([]byte, 0, bufSize),
				Slots: make([]uintptr, 128*slotWords),
			}
			// the escaper is set before the first encoding, of options which differ from noEscaperFlags ( see
			// run.Code ).
			ctx.option.EscaperFlags = noEscaperFlags
			ctx.Option = &ctx.option
			return ctx
		},
	}
)

// Slot is the layout of a slot of the VM. The opcodes refer to a half of a slot by its offset from the head of
// the frame: Ptr is for a pointer ( the address of a value, the context of a map, the opcode to return to ) and
// Int is for the other values ( an index, a length, the offset of a frame, an indent ).
//
// The slots are in the heap, and both halves are stored as uintptr there: a store of a pointer to the heap goes
// through the write barrier while the GC is marking, which made the encoding of a struct about 50% slower during
// that time. So the GC doesn't see the slots, and what they refer to is kept alive by others:
//   - the value passed to Marshal is kept alive by its caller, and so is everything reachable from it
//   - the value copied from an interface value is referred to by RuntimeContext
//   - the context of a map is referred to by RuntimeContext
//
// The opcodes are never freed.
type Slot struct {
	Ptr unsafe.Pointer
	Int uintptr
}

// slotWords is the number of the words of a slot.
const slotWords = 2

const (
	// recentCodeSetSets is the number of the sets of the recent opcodes, of recentCodeSetWays entries each.
	// The set of a type is the top bits of the product of its address with an odd constant, which spreads
	// the addresses of the types, which are close to each other, over the sets.
	//
	// The shape of the table is by BenchmarkVariant_RecentCodeSets and by the encoding of values whose types
	// are of the same set, on arm64 and on the amd64 machines of the CI:
	//   - a lookup which hits costs the same whatever the number of the sets ( 16 to 128 ), and a miss
	//     costs the lookup of the shared table on top: 4 ns on arm64, 8 ns on amd64;
	//   - two entries per set cost the same as one when the first entry hits, and a third and a fourth
	//     entry cost a nanosecond for every value of interface{} on arm64, whether they hit or not;
	//   - more sets only make it rarer for three types encoded by turns to be of one set: it is the case
	//     for some three of six types, which a document of map[string]interface{} has, in 8% of the
	//     binaries with 16 sets and in 2% with 32. That is not worth the memory of every context: 64 sets,
	//     1 KB, measured +1% on the whole on amd64, and 32 sets measured nothing.
	// So the table is 256 B: 16 sets of two entries.
	recentCodeSetSets      = 16
	recentCodeSetHashShift = 64 - 4
	// recentCodeSetWays is the number of the types a set holds: the ones hashed to it which were encoded
	// last. Two types encoded by turns, such as the type passed to Marshal and the type held by its values
	// of interface{}, never evict each other then, whatever their addresses are. With one entry per set,
	// they did whenever their addresses hashed to the same entry, which depends on where the binary has the
	// types, and every Marshal of them cost two lookups of the table shared by every goroutine: 20% of the
	// encoding of a small value, present or absent by the build. Three types of a set encoded by turns still
	// evict each other: a type which its set doesn't hold is then looked up in the shared table, mostly
	// without a call, and taken back into its set ( see SharedCodeSets ).
	recentCodeSetWays = 2
)

type recentCodeSet struct {
	typeptr uintptr
	codeSet *OpcodeSet
}

// recentCodeSetSet is the entries of a set, the one encoded last first.
type recentCodeSetSet [recentCodeSetWays]recentCodeSet

// SeenValue is a value recorded to detect a cycle: its address, or the pointer which it is, the length of a
// slice, which another slice of the same array is not the same value as, or -1 for a struct recorded by the
// pointer it holds ( see recordSeenValue ), and its type, as values of other types at the same address, a struct
// and its first field, are other values. A cycle passes the same values again, of
// the same types.
type SeenValue struct {
	p   unsafe.Pointer
	n   int
	typ unsafe.Pointer
}

type RuntimeContext struct {
	Buf        []byte
	MarshalBuf []byte
	Slots      []uintptr
	SeenPtr    []SeenValue
	// seenMaps are the maps recorded for the detection of cycles ( see RecordMap ).
	seenMaps   []unsafe.Pointer
	BaseIndent uint32
	// RecursiveLevel and SlotOffset are the state of the VM which only the opcodes of an interface value and of
	// a recursive type use. They are here, not in the variables of the VM: the VM keeps its variables in the
	// registers across the opcodes, and it has to restore every one of them after each call in an opcode.
	RecursiveLevel int
	SlotOffset     uintptr
	// TailLevels is the number of the values of a recursive type which are being encoded in the current frame,
	// one after the other as the last field of the previous, without a frame of their own: see the cases of
	// OpRecursive and OpRecursiveEnd of the VM. Their braces are closed one by one when the last of them ends.
	TailLevels uint32
	Prefix     []byte
	IndentStr  []byte
	Option     *Option
	// mapContexts are the contexts of the maps nested in each other, one for each level, and mapDepth is the
	// number of the maps being encoded.
	mapContexts []*MapContext
	mapDepth    int
	// nested is whether a frame was added by ReserveSlots: only such a frame uses SeenPtr. sharedSeen is whether
	// the records are the ones of an outer context, which clears them itself ( see EnterNested ).
	nested     bool
	sharedSeen bool
	// Outer is the context of the encoding which a call of MarshalEncode by a method is nested in, for the call
	// ( see EnterNested ).
	Outer *RuntimeContext
	// topValue holds the value passed to Marshal when it is stored directly in its interface value: the
	// interface value is an argument, whose address may change with the stack, so the value is copied here.
	topValue unsafe.Pointer
	// recentCodeSets are the opcodes of the types encoded last, in the sets indexed by the address of the type.
	recentCodeSets [recentCodeSetSets]recentCodeSetSet
	// recentV1Options are the options of encoding/json which change the opcodes ( see v1OpcodeOptions ) that
	// recentCodeSets were compiled for: the keys of the sets have no room for them, so the sets are emptied when an
	// encoding has others ( see SetFlags ).
	recentV1Options OptionFlag
	// value is a zero value of the type of valueCodeSet in the heap, which MarshalOf copies its argument to.
	// It is zeroed again after the encoding.
	valueCodeSet *OpcodeSet
	value        unsafe.Pointer
	// KeyName is whether the key of a map is being written, by a method or a function of marshaling.
	KeyName bool
	// V2State is the state of the calls of the v2 json package which use the context, which is kept with it: a
	// pointer to its type, which the encoder doesn't know, read without the check of a type assertion.
	V2State unsafe.Pointer
	// RescanNames is whether the output is to be read again for the names which an object has twice, for the
	// v2 semantics: a name with invalid UTF-8 was written with U+FFFD.
	RescanNames bool
	// RewriteFrom is the first offset of the output which was written again, or shortened, since it was last
	// reset, or math.MaxInt: the v2 json package, which follows the output as it grows, reads it again from there.
	RewriteFrom int
	// ValueDepth is the number of the objects and the arrays which are open around the value which a function or a
	// method of the v2 semantics is called for, in the whole output ( see AppendMarshalJSON ): the encoder which it
	// is given, and the values which it writes in a context of their own ( see runInner ), count their levels after
	// it.
	ValueDepth uint32
	// rawLevels are the levels of the walk of a raw value of the v2 semantics ( see AppendFormattedRaw ), made by
	// the first walk ( see levelsOfRaw ): the context of an encoding of v1 doesn't have them, which keeps it small
	// to make again after the GC emptied the pool.
	rawLevels *rawLevels
	// marshalTo is the encoder which MarshalJSONTo of the values of v1 writes to ( see appendMarshalTo ), made by
	// the first such value, which keeps the context of an encoding without one small, as rawLevels does.
	marshalTo *toAttachment
	// strictEscaper is the escaper of RejectInvalidUTF8Option of the options of strictIndex - 1, which records the
	// first string of invalid UTF-8 ( see SetStrictEscaper ): at the end, after the fields of every encoding.
	strictEscaper jsonstring.Escaper
	strictIndex   uint
	// option is the Option of the context, which every encoding writes: in the object of the context, as an object
	// of its own would share its cache lines with the options of other contexts, which other threads write.
	option Option
}

// levelsOfRaw returns the levels of the walks of raw values of the context ( see rawLevels ).
func (c *RuntimeContext) levelsOfRaw() *rawLevels {
	if c.rawLevels == nil {
		c.rawLevels = new(rawLevels)
	}
	return c.rawLevels
}

// Rewrote records that the output was written again, or shortened, from the offset at.
func (c *RuntimeContext) Rewrote(at int) {
	c.RewriteFrom = min(c.RewriteFrom, at)
}

// ValueAddr returns the address of the value passed to Marshal, which the data word of its interface value
// represents.
//
// The opcodes always take the address of a value. The data word of an interface value is the address
// for most of the types, but it is the value itself if the type is stored directly ( a pointer, a map,
// a struct of a single pointer, ... ). Such a value is copied to the context, and the address of the copy
// is returned.
func (c *RuntimeContext) ValueAddr(codeSet *OpcodeSet, dataWord unsafe.Pointer) unsafe.Pointer {
	if codeSet.DataWordIsAddr {
		return dataWord
	}
	c.topValue = dataWord
	return unsafe.Pointer(&c.topValue)
}

func (c *RuntimeContext) Init(p unsafe.Pointer, codelen int) {
	if len(c.Slots) < codelen*slotWords {
		c.Slots = make([]uintptr, codelen*slotWords)
	}
	c.Slots[0] = uintptr(p)
	c.SeenPtr = c.SeenPtr[:0]
	c.BaseIndent = 0
	c.RecursiveLevel = 0
	c.SlotOffset = 0
	c.TailLevels = 0
}

// ReserveSlots makes the context have the slots of the frames up to the length.
func (c *RuntimeContext) ReserveSlots(length uintptr) {
	c.nested = true
	if uintptr(len(c.Slots)) < length*slotWords {
		c.growSlots(length)
	}
}

//go:noinline
func (c *RuntimeContext) growSlots(length uintptr) {
	c.Slots = append(c.Slots, make([]uintptr, int(length)*slotWords-len(c.Slots))...)
}

// Ptr returns the pointer to the slots.
// It is unsafe.Pointer, not uintptr, so that the address of a slot is calculated by unsafe.Add,
// which the compiler folds into the addressing mode of the load / store of the slot.
func (c *RuntimeContext) Ptr() unsafe.Pointer {
	header := (*runtime.SliceHeader)(unsafe.Pointer(&c.Slots))
	return header.Data
}

func TakeRuntimeContext() *RuntimeContext {
	return runtimeContextPool.Get().(*RuntimeContext)
}

func ReleaseRuntimeContext(ctx *RuntimeContext) {
	// The context of a call must neither be kept by the pool nor be seen by the next call,
	// which may not be given a context at all.
	ctx.Option.Context = nil
	ctx.releaseValues()
	runtimeContextPool.Put(ctx)
}

// releaseValues clears every pointer to the values which were encoded, so that the pool doesn't keep them alive.
func (c *RuntimeContext) releaseValues() {
	c.topValue = nil
	// the keys of the maps which an error left open ( see ReleaseMapContext ).
	for _, m := range c.mapContexts[:c.mapDepth] {
		clear(m.Keys)
		m.Keys = m.Keys[:0]
	}
	c.mapDepth = 0
	if c.nested {
		// what only the frames of an interface value and of a recursive type use, and the maps in them.
		if c.sharedSeen {
			c.SeenPtr, c.seenMaps, c.sharedSeen = nil, nil, false
		} else {
			clear(c.SeenPtr[:cap(c.SeenPtr)])
			clear(c.seenMaps[:cap(c.seenMaps)])
			c.seenMaps = c.seenMaps[:0] // also the records which an error left
		}
		c.nested = false
	}
}

// noEscaperFlags are the options of a context whose escaper is not set: the ones of no encoding, as they set both
// RejectInvalidUTF8Option, whose escaper is set by SetStrictEscaper, and other bits.
const noEscaperFlags = ^OptionFlag(0)

// SetEscaper sets the escaper of the strings of the options, which the VM writes them by ( see StringEscaper ),
// for an encoding by the context. The VM is run by the run package, which sets it itself for the options which
// most encodings have, without a call: it calls SetStrictEscaper for RejectInvalidUTF8Option.
func (c *RuntimeContext) SetEscaper() {
	if c.Option.Flag&RejectInvalidUTF8Option != 0 {
		c.SetStrictEscaper()
		return
	}
	c.Option.Escaper, c.Option.EscaperFlags = jsonstring.EscaperOf(uint(c.Option.Flag)), c.Option.Flag
}

// SetStrictEscaper sets the escaper of RejectInvalidUTF8Option: one of the context, which records the first
// string of invalid UTF-8 ( see InvalidUTF8Output ).
//
// It is not inlined into the run of the VM, which most encodings, of other options, run without its code.
//
//go:noinline
func (c *RuntimeContext) SetStrictEscaper() {
	flags := uint(c.Option.Flag)
	if index := flags%uint(RejectInvalidUTF8Option<<1) + 1; c.strictIndex != index {
		c.strictEscaper.CopyOf(flags)
		c.strictIndex = index
	} else {
		c.strictEscaper.ClearInvalid()
	}
	if c.Option.Escaper != &c.strictEscaper {
		c.Option.Escaper = &c.strictEscaper
	}
	// the options of RejectInvalidUTF8Option, which those of another escaper are not, which then set it again.
	c.Option.EscaperFlags = c.Option.Flag
}

// HasInvalidUTF8 reports whether the encoding of RejectInvalidUTF8Option wrote a string of invalid UTF-8, which
// InvalidUTF8Output returns.
func (c *RuntimeContext) HasInvalidUTF8() bool {
	return c.strictEscaper.HasInvalid()
}

// InvalidUTF8Output returns the output before the first string of invalid UTF-8 which the encoding of
// RejectInvalidUTF8Option wrote, and true, and clears the record, which refers to the output; or false if it
// wrote none.
func (c *RuntimeContext) InvalidUTF8Output() ([]byte, bool) {
	if c.Option.Flag&RejectInvalidUTF8Option == 0 {
		return nil, false
	}
	out, invalid := c.strictEscaper.Invalid()
	c.strictEscaper.ClearInvalid()
	return out, invalid
}

// marshalerContext returns the context to call MarshalJSON(context.Context) with.
// It is never nil, also for a call which is not given a context.
func (c *RuntimeContext) marshalerContext() context.Context {
	if c.Option.Context == nil {
		return context.Background()
	}
	return c.Option.Context
}
