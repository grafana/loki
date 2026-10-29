package builder

import "unsafe"

// adviseHugePages requests transparent huge pages for the four sort buffers.
func adviseHugePages(keys [][8]byte, docs []uint32, keyBuf [][8]byte, docBuf []uint32) {
	adviseHugePageRange(sliceBytes(keys))
	adviseHugePageRange(sliceBytes(docs))
	adviseHugePageRange(sliceBytes(keyBuf))
	adviseHugePageRange(sliceBytes(docBuf))
}

func sliceBytes[T any](s []T) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*int(unsafe.Sizeof(s[0])))
}
