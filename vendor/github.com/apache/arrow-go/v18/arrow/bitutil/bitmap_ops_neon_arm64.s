//go:build !noasm && !appengine
// +build !noasm,!appengine

// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

TEXT ·_bitmap_aligned_and_neon(SB), $0-32
	MOVD left+0(FP), R0
	MOVD right+8(FP), R1
	MOVD out+16(FP), R2
	MOVD length+24(FP), R3

and_loop:
	CMP    $64, R3
	BLO    and_tail
	VLD1.P 64(R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	VLD1.P 64(R1), [V4.B16, V5.B16, V6.B16, V7.B16]
	VAND   V0.B16, V4.B16, V0.B16
	VAND   V1.B16, V5.B16, V1.B16
	VAND   V2.B16, V6.B16, V2.B16
	VAND   V3.B16, V7.B16, V3.B16
	VST1.P [V0.B16, V1.B16, V2.B16, V3.B16], 64(R2)
	SUB    $64, R3
	B      and_loop

and_tail:
	CMP  $8, R3
	BLO  and_byte_tail
	MOVD (R0), R4
	MOVD (R1), R5
	AND  R5, R4, R4
	MOVD R4, (R2)
	ADD  $8, R0
	ADD  $8, R1
	ADD  $8, R2
	SUB  $8, R3
	B    and_tail

and_byte_tail:
	CBZ R3, and_done

and_tail_loop:
	MOVBU (R0), R4
	MOVBU (R1), R5
	AND   R5, R4, R4
	MOVB  R4, (R2)
	ADD   $1, R0
	ADD   $1, R1
	ADD   $1, R2
	SUBS  $1, R3
	BNE   and_tail_loop

and_done:
	RET

TEXT ·_bitmap_aligned_or_neon(SB), $0-32
	MOVD left+0(FP), R0
	MOVD right+8(FP), R1
	MOVD out+16(FP), R2
	MOVD length+24(FP), R3

or_loop:
	CMP    $64, R3
	BLO    or_tail
	VLD1.P 64(R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	VLD1.P 64(R1), [V4.B16, V5.B16, V6.B16, V7.B16]
	VORR   V0.B16, V4.B16, V0.B16
	VORR   V1.B16, V5.B16, V1.B16
	VORR   V2.B16, V6.B16, V2.B16
	VORR   V3.B16, V7.B16, V3.B16
	VST1.P [V0.B16, V1.B16, V2.B16, V3.B16], 64(R2)
	SUB    $64, R3
	B      or_loop

or_tail:
	CMP  $8, R3
	BLO  or_byte_tail
	MOVD (R0), R4
	MOVD (R1), R5
	ORR  R5, R4, R4
	MOVD R4, (R2)
	ADD  $8, R0
	ADD  $8, R1
	ADD  $8, R2
	SUB  $8, R3
	B    or_tail

or_byte_tail:
	CBZ R3, or_done

or_tail_loop:
	MOVBU (R0), R4
	MOVBU (R1), R5
	ORR   R5, R4, R4
	MOVB  R4, (R2)
	ADD   $1, R0
	ADD   $1, R1
	ADD   $1, R2
	SUBS  $1, R3
	BNE   or_tail_loop

or_done:
	RET

TEXT ·_bitmap_aligned_and_not_neon(SB), $0-32
	MOVD left+0(FP), R0
	MOVD right+8(FP), R1
	MOVD out+16(FP), R2
	MOVD length+24(FP), R3
	VEOR V31.B16, V31.B16, V31.B16

and_not_loop:
	CMP    $64, R3
	BLO    and_not_tail
	VLD1.P 64(R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	VLD1.P 64(R1), [V4.B16, V5.B16, V6.B16, V7.B16]
	VBSL   V0.B16, V31.B16, V4.B16
	VBSL   V1.B16, V31.B16, V5.B16
	VBSL   V2.B16, V31.B16, V6.B16
	VBSL   V3.B16, V31.B16, V7.B16
	VST1.P [V4.B16, V5.B16, V6.B16, V7.B16], 64(R2)
	SUB    $64, R3
	B      and_not_loop

and_not_tail:
	CMP  $8, R3
	BLO  and_not_byte_tail
	MOVD (R0), R4
	MOVD (R1), R5
	BIC  R5, R4, R4
	MOVD R4, (R2)
	ADD  $8, R0
	ADD  $8, R1
	ADD  $8, R2
	SUB  $8, R3
	B    and_not_tail

and_not_byte_tail:
	CBZ R3, and_not_done

and_not_tail_loop:
	MOVBU (R0), R4
	MOVBU (R1), R5
	BIC   R5, R4, R4
	MOVB  R4, (R2)
	ADD   $1, R0
	ADD   $1, R1
	ADD   $1, R2
	SUBS  $1, R3
	BNE   and_not_tail_loop

and_not_done:
	RET

TEXT ·_bitmap_aligned_xor_neon(SB), $0-32
	MOVD left+0(FP), R0
	MOVD right+8(FP), R1
	MOVD out+16(FP), R2
	MOVD length+24(FP), R3

xor_loop:
	CMP    $64, R3
	BLO    xor_tail
	VLD1.P 64(R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	VLD1.P 64(R1), [V4.B16, V5.B16, V6.B16, V7.B16]
	VEOR   V0.B16, V4.B16, V0.B16
	VEOR   V1.B16, V5.B16, V1.B16
	VEOR   V2.B16, V6.B16, V2.B16
	VEOR   V3.B16, V7.B16, V3.B16
	VST1.P [V0.B16, V1.B16, V2.B16, V3.B16], 64(R2)
	SUB    $64, R3
	B      xor_loop

xor_tail:
	CMP  $8, R3
	BLO  xor_byte_tail
	MOVD (R0), R4
	MOVD (R1), R5
	EOR  R5, R4, R4
	MOVD R4, (R2)
	ADD  $8, R0
	ADD  $8, R1
	ADD  $8, R2
	SUB  $8, R3
	B    xor_tail

xor_byte_tail:
	CBZ R3, xor_done

xor_tail_loop:
	MOVBU (R0), R4
	MOVBU (R1), R5
	EOR   R5, R4, R4
	MOVB  R4, (R2)
	ADD   $1, R0
	ADD   $1, R1
	ADD   $1, R2
	SUBS  $1, R3
	BNE   xor_tail_loop

xor_done:
	RET

TEXT ·_bitmap_aligned_xnor_neon(SB), $0-32
	MOVD  left+0(FP), R0
	MOVD  right+8(FP), R1
	MOVD  out+16(FP), R2
	MOVD  length+24(FP), R3
	VMOVI $0xff, V31.B16

xnor_loop:
	CMP    $64, R3
	BLO    xnor_tail
	VLD1.P 64(R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	VLD1.P 64(R1), [V4.B16, V5.B16, V6.B16, V7.B16]
	VEOR   V0.B16, V4.B16, V0.B16
	VEOR   V31.B16, V0.B16, V0.B16
	VEOR   V1.B16, V5.B16, V1.B16
	VEOR   V31.B16, V1.B16, V1.B16
	VEOR   V2.B16, V6.B16, V2.B16
	VEOR   V31.B16, V2.B16, V2.B16
	VEOR   V3.B16, V7.B16, V3.B16
	VEOR   V31.B16, V3.B16, V3.B16
	VST1.P [V0.B16, V1.B16, V2.B16, V3.B16], 64(R2)
	SUB    $64, R3
	B      xnor_loop

xnor_tail:
	CMP  $8, R3
	BLO  xnor_byte_tail
	MOVD (R0), R4
	MOVD (R1), R5
	EOR  R5, R4, R4
	MVN  R4, R4
	MOVD R4, (R2)
	ADD  $8, R0
	ADD  $8, R1
	ADD  $8, R2
	SUB  $8, R3
	B    xnor_tail

xnor_byte_tail:
	CBZ R3, xnor_done

xnor_tail_loop:
	MOVBU (R0), R4
	MOVBU (R1), R5
	EOR   R5, R4, R4
	MVN   R4, R4
	MOVB  R4, (R2)
	ADD   $1, R0
	ADD   $1, R1
	ADD   $1, R2
	SUBS  $1, R3
	BNE   xnor_tail_loop

xnor_done:
	RET
