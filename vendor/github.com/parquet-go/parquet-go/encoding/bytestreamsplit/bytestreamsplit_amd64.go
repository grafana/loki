//go:build !purego

package bytestreamsplit

import (
	"golang.org/x/sys/cpu"
)

var encodeFloatHasAVX512 = cpu.X86.HasAVX512 &&
	cpu.X86.HasAVX512F &&
	cpu.X86.HasAVX512VL

var encodeDoubleHasAVX512 = cpu.X86.HasAVX512 &&
	cpu.X86.HasAVX512F &&
	cpu.X86.HasAVX512VL &&
	cpu.X86.HasAVX512VBMI // VPERMB

var decodeFloatHasAVX2 = cpu.X86.HasAVX2

var decodeDoubleHasAVX512 = cpu.X86.HasAVX512 &&
	cpu.X86.HasAVX512F &&
	cpu.X86.HasAVX512VL &&
	cpu.X86.HasAVX512VBMI // VPERMB

//go:noescape
func encodeFloat(dst, src []byte)

//go:noescape
func encodeDouble(dst, src []byte)

//go:noescape
func decodeFloat(dst, src []byte)

//go:noescape
func decodeDouble(dst, src []byte)
