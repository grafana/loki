package util

import "unsafe"

func YoloBuf(s string) []byte {
	return *((*[]byte)(unsafe.Pointer(&s))) //#nosec G103 -- This is used correctly; all uses of this function do not allow the mutable reference to escape
}
