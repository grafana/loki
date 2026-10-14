package encoder

import (
	"slices"
	"unsafe"
)

// The frames of the VM.
//
// The value held by an interface value and the value of a recursive type are encoded by the opcodes of their
// own type, in a frame of slots after the frame of the opcodes which reached them. The opcode which ends
// such a frame has three slots: the opcode to return to, the offset of the previous frame and the indent to
// restore.
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
	if p != nil {
		if slices.Contains(c.SeenPtr, p) {
			return ErrUnsupportedValue(code, p)
		}
	}
	c.SeenPtr = append(c.SeenPtr, p)
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
	// the indent and the tail levels of the frame left are in one slot: both are small.
	storeSlotInt(base, end.Length, uintptr(c.BaseIndent)|uintptr(c.TailLevels)<<32)
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
	if code.Flags&NonEmptyInterfaceFlags != 0 {
		iface := (*nonEmptyInterface)(p)
		ifacePtr = iface.ptr
		if iface.itab != nil {
			typ = iface.itab.typ
		}
	} else {
		iface := (*emptyInterface)(p)
		ifacePtr = iface.ptr
		typ = iface.typ
	}
	if ifacePtr == nil {
		isDirectedNil := typ != nil && ShapeOf(typ) == ValueShapeAggregate && !IfaceIndir(typ)
		if !isDirectedNil {
			return nil, nil, false, nil
		}
	}
	codeSet := c.RecentCodeSet(uintptr(typ))
	if codeSet == nil {
		var err error
		codeSet, err = CompileToGetCodeSet(c, uintptr(typ))
		if err != nil {
			return nil, nil, false, err
		}
	}
	if codeSet.Scalar != nil {
		// a scalar is never stored directly in an interface value: the data word is its address.
		return codeSet.Scalar, ifacePtr, true, nil
	}
	// after every path which doesn't go into the value, so that a record always has its end.
	if c.RecursiveLevel > StartDetectingCyclesAfter {
		if err := c.recordSeen(code, p); err != nil {
			return nil, nil, false, err
		}
	}
	var first *Opcode
	if (c.Option.Flag & HTMLEscapeOption) != 0 {
		first = codeSet.InterfaceEscapeKeyCode
	} else {
		first = codeSet.InterfaceNoescapeKeyCode
	}
	// The opcodes take the address of the value. The data word of the interface value is the address for
	// most of the types, and the value itself for a type of a pointer shape which is not a pointer, such as a
	// map: then the address of the data word, in the interface value at p, is the address of the value.
	value := ifacePtr
	if !codeSet.DataWordIsAddr {
		value = unsafe.Add(p, unsafe.Sizeof(uintptr(0)))
	}
	base := c.enterFrame(first, codeSet.EndCode, code.Next, value,
		uintptr(code.Length)+interfaceEndSlots, uintptr(codeSet.CodeLength)+interfaceEndSlots, c.BaseIndent+code.Indent)
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
	if c.RecursiveLevel > StartDetectingCyclesAfter {
		if err := c.recordSeen(code, p); err != nil {
			return nil, nil, err
		}
	}
	first := code.Jmp.Code
	indentDiffFromTop := first.Indent - 1
	base := c.enterFrame(first, first.End.Next, code.Next, p,
		code.Jmp.CurLen, code.Jmp.NextLen, c.BaseIndent+code.Indent-indentDiffFromTop)
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
	saved := loadSlotInt(base, end.Length)
	c.BaseIndent = uint32(saved)
	c.TailLevels = uint32(saved >> 32)
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
