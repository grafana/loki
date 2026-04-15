//go:build purego || !amd64

package bytealg

func Broadcast(dst []byte, src byte) {
	for i := range dst {
		dst[i] = src
	}
}
