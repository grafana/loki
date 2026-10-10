package encoder

import (
	"context"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

type compileContext struct {
	opcodeIndex       uint32
	ptrIndex          int
	indent            uint32
	escapeKey         bool
	structTypeToCodes map[uintptr]Opcodes
	recursiveCodes    *Opcodes
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
			return &RuntimeContext{
				Buf:    make([]byte, 0, bufSize),
				Slots:  make([]uintptr, 128*slotWords),
				Option: &Option{},
			}
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
	// evict each other, which costs those lookups, not the result.
	recentCodeSetWays = 2
)

type recentCodeSet struct {
	typeptr uintptr
	codeSet *OpcodeSet
}

// recentCodeSetSet is the entries of a set, the one encoded last first.
type recentCodeSetSet [recentCodeSetWays]recentCodeSet

type RuntimeContext struct {
	Context    context.Context
	Buf        []byte
	MarshalBuf []byte
	Slots      []uintptr
	SeenPtr    []unsafe.Pointer
	BaseIndent uint32
	// RecursiveLevel and SlotOffset are the state of the VM which only the opcodes of an interface value and of
	// a recursive type use. They are here, not in the variables of the VM: the VM keeps its variables in the
	// registers across the opcodes, and it has to restore every one of them after each call in an opcode.
	RecursiveLevel int
	SlotOffset     uintptr
	// TailLevels is the number of the values of a recursive type which are being encoded in the current frame,
	// one after the other as the last field of the previous, without a frame of their own: see
	// EnterTailRecursive. Their braces are closed one by one when the last of them ends.
	TailLevels uint32
	Prefix     []byte
	IndentStr  []byte
	Option     *Option
	// mapContexts are the contexts of the maps nested in each other, one for each level, and mapDepth is the
	// number of the maps being encoded.
	mapContexts []*MapContext
	mapDepth    int
	// nested is whether a frame was added by ReserveSlots: only such a frame uses SeenPtr.
	nested bool
	// topValue holds the value passed to Marshal when it is stored directly in its interface value: the
	// interface value is an argument, whose address may change with the stack, so the value is copied here.
	topValue unsafe.Pointer
	// recentCodeSets are the opcodes of the types encoded last, in the sets indexed by the address of the type.
	recentCodeSets [recentCodeSetSets]recentCodeSetSet
	// value is a zero value of the type of valueCodeSet in the heap, which MarshalOf copies its argument to.
	// It is zeroed again after the encoding.
	valueCodeSet *OpcodeSet
	value        unsafe.Pointer
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
	c.mapDepth = 0
	if c.nested {
		// what only the frames of an interface value and of a recursive type use.
		clear(c.SeenPtr[:cap(c.SeenPtr)])
		c.nested = false
	}
}

// marshalerContext returns the context to call MarshalJSON(context.Context) with.
// It is never nil, also for a call which is not given a context.
func (c *RuntimeContext) marshalerContext() context.Context {
	if c.Option.Context == nil {
		return context.Background()
	}
	return c.Option.Context
}
