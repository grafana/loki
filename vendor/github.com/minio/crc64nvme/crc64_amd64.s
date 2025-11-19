// Copyright (c) 2025 Minio Inc. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

//go:build !noasm && !appengine && !gccgo

#include "textflag.h"

TEXT ·updateAsm(SB), $0-40
	MOVQ crc+0(FP), AX    // checksum
	MOVQ p_base+8(FP), SI // start pointer
	MOVQ p_len+16(FP), CX // length of buffer
	NOTQ AX
	SHRQ $7, CX
	CMPQ CX, $1
	JLT  skip128

	VMOVDQA 0x00(SI), X0
	VMOVDQA 0x10(SI), X1
	VMOVDQA 0x20(SI), X2
	VMOVDQA 0x30(SI), X3
	VMOVDQA 0x40(SI), X4
	VMOVDQA 0x50(SI), X5
	VMOVDQA 0x60(SI), X6
	VMOVDQA 0x70(SI), X7
	MOVQ    AX, X8
	PXOR    X8, X0
	CMPQ    CX, $1
	JE      tail128

	MOVQ   $0xa1ca681e733f9c40, AX
	MOVQ   AX, X8
	MOVQ   $0x5f852fb61e8d92dc, AX
	PINSRQ $0x1, AX, X9

loop128:
	ADDQ      $128, SI
	SUBQ      $1, CX
	VMOVDQA   X0, X10
	PCLMULQDQ $0x00, X8, X10
	PCLMULQDQ $0x11, X9, X0
	PXOR      X10, X0
	PXOR      0(SI), X0
	VMOVDQA   X1, X10
	PCLMULQDQ $0x00, X8, X10
	PCLMULQDQ $0x11, X9, X1
	PXOR      X10, X1
	PXOR      0x10(SI), X1
	VMOVDQA   X2, X10
	PCLMULQDQ $0x00, X8, X10
	PCLMULQDQ $0x11, X9, X2
	PXOR      X10, X2
	PXOR      0x20(SI), X2
	VMOVDQA   X3, X10
	PCLMULQDQ $0x00, X8, X10
	PCLMULQDQ $0x11, X9, X3
	PXOR      X10, X3
	PXOR      0x30(SI), X3
	VMOVDQA   X4, X10
	PCLMULQDQ $0x00, X8, X10
	PCLMULQDQ $0x11, X9, X4
	PXOR      X10, X4
	PXOR      0x40(SI), X4
	VMOVDQA   X5, X10
	PCLMULQDQ $0x00, X8, X10
	PCLMULQDQ $0x11, X9, X5
	PXOR      X10, X5
	PXOR      0x50(SI), X5
	VMOVDQA   X6, X10
	PCLMULQDQ $0x00, X8, X10
	PCLMULQDQ $0x11, X9, X6
	PXOR      X10, X6
	PXOR      0x60(SI), X6
	VMOVDQA   X7, X10
	PCLMULQDQ $0x00, X8, X10
	PCLMULQDQ $0x11, X9, X7
	PXOR      X10, X7
	PXOR      0x70(SI), X7
	CMPQ      CX, $1
	JGT       loop128

tail128:
	MOVQ      $0xd083dd594d96319d, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X0, X11
	MOVQ      $0x946588403d4adcbc, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X0
	PXOR      X11, X7
	PXOR      X0, X7
	MOVQ      $0x3c255f5ebc414423, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X1, X11
	MOVQ      $0x34f5a24e22d66e90, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X1
	PXOR      X11, X1
	PXOR      X7, X1
	MOVQ      $0x7b0ab10dd0f809fe, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X2, X11
	MOVQ      $0x03363823e6e791e5, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X2
	PXOR      X11, X2
	PXOR      X1, X2
	MOVQ      $0x0c32cdb31e18a84a, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X3, X11
	MOVQ      $0x62242240ace5045a, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X3
	PXOR      X11, X3
	PXOR      X2, X3
	MOVQ      $0xbdd7ac0ee1a4a0f0, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X4, X11
	MOVQ      $0xa3ffdc1fe8e82a8b, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X4
	PXOR      X11, X4
	PXOR      X3, X4
	MOVQ      $0xb0bc2e589204f500, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X5, X11
	MOVQ      $0xe1e0bb9d45d7a44c, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X5
	PXOR      X11, X5
	PXOR      X4, X5
	MOVQ      $0xeadc41fd2ba3d420, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X6, X11
	MOVQ      $0x21e9761e252621ac, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X6
	PXOR      X11, X6
	PXOR      X5, X6
	MOVQ      AX, X5
	PCLMULQDQ $0x00, X6, X5
	PSHUFD    $0xee, X6, X6
	PXOR      X5, X6
	MOVQ      $0x27ecfa329aef9f77, AX
	MOVQ      AX, X4
	PCLMULQDQ $0x00, X4, X6
	PEXTRQ    $0, X6, BX
	MOVQ      $0x34d926535897936b, AX
	MOVQ      AX, X4
	PCLMULQDQ $0x00, X4, X6
	PXOR      X5, X6
	PEXTRQ    $1, X6, AX
	XORQ      BX, AX

skip128:
	NOTQ AX
	MOVQ AX, checksum+32(FP)
	RET

// Constants, pre-splatted.
DATA ·asmConstantsPoly<>+0x00(SB)/8, $0xa1ca681e733f9c40
DATA ·asmConstantsPoly<>+0x08(SB)/8, $0
DATA ·asmConstantsPoly<>+0x10(SB)/8, $0xa1ca681e733f9c40
DATA ·asmConstantsPoly<>+0x18(SB)/8, $0
DATA ·asmConstantsPoly<>+0x20(SB)/8, $0xa1ca681e733f9c40
DATA ·asmConstantsPoly<>+0x28(SB)/8, $0
DATA ·asmConstantsPoly<>+0x30(SB)/8, $0xa1ca681e733f9c40
DATA ·asmConstantsPoly<>+0x38(SB)/8, $0
// Upper
DATA ·asmConstantsPoly<>+0x40(SB)/8, $0
DATA ·asmConstantsPoly<>+0x48(SB)/8, $0x5f852fb61e8d92dc
DATA ·asmConstantsPoly<>+0x50(SB)/8, $0
DATA ·asmConstantsPoly<>+0x58(SB)/8, $0x5f852fb61e8d92dc
DATA ·asmConstantsPoly<>+0x60(SB)/8, $0
DATA ·asmConstantsPoly<>+0x68(SB)/8, $0x5f852fb61e8d92dc
DATA ·asmConstantsPoly<>+0x70(SB)/8, $0
DATA ·asmConstantsPoly<>+0x78(SB)/8, $0x5f852fb61e8d92dc
GLOBL ·asmConstantsPoly<>(SB), (NOPTR+RODATA), $128

TEXT ·updateAsm512(SB), $0-40
	MOVQ   crc+0(FP), AX    // checksum
	MOVQ   p_base+8(FP), SI // start pointer
	MOVQ   p_len+16(FP), CX // length of buffer
	NOTQ   AX
	SHRQ   $7, CX
	CMPQ   CX, $1
	VPXORQ Z8, Z8, Z8       // Initialize ZMM8 to zero
	JLT    skip128

	VMOVDQU64 0x00(SI), Z0
	VMOVDQU64 0x40(SI), Z4
	MOVQ      $·asmConstantsPoly<>(SB), BX
	VMOVQ     AX, X8

	// XOR initialization value into lower 64 bits of ZMM0
	VPXORQ Z8, Z0, Z0
	CMPQ   CX, $1
	JE     tail128

	VMOVDQU64 0(BX), Z8
	VMOVDQU64 64(BX), Z9

	PCALIGN $16

loop128:
	VMOVDQU64 0x80(SI), Z1
	VMOVDQU64 0xc0(SI), Z5
	ADDQ      $128, SI

	SUBQ       $1, CX
	VPCLMULQDQ $0x00, Z8, Z0, Z10
	VPCLMULQDQ $0x11, Z9, Z0, Z0
	VPTERNLOGD $0x96, Z1, Z10, Z0 // Combine results with xor into Z0

	VPCLMULQDQ $0x00, Z8, Z4, Z10
	VPCLMULQDQ $0x11, Z9, Z4, Z4
	VPTERNLOGD $0x96, Z5, Z10, Z4 // Combine results with xor into Z4

	CMPQ CX, $1
	JGT  loop128

tail128:
	// Extract X0 to X3 from ZMM0
	VEXTRACTF32X4 $1, Z0, X1 // X1: Second 128-bit lane
	VEXTRACTF32X4 $2, Z0, X2 // X2: Third 128-bit lane
	VEXTRACTF32X4 $3, Z0, X3 // X3: Fourth 128-bit lane

	// Extract X4 to X7 from ZMM4
	VEXTRACTF32X4 $1, Z4, X5 // X5: Second 128-bit lane
	VEXTRACTF32X4 $2, Z4, X6 // X6: Third 128-bit lane
	VEXTRACTF32X4 $3, Z4, X7 // X7: Fourth 128-bit lane

	MOVQ      $0xd083dd594d96319d, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X0, X11
	MOVQ      $0x946588403d4adcbc, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X0
	PXOR      X11, X7
	PXOR      X0, X7
	MOVQ      $0x3c255f5ebc414423, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X1, X11
	MOVQ      $0x34f5a24e22d66e90, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X1
	PXOR      X11, X1
	PXOR      X7, X1
	MOVQ      $0x7b0ab10dd0f809fe, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X2, X11
	MOVQ      $0x03363823e6e791e5, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X2
	PXOR      X11, X2
	PXOR      X1, X2
	MOVQ      $0x0c32cdb31e18a84a, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X3, X11
	MOVQ      $0x62242240ace5045a, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X3
	PXOR      X11, X3
	PXOR      X2, X3
	MOVQ      $0xbdd7ac0ee1a4a0f0, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X4, X11
	MOVQ      $0xa3ffdc1fe8e82a8b, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X4
	PXOR      X11, X4
	PXOR      X3, X4
	MOVQ      $0xb0bc2e589204f500, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X5, X11
	MOVQ      $0xe1e0bb9d45d7a44c, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X5
	PXOR      X11, X5
	PXOR      X4, X5
	MOVQ      $0xeadc41fd2ba3d420, AX
	MOVQ      AX, X11
	PCLMULQDQ $0x00, X6, X11
	MOVQ      $0x21e9761e252621ac, AX
	PINSRQ    $0x1, AX, X12
	PCLMULQDQ $0x11, X12, X6
	PXOR      X11, X6
	PXOR      X5, X6
	MOVQ      AX, X5
	PCLMULQDQ $0x00, X6, X5
	PSHUFD    $0xee, X6, X6
	PXOR      X5, X6
	MOVQ      $0x27ecfa329aef9f77, AX
	MOVQ      AX, X4
	PCLMULQDQ $0x00, X4, X6
	PEXTRQ    $0, X6, BX
	MOVQ      $0x34d926535897936b, AX
	MOVQ      AX, X4
	PCLMULQDQ $0x00, X4, X6
	PXOR      X5, X6
	PEXTRQ    $1, X6, AX
	XORQ      BX, AX

skip128:
	NOTQ AX
	MOVQ AX, checksum+32(FP)
	VZEROUPPER
	RET
