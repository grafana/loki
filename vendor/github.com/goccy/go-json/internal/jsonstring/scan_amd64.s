#include "textflag.h"

// MASK computes into Yd the mask of the bytes of Yx which may need an escape, using Yt as a temporary:
// the lane of such a byte is not zero. See nibbleTables: Y8 is the table of the low nibbles in both halves,
// Y9 the one of the high nibbles and Y10 has 0x0f in every lane.
//
// Only VEX instructions are used: a legacy SSE instruction, such as a move from a general register to an XMM
// register, mixed with them cost about 500 ns per call.
#define MASK(Yx, Yt, Yd) \
	VPAND   Y10, Yx, Yt \
	VPSRLW  $4, Yx, Yd \
	VPAND   Y10, Yd, Yd \
	VPSHUFB Yt, Y8, Yt \
	VPSHUFB Yd, Y9, Yd \
	VPAND   Yt, Yd, Yd

// func scanStringAVX2(p unsafe.Pointer, n int, tables *nibbleTables) int
//
// It returns 1 if a byte of the n bytes at p may need an escape by the tables, or 0. n is 32 or more.
// The blocks of 128 bytes are looked at first, then the blocks of 32 bytes, and the last block overlaps the
// previous one.
TEXT ·scanStringAVX2(SB), NOSPLIT, $0-32
	MOVQ p+0(FP), SI
	MOVQ n+8(FP), CX
	MOVQ tables+16(FP), AX

	VBROADCASTI128 (AX), Y8
	VBROADCASTI128 16(AX), Y9
	VPBROADCASTB   c0f<>(SB), Y10

	MOVQ SI, DI                  // the address of the block
	LEAQ -128(SI)(CX*1), R11     // the address of the last block of 128 bytes
	CMPQ DI, R11
	JGT  small
loop128:
	VMOVDQU (DI), Y0
	VMOVDQU 32(DI), Y1
	VMOVDQU 64(DI), Y2
	VMOVDQU 96(DI), Y3
	MASK(Y0, Y4, Y5)
	MASK(Y1, Y4, Y6)
	VPOR    Y6, Y5, Y5
	MASK(Y2, Y4, Y6)
	VPOR    Y6, Y5, Y5
	MASK(Y3, Y4, Y6)
	VPOR    Y6, Y5, Y5
	VPTEST  Y5, Y5
	JNE     found
	ADDQ    $128, DI
	CMPQ    DI, R11
	JLE     loop128
small:
	LEAQ -32(SI)(CX*1), R11      // the address of the last block of 32 bytes
	CMPQ DI, R11
	JGT  last
loop32:
	VMOVDQU (DI), Y0
	MASK(Y0, Y4, Y5)
	VPTEST  Y5, Y5
	JNE     found
	ADDQ    $32, DI
	CMPQ    DI, R11
	JLE     loop32
last:
	// the last block, which overlaps the previous one unless the blocks ended exactly at n.
	LEAQ (SI)(CX*1), DX
	CMPQ DI, DX
	JEQ  none
	VMOVDQU (R11), Y0
	MASK(Y0, Y4, Y5)
	VPTEST  Y5, Y5
	JNE     found
none:
	VZEROUPPER
	MOVQ $0, ret+24(FP)
	RET
found:
	VZEROUPPER
	MOVQ $1, ret+24(FP)
	RET

// func escapeStringAVX2(dst, src unsafe.Pointer, n int, tables *nibbleTables, seqs *[256]uint64) (consumed, written int)
//
// It appends the n bytes at src to dst, escaped, 32 bytes at a time: a block is stored to dst as it is, and the
// bytes of it which may need an escape by the tables are found by one mask. For each of them in order, dst is
// advanced to it, its escape, the sequence of seqs of the byte ( see escapeSequences ), is stored, and the rest of
// the block is stored again after it from src, 32 bytes, which is why that is done only while 32 bytes of src
// remain from there. It stops at a byte whose sequence is 0, which the caller escapes, or when fewer than 32 bytes
// remain, and returns the numbers of the bytes it read and wrote. dst has room for 6*n+32 bytes: a byte is
// escaped in 6 bytes at most, and 32 bytes are stored at a time.
TEXT ·escapeStringAVX2(SB), NOSPLIT, $0-56
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ n+16(FP), CX
	MOVQ tables+24(FP), AX
	MOVQ seqs+32(FP), R9

	VBROADCASTI128 (AX), Y8
	VBROADCASTI128 16(AX), Y9
	VPBROADCASTB   c0f<>(SB), Y10
	VPXOR          Y11, Y11, Y11

	MOVQ SI, R12                 // the start of src
	MOVQ DI, R13                 // the start of dst
	LEAQ (SI)(CX*1), R11         // the end of src
eloop:
	LEAQ    32(SI), R10          // the end of the block
	CMPQ    R10, R11
	JGT     edone
	VMOVDQU (SI), Y0
	VMOVDQU Y0, (DI)
	MASK(Y0, Y4, Y5)
	// the lanes which are zero are the ones of the bytes which need no escape.
	VPCMPEQB  Y11, Y5, Y5
	VPMOVMSKB Y5, DX
	NOTL      DX
	TESTL     DX, DX
	JNE       eescape
	MOVQ      R10, SI
	ADDQ      $32, DI
	JMP       eloop
eescape:
	MOVQ SI, R14                 // the address of the bit 0 of the mask
ebyte:
	// dst is advanced over the bytes before the one to escape, which are stored already.
	BSFL    DX, CX
	LEAQ    (R14)(CX*1), AX
	MOVQ    AX, BX
	SUBQ    SI, BX
	ADDQ    BX, DI
	MOVQ    AX, SI
	MOVBLZX (SI), AX
	MOVQ    (R9)(AX*8), BX
	TESTQ   BX, BX
	JEQ     edone
	// the sequence, whose length is in the top byte: the bytes after it are written over.
	MOVQ    BX, (DI)
	SHRQ    $56, BX
	ADDQ    BX, DI
	INCQ    SI
	// the rest of the block is stored again after the escape, from src, if 32 bytes of src remain.
	LEAQ    32(SI), AX
	CMPQ    AX, R11
	JGT     edone
	VMOVDQU (SI), Y1
	VMOVDQU Y1, (DI)
	LEAL    -1(DX), AX
	ANDL    AX, DX
	JNE     ebyte
	// the rest of the block needs no escape.
	MOVQ    R10, BX
	SUBQ    SI, BX
	ADDQ    BX, DI
	MOVQ    R10, SI
	JMP     eloop
edone:
	VZEROUPPER
	SUBQ R12, SI
	MOVQ SI, consumed+40(FP)
	SUBQ R13, DI
	MOVQ DI, written+48(FP)
	RET

// func escapeUTF8AVX2(dst, src unsafe.Pointer, n int, tables *nibbleTables, seqs *[256]uint64, utf8 *utf8Tables) (consumed, written int)
//
// It is escapeStringAVX2 for the options which normalize UTF-8: the tables are the ones of the bytes of ASCII to
// escape, and a block with a byte which is not ASCII is validated as UTF-8 by the tables of utf8 ( see utf8Tables ).
//
// The blocks follow each other by 32 bytes, and the bytes before each byte of a block are taken from the previous
// block ( Y11 ), so that a character which goes on after a block is validated with the next block. A block of
// ASCII is taken as zeros, which are validated as it is: the loop of the blocks after a block of ASCII ( uloop )
// neither keeps the previous block nor looks at R8, which is 0 there, as the loop of escapeStringAVX2 does, and the
// one after a block with a byte which is not ASCII ( unext ) keeps it and has R8 = 1. Then the number of the bytes
// of the character which that block leaves to the next one, 0 to 3, is counted from its last bytes where it is
// needed, which is rarely:
//   - a block with invalid UTF-8, or with U+2028 or U+2029, which are escaped, stops the loop at the start of that
//     character, or else at its first byte which is not ASCII, the start of a character, which the caller escapes;
//   - a block of ASCII after such a character stops the loop at the start of the character, which is invalid;
//   - the loop ends before such a character in its last block.
// So the loop always stops at the start of a character, and the bytes after it which are in dst are written over
// by the caller.
TEXT ·escapeUTF8AVX2(SB), NOSPLIT, $0-64
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ n+16(FP), CX
	MOVQ tables+24(FP), AX
	MOVQ seqs+32(FP), R9
	MOVQ utf8+40(FP), R8

	VBROADCASTI128 (AX), Y8
	VBROADCASTI128 16(AX), Y9
	VPBROADCASTB   c0f<>(SB), Y10
	VBROADCASTI128 (R8), Y12     // the errors by the high nibble of the first byte
	VBROADCASTI128 16(R8), Y13   // by its low nibble
	VBROADCASTI128 32(R8), Y14   // by the high nibble of the second byte
	VPXOR          Y15, Y15, Y15
	XORL           R8, R8

	MOVQ SI, R12                 // the start of src
	MOVQ DI, R13                 // the start of dst
	LEAQ (SI)(CX*1), R11         // the end of src
uloop:
	LEAQ    32(SI), R10          // the end of the block
	CMPQ    R10, R11
	JGT     uend
	VMOVDQU (SI), Y0
	VMOVDQU Y0, (DI)
	MASK(Y0, Y4, Y5)
	VPCMPEQB  Y15, Y5, Y5
	VPMOVMSKB Y5, DX
	NOTL      DX                 // the bytes of ASCII to escape
	VPMOVMSKB Y0, BX             // the bytes which are not ASCII
	TESTL     BX, BX
	JNE       uutf8first
ublock:
	TESTL     DX, DX
	JNE       uescape
	ADDQ      $32, SI
	ADDQ      $32, DI
	JMP       uloop
uutf8first:
	// the previous block, if there is one, is of ASCII, which is validated as zeros are.
	VPXOR     Y11, Y11, Y11
	JMP       uutf8
uascii:
	// a block of ASCII after a block with a byte which is not ASCII: a character which that block leaves to this
	// one is invalid.
	XORL    R8, R8
	MOVBLZX -1(SI), AX
	CMPL    AX, $0xc0
	JAE     uasciileft1
	MOVBLZX -2(SI), AX
	CMPL    AX, $0xe0
	JAE     uasciileft2
	MOVBLZX -3(SI), AX
	CMPL    AX, $0xf0
	JB      uasciiok
	INCQ    R8
uasciileft2:
	INCQ    R8
uasciileft1:
	INCQ    R8
	JMP     uincomplete
uasciiok:
	JMP     ublock
uutf8:
	// the bytes one, two and three before every byte, with the previous block before the block.
	VPERM2I128 $0x03, Y11, Y0, Y1
	VPALIGNR   $15, Y1, Y0, Y2
	VPALIGNR   $14, Y1, Y0, Y3
	VPALIGNR   $13, Y1, Y0, Y6
	// the errors of every two bytes in a row.
	VPSRLW  $4, Y2, Y7
	VPAND   Y10, Y7, Y7
	VPSHUFB Y7, Y12, Y7
	VPAND   Y10, Y2, Y4
	VPSHUFB Y4, Y13, Y4
	VPAND   Y4, Y7, Y7
	VPSRLW  $4, Y0, Y4
	VPAND   Y10, Y4, Y4
	VPSHUFB Y4, Y14, Y4
	VPAND   Y4, Y7, Y7
	// the third and the fourth bytes of a character must be continuation bytes, which the lookups take as errors
	// of two continuation bytes in a row: the bit of that error is flipped there.
	// The constants are operands in memory, of 32 bytes: a broadcast would take the port of the shuffles, which
	// the lookups above need.
	VPSUBUSB     c60x32<>(SB), Y3, Y4  // 0x80 or more if the byte two before is 111_____
	VPSUBUSB     c70x32<>(SB), Y6, Y6  // 0x80 or more if the byte three before is 1111____
	VPOR         Y6, Y4, Y4
	VPAND        c80x32<>(SB), Y4, Y4
	VPXOR        Y4, Y7, Y7
	// U+2028 and U+2029: E2 80 A8 and E2 80 A9, looked for only if a byte two before a byte is E2, as few are.
	VPCMPEQB     cE2x32<>(SB), Y3, Y4
	VPMOVMSKB    Y4, AX
	TESTL        AX, AX
	JEQ          uvalid
	VPCMPEQB     c80x32<>(SB), Y2, Y6
	VPAND        Y6, Y4, Y4
	VPAND        cFEx32<>(SB), Y0, Y6
	VPCMPEQB     cA8x32<>(SB), Y6, Y6
	VPAND        Y6, Y4, Y4
	VPOR         Y4, Y7, Y7
uvalid:
	VPTEST       Y7, Y7
	JNE          uproblem
	MOVL         $1, R8
	TESTL        DX, DX
	JNE          uescape8
	// the next block, which is likely to have bytes which are not ASCII too: the loop goes on from here, with one
	// jump for a block.
	ADDQ      $32, SI
	ADDQ      $32, DI
unext:
	LEAQ      32(SI), R10
	CMPQ      R10, R11
	JGT       uend
	VMOVDQA   Y0, Y11
	VMOVDQU   (SI), Y0
	VMOVDQU   Y0, (DI)
	MASK(Y0, Y4, Y5)
	VPCMPEQB  Y15, Y5, Y5
	VPMOVMSKB Y5, DX
	NOTL      DX
	VPMOVMSKB Y0, BX
	TESTL     BX, BX
	JNE       uutf8
	JMP       uascii
uproblem:
	// the loop stops at the start of the character which the previous block leaves to this one, if there is one,
	// or else at the first byte which is not ASCII, as at a byte to escape whose sequence is 0.
	TESTQ   R8, R8
	JEQ     ufirst
	XORL    R8, R8
	MOVBLZX -1(SI), AX
	CMPL    AX, $0xc0
	JAE     uproblemleft1
	MOVBLZX -2(SI), AX
	CMPL    AX, $0xe0
	JAE     uproblemleft2
	MOVBLZX -3(SI), AX
	CMPL    AX, $0xf0
	JB      ufirst
	INCQ    R8
uproblemleft2:
	INCQ    R8
uproblemleft1:
	INCQ    R8
	JMP     uincomplete
ufirst:
	BSFL    BX, CX
	MOVL    $1, AX
	SHLL    CX, AX
	ORL     AX, DX
	JMP     ublock
uescape:
	MOVQ SI, R14                 // the address of the bit 0 of the mask
ubyte:
	// dst is advanced over the bytes before the one to escape, which are stored already.
	BSFL    DX, CX
	LEAQ    (R14)(CX*1), AX
	MOVQ    AX, BX
	SUBQ    SI, BX
	ADDQ    BX, DI
	MOVQ    AX, SI
	MOVBLZX (SI), AX
	MOVQ    (R9)(AX*8), BX
	TESTQ   BX, BX
	JEQ     udone
	// the sequence, whose length is in the top byte: the bytes after it are written over.
	MOVQ    BX, (DI)
	SHRQ    $56, BX
	ADDQ    BX, DI
	INCQ    SI
	// the rest of the block is stored again after the escape, from src, if 32 bytes of src remain.
	LEAQ    32(SI), AX
	CMPQ    AX, R11
	JGT     udone
	VMOVDQU (SI), Y1
	VMOVDQU Y1, (DI)
	LEAL    -1(DX), AX
	ANDL    AX, DX
	JNE     ubyte
	// the rest of the block needs no escape.
	MOVQ    R10, BX
	SUBQ    SI, BX
	ADDQ    BX, DI
	MOVQ    R10, SI
	JMP     uloop
uescape8:
	// the escapes of a block of the loop after a block with a byte which is not ASCII, which goes on in that loop.
	MOVQ SI, R14                 // the address of the bit 0 of the mask
ubyte8:
	// dst is advanced over the bytes before the one to escape, which are stored already.
	BSFL    DX, CX
	LEAQ    (R14)(CX*1), AX
	MOVQ    AX, BX
	SUBQ    SI, BX
	ADDQ    BX, DI
	MOVQ    AX, SI
	MOVBLZX (SI), AX
	MOVQ    (R9)(AX*8), BX
	TESTQ   BX, BX
	JEQ     udone
	// the sequence, whose length is in the top byte: the bytes after it are written over.
	MOVQ    BX, (DI)
	SHRQ    $56, BX
	ADDQ    BX, DI
	INCQ    SI
	// the rest of the block is stored again after the escape, from src, if 32 bytes of src remain.
	LEAQ    32(SI), AX
	CMPQ    AX, R11
	JGT     udone
	VMOVDQU (SI), Y1
	VMOVDQU Y1, (DI)
	LEAL    -1(DX), AX
	ANDL    AX, DX
	JNE     ubyte8
	// the rest of the block needs no escape.
	MOVQ    R10, BX
	SUBQ    SI, BX
	ADDQ    BX, DI
	MOVQ    R10, SI
	JMP     unext
uend:
	// the loop ends before the character which the last block leaves to the next one, which the caller validates.
	TESTQ   R8, R8
	JEQ     udone
	XORL    R8, R8
	MOVBLZX -1(SI), AX
	CMPL    AX, $0xc0
	JAE     uendleft1
	MOVBLZX -2(SI), AX
	CMPL    AX, $0xe0
	JAE     uendleft2
	MOVBLZX -3(SI), AX
	CMPL    AX, $0xf0
	JB      udone
	INCQ    R8
uendleft2:
	INCQ    R8
uendleft1:
	INCQ    R8
uincomplete:
	SUBQ R8, SI
	SUBQ R8, DI
udone:
	VZEROUPPER
	SUBQ R12, SI
	MOVQ SI, consumed+48(FP)
	SUBQ R13, DI
	MOVQ DI, written+56(FP)
	RET

DATA c0f<>+0(SB)/1, $0x0f
GLOBL c0f<>(SB), RODATA|NOPTR, $1

// the constants of the check of UTF-8, repeated in the 32 bytes of a register.
DATA c60x32<>+0(SB)/8, $0x6060606060606060
DATA c60x32<>+8(SB)/8, $0x6060606060606060
DATA c60x32<>+16(SB)/8, $0x6060606060606060
DATA c60x32<>+24(SB)/8, $0x6060606060606060
GLOBL c60x32<>(SB), RODATA|NOPTR, $32
DATA c70x32<>+0(SB)/8, $0x7070707070707070
DATA c70x32<>+8(SB)/8, $0x7070707070707070
DATA c70x32<>+16(SB)/8, $0x7070707070707070
DATA c70x32<>+24(SB)/8, $0x7070707070707070
GLOBL c70x32<>(SB), RODATA|NOPTR, $32
DATA c80x32<>+0(SB)/8, $0x8080808080808080
DATA c80x32<>+8(SB)/8, $0x8080808080808080
DATA c80x32<>+16(SB)/8, $0x8080808080808080
DATA c80x32<>+24(SB)/8, $0x8080808080808080
GLOBL c80x32<>(SB), RODATA|NOPTR, $32
DATA cE2x32<>+0(SB)/8, $0xe2e2e2e2e2e2e2e2
DATA cE2x32<>+8(SB)/8, $0xe2e2e2e2e2e2e2e2
DATA cE2x32<>+16(SB)/8, $0xe2e2e2e2e2e2e2e2
DATA cE2x32<>+24(SB)/8, $0xe2e2e2e2e2e2e2e2
GLOBL cE2x32<>(SB), RODATA|NOPTR, $32
DATA cFEx32<>+0(SB)/8, $0xfefefefefefefefe
DATA cFEx32<>+8(SB)/8, $0xfefefefefefefefe
DATA cFEx32<>+16(SB)/8, $0xfefefefefefefefe
DATA cFEx32<>+24(SB)/8, $0xfefefefefefefefe
GLOBL cFEx32<>(SB), RODATA|NOPTR, $32
DATA cA8x32<>+0(SB)/8, $0xa8a8a8a8a8a8a8a8
DATA cA8x32<>+8(SB)/8, $0xa8a8a8a8a8a8a8a8
DATA cA8x32<>+16(SB)/8, $0xa8a8a8a8a8a8a8a8
DATA cA8x32<>+24(SB)/8, $0xa8a8a8a8a8a8a8a8
GLOBL cA8x32<>(SB), RODATA|NOPTR, $32
