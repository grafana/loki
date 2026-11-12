//go:build (amd64 || arm || arm64) && gc && !noasm

package lz4block

//go:noescape
func decodeBlock(dst, src, dict []byte) int
