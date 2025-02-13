//go:build purego || !amd64

package bloom

func filterInsertBulk(f []Block, x []uint64) {
	for i := range x {
		filterInsert(f, x[i])
	}
}

func filterInsert(f []Block, x uint64) {
	f[fasthash1x64(x, int32(len(f)))].Insert(uint32(x))
}

func filterCheck(f []Block, x uint64) bool {
	return f[fasthash1x64(x, int32(len(f)))].Check(uint32(x))
}
