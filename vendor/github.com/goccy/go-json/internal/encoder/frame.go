package encoder

import (
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

// The frames of the VM.
//
// The value held by an interface value and the value of a recursive type are encoded by the opcodes of their
// own type, in a frame of slots after the frame of the opcodes which reached them. The opcode which ends
// such a frame has three slots: the opcode to return to with the tail levels to restore, the offset of the
// previous frame and the indent to restore.
//
// Entering and leaving a frame is done here, not in the VM: it needs several calls and many variables, and
// the VM, which is a large function, has to spill and restore its variables around each of them.

type nonEmptyInterface struct {
	itab *struct {
		ityp unsafe.Pointer // static interface type
		typ  unsafe.Pointer // dynamic concrete type
		// unused fields...
	}
	ptr unsafe.Pointer
}

func storeSlotPtr(base unsafe.Pointer, idx uint32, p unsafe.Pointer) {
	*(*uintptr)(unsafe.Add(base, idx)) = uintptr(p)
}

func storeSlotInt(base unsafe.Pointer, idx uint32, v uintptr) {
	*(*uintptr)(unsafe.Add(base, idx)) = v
}

func loadSlotPtr(base unsafe.Pointer, idx uint32) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Add(base, idx))
}

func loadSlotInt(base unsafe.Pointer, idx uint32) uintptr {
	return *(*uintptr)(unsafe.Add(base, idx))
}

// recordSeen records the value to detect a cycle, after the nesting got deep: a cycle keeps passing the same
// values, so it is still detected, and the values which are not nested deeply cost nothing.
func (c *RuntimeContext) recordSeen(code *Opcode, p unsafe.Pointer) error {
	id := SeenValue{p: p, typ: code.Type}
	if p != nil && c.seen(id) {
		return ErrUnsupportedValue(c, code, p)
	}
	c.SeenPtr = append(c.SeenPtr, id)
	return nil
}

// seen reports whether id is recorded: by its address first, which the other records rarely have.
func (c *RuntimeContext) seen(id SeenValue) bool {
	for _, v := range c.SeenPtr {
		if v.p == id.p && v == id {
			return true
		}
	}
	return false
}

// entersNilValue is whether the value of the type typ, held by an interface value whose data word is nil, is
// encoded by the opcodes of typ: a struct or an array stored directly, whose single pointer is nil, and for the
// v2 semantics a value of any type but a pointer, which writes a nil map as {} and fails for a nil func. The
// other values are null, as no type is.
func (c *RuntimeContext) entersNilValue(typ unsafe.Pointer) bool {
	if typ == nil {
		return false
	}
	if c.Option.Flag&MarshalFuncsOption != 0 {
		// a function of marshaling may take a nil pointer: its type tells.
		return true
	}
	if c.Option.Flag&V2Option != 0 {
		return runtime.TypeOfPtr(typ).Kind() != reflect.Ptr
	}
	return ShapeOf(typ) == ValueShapeAggregate && !IfaceIndir(typ)
}

// recordSeenValue is recordSeen of the value at p, of the type of codeSet, which a frame of an interface value
// encodes: the value is recorded by its identity, as the interface value which holds it may be a copy, as the
// value of a map is.
func (c *RuntimeContext) recordSeenValue(codeSet *OpcodeSet, p unsafe.Pointer) error {
	id := seenValueOf(codeSet, p)
	if id.p != nil && c.seen(id) {
		return errCycle(c, codeSet.Type, p)
	}
	c.SeenPtr = append(c.SeenPtr, id)
	return nil
}

// seenValueOf returns the record of the value at p, of the type of codeSet, by its identity ( see recordSeenValue ).
func seenValueOf(codeSet *OpcodeSet, p unsafe.Pointer) SeenValue {
	id := SeenValue{p: p, typ: runtime.TypePtr(codeSet.Type)}
	// a value is identified by its first word, which a cycle passes again and again, if it is a map, a slice (
	// the address of its array ), or a type stored directly in an interface value, as a pointer which a function
	// of marshaling takes. The identity of another value, as a pointer which is its address, is its address.
	if !codeSet.DataWordIsAddr || codeSet.Type.Kind() == reflect.Slice {
		id.p = *(*unsafe.Pointer)(p)
		switch codeSet.Type.Kind() {
		case reflect.Slice:
			id.n = (*runtime.SliceHeader)(p).Len
		case reflect.Struct:
			// a struct stored directly in the interface value, recorded by the pointer it holds, is not the struct
			// of its type which a recursive frame records at that address.
			id.n = -1
		}
	}
	return id
}

// EnterNested makes c, which encodes the value at p by codeSet within the encoding by outer, the frame after the
// ones of outer for the detection of cycles: a default representation of a value, which runs in a context of its
// own ( see runInner ), or MarshalEncode called by a method, whose cycles pass no frame of the VM. Past
// StartDetectingCyclesAfter, c has the records of outer, in the same arrays, as outer goes on only after c ends, and
// records the value by codeSet, not by its type: a value which a function declined is encoded again by the opcodes
// of its default representation, at the same address.
func EnterNested(c, outer *RuntimeContext, codeSet *OpcodeSet, p unsafe.Pointer) error {
	c.RecursiveLevel = outer.RecursiveLevel + 1
	if c.RecursiveLevel <= StartDetectingCyclesAfter {
		return nil
	}
	return recordNested(c, outer, codeSet, p)
}

// recordNested is EnterNested past StartDetectingCyclesAfter, apart from it, so that the calls which are not nested
// deeply have EnterNested inlined. It is a function, not a method: a method of RuntimeContext changes its method
// table, which moved data the encodings read and made them slower.
func recordNested(c, outer *RuntimeContext, codeSet *OpcodeSet, p unsafe.Pointer) error {
	c.SeenPtr, c.seenMaps = outer.SeenPtr, outer.seenMaps
	c.nested, c.sharedSeen, outer.nested = true, true, true
	id := seenValueOf(codeSet, p)
	id.typ = unsafe.Pointer(codeSet)
	if id.p != nil && c.seen(id) {
		return errCycle(c, codeSet.Type, p)
	}
	c.SeenPtr = append(c.SeenPtr, id)
	return nil
}

// enterFrame allocates the frame of the code after the current one, stores the value at its first slot and
// what the end opcode restores, and returns the base of the slots of the new frame.
func (c *RuntimeContext) enterFrame(first, end, next *Opcode, p unsafe.Pointer, curLen, nextLen uintptr, indent uint32) unsafe.Pointer {
	oldOffset := c.SlotOffset
	c.SlotOffset += curLen * slotSize
	c.ReserveSlots(oldOffset/slotSize + curLen + nextLen)
	base := unsafe.Add(c.Ptr(), c.SlotOffset)
	storeSlotPtr(base, first.Idx, p)
	storeSlotPtr(base, end.Idx, unsafe.Pointer(next))
	storeSlotInt(base, end.ElemIdx, oldOffset)
	storeSlotInt(base, end.Length, uintptr(c.BaseIndent))
	// the tail levels of the frame left are in the other half of the slot of the opcode to return to.
	storeSlotInt(base, end.Idx+slotIntOffset, uintptr(c.TailLevels))
	c.BaseIndent = indent
	c.TailLevels = 0
	c.RecursiveLevel++
	return base
}

// EnterInterface enters the frame of the value held by the interface value at p, which is the operand of
// code. It returns the first opcode of the frame and the base of its slots, or a nil opcode if the value is
// nil, which the caller writes as null.
//
// If the value is a scalar, no frame is entered: it returns the opcode of the scalar, the address of the value
// and true, and the caller encodes the value by the opcode in its own frame. It saves the frame and two
// dispatches for the values which JSON has in a value of interface{}.
//
//go:noinline
func (c *RuntimeContext) EnterInterface(code *Opcode, p unsafe.Pointer) (*Opcode, unsafe.Pointer, bool, error) {
	var typ, ifacePtr unsafe.Pointer
	// a value of interface{} is told from the others by one check.
	switch {
	case code.Flags&(NonEmptyInterfaceFlags|StaticTypeFlags) == 0:
		iface := (*emptyInterface)(p)
		ifacePtr = iface.ptr
		typ = iface.typ
	case code.Flags&StaticTypeFlags != 0:
		// a value of the type of the opcode, encoded as it would be held by an interface value, by the opcodes
		// of the type. p is what they take: the address of the value, or, for a pointer type compiled to the value
		// it points to, the pointer itself ( see InterfaceCode.static ).
		typ = code.Type
		ifacePtr = p
	default:
		iface := (*nonEmptyInterface)(p)
		ifacePtr = iface.ptr
		if iface.itab != nil {
			typ = iface.itab.typ
		}
	}
	if ifacePtr == nil && !c.entersNilValue(typ) {
		return nil, nil, false, nil
	}
	codeSet := c.RecentCodeSet(uintptr(typ))
	if codeSet == nil {
		if c.Option.Flag&UncachedOption == 0 {
			codeSet = c.SharedCodeSets().LoadFirst(uintptr(typ))
		}
		if codeSet != nil {
			c.RememberCodeSet(uintptr(typ), codeSet)
		} else {
			var err error
			codeSet, err = CompileToGetCodeSet(c, uintptr(typ))
			if err != nil {
				return nil, nil, false, err
			}
		}
	}
	if codeSet.Scalar != nil {
		// a scalar is never stored directly in an interface value: the data word is its address.
		return codeSet.Scalar, ifacePtr, true, nil
	}
	// The opcodes take the address of the value. The data word of the interface value is the address for
	// most of the types, and the value itself for a type of a pointer shape which is not a pointer, such as a
	// map: then the address of the data word, in the interface value at p, is the address of the value.
	value := ifacePtr
	if !codeSet.DataWordIsAddr && code.Flags&StaticTypeFlags == 0 {
		value = unsafe.Add(p, unsafe.Sizeof(uintptr(0)))
	}
	indent := c.BaseIndent + code.Indent
	if indent+codeSet.Levels > MaxDepth {
		return nil, nil, false, ErrMaxDepth(c)
	}
	// after every path which doesn't go into the value, so that a record always has its end.
	if c.RecursiveLevel > StartDetectingCyclesAfter {
		if err := c.recordSeenValue(codeSet, value); err != nil {
			return nil, nil, false, err
		}
	}
	first := codeSet.InterfaceKeyCodes[c.Option.Flag&KeyEscapeOptions]
	base := c.enterFrame(first, codeSet.EndCode, code.Next, value,
		uintptr(code.Length)+interfaceEndSlots, uintptr(codeSet.CodeLength)+interfaceEndSlots, indent)
	return first, base, false, nil
}

// interfaceEndSlots is the number of the slots which a frame of an interface value has after the ones of
// the code.
const interfaceEndSlots = 3

// EnterRecursive enters the frame of the value of the recursive type at p, which is the operand of code.
// It returns the first opcode of the frame and the base of its slots.
//
//go:noinline
func (c *RuntimeContext) EnterRecursive(code *Opcode, p unsafe.Pointer) (*Opcode, unsafe.Pointer, error) {
	if code.Jmp.Code == nil {
		// an embedded struct which has no field to write there ( see Compiler.linkRecursiveCode ): no frame.
		return code.Next, unsafe.Add(c.Ptr(), c.SlotOffset), nil
	}
	if c.RecursiveLevel > StartDetectingCyclesAfter {
		if err := c.recordSeen(code, p); err != nil {
			return nil, nil, err
		}
	}
	first := code.Jmp.Code
	// the fields of the value are one level deeper than the place of the value, which opens it, and at the level
	// of the place of an embedded struct, which writes them to the object it is embedded in.
	indentDiffFromTop := first.Indent - 1
	if code.Jmp.Embedded {
		indentDiffFromTop++
	}
	indent := c.BaseIndent + code.Indent - indentDiffFromTop
	if indent+code.Jmp.Levels > MaxDepth && p != nil {
		// a nil pointer is null, which opens no level.
		return nil, nil, ErrMaxDepth(c)
	}
	base := c.enterFrame(first, first.End.Next, code.Next, p, code.Jmp.CurLen, code.Jmp.NextLen, indent)
	return first, base, nil
}

// LeaveFrame leaves the frame which the end opcode ends, and returns the opcode to go on with and the base of
// the slots of the previous frame.
//
//go:noinline
func (c *RuntimeContext) LeaveFrame(end *Opcode) (*Opcode, unsafe.Pointer) {
	base := unsafe.Add(c.Ptr(), c.SlotOffset)
	c.RecursiveLevel--
	if c.RecursiveLevel > StartDetectingCyclesAfter {
		c.SeenPtr = c.SeenPtr[:len(c.SeenPtr)-1]
	}
	c.BaseIndent = uint32(loadSlotInt(base, end.Length))
	c.TailLevels = uint32(loadSlotInt(base, end.Idx+slotIntOffset))
	c.SlotOffset = loadSlotInt(base, end.ElemIdx)
	return (*Opcode)(loadSlotPtr(base, end.Idx)), unsafe.Add(c.Ptr(), c.SlotOffset)
}

// The last field of a value of a recursive type may be a value of the same type: a list. Such a value is
// encoded in the frame of the value it is the field of ( TailRecursiveFlags ): the slots of that value are not
// needed any more, and the only thing left to do for it is to close its braces, which the end of the last
// value of the list does for every value of the list. No frame is entered and left for a value of the list, so
// the slots, the offsets and the opcode to return to are neither stored nor restored, and the end of a value
// is not dispatched: that makes a list as cheap as a nest of values of different types. The VM does it inline,
// as the functions here cost more than the inliner allows into it; the end opcode of the list has what the
// indent is deeper by for a value of it.

// RecordSeen records the value of a recursive type at p for the detection of cycles: the VM calls it before
// it begins a value of a list, when the level is deep enough for the detection.
func (c *RuntimeContext) RecordSeen(code *Opcode, p unsafe.Pointer) error {
	return c.recordSeen(code, p)
}
