package guest

import "unsafe"

//go:wasmimport env pushString_import
func pushString_import(ptr, size uint32)

func PushString(s string) {
	pushString_import(stringToPtr(s))
}

func PushStrings(s []string) {
	for _, str := range s {
		PushString(str)
	}
}

func stringToPtr(s string) (uint32, uint32) {
	ptr := unsafe.Pointer(unsafe.StringData(s))
	return uint32(uintptr(ptr)), uint32(len(s))
}

func ReadString(stringPtr uint64) string {
	// offset is high 32 bits, size is low 32 bits
	offset := uint32(stringPtr >> 32)
	size := uint32(stringPtr & 0xFFFFFFFF)

	// Read name from memory at address 0
	nameBytes := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(offset))), size)
	return string(nameBytes)
}
