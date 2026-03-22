//go:build !purego

#include "textflag.h"

// -----------------------------------------------------------------------------
// Shuffle masks used to broadcast bytes of bit-packed valued into vector
// registers at positions where they can then be shifted into the right
// locations.
// -----------------------------------------------------------------------------

// Shuffle masks for unpacking values from bit widths 1 to 16.
//
// The masks are grouped in 32 bytes chunks containing 2 masks of 16 bytes, with
// the following layout:
//
// - The first mask is used to shuffle values from the 16 bytes of input into
//   the lower 16 bytes of output. These values are then shifted RIGHT to be
//   aligned on the begining of each 32 bit word.
//
// - The second mask selects values from the 16 bytes of input into the upper
//   16 bytes of output. These values are then shifted RIGHT to be aligned on
//   the beginning of each 32 bit word.
//
// The bit width is intended to be used as an index into this array, using this
// formula to convert from the index to a byte offset:
//
//      offset = 32 * (bitWidth - 1)
//
GLOBL ·shuffleInt32x1to16bits(SB), RODATA|NOPTR, $512

// 1 bit => 32 bits
// -----------------
// 0: [a,b,c,d,e,f,g,h]
// ...
DATA ·shuffleInt32x1to16bits+0+0(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+0+4(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+0+8(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+0+12(SB)/4, $0x80808000

DATA ·shuffleInt32x1to16bits+0+16(SB)/4, $0x80808000
DATA ·shuffleInt32x1to16bits+0+20(SB)/4, $0x80808000
DATA ·shuffleInt32x1to16bits+0+24(SB)/4, $0x80808000
DATA ·shuffleInt32x1to16bits+0+28(SB)/4, $0x80808000

// 2 bits => 32 bits
// -----------------
// 0: [a,a,b,b,c,c,d,d]
// 1: [e,e,f,f,g,g,h,h]
// ...
DATA ·shuffleInt32x1to16bits+32+0(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+32+4(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+32+8(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+32+12(SB)/4, $0x80808000

DATA ·shuffleInt32x1to16bits+32+16(SB)/4, $0x80808001
DATA ·shuffleInt32x1to16bits+32+20(SB)/4, $0x80808001
DATA ·shuffleInt32x1to16bits+32+24(SB)/4, $0x80808001
DATA ·shuffleInt32x1to16bits+32+28(SB)/4, $0x80808001

// 3 bits => 32 bits
// -----------------
// 0: [a,a,a,b,b,b,c,c]
// 1: [c,d,d,d,e,e,e,f]
// 2: [f,f,g,g,g,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+64+0(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+64+4(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+64+8(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+64+12(SB)/4, $0x80808001

DATA ·shuffleInt32x1to16bits+64+16(SB)/4, $0x80808001
DATA ·shuffleInt32x1to16bits+64+20(SB)/4, $0x80800201
DATA ·shuffleInt32x1to16bits+64+24(SB)/4, $0x80808002
DATA ·shuffleInt32x1to16bits+64+28(SB)/4, $0x80808002

// 4 bits => 32 bits
// -----------------
// 0: [a,a,a,a,b,b,b,b]
// 1: [c,c,c,c,d,d,d,d]
// 2: [e,e,e,e,f,f,f,f]
// 3: [g,g,g,g,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+96+0(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+96+4(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+96+8(SB)/4,  $0x80808001
DATA ·shuffleInt32x1to16bits+96+12(SB)/4, $0x80808001

DATA ·shuffleInt32x1to16bits+96+16(SB)/4, $0x80808002
DATA ·shuffleInt32x1to16bits+96+20(SB)/4, $0x80808002
DATA ·shuffleInt32x1to16bits+96+24(SB)/4, $0x80808003
DATA ·shuffleInt32x1to16bits+96+28(SB)/4, $0x80808003

// 5 bits => 32 bits
// -----------------
// 0: [a,a,a,a,a,b,b,b]
// 1: [b,b,c,c,c,c,c,d]
// 2: [d,d,d,d,e,e,e,e]
// 3: [e,f,f,f,f,f,g,g]
// 4: [g,g,g,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+128+0(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+128+4(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+128+8(SB)/4,  $0x80808001
DATA ·shuffleInt32x1to16bits+128+12(SB)/4, $0x80800201

DATA ·shuffleInt32x1to16bits+128+16(SB)/4, $0x80800302
DATA ·shuffleInt32x1to16bits+128+20(SB)/4, $0x80808003
DATA ·shuffleInt32x1to16bits+128+24(SB)/4, $0x80800403
DATA ·shuffleInt32x1to16bits+128+28(SB)/4, $0x80808004

// 6 bits => 32 bits
// -----------------
// 0: [a,a,a,a,a,a,b,b]
// 1: [b,b,b,b,c,c,c,c]
// 2: [c,c,d,d,d,d,d,d]
// 3: [e,e,e,e,e,e,f,f]
// 4: [f,f,f,f,g,g,g,g]
// 5: [g,g,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+160+0(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+160+4(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+160+8(SB)/4,  $0x80800201
DATA ·shuffleInt32x1to16bits+160+12(SB)/4, $0x80808002

DATA ·shuffleInt32x1to16bits+160+16(SB)/4, $0x80808003
DATA ·shuffleInt32x1to16bits+160+20(SB)/4, $0x80800403
DATA ·shuffleInt32x1to16bits+160+24(SB)/4, $0x80800504
DATA ·shuffleInt32x1to16bits+160+28(SB)/4, $0x80808005

// 7 bits => 32 bits
// -----------------
// 0: [a,a,a,a,a,a,a,b]
// 1: [b,b,b,b,b,b,c,c]
// 2: [c,c,c,c,c,d,d,d]
// 3: [d,d,d,d,e,e,e,e]
// 4: [e,e,e,f,f,f,f,f]
// 5: [f,f,g,g,g,g,g,g]
// 6: [g,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+192+0(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+192+4(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+192+8(SB)/4,  $0x80800201
DATA ·shuffleInt32x1to16bits+192+12(SB)/4, $0x80800302

DATA ·shuffleInt32x1to16bits+192+16(SB)/4, $0x80800403
DATA ·shuffleInt32x1to16bits+192+20(SB)/4, $0x80800504
DATA ·shuffleInt32x1to16bits+192+24(SB)/4, $0x80800605
DATA ·shuffleInt32x1to16bits+192+28(SB)/4, $0x80808006

// 8 bits => 32 bits
// -----------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [b,b,b,b,b,b,b,b]
// 2: [c,c,c,c,c,c,c,c]
// 3: [d,d,d,d,d,d,d,d]
// 4: [e,e,e,e,e,e,e,e]
// 5: [f,f,f,f,f,f,f,f]
// 6: [g,g,g,g,g,g,g,g]
// 7: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+224+0(SB)/4,  $0x80808000
DATA ·shuffleInt32x1to16bits+224+4(SB)/4,  $0x80808001
DATA ·shuffleInt32x1to16bits+224+8(SB)/4,  $0x80808002
DATA ·shuffleInt32x1to16bits+224+12(SB)/4, $0x80808003

DATA ·shuffleInt32x1to16bits+224+16(SB)/4, $0x80808004
DATA ·shuffleInt32x1to16bits+224+20(SB)/4, $0x80808005
DATA ·shuffleInt32x1to16bits+224+24(SB)/4, $0x80808006
DATA ·shuffleInt32x1to16bits+224+28(SB)/4, $0x80808007

// 9 bits => 32 bits
// -----------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,b,b,b,b,b,b,b]
// 2: [b,b,c,c,c,c,c,c]
// 3: [c,c,c,d,d,d,d,d]
// 4: [d,d,d,d,e,e,e,e]
// 5: [e,e,e,e,e,f,f,f]
// 6: [f,f,f,f,f,f,g,g]
// 7: [g,g,g,g,g,g,g,h]
// 8: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+256+0(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+256+4(SB)/4,  $0x80800201
DATA ·shuffleInt32x1to16bits+256+8(SB)/4,  $0x80800302
DATA ·shuffleInt32x1to16bits+256+12(SB)/4, $0x80800403

DATA ·shuffleInt32x1to16bits+256+16(SB)/4, $0x80800504
DATA ·shuffleInt32x1to16bits+256+20(SB)/4, $0x80800605
DATA ·shuffleInt32x1to16bits+256+24(SB)/4, $0x80800706
DATA ·shuffleInt32x1to16bits+256+28(SB)/4, $0x80800807

// 10 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,b,b,b,b,b,b]
// 2: [b,b,b,b,c,c,c,c]
// 3: [c,c,c,c,c,c,d,d]
// 4: [d,d,d,d,d,d,d,d]
// 5: [e,e,e,e,e,e,e,e]
// 6: [e,e,f,f,f,f,f,f]
// 7: [f,f,f,f,g,g,g,g]
// 8: [g,g,g,g,g,g,h,h]
// 9: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+288+0(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+288+4(SB)/4,  $0x80800201
DATA ·shuffleInt32x1to16bits+288+8(SB)/4,  $0x80800302
DATA ·shuffleInt32x1to16bits+288+12(SB)/4, $0x80800403

DATA ·shuffleInt32x1to16bits+288+16(SB)/4, $0x80800605
DATA ·shuffleInt32x1to16bits+288+20(SB)/4, $0x80800706
DATA ·shuffleInt32x1to16bits+288+24(SB)/4, $0x80800807
DATA ·shuffleInt32x1to16bits+288+28(SB)/4, $0x80800908

// 11 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,b,b,b,b,b]
// 2: [b,b,b,b,b,b,c,c]
// 3: [c,c,c,c,c,c,c,c]
// 4: [c,d,d,d,d,d,d,d]
// 5: [d,d,d,d,e,e,e,e]
// 6: [e,e,e,e,e,e,e,f]
// 7: [f,f,f,f,f,f,f,f]
// 8: [f,f,g,g,g,g,g,g]
// 9: [g,g,g,g,g,h,h,h]
// A: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+320+0(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+320+4(SB)/4,  $0x80800201
DATA ·shuffleInt32x1to16bits+320+8(SB)/4,  $0x80040302
DATA ·shuffleInt32x1to16bits+320+12(SB)/4, $0x80800504

DATA ·shuffleInt32x1to16bits+320+16(SB)/4, $0x80800605
DATA ·shuffleInt32x1to16bits+320+20(SB)/4, $0x80080706
DATA ·shuffleInt32x1to16bits+320+24(SB)/4, $0x80800908
DATA ·shuffleInt32x1to16bits+320+28(SB)/4, $0x80800A09

// 12 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,b,b,b,b]
// 2: [b,b,b,b,b,b,b,b]
// 3: [c,c,c,c,c,c,c,c]
// 4: [c,c,c,c,d,d,d,d]
// 5: [d,d,d,d,d,d,d,d]
// 6: [e,e,e,e,e,e,e,e]
// 7: [e,e,e,e,f,f,f,f]
// 8: [f,f,f,f,f,f,f,f]
// 9: [g,g,g,g,g,g,g,g]
// A: [g,g,g,g,h,h,h,h]
// B: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+352+0(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+352+4(SB)/4,  $0x80800201
DATA ·shuffleInt32x1to16bits+352+8(SB)/4,  $0x80080403
DATA ·shuffleInt32x1to16bits+352+12(SB)/4, $0x80800504

DATA ·shuffleInt32x1to16bits+352+16(SB)/4, $0x80800706
DATA ·shuffleInt32x1to16bits+352+20(SB)/4, $0x80800807
DATA ·shuffleInt32x1to16bits+352+24(SB)/4, $0x80800A09
DATA ·shuffleInt32x1to16bits+352+28(SB)/4, $0x80800B0A

// 13 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,b,b,b]
// 2: [b,b,b,b,b,b,b,b]
// 3: [b,b,c,c,c,c,c,c]
// 4: [c,c,c,c,c,c,c,d]
// 5: [d,d,d,d,d,d,d,d]
// 6: [d,d,d,d,e,e,e,e]
// 7: [e,e,e,e,e,e,e,e]
// 8: [e,f,f,f,f,f,f,f]
// 9: [f,f,f,f,f,f,g,g]
// A: [g,g,g,g,g,g,g,g]
// B: [g,g,g,h,h,h,h,h]
// C: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+384+0(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+384+4(SB)/4,  $0x80030201
DATA ·shuffleInt32x1to16bits+384+8(SB)/4,  $0x80800403
DATA ·shuffleInt32x1to16bits+384+12(SB)/4, $0x80060504

DATA ·shuffleInt32x1to16bits+384+16(SB)/4, $0x80080706
DATA ·shuffleInt32x1to16bits+384+20(SB)/4, $0x80800908
DATA ·shuffleInt32x1to16bits+384+24(SB)/4, $0x800B0A09
DATA ·shuffleInt32x1to16bits+384+28(SB)/4, $0x80800C0B

// 14 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,b,b]
// 2: [b,b,b,b,b,b,b,b]
// 3: [b,b,b,b,c,c,c,c]
// 4: [c,c,c,c,c,c,c,c]
// 5: [c,c,d,d,d,d,d,d]
// 6: [d,d,d,d,d,d,d,d]
// 7: [e,e,e,e,e,e,e,e]
// 8: [e,e,e,e,e,e,f,f]
// 9: [f,f,f,f,f,f,f,f]
// A: [f,f,f,f,g,g,g,g]
// B: [g,g,g,g,g,g,g,g]
// C: [g,g,h,h,h,h,h,h]
// D: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+416+0(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+416+4(SB)/4,  $0x80030201
DATA ·shuffleInt32x1to16bits+416+8(SB)/4,  $0x80050403
DATA ·shuffleInt32x1to16bits+416+12(SB)/4, $0x80800605

DATA ·shuffleInt32x1to16bits+416+16(SB)/4, $0x80080807
DATA ·shuffleInt32x1to16bits+416+20(SB)/4, $0x800A0908
DATA ·shuffleInt32x1to16bits+416+24(SB)/4, $0x800C0B0A
DATA ·shuffleInt32x1to16bits+416+28(SB)/4, $0x80800D0C

// 15 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,b]
// 2: [b,b,b,b,b,b,b,b]
// 3: [b,b,b,b,b,b,c,c]
// 4: [c,c,c,c,c,c,c,c]
// 5: [c,c,c,c,c,d,d,d]
// 6: [d,d,d,d,d,d,d,d]
// 7: [d,d,d,d,e,e,e,e]
// 8: [e,e,e,e,e,e,e,e]
// 9: [e,e,e,f,f,f,f,f]
// A: [f,f,f,f,f,f,f,f]
// B: [f,f,g,g,g,g,g,g]
// C: [g,g,g,g,g,g,g,g]
// D: [g,h,h,h,h,h,h,h]
// E: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+448+0(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+448+4(SB)/4,  $0x80030201
DATA ·shuffleInt32x1to16bits+448+8(SB)/4,  $0x80050403
DATA ·shuffleInt32x1to16bits+448+12(SB)/4, $0x80070605

DATA ·shuffleInt32x1to16bits+448+16(SB)/4, $0x80090807
DATA ·shuffleInt32x1to16bits+448+20(SB)/4, $0x800B0A09
DATA ·shuffleInt32x1to16bits+448+24(SB)/4, $0x800D0C0B
DATA ·shuffleInt32x1to16bits+448+28(SB)/4, $0x80800E0D

// 16 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [b,b,b,b,b,b,b,b]
// 3: [b,b,b,b,b,b,c,b]
// 4: [c,c,c,c,c,c,c,c]
// 5: [c,c,c,c,c,c,c,c]
// 6: [d,d,d,d,d,d,d,d]
// 7: [d,d,d,d,d,d,d,d]
// 8: [e,e,e,e,e,e,e,e]
// 9: [e,e,e,e,e,e,e,e]
// A: [f,f,f,f,f,f,f,f]
// B: [f,f,f,f,f,f,f,f]
// C: [g,g,g,g,g,g,g,g]
// D: [g,g,g,g,g,g,g,g]
// E: [h,h,h,h,h,h,h,h]
// F: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x1to16bits+480+0(SB)/4,  $0x80800100
DATA ·shuffleInt32x1to16bits+480+4(SB)/4,  $0x80800302
DATA ·shuffleInt32x1to16bits+480+8(SB)/4,  $0x80800504
DATA ·shuffleInt32x1to16bits+480+12(SB)/4, $0x80800706

DATA ·shuffleInt32x1to16bits+480+16(SB)/4, $0x80800908
DATA ·shuffleInt32x1to16bits+480+20(SB)/4, $0x80800B0A
DATA ·shuffleInt32x1to16bits+480+24(SB)/4, $0x80800D0C
DATA ·shuffleInt32x1to16bits+480+28(SB)/4, $0x80800F0E

// Shuffle masks for unpacking values from bit widths 17 to 26.
//
// The masks are grouped in 48 bytes chunks containing 3 masks of 16 bytes, with
// the following layout:
//
// - The first mask is used to shuffle values from the first 16 bytes of input
//   into the lower 16 bytes of output. These values are then shifted RIGHT to
//   be aligned on the begining of each 32 bit word.
//
// - The second mask selects values from the first 16 bytes of input into the
//   upper 16 bytes of output. These values are then shifted RIGHT to be aligned
//   on the beginning of each 32 bit word.
//
// - The third mask selects values from the second 16 bytes of input into the
//   upper 16 bytes of output. These values are then shifted RIGHT to be aligned
//   on the beginning of each 32 bit word.
//
// The bit width is intended to be used as an index into this array, using this
// formula to convert from the index to a byte offset:
//
//      offset = 48 * (bitWidth - 17)
//
GLOBL ·shuffleInt32x17to26bits(SB), RODATA|NOPTR, $480

// 17 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,b,b,b,b,b,b,b]
// 3: [b,b,b,b,b,b,b,b]
// 4: [b,b,c,c,c,c,c,c]
// 5: [c,c,c,c,c,c,c,c]
// 6: [c,c,c,d,d,d,d,d]
// 7: [d,d,d,d,d,d,d,d]
// 8: [d,d,d,d,e,e,e,e]
// 9: [e,e,e,e,e,e,e,e]
// A: [e,e,e,e,e,f,f,f]
// B: [f,f,f,f,f,f,f,f]
// C: [f,f,f,f,f,f,g,g]
// D: [g,g,g,g,g,g,g,g]
// E: [g,g,g,g,g,g,g,h]
// F: [h,h,h,h,h,h,h,h]
// ---
// 0: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+0+0(SB)/4,  $0x80020100
DATA ·shuffleInt32x17to26bits+0+4(SB)/4,  $0x80040302
DATA ·shuffleInt32x17to26bits+0+8(SB)/4,  $0x80060504
DATA ·shuffleInt32x17to26bits+0+12(SB)/4, $0x80080706

DATA ·shuffleInt32x17to26bits+0+16(SB)/4, $0x800A0908
DATA ·shuffleInt32x17to26bits+0+20(SB)/4, $0x800C0B0A
DATA ·shuffleInt32x17to26bits+0+24(SB)/4, $0x800E0D0C
DATA ·shuffleInt32x17to26bits+0+28(SB)/4, $0x80800F0E

DATA ·shuffleInt32x17to26bits+0+32(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+0+36(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+0+40(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+0+44(SB)/4, $0x80008080

// 18 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,b,b,b,b,b,b]
// 3: [b,b,b,b,b,b,b,b]
// 4: [b,b,b,b,c,c,c,c]
// 5: [c,c,c,c,c,c,c,c]
// 6: [c,c,c,c,c,c,d,d]
// 7: [d,d,d,d,d,d,d,d]
// 8: [d,d,d,d,d,d,d,d]
// 9: [e,e,e,e,e,e,e,e]
// A: [e,e,e,e,e,e,e,e]
// B: [e,e,f,f,f,f,f,f]
// C: [f,f,f,f,f,f,f,f]
// D: [f,f,f,f,g,g,g,g]
// E: [g,g,g,g,g,g,g,g]
// F: [g,g,g,g,g,g,h,h]
// ---
// 0: [h,h,h,h,h,h,h,h]
// 1: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+48+0(SB)/4,  $0x80020100
DATA ·shuffleInt32x17to26bits+48+4(SB)/4,  $0x80040302
DATA ·shuffleInt32x17to26bits+48+8(SB)/4,  $0x80060504
DATA ·shuffleInt32x17to26bits+48+12(SB)/4, $0x80080706

DATA ·shuffleInt32x17to26bits+48+16(SB)/4, $0x800B0A09
DATA ·shuffleInt32x17to26bits+48+20(SB)/4, $0x800D0C0B
DATA ·shuffleInt32x17to26bits+48+24(SB)/4, $0x800F0E0D
DATA ·shuffleInt32x17to26bits+48+28(SB)/4, $0x8080800F

DATA ·shuffleInt32x17to26bits+48+32(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+48+36(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+48+40(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+48+44(SB)/4, $0x80010080

// 19 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,b,b,b,b,b]
// 3: [b,b,b,b,b,b,b,b]
// 4: [b,b,b,b,b,b,c,c]
// 5: [c,c,c,c,c,c,c,c]
// 6: [c,c,c,c,c,c,c,c]
// 7: [c,d,d,d,d,d,d,d]
// 8: [d,d,d,d,d,d,d,d]
// 9: [d,d,d,d,e,e,e,e]
// A: [e,e,e,e,e,e,e,e]
// B: [e,e,e,e,e,e,e,f]
// C: [f,f,f,f,f,f,f,f]
// D: [f,f,f,f,f,f,f,f]
// E: [f,f,g,g,g,g,g,g]
// F: [g,g,g,g,g,g,g,g]
// ---
// 0: [g,g,g,g,g,h,h,h]
// 1: [h,h,h,h,h,h,h,h]
// 2: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+96+0(SB)/4,  $0x80020100
DATA ·shuffleInt32x17to26bits+96+4(SB)/4,  $0x80040302
DATA ·shuffleInt32x17to26bits+96+8(SB)/4,  $0x07060504
DATA ·shuffleInt32x17to26bits+96+12(SB)/4, $0x80090807

DATA ·shuffleInt32x17to26bits+96+16(SB)/4, $0x800B0A09
DATA ·shuffleInt32x17to26bits+96+20(SB)/4, $0x0E0D0C0B
DATA ·shuffleInt32x17to26bits+96+24(SB)/4, $0x80800F0E
DATA ·shuffleInt32x17to26bits+96+28(SB)/4, $0x80808080

DATA ·shuffleInt32x17to26bits+96+32(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+96+36(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+96+40(SB)/4, $0x80008080
DATA ·shuffleInt32x17to26bits+96+44(SB)/4, $0x80020100

// 20 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,b,b,b,b]
// 3: [b,b,b,b,b,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [c,c,c,c,c,c,c,c]
// 6: [c,c,c,c,c,c,c,c]
// 7: [c,c,c,c,d,d,d,d]
// 8: [d,d,d,d,d,d,d,d]
// 9: [d,d,d,d,d,d,d,d]
// A: [e,e,e,e,e,e,e,e]
// B: [e,e,e,e,e,e,e,e]
// C: [e,e,e,e,f,f,f,f]
// D: [f,f,f,f,f,f,f,f]
// E: [f,f,f,f,f,f,f,f]
// F: [g,g,g,g,g,g,g,g]
// ---
// 0: [g,g,g,g,g,g,g,g]
// 1: [g,g,g,g,h,h,h,h]
// 2: [h,h,h,h,h,h,h,h]
// 3: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+144+0(SB)/4,  $0x80020100
DATA ·shuffleInt32x17to26bits+144+4(SB)/4,  $0x80040302
DATA ·shuffleInt32x17to26bits+144+8(SB)/4,  $0x80070605
DATA ·shuffleInt32x17to26bits+144+12(SB)/4, $0x80090807

DATA ·shuffleInt32x17to26bits+144+16(SB)/4, $0x800C0B0A
DATA ·shuffleInt32x17to26bits+144+20(SB)/4, $0x800E0D0C
DATA ·shuffleInt32x17to26bits+144+24(SB)/4, $0x8080800F
DATA ·shuffleInt32x17to26bits+144+28(SB)/4, $0x80808080

DATA ·shuffleInt32x17to26bits+144+32(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+144+36(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+144+40(SB)/4, $0x80010080
DATA ·shuffleInt32x17to26bits+144+44(SB)/4, $0x80030201

// 21 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,b,b,b]
// 3: [b,b,b,b,b,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,c,c,c,c,c,c]
// 6: [c,c,c,c,c,c,c,c]
// 7: [c,c,c,c,c,c,c,d]
// 8: [d,d,d,d,d,d,d,d]
// 9: [d,d,d,d,d,d,d,d]
// A: [d,d,d,d,e,e,e,e]
// B: [e,e,e,e,e,e,e,e]
// C: [e,e,e,e,e,e,e,e]
// D: [e,f,f,f,f,f,f,f]
// E: [f,f,f,f,f,f,f,f]
// F: [f,f,f,f,f,f,g,g]
// ---
// 0: [g,g,g,g,g,g,g,g]
// 1: [g,g,g,g,g,g,g,g]
// 2: [g,g,g,h,h,h,h,h]
// 3: [h,h,h,h,h,h,h,h]
// 4: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+192+0(SB)/4,  $0x80020100
DATA ·shuffleInt32x17to26bits+192+4(SB)/4,  $0x05040302
DATA ·shuffleInt32x17to26bits+192+8(SB)/4,  $0x80070605
DATA ·shuffleInt32x17to26bits+192+12(SB)/4, $0x0A090807

DATA ·shuffleInt32x17to26bits+192+16(SB)/4, $0x0D0C0B0A
DATA ·shuffleInt32x17to26bits+192+20(SB)/4, $0x800F0E0D
DATA ·shuffleInt32x17to26bits+192+24(SB)/4, $0x8080800F
DATA ·shuffleInt32x17to26bits+192+28(SB)/4, $0x80808080

DATA ·shuffleInt32x17to26bits+192+32(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+192+36(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+192+40(SB)/4, $0x02010080
DATA ·shuffleInt32x17to26bits+192+44(SB)/4, $0x80040302

// 22 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,b,b]
// 3: [b,b,b,b,b,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,c,c,c,c]
// 6: [c,c,c,c,c,c,c,c]
// 7: [c,c,c,c,c,c,c,c]
// 8: [c,c,d,d,d,d,d,d]
// 9: [d,d,d,d,d,d,d,d]
// A: [d,d,d,d,d,d,d,d]
// B: [e,e,e,e,e,e,e,e]
// C: [e,e,e,e,e,e,e,e]
// D: [e,e,e,e,e,e,f,f]
// E: [f,f,f,f,f,f,f,f]
// F: [f,f,f,f,f,f,f,f]
// ---
// 0: [f,f,f,f,g,g,g,g]
// 1: [g,g,g,g,g,g,g,g]
// 2: [g,g,g,g,g,g,g,g]
// 3: [g,g,h,h,h,h,h,h]
// 4: [h,h,h,h,h,h,h,h]
// 5: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+240+0(SB)/4,  $0x80020100
DATA ·shuffleInt32x17to26bits+240+4(SB)/4,  $0x05040302
DATA ·shuffleInt32x17to26bits+240+8(SB)/4,  $0x08070605
DATA ·shuffleInt32x17to26bits+240+12(SB)/4, $0x800A0908

DATA ·shuffleInt32x17to26bits+240+16(SB)/4, $0x800D0C0B
DATA ·shuffleInt32x17to26bits+240+20(SB)/4, $0x800F0E0D
DATA ·shuffleInt32x17to26bits+240+24(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+240+28(SB)/4, $0x80808080

DATA ·shuffleInt32x17to26bits+240+32(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+240+36(SB)/4, $0x00808080
DATA ·shuffleInt32x17to26bits+240+40(SB)/4, $0x03020100
DATA ·shuffleInt32x17to26bits+240+44(SB)/4, $0x80050403

// 23 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,a,b]
// 3: [b,b,b,b,b,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,b,b,c,c]
// 6: [c,c,c,c,c,c,c,c]
// 7: [c,c,c,c,c,c,c,c]
// 8: [c,c,c,c,c,d,d,d]
// 9: [d,d,d,d,d,d,d,d]
// A: [d,d,d,d,d,d,d,d]
// B: [d,d,d,d,e,e,e,e]
// C: [e,e,e,e,e,e,e,e]
// D: [e,e,e,e,e,e,e,e]
// E: [e,e,e,f,f,f,f,f]
// F: [f,f,f,f,f,f,f,f]
// ---
// 0: [f,f,f,f,f,f,f,f]
// 1: [f,f,g,g,g,g,g,g]
// 2: [g,g,g,g,g,g,g,g]
// 3: [g,g,g,g,g,g,g,g]
// 4: [g,h,h,h,h,h,h,h]
// 5: [h,h,h,h,h,h,h,h]
// 6: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+288+0(SB)/4,  $0x80020100
DATA ·shuffleInt32x17to26bits+288+4(SB)/4,  $0x05040302
DATA ·shuffleInt32x17to26bits+288+8(SB)/4,  $0x08070605
DATA ·shuffleInt32x17to26bits+288+12(SB)/4, $0x0B0A0908

DATA ·shuffleInt32x17to26bits+288+16(SB)/4, $0x0E0D0C0B
DATA ·shuffleInt32x17to26bits+288+20(SB)/4, $0x80800F0E
DATA ·shuffleInt32x17to26bits+288+24(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+288+28(SB)/4, $0x80808080

DATA ·shuffleInt32x17to26bits+288+32(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+288+36(SB)/4, $0x01008080
DATA ·shuffleInt32x17to26bits+288+40(SB)/4, $0x04030201
DATA ·shuffleInt32x17to26bits+288+44(SB)/4, $0x80060504

// 24 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,a,a]
// 3: [b,b,b,b,b,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,b,b,b,b]
// 6: [c,c,c,c,c,c,c,c]
// 7: [c,c,c,c,c,c,c,c]
// 8: [c,c,c,c,c,c,c,c]
// 9: [d,d,d,d,d,d,d,d]
// A: [d,d,d,d,d,d,d,d]
// B: [d,d,d,d,d,d,d,d]
// C: [e,e,e,e,e,e,e,e]
// D: [e,e,e,e,e,e,e,e]
// E: [e,e,e,e,e,e,e,e]
// F: [f,f,f,f,f,f,f,f]
// ---
// 0: [f,f,f,f,f,f,f,f]
// 1: [f,f,f,f,f,f,f,f]
// 2: [g,g,g,g,g,g,g,g]
// 3: [g,g,g,g,g,g,g,g]
// 4: [g,g,g,g,g,g,g,g]
// 5: [h,h,h,h,h,h,h,h]
// 6: [h,h,h,h,h,h,h,h]
// 7: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+336+0(SB)/4,  $0x80020100
DATA ·shuffleInt32x17to26bits+336+4(SB)/4,  $0x80050403
DATA ·shuffleInt32x17to26bits+336+8(SB)/4,  $0x80080706
DATA ·shuffleInt32x17to26bits+336+12(SB)/4, $0x800B0A09

DATA ·shuffleInt32x17to26bits+336+16(SB)/4, $0x800E0D0C
DATA ·shuffleInt32x17to26bits+336+20(SB)/4, $0x8080800F
DATA ·shuffleInt32x17to26bits+336+24(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+336+28(SB)/4, $0x80808080

DATA ·shuffleInt32x17to26bits+336+32(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+336+36(SB)/4, $0x80010080
DATA ·shuffleInt32x17to26bits+336+40(SB)/4, $0x80040302
DATA ·shuffleInt32x17to26bits+336+44(SB)/4, $0x80070605

// 25 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,a,a]
// 3: [a,b,b,b,b,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,b,b,b,b]
// 6: [b,b,c,c,c,c,c,c]
// 7: [c,c,c,c,c,c,c,c]
// 8: [c,c,c,c,c,c,c,c]
// 9: [c,c,c,d,d,d,d,d]
// A: [d,d,d,d,d,d,d,d]
// B: [d,d,d,d,d,d,d,d]
// C: [d,d,d,d,e,e,e,e]
// D: [e,e,e,e,e,e,e,e]
// E: [e,e,e,e,e,e,e,e]
// F: [e,e,e,e,e,f,f,f]
// ---
// 0: [f,f,f,f,f,f,f,f]
// 1: [f,f,f,f,f,f,f,f]
// 2: [f,f,f,f,f,f,g,g]
// 3: [g,g,g,g,g,g,g,g]
// 4: [g,g,g,g,g,g,g,g]
// 5: [g,g,g,g,g,g,g,h]
// 6: [h,h,h,h,h,h,h,h]
// 7: [h,h,h,h,h,h,h,h]
// 8: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+384+0(SB)/4,  $0x03020100
DATA ·shuffleInt32x17to26bits+384+4(SB)/4,  $0x06050403
DATA ·shuffleInt32x17to26bits+384+8(SB)/4,  $0x09080706
DATA ·shuffleInt32x17to26bits+384+12(SB)/4, $0x0C0B0A09

DATA ·shuffleInt32x17to26bits+384+16(SB)/4, $0x0F0E0D0C
DATA ·shuffleInt32x17to26bits+384+20(SB)/4, $0x8080800F
DATA ·shuffleInt32x17to26bits+384+24(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+384+28(SB)/4, $0x80808080

DATA ·shuffleInt32x17to26bits+384+32(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+384+36(SB)/4, $0x02010080
DATA ·shuffleInt32x17to26bits+384+40(SB)/4, $0x05040302
DATA ·shuffleInt32x17to26bits+384+44(SB)/4, $0x08070605

// 26 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,a,a]
// 3: [a,a,b,b,b,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,b,b,b,b]
// 6: [b,b,b,b,c,c,c,c]
// 7: [c,c,c,c,c,c,c,c]
// 8: [c,c,c,c,c,c,c,c]
// 9: [c,c,c,c,c,c,d,d]
// A: [d,d,d,d,d,d,d,d]
// B: [d,d,d,d,d,d,d,d]
// C: [d,d,d,d,d,d,d,d]
// D: [e,e,e,e,e,e,e,e]
// E: [e,e,e,e,e,e,e,e]
// F: [e,e,e,e,e,e,e,e]
// ---
// 0: [e,e,f,f,f,f,f,f]
// 1: [f,f,f,f,f,f,f,f]
// 2: [f,f,f,f,f,f,f,f]
// 3: [f,f,f,f,g,g,g,g]
// 4: [g,g,g,g,g,g,g,g]
// 5: [g,g,g,g,g,g,g,g]
// 6: [g,g,g,g,g,g,h,h]
// 7: [h,h,h,h,h,h,h,h]
// 8: [h,h,h,h,h,h,h,h]
// 9: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x17to26bits+432+0(SB)/4,  $0x03020100
DATA ·shuffleInt32x17to26bits+432+4(SB)/4,  $0x06050403
DATA ·shuffleInt32x17to26bits+432+8(SB)/4,  $0x09080706
DATA ·shuffleInt32x17to26bits+432+12(SB)/4, $0x0C0B0A09

DATA ·shuffleInt32x17to26bits+432+16(SB)/4, $0x800F0E0D
DATA ·shuffleInt32x17to26bits+432+20(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+432+24(SB)/4, $0x80808080
DATA ·shuffleInt32x17to26bits+432+28(SB)/4, $0x80808080

DATA ·shuffleInt32x17to26bits+432+32(SB)/4, $0x00808080
DATA ·shuffleInt32x17to26bits+432+36(SB)/4, $0x03020100
DATA ·shuffleInt32x17to26bits+432+40(SB)/4, $0x06050403
DATA ·shuffleInt32x17to26bits+432+44(SB)/4, $0x09080706

// Shuffle masks for unpacking values from bit widths 27 to 31.
//
// The masks are grouped in 80 bytes chunks containing 5 masks of 16 bytes, with
// the following layout:
//
// - The first mask is used to shuffle values from the first 16 bytes of input
//   into the lower 16 bytes of output. These values are then shifted RIGHT to
//   be aligned on the begining of each 32 bit word.
//
// - The second mask is used to shuffle upper bits of bit-packed values of the
//   first 16 bytes of input that spanned across 5 bytes. These extra bits cannot
//   be selected by the first mask (which can select at most 4 bytes per word).
//   The extra bits are then shifted LEFT to be positioned at the end of the
//   words, after the bits extracted by the first mask.
//
// - The third mask selects values from the first 16 bytes of input into the
//   upper 16 bytes of output. These values are then shifted RIGHT to be aligned
//   on the beginning of each 32 bit word.
//
// - The fourth mask selects values from the second 16 bytes of input into the
//   upper 16 bytes of output. These values are then shifted RIGHT to be aligned
//   on the beginning of each 32 bit word.
//
// - The fifth mask is used to shuffle upper bits of bit-packed values values of
//   second 16 bytes of input that spanned across 5 bytes. These values are then
//   shifted LEFT to be aligned on the beginning of each 32 bit word.
//
// The bit width is intended to be used as an index into this array, using this
// formula to convert from the index to a byte offset:
//
//      offset = 80 * (bitWidth - 27)
//
GLOBL ·shuffleInt32x27to31bits(SB), RODATA|NOPTR, $400

// 27 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,a,a]
// 3: [a,a,a,b,b,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,b,b,b,b]
// 6: [b,b,b,b,b,b,c,c]
// 7: [c,c,c,c,c,c,c,c]
// 8: [c,c,c,c,c,c,c,c]
// 9: [c,c,c,c,c,c,c,c]
// A: [c,d,d,d,d,d,d,d]
// B: [d,d,d,d,d,d,d,d]
// C: [d,d,d,d,d,d,d,d]
// D: [d,d,d,d,e,e,e,e]
// E: [e,e,e,e,e,e,e,e]
// F: [e,e,e,e,e,e,e,e]
// ---
// 0: [e,e,e,e,e,e,e,f]
// 1: [f,f,f,f,f,f,f,f]
// 2: [f,f,f,f,f,f,f,f]
// 3: [f,f,f,f,f,f,f,f]
// 4: [f,f,g,g,g,g,g,g]
// 5: [g,g,g,g,g,g,g,g]
// 6: [g,g,g,g,g,g,g,g]
// 7: [g,g,g,g,g,h,h,h]
// 8: [h,h,h,h,h,h,h,h]
// 9: [h,h,h,h,h,h,h,h]
// A: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x27to31bits+0+0(SB)/4,  $0x03020100
DATA ·shuffleInt32x27to31bits+0+4(SB)/4,  $0x06050403
DATA ·shuffleInt32x27to31bits+0+8(SB)/4,  $0x09080706
DATA ·shuffleInt32x27to31bits+0+12(SB)/4, $0x0D0C0B0A

DATA ·shuffleInt32x27to31bits+0+16(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+0+20(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+0+24(SB)/4, $0x0A808080
DATA ·shuffleInt32x27to31bits+0+28(SB)/4, $0x80808080

DATA ·shuffleInt32x27to31bits+0+32(SB)/4, $0x800F0E0D
DATA ·shuffleInt32x27to31bits+0+36(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+0+40(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+0+44(SB)/4, $0x80808080

DATA ·shuffleInt32x27to31bits+0+48(SB)/4, $0x00808080
DATA ·shuffleInt32x27to31bits+0+52(SB)/4, $0x03020100
DATA ·shuffleInt32x27to31bits+0+56(SB)/4, $0x07060504
DATA ·shuffleInt32x27to31bits+0+60(SB)/4, $0x0A090807

DATA ·shuffleInt32x27to31bits+0+64(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+0+68(SB)/4, $0x04808080
DATA ·shuffleInt32x27to31bits+0+72(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+0+76(SB)/4, $0x80808080

// 28 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,a,a]
// 3: [a,a,a,a,b,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,b,b,b,b]
// 6: [b,b,b,b,b,b,b,b]
// 7: [c,c,c,c,c,c,c,c]
// 8: [c,c,c,c,c,c,c,c]
// 9: [c,c,c,c,c,c,c,c]
// A: [c,c,c,c,d,d,d,d]
// B: [d,d,d,d,d,d,d,d]
// C: [d,d,d,d,d,d,d,d]
// D: [d,d,d,d,d,d,d,d]
// E: [e,e,e,e,e,e,e,e]
// F: [e,e,e,e,e,e,e,e]
// ---
// 0: [e,e,e,e,e,e,e,e]
// 1: [e,e,e,e,f,f,f,f]
// 2: [f,f,f,f,f,f,f,f]
// 3: [f,f,f,f,f,f,f,f]
// 4: [f,f,f,f,f,f,f,f]
// 5: [g,g,g,g,g,g,g,g]
// 6: [g,g,g,g,g,g,g,g]
// 7: [g,g,g,g,g,g,g,g]
// 8: [g,g,g,g,h,h,h,h]
// 9: [h,h,h,h,h,h,h,h]
// A: [h,h,h,h,h,h,h,h]
// B: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x27to31bits+80+0(SB)/4,  $0x03020100
DATA ·shuffleInt32x27to31bits+80+4(SB)/4,  $0x06050403
DATA ·shuffleInt32x27to31bits+80+8(SB)/4,  $0x0A090807
DATA ·shuffleInt32x27to31bits+80+12(SB)/4, $0x0D0C0B0A

DATA ·shuffleInt32x27to31bits+80+16(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+80+20(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+80+24(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+80+28(SB)/4, $0x80808080

DATA ·shuffleInt32x27to31bits+80+32(SB)/4, $0x80800F0E
DATA ·shuffleInt32x27to31bits+80+36(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+80+40(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+80+44(SB)/4, $0x80808080

DATA ·shuffleInt32x27to31bits+80+48(SB)/4, $0x01008080
DATA ·shuffleInt32x27to31bits+80+52(SB)/4, $0x04030201
DATA ·shuffleInt32x27to31bits+80+56(SB)/4, $0x08070605
DATA ·shuffleInt32x27to31bits+80+60(SB)/4, $0x0B0A0908

DATA ·shuffleInt32x27to31bits+80+64(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+80+68(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+80+72(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+80+76(SB)/4, $0x80808080

// 29 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,a,a]
// 3: [a,a,a,a,a,b,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,b,b,b,b]
// 6: [b,b,b,b,b,b,b,b]
// 7: [b,b,c,c,c,c,c,c]
// 8: [c,c,c,c,c,c,c,c]
// 9: [c,c,c,c,c,c,c,c]
// A: [c,c,c,c,c,c,c,d]
// B: [d,d,d,d,d,d,d,d]
// C: [d,d,d,d,d,d,d,d]
// D: [d,d,d,d,d,d,d,d]
// E: [d,d,d,d,e,e,e,e]
// F: [e,e,e,e,e,e,e,e]
// ---
// 0: [e,e,e,e,e,e,e,e]
// 1: [e,e,e,e,e,e,e,e]
// 2: [e,f,f,f,f,f,f,f]
// 3: [f,f,f,f,f,f,f,f]
// 4: [f,f,f,f,f,f,f,f]
// 5: [f,f,f,f,f,f,g,g]
// 6: [g,g,g,g,g,g,g,g]
// 7: [g,g,g,g,g,g,g,g]
// 8: [g,g,g,g,g,g,g,g]
// 9: [g,g,g,h,h,h,h,h]
// A: [h,h,h,h,h,h,h,h]
// B: [h,h,h,h,h,h,h,h]
// C: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x27to31bits+160+0(SB)/4,  $0x03020100
DATA ·shuffleInt32x27to31bits+160+4(SB)/4,  $0x06050403
DATA ·shuffleInt32x27to31bits+160+8(SB)/4,  $0x0A090807
DATA ·shuffleInt32x27to31bits+160+12(SB)/4, $0x0D0C0B0A

DATA ·shuffleInt32x27to31bits+160+16(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+160+20(SB)/4, $0x07808080
DATA ·shuffleInt32x27to31bits+160+24(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+160+28(SB)/4, $0x0E808080

DATA ·shuffleInt32x27to31bits+160+32(SB)/4, $0x80800F0E
DATA ·shuffleInt32x27to31bits+160+36(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+160+40(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+160+44(SB)/4, $0x80808080

DATA ·shuffleInt32x27to31bits+160+48(SB)/4, $0x01008080
DATA ·shuffleInt32x27to31bits+160+52(SB)/4, $0x05040302
DATA ·shuffleInt32x27to31bits+160+56(SB)/4, $0x08070605
DATA ·shuffleInt32x27to31bits+160+60(SB)/4, $0x0C0B0A09

DATA ·shuffleInt32x27to31bits+160+64(SB)/4, $0x02808080
DATA ·shuffleInt32x27to31bits+160+68(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+160+72(SB)/4, $0x09808080
DATA ·shuffleInt32x27to31bits+160+76(SB)/4, $0x80808080

// 30 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,a,a]
// 3: [a,a,a,a,a,a,b,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,b,b,b,b]
// 6: [b,b,b,b,b,b,b,b]
// 7: [b,b,b,b,c,c,c,c]
// 8: [c,c,c,c,c,c,c,c]
// 9: [c,c,c,c,c,c,c,c]
// A: [c,c,c,c,c,c,c,c]
// B: [c,c,d,d,d,d,d,d]
// C: [d,d,d,d,d,d,d,d]
// D: [d,d,d,d,d,d,d,d]
// E: [d,d,d,d,d,d,d,d]
// F: [e,e,e,e,e,e,e,e]
// ---
// 0: [e,e,e,e,e,e,e,e]
// 1: [e,e,e,e,e,e,e,e]
// 2: [e,e,e,e,e,e,f,f]
// 3: [f,f,f,f,f,f,f,f]
// 4: [f,f,f,f,f,f,f,f]
// 5: [f,f,f,f,f,f,f,f]
// 6: [f,f,f,f,g,g,g,g]
// 7: [g,g,g,g,g,g,g,g]
// 8: [g,g,g,g,g,g,g,g]
// 9: [g,g,g,g,g,g,g,g]
// A: [g,g,h,h,h,h,h,h]
// B: [h,h,h,h,h,h,h,h]
// C: [h,h,h,h,h,h,h,h]
// D: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x27to31bits+240+0(SB)/4,  $0x03020100
DATA ·shuffleInt32x27to31bits+240+4(SB)/4,  $0x06050403
DATA ·shuffleInt32x27to31bits+240+8(SB)/4,  $0x0A090807
DATA ·shuffleInt32x27to31bits+240+12(SB)/4, $0x0E0D0C0B

DATA ·shuffleInt32x27to31bits+240+16(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+240+20(SB)/4, $0x07808080
DATA ·shuffleInt32x27to31bits+240+24(SB)/4, $0x0B808080
DATA ·shuffleInt32x27to31bits+240+28(SB)/4, $0x80808080

DATA ·shuffleInt32x27to31bits+240+32(SB)/4, $0x8080800F
DATA ·shuffleInt32x27to31bits+240+36(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+240+40(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+240+44(SB)/4, $0x80808080

DATA ·shuffleInt32x27to31bits+240+48(SB)/4, $0x02010080
DATA ·shuffleInt32x27to31bits+240+52(SB)/4, $0x05040302
DATA ·shuffleInt32x27to31bits+240+56(SB)/4, $0x09080706
DATA ·shuffleInt32x27to31bits+240+60(SB)/4, $0x0D0C0B0A

DATA ·shuffleInt32x27to31bits+240+64(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+240+68(SB)/4, $0x06808080
DATA ·shuffleInt32x27to31bits+240+72(SB)/4, $0x0A808080
DATA ·shuffleInt32x27to31bits+240+76(SB)/4, $0x80808080

// 31 bits => 32 bits
// ------------------
// 0: [a,a,a,a,a,a,a,a]
// 1: [a,a,a,a,a,a,a,a]
// 2: [a,a,a,a,a,a,a,a]
// 3: [a,a,a,a,a,a,a,b]
// 4: [b,b,b,b,b,b,b,b]
// 5: [b,b,b,b,b,b,b,b]
// 6: [b,b,b,b,b,b,b,b]
// 7: [b,b,b,b,b,b,c,c]
// 8: [c,c,c,c,c,c,c,c]
// 9: [c,c,c,c,c,c,c,c]
// A: [c,c,c,c,c,c,c,c]
// B: [c,c,c,c,c,d,d,d]
// C: [d,d,d,d,d,d,d,d]
// D: [d,d,d,d,d,d,d,d]
// E: [d,d,d,d,d,d,d,d]
// F: [d,d,d,d,e,e,e,e]
// ---
// 0: [e,e,e,e,e,e,e,e]
// 1: [e,e,e,e,e,e,e,e]
// 2: [e,e,e,e,e,e,e,e]
// 3: [e,e,e,f,f,f,f,f]
// 4: [f,f,f,f,f,f,f,f]
// 5: [f,f,f,f,f,f,f,f]
// 6: [f,f,f,f,f,f,f,f]
// 7: [f,f,g,g,g,g,g,g]
// 8: [g,g,g,g,g,g,g,g]
// 9: [g,g,g,g,g,g,g,g]
// A: [g,g,g,g,g,g,g,g]
// B: [g,h,h,h,h,h,h,h]
// C: [h,h,h,h,h,h,h,h]
// D: [h,h,h,h,h,h,h,h]
// E: [h,h,h,h,h,h,h,h]
// ...
DATA ·shuffleInt32x27to31bits+320+0(SB)/4,  $0x03020100
DATA ·shuffleInt32x27to31bits+320+4(SB)/4,  $0x06050403
DATA ·shuffleInt32x27to31bits+320+8(SB)/4,  $0x0A090807
DATA ·shuffleInt32x27to31bits+320+12(SB)/4, $0x0E0D0C0B

DATA ·shuffleInt32x27to31bits+320+16(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+320+20(SB)/4, $0x07808080
DATA ·shuffleInt32x27to31bits+320+24(SB)/4, $0x0B808080
DATA ·shuffleInt32x27to31bits+320+28(SB)/4, $0x0F808080

DATA ·shuffleInt32x27to31bits+320+32(SB)/4, $0x8080800F
DATA ·shuffleInt32x27to31bits+320+36(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+320+40(SB)/4, $0x80808080
DATA ·shuffleInt32x27to31bits+320+44(SB)/4, $0x80808080

DATA ·shuffleInt32x27to31bits+320+48(SB)/4, $0x02010080
DATA ·shuffleInt32x27to31bits+320+52(SB)/4, $0x06050403
DATA ·shuffleInt32x27to31bits+320+56(SB)/4, $0x0A090807
DATA ·shuffleInt32x27to31bits+320+60(SB)/4, $0x0E0D0C0B

DATA ·shuffleInt32x27to31bits+320+64(SB)/4, $0x03808080
DATA ·shuffleInt32x27to31bits+320+68(SB)/4, $0x07808080
DATA ·shuffleInt32x27to31bits+320+72(SB)/4, $0x0B808080
DATA ·shuffleInt32x27to31bits+320+76(SB)/4, $0x80808080

// The RIGHT shifts to unpack 32 bits integers.
//
// The following formula was determined empirically as the expression which
// generates shift values:
//
//      shift[i] = (i * bitWidth) % 8
//
GLOBL ·shiftRightInt32(SB), RODATA|NOPTR, $256

DATA ·shiftRightInt32+0+0(SB)/4,  $0
DATA ·shiftRightInt32+0+4(SB)/4,  $1
DATA ·shiftRightInt32+0+8(SB)/4,  $2
DATA ·shiftRightInt32+0+12(SB)/4, $3
DATA ·shiftRightInt32+0+16(SB)/4, $4
DATA ·shiftRightInt32+0+20(SB)/4, $5
DATA ·shiftRightInt32+0+24(SB)/4, $6
DATA ·shiftRightInt32+0+28(SB)/4, $7

DATA ·shiftRightInt32+32+0(SB)/4,  $0
DATA ·shiftRightInt32+32+4(SB)/4,  $2
DATA ·shiftRightInt32+32+8(SB)/4,  $4
DATA ·shiftRightInt32+32+12(SB)/4, $6
DATA ·shiftRightInt32+32+16(SB)/4, $0
DATA ·shiftRightInt32+32+20(SB)/4, $2
DATA ·shiftRightInt32+32+24(SB)/4, $4
DATA ·shiftRightInt32+32+28(SB)/4, $6

DATA ·shiftRightInt32+64+0(SB)/4,  $0
DATA ·shiftRightInt32+64+4(SB)/4,  $3
DATA ·shiftRightInt32+64+8(SB)/4,  $6
DATA ·shiftRightInt32+64+12(SB)/4, $1
DATA ·shiftRightInt32+64+16(SB)/4, $4
DATA ·shiftRightInt32+64+20(SB)/4, $7
DATA ·shiftRightInt32+64+24(SB)/4, $2
DATA ·shiftRightInt32+64+28(SB)/4, $5

DATA ·shiftRightInt32+96+0(SB)/4,  $0
DATA ·shiftRightInt32+96+4(SB)/4,  $4
DATA ·shiftRightInt32+96+8(SB)/4,  $0
DATA ·shiftRightInt32+96+12(SB)/4, $4
DATA ·shiftRightInt32+96+16(SB)/4, $0
DATA ·shiftRightInt32+96+20(SB)/4, $4
DATA ·shiftRightInt32+96+24(SB)/4, $0
DATA ·shiftRightInt32+96+28(SB)/4, $4

DATA ·shiftRightInt32+128+0(SB)/4,  $0
DATA ·shiftRightInt32+128+4(SB)/4,  $5
DATA ·shiftRightInt32+128+8(SB)/4,  $2
DATA ·shiftRightInt32+128+12(SB)/4, $7
DATA ·shiftRightInt32+128+16(SB)/4, $4
DATA ·shiftRightInt32+128+20(SB)/4, $1
DATA ·shiftRightInt32+128+24(SB)/4, $6
DATA ·shiftRightInt32+128+28(SB)/4, $3

DATA ·shiftRightInt32+160+0(SB)/4,  $0
DATA ·shiftRightInt32+160+4(SB)/4,  $6
DATA ·shiftRightInt32+160+8(SB)/4,  $4
DATA ·shiftRightInt32+160+12(SB)/4, $2
DATA ·shiftRightInt32+160+16(SB)/4, $0
DATA ·shiftRightInt32+160+20(SB)/4, $6
DATA ·shiftRightInt32+160+24(SB)/4, $4
DATA ·shiftRightInt32+160+28(SB)/4, $2

DATA ·shiftRightInt32+192+0(SB)/4,  $0
DATA ·shiftRightInt32+192+4(SB)/4,  $7
DATA ·shiftRightInt32+192+8(SB)/4,  $6
DATA ·shiftRightInt32+192+12(SB)/4, $5
DATA ·shiftRightInt32+192+16(SB)/4, $4
DATA ·shiftRightInt32+192+20(SB)/4, $3
DATA ·shiftRightInt32+192+24(SB)/4, $2
DATA ·shiftRightInt32+192+28(SB)/4, $1

DATA ·shiftRightInt32+224+0(SB)/4,  $0
DATA ·shiftRightInt32+224+4(SB)/4,  $0
DATA ·shiftRightInt32+224+8(SB)/4,  $0
DATA ·shiftRightInt32+224+12(SB)/4, $0
DATA ·shiftRightInt32+224+16(SB)/4, $0
DATA ·shiftRightInt32+224+20(SB)/4, $0
DATA ·shiftRightInt32+224+24(SB)/4, $0
DATA ·shiftRightInt32+224+28(SB)/4, $0

// The LEFT shifts to unpack 32 bits integers.
//
// The following formula was determined empirically as the expression which
// generates shift values:
//
//      shift[i] = (8 - (i * bitWidth)) % 8
//
GLOBL ·shiftLeftInt32(SB), RODATA|NOPTR, $256

DATA ·shiftLeftInt32+0+0(SB)/4,  $0
DATA ·shiftLeftInt32+0+4(SB)/4,  $7
DATA ·shiftLeftInt32+0+8(SB)/4,  $6
DATA ·shiftLeftInt32+0+12(SB)/4, $5
DATA ·shiftLeftInt32+0+16(SB)/4, $4
DATA ·shiftLeftInt32+0+20(SB)/4, $3
DATA ·shiftLeftInt32+0+24(SB)/4, $2
DATA ·shiftLeftInt32+0+28(SB)/4, $1

DATA ·shiftLeftInt32+32+0(SB)/4,  $0
DATA ·shiftLeftInt32+32+4(SB)/4,  $6
DATA ·shiftLeftInt32+32+8(SB)/4,  $4
DATA ·shiftLeftInt32+32+12(SB)/4, $2
DATA ·shiftLeftInt32+32+16(SB)/4, $0
DATA ·shiftLeftInt32+32+20(SB)/4, $6
DATA ·shiftLeftInt32+32+24(SB)/4, $4
DATA ·shiftLeftInt32+32+28(SB)/4, $2

DATA ·shiftLeftInt32+64+0(SB)/4,  $0
DATA ·shiftLeftInt32+64+4(SB)/4,  $5
DATA ·shiftLeftInt32+64+8(SB)/4,  $2
DATA ·shiftLeftInt32+64+12(SB)/4, $7
DATA ·shiftLeftInt32+64+16(SB)/4, $4
DATA ·shiftLeftInt32+64+20(SB)/4, $1
DATA ·shiftLeftInt32+64+24(SB)/4, $6
DATA ·shiftLeftInt32+64+28(SB)/4, $3

DATA ·shiftLeftInt32+96+0(SB)/4,  $0
DATA ·shiftLeftInt32+96+4(SB)/4,  $4
DATA ·shiftLeftInt32+96+8(SB)/4,  $0
DATA ·shiftLeftInt32+96+12(SB)/4, $4
DATA ·shiftLeftInt32+96+16(SB)/4, $0
DATA ·shiftLeftInt32+96+20(SB)/4, $4
DATA ·shiftLeftInt32+96+24(SB)/4, $0
DATA ·shiftLeftInt32+96+28(SB)/4, $4

DATA ·shiftLeftInt32+128+0(SB)/4,  $0
DATA ·shiftLeftInt32+128+4(SB)/4,  $3
DATA ·shiftLeftInt32+128+8(SB)/4,  $6
DATA ·shiftLeftInt32+128+12(SB)/4, $1
DATA ·shiftLeftInt32+128+16(SB)/4, $4
DATA ·shiftLeftInt32+128+20(SB)/4, $7
DATA ·shiftLeftInt32+128+24(SB)/4, $2
DATA ·shiftLeftInt32+128+28(SB)/4, $5

DATA ·shiftLeftInt32+160+0(SB)/4,  $0
DATA ·shiftLeftInt32+160+4(SB)/4,  $2
DATA ·shiftLeftInt32+160+8(SB)/4,  $4
DATA ·shiftLeftInt32+160+12(SB)/4, $6
DATA ·shiftLeftInt32+160+16(SB)/4, $0
DATA ·shiftLeftInt32+160+20(SB)/4, $2
DATA ·shiftLeftInt32+160+24(SB)/4, $4
DATA ·shiftLeftInt32+160+28(SB)/4, $6

DATA ·shiftLeftInt32+192+0(SB)/4,  $0
DATA ·shiftLeftInt32+192+4(SB)/4,  $1
DATA ·shiftLeftInt32+192+8(SB)/4,  $2
DATA ·shiftLeftInt32+192+12(SB)/4, $3
DATA ·shiftLeftInt32+192+16(SB)/4, $4
DATA ·shiftLeftInt32+192+20(SB)/4, $5
DATA ·shiftLeftInt32+192+24(SB)/4, $6
DATA ·shiftLeftInt32+192+28(SB)/4, $7

DATA ·shiftLeftInt32+224+0(SB)/4,  $0
DATA ·shiftLeftInt32+224+4(SB)/4,  $0
DATA ·shiftLeftInt32+224+8(SB)/4,  $0
DATA ·shiftLeftInt32+224+12(SB)/4, $0
DATA ·shiftLeftInt32+224+16(SB)/4, $0
DATA ·shiftLeftInt32+224+20(SB)/4, $0
DATA ·shiftLeftInt32+224+24(SB)/4, $0
DATA ·shiftLeftInt32+224+28(SB)/4, $0
