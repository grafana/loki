//go:build !purego

package bytealg

//go:noescape
func broadcastAVX2(dst []byte, src byte)

// Broadcast writes the src value to all bytes of dst.
func Broadcast(dst []byte, src byte) {
	if len(dst) >= 8 && hasAVX2 {
		broadcastAVX2(dst, src)
	} else {
		for i := range dst {
			dst[i] = src
		}
	}
}
