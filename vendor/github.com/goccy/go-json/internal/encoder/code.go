package encoder

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

type Code interface {
	Kind() CodeKind
	ToOpcode(*compileContext) Opcodes
	Filter(*FieldQuery) Code
}

type AnonymousCode interface {
	ToAnonymousOpcode(*compileContext) Opcodes
}

type Opcodes []*Opcode

func (o Opcodes) First() *Opcode {
	if len(o) == 0 {
		return nil
	}
	return o[0]
}

func (o Opcodes) Last() *Opcode {
	if len(o) == 0 {
		return nil
	}
	return o[len(o)-1]
}

func (o Opcodes) Add(codes ...*Opcode) Opcodes {
	return append(o, codes...)
}

type CodeKind int

const (
	CodeKindInterface CodeKind = iota
	CodeKindPtr
	CodeKindInt
	CodeKindUint
	CodeKindFloat
	CodeKindString
	CodeKindBool
	CodeKindStruct
	CodeKindMap
	CodeKindSlice
	CodeKindArray
	CodeKindBytes
	CodeKindMarshalJSON
	CodeKindMarshalText
	CodeKindRecursive
)

type IntCode struct {
	typ      reflect.Type
	bitSize  uint8
	isString bool
	isPtr    bool
}

func (c *IntCode) Kind() CodeKind {
	return CodeKindInt
}

func (c *IntCode) ToOpcode(ctx *compileContext) Opcodes {
	var code *Opcode
	switch {
	case c.isPtr:
		code = newOpCode(ctx, c.typ, OpIntPtr)
	case c.isString:
		code = newOpCode(ctx, c.typ, OpIntString)
	default:
		code = newOpCode(ctx, c.typ, OpInt)
	}
	code.NumBitSize = c.bitSize
	ctx.incIndex()
	return Opcodes{code}
}

func (c *IntCode) Filter(_ *FieldQuery) Code {
	return c
}

type UintCode struct {
	typ      reflect.Type
	bitSize  uint8
	isString bool
	isPtr    bool
}

func (c *UintCode) Kind() CodeKind {
	return CodeKindUint
}

func (c *UintCode) ToOpcode(ctx *compileContext) Opcodes {
	var code *Opcode
	switch {
	case c.isPtr:
		code = newOpCode(ctx, c.typ, OpUintPtr)
	case c.isString:
		code = newOpCode(ctx, c.typ, OpUintString)
	default:
		code = newOpCode(ctx, c.typ, OpUint)
	}
	code.NumBitSize = c.bitSize
	ctx.incIndex()
	return Opcodes{code}
}

func (c *UintCode) Filter(_ *FieldQuery) Code {
	return c
}

type FloatCode struct {
	typ     reflect.Type
	bitSize uint8
	isPtr   bool
}

func (c *FloatCode) Kind() CodeKind {
	return CodeKindFloat
}

func (c *FloatCode) ToOpcode(ctx *compileContext) Opcodes {
	var code *Opcode
	switch {
	case c.isPtr:
		switch c.bitSize {
		case 32:
			code = newOpCode(ctx, c.typ, OpFloat32Ptr)
		default:
			code = newOpCode(ctx, c.typ, OpFloat64Ptr)
		}
	default:
		switch c.bitSize {
		case 32:
			code = newOpCode(ctx, c.typ, OpFloat32)
		default:
			code = newOpCode(ctx, c.typ, OpFloat64)
		}
	}
	ctx.incIndex()
	return Opcodes{code}
}

func (c *FloatCode) Filter(_ *FieldQuery) Code {
	return c
}

type StringCode struct {
	typ   reflect.Type
	isPtr bool
}

func (c *StringCode) Kind() CodeKind {
	return CodeKindString
}

func (c *StringCode) ToOpcode(ctx *compileContext) Opcodes {
	isJSONNumberType := c.typ == jsonNumberType
	var code *Opcode
	if c.isPtr {
		if isJSONNumberType {
			code = newOpCode(ctx, c.typ, OpNumberPtr)
		} else {
			code = newOpCode(ctx, c.typ, OpStringPtr)
		}
	} else {
		if isJSONNumberType {
			code = newOpCode(ctx, c.typ, OpNumber)
		} else {
			code = newOpCode(ctx, c.typ, OpString)
		}
	}
	ctx.incIndex()
	return Opcodes{code}
}

func (c *StringCode) Filter(_ *FieldQuery) Code {
	return c
}

type BoolCode struct {
	typ   reflect.Type
	isPtr bool
}

func (c *BoolCode) Kind() CodeKind {
	return CodeKindBool
}

func (c *BoolCode) ToOpcode(ctx *compileContext) Opcodes {
	var code *Opcode
	switch {
	case c.isPtr:
		code = newOpCode(ctx, c.typ, OpBoolPtr)
	default:
		code = newOpCode(ctx, c.typ, OpBool)
	}
	ctx.incIndex()
	return Opcodes{code}
}

func (c *BoolCode) Filter(_ *FieldQuery) Code {
	return c
}

type BytesCode struct {
	typ   reflect.Type
	isPtr bool
}

func (c *BytesCode) Kind() CodeKind {
	return CodeKindBytes
}

func (c *BytesCode) ToOpcode(ctx *compileContext) Opcodes {
	var code *Opcode
	switch {
	case c.isPtr:
		code = newOpCode(ctx, c.typ, OpBytesPtr)
	default:
		code = newOpCode(ctx, c.typ, OpBytes)
	}
	ctx.incIndex()
	return Opcodes{code}
}

func (c *BytesCode) Filter(_ *FieldQuery) Code {
	return c
}

type SliceCode struct {
	typ   reflect.Type
	value Code
}

func (c *SliceCode) Kind() CodeKind {
	return CodeKindSlice
}

func (c *SliceCode) ToOpcode(ctx *compileContext) Opcodes {
	// header => opcode => elem => end
	//             ^        |
	//             |________|
	size := c.typ.Elem().Size()
	header := newSliceHeaderCode(ctx, c.typ)
	ctx.incIndex()

	ctx.incIndent()
	codes := c.value.ToOpcode(ctx)
	ctx.decIndent()

	codes.First().Flags |= IndirectFlags
	// the opcodes after the elements refer to the slots of the header, and they don't take a slot.
	elemCode := newSliceElemCode(ctx, c.typ.Elem(), header, size)
	ctx.incOpcodeIndex()
	end := newOpCode(ctx, c.typ, OpSliceEnd)
	ctx.incOpcodeIndex()
	header.End = end
	header.Next = codes.First()
	codes.Last().Next = elemCode
	elemCode.Next = codes.First()
	elemCode.End = end
	return Opcodes{header}.Add(codes...).Add(elemCode).Add(end)
}

func (c *SliceCode) Filter(_ *FieldQuery) Code {
	return c
}

type ArrayCode struct {
	typ   reflect.Type
	value Code
}

func (c *ArrayCode) Kind() CodeKind {
	return CodeKindArray
}

func (c *ArrayCode) ToOpcode(ctx *compileContext) Opcodes {
	// header => opcode => elem => end
	//             ^        |
	//             |________|
	elem := c.typ.Elem()
	alen := c.typ.Len()
	size := elem.Size()

	header := newArrayHeaderCode(ctx, c.typ, alen)
	ctx.incIndex()

	ctx.incIndent()
	codes := c.value.ToOpcode(ctx)
	ctx.decIndent()

	codes.First().Flags |= IndirectFlags

	// the opcodes after the elements refer to the slot of the header, and they don't take a slot.
	elemCode := newArrayElemCode(ctx, elem, header, alen, size)
	ctx.incOpcodeIndex()

	end := newOpCode(ctx, c.typ, OpArrayEnd)
	ctx.incOpcodeIndex()

	header.End = end
	header.Next = codes.First()
	codes.Last().Next = elemCode
	elemCode.Next = codes.First()
	elemCode.End = end

	return Opcodes{header}.Add(codes...).Add(elemCode).Add(end)
}

func (c *ArrayCode) Filter(_ *FieldQuery) Code {
	return c
}

type MapCode struct {
	typ   reflect.Type
	key   Code
	value Code
}

func (c *MapCode) Kind() CodeKind {
	return CodeKindMap
}

func (c *MapCode) ToOpcode(ctx *compileContext) Opcodes {
	// header => code => value => code => key => code => value => code => end
	//                                     ^                       |
	//                                     |_______________________|
	header := newMapHeaderCode(ctx, c.typ)
	ctx.incIndex()

	if c.isStringKey() {
		// header => value => key => end: the key of an entry is written by the header for the first entry and
		// by OpMapKey for the others, which give the value to its opcode.
		//                     ^         |
		//                     |_________|
		header.Flags |= MapStringKeyFlags

		ctx.incIndent()
		valueCodes := c.value.ToOpcode(ctx)
		ctx.decIndent()
		valueCodes.First().Flags |= IndirectFlags
		header.Map.ScalarValue = len(valueCodes) == 1 && isScalarOp(valueCodes.First().Op) &&
			header.Map.ValueWords >= 0 && header.Map.ValueWords <= MapScalarValueWords

		key := newMapKeyCode(ctx, c.typ.Key(), header)
		key.Flags |= MapStringKeyFlags
		ctx.incOpcodeIndex()

		end := newMapEndCode(ctx, c.typ, header)
		ctx.incOpcodeIndex()

		header.Next = valueCodes.First()
		valueCodes.Last().Next = key
		key.Next = valueCodes.First()
		header.End = end
		key.End = end
		return Opcodes{header}.Add(valueCodes...).Add(key).Add(end)
	}

	keyCodes := c.key.ToOpcode(ctx)
	if first := keyCodes.First(); first.Op == OpMarshalText && first.Flags&IsNilableTypeFlags != 0 {
		// the opcode of the key is given the address of the key, and the marshaler of a key of a pointer
		// type is called with the pointer.
		first.Flags |= IndirectFlags
	}

	// the opcodes other than the header refer to the slot of the header, and they don't take a slot.
	value := newMapValueCode(ctx, c.typ.Elem(), header)
	ctx.incOpcodeIndex()

	ctx.incIndent()
	valueCodes := c.value.ToOpcode(ctx)
	ctx.decIndent()

	valueCodes.First().Flags |= IndirectFlags

	key := newMapKeyCode(ctx, c.typ.Key(), header)
	ctx.incOpcodeIndex()

	end := newMapEndCode(ctx, c.typ, header)
	ctx.incOpcodeIndex()

	header.Next = keyCodes.First()
	keyCodes.Last().Next = value
	value.Next = valueCodes.First()
	valueCodes.Last().Next = key
	key.Next = keyCodes.First()

	header.End = end
	key.End = end
	value.End = end
	return Opcodes{header}.Add(keyCodes...).Add(value).Add(valueCodes...).Add(key).Add(end)
}

// isStringKey is whether the key of the map is a plain string, which OpMapKey writes itself: not a pointer,
// not json.Number, and not a type with a marshaler, whose code is not a StringCode.
func (c *MapCode) isStringKey() bool {
	key, ok := c.key.(*StringCode)
	return ok && !key.isPtr && key.typ != jsonNumberType
}

func (c *MapCode) Filter(_ *FieldQuery) Code {
	return c
}

type StructCode struct {
	typ                       reflect.Type
	fields                    []*StructFieldCode
	isPtr                     bool
	disableIndirectConversion bool
	isIndirect                bool
	isRecursive               bool
	// isHiddenByItself is whether the struct is a recursive one embedded in itself, which has nothing to write.
	isHiddenByItself bool
	// fieldQuery is the query which the fields of a recursive struct are filtered by: the code which is jumped
	// to is filtered by it when the recursive codes are linked.
	fieldQuery *FieldQuery
}

func (c *StructCode) Kind() CodeKind {
	return CodeKindStruct
}

func (c *StructCode) lastFieldCode(field *StructFieldCode, firstField *Opcode) *Opcode {
	if isEmbeddedStruct(field) {
		return c.lastAnonymousFieldCode(firstField)
	}
	lastField := firstField
	for lastField.NextField != nil {
		lastField = lastField.NextField
	}
	return lastField
}

func (c *StructCode) lastAnonymousFieldCode(firstField *Opcode) *Opcode {
	// firstField is special StructHead operation for anonymous structure.
	// So, StructHead's next operation is truly struct head operation.
	for firstField.Op == OpStructHead || firstField.Op == OpStructField {
		firstField = firstField.Next
	}
	return lastFieldOpcode(firstField)
}

// lastFieldOpcode returns the last field of the chain of the fields which starts from field.
// If a field of the chain is an embedded struct, which is a head without a value followed by
// the fields of the struct, the chain continues with those fields.
func lastFieldOpcode(field *Opcode) *Opcode {
	for field.NextField != nil {
		field = field.NextField
		for field.Flags&AnonymousHeadFlags != 0 && (field.Op == OpStructField || field.Op == OpStructHead) {
			field = field.Next
		}
	}
	return field
}

// fieldSlots makes the fields of a struct share the slots.
//
// The slot of the first field holds the address of the struct, which every field refers to. The slots after it
// are for the value of a field, and they are used only while that field is encoded, so every field takes the
// same ones: the length of a frame depends on how deep the values are nested, not on how many fields there are.
type fieldSlots struct {
	structIndex int
	isFirst     bool
}

func newFieldSlots(ctx *compileContext) *fieldSlots {
	return &fieldSlots{structIndex: ctx.ptrIndex, isFirst: true}
}

// reuse is called before a field is compiled.
func (s *fieldSlots) reuse(ctx *compileContext) {
	if s.isFirst {
		s.isFirst = false
		return
	}
	ctx.ptrIndex = s.structIndex + 1
}

func (c *StructCode) ToOpcode(ctx *compileContext) Opcodes {
	// header => code => structField => code => end
	//                        ^          |
	//                        |__________|
	if c.isRecursive {
		recursive := newRecursiveCode(ctx, c.typ, &CompiledCode{})
		recursive.Type = runtime.TypePtr(c.typ)
		recursive.FieldQuery = c.fieldQuery
		ctx.incIndex()
		*ctx.recursiveCodes = append(*ctx.recursiveCodes, recursive)
		return Opcodes{recursive}
	}
	codes := Opcodes{}
	var prevField *Opcode
	ctx.incIndent()
	fieldSlots := newFieldSlots(ctx)
	for idx, field := range c.fields {
		isFirstField := idx == 0
		isEndField := idx == len(c.fields)-1
		fieldSlots.reuse(ctx)
		fieldCodes := field.ToOpcode(ctx, isFirstField, isEndField)
		for _, code := range fieldCodes {
			if c.isIndirect {
				code.Flags |= IndirectFlags
			}
		}
		firstField := fieldCodes.First()
		if len(codes) > 0 {
			codes.Last().Next = firstField
			firstField.Idx = codes.First().Idx
		}
		if prevField != nil {
			prevField.NextField = firstField
		}
		if isEndField {
			endField := fieldCodes.Last()
			if len(codes) > 0 {
				codes.First().End = endField
			} else {
				firstField.End = endField
			}
			codes = codes.Add(fieldCodes...)
			break
		}
		prevField = c.lastFieldCode(field, firstField)
		codes = codes.Add(fieldCodes...)
	}
	if len(codes) == 0 {
		head := &Opcode{
			Op:         OpStructHead,
			Idx:        opcodeOffset(ctx.ptrIndex),
			Type:       runtime.TypePtr(c.typ),
			DisplayIdx: ctx.opcodeIndex,
			Indent:     ctx.indent,
		}
		ctx.incOpcodeIndex()
		end := &Opcode{
			Op:         OpStructEnd,
			Idx:        opcodeOffset(ctx.ptrIndex),
			DisplayIdx: ctx.opcodeIndex,
			Indent:     ctx.indent,
		}
		head.NextField = end
		head.Next = end
		head.End = end
		codes = codes.Add(head, end)
		ctx.incIndex()
	}
	ctx.decIndent()
	markFieldRuns(codes)
	ctx.structTypeToCodes[uintptr(runtime.TypePtr(c.typ))] = codes
	return codes
}

// fieldRunOps are, for the opcode of a field, the opcodes of a run of two and of three such fields in a row.
var fieldRunOps = map[OpType][2]OpType{
	OpStructFieldInt:     {OpStructFieldInt2, OpStructFieldInt3},
	OpStructFieldUint:    {OpStructFieldUint2, OpStructFieldUint3},
	OpStructFieldFloat64: {OpStructFieldFloat642, OpStructFieldFloat643},
	OpStructFieldString:  {OpStructFieldString2, OpStructFieldString3},
	OpStructFieldBool:    {OpStructFieldBool2, OpStructFieldBool3},
}

// maxFieldRun is the number of the fields of a run.
const maxFieldRun = 3

// markFieldRuns gives the first field of a run of fields of the same kind in a row the opcode of the run: the
// VM encodes the fields after it without a dispatch. The fields keep their opcodes, which the VM falls
// through; a run is never entered but at its first field, since only an omitted field is jumped over.
func markFieldRuns(codes Opcodes) {
	for i := 0; i < len(codes); {
		runs, ok := fieldRunOps[codes[i].Op]
		if !ok {
			i++
			continue
		}
		n := 1
		for i+n < len(codes) && n < maxFieldRun && codes[i+n].Op == codes[i].Op && codes[i+n-1].Next == codes[i+n] {
			n++
		}
		if n > 1 {
			codes[i].Op = runs[n-2]
		}
		i += n
	}
}

func (c *StructCode) ToAnonymousOpcode(ctx *compileContext) Opcodes {
	// header => code => structField => code => end
	//                        ^          |
	//                        |__________|
	if c.isRecursive {
		recursive := newRecursiveCode(ctx, c.typ, &CompiledCode{Embedded: true})
		recursive.Type = runtime.TypePtr(c.typ)
		recursive.FieldQuery = c.fieldQuery
		ctx.incIndex()
		*ctx.recursiveCodes = append(*ctx.recursiveCodes, recursive)
		return Opcodes{recursive}
	}
	codes := Opcodes{}
	var prevField *Opcode
	fieldSlots := newFieldSlots(ctx)
	for idx, field := range c.fields {
		isFirstField := idx == 0
		isEndField := idx == len(c.fields)-1
		fieldSlots.reuse(ctx)
		fieldCodes := field.ToAnonymousOpcode(ctx, isFirstField, isEndField)
		for _, code := range fieldCodes {
			if c.isIndirect {
				code.Flags |= IndirectFlags
			}
		}
		firstField := fieldCodes.First()
		if len(codes) > 0 {
			codes.Last().Next = firstField
			firstField.Idx = codes.First().Idx
		}
		if prevField != nil {
			prevField.NextField = firstField
		}
		if isEndField {
			lastField := fieldCodes.Last()
			if len(codes) > 0 {
				codes.First().End = lastField
			} else {
				firstField.End = lastField
			}
		}
		// the next field is linked from the last field of this one:
		// if this field is an embedded struct, it is the last field of that struct, as ToOpcode does.
		prevField = c.lastFieldCode(field, firstField)
		codes = codes.Add(fieldCodes...)
	}
	// The opcodes of an embedded struct are only the fields: they have neither the braces nor the check
	// of nil. So they are not registered to structTypeToCodes, which is where a recursive code jumps to.
	return codes
}

func (c *StructCode) removeFieldsByTags(tags runtime.StructTags) {
	fields := make([]*StructFieldCode, 0, len(c.fields))
	for _, field := range c.fields {
		if field.isAnonymous {
			structCode := field.getAnonymousStruct()
			if structCode != nil && !structCode.isRecursive {
				structCode.removeFieldsByTags(tags)
				if len(structCode.fields) > 0 {
					fields = append(fields, field)
				}
				continue
			}
		}
		if tags.ExistsKey(field.key) {
			continue
		}
		fields = append(fields, field)
	}
	c.fields = fields
}

func (c *StructCode) enableIndirect() {
	if c.isIndirect {
		return
	}
	c.isIndirect = true
	if len(c.fields) == 0 {
		return
	}
	structCode := c.fields[0].getStruct()
	if structCode == nil {
		return
	}
	structCode.enableIndirect()
}

func (c *StructCode) Filter(query *FieldQuery) Code {
	if c.isRecursive {
		// the code of a recursive struct is a jump: the code which is jumped to is filtered when it is linked.
		filtered := *c
		filtered.fieldQuery = query
		return &filtered
	}
	fieldMap := map[string]*FieldQuery{}
	for _, field := range query.Fields {
		fieldMap[field.Name] = field
	}
	fields := make([]*StructFieldCode, 0, len(c.fields))
	for _, field := range c.fields {
		query, exists := fieldMap[field.key]
		if !exists {
			continue
		}
		fieldCode := &StructFieldCode{
			typ:                field.typ,
			key:                field.key,
			tag:                field.tag,
			value:              field.value,
			offset:             field.offset,
			isAnonymous:        field.isAnonymous,
			isTaggedKey:        field.isTaggedKey,
			isNilableType:      field.isNilableType,
			isNilCheck:         field.isNilCheck,
			isAddrForMarshaler: field.isAddrForMarshaler,
			isNextOpPtrType:    field.isNextOpPtrType,
		}
		if len(query.Fields) > 0 {
			fieldCode.value = fieldCode.value.Filter(query)
		}
		fields = append(fields, fieldCode)
	}
	return &StructCode{
		typ:                       c.typ,
		fields:                    fields,
		isPtr:                     c.isPtr,
		disableIndirectConversion: c.disableIndirectConversion,
		isIndirect:                c.isIndirect,
		isRecursive:               c.isRecursive,
	}
}

type StructFieldCode struct {
	typ                reflect.Type
	key                string
	tag                *runtime.StructTag
	value              Code
	offset             uintptr
	isAnonymous        bool
	isTaggedKey        bool
	isNilableType      bool
	isNilCheck         bool
	isAddrForMarshaler bool
	isNextOpPtrType    bool
	isMarshalerContext bool
}

// runKind returns the kind of the field if it is one which has the opcodes of a run ( fieldRunOps ): a field of
// int, uint, float64, string or bool, not a pointer, without omitempty or the string option, and not embedded.
func (c *StructFieldCode) runKind() (CodeKind, bool) {
	if c.isAnonymous || c.tag.IsOmitEmpty || c.tag.IsOmitZero || c.tag.IsString {
		return 0, false
	}
	switch value := c.value.(type) {
	case *IntCode:
		return CodeKindInt, !value.isPtr && !value.isString
	case *UintCode:
		return CodeKindUint, !value.isPtr && !value.isString
	case *FloatCode:
		return CodeKindFloat, !value.isPtr && value.bitSize == 64
	case *StringCode:
		return CodeKindString, !value.isPtr && value.typ != jsonNumberType
	case *BoolCode:
		return CodeKindBool, !value.isPtr
	}
	return 0, false
}

// isRecursiveValueOf is whether the field is a value of typ, the struct it is a field of, or a pointer to one:
// what markTailRecursion encodes without a frame when the field is the last.
func (c *StructFieldCode) isRecursiveValueOf(typ reflect.Type) bool {
	if c.isAnonymous {
		return false
	}
	structCode := c.getStruct()
	return structCode != nil && structCode.isRecursive && structCode.typ == typ
}

func (c *StructFieldCode) getStruct() *StructCode {
	value := c.value
	ptr, ok := value.(*PtrCode)
	if ok {
		value = ptr.value
	}
	structCode, ok := value.(*StructCode)
	if ok {
		return structCode
	}
	return nil
}

func (c *StructFieldCode) getAnonymousStruct() *StructCode {
	if !c.isAnonymous {
		return nil
	}
	return c.getStruct()
}

func optimizeStructField(code *Opcode, tag *runtime.StructTag) OpType {
	fieldType := code.ToFieldType(tag.IsString)
	if tag.IsOmitEmpty {
		fieldType = fieldType.FieldToOmitEmptyField()
	}
	return fieldType
}

// headOpcode returns the opcode of the head of the struct, which precedes the opcode of the first field.
// It is made before the first field, so that it has the index before the ones of the field.
//
// The head is not fused with the first field: that made an opcode per type of a field for the head, which was
// more than half of the VM, for a dispatch saved per struct. The head writes the brace, and the first field
// is encoded by the same opcode as the other fields.
func (c *StructFieldCode) headOpcode(ctx *compileContext, flags OpFlags) *Opcode {
	head := &Opcode{
		Op:         OpStructHead,
		Idx:        opcodeOffset(ctx.ptrIndex),
		Flags:      flags & AnonymousHeadFlags,
		Type:       runtime.TypePtr(c.typ),
		DisplayIdx: ctx.opcodeIndex,
		Indent:     ctx.indent,
	}
	ctx.incOpcodeIndex()
	return head
}

// withHead links the head to the opcodes of the first field.
func withHead(head *Opcode, codes Opcodes) Opcodes {
	head.Next = codes.First()
	head.NextField = codes.First()
	return append(Opcodes{head}, codes...)
}

// isLongKey is whether the key of the field is longer than a chunk: then the field is not encoded by one opcode
// with its value, but by the generic field opcode, which writes a key of any length, and the opcode of the value.
func (c *StructFieldCode) isLongKey(field *Opcode) bool {
	return len(field.Key) > KeyChunkSize
}

// isGenericField is whether the field is encoded by the generic field opcode and the opcode of the value: a
// field of a long key, or of omitzero, whose check the generic opcode makes for a value of any type.
func (c *StructFieldCode) isGenericField(field *Opcode) bool {
	return c.isLongKey(field) || c.tag.IsOmitZero
}

func (c *StructFieldCode) fieldOpcodes(ctx *compileContext, field *Opcode, valueCodes Opcodes) Opcodes {
	value := valueCodes.First()
	var op OpType
	if c.isGenericField(field) {
		op = OpStructField
		if c.tag.IsOmitEmpty || c.tag.IsOmitZero {
			op = OpStructFieldOmitEmpty
		}
		if c.tag.IsString {
			value.Op = value.Op.ToStringOp()
		}
		// the generic field opcode gives the opcode of the value the address of the field, while the opcode of
		// a map takes the map: the map is followed from the address, as the value of a map or of a list is.
		switch value.Op {
		case OpMap:
			value.Op = OpMapPtr
			value.PtrNum = 1
		case OpMapPtr:
			value.PtrNum++
		}
	} else {
		op = optimizeStructField(value, c.tag)
	}
	field.Op = op
	if op == OpStructFieldOmitEmpty {
		if c.tag.IsOmitEmpty {
			field.EmptyKind = emptyKindOf(c.typ)
		}
		if c.tag.IsOmitZero {
			field.ZeroKind = zeroKindOf(c.typ)
		}
	}
	if value.Flags&MarshalerContextFlags != 0 {
		field.Flags |= MarshalerContextFlags
	}
	// the field of interface{} enters the value as the opcode of the value does: it needs to know the interface.
	if value.Flags&NonEmptyInterfaceFlags != 0 {
		field.Flags |= NonEmptyInterfaceFlags
	}
	field.NumBitSize = value.NumBitSize
	field.PtrNum = value.PtrNum
	field.FieldQuery = value.FieldQuery
	field.Marshaler = value.Marshaler

	fieldCodes := Opcodes{field}
	if op.IsMultipleOpField() {
		field.Next = value
		fieldCodes = fieldCodes.Add(valueCodes...)
	} else {
		ctx.decIndex()
	}
	return fieldCodes
}

func (c *StructFieldCode) addStructEndCode(ctx *compileContext, codes Opcodes) Opcodes {
	end := &Opcode{
		Op:         OpStructEnd,
		Idx:        opcodeOffset(ctx.ptrIndex),
		DisplayIdx: ctx.opcodeIndex,
		Indent:     ctx.indent,
	}
	codes.Last().Next = end
	code := codes.First()
	for code.Op == OpStructField || code.Op == OpStructHead {
		code = code.Next
	}
	lastFieldOpcode(code).NextField = end

	codes = codes.Add(end)
	ctx.incOpcodeIndex()
	return codes
}

// KeyChunkSize is the size of the chunk which the VM copies a key by: a key up to it is written by the
// opcode of its field, as a chunk. A longer key is written by the generic field opcode, and its value by the
// opcode of the value.
const KeyChunkSize = 32

// PaddedKey returns the key whose memory has the bytes after it up to a chunk,
// so that a chunk is copied from the key without reading the memory of others.
func PaddedKey(key string) string {
	buf := make([]byte, len(key)+KeyChunkSize)
	copy(buf, key)
	return unsafe.String(unsafe.SliceData(buf), len(key))
}

// keyChunk returns the chunk at the start of a padded key.
func keyChunk(paddedKey string) *[KeyChunkSize]byte {
	return (*[KeyChunkSize]byte)(unsafe.Pointer(unsafe.StringData(paddedKey)))
}

// EmptyKind is what makes the value of a field empty for omitempty, as encoding/json decides it by the kind
// of the type of the field: false, 0, a nil pointer or interface value, or an empty array, slice, map or string.
type EmptyKind uint8

const (
	EmptyNever EmptyKind = iota
	EmptyNil             // a nil pointer or interface value: the first word is zero
	EmptyBool
	EmptyInt // an integer of NumBitSize bits which is 0
	EmptyFloat32
	EmptyFloat64
	EmptyLen    // a string or a slice whose length is zero
	EmptyMapLen // a map whose length is zero
	EmptyAlways // an array of no element
)

// emptyKindOf returns what makes a value of the type empty.
func emptyKindOf(typ reflect.Type) EmptyKind {
	switch typ.Kind() {
	case reflect.Ptr, reflect.Interface:
		return EmptyNil
	case reflect.Bool:
		return EmptyBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return EmptyInt
	case reflect.Float32:
		return EmptyFloat32
	case reflect.Float64:
		return EmptyFloat64
	case reflect.String, reflect.Slice:
		return EmptyLen
	case reflect.Map:
		return EmptyMapLen
	case reflect.Array:
		if typ.Len() == 0 {
			return EmptyAlways
		}
	}
	return EmptyNever
}

// IsEmptyField is whether the value at p, of a field whose kind of emptiness is kind, is empty for omitempty.
func IsEmptyField(kind EmptyKind, bitSize uint8, p unsafe.Pointer) bool {
	switch kind {
	case EmptyNil:
		return *(*unsafe.Pointer)(p) == nil
	case EmptyBool:
		return !*(*bool)(p)
	case EmptyInt:
		switch bitSize {
		case 8:
			return *(*uint8)(p) == 0
		case 16:
			return *(*uint16)(p) == 0
		case 32:
			return *(*uint32)(p) == 0
		}
		return *(*uint64)(p) == 0
	case EmptyFloat32:
		return *(*float32)(p) == 0
	case EmptyFloat64:
		return *(*float64)(p) == 0
	case EmptyLen:
		return (*runtime.SliceHeader)(p).Len == 0
	case EmptyMapLen:
		return MapLen(*(*unsafe.Pointer)(p)) == 0
	case EmptyAlways:
		return true
	}
	return false
}

func (c *StructFieldCode) structKey(ctx *compileContext) string {
	if ctx.escapeKey {
		rctx := &RuntimeContext{Option: &Option{Flag: HTMLEscapeOption}}
		return PaddedKey(fmt.Sprintf(`%s:`, string(AppendString(rctx, []byte{}, c.key))))
	}
	return PaddedKey(fmt.Sprintf(`"%s":`, c.key))
}

func (c *StructFieldCode) flags() OpFlags {
	var flags OpFlags
	if c.isTaggedKey {
		flags |= IsTaggedKeyFlags
	}
	if c.isNilableType {
		flags |= IsNilableTypeFlags
	}
	if c.isNilCheck {
		flags |= NilCheckFlags
	}
	if c.isAddrForMarshaler {
		flags |= AddrForMarshalerFlags
	}
	if c.isNextOpPtrType {
		flags |= IsNextOpPtrTypeFlags
	}
	if c.isAnonymous {
		flags |= AnonymousKeyFlags
	}
	if c.isMarshalerContext {
		flags |= MarshalerContextFlags
	}
	return flags
}

func (c *StructFieldCode) toValueOpcodes(ctx *compileContext) Opcodes {
	if c.isAnonymous {
		anonymCode, ok := c.value.(AnonymousCode)
		if ok {
			return anonymCode.ToAnonymousOpcode(ctx)
		}
	}
	return c.value.ToOpcode(ctx)
}

func (c *StructFieldCode) ToOpcode(ctx *compileContext, isFirstField, isEndField bool) Opcodes {
	var head *Opcode
	if isFirstField {
		head = c.headOpcode(ctx, c.flags())
	}
	key := c.structKey(ctx)
	field := &Opcode{
		Idx:        opcodeOffset(ctx.ptrIndex),
		Flags:      c.flags(),
		Key:        key,
		KeyChunk:   keyChunk(key),
		Offset:     uint32(c.offset),
		Type:       runtime.TypePtr(c.typ),
		DisplayIdx: ctx.opcodeIndex,
		Indent:     ctx.indent,
	}
	ctx.incIndex()
	valueCodes := c.toValueOpcodes(ctx)
	codes := c.fieldOpcodes(ctx, field, valueCodes)
	if isEndField {
		if isEnableStructEndOptimization(c.value) && !c.isGenericField(field) {
			field.Op = field.Op.FieldToEnd()
		} else {
			codes = c.addStructEndCode(ctx, codes)
		}
	}
	if head != nil {
		codes = withHead(head, codes)
	}
	return codes
}

func (c *StructFieldCode) ToAnonymousOpcode(ctx *compileContext, isFirstField, isEndField bool) Opcodes {
	var head *Opcode
	if isFirstField {
		head = c.headOpcode(ctx, c.flags()|AnonymousHeadFlags)
	}
	key := c.structKey(ctx)
	field := &Opcode{
		Idx:        opcodeOffset(ctx.ptrIndex),
		Flags:      c.flags() | AnonymousHeadFlags,
		Key:        key,
		KeyChunk:   keyChunk(key),
		Offset:     uint32(c.offset),
		Type:       runtime.TypePtr(c.typ),
		DisplayIdx: ctx.opcodeIndex,
		Indent:     ctx.indent,
	}
	ctx.incIndex()
	valueCodes := c.toValueOpcodes(ctx)
	codes := c.fieldOpcodes(ctx, field, valueCodes)
	if head != nil {
		codes = withHead(head, codes)
	}
	return codes
}

func isEnableStructEndOptimization(value Code) bool {
	switch value.Kind() {
	case CodeKindInt,
		CodeKindUint,
		CodeKindFloat,
		CodeKindString,
		CodeKindBool,
		CodeKindBytes:
		return true
	case CodeKindPtr:
		return isEnableStructEndOptimization(value.(*PtrCode).value)
	default:
		return false
	}
}

type InterfaceCode struct {
	typ        reflect.Type
	fieldQuery *FieldQuery
	isPtr      bool
}

func (c *InterfaceCode) Kind() CodeKind {
	return CodeKindInterface
}

func (c *InterfaceCode) ToOpcode(ctx *compileContext) Opcodes {
	var code *Opcode
	switch {
	case c.isPtr:
		code = newOpCode(ctx, c.typ, OpInterfacePtr)
	default:
		code = newOpCode(ctx, c.typ, OpInterface)
	}
	code.FieldQuery = c.fieldQuery
	if c.typ.NumMethod() > 0 {
		code.Flags |= NonEmptyInterfaceFlags
	}
	ctx.incIndex()
	return Opcodes{code}
}

func (c *InterfaceCode) Filter(query *FieldQuery) Code {
	return &InterfaceCode{
		typ:        c.typ,
		fieldQuery: query,
		isPtr:      c.isPtr,
	}
}

type MarshalJSONCode struct {
	typ                reflect.Type
	fieldQuery         *FieldQuery
	isAddrForMarshaler bool
	isNilableType      bool
	isMarshalerContext bool
}

func (c *MarshalJSONCode) Kind() CodeKind {
	return CodeKindMarshalJSON
}

func (c *MarshalJSONCode) ToOpcode(ctx *compileContext) Opcodes {
	code := newOpCode(ctx, c.typ, OpMarshalJSON)
	code.FieldQuery = c.fieldQuery
	code.Marshaler = c.marshalerCall()
	if c.isAddrForMarshaler {
		code.Flags |= AddrForMarshalerFlags
	}
	if c.isMarshalerContext {
		code.Flags |= MarshalerContextFlags
	}
	if c.isNilableType {
		code.Flags |= IsNilableTypeFlags
	} else {
		code.Flags &= ^IsNilableTypeFlags
	}
	ctx.incIndex()
	return Opcodes{code}
}

// marshalerCall returns the direct call of the method, or nil if the method is called through the interface:
// a value of the size of a pointer whose method is on the pointer is copied first, see addrForMarshaler.
func (c *MarshalJSONCode) marshalerCall() *MarshalerCall {
	recv := c.typ
	if c.isAddrForMarshaler {
		if c.typ.Size() == unsafe.Sizeof(unsafe.Pointer(nil)) {
			return nil
		}
		recv = reflect.PointerTo(c.typ)
	}
	iface := marshalJSONType
	if c.isMarshalerContext {
		iface = marshalJSONContextType
	}
	return newMarshalerCall(recv, iface)
}

func (c *MarshalJSONCode) Filter(query *FieldQuery) Code {
	return &MarshalJSONCode{
		typ:                c.typ,
		fieldQuery:         query,
		isAddrForMarshaler: c.isAddrForMarshaler,
		isNilableType:      c.isNilableType,
		isMarshalerContext: c.isMarshalerContext,
	}
}

type MarshalTextCode struct {
	typ                reflect.Type
	fieldQuery         *FieldQuery
	isAddrForMarshaler bool
	isNilableType      bool
	// isInterfaceMapKey is whether the code is the one of the key of a map of an interface type, whose name is
	// of the dynamic value of the key ( see appendInterfaceMapKey ).
	isInterfaceMapKey bool
	// isMapKey is whether the code is the one of the key of a map, whose name is "" for a nil pointer.
	isMapKey bool
}

func (c *MarshalTextCode) Kind() CodeKind {
	return CodeKindMarshalText
}

func (c *MarshalTextCode) ToOpcode(ctx *compileContext) Opcodes {
	code := newOpCode(ctx, c.typ, OpMarshalText)
	code.FieldQuery = c.fieldQuery
	if c.isInterfaceMapKey {
		// the opcode is given the address of the key, whose name AppendMarshalText makes without a marshaler.
		code.Flags |= InterfaceMapKeyFlags
		code.Flags &= ^IsNilableTypeFlags
		ctx.incIndex()
		return Opcodes{code}
	}
	code.Marshaler = c.marshalerCall()
	if c.isAddrForMarshaler {
		code.Flags |= AddrForMarshalerFlags
	}
	if c.isMapKey {
		code.Flags |= MapKeyFlags
	}
	if c.isNilableType {
		code.Flags |= IsNilableTypeFlags
	} else {
		code.Flags &= ^IsNilableTypeFlags
	}
	ctx.incIndex()
	return Opcodes{code}
}

// marshalerCall returns the direct call of the method, or nil if the method is called through the interface:
// see MarshalJSONCode.marshalerCall.
func (c *MarshalTextCode) marshalerCall() *MarshalerCall {
	recv := c.typ
	if c.isAddrForMarshaler {
		if c.typ.Size() == unsafe.Sizeof(unsafe.Pointer(nil)) {
			return nil
		}
		recv = reflect.PointerTo(c.typ)
	}
	return newMarshalerCall(recv, marshalTextType)
}

func (c *MarshalTextCode) Filter(query *FieldQuery) Code {
	return &MarshalTextCode{
		typ:                c.typ,
		fieldQuery:         query,
		isAddrForMarshaler: c.isAddrForMarshaler,
		isNilableType:      c.isNilableType,
		isInterfaceMapKey:  c.isInterfaceMapKey,
		isMapKey:           c.isMapKey,
	}
}

type PtrCode struct {
	typ    reflect.Type
	value  Code
	ptrNum uint8
}

func (c *PtrCode) Kind() CodeKind {
	return CodeKindPtr
}

func (c *PtrCode) ToOpcode(ctx *compileContext) Opcodes {
	codes := c.value.ToOpcode(ctx)
	codes.First().Op = convertPtrOp(codes.First())
	codes.First().PtrNum = c.ptrNum
	return codes
}

func (c *PtrCode) ToAnonymousOpcode(ctx *compileContext) Opcodes {
	var codes Opcodes
	anonymCode, ok := c.value.(AnonymousCode)
	if ok {
		codes = anonymCode.ToAnonymousOpcode(ctx)
	} else {
		codes = c.value.ToOpcode(ctx)
	}
	codes.First().Op = convertPtrOp(codes.First())
	codes.First().PtrNum = c.ptrNum
	return codes
}

func (c *PtrCode) Filter(query *FieldQuery) Code {
	return &PtrCode{
		typ:    c.typ,
		value:  c.value.Filter(query),
		ptrNum: c.ptrNum,
	}
}

func convertPtrOp(code *Opcode) OpType {
	ptrHeadOp := code.Op.HeadToPtrHead()
	if code.Op != ptrHeadOp {
		if code.PtrNum > 0 {
			// ptr field and ptr head
			code.PtrNum--
		}
		return ptrHeadOp
	}
	switch code.Op {
	case OpInt:
		return OpIntPtr
	case OpUint:
		return OpUintPtr
	case OpFloat32:
		return OpFloat32Ptr
	case OpFloat64:
		return OpFloat64Ptr
	case OpString:
		return OpStringPtr
	case OpBool:
		return OpBoolPtr
	case OpBytes:
		return OpBytesPtr
	case OpNumber:
		return OpNumberPtr
	case OpArray:
		return OpArrayPtr
	case OpSlice:
		return OpSlicePtr
	case OpMap:
		return OpMapPtr
	case OpMarshalJSON:
		return OpMarshalJSONPtr
	case OpMarshalText:
		return OpMarshalTextPtr
	case OpInterface:
		return OpInterfacePtr
	case OpRecursive:
		return OpRecursivePtr
	}
	return code.Op
}

func isEmbeddedStruct(field *StructFieldCode) bool {
	if !field.isAnonymous {
		return false
	}
	t := field.typ
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct
}
