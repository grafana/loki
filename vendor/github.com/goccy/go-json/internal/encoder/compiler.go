package encoder

import (
	"context"
	"encoding"
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

// CompileToGetCodeSet returns the opcodes of the type, compiling them if the type is new.
func CompileToGetCodeSet(ctx *RuntimeContext, typeptr uintptr) (*OpcodeSet, error) {
	if codeSet := ctx.RecentCodeSet(typeptr); codeSet != nil {
		return codeSet, nil
	}
	if ctx.Option.Flag&MarshalFuncsOption != 0 {
		return ctx.Option.Funcs.codeSet(typeptr, ctx.compileMode())
	}
	// a context which may filter the fields finds the type in its set here, as RecentCodeSet doesn't for it.
	key := ctx.codeSetKey(typeptr)
	set := &ctx.recentCodeSets[recentCodeSetIndex(key)]
	for i := range set {
		if set[i].typeptr == key {
			return getFilteredCodeSetIfNeeded(ctx, set[i].codeSet)
		}
	}
	if ctx.Option.Flag&UncachedOption == 0 {
		if codeSet := ctx.SharedCodeSets().Load(typeptr); codeSet != nil {
			ctx.RememberCodeSet(typeptr, codeSet)
			return codeSet, nil
		}
	}
	codeSet, err := compileToGetUnfilteredCodeSet(typeptr, ctx.compileMode())
	if err != nil {
		return nil, err
	}
	ctx.RememberCodeSet(typeptr, codeSet)
	return getFilteredCodeSetIfNeeded(ctx, codeSet)
}

// compileMode is how the opcodes of a type are compiled, which the options of a context choose: the fields of a
// struct in their order or ordered by the encoder, the semantics of v1 or of v2, the options of v2 which change the
// opcodes of a type, and the options of encoding/json which do, for v2. The opcodes of each mode are cached apart.
type compileMode uint16

const (
	modeOptimizeFieldOrder   compileMode = 1 << iota // OptimizeFieldOrderOption
	modeV2                                           // V2Option
	modeStringifyNumbers                             // StringifyNumbersOption, with V2Option
	modeOmitZeroStructFields                         // OmitZeroStructFieldsOption, with V2Option
	modeStringTag                                    // StringTagOption, whose numbers are written as modeStringifyNumbers writes them

	// compileModes is the number of the modes of cachedOpcodeSets, every set of the modes above, whose bits are below
	// this one: the ones without the options of encoding/json below.
	compileModes = 1 << iota
)

// The modes of the options of encoding/json, with V2Option, above the ones of cachedOpcodeSets, in the order of their
// options from CallMethodsWithLegacySemanticsOption: their opcodes are cached in v1OptionCodeSets, apart from the
// tables shared by every goroutine, which have no room for them, as few programs use them.
const (
	modeCallMethodsWithLegacySemantics compileMode = compileModes << iota
	modeFormatByteArrayAsArray
	modeFormatBytesWithLegacySemantics
	modeFormatDurationAsNano
	modeReportErrorsWithLegacySemantics
	modeStringifyWithLegacySemantics
	modeOmitEmptyWithLegacySemantics

	v1OptionModes = modeOmitEmptyWithLegacySemantics<<1 - modeCallMethodsWithLegacySemantics
)

// the last of the modes of encoding/json is as far from the first as its option is, as both are made by iota in the
// same order: the length of the array type is 0 then, and it is another type, or a negative constant, otherwise.
var _ [0]struct{} = [OmitEmptyWithLegacySemanticsOption/CallMethodsWithLegacySemanticsOption -
	OptionFlag(modeOmitEmptyWithLegacySemantics/modeCallMethodsWithLegacySemantics)]struct{}{}

// the options of the compile mode are adjacent bits, from OptimizeFieldOrderOption.
var _ [0]struct{} = [OptimizeFieldOrderOption*OptionFlag(modeOptimizeFieldOrder) - OptimizeFieldOrderOption +
	(OptimizeFieldOrderOption*OptionFlag(modeV2) - V2Option) +
	(OptimizeFieldOrderOption*OptionFlag(modeStringifyNumbers) - StringifyNumbersOption) +
	(OptimizeFieldOrderOption*OptionFlag(modeOmitZeroStructFields) - OmitZeroStructFieldsOption) +
	(OptimizeFieldOrderOption*OptionFlag(modeStringTag) - StringTagOption)]struct{}{}

// compileMode is the compile mode of the options of c.
func (c *RuntimeContext) compileMode() compileMode {
	return compileMode(c.Option.Flag/OptimizeFieldOrderOption&(compileModes-1)) |
		compileMode(c.Option.Flag&v1OpcodeOptions/CallMethodsWithLegacySemanticsOption)*modeCallMethodsWithLegacySemantics
}

// codeSetKey is what the opcodes of a type are looked up by in a context: the address of the type plus the
// compile mode without the options of encoding/json, which the recent opcodes are emptied for when they change ( see
// SetFlags ). A type descriptor is larger than the number of the modes, so the key of a type in a mode is not the key
// of another type in any mode.
func (c *RuntimeContext) codeSetKey(typeptr uintptr) uintptr {
	return typeptr + uintptr(c.Option.Flag/OptimizeFieldOrderOption&(compileModes-1))
}

// RecentCodeSet returns the opcodes of the type if the context encoded it recently and has no context which
// may filter the fields, or nil: then, for a context which doesn't filter the fields, the first entry of the type
// in SharedCodeSets ( LoadFirst ), and CompileToGetCodeSet if that has another type.
//
// A runtime context remembers the opcodes of the types it encoded last: the same types are encoded again
// and again in most of the programs, and this is cheaper than a lookup of the table shared by every goroutine.
// This is inlined into the callers: the values of interface{} come through it one by one.
func (c *RuntimeContext) RecentCodeSet(typeptr uintptr) *OpcodeSet {
	key := c.codeSetKey(typeptr)
	set := &c.recentCodeSets[recentCodeSetIndex(key)]
	if c.Option.Flag&recentUncachedOption == 0 {
		if set[0].typeptr == key {
			return set[0].codeSet
		}
		if set[1].typeptr == key {
			return set[1].codeSet
		}
	}
	return nil
}

// SharedCodeSets returns the table shared by every goroutine which has the opcodes of the context, of its compile
// mode. Its opcodes are not filtered by a field query, nor of the options of encoding/json, which have no such table:
// a context of them ( see UncachedOption ) is left to CompileToGetCodeSet, and the callers look at this table only
// for the other contexts.
//
// Which types share a set of the recent opcodes depends on where the binary has them, and three types of a set
// encoded by turns, as the values of a map[string]interface{} are, evict each other. The VM looks such a type up in
// the entry it is hashed to in this table ( LoadFirst ), without a call, and gives the opcodes it finds to
// RememberCodeSet; only a type which another type has that entry of goes on to CompileToGetCodeSet, whose lookup
// of this table looks at the next entries without a call as well ( Load ). A full lookup in the VM measured
// slower even when the recent opcodes hit, as its loop changed the code of the VM around them.
func (c *RuntimeContext) SharedCodeSets() *runtime.TypeCache[OpcodeSet] {
	return &cachedOpcodeSets[c.Option.Flag/OptimizeFieldOrderOption&(compileModes-1)]
}

// RememberCodeSet puts the opcodes of the type in the first entry of its set in the context: the one encoded
// before it is kept in the second.
func (c *RuntimeContext) RememberCodeSet(typeptr uintptr, codeSet *OpcodeSet) {
	key := c.codeSetKey(typeptr)
	set := &c.recentCodeSets[recentCodeSetIndex(key)]
	set[1] = set[0]
	set[0] = recentCodeSet{typeptr: key, codeSet: codeSet}
}

// SetFlags sets the options of ctx for an encoding of v2, which every setting of them which may change the options of
// encoding/json is made by, the others adding or dropping other options only: the recent opcodes are emptied if they
// were compiled for other options of encoding/json which change the opcodes, which their keys have no room for
// ( see recentV1Options ), by a call of its own, so that SetFlags is inlined into the callers.
func SetFlags(ctx *RuntimeContext, flags OptionFlag) {
	ctx.Option.Flag = flags
	if v1Options := flags & v1OpcodeOptions; v1Options != ctx.recentV1Options {
		emptyRecentCodeSets(ctx, v1Options)
	}
}

// emptyRecentCodeSets empties the recent opcodes of ctx, which are compiled for the options v1Options of encoding/json
// from now on.
func emptyRecentCodeSets(ctx *RuntimeContext, v1Options OptionFlag) {
	ctx.recentCodeSets = [recentCodeSetSets]recentCodeSetSet{}
	ctx.recentV1Options = v1Options
}

func recentCodeSetIndex(typeptr uintptr) uint64 {
	return (uint64(typeptr) * runtime.TypeHashMultiplier) >> recentCodeSetHashShift
}

// compileToGetUnfilteredCodeSet is CompileToGetCodeSet without the filter by the field query of the context.
// The opcodes of each compile mode are cached apart.
func compileToGetUnfilteredCodeSet(typeptr uintptr, mode compileMode) (*OpcodeSet, error) {
	if mode&v1OptionModes != 0 {
		return codeSetIn(&v1OptionCodeSets, typeptr, mode, nil)
	}
	cache := &cachedOpcodeSets[mode]
	if codeSet := cache.Load(typeptr); codeSet != nil {
		return codeSet, nil
	}
	codeSet, err := newCompiler(mode).compile(typeptr)
	if err != nil {
		return nil, err
	}
	return cache.Store(typeptr, codeSet), nil
}

// modeKey is what the opcodes of a type in a compile mode are cached by in a sync.Map: the ones of the modes with
// options of encoding/json ( see v1OptionCodeSets ), and the ones of the functions of marshaling ( see MarshalFuncs ).
type modeKey struct {
	typeptr uintptr
	mode    compileMode
}

// v1OptionCodeSets are the opcodes of the modes with options of encoding/json ( see v1OptionModes ).
var v1OptionCodeSets sync.Map // map[modeKey]*OpcodeSet

// codeSetIn returns the opcodes of the type in mode from m, compiling them for the functions funcs, or none, if the
// type is new.
func codeSetIn(m *sync.Map, typeptr uintptr, mode compileMode, funcs *MarshalFuncs) (*OpcodeSet, error) {
	key := modeKey{typeptr: typeptr, mode: mode}
	if v, ok := m.Load(key); ok {
		return v.(*OpcodeSet), nil
	}
	c := newCompiler(mode)
	c.funcs = funcs
	codeSet, err := c.compile(typeptr)
	if err != nil {
		return nil, err
	}
	v, _ := m.LoadOrStore(key, codeSet)
	return v.(*OpcodeSet), nil
}

type marshalerContext interface {
	MarshalJSON(context.Context) ([]byte, error)
}

var (
	marshalJSONType        = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	marshalJSONContextType = reflect.TypeOf((*marshalerContext)(nil)).Elem()
	marshalTextType        = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	jsonNumberType         = reflect.TypeOf(json.Number(""))
	// cachedOpcodeSets are the opcodes of the types, of each compile mode.
	cachedOpcodeSets [compileModes]runtime.TypeCache[OpcodeSet]
)

func getFilteredCodeSetIfNeeded(ctx *RuntimeContext, codeSet *OpcodeSet) (*OpcodeSet, error) {
	if (ctx.Option.Flag & ContextOption) == 0 {
		return codeSet, nil
	}
	query := FieldQueryFromContext(ctx.Option.Context)
	if query == nil {
		return codeSet, nil
	}
	ctx.Option.Flag |= FieldQueryOption
	cacheCodeSet := codeSet.getQueryCache(query.Hash())
	if cacheCodeSet != nil {
		return cacheCodeSet, nil
	}
	// the fields of the code are already in their order: the compiler orders only the fields of the recursive
	// structs it compiles when it links them, as the code was compiled.
	compiler := newCompiler(ctx.compileMode())
	compiler.isFiltered = true
	queryCodeSet, err := compiler.codeToOpcodeSet(codeSet.Type, codeSet.Code.Filter(query))
	if err != nil {
		return nil, err
	}
	codeSet.setQueryCache(query.Hash(), queryCodeSet)
	return queryCodeSet, nil
}

type Compiler struct {
	structTypeToCode map[uintptr]*StructCode
	// optimizeFieldOrder is whether the fields of a struct are ordered as they are encoded fastest.
	optimizeFieldOrder bool
	// v2 is whether the opcodes have the semantics of encoding/json/v2.
	v2 bool
	// stringifyNumbers is whether the numbers are JSON strings ( StringifyNumbersOption ).
	stringifyNumbers bool
	// omitZeroStructFields is whether every field of a struct is omitted if it is zero ( OmitZeroStructFieldsOption ).
	omitZeroStructFields bool
	// stringTag is whether the value is the one of a field of the `string` option ( StringTagOption, see
	// stringTagCode ).
	stringTag bool
	// v1Options are the options of encoding/json which the opcodes are compiled for ( see v1OptionModes ).
	v1Options compileMode
	// embeddingChain is the types of the structs whose fields are written to the JSON object being compiled:
	// the struct of the object and the structs embedded in it, down to the one being compiled.
	embeddingChain []uintptr
	// structFieldCounts is, for a struct which has been compiled as a value of its own ( not embedded ), how
	// many fields its code has, with the fields of the structs in it: see sharedStructFieldCount.
	structFieldCounts map[uintptr]int
	// fieldCount is how many fields have been compiled.
	fieldCount int
	// nextIsEmbedded is whether the struct compiled next is an embedded struct, whose fields are written to
	// the JSON object of the struct which embeds it.
	nextIsEmbedded bool
	// isFiltered is whether the code being compiled to opcodes is filtered by a field query: then the opcodes
	// which a struct has in one place are not the ones of the struct in another.
	isFiltered bool
	// valuesOnPath are the named slice, array, map and pointer types whose codes are being compiled: a value of
	// one of them in itself is encoded by its type at run time ( see recursiveValueCode ).
	valuesOnPath map[uintptr]struct{}
	// v2Object is the JSON object whose struct is being compiled, for the v2 semantics: its members, and the
	// index of the embedded struct being compiled ( see v2StructTags ).
	v2Object *v2Object
	// v2EmbedPath is the index of the embedded field being compiled, whose struct is a part of v2Object, or nil.
	v2EmbedPath []int
	// v2DefaultType is the type whose default representation is compiled after the functions of marshaling and the
	// method v2DefaultAfter, which declined to write a value of it ( see appendDefault ). For v1, it is the type
	// whose MarshalJSONTo declined, compiled after the method until its value is reached ( see appendDeclined and
	// typeToCodeWithPtr ).
	v2DefaultType  reflect.Type
	v2DefaultAfter int
	// funcs are the functions of marshaling of the opcodes, of MarshalFuncsOption.
	funcs *MarshalFuncs
}

// recursiveValueCode returns the code of a value of typ if typ is a slice, an array, a map or a pointer type whose
// code is being compiled, as a value of itself: such a type, as `type T []T`, would have a code of an infinite
// size, which a struct type avoids by a jump to the code of the struct ( see linkRecursiveCode ). Its value is
// encoded as a value of interface{} of typ would be, by the opcodes of typ, which are compiled when the first
// value is encoded. They take the address of the value, or, for a pointer type compiled to the value it points to
// ( see compilesToPointee ), the pointer: the code returned for such a type is a PtrCode, which loads it first.
func (c *Compiler) recursiveValueCode(typ reflect.Type, addressable bool) (Code, bool) {
	if _, ok := c.valuesOnPath[uintptr(runtime.TypePtr(typ))]; !ok {
		return nil, false
	}
	if c.compilesToPointee(typ) {
		return &PtrCode{typ: typ, value: &InterfaceCode{typ: typ, static: true}, ptrNum: 1}, true
	}
	if addressable && c.v1Options&modeCallMethodsWithLegacySemantics != 0 && c.addressableMatters(typ) {
		// with CallMethodsWithLegacySemantics, the opcodes of the type, which a static interface writes the value by,
		// are the ones of a value given by itself, which is not addressable: an addressable value whose code differs
		// is written by its default representation, addressable, as appendDefault compiles it.
		return c.appendFuncCode(typ, appendAddressable(typ)), true
	}
	return &InterfaceCode{typ: typ, static: true}, true
}

// addressableMatters reports whether the code of a value of typ differs as the value is addressable or not, with
// CallMethodsWithLegacySemantics: the method which writes the value, or a value in it which is addressable as it is
// ( an element of an array, a field of a struct which is a member of its JSON object ), is not the same. The values
// in it are finite: a type can't hold itself but through a pointer, a slice, a map or an interface, whose values are
// addressable or not whatever it is.
func (c *Compiler) addressableMatters(typ reflect.Type) bool {
	if m := c.v2MethodOfAt(typ, noMethod, true); m != c.v2MethodOfAt(typ, noMethod, false) {
		return true
	} else if m != noMethod {
		// written by the method, whatever is in it
		return false
	}
	switch typ.Kind() {
	case reflect.Array:
		return c.addressableMatters(typ.Elem())
	case reflect.Struct:
		for _, f := range v2FieldsOf(typ).List {
			if c.addressableMatters(f.Type) {
				return true
			}
		}
	}
	return false
}

// compilesToPointee is whether the opcodes of typ are the ones of the value it points to: a pointer is the address
// of a value. A pointer which a function of marshaling takes is a value of its own, whose address the function is
// given.
func (c *Compiler) compilesToPointee(typ reflect.Type) bool {
	return typ.Kind() == reflect.Ptr && len(c.funcs.funcsOf(typ)) == 0
}

// enterValue records that the code of typ is being compiled, if typ may be a value of itself ( see
// recursiveValueCode ), and returns the function which records the end of it.
func (c *Compiler) enterValue(typ reflect.Type) func() {
	switch typ.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Map:
		if typ.Name() == "" {
			// a type which is a value of itself is a named type: an unnamed type can't refer to itself.
			return func() {}
		}
	default:
		return func() {}
	}
	if c.valuesOnPath == nil {
		c.valuesOnPath = map[uintptr]struct{}{}
	}
	key := uintptr(runtime.TypePtr(typ))
	c.valuesOnPath[key] = struct{}{}
	return func() { delete(c.valuesOnPath, key) }
}

// sharedStructFieldCount is how many fields the code of a struct has, with the fields of the structs in it,
// above which the struct is encoded, where it is again a value in the same type, by a jump to one code of it
// as a recursive struct is, instead of by a copy of its code. A type whose structs refer to each other in
// many places would otherwise have a code of a size exponential to their depth, which takes the memory and
// the time of compiling it ( the structs of an API with many resources expanded into each other ). A smaller
// struct is copied, which saves the jump for each value.
const sharedStructFieldCount = 128

func newCompiler(mode compileMode) *Compiler {
	return &Compiler{
		structTypeToCode:     map[uintptr]*StructCode{},
		optimizeFieldOrder:   mode&modeOptimizeFieldOrder != 0,
		v2:                   mode&modeV2 != 0,
		stringifyNumbers:     mode&(modeStringifyNumbers|modeStringTag) != 0,
		omitZeroStructFields: mode&modeOmitZeroStructFields != 0,
		stringTag:            mode&modeStringTag != 0,
		v1Options:            mode & v1OptionModes,
		structFieldCounts:    map[uintptr]int{},
	}
}

func (c *Compiler) compile(typeptr uintptr) (*OpcodeSet, error) {
	// noescape trick for header.typ ( reflect.*rtype )
	typ := runtime.TypeOfPtr(*(*unsafe.Pointer)(unsafe.Pointer(&typeptr)))
	if c.compilesToPointee(typ) {
		code, err := c.pointeeCode(typ.Elem())
		if err != nil {
			return nil, err
		}
		return c.codeToOpcodeSet(typ, code)
	}
	code, err := c.valueCode(typ)
	if err != nil {
		return nil, err
	}
	if iface, ok := code.(*InterfaceCode); ok && iface.static && iface.typ == typ {
		// a value of an unsupported type, which the code of a value in another encodes by this one.
		return nil, &errors.UnsupportedTypeError{Type: typ}
	}
	return c.codeToOpcodeSet(typ, code)
}

// pointeeCodeSet returns the opcodes of the value of typ which a pointer at the top level points to, which is
// addressable ( see PointeeOf ): with CallMethodsWithLegacySemantics, which calls the methods of pointers for
// addressable values only, they are compiled as the value of a pointer, apart from the opcodes of typ, which are the
// ones of a value given by itself. Without it, both are written alike, by the opcodes of typ.
func pointeeCodeSet(ctx *RuntimeContext, typ reflect.Type) (*OpcodeSet, error) {
	if ctx.Option.Flag&CallMethodsWithLegacySemanticsOption == 0 {
		return CompileToGetCodeSet(ctx, uintptr(runtime.TypePtr(typ)))
	}
	c := newCompiler(ctx.compileMode())
	c.funcs = ctx.Option.Funcs
	code, err := c.pointeeCode(typ)
	if err != nil {
		return nil, err
	}
	return c.codeToOpcodeSet(typ, code)
}

// pointeeCode returns the code for the value which a pointer points to.
// It is addressable, as an element of a slice is.
func (c *Compiler) pointeeCode(typ reflect.Type) (Code, error) {
	code, err := c.listElemCode(typ, true)
	if err != nil {
		return nil, err
	}
	if code.Kind() == CodeKindStruct {
		structCode := code.(*StructCode)
		structCode.enableIndirect()
	}
	return code, nil
}

// valueCode returns the code for a value which is not addressable and whose address is given to the opcode:
// the value passed to Marshal and the value held by an interface value.
// It is the same as the value of a map, which is not addressable either.
func (c *Compiler) valueCode(typ reflect.Type) (Code, error) {
	code, err := c.mapValueCode(typ)
	if err != nil {
		return nil, err
	}
	if code.Kind() == CodeKindStruct {
		structCode := code.(*StructCode)
		structCode.enableIndirect()
	}
	return code, nil
}

func (c *Compiler) codeToOpcodeSet(typ reflect.Type, code Code) (*OpcodeSet, error) {
	// the opcodes of each escape of the names of the fields ( see KeyEscapeOptions ): the ones of the escape for
	// JavaScript are the others unless a name has a character which it escapes.
	var keyCodes, interfaceKeyCodes [4]*Opcode
	jsKey := false
	for i := range keyCodes {
		if i&int(NormalizeUTF8Option) != 0 && !jsKey {
			keyCodes[i], interfaceKeyCodes[i] = keyCodes[i&^int(NormalizeUTF8Option)], interfaceKeyCodes[i&^int(NormalizeUTF8Option)]
			continue
		}
		ctx := &compileContext{
			structTypeToCodes: map[uintptr]Opcodes{},
			recursiveCodes:    &Opcodes{},
			hiddenNames:       map[*Opcode][]string{},
			escapeKey:         i&int(HTMLEscapeOption) != 0,
			escapeJSKey:       i&int(NormalizeUTF8Option) != 0,
		}
		keyCode, err := c.codeToOpcode(ctx, typ, code)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			if err := keyCode.Validate(); err != nil {
				return nil, err
			}
		}
		jsKey = jsKey || ctx.jsKey
		keyCode = copyOpcode(keyCode)
		setTotalLengthToInterfaceOp(keyCode)
		keyCodes[i], interfaceKeyCodes[i] = keyCode, copyToInterfaceOpcode(keyCode)
	}
	codeLength := keyCodes[0].TotalLength()
	// the data word is the address of the value which the opcodes take, which identifies it, or the value itself,
	// whose first word identifies it, as the array of a slice identifies the slice ( see recordSeenValue ).
	dataWordIsAddr := runtime.IfaceIndir(typ) || c.compilesToPointee(typ)
	return &OpcodeSet{
		Type:              typ,
		IfaceIndir:        runtime.IfaceIndir(typ),
		DataWordIsAddr:    dataWordIsAddr,
		Levels:            deepestLevel(keyCodes[0]),
		KeyCodes:          keyCodes,
		InterfaceKeyCodes: interfaceKeyCodes,
		CodeLength:        codeLength,
		EndCode:           ToEndCode(interfaceKeyCodes[0]),
		Scalar:            scalarOpcode(keyCodes[0]),
		Code:              code,
	}, nil
}

// deepestLevel returns the deepest level which the opcodes from code to their end go to, after the indent of the
// frame they are run in: the indent of a struct is the one of its fields, and an array, a slice or a map opens a
// level after its indent. The opcodes of an interface value or of a recursive type are in frames of their own, and
// a value which is null, omitted or empty is counted as the levels its type would open ( see MaxDepth ).
func deepestLevel(code *Opcode) uint32 {
	var level uint32
	for c := code; !c.IsEnd(); c = c.IterNext() {
		l := c.Indent
		switch c.Op.CodeType() {
		case CodeArrayHead, CodeSliceHead, CodeMapHead:
			l++
		}
		level = max(level, l)
	}
	return level
}

// scalarOpcode returns the opcode if the code is a single opcode of a scalar followed by the end, or nil.
func scalarOpcode(code *Opcode) *Opcode {
	if isScalarOp(code.Op) && code.Next != nil && code.Next.Op == OpEnd {
		return code
	}
	return nil
}

// isScalarOp is whether the opcode writes a scalar from the address of its value, which appendScalar of the VMs
// does for such an opcode wherever the value is.
func isScalarOp(op OpType) bool {
	switch op {
	case OpInt, OpUint, OpFloat32, OpFloat64, OpString, OpBool, OpBytes, OpNumber:
		return true
	}
	return false
}

func (c *Compiler) typeToCodeWithPtr(typ reflect.Type, isPtr bool) (Code, error) {
	if code, ok := c.recursiveValueCode(typ, isPtr); ok {
		return code, nil
	}
	if c.v2 {
		if code, ok := c.v2Code(typ, isPtr); ok {
			return code, nil
		}
	} else {
		switch {
		case c.implementsMarshalJSON(typ):
			return c.marshalJSONCode(typ, isPtr)
		case c.implementsMarshalText(typ):
			return c.marshalTextCode(typ)
		}
		if c.declines(typ) && typ.Kind() != reflect.Ptr {
			// the value whose MarshalJSONTo declined, which is written by its kind once: a value of the type in it
			// calls the method ( see appendDeclined ). A kind which has no representation fails now, as the value
			// is being written: its code would find the method again.
			c.v2DefaultType = nil
			switch typ.Kind() {
			case reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
				return nil, &errors.UnsupportedTypeError{Type: typ}
			}
		}
	}
	defer c.enterValue(typ)()
	switch typ.Kind() {
	case reflect.Ptr:
		return c.ptrCode(typ)
	case reflect.Slice:
		elem := typ.Elem()
		if elem.Kind() == reflect.Uint8 {
			p := reflect.PointerTo(elem)
			if !c.implementsMarshalJSONType(p) && !p.Implements(marshalTextType) {
				return c.bytesCode(typ, false)
			}
		}
		return c.sliceCode(typ)
	case reflect.Array:
		return c.arrayCode(typ, isPtr)
	case reflect.Map:
		return c.mapCode(typ)
	case reflect.Struct:
		return c.structCode(typ, isPtr)
	case reflect.Interface:
		return c.interfaceCode(typ, false)
	case reflect.Int:
		return c.intCode(typ, false)
	case reflect.Int8:
		return c.int8Code(typ, false)
	case reflect.Int16:
		return c.int16Code(typ, false)
	case reflect.Int32:
		return c.int32Code(typ, false)
	case reflect.Int64:
		return c.int64Code(typ, false)
	case reflect.Uint:
		return c.uintCode(typ, false)
	case reflect.Uint8:
		return c.uint8Code(typ, false)
	case reflect.Uint16:
		return c.uint16Code(typ, false)
	case reflect.Uint32:
		return c.uint32Code(typ, false)
	case reflect.Uint64:
		return c.uint64Code(typ, false)
	case reflect.Uintptr:
		return c.uintCode(typ, false)
	case reflect.Float32:
		return c.float32Code(typ, false)
	case reflect.Float64:
		return c.float64Code(typ, false)
	case reflect.String:
		return c.stringCode(typ, false)
	case reflect.Bool:
		return c.boolCode(typ, false)
	}
	if !c.v2 {
		// encoding/json fails for a value of an unsupported type only when it encodes one: the value is encoded
		// by its type when it is, whose compile fails ( see compile ), as a value of interface{} of the type is.
		return &InterfaceCode{typ: typ, static: true}, nil
	}
	return nil, &errors.UnsupportedTypeError{Type: typ}
}

const intSize = 32 << (^uint(0) >> 63)

//nolint:unparam
func (c *Compiler) intCode(typ reflect.Type, isPtr bool) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: intSize, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) int8Code(typ reflect.Type, isPtr bool) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: 8, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) int16Code(typ reflect.Type, isPtr bool) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: 16, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) int32Code(typ reflect.Type, isPtr bool) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: 32, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) int64Code(typ reflect.Type, isPtr bool) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: 64, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) uintCode(typ reflect.Type, isPtr bool) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: intSize, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) uint8Code(typ reflect.Type, isPtr bool) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: 8, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) uint16Code(typ reflect.Type, isPtr bool) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: 16, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) uint32Code(typ reflect.Type, isPtr bool) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: 32, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) uint64Code(typ reflect.Type, isPtr bool) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: 64, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) float32Code(typ reflect.Type, isPtr bool) (*FloatCode, error) {
	return &FloatCode{typ: typ, bitSize: 32, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) float64Code(typ reflect.Type, isPtr bool) (*FloatCode, error) {
	return &FloatCode{typ: typ, bitSize: 64, isPtr: isPtr, isString: c.stringifyNumbers}, nil
}

//nolint:unparam
func (c *Compiler) stringCode(typ reflect.Type, isPtr bool) (*StringCode, error) {
	// a json.Number is a number, which StringifyNumbersOption writes in a string.
	return &StringCode{typ: typ, isPtr: isPtr, isJSONNumberString: c.stringifyNumbers && typ == jsonNumberType}, nil
}

//nolint:unparam
func (c *Compiler) boolCode(typ reflect.Type, isPtr bool) (*BoolCode, error) {
	return &BoolCode{typ: typ, isPtr: isPtr}, nil
}

//nolint:unparam
func (c *Compiler) intStringCode(typ reflect.Type) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: intSize, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) int8StringCode(typ reflect.Type) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: 8, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) int16StringCode(typ reflect.Type) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: 16, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) int32StringCode(typ reflect.Type) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: 32, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) int64StringCode(typ reflect.Type) (*IntCode, error) {
	return &IntCode{typ: typ, bitSize: 64, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) uintStringCode(typ reflect.Type) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: intSize, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) uint8StringCode(typ reflect.Type) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: 8, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) uint16StringCode(typ reflect.Type) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: 16, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) uint32StringCode(typ reflect.Type) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: 32, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) uint64StringCode(typ reflect.Type) (*UintCode, error) {
	return &UintCode{typ: typ, bitSize: 64, isString: true}, nil
}

//nolint:unparam
func (c *Compiler) bytesCode(typ reflect.Type, isPtr bool) (*BytesCode, error) {
	return &BytesCode{typ: typ, isPtr: isPtr}, nil
}

//nolint:unparam
func (c *Compiler) interfaceCode(typ reflect.Type, isPtr bool) (*InterfaceCode, error) {
	return &InterfaceCode{typ: typ, isPtr: isPtr}, nil
}

// marshalJSONCode returns the code of a value of the type with MarshalJSON or MarshalJSONTo, which is addressable
// if addressable is set.
//
//nolint:unparam
func (c *Compiler) marshalJSONCode(typ reflect.Type, addressable bool) (*MarshalJSONCode, error) {
	code := &MarshalJSONCode{
		typ:                typ,
		isAddrForMarshaler: c.isPtrMarshalJSONType(typ),
		isNilableType:      c.isNilableType(typ),
		isMarshalerContext: typ.Implements(marshalJSONContextType) || reflect.PointerTo(typ).Implements(marshalJSONContextType),
	}
	switch {
	case code.isAddrForMarshaler:
		code.call = c.marshalToCall(reflect.PointerTo(typ), addressable)
	case addressable && runtime.IfaceIndir(typ) && !c.implementsMarshalTo(typ):
		// an addressable value has the methods of its pointer, whose MarshalJSONTo comes before its own MarshalJSON:
		// the opcode gives the method the address of the value, the data word of the value
		if code.call = c.marshalToCall(reflect.PointerTo(typ), true); code.call != nil {
			break
		}
		fallthrough
	default:
		code.call = c.marshalToCall(typ, addressable)
	}
	return code, nil
}

//nolint:unparam
func (c *Compiler) marshalTextCode(typ reflect.Type) (*MarshalTextCode, error) {
	return &MarshalTextCode{
		typ:                typ,
		isAddrForMarshaler: c.isPtrMarshalTextType(typ),
		isNilableType:      c.isNilableType(typ),
	}, nil
}

func (c *Compiler) ptrCode(typ reflect.Type) (*PtrCode, error) {
	return c.pointerCode(typ, true)
}

// pointerCode returns the code of the pointer type typ, whose value is addressable if addressable is set: the value
// of a pointer is, but the one of the pointer to a map by which the opcodes of a map in a value read the map, which is
// addressable as the map is ( see mapValueCode ).
func (c *Compiler) pointerCode(typ reflect.Type, addressable bool) (*PtrCode, error) {
	if elem := typ.Elem(); elem.Kind() == reflect.Ptr && len(c.funcs.funcsOf(elem)) > 0 {
		// a pointer which a function of marshaling takes is not followed with the pointer to it: it is a value of
		// its own, encoded by its type after the pointer to it.
		return &PtrCode{typ: typ, value: &InterfaceCode{typ: elem, static: true}, ptrNum: 1}, nil
	}
	code, err := c.typeToCodeWithPtr(typ.Elem(), addressable)
	if err != nil {
		return nil, err
	}
	ptr, ok := code.(*PtrCode)
	if ok {
		return &PtrCode{typ: typ, value: ptr.value, ptrNum: ptr.ptrNum + 1}, nil
	}
	return &PtrCode{typ: typ, value: code, ptrNum: 1}, nil
}

func (c *Compiler) sliceCode(typ reflect.Type) (*SliceCode, error) {
	elem := typ.Elem()
	code, err := c.listElemCode(elem, true)
	if err != nil {
		return nil, err
	}
	if code.Kind() == CodeKindStruct {
		structCode := code.(*StructCode)
		structCode.enableIndirect()
	}
	return &SliceCode{typ: typ, value: code}, nil
}

// arrayCode returns the code of the array type typ, addressable or not: its elements are addressable as v1 takes
// them, but as encoding/json takes them with CallMethodsWithLegacySemantics, if the array is.
func (c *Compiler) arrayCode(typ reflect.Type, addressable bool) (*ArrayCode, error) {
	elem := typ.Elem()
	code, err := c.listElemCode(elem, addressable || c.v1Options&modeCallMethodsWithLegacySemantics == 0)
	if err != nil {
		return nil, err
	}
	if code.Kind() == CodeKindStruct {
		structCode := code.(*StructCode)
		structCode.enableIndirect()
	}
	return &ArrayCode{typ: typ, value: code}, nil
}

func (c *Compiler) mapCode(typ reflect.Type) (*MapCode, error) {
	var keyCode Code
	var err error
	switch key := typ.Key(); {
	case c.v2:
		keyCode, err = c.v2MapKeyCode(key)
	case key.Kind() == reflect.String:
		// a key of a string type is the string, without its MarshalText, as encoding/json documents it; a pointer
		// to one is not of a string type ( see mapKeyCode ).
		keyCode, err = c.stringCode(key, false)
	default:
		keyCode, err = c.mapKeyCode(key)
	}
	if err != nil {
		return nil, err
	}
	valueCode, err := c.mapValueCode(typ.Elem())
	if err != nil {
		return nil, err
	}
	if valueCode.Kind() == CodeKindStruct {
		structCode := valueCode.(*StructCode)
		structCode.enableIndirect()
	}
	// the names of the keys which a function, a method or a pointer writes may be the same.
	_, marshaledKey := keyCode.(*MarshalJSONCode)
	return &MapCode{typ: typ, key: keyCode, value: valueCode, mayRepeatNames: c.v2 && marshaledKey}, nil
}

func (c *Compiler) listElemCode(typ reflect.Type, addressable bool) (Code, error) {
	if code, ok := c.recursiveValueCode(typ, addressable); ok {
		return code, nil
	}
	switch {
	case !c.v2 && c.isPtrMarshalJSONType(typ):
		// The opcode takes the address of the element, which is the very pointer the marshaler is called with.
		// So the opcode is the one of the pointer type, and neither a copy of the element nor reflect is needed.
		ptrType := reflect.PointerTo(typ)
		return &MarshalJSONCode{
			typ:                ptrType,
			isMarshalerContext: ptrType.Implements(marshalJSONContextType),
			call:               c.marshalToCall(ptrType, true),
		}, nil
	case !c.v2 && c.implementsMarshalJSONType(typ):
		return c.marshalJSONCode(typ, true)
	case !c.v2 && !typ.Implements(marshalTextType) && reflect.PointerTo(typ).Implements(marshalTextType):
		return &MarshalTextCode{typ: reflect.PointerTo(typ)}, nil
	case typ.Kind() == reflect.Map && !(c.v2 && (c.hasMarshaler(typ, addressable) || c.stringTag)):
		// a map is read by the pointer to it, but one which has a marshaler is encoded by the marshaler, and the value
		// of the `string` option by its code ( see v2Code ), which are given its address
		return c.pointerCode(reflect.PointerTo(typ), addressable)
	default:
		// an element of a slice or an array is addressable ( see https://github.com/goccy/go-json/issues/370 ), but
		// the one of an array which is not, with CallMethodsWithLegacySemantics ( see arrayCode )
		code, err := c.typeToCodeWithPtr(typ, addressable)
		if err != nil {
			return nil, err
		}
		ptr, ok := code.(*PtrCode)
		if ok {
			if ptr.value.Kind() == CodeKindMap {
				ptr.ptrNum++
			}
		}
		return code, nil
	}
}

// pointerMapKeyCode returns the code of a key of the pointer type typ, which is named as the value it points to, as
// encoding/json of Go 1.27 names it, and true: a nil pointer has no name, and a value which can't be a key fails.
// The pointers are followed by the opcode of what they lead to: a pointer of MarshalText is followed too, and its
// method is called with its value ( see PtrCode.ToOpcode ). A key of a type of pointers which points to itself is
// followed when it is encoded ( see appendInterfaceMapKey ). It returns false for a pointer whose own type has
// MarshalText, which mapKeyCode compiles as the pointer it is: a nil one is named "".
func (c *Compiler) pointerMapKeyCode(typ reflect.Type) (Code, bool, error) {
	var pointers []reflect.Type
	elem := typ
	for elem.Kind() == reflect.Ptr && !c.implementsMarshalText(elem) {
		if slices.Contains(pointers, elem) {
			// a type of pointers which points to itself, as type R *R: the pointers of a key end at nil or lead back
			// to themselves, which fails when it is encoded. A map without keys is written as any other.
			return &MarshalTextCode{typ: typ, isInterfaceMapKey: true}, true, nil
		}
		pointers = append(pointers, elem)
		elem = elem.Elem()
	}
	var key Code
	if elem.Kind() == reflect.Ptr {
		// a pointer of MarshalText, which is followed as well: the opcode is given its value, the receiver of the
		// method, which it doesn't load.
		pointers = append(pointers, elem)
		key = &MarshalTextCode{typ: elem}
	}
	if len(pointers) > math.MaxUint8 {
		// more pointers than an opcode counts ( see Opcode.PtrNum ).
		return nil, true, &errors.UnsupportedTypeError{Type: typ}
	}
	if key == nil {
		var err error
		if key, err = c.mapKeyCode(elem); err != nil {
			return nil, true, err
		}
		if _, text := key.(*MarshalTextCode); text && elem.Kind() != reflect.Interface {
			if typ.Implements(marshalTextType) {
				return nil, false, nil
			}
			// a value of MarshalText, whose method the pointer to it has: the opcode is given the pointer, as for a
			// pointer of MarshalText, not the value, which an interface may hold directly, as a map behind a pointer
			// of a type of its own ( type P *M ) is.
			key = &MarshalTextCode{typ: reflect.PointerTo(elem)}
		}
	}
	return &PtrCode{typ: typ, value: key, ptrNum: uint8(len(pointers)), isMapKey: true}, true, nil
}

func (c *Compiler) mapKeyCode(typ reflect.Type) (Code, error) {
	if typ == jsonNumberType {
		// a json.Number is named by its string, as encoding/json names it: not as a number.
		typ = reflect.TypeFor[string]()
	}
	switch {
	case typ.Kind() == reflect.Interface && (interfaceMapKeys || c.implementsMarshalText(typ)):
		// the name of a key is of its dynamic value, as encoding/json of the Go it is built with makes it: every
		// key of an interface type where it is built on encoding/json/v2, and else a key of an interface type
		// which has MarshalText ( see appendInterfaceMapKey ).
		return &MarshalTextCode{typ: typ, isInterfaceMapKey: true}, nil
	case c.implementsMarshalText(typ):
		code, err := c.marshalTextCode(typ)
		if err != nil {
			return nil, err
		}
		code.isMapKey = true
		return code, nil
	}
	switch typ.Kind() {
	case reflect.Ptr:
		if key, ok, err := c.pointerMapKeyCode(typ); ok {
			return key, err
		}
		code, err := c.ptrCode(typ)
		if err != nil {
			return nil, err
		}
		if text, ok := code.value.(*MarshalTextCode); ok {
			// a pointer to a value whose MarshalText has a value receiver
			text.isMapKey = true
		}
		return code, nil
	case reflect.String:
		return c.stringCode(typ, false)
	case reflect.Int:
		return c.intStringCode(typ)
	case reflect.Int8:
		return c.int8StringCode(typ)
	case reflect.Int16:
		return c.int16StringCode(typ)
	case reflect.Int32:
		return c.int32StringCode(typ)
	case reflect.Int64:
		return c.int64StringCode(typ)
	case reflect.Uint:
		return c.uintStringCode(typ)
	case reflect.Uint8:
		return c.uint8StringCode(typ)
	case reflect.Uint16:
		return c.uint16StringCode(typ)
	case reflect.Uint32:
		return c.uint32StringCode(typ)
	case reflect.Uint64:
		return c.uint64StringCode(typ)
	case reflect.Uintptr:
		return c.uintStringCode(typ)
	}
	return nil, &errors.UnsupportedTypeError{Type: typ}
}

func (c *Compiler) mapValueCode(typ reflect.Type) (Code, error) {
	if code, ok := c.recursiveValueCode(typ, false); ok {
		return code, nil
	}
	switch {
	case typ.Kind() == reflect.Map && !c.hasMarshaler(typ, false) && !c.stringTag:
		// a map which has a marshaler is encoded by the marshaler, not as a map, and so is the value of the
		// `string` option by its code ( see v2Code ), which is given its address.
		return c.pointerCode(reflect.PointerTo(typ), false)
	default:
		code, err := c.typeToCodeWithPtr(typ, false)
		if err != nil {
			return nil, err
		}
		ptr, ok := code.(*PtrCode)
		if ok {
			if ptr.value.Kind() == CodeKindMap {
				ptr.ptrNum++
			}
		}
		return code, nil
	}
}

func (c *Compiler) structCode(typ reflect.Type, isPtr bool) (*StructCode, error) {
	embedded := c.nextIsEmbedded
	c.nextIsEmbedded = false
	return c.compileStruct(typ, isPtr, embedded, !embedded)
}

// compileStruct returns the code of the struct. embedded is whether it is an embedded struct. mayShare is
// whether the struct may be encoded by a jump to one code of it, if it is large and has been compiled before
// ( see sharedStructFieldCount ): an embedded struct is not, because its fields are the ones of the struct
// which embeds it, which may hide some of them.
func (c *Compiler) compileStruct(typ reflect.Type, isPtr, embedded, mayShare bool) (*StructCode, error) {
	typeptr := uintptr(runtime.TypePtr(typ))
	key := c.structKey(typeptr, isPtr)
	if count, compiled := c.structFieldCounts[key]; mayShare && compiled && count > sharedStructFieldCount {
		if _, onPath := c.structTypeToCode[key]; !onPath {
			// the code which is jumped to is compiled when the recursive codes are linked.
			return &StructCode{typ: typ, key: key, isPtr: isPtr, isIndirect: runtime.IfaceIndir(typ), isRecursive: true}, nil
		}
	}
	if code, exists := c.structTypeToCode[key]; exists {
		derefCode := *code
		derefCode.isRecursive = true
		if slices.Contains(c.embeddingChain, typeptr) {
			// The struct is embedded in itself, directly or through the other embedded structs. All of its
			// fields are hidden by the same fields of itself at the shallower depth of the same JSON
			// object, as in encoding/json, so it has nothing to write.
			derefCode.isHiddenByItself = true
		}
		return &derefCode, nil
	}
	c.embeddingChain = append(c.embeddingChain, typeptr)
	defer func() { c.embeddingChain = c.embeddingChain[:len(c.embeddingChain)-1] }()
	indirect := runtime.IfaceIndir(typ)
	code := &StructCode{typ: typ, key: key, isPtr: isPtr, isIndirect: indirect}
	c.structTypeToCode[key] = code
	firstField := c.fieldCount

	fieldNum := typ.NumField()
	var tags runtime.StructTags
	var embeds map[*runtime.StructTag][]int
	if c.v2 {
		var restore func()
		tags, embeds, restore = c.v2StructTags(typ, embedded)
		defer restore()
	} else {
		tags = c.typeToStructTags(typ)
	}
	fields := []*StructFieldCode{}
	for i, tag := range tags {
		if isAlwaysOmitted(tag) {
			if !c.v2 {
				// a field which is never written, whose value is not compiled, as its type may not be encodable:
				// it takes part only in the choice of the fields of its name ( see filteredDuplicatedFields ). The
				// members of the v2 semantics are chosen before ( see v2StructTags ).
				fields = append(fields, &StructFieldCode{typ: tag.Field.Type, key: tag.Key, tag: tag, isTaggedKey: tag.IsTaggedKey})
			}
			continue
		}
		isOnlyOneFirstField := i == 0 && fieldNum == 1
		c.v2EmbedPath = embeds[tag]
		field, err := c.structFieldCode(code, tag, isPtr, isOnlyOneFirstField)
		c.v2EmbedPath = nil
		if err != nil {
			return nil, err
		}
		if field.isAnonymous {
			structCode := field.getAnonymousStruct()
			if structCode != nil && structCode.isHiddenByItself {
				continue
			}
			if structCode != nil {
				if !c.v2 {
					// the members of the v2 semantics are chosen before ( see v2StructTags ).
					structCode.removeFieldsByTags(c.keyTags(tags))
				} else if structCode.isRecursive {
					structCode.hiddenNames = c.v2HiddenNames(structCode.typ, embeds[tag])
				}
				if c.isAssignableIndirect(field, isPtr) {
					if indirect {
						structCode.isIndirect = true
					} else {
						structCode.isIndirect = false
					}
				}
			}
		} else {
			structCode := field.getStruct()
			if structCode != nil {
				if indirect {
					// if parent is indirect type, set child indirect property to true
					structCode.isIndirect = true
				} else {
					// if parent is not indirect type, set child indirect property to false.
					// but if parent's indirect is false and isPtr is true, then indirect must be true.
					// Do this only if indirectConversion is enabled at the end of compileStruct.
					structCode.isIndirect = false
				}
			}
		}
		fields = append(fields, field)
	}
	if c.v2 {
		if fs := v2FieldsOf(typ); !embedded && fs.Fallback != nil {
			// the members of the embedded fallback are written after the ones of the fields.
			fields = append(fields, c.v2FallbackField(typ, fs))
		}
		code.fields = fields
	} else if embedded {
		// the fields of the same name are chosen in the struct which embeds it, by their depths there.
		code.fields = fields
	} else {
		code.fields = c.filteredDuplicatedFields(fields, c.hiddenFields(fields))
	}
	if c.optimizeFieldOrder {
		code.fields = orderFieldsForSpeed(code.fields, typ)
	}
	if !code.disableIndirectConversion && !indirect && isPtr {
		code.enableIndirect()
	}
	delete(c.structTypeToCode, key)
	c.fieldCount += len(code.fields)
	if !embedded {
		c.structFieldCounts[key] = c.fieldCount - firstField
	}
	return code, nil
}

// addressableStructMark marks the key of the code of an addressable struct with CallMethodsWithLegacySemantics in the
// lowest bit of the address of its type, which is 0 ( see structKey ).
const addressableStructMark = 1

// structKey returns what the code of a struct of the type typeptr, addressable if isPtr is set, is kept by while it is
// compiled ( structTypeToCode, structFieldCounts ) and linked ( compileContext.structTypeToCodes ): its type, and
// with CallMethodsWithLegacySemantics whether it is addressable, as its fields are addressable if it is, which calls
// the methods of their pointers then only ( see v2MethodOfAt ).
func (c *Compiler) structKey(typeptr uintptr, isPtr bool) uintptr {
	if isPtr && c.v1Options&modeCallMethodsWithLegacySemantics != 0 {
		return typeptr | addressableStructMark
	}
	return typeptr
}

// orderFieldsForSpeed returns the fields in the order they are encoded fastest ( OptimizeFieldOrderOption ):
// the fields of a kind which has the opcodes of a run ( fieldRunOps ) are put together, where the first field
// of the kind is, so that a run is as long as it can be; and a field which is a value of the struct's own type,
// if there is one, is put last, so that a list of values is encoded in one frame ( markTailRecursion ).
// The other fields stay in their order.
func orderFieldsForSpeed(fields []*StructFieldCode, typ reflect.Type) []*StructFieldCode {
	recursive := -1
	for i, field := range fields {
		if field.isRecursiveValueOf(typ) {
			if recursive >= 0 {
				recursive = -1 // more than one: none of them is the last
				break
			}
			recursive = i
		}
	}
	// the order key of a field: where the first field of its kind is, or its own place.
	keys := make([]int, len(fields))
	firstOfKind := map[CodeKind]int{}
	for i, field := range fields {
		keys[i] = i
		if i == recursive {
			keys[i] = len(fields)
			continue
		}
		if kind, ok := field.runKind(); ok {
			if first, seen := firstOfKind[kind]; seen {
				keys[i] = first
			} else {
				firstOfKind[kind] = i
			}
		}
	}
	order := make([]int, len(fields))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return keys[order[i]] < keys[order[j]] })
	ordered := make([]*StructFieldCode, len(fields))
	for i, j := range order {
		ordered[i] = fields[j]
	}
	return ordered
}

func toElemType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func (c *Compiler) structFieldCode(structCode *StructCode, tag *runtime.StructTag, isPtr, isOnlyOneFirstField bool) (*StructFieldCode, error) {
	field := tag.Field
	fieldType := field.Type
	isIndirectSpecialCase := isPtr && isOnlyOneFirstField
	fieldCode := &StructFieldCode{
		typ:           fieldType,
		key:           tag.Key,
		tag:           tag,
		offset:        field.Offset,
		isAnonymous:   c.isEmbeddedField(tag),
		isTaggedKey:   tag.IsTaggedKey,
		isNilableType: c.isNilableType(fieldType),
		// The check writes null instead of calling the marshaler, which encoding/json does only for a nil
		// pointer: the marshaler of a nil map is called. With omitempty the check is what decides that
		// the field is empty, for every kind.
		isNilCheck: tag.IsOmitEmpty || fieldType.Kind() == reflect.Ptr,
	}
	if !fieldCode.isAnonymous {
		// the value of the field is not a part of the JSON object being compiled.
		embeddingChain := c.embeddingChain
		c.embeddingChain = nil
		defer func() { c.embeddingChain = embeddingChain }()
	}
	if fieldCode.isAnonymous && (tag.IsOmitEmpty || tag.IsOmitZero) {
		// The fields of an embedded struct are written as the fields of the struct which embeds it, so there is
		// nothing for omitempty or omitzero of the embedded struct itself to omit, as in encoding/json. The
		// opcode for them would write the key of the embedded struct.
		inlined := *tag
		inlined.IsOmitEmpty = false
		inlined.IsOmitZero = false
		fieldCode.tag = &inlined
	}
	if c.v2 && !fieldCode.isAnonymous {
		if code, ok := c.v2FieldValueCode(fieldCode, isPtr); ok {
			fieldCode.value = code
			return fieldCode, nil
		}
	}
	switch {
	case !c.v2 && c.isMovePointerPositionFromHeadToFirstMarshalJSONFieldCase(fieldType, isIndirectSpecialCase):
		code, err := c.marshalJSONCode(fieldType, true)
		if err != nil {
			return nil, err
		}
		fieldCode.value = code
		fieldCode.isAddrForMarshaler = true
		fieldCode.isNilCheck = c.isNilCheckForAddrMarshaler(tag)
		structCode.isIndirect = false
		structCode.disableIndirectConversion = true
	case !c.v2 && c.isMovePointerPositionFromHeadToFirstMarshalTextFieldCase(fieldType, isIndirectSpecialCase):
		code, err := c.marshalTextCode(fieldType)
		if err != nil {
			return nil, err
		}
		fieldCode.value = code
		fieldCode.isAddrForMarshaler = true
		fieldCode.isNilCheck = c.isNilCheckForAddrMarshaler(tag)
		structCode.isIndirect = false
		structCode.disableIndirectConversion = true
	case !c.v2 && isPtr && c.isPtrMarshalJSONType(fieldType):
		// *struct{ field T }
		// func (*T) MarshalJSON() ([]byte, error)
		code, err := c.marshalJSONCode(fieldType, true)
		if err != nil {
			return nil, err
		}
		fieldCode.value = code
		fieldCode.isAddrForMarshaler = true
		fieldCode.isNilCheck = c.isNilCheckForAddrMarshaler(tag)
	case !c.v2 && isPtr && c.isPtrMarshalTextType(fieldType):
		// *struct{ field T }
		// func (*T) MarshalText() ([]byte, error)
		code, err := c.marshalTextCode(fieldType)
		if err != nil {
			return nil, err
		}
		fieldCode.value = code
		fieldCode.isAddrForMarshaler = true
		fieldCode.isNilCheck = c.isNilCheckForAddrMarshaler(tag)
	default:
		c.nextIsEmbedded = fieldCode.isAnonymous
		code, err := c.typeToCodeWithPtr(fieldType, isPtr)
		c.nextIsEmbedded = false
		if err != nil {
			return nil, err
		}
		switch code.Kind() {
		case CodeKindPtr, CodeKindInterface:
			fieldCode.isNextOpPtrType = true
		}
		fieldCode.value = code
	}
	return fieldCode, nil
}

// isNilCheckForAddrMarshaler returns whether a field whose marshaler is called with its address is checked.
//
// The check is what decides that the field is empty for omitempty, by the kind of the value as encoding/json
// does, so it is required for omitempty. Without omitempty it would write null instead of calling the
// marshaler, which a pointer to the field never needs.
func (c *Compiler) isNilCheckForAddrMarshaler(tag *runtime.StructTag) bool {
	return tag.IsOmitEmpty
}

// isEmbeddedField reports whether the field is an embedded struct whose fields are written as the fields of the
// struct which embeds it: for the v2 semantics, the field whose path v2StructTags gave.
func (c *Compiler) isEmbeddedField(tag *runtime.StructTag) bool {
	if c.v2 {
		return c.v2EmbedPath != nil
	}
	return c.isEmbeddedStruct(tag)
}

// isEmbeddedStruct reports whether the field is an embedded struct whose fields are written as the fields of
// the struct which embeds it.
func (c *Compiler) isEmbeddedStruct(tag *runtime.StructTag) bool {
	return tag.Field.Anonymous && !tag.IsTaggedKey && toElemType(tag.Field.Type).Kind() == reflect.Struct
}

// keyTags returns the tags of the fields which are written with their own keys.
// An embedded struct is not: its name is not a key, so it doesn't hide a field of the same name.
func (c *Compiler) keyTags(tags runtime.StructTags) runtime.StructTags {
	keyTags := make(runtime.StructTags, 0, len(tags))
	for _, tag := range tags {
		if c.isEmbeddedStruct(tag) {
			continue
		}
		keyTags = append(keyTags, tag)
	}
	return keyTags
}

func (c *Compiler) isAssignableIndirect(fieldCode *StructFieldCode, isPtr bool) bool {
	if isPtr {
		return false
	}
	codeType := fieldCode.value.Kind()
	if codeType == CodeKindMarshalJSON {
		return false
	}
	if codeType == CodeKindMarshalText {
		return false
	}
	return true
}

// fieldAtDepth is a field written to the JSON object of a struct, and how deep in the structs embedded in it the
// field is: a field of the struct's code, or of recursive, a recursive struct embedded in it, whose fields are
// compiled where the struct is linked, and are known here by its type.
type fieldAtDepth struct {
	field     *StructFieldCode
	recursive *StructCode
	depth     int
	tagged    bool
}

func (f fieldAtDepth) Depth() int        { return f.depth }
func (f fieldAtDepth) IsTaggedKey() bool { return f.tagged }

// fieldsByName appends the fields written to the JSON object of fields, the ones of the structs embedded in it
// too, by their names, at the depth of fields and deeper.
func (c *Compiler) fieldsByName(m map[string][]fieldAtDepth, fields []*StructFieldCode, depth int) {
	for _, field := range fields {
		if field.isAnonymous {
			if structCode := field.getAnonymousStruct(); structCode != nil {
				if !structCode.isRecursive {
					c.fieldsByName(m, structCode.fields, depth+1)
				} else if !structCode.isHiddenByItself {
					c.recursiveFieldsByName(m, structCode, structCode.typ, depth+1, nil)
				}
				continue
			}
		}
		m[field.key] = append(m[field.key], fieldAtDepth{field: field, depth: depth, tagged: field.isTaggedKey})
	}
}

// recursiveFieldsByName appends the fields of typ, a struct embedded in recursive or recursive itself, by their
// names, as fieldsByName does, from the tags of the fields. A struct embedded in itself writes nothing of its own.
func (c *Compiler) recursiveFieldsByName(m map[string][]fieldAtDepth, recursive *StructCode, typ reflect.Type, depth int, path []reflect.Type) {
	if slices.Contains(path, typ) {
		return
	}
	path = append(path, typ)
	for _, tag := range c.typeToStructTags(typ) {
		if c.isEmbeddedStruct(tag) {
			c.recursiveFieldsByName(m, recursive, toElemType(tag.Field.Type), depth+1, path)
			continue
		}
		m[tag.Key] = append(m[tag.Key], fieldAtDepth{recursive: recursive, depth: depth, tagged: tag.IsTaggedKey})
	}
}

// hiddenFields returns the fields of a struct which are not written because another field has the same name, as
// encoding/json chooses them ( see runtime.DominantField ). The names hidden in a recursive struct embedded in it
// are added to that struct ( see StructCode.hiddenNames ).
func (c *Compiler) hiddenFields(fields []*StructFieldCode) map[*StructFieldCode]struct{} {
	byName := map[string][]fieldAtDepth{}
	c.fieldsByName(byName, fields, 0)
	hidden := map[*StructFieldCode]struct{}{}
	for name, named := range byName {
		if len(named) == 1 {
			continue
		}
		dominant := runtime.DominantField(named)
		for i, f := range named {
			switch {
			case i == dominant:
			case f.field != nil:
				hidden[f.field] = struct{}{}
			case dominant >= 0 && named[dominant].recursive == f.recursive:
				// a field of the recursive struct hidden by another of its own, which it chooses itself.
			case !slices.Contains(f.recursive.hiddenNames, name):
				f.recursive.hiddenNames = append(f.recursive.hiddenNames, name)
			}
		}
	}
	return hidden
}

func (c *Compiler) filteredDuplicatedFields(fields []*StructFieldCode, duplicatedFieldMap map[*StructFieldCode]struct{}) []*StructFieldCode {
	filteredFields := make([]*StructFieldCode, 0, len(fields))
	for _, field := range fields {
		if field.isAnonymous {
			structCode := field.getAnonymousStruct()
			if structCode != nil && !structCode.isRecursive {
				structCode.fields = c.filteredDuplicatedFields(structCode.fields, duplicatedFieldMap)
				if len(structCode.fields) > 0 {
					filteredFields = append(filteredFields, field)
				}
				continue
			}
		}
		if _, exists := duplicatedFieldMap[field]; exists || isAlwaysOmitted(field.tag) {
			continue
		}
		filteredFields = append(filteredFields, field)
	}
	return filteredFields
}

// isAlwaysOmitted reports whether the field is never written: an array of no elements is always empty and zero, as
// in encoding/json. It still hides the deeper fields of its name.
func isAlwaysOmitted(tag *runtime.StructTag) bool {
	return (tag.IsOmitEmpty || tag.IsOmitZero) && tag.Field.Type.Kind() == reflect.Array && tag.Field.Type.Len() == 0
}

func (c *Compiler) typeToStructTags(typ reflect.Type) runtime.StructTags {
	tags := runtime.StructTags{}
	fieldNum := typ.NumField()
	for i := 0; i < fieldNum; i++ {
		field := typ.Field(i)
		if runtime.IsIgnoredStructField(field) {
			continue
		}
		tag := runtime.StructTagFromField(field)
		if tag.IsString && !stringTagApplies(field.Type) {
			tag.IsString = false
		}
		tags = append(tags, tag)
	}
	return tags
}

// stringTagApplies reports whether the option ,string applies to a field of typ, as encoding/json documents it: to
// a field of a string, a number or a bool. As encoding/json of Go 1.27 does, it follows one pointer, and does not
// apply to the value of a pointer to a pointer.
func stringTagApplies(typ reflect.Type) bool {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	}
	return false
}

// *struct{ field T } => struct { field *T }
// func (*T) MarshalJSON() ([]byte, error)
func (c *Compiler) isMovePointerPositionFromHeadToFirstMarshalJSONFieldCase(typ reflect.Type, isIndirectSpecialCase bool) bool {
	return isIndirectSpecialCase && !c.isNilableType(typ) && c.isPtrMarshalJSONType(typ)
}

// *struct{ field T } => struct { field *T }
// func (*T) MarshalText() ([]byte, error)
func (c *Compiler) isMovePointerPositionFromHeadToFirstMarshalTextFieldCase(typ reflect.Type, isIndirectSpecialCase bool) bool {
	return isIndirectSpecialCase && !c.isNilableType(typ) && c.isPtrMarshalTextType(typ)
}

func (c *Compiler) implementsMarshalJSON(typ reflect.Type) bool {
	if !c.implementsMarshalJSONType(typ) {
		return false
	}
	if typ.Kind() != reflect.Ptr {
		return true
	}
	// type kind is reflect.Ptr
	if !c.implementsMarshalJSONType(typ.Elem()) {
		return true
	}
	if !runtime.IfaceIndir(typ.Elem()) {
		// the value is stored directly in an interface value ( a map, a func, a struct of one pointer, ... ): the
		// method of the pointer takes the pointer, where the one of the value would take the value itself, which the
		// opcodes of a dereferenced pointer don't load.
		return true
	}
	// needs to dereference
	return false
}

func (c *Compiler) implementsMarshalText(typ reflect.Type) bool {
	if !typ.Implements(marshalTextType) {
		return false
	}
	if typ.Kind() != reflect.Ptr {
		return true
	}
	// type kind is reflect.Ptr
	if !typ.Elem().Implements(marshalTextType) {
		return true
	}
	if !runtime.IfaceIndir(typ.Elem()) {
		// the value is stored directly in an interface value ( a map, a func, a struct of one pointer, ... ): the
		// method of the pointer takes the pointer, where the one of the value would take the value itself, which the
		// opcodes of a dereferenced pointer don't load.
		return true
	}
	// needs to dereference
	return false
}

// hasMarshaler reports whether a value of the type, addressable or not, is written by a marshaler: a method of its
// own, or of v1 the method of the pointer to it.
func (c *Compiler) hasMarshaler(typ reflect.Type, addressable bool) bool {
	if c.v2 {
		return c.v2HasMethodAt(typ, addressable)
	}
	return c.implementsMarshalJSON(typ) || c.implementsMarshalText(typ)
}

func (c *Compiler) isNilableType(typ reflect.Type) bool {
	if !runtime.IfaceIndir(typ) {
		return true
	}
	switch typ.Kind() {
	case reflect.Ptr:
		return true
	case reflect.Map:
		return true
	case reflect.Func:
		return true
	default:
		return false
	}
}

// implementsMarshalJSONType reports whether a value of the type is written by MarshalJSON, or by MarshalJSONTo as
// MarshalJSON is ( see marshalToCall ).
func (c *Compiler) implementsMarshalJSONType(typ reflect.Type) bool {
	return typ.Implements(marshalJSONType) || typ.Implements(marshalJSONContextType) || c.implementsMarshalTo(typ)
}

func (c *Compiler) isPtrMarshalJSONType(typ reflect.Type) bool {
	return !c.implementsMarshalJSONType(typ) && c.implementsMarshalJSONType(reflect.PointerTo(typ))
}

func (c *Compiler) isPtrMarshalTextType(typ reflect.Type) bool {
	return !typ.Implements(marshalTextType) && reflect.PointerTo(typ).Implements(marshalTextType)
}

func (c *Compiler) codeToOpcode(ctx *compileContext, typ reflect.Type, code Code) (*Opcode, error) {
	codes := code.ToOpcode(ctx)
	// the first opcode takes the address of the value, as the one of the value of a map does.
	codes.First().Flags |= IndirectFlags
	codes.Last().Next = newEndOp(ctx, typ)
	if err := c.linkRecursiveCode(ctx); err != nil {
		return nil, err
	}
	return codes.First(), nil
}

// markTailRecursion marks the opcodes of code, the code of a recursive struct, which encode a value of the same
// type as its last field: such a value is encoded in the frame of the struct, without a frame of its own
// ( the case of OpRecursive of the VM ), and the end of the struct closes the braces of the values one by one
// when the last of them ends ( the case of OpRecursiveEnd ), which is why the end opcode leads back to it.
//
// The opcodes of the struct which the code is copied from, the ones which are jumped to it from, are not
// marked: the first value of the type in a frame enters a frame, so that the last value returns from it.
func markTailRecursion(code, end *Opcode, compiled *CompiledCode) {
	// the end of the struct is an opcode of its own only when the last field is not encoded by one opcode:
	// then it is the end which closes the braces, and which a value in the last field is followed by.
	if code.End.Op != OpStructEnd {
		return
	}
	for c := code; !c.IsEnd(); c = c.IterNext() {
		if (c.Op == OpRecursive || c.Op == OpRecursivePtr) && c.Jmp == compiled && c.Next == code.End {
			c.Flags |= TailRecursiveFlags
			// what the indent is deeper by for a value: OpRecursiveEnd takes it back.
			end.Indent = c.Indent - (code.Indent - 1)
		}
	}
	end.Next = code.End
}

func (c *Compiler) linkRecursiveCode(ctx *compileContext) error {
	type recursiveTarget struct {
		key      uintptr
		embedded bool
		query    *FieldQuery
		hidden   string
	}
	recursiveCodes := map[recursiveTarget]*CompiledCode{}
	// maxFrameLength is the length of the longest frame which a recursive code is jumped from.
	var maxFrameLength uintptr
	// the recursive codes may increase while they are linked, so the length is evaluated every time.
	for i := 0; i < len(*ctx.recursiveCodes); i++ {
		recursive := (*ctx.recursiveCodes)[i]
		// the struct jumped to by its key, which tells an addressable one apart ( see Compiler.structKey )
		key := uintptr(recursive.Type)
		if k, ok := ctx.recursiveKeys[recursive]; ok {
			key = k
		}
		target := recursiveTarget{key: key, embedded: recursive.Jmp.Embedded, query: recursive.FieldQuery,
			hidden: strings.Join(ctx.hiddenNames[recursive], "\x00")}
		if recursiveCode, ok := recursiveCodes[target]; ok {
			*recursive.Jmp = *recursiveCode
			continue
		}
		codes, exists := ctx.structTypeToCodes[key]
		if target.embedded || !exists || c.isFiltered {
			// structTypeToCodes has the opcodes of the struct itself, with the braces and the check of nil.
			// - A recursive struct which is embedded jumps to the opcodes only of the fields.
			// - A struct which has been compiled only as an embedded struct is not in structTypeToCodes.
			// - The opcodes of a struct filtered by a field query are filtered by the query of their place,
			//   and the struct is filtered by the query of the code which jumps to it.
			// In these cases the opcodes to jump to are compiled here.
			structCode, err := c.compileStruct(runtime.TypeOfPtr(recursive.Type), key&addressableStructMark != 0, target.embedded, false)
			if err != nil {
				return err
			}
			if target.embedded && !c.v2 {
				// a recursive struct which is embedded chooses here, by depth, among its own fields of the same name,
				// as the struct it is embedded in would: they are not compared with the fields of that struct.
				structCode.fields = c.filteredDuplicatedFields(structCode.fields, c.hiddenFields(structCode.fields))
			}
			if len(ctx.hiddenNames[recursive]) > 0 {
				// the names which the fields of the struct it is embedded in hide.
				hidden := make(runtime.StructTags, len(ctx.hiddenNames[recursive]))
				for i, name := range ctx.hiddenNames[recursive] {
					hidden[i] = &runtime.StructTag{Key: name}
				}
				structCode.removeFieldsByTags(hidden)
			}
			if target.query != nil {
				structCode = structCode.Filter(target.query).(*StructCode)
			}
			structCode.enableIndirect()
			if target.embedded {
				codes = structCode.ToAnonymousOpcode(ctx)
				if len(codes) == 0 {
					// an embedded struct which has no field to write here: the code jumps to nothing ( see
					// RuntimeContext.EnterRecursive ).
					recursiveCodes[target] = recursive.Jmp
					continue
				}
			} else {
				codes = structCode.ToOpcode(ctx)
			}
			// the opcodes are copied up to the end, as the ones in the code which jumps to them are.
			codes.Last().Next = newEndOp(ctx, runtime.TypeOfPtr(recursive.Type))
		}

		code := copyOpcode(codes.First())
		code.Op = code.Op.PtrHeadToHead()
		lastCode := newEndOp(&compileContext{}, runtime.TypeOfPtr(recursive.Type))
		lastCode.Op = OpRecursiveEnd

		// OpRecursiveEnd must set before call TotalLength
		code.End.Next = lastCode
		if target.embedded {
			// the fields of an embedded struct, whose last field goes on to the end when it is omitted: the struct
			// it is embedded in sets that for the fields of its own, which a target has not.
			lastFieldOpcode(code).NextField = lastCode
		}
		markTailRecursion(code, lastCode, recursive.Jmp)

		totalLength := code.TotalLength()

		// Idx, ElemIdx, Length must set after call TotalLength
		lastCode.Idx = opcodeOffset(totalLength + 1)
		lastCode.setEndSlots()

		// An interface in the recursive code allocates its frame after the frame it is in, which is
		// the one of the recursive code, not of the code which jumps to it. The length must include
		// the slots of OpRecursiveEnd, so it is set after they are decided.
		setTotalLengthToInterfaceOp(code)

		// The frame of the recursive code has the slots up to the ones of OpRecursiveEnd
		// ( the opcode to return to with the tail levels, the offset to return to and the indent to restore ),
		// so its length is the last index + 1.
		nextTotalLength := uintptr(code.TotalLength()) + 1
		if maxFrameLength < nextTotalLength {
			maxFrameLength = nextTotalLength
		}
		// extend length to alloc slot for elemIdx + length
		if curTotalLength := uintptr(recursive.TotalLength()) + 3; maxFrameLength < curTotalLength {
			maxFrameLength = curTotalLength
		}

		compiled := recursive.Jmp
		compiled.Code = code
		compiled.NextLen = nextTotalLength
		compiled.Levels = deepestLevel(code)

		recursiveCodes[target] = compiled
	}
	// The new frame is allocated after the frame which the recursive code is jumped from, and CurLen is the
	// length of that frame. The same CompiledCode is jumped to from the code of the top level and from every
	// recursive code, including itself, so CurLen must not be less than any of those frames: otherwise the
	// new frame overlaps the slots of OpRecursiveEnd of the frame it is jumped from, and the indent to
	// restore is broken by a pointer.
	for _, compiled := range recursiveCodes {
		compiled.CurLen = maxFrameLength
	}
	for _, recursive := range *ctx.recursiveCodes {
		recursive.Jmp.CurLen = maxFrameLength
	}
	return nil
}
