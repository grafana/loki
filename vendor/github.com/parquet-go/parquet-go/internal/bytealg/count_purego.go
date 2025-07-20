//go:build purego || !amd64

package bytealg

import "bytes"

func Count(data []byte, value byte) int {
	return bytes.Count(data, []byte{value})
}
