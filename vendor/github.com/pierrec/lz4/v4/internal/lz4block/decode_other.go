//go:build (!amd64 && !arm && !arm64) || !gc || noasm || purego

package lz4block

func decodeBlock(dst, src, dict []byte) int { return decodeBlockGo(dst, src, dict) }
