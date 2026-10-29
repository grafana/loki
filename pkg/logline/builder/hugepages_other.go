//go:build !linux

package builder

func adviseHugePageRange([]byte) {}
