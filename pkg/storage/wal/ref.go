package wal

type DataRef uint64

func NewDataRef(offset, size uint64) DataRef {
	return DataRef(offset<<32 | size)
}

func (b DataRef) Unpack() (int, int) {
	offset := int(b >> 32)
	size := int((b << 32) >> 32)
	return offset, size
}
