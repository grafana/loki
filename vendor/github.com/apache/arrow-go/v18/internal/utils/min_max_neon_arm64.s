//+build !noasm !appengine

// ARROW-15336: optimized NEON min/max for ARM64
// 8-bit functions use .16b (128-bit Q registers, 16 lanes) processing 32 elements/iteration
// 16-bit functions use .8h (128-bit Q registers, 8 lanes) processing 16 elements/iteration
// 32-bit functions use .4s (128-bit Q registers, 4 lanes) processing 8 elements/iteration
// 64-bit functions use BIT/BIF instead of BSL+MOV to eliminate register saves

// func _int8_max_min_neon(values unsafe.Pointer, length int, minout, maxout unsafe.Pointer)
TEXT ·_int8_max_min_neon(SB), $0-32

	MOVD    values+0(FP), R0
	MOVD    length+8(FP), R1
	MOVD    minout+16(FP), R2
	MOVD    maxout+24(FP), R3

	WORD $0x7100043f // cmp    w1, #1
	BLT int8_early_exit

	WORD $0x71007c3f // cmp    w1, #31
	WORD $0x2a0103e8 // mov    w8, w1
	BHI int8_neon

	WORD $0xaa1f03e9 // mov    x9, xzr
	WORD $0x5280100b // mov    w11, #128
	WORD $0x52800fea // mov    w10, #127
	JMP int8_scalar
int8_early_exit:
	WORD $0x52800fea // mov    w10, #127
	WORD $0x5280100b // mov    w11, #128
	WORD $0x3900006b // strb   w11, [x3]
	WORD $0x3900004a // strb   w10, [x2]
	RET
int8_neon:
	WORD $0x927be909 // and    x9, x8, #0xffffffffffffffe0
	WORD $0x4f03e7e0 // movi   v0.16b, #127
	WORD $0x4f03e7e1 // movi   v1.16b, #127
	WORD $0x4f04e402 // movi   v2.16b, #128
	WORD $0x4f04e403 // movi   v3.16b, #128
	WORD $0x9100400a // add    x10, x0, #16
	WORD $0xaa0903eb // mov    x11, x9
int8_loop:
	WORD $0xad7f9544 // ldp    q4, q5, [x10, #-16]
	WORD $0x4e246c00 // smin   v0.16b, v0.16b, v4.16b
	WORD $0x4e256c21 // smin   v1.16b, v1.16b, v5.16b
	WORD $0x4e246442 // smax   v2.16b, v2.16b, v4.16b
	WORD $0x4e256463 // smax   v3.16b, v3.16b, v5.16b
	WORD $0xf100816b // subs   x11, x11, #32
	WORD $0x9100814a // add    x10, x10, #32
	BNE int8_loop

	WORD $0x4e216c00 // smin   v0.16b, v0.16b, v1.16b
	WORD $0x4e236442 // smax   v2.16b, v2.16b, v3.16b
	WORD $0x4e31a800 // sminv  b0, v0.16b
	WORD $0x4e30a841 // smaxv  b1, v2.16b
	WORD $0xeb08013f // cmp    x9, x8
	WORD $0x1e26000a // fmov   w10, s0
	WORD $0x1e26002b // fmov   w11, s1
	BEQ int8_done
int8_scalar:
	WORD $0x8b09000c // add    x12, x0, x9
	WORD $0xcb090108 // sub    x8, x8, x9
int8_scalar_loop:
	WORD $0x38c01589 // ldrsb  w9, [x12], #1
	WORD $0x13001d4a // sxtb   w10, w10
	WORD $0x6b09015f // cmp    w10, w9
	WORD $0x1a89b14a // csel   w10, w10, w9, lt
	WORD $0x13001d6b // sxtb   w11, w11
	WORD $0x6b09017f // cmp    w11, w9
	WORD $0x1a89c16b // csel   w11, w11, w9, gt
	WORD $0xf1000508 // subs   x8, x8, #1
	BNE int8_scalar_loop
int8_done:
	WORD $0x3900006b // strb   w11, [x3]
	WORD $0x3900004a // strb   w10, [x2]
	RET

// func _uint8_max_min_neon(values unsafe.Pointer, length int, minout, maxout unsafe.Pointer)
TEXT ·_uint8_max_min_neon(SB), $0-32

	MOVD    values+0(FP), R0
	MOVD    length+8(FP), R1
	MOVD    minout+16(FP), R2
	MOVD    maxout+24(FP), R3

	WORD $0x7100043f // cmp    w1, #1
	BLT uint8_early_exit

	WORD $0x71007c3f // cmp    w1, #31
	WORD $0x2a0103e8 // mov    w8, w1
	BHI uint8_neon

	WORD $0xaa1f03e9 // mov    x9, xzr
	WORD $0x5280000b // mov    w11, #0
	WORD $0x52801fea // mov    w10, #255
	JMP uint8_scalar
uint8_early_exit:
	WORD $0x5280000b // mov    w11, #0
	WORD $0x52801fea // mov    w10, #255
	WORD $0x3900006b // strb   w11, [x3]
	WORD $0x3900004a // strb   w10, [x2]
	RET
uint8_neon:
	WORD $0x927be909 // and    x9, x8, #0xffffffffffffffe0
	WORD $0x6f07e7e0 // mvni   v0.16b, #0
	WORD $0x6f07e7e1 // mvni   v1.16b, #0
	WORD $0x6f00e402 // movi   v2.16b, #0
	WORD $0x6f00e403 // movi   v3.16b, #0
	WORD $0x9100400a // add    x10, x0, #16
	WORD $0xaa0903eb // mov    x11, x9
uint8_loop:
	WORD $0xad7f9544 // ldp    q4, q5, [x10, #-16]
	WORD $0x6e246c00 // umin   v0.16b, v0.16b, v4.16b
	WORD $0x6e256c21 // umin   v1.16b, v1.16b, v5.16b
	WORD $0x6e246442 // umax   v2.16b, v2.16b, v4.16b
	WORD $0x6e256463 // umax   v3.16b, v3.16b, v5.16b
	WORD $0xf100816b // subs   x11, x11, #32
	WORD $0x9100814a // add    x10, x10, #32
	BNE uint8_loop

	WORD $0x6e216c00 // umin   v0.16b, v0.16b, v1.16b
	WORD $0x6e236442 // umax   v2.16b, v2.16b, v3.16b
	WORD $0x6e31a800 // uminv  b0, v0.16b
	WORD $0x6e30a841 // umaxv  b1, v2.16b
	WORD $0xeb08013f // cmp    x9, x8
	WORD $0x1e26000a // fmov   w10, s0
	WORD $0x1e26002b // fmov   w11, s1
	BEQ uint8_done
uint8_scalar:
	WORD $0x8b09000c // add    x12, x0, x9
	WORD $0xcb090108 // sub    x8, x8, x9
uint8_scalar_loop:
	WORD $0x38401589 // ldrb   w9, [x12], #1
	WORD $0x12001d4a // and    w10, w10, #0xff
	WORD $0x6b09015f // cmp    w10, w9
	WORD $0x1a89314a // csel   w10, w10, w9, lo
	WORD $0x12001d6b // and    w11, w11, #0xff
	WORD $0x6b09017f // cmp    w11, w9
	WORD $0x1a89816b // csel   w11, w11, w9, hi
	WORD $0xf1000508 // subs   x8, x8, #1
	BNE uint8_scalar_loop
uint8_done:
	WORD $0x3900006b // strb   w11, [x3]
	WORD $0x3900004a // strb   w10, [x2]
	RET

// func _int16_max_min_neon(values unsafe.Pointer, length int, minout, maxout unsafe.Pointer)
TEXT ·_int16_max_min_neon(SB), $0-32

	MOVD    values+0(FP), R0
	MOVD    length+8(FP), R1
	MOVD    minout+16(FP), R2
	MOVD    maxout+24(FP), R3

	WORD $0x7100043f // cmp    w1, #1
	BLT int16_early_exit

	WORD $0x71003c3f // cmp    w1, #15
	WORD $0x2a0103e8 // mov    w8, w1
	BHI int16_neon

	WORD $0xaa1f03e9 // mov    x9, xzr
	WORD $0x5290000b // mov    w11, #32768
	WORD $0x528fffea // mov    w10, #32767
	JMP int16_scalar
int16_early_exit:
	WORD $0x528fffea // mov    w10, #32767
	WORD $0x5290000b // mov    w11, #32768
	WORD $0x7900006b // strh   w11, [x3]
	WORD $0x7900004a // strh   w10, [x2]
	RET
int16_neon:
	WORD $0x927ced09 // and    x9, x8, #0xfffffffffffffff0
	WORD $0x6f04a400 // mvni   v0.8h, #32768
	WORD $0x6f04a401 // mvni   v1.8h, #32768
	WORD $0x4f04a402 // movi   v2.8h, #32768
	WORD $0x4f04a403 // movi   v3.8h, #32768
	WORD $0x9100400a // add    x10, x0, #16
	WORD $0xaa0903eb // mov    x11, x9
int16_loop:
	WORD $0xad7f9544 // ldp    q4, q5, [x10, #-16]
	WORD $0x4e646c00 // smin   v0.8h, v0.8h, v4.8h
	WORD $0x4e656c21 // smin   v1.8h, v1.8h, v5.8h
	WORD $0x4e646442 // smax   v2.8h, v2.8h, v4.8h
	WORD $0x4e656463 // smax   v3.8h, v3.8h, v5.8h
	WORD $0xf100416b // subs   x11, x11, #16
	WORD $0x9100814a // add    x10, x10, #32
	BNE int16_loop

	WORD $0x4e616c00 // smin   v0.8h, v0.8h, v1.8h
	WORD $0x4e636442 // smax   v2.8h, v2.8h, v3.8h
	WORD $0x4e71a800 // sminv  h0, v0.8h
	WORD $0x4e70a841 // smaxv  h1, v2.8h
	WORD $0xeb08013f // cmp    x9, x8
	WORD $0x1e26000a // fmov   w10, s0
	WORD $0x1e26002b // fmov   w11, s1
	BEQ int16_done
int16_scalar:
	WORD $0x8b09040c // add    x12, x0, x9, lsl #1
	WORD $0xcb090108 // sub    x8, x8, x9
int16_scalar_loop:
	WORD $0x78c02589 // ldrsh  w9, [x12], #2
	WORD $0x13003d4a // sxth   w10, w10
	WORD $0x6b09015f // cmp    w10, w9
	WORD $0x1a89b14a // csel   w10, w10, w9, lt
	WORD $0x13003d6b // sxth   w11, w11
	WORD $0x6b09017f // cmp    w11, w9
	WORD $0x1a89c16b // csel   w11, w11, w9, gt
	WORD $0xf1000508 // subs   x8, x8, #1
	BNE int16_scalar_loop
int16_done:
	WORD $0x7900006b // strh   w11, [x3]
	WORD $0x7900004a // strh   w10, [x2]
	RET

// func _uint16_max_min_neon(values unsafe.Pointer, length int, minout, maxout unsafe.Pointer)
TEXT ·_uint16_max_min_neon(SB), $0-32

	MOVD    values+0(FP), R0
	MOVD    length+8(FP), R1
	MOVD    minout+16(FP), R2
	MOVD    maxout+24(FP), R3

	WORD $0x7100043f // cmp    w1, #1
	BLT uint16_early_exit

	WORD $0x71003c3f // cmp    w1, #15
	WORD $0x2a0103e8 // mov    w8, w1
	BHI uint16_neon

	WORD $0xaa1f03e9 // mov    x9, xzr
	WORD $0x5280000b // mov    w11, #0
	WORD $0x529fffea // mov    w10, #65535
	JMP uint16_scalar
uint16_early_exit:
	WORD $0x5280000b // mov    w11, #0
	WORD $0x529fffea // mov    w10, #65535
	WORD $0x7900006b // strh   w11, [x3]
	WORD $0x7900004a // strh   w10, [x2]
	RET
uint16_neon:
	WORD $0x927ced09 // and    x9, x8, #0xfffffffffffffff0
	WORD $0x6f07e7e0 // mvni   v0.8h, #0
	WORD $0x6f07e7e1 // mvni   v1.8h, #0
	WORD $0x6f00e402 // movi   v2.8h, #0
	WORD $0x6f00e403 // movi   v3.8h, #0
	WORD $0x9100400a // add    x10, x0, #16
	WORD $0xaa0903eb // mov    x11, x9
uint16_loop:
	WORD $0xad7f9544 // ldp    q4, q5, [x10, #-16]
	WORD $0x6e646c00 // umin   v0.8h, v0.8h, v4.8h
	WORD $0x6e656c21 // umin   v1.8h, v1.8h, v5.8h
	WORD $0x6e646442 // umax   v2.8h, v2.8h, v4.8h
	WORD $0x6e656463 // umax   v3.8h, v3.8h, v5.8h
	WORD $0xf100416b // subs   x11, x11, #16
	WORD $0x9100814a // add    x10, x10, #32
	BNE uint16_loop

	WORD $0x6e616c00 // umin   v0.8h, v0.8h, v1.8h
	WORD $0x6e636442 // umax   v2.8h, v2.8h, v3.8h
	WORD $0x6e71a800 // uminv  h0, v0.8h
	WORD $0x6e70a841 // umaxv  h1, v2.8h
	WORD $0xeb08013f // cmp    x9, x8
	WORD $0x1e26000a // fmov   w10, s0
	WORD $0x1e26002b // fmov   w11, s1
	BEQ uint16_done
uint16_scalar:
	WORD $0x8b09040c // add    x12, x0, x9, lsl #1
	WORD $0xcb090108 // sub    x8, x8, x9
uint16_scalar_loop:
	WORD $0x78402589 // ldrh   w9, [x12], #2
	WORD $0x12003d4a // and    w10, w10, #0xffff
	WORD $0x6b09015f // cmp    w10, w9
	WORD $0x1a89314a // csel   w10, w10, w9, lo
	WORD $0x12003d6b // and    w11, w11, #0xffff
	WORD $0x6b09017f // cmp    w11, w9
	WORD $0x1a89816b // csel   w11, w11, w9, hi
	WORD $0xf1000508 // subs   x8, x8, #1
	BNE uint16_scalar_loop
uint16_done:
	WORD $0x7900006b // strh   w11, [x3]
	WORD $0x7900004a // strh   w10, [x2]
	RET

// func _int32_max_min_neon(values unsafe.Pointer, length int, minout, maxout unsafe.Pointer)
TEXT ·_int32_max_min_neon(SB), $0-32

	MOVD    values+0(FP), R0
	MOVD    length+8(FP), R1
	MOVD    minout+16(FP), R2
	MOVD    maxout+24(FP), R3

	WORD $0x7100043f // cmp    w1, #1
	BLT int32_early_exit

	WORD $0x71001c3f // cmp    w1, #7
	WORD $0x2a0103e8 // mov    w8, w1
	BHI int32_neon

	WORD $0xaa1f03e9 // mov    x9, xzr
	WORD $0x52b0000b // mov    w11, #-2147483648
	WORD $0x12b0000a // mov    w10, #2147483647
	JMP int32_scalar
int32_early_exit:
	WORD $0x12b0000a // mov    w10, #2147483647
	WORD $0x52b0000b // mov    w11, #-2147483648
	WORD $0xb900006b // str    w11, [x3]
	WORD $0xb900004a // str    w10, [x2]
	RET
int32_neon:
	WORD $0x927d7109 // and    x9, x8, #0xfffffff8
	WORD $0x9100400a // add    x10, x0, #16
	WORD $0x4f046402 // movi   v2.4s, #128, lsl #24
	WORD $0x6f046400 // mvni   v0.4s, #128, lsl #24
	WORD $0x6f046401 // mvni   v1.4s, #128, lsl #24
	WORD $0xaa0903eb // mov    x11, x9
	WORD $0x4f046403 // movi   v3.4s, #128, lsl #24
int32_loop:
	WORD $0xad7f9544 // ldp    q4, q5, [x10, #-16]
	WORD $0xf100216b // subs   x11, x11, #8
	WORD $0x9100814a // add    x10, x10, #32
	WORD $0x4ea46c00 // smin   v0.4s, v0.4s, v4.4s
	WORD $0x4ea56c21 // smin   v1.4s, v1.4s, v5.4s
	WORD $0x4ea46442 // smax   v2.4s, v2.4s, v4.4s
	WORD $0x4ea56463 // smax   v3.4s, v3.4s, v5.4s
	BNE int32_loop

	WORD $0x4ea36442 // smax   v2.4s, v2.4s, v3.4s
	WORD $0x4ea16c00 // smin   v0.4s, v0.4s, v1.4s
	WORD $0x4eb0a842 // smaxv  s2, v2.4s
	WORD $0x4eb1a800 // sminv  s0, v0.4s
	WORD $0xeb08013f // cmp    x9, x8
	WORD $0x1e26004b // fmov   w11, s2
	WORD $0x1e26000a // fmov   w10, s0
	BEQ int32_done
int32_scalar:
	WORD $0x8b09080c // add    x12, x0, x9, lsl #2
	WORD $0xcb090108 // sub    x8, x8, x9
int32_scalar_loop:
	WORD $0xb8404589 // ldr    w9, [x12], #4
	WORD $0x6b09015f // cmp    w10, w9
	WORD $0x1a89b14a // csel   w10, w10, w9, lt
	WORD $0x6b09017f // cmp    w11, w9
	WORD $0x1a89c16b // csel   w11, w11, w9, gt
	WORD $0xf1000508 // subs   x8, x8, #1
	BNE int32_scalar_loop
int32_done:
	WORD $0xb900006b // str    w11, [x3]
	WORD $0xb900004a // str    w10, [x2]
	RET

// func _uint32_max_min_neon(values unsafe.Pointer, length int, minout, maxout unsafe.Pointer)
TEXT ·_uint32_max_min_neon(SB), $0-32

	MOVD    values+0(FP), R0
	MOVD    length+8(FP), R1
	MOVD    minout+16(FP), R2
	MOVD    maxout+24(FP), R3

	WORD $0x7100043f // cmp    w1, #1
	BLT uint32_early_exit

	WORD $0x71001c3f // cmp    w1, #7
	WORD $0x2a0103e8 // mov    w8, w1
	BHI uint32_neon

	WORD $0xaa1f03e9 // mov    x9, xzr
	WORD $0x2a1f03ea // mov    w10, wzr
	WORD $0x1280000b // mov    w11, #-1
	JMP uint32_scalar
uint32_early_exit:
	WORD $0x2a1f03ea // mov    w10, wzr
	WORD $0x1280000b // mov    w11, #-1
	WORD $0xb900006a // str    w10, [x3]
	WORD $0xb900004b // str    w11, [x2]
	RET
uint32_neon:
	WORD $0x927d7109 // and    x9, x8, #0xfffffff8
	WORD $0x6f00e401 // movi   v1.2d, #0000000000000000
	WORD $0x6f07e7e0 // movi   v0.2d, #0xffffffffffffffff
	WORD $0x9100400a // add    x10, x0, #16
	WORD $0x6f07e7e2 // movi   v2.2d, #0xffffffffffffffff
	WORD $0xaa0903eb // mov    x11, x9
	WORD $0x6f00e403 // movi   v3.2d, #0000000000000000
uint32_loop:
	WORD $0xad7f9544 // ldp    q4, q5, [x10, #-16]
	WORD $0xf100216b // subs   x11, x11, #8
	WORD $0x9100814a // add    x10, x10, #32
	WORD $0x6ea46c00 // umin   v0.4s, v0.4s, v4.4s
	WORD $0x6ea56c42 // umin   v2.4s, v2.4s, v5.4s
	WORD $0x6ea46421 // umax   v1.4s, v1.4s, v4.4s
	WORD $0x6ea56463 // umax   v3.4s, v3.4s, v5.4s
	BNE uint32_loop

	WORD $0x6ea36421 // umax   v1.4s, v1.4s, v3.4s
	WORD $0x6ea26c00 // umin   v0.4s, v0.4s, v2.4s
	WORD $0x6eb0a821 // umaxv  s1, v1.4s
	WORD $0x6eb1a800 // uminv  s0, v0.4s
	WORD $0xeb08013f // cmp    x9, x8
	WORD $0x1e26002a // fmov   w10, s1
	WORD $0x1e26000b // fmov   w11, s0
	BEQ uint32_done
uint32_scalar:
	WORD $0x8b09080c // add    x12, x0, x9, lsl #2
	WORD $0xcb090108 // sub    x8, x8, x9
uint32_scalar_loop:
	WORD $0xb8404589 // ldr    w9, [x12], #4
	WORD $0x6b09017f // cmp    w11, w9
	WORD $0x1a89316b // csel   w11, w11, w9, lo
	WORD $0x6b09015f // cmp    w10, w9
	WORD $0x1a89814a // csel   w10, w10, w9, hi
	WORD $0xf1000508 // subs   x8, x8, #1
	BNE uint32_scalar_loop
uint32_done:
	WORD $0xb900006a // str    w10, [x3]
	WORD $0xb900004b // str    w11, [x2]
	RET

// func _int64_max_min_neon(values unsafe.Pointer, length int, minout, maxout unsafe.Pointer)
TEXT ·_int64_max_min_neon(SB), $0-32

	MOVD    values+0(FP), R0
	MOVD    length+8(FP), R1
	MOVD    minout+16(FP), R2
	MOVD    maxout+24(FP), R3

	WORD $0x7100043f // cmp    w1, #1
	BLT int64_early_exit

	WORD $0x2a0103e8 // mov    w8, w1
	WORD $0xd2f0000b // mov    x11, #-9223372036854775808
	WORD $0x71000c3f // cmp    w1, #3
	WORD $0x92f0000a // mov    x10, #9223372036854775807
	BHI int64_neon

	WORD $0xaa1f03e9 // mov    x9, xzr
	JMP int64_scalar
int64_early_exit:
	WORD $0x92f0000a // mov    x10, #9223372036854775807
	WORD $0xd2f0000b // mov    x11, #-9223372036854775808
	WORD $0xf900006b // str    x11, [x3]
	WORD $0xf900004a // str    x10, [x2]
	RET
int64_neon:
	WORD $0x927e7509 // and    x9, x8, #0xfffffffc
	WORD $0x4e080d61 // dup    v1.2d, x11
	WORD $0x4e080d40 // dup    v0.2d, x10
	WORD $0x9100400a // add    x10, x0, #16
	WORD $0xaa0903eb // mov    x11, x9
	WORD $0x4ea01c02 // mov    v2.16b, v0.16b
	WORD $0x4ea11c23 // mov    v3.16b, v1.16b
int64_loop:
	WORD $0xad7f9544 // ldp    q4, q5, [x10, #-16]
	WORD $0x4ee03486 // cmgt   v6.2d, v4.2d, v0.2d
	WORD $0x4ee234a7 // cmgt   v7.2d, v5.2d, v2.2d
	WORD $0x4ee13490 // cmgt   v16.2d, v4.2d, v1.2d
	WORD $0x4ee334b1 // cmgt   v17.2d, v5.2d, v3.2d
	WORD $0x6ee61c80 // bif    v0.16b, v4.16b, v6.16b
	WORD $0x6ee71ca2 // bif    v2.16b, v5.16b, v7.16b
	WORD $0x6eb01c81 // bit    v1.16b, v4.16b, v16.16b
	WORD $0x6eb11ca3 // bit    v3.16b, v5.16b, v17.16b
	WORD $0xf100116b // subs   x11, x11, #4
	WORD $0x9100814a // add    x10, x10, #32
	BNE int64_loop

	WORD $0x4ee33424 // cmgt   v4.2d, v1.2d, v3.2d
	WORD $0x4ee03445 // cmgt   v5.2d, v2.2d, v0.2d
	WORD $0x6e631c24 // bsl    v4.16b, v1.16b, v3.16b
	WORD $0x6e621c05 // bsl    v5.16b, v0.16b, v2.16b
	WORD $0x4e180480 // dup    v0.2d, v4.d[1]
	WORD $0x4e1804a1 // dup    v1.2d, v5.d[1]
	WORD $0x4ee03482 // cmgt   v2.2d, v4.2d, v0.2d
	WORD $0x4ee53423 // cmgt   v3.2d, v1.2d, v5.2d
	WORD $0x6e601c82 // bsl    v2.16b, v4.16b, v0.16b
	WORD $0x6e611ca3 // bsl    v3.16b, v5.16b, v1.16b
	WORD $0xeb08013f // cmp    x9, x8
	WORD $0x9e66004b // fmov   x11, d2
	WORD $0x9e66006a // fmov   x10, d3
	BEQ int64_done
int64_scalar:
	WORD $0x8b090c0c // add    x12, x0, x9, lsl #3
	WORD $0xcb090108 // sub    x8, x8, x9
int64_scalar_loop:
	WORD $0xf8408589 // ldr    x9, [x12], #8
	WORD $0xeb09015f // cmp    x10, x9
	WORD $0x9a89b14a // csel   x10, x10, x9, lt
	WORD $0xeb09017f // cmp    x11, x9
	WORD $0x9a89c16b // csel   x11, x11, x9, gt
	WORD $0xf1000508 // subs   x8, x8, #1
	BNE int64_scalar_loop
int64_done:
	WORD $0xf900006b // str    x11, [x3]
	WORD $0xf900004a // str    x10, [x2]
	RET


// func _uint64_max_min_neon(values unsafe.Pointer, length int, minout, maxout unsafe.Pointer)
TEXT ·_uint64_max_min_neon(SB), $0-32

	MOVD    values+0(FP), R0
	MOVD    length+8(FP), R1
	MOVD    minout+16(FP), R2
	MOVD    maxout+24(FP), R3

	WORD $0x7100043f // cmp    w1, #1
	BLT uint64_early_exit

	WORD $0x71000c3f // cmp    w1, #3
	WORD $0x2a0103e8 // mov    w8, w1
	BHI uint64_neon

	WORD $0xaa1f03e9 // mov    x9, xzr
	WORD $0xaa1f03ea // mov    x10, xzr
	WORD $0x9280000b // mov    x11, #-1
	JMP uint64_scalar
uint64_early_exit:
	WORD $0xaa1f03ea // mov    x10, xzr
	WORD $0x9280000b // mov    x11, #-1
	WORD $0xf900006a // str    x10, [x3]
	WORD $0xf900004b // str    x11, [x2]
	RET
uint64_neon:
	WORD $0x927e7509 // and    x9, x8, #0xfffffffc
	WORD $0x9100400a // add    x10, x0, #16
	WORD $0x6f00e401 // movi   v1.2d, #0000000000000000
	WORD $0x6f07e7e0 // movi   v0.2d, #0xffffffffffffffff
	WORD $0x6f07e7e2 // movi   v2.2d, #0xffffffffffffffff
	WORD $0xaa0903eb // mov    x11, x9
	WORD $0x6f00e403 // movi   v3.2d, #0000000000000000
uint64_loop:
	WORD $0xad7f9544 // ldp    q4, q5, [x10, #-16]
	WORD $0x6ee03486 // cmhi   v6.2d, v4.2d, v0.2d
	WORD $0x6ee234a7 // cmhi   v7.2d, v5.2d, v2.2d
	WORD $0x6ee13490 // cmhi   v16.2d, v4.2d, v1.2d
	WORD $0x6ee334b1 // cmhi   v17.2d, v5.2d, v3.2d
	WORD $0x6ee61c80 // bif    v0.16b, v4.16b, v6.16b
	WORD $0x6ee71ca2 // bif    v2.16b, v5.16b, v7.16b
	WORD $0x6eb01c81 // bit    v1.16b, v4.16b, v16.16b
	WORD $0x6eb11ca3 // bit    v3.16b, v5.16b, v17.16b
	WORD $0xf100116b // subs   x11, x11, #4
	WORD $0x9100814a // add    x10, x10, #32
	BNE uint64_loop

	WORD $0x6ee33424 // cmhi   v4.2d, v1.2d, v3.2d
	WORD $0x6ee03445 // cmhi   v5.2d, v2.2d, v0.2d
	WORD $0x6e631c24 // bsl    v4.16b, v1.16b, v3.16b
	WORD $0x6e621c05 // bsl    v5.16b, v0.16b, v2.16b
	WORD $0x4e180480 // dup    v0.2d, v4.d[1]
	WORD $0x4e1804a1 // dup    v1.2d, v5.d[1]
	WORD $0x6ee03482 // cmhi   v2.2d, v4.2d, v0.2d
	WORD $0x6ee53423 // cmhi   v3.2d, v1.2d, v5.2d
	WORD $0x6e601c82 // bsl    v2.16b, v4.16b, v0.16b
	WORD $0x6e611ca3 // bsl    v3.16b, v5.16b, v1.16b
	WORD $0xeb08013f // cmp    x9, x8
	WORD $0x9e66004a // fmov   x10, d2
	WORD $0x9e66006b // fmov   x11, d3
	BEQ uint64_done
uint64_scalar:
	WORD $0x8b090c0c // add    x12, x0, x9, lsl #3
	WORD $0xcb090108 // sub    x8, x8, x9
uint64_scalar_loop:
	WORD $0xf8408589 // ldr    x9, [x12], #8
	WORD $0xeb09017f // cmp    x11, x9
	WORD $0x9a89316b // csel   x11, x11, x9, lo
	WORD $0xeb09015f // cmp    x10, x9
	WORD $0x9a89814a // csel   x10, x10, x9, hi
	WORD $0xf1000508 // subs   x8, x8, #1
	BNE uint64_scalar_loop
uint64_done:
	WORD $0xf900006a // str    x10, [x3]
	WORD $0xf900004b // str    x11, [x2]
	RET
