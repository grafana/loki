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

func encodeInt32(dst, src []byte) { encodeFloat(dst, src) }

func decodeInt32(dst, src []byte) { decodeFloat(dst, src) }

func encodeInt64(dst, src []byte) { encodeDouble(dst, src) }

func decodeInt64(dst, src []byte) { decodeDouble(dst, src) }
