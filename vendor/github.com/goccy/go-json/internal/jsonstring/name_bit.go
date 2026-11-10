package jsonstring

// NameBit is one of 64 bits for a name of an object, chosen by the length of the name and its first and last
// bytes: two names of other bits differ. The bits of the names of an object tell most new names from the ones it
// has without comparing them, where a name which an object has twice is an error.
func NameBit(name []byte) uint64 {
	h := uint(len(name))
	if len(name) > 0 {
		h += uint(name[0])*7 + uint(name[len(name)-1])*31
	}
	return 1 << (h & 63)
}
