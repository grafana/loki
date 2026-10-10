//go:build (!amd64 && !arm && !arm64) || appengine || !gc || noasm
// +build !amd64,!arm,!arm64 appengine !gc noasm

package lz4block

func decodeBlock(dst, src, dict []byte) int { return decodeBlockGo(dst, src, dict) }
