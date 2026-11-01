package encoder

import (
	"reflect"
	"unsafe"
)

// startDetectingKeyCyclesAfter is the number of the pointers a key is followed through before they are checked for
// a cycle, as encoding/json starts to check its values after 1000 levels: a key whose pointers lead back to
// themselves, as a key of type R *R can, never reaches a value which names it, and the keys of fewer pointers cost
// nothing more than counting them.
const startDetectingKeyCyclesAfter = 1000

// keyCycle finds a cycle of the pointers which a key is followed through, one by one, by v1 and v2: past
// startDetectingKeyCyclesAfter, each pointer is compared with one seen before it, which is replaced at every power
// of 2 ( Brent's algorithm ), so a cycle is found within twice its length. Two pointers are the same by their type
// and their address: a pointer to a struct and one to its first field have the same address.
type keyCycle struct {
	steps, power, length int
	typ                  reflect.Type
	addr                 unsafe.Pointer
}

// reached is whether v, the next pointer or interface of a key, is one which was followed before.
func (c *keyCycle) reached(v reflect.Value) bool {
	if c.steps < startDetectingKeyCyclesAfter || v.Kind() != reflect.Pointer {
		// an interface holds a value which is not an interface: a cycle has a pointer in it.
		c.steps++
		return false
	}
	return c.reachedPointer(v.Type(), v.UnsafePointer())
}

// reachedPointer is reached for the next pointer of a key, the pointer addr of the type typ: it is compared with the
// one remembered, which it replaces at every power of 2.
func (c *keyCycle) reachedPointer(typ reflect.Type, addr unsafe.Pointer) bool {
	if c.steps++; c.steps <= startDetectingKeyCyclesAfter {
		return false
	}
	if typ == c.typ && addr == c.addr {
		return true
	}
	if c.length == c.power {
		c.typ, c.addr = typ, addr
		c.power, c.length = max(2*c.power, 1), 0
	}
	c.length++
	return false
}
