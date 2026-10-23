package decoder

import (
	"math/bits"
	"reflect"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

type RuntimeContext struct {
	Buf    []byte
	Option *Option
	// slot is where a value of interface{} is decoded before it is put into a map or a slice.
	// The decoders of the values nested in it use it too: each of them takes the value out of it
	// before the next value is decoded, and puts its own value into it only at its end.
	slot any
	// anyStack holds the elements of the arrays being decoded into []interface{}: an array pushes
	// its elements above the ones of the arrays it is nested in, and pops them at its end. The objects
	// being decoded into new maps of map[string]interface{} push their entries to entryStack, a key and its
	// value at once, so that a map is made of the size of its object ( see decodeNewStringAnyMap ).
	anyStack   []any
	entryStack []anyEntry
	// floats and strings are the slabs in which the numbers and the strings decoded into interface{}
	// are kept, so that an interface value refers to them without an allocation of its own.
	// A slot of a slab is never written again once an interface value refers to it.
	floats  []float64
	strings []string
	// input is the buffer which the input of Unmarshal is copied to, followed by a nul byte. It is reused by
	// the calls: nothing decoded refers to it, because the decoded strings are copied out of it ( makeString ).
	input []byte
	// origin is the input of the call when Buf is the reused copy of it, at the same offsets: the decoded strings
	// are copied, or refer to origin under NoCopyStringOption. It is nil when Buf is never written again after the
	// value is decoded, as the buffer of a stream: the strings refer to Buf then.
	origin []byte
	// arena is where the bytes of the decoded strings are copied to. A string refers to a part of it which is
	// never written again, so the arena is kept from a call to the next, as the slab of floats.
	arena []byte
	// unescaped is where an escaped string too long for the arena is decoded to before it is copied, which is kept
	// from a call to the next up to maxUnescapeScratchSize ( see unescapeLong ).
	unescaped []byte
	// value is a zero value of the type valueType in the heap, which UnmarshalOf decodes into
	// before it copies the result to the value of the caller.
	valueType unsafe.Pointer
	value     unsafe.Pointer
	// recentDecoders are the decoders of the types decoded last, in the sets indexed by the address of the type.
	// Only the contexts of the pool have them: a Decoder has a context of its own, which it would allocate
	// with them for every stream.
	recentDecoders *[recentDecoderSets]recentDecoderSet
	// typeError is the first type error of the decoding, which is returned at its end ( see TypeError ). It is nil
	// in a context of the pool: TypeError clears it, and DiscardTypeError when the decoding fails.
	typeError *pendingTypeError
}

const (
	// recentDecoderSets is the number of the sets of the recent decoders, of recentDecoderWays entries each,
	// as the recent opcodes of the encoder: the set of a type is the top bits of the product of its address
	// with an odd constant, and a set holds the two types hashed to it which were decoded last, so that the
	// type passed to Unmarshal and a type held by its values of interface{} never evict each other.
	recentDecoderSets      = 16
	recentDecoderHashShift = 64 - 4
	recentDecoderWays      = 2
)

type recentDecoder struct {
	typeptr uintptr
	dec     Decoder
}

// recentDecoderSet is the entries of a set, the one decoded last first.
type recentDecoderSet [recentDecoderWays]recentDecoder

// DecoderOf returns the decoder of the type, compiling it if the type is new.
//
// A runtime context remembers the decoders of the types it decoded last: the same types are decoded again
// and again in most of the programs, and this is cheaper than a lookup of the table shared by every goroutine.
// The first entry of the set is looked at here, which is inlined into the callers; the rest in lookupDecoder.
func (ctx *RuntimeContext) DecoderOf(typ unsafe.Pointer) (Decoder, error) {
	if ctx.recentDecoders == nil {
		return CompileToGetDecoder(typ)
	}
	set := &ctx.recentDecoders[(uint64(uintptr(typ))*runtime.TypeHashMultiplier)>>recentDecoderHashShift]
	if set[0].typeptr == uintptr(typ) {
		return set[0].dec, nil
	}
	return lookupDecoder(set, typ)
}

// lookupDecoder returns the decoder of the type from the second entry of its set, or from the shared table,
// compiling it if the type is new. A decoder found in the shared table takes the first entry of the set,
// and the one decoded before it is kept in the second.
func lookupDecoder(set *recentDecoderSet, typ unsafe.Pointer) (Decoder, error) {
	if set[1].typeptr == uintptr(typ) {
		return set[1].dec, nil
	}
	dec, err := CompileToGetDecoder(typ)
	if err != nil {
		return nil, err
	}
	set[1] = set[0]
	set[0] = recentDecoder{typeptr: uintptr(typ), dec: dec}
	return dec, nil
}

const (
	// minArenaChunkSize and arenaChunkSize are the sizes of the first and of the largest chunks of the arena
	// of the strings: a chunk is twice as large as the previous one, so that a context which decodes a few
	// short strings, as the one of a Decoder made for a small value, allocates little.
	minArenaChunkSize = 64
	arenaChunkSize    = 16384
	// maxArenaStringSize is the length of the longest string which is copied to the arena:
	// a longer one gets an allocation of its own, which it alone keeps alive.
	maxArenaStringSize = 512
	// maxUnescapeScratchSize is the length of the longest escaped string which is decoded into the scratch bytes of
	// a context: a longer one is decoded into bytes of its own, which the context doesn't keep.
	maxUnescapeScratchSize = 64 << 10
)

// inputPadding is the number of the bytes the buffer of the input has after its nul byte, which the decoders
// may read: the words of an object key are read from any byte of the buffer ( see structDecoder.Decode ).
const inputPadding = 16

// SetInput copies data to the buffer of the context, followed by a nul byte which ends every scan,
// and makes it the buffer to decode.
func (ctx *RuntimeContext) SetInput(data []byte) []byte {
	n := len(data) + 1
	if cap(ctx.input) < n+inputPadding {
		// at least a cache line, which the buffer of another context doesn't share: it is written for every call.
		ctx.input = make([]byte, n, max(n+inputPadding, cacheLineSize))
	}
	buf := ctx.input[:n]
	copy(buf, data)
	buf[len(data)] = nul
	ctx.Buf = buf
	ctx.origin = data
	return buf
}

// makeString returns the string of lit, which the string decoder returned from Buf: lit is either a part of Buf,
// which is reused by the next call and has the bytes of the input ( Buf is never written ), or a new slice of its
// own ( an escape was decoded, or an invalid UTF-8 sequence was replaced ).
//
// Only the buffer of Unmarshal is reused ( origin is its input ): the strings are copied out of it, or refer to
// the input under NoCopyStringOption. The other buffers, as the one of a stream, are never written again after
// a value is decoded, so the strings refer to them.
func (ctx *RuntimeContext) makeString(lit []byte) string {
	if len(lit) == 0 {
		return ""
	}
	if ctx.origin == nil {
		return unsafe.String(unsafe.SliceData(lit), len(lit))
	}
	if (ctx.Option.Flags & NoCopyStringOption) != 0 {
		return ctx.referString(lit)
	}
	return ctx.copyString(lit)
}

// isPartOf reports whether b is a part of buf.
func isPartOf(b, buf []byte) bool {
	base := uintptr(unsafe.Pointer(unsafe.SliceData(buf)))
	p := uintptr(unsafe.Pointer(unsafe.SliceData(b)))
	return base <= p && p < base+uintptr(len(buf))
}

// referString returns a string which refers to the input, where lit is a part of Buf, without a copy.
func (ctx *RuntimeContext) referString(lit []byte) string {
	if !isPartOf(lit, ctx.Buf) {
		// a slice of its own
		return unsafe.String(unsafe.SliceData(lit), len(lit))
	}
	off := uintptr(unsafe.Pointer(unsafe.SliceData(lit))) - uintptr(unsafe.Pointer(unsafe.SliceData(ctx.Buf)))
	return unsafe.String(&ctx.origin[off], len(lit))
}

// reserveArena returns n bytes of the arena after its end, which a string may be written to:
// the caller extends the arena by the length of the string it wrote.
func (ctx *RuntimeContext) reserveArena(n int) []byte {
	if cap(ctx.arena)-len(ctx.arena) < n {
		size := min(max(2*cap(ctx.arena), minArenaChunkSize), arenaChunkSize)
		ctx.arena = make([]byte, 0, max(size, n))
	}
	return ctx.arena[len(ctx.arena) : len(ctx.arena)+n]
}

// copyString returns a copy of lit, in the arena unless it is long.
func (ctx *RuntimeContext) copyString(lit []byte) string {
	if len(lit) > maxArenaStringSize {
		return string(lit)
	}
	dst := ctx.reserveArena(len(lit))
	copy(dst, lit)
	ctx.arena = ctx.arena[:len(ctx.arena)+len(lit)]
	return unsafe.String(unsafe.SliceData(dst), len(lit))
}

// boxSlabSize is the number of the values a slab of floats or strings holds.
const boxSlabSize = 32

var (
	float64TypePtr = runtime.TypePtr(reflect.TypeOf(float64(0)))
	stringTypePtr  = runtime.TypePtr(reflect.TypeOf(""))
)

// boxFloat returns f as an interface value, which refers to a slot of the slab of floats.
func (ctx *RuntimeContext) boxFloat(f float64) any {
	if len(ctx.floats) == cap(ctx.floats) {
		ctx.floats = make([]float64, 0, boxSlabSize)
	}
	ctx.floats = append(ctx.floats, f)
	return *(*any)(unsafe.Pointer(&emptyInterface{typ: float64TypePtr, ptr: unsafe.Pointer(&ctx.floats[len(ctx.floats)-1])}))
}

// boxString returns s as an interface value, which refers to a slot of the slab of strings.
func (ctx *RuntimeContext) boxString(s string) any {
	if len(ctx.strings) == cap(ctx.strings) {
		ctx.strings = make([]string, 0, boxSlabSize)
	}
	ctx.strings = append(ctx.strings, s)
	return *(*any)(unsafe.Pointer(&emptyInterface{typ: stringTypePtr, ptr: unsafe.Pointer(&ctx.strings[len(ctx.strings)-1])}))
}

// popAny removes the elements of anyStack from base, clearing them so that the stack keeps nothing alive.
func (ctx *RuntimeContext) popAny(base int) {
	clear(ctx.anyStack[base:])
	ctx.anyStack = ctx.anyStack[:base]
}

// anyEntry is an entry of an object being decoded into a map[string]interface{}.
type anyEntry struct {
	key   string
	value any
}

// popEntries removes the entries of entryStack from base, clearing them so that the stack keeps nothing alive.
func (ctx *RuntimeContext) popEntries(base int) {
	clear(ctx.entryStack[base:])
	ctx.entryStack = ctx.entryStack[:base]
}

// cacheLineSize is the size of the cache lines the contexts are kept apart by: 128 bytes, which is the line of
// the Apple M processors and two lines of amd64, whose adjacent lines are fetched together.
const cacheLineSize = 128

// pooledContext is a context of the pool with its option, in one allocation which fills its cache lines: the
// contexts of the goroutines which decode at the same time are written for every call, so a context which
// shared a cache line with another made each goroutine wait for the other.
type pooledContext struct {
	ctx RuntimeContext
	opt Option
	_   [cacheLineSize - (unsafe.Sizeof(RuntimeContext{})+unsafe.Sizeof(Option{}))%cacheLineSize]byte
}

var (
	runtimeContextPool = sync.Pool{
		New: func() any {
			c := &pooledContext{}
			c.ctx.Option = &c.opt
			c.ctx.recentDecoders = &[recentDecoderSets]recentDecoderSet{}
			return &c.ctx
		},
	}
)

func TakeRuntimeContext() *RuntimeContext {
	return runtimeContextPool.Get().(*RuntimeContext)
}

func ReleaseRuntimeContext(ctx *RuntimeContext) {
	// Nothing of the call is kept: the input of the caller may be referred to by the decoded strings.
	ctx.Buf = nil
	ctx.origin = nil
	ctx.slot = nil
	ctx.popAny(0)
	ctx.popEntries(0)
	// The strings refer to the input: the slab is not kept, so that a context in the pool doesn't keep
	// the input of a previous call alive. The slab of floats refers to nothing and is kept.
	ctx.strings = nil
	runtimeContextPool.Put(ctx)
}

var (
	isWhiteSpace = [256]bool{}
)

func init() {
	isWhiteSpace[' '] = true
	isWhiteSpace['\n'] = true
	isWhiteSpace['\t'] = true
	isWhiteSpace['\r'] = true
}

func char(ptr unsafe.Pointer, offset int64) byte {
	return *(*byte)(unsafe.Add(ptr, offset))
}

func skipWhiteSpace(buf []byte, cursor int64) int64 {
	for isWhiteSpace[buf[cursor]] {
		cursor++
	}
	return cursor
}

// skipStringDecoder scans the strings which skipValue skips.
var skipStringDecoder = newStringDecoder("", "")

// skipString returns the position after the string at cursor. A string without an escape is skipped word by
// word up to its quote, where the buffer has room for the words ( the nul byte at its end stops them ), and, where the CPU has a SIMD scan, for its
// first 64 bytes, after which the rest of a long string is scanned by SIMD ( see scanStringRest ). Any other
// string is scanned as a string is decoded, and validated so. It is a function of its own, so that the code of
// skipValue for the other values is the same whatever a string takes.
func skipString(buf []byte, cursor int64) (int64, error) {
	start := cursor + 1
	wordsEnd := int64(len(buf))
	if hasStringSIMD {
		wordsEnd = min(wordsEnd, start+64)
	}
	c := start
	for ; c+8 <= wordsEnd; c += 8 {
		if special := keyEndBytes(load64(buf, c)); special != 0 {
			c += int64(bits.TrailingZeros64(special) / 8)
			if buf[c] == '"' {
				return c + 1, nil
			}
			// an escape or a byte which is not valid in a string
			return skipStringByScan(buf, cursor)
		}
	}
	if c+8 > int64(len(buf)) {
		// the end of the buffer, whose last bytes are scanned as a string
		return skipStringByScan(buf, cursor)
	}
	_, next, _, err := skipStringDecoder.scanStringRest(buf, buf[start:c], c, stringInfo{firstEscape: -1})
	if err != nil {
		return 0, err
	}
	return next, nil
}

// skipStringByScan is skipString for any string, which it scans as a string is decoded.
func skipStringByScan(buf []byte, cursor int64) (int64, error) {
	literal, next, info, err := skipStringDecoder.scanString(buf, cursor)
	if next < 0 {
		_, next, _, err = skipStringDecoder.scanStringRest(buf, literal, -next-1, info)
	}
	if err != nil {
		return 0, err
	}
	return next, nil
}

func skipValue(buf []byte, cursor, depth int64) (int64, error) {
	for {
		switch buf[cursor] {
		case ' ', '\t', '\n', '\r':
			cursor++
			continue
		case '{', '[':
			// the grammar of the object or the array, by skipFast without the call of skipGrammarFast
			end, level, objects, resume, ev := skipFast(buf, cursor, 0, 0, resumeValue, skipMaxLevel(depth))
			if ev == skipDone {
				return end, nil
			}
			return skipGrammarEvent(buf, end, depth, level, objects, resume, ev)
		case '"':
			end, err := skipString(buf, cursor)
			if err != nil {
				return 0, stringError(buf, cursor, err)
			}
			return end, nil
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			// an integer, which most numbers are, without a call
			start := cursor
			cursor++
			for buf[cursor]-'0' <= 9 {
				cursor++
			}
			if c := buf[cursor]; c == '.' || c == 'e' || c == 'E' {
				return skipNumberRest(buf, start, cursor)
			}
			return cursor, nil
		case '-', '0':
			return skipNumber(buf, cursor)
		case 't':
			if buf[cursor+1] != 'r' || buf[cursor+2] != 'u' || buf[cursor+3] != 'e' {
				return 0, literalSyntaxError(buf, cursor, "true")
			}
			return cursor + 4, nil
		case 'f':
			if buf[cursor+1] != 'a' || buf[cursor+2] != 'l' || buf[cursor+3] != 's' || buf[cursor+4] != 'e' {
				return 0, literalSyntaxError(buf, cursor, "false")
			}
			return cursor + 5, nil
		case 'n':
			if buf[cursor+1] != 'u' || buf[cursor+2] != 'l' || buf[cursor+3] != 'l' {
				return 0, literalSyntaxError(buf, cursor, "null")
			}
			return cursor + 4, nil
		default:
			return 0, syntaxErrorAt(buf, cursor, whereValue)
		}
	}
}

func validateTrue(buf []byte, cursor int64) error {
	if cursor+3 >= int64(len(buf)) {
		return errors.ErrUnexpectedEndOfJSON("true", cursor)
	}
	if buf[cursor+1] != 'r' {
		return errors.ErrInvalidCharacter(buf[cursor+1], "true", cursor+1)
	}
	if buf[cursor+2] != 'u' {
		return errors.ErrInvalidCharacter(buf[cursor+2], "true", cursor+2)
	}
	if buf[cursor+3] != 'e' {
		return errors.ErrInvalidCharacter(buf[cursor+3], "true", cursor+3)
	}
	return nil
}

func validateFalse(buf []byte, cursor int64) error {
	if cursor+4 >= int64(len(buf)) {
		return errors.ErrUnexpectedEndOfJSON("false", cursor)
	}
	if buf[cursor+1] != 'a' {
		return errors.ErrInvalidCharacter(buf[cursor+1], "false", cursor+1)
	}
	if buf[cursor+2] != 'l' {
		return errors.ErrInvalidCharacter(buf[cursor+2], "false", cursor+2)
	}
	if buf[cursor+3] != 's' {
		return errors.ErrInvalidCharacter(buf[cursor+3], "false", cursor+3)
	}
	if buf[cursor+4] != 'e' {
		return errors.ErrInvalidCharacter(buf[cursor+4], "false", cursor+4)
	}
	return nil
}

func validateNull(buf []byte, cursor int64) error {
	if cursor+3 >= int64(len(buf)) {
		return errors.ErrUnexpectedEndOfJSON("null", cursor)
	}
	if buf[cursor+1] != 'u' {
		return errors.ErrInvalidCharacter(buf[cursor+1], "null", cursor+1)
	}
	if buf[cursor+2] != 'l' {
		return errors.ErrInvalidCharacter(buf[cursor+2], "null", cursor+2)
	}
	if buf[cursor+3] != 'l' {
		return errors.ErrInvalidCharacter(buf[cursor+3], "null", cursor+3)
	}
	return nil
}

// valuePools are the zero values of every type which UnmarshalOf decoded, besides the one a context keeps.
var valuePools runtime.TypeCache[sync.Pool]

// TakeValue returns the address of a zero value of typ in the heap, which the context keeps for the next call
// with the same type. typ is given as its type descriptor too, which is how it is looked up.
func (ctx *RuntimeContext) TakeValue(typ reflect.Type, typeptr unsafe.Pointer) unsafe.Pointer {
	if ctx.valueType == typeptr {
		return ctx.value
	}
	if ctx.valueType != nil {
		valuePool(ctx.valueType).Put(ctx.value)
	}
	ctx.valueType = typeptr
	if v := valuePool(typeptr).Get(); v != nil {
		ctx.value = v.(unsafe.Pointer)
	} else {
		ctx.value = newValue(typ)
	}
	return ctx.value
}

func valuePool(typeptr unsafe.Pointer) *sync.Pool {
	if pool := valuePools.Load(uintptr(typeptr)); pool != nil {
		return pool
	}
	return valuePools.Store(uintptr(typeptr), &sync.Pool{})
}
