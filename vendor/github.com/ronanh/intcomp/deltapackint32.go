// This file is generated, do not modify directly
// Use 'go generate' to regenerate.

package intcomp

import "unsafe"

// deltaPack_int32 Binary packing of one block of `in`, starting from `initoffset`
// to out. Differential coding is applied first.
// Caller must give the proper `bitlen` of the block
func deltaPack_int32[T uint32 | int32](initoffset T, in []T, out []uint32, bitlen int) {
	switch bitlen {
	case 0:
		deltapack32_0(initoffset, (*[32]T)(in), (*[0]uint32)(out))
	case 1:
		deltapack32_1(initoffset, (*[32]T)(in), (*[1]uint32)(out))
	case 2:
		deltapack32_2(initoffset, (*[32]T)(in), (*[2]uint32)(out))
	case 3:
		deltapack32_3(initoffset, (*[32]T)(in), (*[3]uint32)(out))
	case 4:
		deltapack32_4(initoffset, (*[32]T)(in), (*[4]uint32)(out))
	case 5:
		deltapack32_5(initoffset, (*[32]T)(in), (*[5]uint32)(out))
	case 6:
		deltapack32_6(initoffset, (*[32]T)(in), (*[6]uint32)(out))
	case 7:
		deltapack32_7(initoffset, (*[32]T)(in), (*[7]uint32)(out))
	case 8:
		deltapack32_8(initoffset, (*[32]T)(in), (*[8]uint32)(out))
	case 9:
		deltapack32_9(initoffset, (*[32]T)(in), (*[9]uint32)(out))
	case 10:
		deltapack32_10(initoffset, (*[32]T)(in), (*[10]uint32)(out))
	case 11:
		deltapack32_11(initoffset, (*[32]T)(in), (*[11]uint32)(out))
	case 12:
		deltapack32_12(initoffset, (*[32]T)(in), (*[12]uint32)(out))
	case 13:
		deltapack32_13(initoffset, (*[32]T)(in), (*[13]uint32)(out))
	case 14:
		deltapack32_14(initoffset, (*[32]T)(in), (*[14]uint32)(out))
	case 15:
		deltapack32_15(initoffset, (*[32]T)(in), (*[15]uint32)(out))
	case 16:
		deltapack32_16(initoffset, (*[32]T)(in), (*[16]uint32)(out))
	case 17:
		deltapack32_17(initoffset, (*[32]T)(in), (*[17]uint32)(out))
	case 18:
		deltapack32_18(initoffset, (*[32]T)(in), (*[18]uint32)(out))
	case 19:
		deltapack32_19(initoffset, (*[32]T)(in), (*[19]uint32)(out))
	case 20:
		deltapack32_20(initoffset, (*[32]T)(in), (*[20]uint32)(out))
	case 21:
		deltapack32_21(initoffset, (*[32]T)(in), (*[21]uint32)(out))
	case 22:
		deltapack32_22(initoffset, (*[32]T)(in), (*[22]uint32)(out))
	case 23:
		deltapack32_23(initoffset, (*[32]T)(in), (*[23]uint32)(out))
	case 24:
		deltapack32_24(initoffset, (*[32]T)(in), (*[24]uint32)(out))
	case 25:
		deltapack32_25(initoffset, (*[32]T)(in), (*[25]uint32)(out))
	case 26:
		deltapack32_26(initoffset, (*[32]T)(in), (*[26]uint32)(out))
	case 27:
		deltapack32_27(initoffset, (*[32]T)(in), (*[27]uint32)(out))
	case 28:
		deltapack32_28(initoffset, (*[32]T)(in), (*[28]uint32)(out))
	case 29:
		deltapack32_29(initoffset, (*[32]T)(in), (*[29]uint32)(out))
	case 30:
		deltapack32_30(initoffset, (*[32]T)(in), (*[30]uint32)(out))
	case 31:
		deltapack32_31(initoffset, (*[32]T)(in), (*[31]uint32)(out))
	case 32:
		*(*[32]uint32)(out) = *((*[32]uint32)(unsafe.Pointer((*[32]T)(in))))
	default:
		panic("unsupported bitlen")
	}
}

// deltaUnpack_int32 Decoding operation for DeltaPack_int32
func deltaUnpack_int32[T uint32 | int32](initoffset T, in []uint32, out []T, bitlen int) {
	switch bitlen {
	case 0:
		deltaunpack32_0(initoffset, (*[0]uint32)(in), (*[32]T)(out))
	case 1:
		deltaunpack32_1(initoffset, (*[1]uint32)(in), (*[32]T)(out))
	case 2:
		deltaunpack32_2(initoffset, (*[2]uint32)(in), (*[32]T)(out))
	case 3:
		deltaunpack32_3(initoffset, (*[3]uint32)(in), (*[32]T)(out))
	case 4:
		deltaunpack32_4(initoffset, (*[4]uint32)(in), (*[32]T)(out))
	case 5:
		deltaunpack32_5(initoffset, (*[5]uint32)(in), (*[32]T)(out))
	case 6:
		deltaunpack32_6(initoffset, (*[6]uint32)(in), (*[32]T)(out))
	case 7:
		deltaunpack32_7(initoffset, (*[7]uint32)(in), (*[32]T)(out))
	case 8:
		deltaunpack32_8(initoffset, (*[8]uint32)(in), (*[32]T)(out))
	case 9:
		deltaunpack32_9(initoffset, (*[9]uint32)(in), (*[32]T)(out))
	case 10:
		deltaunpack32_10(initoffset, (*[10]uint32)(in), (*[32]T)(out))
	case 11:
		deltaunpack32_11(initoffset, (*[11]uint32)(in), (*[32]T)(out))
	case 12:
		deltaunpack32_12(initoffset, (*[12]uint32)(in), (*[32]T)(out))
	case 13:
		deltaunpack32_13(initoffset, (*[13]uint32)(in), (*[32]T)(out))
	case 14:
		deltaunpack32_14(initoffset, (*[14]uint32)(in), (*[32]T)(out))
	case 15:
		deltaunpack32_15(initoffset, (*[15]uint32)(in), (*[32]T)(out))
	case 16:
		deltaunpack32_16(initoffset, (*[16]uint32)(in), (*[32]T)(out))
	case 17:
		deltaunpack32_17(initoffset, (*[17]uint32)(in), (*[32]T)(out))
	case 18:
		deltaunpack32_18(initoffset, (*[18]uint32)(in), (*[32]T)(out))
	case 19:
		deltaunpack32_19(initoffset, (*[19]uint32)(in), (*[32]T)(out))
	case 20:
		deltaunpack32_20(initoffset, (*[20]uint32)(in), (*[32]T)(out))
	case 21:
		deltaunpack32_21(initoffset, (*[21]uint32)(in), (*[32]T)(out))
	case 22:
		deltaunpack32_22(initoffset, (*[22]uint32)(in), (*[32]T)(out))
	case 23:
		deltaunpack32_23(initoffset, (*[23]uint32)(in), (*[32]T)(out))
	case 24:
		deltaunpack32_24(initoffset, (*[24]uint32)(in), (*[32]T)(out))
	case 25:
		deltaunpack32_25(initoffset, (*[25]uint32)(in), (*[32]T)(out))
	case 26:
		deltaunpack32_26(initoffset, (*[26]uint32)(in), (*[32]T)(out))
	case 27:
		deltaunpack32_27(initoffset, (*[27]uint32)(in), (*[32]T)(out))
	case 28:
		deltaunpack32_28(initoffset, (*[28]uint32)(in), (*[32]T)(out))
	case 29:
		deltaunpack32_29(initoffset, (*[29]uint32)(in), (*[32]T)(out))
	case 30:
		deltaunpack32_30(initoffset, (*[30]uint32)(in), (*[32]T)(out))
	case 31:
		deltaunpack32_31(initoffset, (*[31]uint32)(in), (*[32]T)(out))
	case 32:
		*(*[32]T)(out) = *(*[32]T)(unsafe.Pointer((*[32]uint32)(in)))
	default:
		panic("unsupported bitlen")
	}
}

func deltapack32_0[T uint32 | int32](initoffset T, in *[32]T, out *[0]uint32) {
}

func deltapack32_1[T uint32 | int32](initoffset T, in *[32]T, out *[1]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 1) |
			((in[2] - in[1]) << 2) |
			((in[3] - in[2]) << 3) |
			((in[4] - in[3]) << 4) |
			((in[5] - in[4]) << 5) |
			((in[6] - in[5]) << 6) |
			((in[7] - in[6]) << 7) |
			((in[8] - in[7]) << 8) |
			((in[9] - in[8]) << 9) |
			((in[10] - in[9]) << 10) |
			((in[11] - in[10]) << 11) |
			((in[12] - in[11]) << 12) |
			((in[13] - in[12]) << 13) |
			((in[14] - in[13]) << 14) |
			((in[15] - in[14]) << 15) |
			((in[16] - in[15]) << 16) |
			((in[17] - in[16]) << 17) |
			((in[18] - in[17]) << 18) |
			((in[19] - in[18]) << 19) |
			((in[20] - in[19]) << 20) |
			((in[21] - in[20]) << 21) |
			((in[22] - in[21]) << 22) |
			((in[23] - in[22]) << 23) |
			((in[24] - in[23]) << 24) |
			((in[25] - in[24]) << 25) |
			((in[26] - in[25]) << 26) |
			((in[27] - in[26]) << 27) |
			((in[28] - in[27]) << 28) |
			((in[29] - in[28]) << 29) |
			((in[30] - in[29]) << 30) |
			((in[31] - in[30]) << 31))
}

func deltapack32_2[T uint32 | int32](initoffset T, in *[32]T, out *[2]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 2) |
			((in[2] - in[1]) << 4) |
			((in[3] - in[2]) << 6) |
			((in[4] - in[3]) << 8) |
			((in[5] - in[4]) << 10) |
			((in[6] - in[5]) << 12) |
			((in[7] - in[6]) << 14) |
			((in[8] - in[7]) << 16) |
			((in[9] - in[8]) << 18) |
			((in[10] - in[9]) << 20) |
			((in[11] - in[10]) << 22) |
			((in[12] - in[11]) << 24) |
			((in[13] - in[12]) << 26) |
			((in[14] - in[13]) << 28) |
			((in[15] - in[14]) << 30))
	out[1] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 2) |
			((in[18] - in[17]) << 4) |
			((in[19] - in[18]) << 6) |
			((in[20] - in[19]) << 8) |
			((in[21] - in[20]) << 10) |
			((in[22] - in[21]) << 12) |
			((in[23] - in[22]) << 14) |
			((in[24] - in[23]) << 16) |
			((in[25] - in[24]) << 18) |
			((in[26] - in[25]) << 20) |
			((in[27] - in[26]) << 22) |
			((in[28] - in[27]) << 24) |
			((in[29] - in[28]) << 26) |
			((in[30] - in[29]) << 28) |
			((in[31] - in[30]) << 30))
}

func deltapack32_3[T uint32 | int32](initoffset T, in *[32]T, out *[3]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 3) |
			((in[2] - in[1]) << 6) |
			((in[3] - in[2]) << 9) |
			((in[4] - in[3]) << 12) |
			((in[5] - in[4]) << 15) |
			((in[6] - in[5]) << 18) |
			((in[7] - in[6]) << 21) |
			((in[8] - in[7]) << 24) |
			((in[9] - in[8]) << 27) |
			((in[10] - in[9]) << 30))
	out[1] = uint32(
		(in[10]-in[9])>>2 |
			((in[11] - in[10]) << 1) |
			((in[12] - in[11]) << 4) |
			((in[13] - in[12]) << 7) |
			((in[14] - in[13]) << 10) |
			((in[15] - in[14]) << 13) |
			((in[16] - in[15]) << 16) |
			((in[17] - in[16]) << 19) |
			((in[18] - in[17]) << 22) |
			((in[19] - in[18]) << 25) |
			((in[20] - in[19]) << 28) |
			((in[21] - in[20]) << 31))
	out[2] = uint32(
		(in[21]-in[20])>>1 |
			((in[22] - in[21]) << 2) |
			((in[23] - in[22]) << 5) |
			((in[24] - in[23]) << 8) |
			((in[25] - in[24]) << 11) |
			((in[26] - in[25]) << 14) |
			((in[27] - in[26]) << 17) |
			((in[28] - in[27]) << 20) |
			((in[29] - in[28]) << 23) |
			((in[30] - in[29]) << 26) |
			((in[31] - in[30]) << 29))
}

func deltapack32_4[T uint32 | int32](initoffset T, in *[32]T, out *[4]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 4) |
			((in[2] - in[1]) << 8) |
			((in[3] - in[2]) << 12) |
			((in[4] - in[3]) << 16) |
			((in[5] - in[4]) << 20) |
			((in[6] - in[5]) << 24) |
			((in[7] - in[6]) << 28))
	out[1] = uint32(
		in[8] - in[7] |
			((in[9] - in[8]) << 4) |
			((in[10] - in[9]) << 8) |
			((in[11] - in[10]) << 12) |
			((in[12] - in[11]) << 16) |
			((in[13] - in[12]) << 20) |
			((in[14] - in[13]) << 24) |
			((in[15] - in[14]) << 28))
	out[2] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 4) |
			((in[18] - in[17]) << 8) |
			((in[19] - in[18]) << 12) |
			((in[20] - in[19]) << 16) |
			((in[21] - in[20]) << 20) |
			((in[22] - in[21]) << 24) |
			((in[23] - in[22]) << 28))
	out[3] = uint32(
		in[24] - in[23] |
			((in[25] - in[24]) << 4) |
			((in[26] - in[25]) << 8) |
			((in[27] - in[26]) << 12) |
			((in[28] - in[27]) << 16) |
			((in[29] - in[28]) << 20) |
			((in[30] - in[29]) << 24) |
			((in[31] - in[30]) << 28))
}

func deltapack32_5[T uint32 | int32](initoffset T, in *[32]T, out *[5]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 5) |
			((in[2] - in[1]) << 10) |
			((in[3] - in[2]) << 15) |
			((in[4] - in[3]) << 20) |
			((in[5] - in[4]) << 25) |
			((in[6] - in[5]) << 30))
	out[1] = uint32(
		(in[6]-in[5])>>2 |
			((in[7] - in[6]) << 3) |
			((in[8] - in[7]) << 8) |
			((in[9] - in[8]) << 13) |
			((in[10] - in[9]) << 18) |
			((in[11] - in[10]) << 23) |
			((in[12] - in[11]) << 28))
	out[2] = uint32(
		(in[12]-in[11])>>4 |
			((in[13] - in[12]) << 1) |
			((in[14] - in[13]) << 6) |
			((in[15] - in[14]) << 11) |
			((in[16] - in[15]) << 16) |
			((in[17] - in[16]) << 21) |
			((in[18] - in[17]) << 26) |
			((in[19] - in[18]) << 31))
	out[3] = uint32(
		(in[19]-in[18])>>1 |
			((in[20] - in[19]) << 4) |
			((in[21] - in[20]) << 9) |
			((in[22] - in[21]) << 14) |
			((in[23] - in[22]) << 19) |
			((in[24] - in[23]) << 24) |
			((in[25] - in[24]) << 29))
	out[4] = uint32(
		(in[25]-in[24])>>3 |
			((in[26] - in[25]) << 2) |
			((in[27] - in[26]) << 7) |
			((in[28] - in[27]) << 12) |
			((in[29] - in[28]) << 17) |
			((in[30] - in[29]) << 22) |
			((in[31] - in[30]) << 27))
}

func deltapack32_6[T uint32 | int32](initoffset T, in *[32]T, out *[6]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 6) |
			((in[2] - in[1]) << 12) |
			((in[3] - in[2]) << 18) |
			((in[4] - in[3]) << 24) |
			((in[5] - in[4]) << 30))
	out[1] = uint32(
		(in[5]-in[4])>>2 |
			((in[6] - in[5]) << 4) |
			((in[7] - in[6]) << 10) |
			((in[8] - in[7]) << 16) |
			((in[9] - in[8]) << 22) |
			((in[10] - in[9]) << 28))
	out[2] = uint32(
		(in[10]-in[9])>>4 |
			((in[11] - in[10]) << 2) |
			((in[12] - in[11]) << 8) |
			((in[13] - in[12]) << 14) |
			((in[14] - in[13]) << 20) |
			((in[15] - in[14]) << 26))
	out[3] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 6) |
			((in[18] - in[17]) << 12) |
			((in[19] - in[18]) << 18) |
			((in[20] - in[19]) << 24) |
			((in[21] - in[20]) << 30))
	out[4] = uint32(
		(in[21]-in[20])>>2 |
			((in[22] - in[21]) << 4) |
			((in[23] - in[22]) << 10) |
			((in[24] - in[23]) << 16) |
			((in[25] - in[24]) << 22) |
			((in[26] - in[25]) << 28))
	out[5] = uint32(
		(in[26]-in[25])>>4 |
			((in[27] - in[26]) << 2) |
			((in[28] - in[27]) << 8) |
			((in[29] - in[28]) << 14) |
			((in[30] - in[29]) << 20) |
			((in[31] - in[30]) << 26))
}

func deltapack32_7[T uint32 | int32](initoffset T, in *[32]T, out *[7]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 7) |
			((in[2] - in[1]) << 14) |
			((in[3] - in[2]) << 21) |
			((in[4] - in[3]) << 28))
	out[1] = uint32(
		(in[4]-in[3])>>4 |
			((in[5] - in[4]) << 3) |
			((in[6] - in[5]) << 10) |
			((in[7] - in[6]) << 17) |
			((in[8] - in[7]) << 24) |
			((in[9] - in[8]) << 31))
	out[2] = uint32(
		(in[9]-in[8])>>1 |
			((in[10] - in[9]) << 6) |
			((in[11] - in[10]) << 13) |
			((in[12] - in[11]) << 20) |
			((in[13] - in[12]) << 27))
	out[3] = uint32(
		(in[13]-in[12])>>5 |
			((in[14] - in[13]) << 2) |
			((in[15] - in[14]) << 9) |
			((in[16] - in[15]) << 16) |
			((in[17] - in[16]) << 23) |
			((in[18] - in[17]) << 30))
	out[4] = uint32(
		(in[18]-in[17])>>2 |
			((in[19] - in[18]) << 5) |
			((in[20] - in[19]) << 12) |
			((in[21] - in[20]) << 19) |
			((in[22] - in[21]) << 26))
	out[5] = uint32(
		(in[22]-in[21])>>6 |
			((in[23] - in[22]) << 1) |
			((in[24] - in[23]) << 8) |
			((in[25] - in[24]) << 15) |
			((in[26] - in[25]) << 22) |
			((in[27] - in[26]) << 29))
	out[6] = uint32(
		(in[27]-in[26])>>3 |
			((in[28] - in[27]) << 4) |
			((in[29] - in[28]) << 11) |
			((in[30] - in[29]) << 18) |
			((in[31] - in[30]) << 25))
}

func deltapack32_8[T uint32 | int32](initoffset T, in *[32]T, out *[8]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 8) |
			((in[2] - in[1]) << 16) |
			((in[3] - in[2]) << 24))
	out[1] = uint32(
		in[4] - in[3] |
			((in[5] - in[4]) << 8) |
			((in[6] - in[5]) << 16) |
			((in[7] - in[6]) << 24))
	out[2] = uint32(
		in[8] - in[7] |
			((in[9] - in[8]) << 8) |
			((in[10] - in[9]) << 16) |
			((in[11] - in[10]) << 24))
	out[3] = uint32(
		in[12] - in[11] |
			((in[13] - in[12]) << 8) |
			((in[14] - in[13]) << 16) |
			((in[15] - in[14]) << 24))
	out[4] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 8) |
			((in[18] - in[17]) << 16) |
			((in[19] - in[18]) << 24))
	out[5] = uint32(
		in[20] - in[19] |
			((in[21] - in[20]) << 8) |
			((in[22] - in[21]) << 16) |
			((in[23] - in[22]) << 24))
	out[6] = uint32(
		in[24] - in[23] |
			((in[25] - in[24]) << 8) |
			((in[26] - in[25]) << 16) |
			((in[27] - in[26]) << 24))
	out[7] = uint32(
		in[28] - in[27] |
			((in[29] - in[28]) << 8) |
			((in[30] - in[29]) << 16) |
			((in[31] - in[30]) << 24))
}

func deltapack32_9[T uint32 | int32](initoffset T, in *[32]T, out *[9]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 9) |
			((in[2] - in[1]) << 18) |
			((in[3] - in[2]) << 27))
	out[1] = uint32(
		(in[3]-in[2])>>5 |
			((in[4] - in[3]) << 4) |
			((in[5] - in[4]) << 13) |
			((in[6] - in[5]) << 22) |
			((in[7] - in[6]) << 31))
	out[2] = uint32(
		(in[7]-in[6])>>1 |
			((in[8] - in[7]) << 8) |
			((in[9] - in[8]) << 17) |
			((in[10] - in[9]) << 26))
	out[3] = uint32(
		(in[10]-in[9])>>6 |
			((in[11] - in[10]) << 3) |
			((in[12] - in[11]) << 12) |
			((in[13] - in[12]) << 21) |
			((in[14] - in[13]) << 30))
	out[4] = uint32(
		(in[14]-in[13])>>2 |
			((in[15] - in[14]) << 7) |
			((in[16] - in[15]) << 16) |
			((in[17] - in[16]) << 25))
	out[5] = uint32(
		(in[17]-in[16])>>7 |
			((in[18] - in[17]) << 2) |
			((in[19] - in[18]) << 11) |
			((in[20] - in[19]) << 20) |
			((in[21] - in[20]) << 29))
	out[6] = uint32(
		(in[21]-in[20])>>3 |
			((in[22] - in[21]) << 6) |
			((in[23] - in[22]) << 15) |
			((in[24] - in[23]) << 24))
	out[7] = uint32(
		(in[24]-in[23])>>8 |
			((in[25] - in[24]) << 1) |
			((in[26] - in[25]) << 10) |
			((in[27] - in[26]) << 19) |
			((in[28] - in[27]) << 28))
	out[8] = uint32(
		(in[28]-in[27])>>4 |
			((in[29] - in[28]) << 5) |
			((in[30] - in[29]) << 14) |
			((in[31] - in[30]) << 23))
}

func deltapack32_10[T uint32 | int32](initoffset T, in *[32]T, out *[10]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 10) |
			((in[2] - in[1]) << 20) |
			((in[3] - in[2]) << 30))
	out[1] = uint32(
		(in[3]-in[2])>>2 |
			((in[4] - in[3]) << 8) |
			((in[5] - in[4]) << 18) |
			((in[6] - in[5]) << 28))
	out[2] = uint32(
		(in[6]-in[5])>>4 |
			((in[7] - in[6]) << 6) |
			((in[8] - in[7]) << 16) |
			((in[9] - in[8]) << 26))
	out[3] = uint32(
		(in[9]-in[8])>>6 |
			((in[10] - in[9]) << 4) |
			((in[11] - in[10]) << 14) |
			((in[12] - in[11]) << 24))
	out[4] = uint32(
		(in[12]-in[11])>>8 |
			((in[13] - in[12]) << 2) |
			((in[14] - in[13]) << 12) |
			((in[15] - in[14]) << 22))
	out[5] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 10) |
			((in[18] - in[17]) << 20) |
			((in[19] - in[18]) << 30))
	out[6] = uint32(
		(in[19]-in[18])>>2 |
			((in[20] - in[19]) << 8) |
			((in[21] - in[20]) << 18) |
			((in[22] - in[21]) << 28))
	out[7] = uint32(
		(in[22]-in[21])>>4 |
			((in[23] - in[22]) << 6) |
			((in[24] - in[23]) << 16) |
			((in[25] - in[24]) << 26))
	out[8] = uint32(
		(in[25]-in[24])>>6 |
			((in[26] - in[25]) << 4) |
			((in[27] - in[26]) << 14) |
			((in[28] - in[27]) << 24))
	out[9] = uint32(
		(in[28]-in[27])>>8 |
			((in[29] - in[28]) << 2) |
			((in[30] - in[29]) << 12) |
			((in[31] - in[30]) << 22))
}

func deltapack32_11[T uint32 | int32](initoffset T, in *[32]T, out *[11]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 11) |
			((in[2] - in[1]) << 22))
	out[1] = uint32(
		(in[2]-in[1])>>10 |
			((in[3] - in[2]) << 1) |
			((in[4] - in[3]) << 12) |
			((in[5] - in[4]) << 23))
	out[2] = uint32(
		(in[5]-in[4])>>9 |
			((in[6] - in[5]) << 2) |
			((in[7] - in[6]) << 13) |
			((in[8] - in[7]) << 24))
	out[3] = uint32(
		(in[8]-in[7])>>8 |
			((in[9] - in[8]) << 3) |
			((in[10] - in[9]) << 14) |
			((in[11] - in[10]) << 25))
	out[4] = uint32(
		(in[11]-in[10])>>7 |
			((in[12] - in[11]) << 4) |
			((in[13] - in[12]) << 15) |
			((in[14] - in[13]) << 26))
	out[5] = uint32(
		(in[14]-in[13])>>6 |
			((in[15] - in[14]) << 5) |
			((in[16] - in[15]) << 16) |
			((in[17] - in[16]) << 27))
	out[6] = uint32(
		(in[17]-in[16])>>5 |
			((in[18] - in[17]) << 6) |
			((in[19] - in[18]) << 17) |
			((in[20] - in[19]) << 28))
	out[7] = uint32(
		(in[20]-in[19])>>4 |
			((in[21] - in[20]) << 7) |
			((in[22] - in[21]) << 18) |
			((in[23] - in[22]) << 29))
	out[8] = uint32(
		(in[23]-in[22])>>3 |
			((in[24] - in[23]) << 8) |
			((in[25] - in[24]) << 19) |
			((in[26] - in[25]) << 30))
	out[9] = uint32(
		(in[26]-in[25])>>2 |
			((in[27] - in[26]) << 9) |
			((in[28] - in[27]) << 20) |
			((in[29] - in[28]) << 31))
	out[10] = uint32(
		(in[29]-in[28])>>1 |
			((in[30] - in[29]) << 10) |
			((in[31] - in[30]) << 21))
}

func deltapack32_12[T uint32 | int32](initoffset T, in *[32]T, out *[12]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 12) |
			((in[2] - in[1]) << 24))
	out[1] = uint32(
		(in[2]-in[1])>>8 |
			((in[3] - in[2]) << 4) |
			((in[4] - in[3]) << 16) |
			((in[5] - in[4]) << 28))
	out[2] = uint32(
		(in[5]-in[4])>>4 |
			((in[6] - in[5]) << 8) |
			((in[7] - in[6]) << 20))
	out[3] = uint32(
		in[8] - in[7] |
			((in[9] - in[8]) << 12) |
			((in[10] - in[9]) << 24))
	out[4] = uint32(
		(in[10]-in[9])>>8 |
			((in[11] - in[10]) << 4) |
			((in[12] - in[11]) << 16) |
			((in[13] - in[12]) << 28))
	out[5] = uint32(
		(in[13]-in[12])>>4 |
			((in[14] - in[13]) << 8) |
			((in[15] - in[14]) << 20))
	out[6] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 12) |
			((in[18] - in[17]) << 24))
	out[7] = uint32(
		(in[18]-in[17])>>8 |
			((in[19] - in[18]) << 4) |
			((in[20] - in[19]) << 16) |
			((in[21] - in[20]) << 28))
	out[8] = uint32(
		(in[21]-in[20])>>4 |
			((in[22] - in[21]) << 8) |
			((in[23] - in[22]) << 20))
	out[9] = uint32(
		in[24] - in[23] |
			((in[25] - in[24]) << 12) |
			((in[26] - in[25]) << 24))
	out[10] = uint32(
		(in[26]-in[25])>>8 |
			((in[27] - in[26]) << 4) |
			((in[28] - in[27]) << 16) |
			((in[29] - in[28]) << 28))
	out[11] = uint32(
		(in[29]-in[28])>>4 |
			((in[30] - in[29]) << 8) |
			((in[31] - in[30]) << 20))
}

func deltapack32_13[T uint32 | int32](initoffset T, in *[32]T, out *[13]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 13) |
			((in[2] - in[1]) << 26))
	out[1] = uint32(
		(in[2]-in[1])>>6 |
			((in[3] - in[2]) << 7) |
			((in[4] - in[3]) << 20))
	out[2] = uint32(
		(in[4]-in[3])>>12 |
			((in[5] - in[4]) << 1) |
			((in[6] - in[5]) << 14) |
			((in[7] - in[6]) << 27))
	out[3] = uint32(
		(in[7]-in[6])>>5 |
			((in[8] - in[7]) << 8) |
			((in[9] - in[8]) << 21))
	out[4] = uint32(
		(in[9]-in[8])>>11 |
			((in[10] - in[9]) << 2) |
			((in[11] - in[10]) << 15) |
			((in[12] - in[11]) << 28))
	out[5] = uint32(
		(in[12]-in[11])>>4 |
			((in[13] - in[12]) << 9) |
			((in[14] - in[13]) << 22))
	out[6] = uint32(
		(in[14]-in[13])>>10 |
			((in[15] - in[14]) << 3) |
			((in[16] - in[15]) << 16) |
			((in[17] - in[16]) << 29))
	out[7] = uint32(
		(in[17]-in[16])>>3 |
			((in[18] - in[17]) << 10) |
			((in[19] - in[18]) << 23))
	out[8] = uint32(
		(in[19]-in[18])>>9 |
			((in[20] - in[19]) << 4) |
			((in[21] - in[20]) << 17) |
			((in[22] - in[21]) << 30))
	out[9] = uint32(
		(in[22]-in[21])>>2 |
			((in[23] - in[22]) << 11) |
			((in[24] - in[23]) << 24))
	out[10] = uint32(
		(in[24]-in[23])>>8 |
			((in[25] - in[24]) << 5) |
			((in[26] - in[25]) << 18) |
			((in[27] - in[26]) << 31))
	out[11] = uint32(
		(in[27]-in[26])>>1 |
			((in[28] - in[27]) << 12) |
			((in[29] - in[28]) << 25))
	out[12] = uint32(
		(in[29]-in[28])>>7 |
			((in[30] - in[29]) << 6) |
			((in[31] - in[30]) << 19))
}

func deltapack32_14[T uint32 | int32](initoffset T, in *[32]T, out *[14]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 14) |
			((in[2] - in[1]) << 28))
	out[1] = uint32(
		(in[2]-in[1])>>4 |
			((in[3] - in[2]) << 10) |
			((in[4] - in[3]) << 24))
	out[2] = uint32(
		(in[4]-in[3])>>8 |
			((in[5] - in[4]) << 6) |
			((in[6] - in[5]) << 20))
	out[3] = uint32(
		(in[6]-in[5])>>12 |
			((in[7] - in[6]) << 2) |
			((in[8] - in[7]) << 16) |
			((in[9] - in[8]) << 30))
	out[4] = uint32(
		(in[9]-in[8])>>2 |
			((in[10] - in[9]) << 12) |
			((in[11] - in[10]) << 26))
	out[5] = uint32(
		(in[11]-in[10])>>6 |
			((in[12] - in[11]) << 8) |
			((in[13] - in[12]) << 22))
	out[6] = uint32(
		(in[13]-in[12])>>10 |
			((in[14] - in[13]) << 4) |
			((in[15] - in[14]) << 18))
	out[7] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 14) |
			((in[18] - in[17]) << 28))
	out[8] = uint32(
		(in[18]-in[17])>>4 |
			((in[19] - in[18]) << 10) |
			((in[20] - in[19]) << 24))
	out[9] = uint32(
		(in[20]-in[19])>>8 |
			((in[21] - in[20]) << 6) |
			((in[22] - in[21]) << 20))
	out[10] = uint32(
		(in[22]-in[21])>>12 |
			((in[23] - in[22]) << 2) |
			((in[24] - in[23]) << 16) |
			((in[25] - in[24]) << 30))
	out[11] = uint32(
		(in[25]-in[24])>>2 |
			((in[26] - in[25]) << 12) |
			((in[27] - in[26]) << 26))
	out[12] = uint32(
		(in[27]-in[26])>>6 |
			((in[28] - in[27]) << 8) |
			((in[29] - in[28]) << 22))
	out[13] = uint32(
		(in[29]-in[28])>>10 |
			((in[30] - in[29]) << 4) |
			((in[31] - in[30]) << 18))
}

func deltapack32_15[T uint32 | int32](initoffset T, in *[32]T, out *[15]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 15) |
			((in[2] - in[1]) << 30))
	out[1] = uint32(
		(in[2]-in[1])>>2 |
			((in[3] - in[2]) << 13) |
			((in[4] - in[3]) << 28))
	out[2] = uint32(
		(in[4]-in[3])>>4 |
			((in[5] - in[4]) << 11) |
			((in[6] - in[5]) << 26))
	out[3] = uint32(
		(in[6]-in[5])>>6 |
			((in[7] - in[6]) << 9) |
			((in[8] - in[7]) << 24))
	out[4] = uint32(
		(in[8]-in[7])>>8 |
			((in[9] - in[8]) << 7) |
			((in[10] - in[9]) << 22))
	out[5] = uint32(
		(in[10]-in[9])>>10 |
			((in[11] - in[10]) << 5) |
			((in[12] - in[11]) << 20))
	out[6] = uint32(
		(in[12]-in[11])>>12 |
			((in[13] - in[12]) << 3) |
			((in[14] - in[13]) << 18))
	out[7] = uint32(
		(in[14]-in[13])>>14 |
			((in[15] - in[14]) << 1) |
			((in[16] - in[15]) << 16) |
			((in[17] - in[16]) << 31))
	out[8] = uint32(
		(in[17]-in[16])>>1 |
			((in[18] - in[17]) << 14) |
			((in[19] - in[18]) << 29))
	out[9] = uint32(
		(in[19]-in[18])>>3 |
			((in[20] - in[19]) << 12) |
			((in[21] - in[20]) << 27))
	out[10] = uint32(
		(in[21]-in[20])>>5 |
			((in[22] - in[21]) << 10) |
			((in[23] - in[22]) << 25))
	out[11] = uint32(
		(in[23]-in[22])>>7 |
			((in[24] - in[23]) << 8) |
			((in[25] - in[24]) << 23))
	out[12] = uint32(
		(in[25]-in[24])>>9 |
			((in[26] - in[25]) << 6) |
			((in[27] - in[26]) << 21))
	out[13] = uint32(
		(in[27]-in[26])>>11 |
			((in[28] - in[27]) << 4) |
			((in[29] - in[28]) << 19))
	out[14] = uint32(
		(in[29]-in[28])>>13 |
			((in[30] - in[29]) << 2) |
			((in[31] - in[30]) << 17))
}

func deltapack32_16[T uint32 | int32](initoffset T, in *[32]T, out *[16]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 16))
	out[1] = uint32(
		in[2] - in[1] |
			((in[3] - in[2]) << 16))
	out[2] = uint32(
		in[4] - in[3] |
			((in[5] - in[4]) << 16))
	out[3] = uint32(
		in[6] - in[5] |
			((in[7] - in[6]) << 16))
	out[4] = uint32(
		in[8] - in[7] |
			((in[9] - in[8]) << 16))
	out[5] = uint32(
		in[10] - in[9] |
			((in[11] - in[10]) << 16))
	out[6] = uint32(
		in[12] - in[11] |
			((in[13] - in[12]) << 16))
	out[7] = uint32(
		in[14] - in[13] |
			((in[15] - in[14]) << 16))
	out[8] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 16))
	out[9] = uint32(
		in[18] - in[17] |
			((in[19] - in[18]) << 16))
	out[10] = uint32(
		in[20] - in[19] |
			((in[21] - in[20]) << 16))
	out[11] = uint32(
		in[22] - in[21] |
			((in[23] - in[22]) << 16))
	out[12] = uint32(
		in[24] - in[23] |
			((in[25] - in[24]) << 16))
	out[13] = uint32(
		in[26] - in[25] |
			((in[27] - in[26]) << 16))
	out[14] = uint32(
		in[28] - in[27] |
			((in[29] - in[28]) << 16))
	out[15] = uint32(
		in[30] - in[29] |
			((in[31] - in[30]) << 16))
}

func deltapack32_17[T uint32 | int32](initoffset T, in *[32]T, out *[17]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 17))
	out[1] = uint32(
		(in[1]-in[0])>>15 |
			((in[2] - in[1]) << 2) |
			((in[3] - in[2]) << 19))
	out[2] = uint32(
		(in[3]-in[2])>>13 |
			((in[4] - in[3]) << 4) |
			((in[5] - in[4]) << 21))
	out[3] = uint32(
		(in[5]-in[4])>>11 |
			((in[6] - in[5]) << 6) |
			((in[7] - in[6]) << 23))
	out[4] = uint32(
		(in[7]-in[6])>>9 |
			((in[8] - in[7]) << 8) |
			((in[9] - in[8]) << 25))
	out[5] = uint32(
		(in[9]-in[8])>>7 |
			((in[10] - in[9]) << 10) |
			((in[11] - in[10]) << 27))
	out[6] = uint32(
		(in[11]-in[10])>>5 |
			((in[12] - in[11]) << 12) |
			((in[13] - in[12]) << 29))
	out[7] = uint32(
		(in[13]-in[12])>>3 |
			((in[14] - in[13]) << 14) |
			((in[15] - in[14]) << 31))
	out[8] = uint32(
		(in[15]-in[14])>>1 |
			((in[16] - in[15]) << 16))
	out[9] = uint32(
		(in[16]-in[15])>>16 |
			((in[17] - in[16]) << 1) |
			((in[18] - in[17]) << 18))
	out[10] = uint32(
		(in[18]-in[17])>>14 |
			((in[19] - in[18]) << 3) |
			((in[20] - in[19]) << 20))
	out[11] = uint32(
		(in[20]-in[19])>>12 |
			((in[21] - in[20]) << 5) |
			((in[22] - in[21]) << 22))
	out[12] = uint32(
		(in[22]-in[21])>>10 |
			((in[23] - in[22]) << 7) |
			((in[24] - in[23]) << 24))
	out[13] = uint32(
		(in[24]-in[23])>>8 |
			((in[25] - in[24]) << 9) |
			((in[26] - in[25]) << 26))
	out[14] = uint32(
		(in[26]-in[25])>>6 |
			((in[27] - in[26]) << 11) |
			((in[28] - in[27]) << 28))
	out[15] = uint32(
		(in[28]-in[27])>>4 |
			((in[29] - in[28]) << 13) |
			((in[30] - in[29]) << 30))
	out[16] = uint32(
		(in[30]-in[29])>>2 |
			((in[31] - in[30]) << 15))
}

func deltapack32_18[T uint32 | int32](initoffset T, in *[32]T, out *[18]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 18))
	out[1] = uint32(
		(in[1]-in[0])>>14 |
			((in[2] - in[1]) << 4) |
			((in[3] - in[2]) << 22))
	out[2] = uint32(
		(in[3]-in[2])>>10 |
			((in[4] - in[3]) << 8) |
			((in[5] - in[4]) << 26))
	out[3] = uint32(
		(in[5]-in[4])>>6 |
			((in[6] - in[5]) << 12) |
			((in[7] - in[6]) << 30))
	out[4] = uint32(
		(in[7]-in[6])>>2 |
			((in[8] - in[7]) << 16))
	out[5] = uint32(
		(in[8]-in[7])>>16 |
			((in[9] - in[8]) << 2) |
			((in[10] - in[9]) << 20))
	out[6] = uint32(
		(in[10]-in[9])>>12 |
			((in[11] - in[10]) << 6) |
			((in[12] - in[11]) << 24))
	out[7] = uint32(
		(in[12]-in[11])>>8 |
			((in[13] - in[12]) << 10) |
			((in[14] - in[13]) << 28))
	out[8] = uint32(
		(in[14]-in[13])>>4 |
			((in[15] - in[14]) << 14))
	out[9] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 18))
	out[10] = uint32(
		(in[17]-in[16])>>14 |
			((in[18] - in[17]) << 4) |
			((in[19] - in[18]) << 22))
	out[11] = uint32(
		(in[19]-in[18])>>10 |
			((in[20] - in[19]) << 8) |
			((in[21] - in[20]) << 26))
	out[12] = uint32(
		(in[21]-in[20])>>6 |
			((in[22] - in[21]) << 12) |
			((in[23] - in[22]) << 30))
	out[13] = uint32(
		(in[23]-in[22])>>2 |
			((in[24] - in[23]) << 16))
	out[14] = uint32(
		(in[24]-in[23])>>16 |
			((in[25] - in[24]) << 2) |
			((in[26] - in[25]) << 20))
	out[15] = uint32(
		(in[26]-in[25])>>12 |
			((in[27] - in[26]) << 6) |
			((in[28] - in[27]) << 24))
	out[16] = uint32(
		(in[28]-in[27])>>8 |
			((in[29] - in[28]) << 10) |
			((in[30] - in[29]) << 28))
	out[17] = uint32(
		(in[30]-in[29])>>4 |
			((in[31] - in[30]) << 14))
}

func deltapack32_19[T uint32 | int32](initoffset T, in *[32]T, out *[19]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 19))
	out[1] = uint32(
		(in[1]-in[0])>>13 |
			((in[2] - in[1]) << 6) |
			((in[3] - in[2]) << 25))
	out[2] = uint32(
		(in[3]-in[2])>>7 |
			((in[4] - in[3]) << 12) |
			((in[5] - in[4]) << 31))
	out[3] = uint32(
		(in[5]-in[4])>>1 |
			((in[6] - in[5]) << 18))
	out[4] = uint32(
		(in[6]-in[5])>>14 |
			((in[7] - in[6]) << 5) |
			((in[8] - in[7]) << 24))
	out[5] = uint32(
		(in[8]-in[7])>>8 |
			((in[9] - in[8]) << 11) |
			((in[10] - in[9]) << 30))
	out[6] = uint32(
		(in[10]-in[9])>>2 |
			((in[11] - in[10]) << 17))
	out[7] = uint32(
		(in[11]-in[10])>>15 |
			((in[12] - in[11]) << 4) |
			((in[13] - in[12]) << 23))
	out[8] = uint32(
		(in[13]-in[12])>>9 |
			((in[14] - in[13]) << 10) |
			((in[15] - in[14]) << 29))
	out[9] = uint32(
		(in[15]-in[14])>>3 |
			((in[16] - in[15]) << 16))
	out[10] = uint32(
		(in[16]-in[15])>>16 |
			((in[17] - in[16]) << 3) |
			((in[18] - in[17]) << 22))
	out[11] = uint32(
		(in[18]-in[17])>>10 |
			((in[19] - in[18]) << 9) |
			((in[20] - in[19]) << 28))
	out[12] = uint32(
		(in[20]-in[19])>>4 |
			((in[21] - in[20]) << 15))
	out[13] = uint32(
		(in[21]-in[20])>>17 |
			((in[22] - in[21]) << 2) |
			((in[23] - in[22]) << 21))
	out[14] = uint32(
		(in[23]-in[22])>>11 |
			((in[24] - in[23]) << 8) |
			((in[25] - in[24]) << 27))
	out[15] = uint32(
		(in[25]-in[24])>>5 |
			((in[26] - in[25]) << 14))
	out[16] = uint32(
		(in[26]-in[25])>>18 |
			((in[27] - in[26]) << 1) |
			((in[28] - in[27]) << 20))
	out[17] = uint32(
		(in[28]-in[27])>>12 |
			((in[29] - in[28]) << 7) |
			((in[30] - in[29]) << 26))
	out[18] = uint32(
		(in[30]-in[29])>>6 |
			((in[31] - in[30]) << 13))
}

func deltapack32_20[T uint32 | int32](initoffset T, in *[32]T, out *[20]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 20))
	out[1] = uint32(
		(in[1]-in[0])>>12 |
			((in[2] - in[1]) << 8) |
			((in[3] - in[2]) << 28))
	out[2] = uint32(
		(in[3]-in[2])>>4 |
			((in[4] - in[3]) << 16))
	out[3] = uint32(
		(in[4]-in[3])>>16 |
			((in[5] - in[4]) << 4) |
			((in[6] - in[5]) << 24))
	out[4] = uint32(
		(in[6]-in[5])>>8 |
			((in[7] - in[6]) << 12))
	out[5] = uint32(
		in[8] - in[7] |
			((in[9] - in[8]) << 20))
	out[6] = uint32(
		(in[9]-in[8])>>12 |
			((in[10] - in[9]) << 8) |
			((in[11] - in[10]) << 28))
	out[7] = uint32(
		(in[11]-in[10])>>4 |
			((in[12] - in[11]) << 16))
	out[8] = uint32(
		(in[12]-in[11])>>16 |
			((in[13] - in[12]) << 4) |
			((in[14] - in[13]) << 24))
	out[9] = uint32(
		(in[14]-in[13])>>8 |
			((in[15] - in[14]) << 12))
	out[10] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 20))
	out[11] = uint32(
		(in[17]-in[16])>>12 |
			((in[18] - in[17]) << 8) |
			((in[19] - in[18]) << 28))
	out[12] = uint32(
		(in[19]-in[18])>>4 |
			((in[20] - in[19]) << 16))
	out[13] = uint32(
		(in[20]-in[19])>>16 |
			((in[21] - in[20]) << 4) |
			((in[22] - in[21]) << 24))
	out[14] = uint32(
		(in[22]-in[21])>>8 |
			((in[23] - in[22]) << 12))
	out[15] = uint32(
		in[24] - in[23] |
			((in[25] - in[24]) << 20))
	out[16] = uint32(
		(in[25]-in[24])>>12 |
			((in[26] - in[25]) << 8) |
			((in[27] - in[26]) << 28))
	out[17] = uint32(
		(in[27]-in[26])>>4 |
			((in[28] - in[27]) << 16))
	out[18] = uint32(
		(in[28]-in[27])>>16 |
			((in[29] - in[28]) << 4) |
			((in[30] - in[29]) << 24))
	out[19] = uint32(
		(in[30]-in[29])>>8 |
			((in[31] - in[30]) << 12))
}

func deltapack32_21[T uint32 | int32](initoffset T, in *[32]T, out *[21]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 21))
	out[1] = uint32(
		(in[1]-in[0])>>11 |
			((in[2] - in[1]) << 10) |
			((in[3] - in[2]) << 31))
	out[2] = uint32(
		(in[3]-in[2])>>1 |
			((in[4] - in[3]) << 20))
	out[3] = uint32(
		(in[4]-in[3])>>12 |
			((in[5] - in[4]) << 9) |
			((in[6] - in[5]) << 30))
	out[4] = uint32(
		(in[6]-in[5])>>2 |
			((in[7] - in[6]) << 19))
	out[5] = uint32(
		(in[7]-in[6])>>13 |
			((in[8] - in[7]) << 8) |
			((in[9] - in[8]) << 29))
	out[6] = uint32(
		(in[9]-in[8])>>3 |
			((in[10] - in[9]) << 18))
	out[7] = uint32(
		(in[10]-in[9])>>14 |
			((in[11] - in[10]) << 7) |
			((in[12] - in[11]) << 28))
	out[8] = uint32(
		(in[12]-in[11])>>4 |
			((in[13] - in[12]) << 17))
	out[9] = uint32(
		(in[13]-in[12])>>15 |
			((in[14] - in[13]) << 6) |
			((in[15] - in[14]) << 27))
	out[10] = uint32(
		(in[15]-in[14])>>5 |
			((in[16] - in[15]) << 16))
	out[11] = uint32(
		(in[16]-in[15])>>16 |
			((in[17] - in[16]) << 5) |
			((in[18] - in[17]) << 26))
	out[12] = uint32(
		(in[18]-in[17])>>6 |
			((in[19] - in[18]) << 15))
	out[13] = uint32(
		(in[19]-in[18])>>17 |
			((in[20] - in[19]) << 4) |
			((in[21] - in[20]) << 25))
	out[14] = uint32(
		(in[21]-in[20])>>7 |
			((in[22] - in[21]) << 14))
	out[15] = uint32(
		(in[22]-in[21])>>18 |
			((in[23] - in[22]) << 3) |
			((in[24] - in[23]) << 24))
	out[16] = uint32(
		(in[24]-in[23])>>8 |
			((in[25] - in[24]) << 13))
	out[17] = uint32(
		(in[25]-in[24])>>19 |
			((in[26] - in[25]) << 2) |
			((in[27] - in[26]) << 23))
	out[18] = uint32(
		(in[27]-in[26])>>9 |
			((in[28] - in[27]) << 12))
	out[19] = uint32(
		(in[28]-in[27])>>20 |
			((in[29] - in[28]) << 1) |
			((in[30] - in[29]) << 22))
	out[20] = uint32(
		(in[30]-in[29])>>10 |
			((in[31] - in[30]) << 11))
}

func deltapack32_22[T uint32 | int32](initoffset T, in *[32]T, out *[22]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 22))
	out[1] = uint32(
		(in[1]-in[0])>>10 |
			((in[2] - in[1]) << 12))
	out[2] = uint32(
		(in[2]-in[1])>>20 |
			((in[3] - in[2]) << 2) |
			((in[4] - in[3]) << 24))
	out[3] = uint32(
		(in[4]-in[3])>>8 |
			((in[5] - in[4]) << 14))
	out[4] = uint32(
		(in[5]-in[4])>>18 |
			((in[6] - in[5]) << 4) |
			((in[7] - in[6]) << 26))
	out[5] = uint32(
		(in[7]-in[6])>>6 |
			((in[8] - in[7]) << 16))
	out[6] = uint32(
		(in[8]-in[7])>>16 |
			((in[9] - in[8]) << 6) |
			((in[10] - in[9]) << 28))
	out[7] = uint32(
		(in[10]-in[9])>>4 |
			((in[11] - in[10]) << 18))
	out[8] = uint32(
		(in[11]-in[10])>>14 |
			((in[12] - in[11]) << 8) |
			((in[13] - in[12]) << 30))
	out[9] = uint32(
		(in[13]-in[12])>>2 |
			((in[14] - in[13]) << 20))
	out[10] = uint32(
		(in[14]-in[13])>>12 |
			((in[15] - in[14]) << 10))
	out[11] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 22))
	out[12] = uint32(
		(in[17]-in[16])>>10 |
			((in[18] - in[17]) << 12))
	out[13] = uint32(
		(in[18]-in[17])>>20 |
			((in[19] - in[18]) << 2) |
			((in[20] - in[19]) << 24))
	out[14] = uint32(
		(in[20]-in[19])>>8 |
			((in[21] - in[20]) << 14))
	out[15] = uint32(
		(in[21]-in[20])>>18 |
			((in[22] - in[21]) << 4) |
			((in[23] - in[22]) << 26))
	out[16] = uint32(
		(in[23]-in[22])>>6 |
			((in[24] - in[23]) << 16))
	out[17] = uint32(
		(in[24]-in[23])>>16 |
			((in[25] - in[24]) << 6) |
			((in[26] - in[25]) << 28))
	out[18] = uint32(
		(in[26]-in[25])>>4 |
			((in[27] - in[26]) << 18))
	out[19] = uint32(
		(in[27]-in[26])>>14 |
			((in[28] - in[27]) << 8) |
			((in[29] - in[28]) << 30))
	out[20] = uint32(
		(in[29]-in[28])>>2 |
			((in[30] - in[29]) << 20))
	out[21] = uint32(
		(in[30]-in[29])>>12 |
			((in[31] - in[30]) << 10))
}

func deltapack32_23[T uint32 | int32](initoffset T, in *[32]T, out *[23]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 23))
	out[1] = uint32(
		(in[1]-in[0])>>9 |
			((in[2] - in[1]) << 14))
	out[2] = uint32(
		(in[2]-in[1])>>18 |
			((in[3] - in[2]) << 5) |
			((in[4] - in[3]) << 28))
	out[3] = uint32(
		(in[4]-in[3])>>4 |
			((in[5] - in[4]) << 19))
	out[4] = uint32(
		(in[5]-in[4])>>13 |
			((in[6] - in[5]) << 10))
	out[5] = uint32(
		(in[6]-in[5])>>22 |
			((in[7] - in[6]) << 1) |
			((in[8] - in[7]) << 24))
	out[6] = uint32(
		(in[8]-in[7])>>8 |
			((in[9] - in[8]) << 15))
	out[7] = uint32(
		(in[9]-in[8])>>17 |
			((in[10] - in[9]) << 6) |
			((in[11] - in[10]) << 29))
	out[8] = uint32(
		(in[11]-in[10])>>3 |
			((in[12] - in[11]) << 20))
	out[9] = uint32(
		(in[12]-in[11])>>12 |
			((in[13] - in[12]) << 11))
	out[10] = uint32(
		(in[13]-in[12])>>21 |
			((in[14] - in[13]) << 2) |
			((in[15] - in[14]) << 25))
	out[11] = uint32(
		(in[15]-in[14])>>7 |
			((in[16] - in[15]) << 16))
	out[12] = uint32(
		(in[16]-in[15])>>16 |
			((in[17] - in[16]) << 7) |
			((in[18] - in[17]) << 30))
	out[13] = uint32(
		(in[18]-in[17])>>2 |
			((in[19] - in[18]) << 21))
	out[14] = uint32(
		(in[19]-in[18])>>11 |
			((in[20] - in[19]) << 12))
	out[15] = uint32(
		(in[20]-in[19])>>20 |
			((in[21] - in[20]) << 3) |
			((in[22] - in[21]) << 26))
	out[16] = uint32(
		(in[22]-in[21])>>6 |
			((in[23] - in[22]) << 17))
	out[17] = uint32(
		(in[23]-in[22])>>15 |
			((in[24] - in[23]) << 8) |
			((in[25] - in[24]) << 31))
	out[18] = uint32(
		(in[25]-in[24])>>1 |
			((in[26] - in[25]) << 22))
	out[19] = uint32(
		(in[26]-in[25])>>10 |
			((in[27] - in[26]) << 13))
	out[20] = uint32(
		(in[27]-in[26])>>19 |
			((in[28] - in[27]) << 4) |
			((in[29] - in[28]) << 27))
	out[21] = uint32(
		(in[29]-in[28])>>5 |
			((in[30] - in[29]) << 18))
	out[22] = uint32(
		(in[30]-in[29])>>14 |
			((in[31] - in[30]) << 9))
}

func deltapack32_24[T uint32 | int32](initoffset T, in *[32]T, out *[24]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 24))
	out[1] = uint32(
		(in[1]-in[0])>>8 |
			((in[2] - in[1]) << 16))
	out[2] = uint32(
		(in[2]-in[1])>>16 |
			((in[3] - in[2]) << 8))
	out[3] = uint32(
		in[4] - in[3] |
			((in[5] - in[4]) << 24))
	out[4] = uint32(
		(in[5]-in[4])>>8 |
			((in[6] - in[5]) << 16))
	out[5] = uint32(
		(in[6]-in[5])>>16 |
			((in[7] - in[6]) << 8))
	out[6] = uint32(
		in[8] - in[7] |
			((in[9] - in[8]) << 24))
	out[7] = uint32(
		(in[9]-in[8])>>8 |
			((in[10] - in[9]) << 16))
	out[8] = uint32(
		(in[10]-in[9])>>16 |
			((in[11] - in[10]) << 8))
	out[9] = uint32(
		in[12] - in[11] |
			((in[13] - in[12]) << 24))
	out[10] = uint32(
		(in[13]-in[12])>>8 |
			((in[14] - in[13]) << 16))
	out[11] = uint32(
		(in[14]-in[13])>>16 |
			((in[15] - in[14]) << 8))
	out[12] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 24))
	out[13] = uint32(
		(in[17]-in[16])>>8 |
			((in[18] - in[17]) << 16))
	out[14] = uint32(
		(in[18]-in[17])>>16 |
			((in[19] - in[18]) << 8))
	out[15] = uint32(
		in[20] - in[19] |
			((in[21] - in[20]) << 24))
	out[16] = uint32(
		(in[21]-in[20])>>8 |
			((in[22] - in[21]) << 16))
	out[17] = uint32(
		(in[22]-in[21])>>16 |
			((in[23] - in[22]) << 8))
	out[18] = uint32(
		in[24] - in[23] |
			((in[25] - in[24]) << 24))
	out[19] = uint32(
		(in[25]-in[24])>>8 |
			((in[26] - in[25]) << 16))
	out[20] = uint32(
		(in[26]-in[25])>>16 |
			((in[27] - in[26]) << 8))
	out[21] = uint32(
		in[28] - in[27] |
			((in[29] - in[28]) << 24))
	out[22] = uint32(
		(in[29]-in[28])>>8 |
			((in[30] - in[29]) << 16))
	out[23] = uint32(
		(in[30]-in[29])>>16 |
			((in[31] - in[30]) << 8))
}

func deltapack32_25[T uint32 | int32](initoffset T, in *[32]T, out *[25]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 25))
	out[1] = uint32(
		(in[1]-in[0])>>7 |
			((in[2] - in[1]) << 18))
	out[2] = uint32(
		(in[2]-in[1])>>14 |
			((in[3] - in[2]) << 11))
	out[3] = uint32(
		(in[3]-in[2])>>21 |
			((in[4] - in[3]) << 4) |
			((in[5] - in[4]) << 29))
	out[4] = uint32(
		(in[5]-in[4])>>3 |
			((in[6] - in[5]) << 22))
	out[5] = uint32(
		(in[6]-in[5])>>10 |
			((in[7] - in[6]) << 15))
	out[6] = uint32(
		(in[7]-in[6])>>17 |
			((in[8] - in[7]) << 8))
	out[7] = uint32(
		(in[8]-in[7])>>24 |
			((in[9] - in[8]) << 1) |
			((in[10] - in[9]) << 26))
	out[8] = uint32(
		(in[10]-in[9])>>6 |
			((in[11] - in[10]) << 19))
	out[9] = uint32(
		(in[11]-in[10])>>13 |
			((in[12] - in[11]) << 12))
	out[10] = uint32(
		(in[12]-in[11])>>20 |
			((in[13] - in[12]) << 5) |
			((in[14] - in[13]) << 30))
	out[11] = uint32(
		(in[14]-in[13])>>2 |
			((in[15] - in[14]) << 23))
	out[12] = uint32(
		(in[15]-in[14])>>9 |
			((in[16] - in[15]) << 16))
	out[13] = uint32(
		(in[16]-in[15])>>16 |
			((in[17] - in[16]) << 9))
	out[14] = uint32(
		(in[17]-in[16])>>23 |
			((in[18] - in[17]) << 2) |
			((in[19] - in[18]) << 27))
	out[15] = uint32(
		(in[19]-in[18])>>5 |
			((in[20] - in[19]) << 20))
	out[16] = uint32(
		(in[20]-in[19])>>12 |
			((in[21] - in[20]) << 13))
	out[17] = uint32(
		(in[21]-in[20])>>19 |
			((in[22] - in[21]) << 6) |
			((in[23] - in[22]) << 31))
	out[18] = uint32(
		(in[23]-in[22])>>1 |
			((in[24] - in[23]) << 24))
	out[19] = uint32(
		(in[24]-in[23])>>8 |
			((in[25] - in[24]) << 17))
	out[20] = uint32(
		(in[25]-in[24])>>15 |
			((in[26] - in[25]) << 10))
	out[21] = uint32(
		(in[26]-in[25])>>22 |
			((in[27] - in[26]) << 3) |
			((in[28] - in[27]) << 28))
	out[22] = uint32(
		(in[28]-in[27])>>4 |
			((in[29] - in[28]) << 21))
	out[23] = uint32(
		(in[29]-in[28])>>11 |
			((in[30] - in[29]) << 14))
	out[24] = uint32(
		(in[30]-in[29])>>18 |
			((in[31] - in[30]) << 7))
}

func deltapack32_26[T uint32 | int32](initoffset T, in *[32]T, out *[26]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 26))
	out[1] = uint32(
		(in[1]-in[0])>>6 |
			((in[2] - in[1]) << 20))
	out[2] = uint32(
		(in[2]-in[1])>>12 |
			((in[3] - in[2]) << 14))
	out[3] = uint32(
		(in[3]-in[2])>>18 |
			((in[4] - in[3]) << 8))
	out[4] = uint32(
		(in[4]-in[3])>>24 |
			((in[5] - in[4]) << 2) |
			((in[6] - in[5]) << 28))
	out[5] = uint32(
		(in[6]-in[5])>>4 |
			((in[7] - in[6]) << 22))
	out[6] = uint32(
		(in[7]-in[6])>>10 |
			((in[8] - in[7]) << 16))
	out[7] = uint32(
		(in[8]-in[7])>>16 |
			((in[9] - in[8]) << 10))
	out[8] = uint32(
		(in[9]-in[8])>>22 |
			((in[10] - in[9]) << 4) |
			((in[11] - in[10]) << 30))
	out[9] = uint32(
		(in[11]-in[10])>>2 |
			((in[12] - in[11]) << 24))
	out[10] = uint32(
		(in[12]-in[11])>>8 |
			((in[13] - in[12]) << 18))
	out[11] = uint32(
		(in[13]-in[12])>>14 |
			((in[14] - in[13]) << 12))
	out[12] = uint32(
		(in[14]-in[13])>>20 |
			((in[15] - in[14]) << 6))
	out[13] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 26))
	out[14] = uint32(
		(in[17]-in[16])>>6 |
			((in[18] - in[17]) << 20))
	out[15] = uint32(
		(in[18]-in[17])>>12 |
			((in[19] - in[18]) << 14))
	out[16] = uint32(
		(in[19]-in[18])>>18 |
			((in[20] - in[19]) << 8))
	out[17] = uint32(
		(in[20]-in[19])>>24 |
			((in[21] - in[20]) << 2) |
			((in[22] - in[21]) << 28))
	out[18] = uint32(
		(in[22]-in[21])>>4 |
			((in[23] - in[22]) << 22))
	out[19] = uint32(
		(in[23]-in[22])>>10 |
			((in[24] - in[23]) << 16))
	out[20] = uint32(
		(in[24]-in[23])>>16 |
			((in[25] - in[24]) << 10))
	out[21] = uint32(
		(in[25]-in[24])>>22 |
			((in[26] - in[25]) << 4) |
			((in[27] - in[26]) << 30))
	out[22] = uint32(
		(in[27]-in[26])>>2 |
			((in[28] - in[27]) << 24))
	out[23] = uint32(
		(in[28]-in[27])>>8 |
			((in[29] - in[28]) << 18))
	out[24] = uint32(
		(in[29]-in[28])>>14 |
			((in[30] - in[29]) << 12))
	out[25] = uint32(
		(in[30]-in[29])>>20 |
			((in[31] - in[30]) << 6))
}

func deltapack32_27[T uint32 | int32](initoffset T, in *[32]T, out *[27]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 27))
	out[1] = uint32(
		(in[1]-in[0])>>5 |
			((in[2] - in[1]) << 22))
	out[2] = uint32(
		(in[2]-in[1])>>10 |
			((in[3] - in[2]) << 17))
	out[3] = uint32(
		(in[3]-in[2])>>15 |
			((in[4] - in[3]) << 12))
	out[4] = uint32(
		(in[4]-in[3])>>20 |
			((in[5] - in[4]) << 7))
	out[5] = uint32(
		(in[5]-in[4])>>25 |
			((in[6] - in[5]) << 2) |
			((in[7] - in[6]) << 29))
	out[6] = uint32(
		(in[7]-in[6])>>3 |
			((in[8] - in[7]) << 24))
	out[7] = uint32(
		(in[8]-in[7])>>8 |
			((in[9] - in[8]) << 19))
	out[8] = uint32(
		(in[9]-in[8])>>13 |
			((in[10] - in[9]) << 14))
	out[9] = uint32(
		(in[10]-in[9])>>18 |
			((in[11] - in[10]) << 9))
	out[10] = uint32(
		(in[11]-in[10])>>23 |
			((in[12] - in[11]) << 4) |
			((in[13] - in[12]) << 31))
	out[11] = uint32(
		(in[13]-in[12])>>1 |
			((in[14] - in[13]) << 26))
	out[12] = uint32(
		(in[14]-in[13])>>6 |
			((in[15] - in[14]) << 21))
	out[13] = uint32(
		(in[15]-in[14])>>11 |
			((in[16] - in[15]) << 16))
	out[14] = uint32(
		(in[16]-in[15])>>16 |
			((in[17] - in[16]) << 11))
	out[15] = uint32(
		(in[17]-in[16])>>21 |
			((in[18] - in[17]) << 6))
	out[16] = uint32(
		(in[18]-in[17])>>26 |
			((in[19] - in[18]) << 1) |
			((in[20] - in[19]) << 28))
	out[17] = uint32(
		(in[20]-in[19])>>4 |
			((in[21] - in[20]) << 23))
	out[18] = uint32(
		(in[21]-in[20])>>9 |
			((in[22] - in[21]) << 18))
	out[19] = uint32(
		(in[22]-in[21])>>14 |
			((in[23] - in[22]) << 13))
	out[20] = uint32(
		(in[23]-in[22])>>19 |
			((in[24] - in[23]) << 8))
	out[21] = uint32(
		(in[24]-in[23])>>24 |
			((in[25] - in[24]) << 3) |
			((in[26] - in[25]) << 30))
	out[22] = uint32(
		(in[26]-in[25])>>2 |
			((in[27] - in[26]) << 25))
	out[23] = uint32(
		(in[27]-in[26])>>7 |
			((in[28] - in[27]) << 20))
	out[24] = uint32(
		(in[28]-in[27])>>12 |
			((in[29] - in[28]) << 15))
	out[25] = uint32(
		(in[29]-in[28])>>17 |
			((in[30] - in[29]) << 10))
	out[26] = uint32(
		(in[30]-in[29])>>22 |
			((in[31] - in[30]) << 5))
}

func deltapack32_28[T uint32 | int32](initoffset T, in *[32]T, out *[28]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 28))
	out[1] = uint32(
		(in[1]-in[0])>>4 |
			((in[2] - in[1]) << 24))
	out[2] = uint32(
		(in[2]-in[1])>>8 |
			((in[3] - in[2]) << 20))
	out[3] = uint32(
		(in[3]-in[2])>>12 |
			((in[4] - in[3]) << 16))
	out[4] = uint32(
		(in[4]-in[3])>>16 |
			((in[5] - in[4]) << 12))
	out[5] = uint32(
		(in[5]-in[4])>>20 |
			((in[6] - in[5]) << 8))
	out[6] = uint32(
		(in[6]-in[5])>>24 |
			((in[7] - in[6]) << 4))
	out[7] = uint32(
		in[8] - in[7] |
			((in[9] - in[8]) << 28))
	out[8] = uint32(
		(in[9]-in[8])>>4 |
			((in[10] - in[9]) << 24))
	out[9] = uint32(
		(in[10]-in[9])>>8 |
			((in[11] - in[10]) << 20))
	out[10] = uint32(
		(in[11]-in[10])>>12 |
			((in[12] - in[11]) << 16))
	out[11] = uint32(
		(in[12]-in[11])>>16 |
			((in[13] - in[12]) << 12))
	out[12] = uint32(
		(in[13]-in[12])>>20 |
			((in[14] - in[13]) << 8))
	out[13] = uint32(
		(in[14]-in[13])>>24 |
			((in[15] - in[14]) << 4))
	out[14] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 28))
	out[15] = uint32(
		(in[17]-in[16])>>4 |
			((in[18] - in[17]) << 24))
	out[16] = uint32(
		(in[18]-in[17])>>8 |
			((in[19] - in[18]) << 20))
	out[17] = uint32(
		(in[19]-in[18])>>12 |
			((in[20] - in[19]) << 16))
	out[18] = uint32(
		(in[20]-in[19])>>16 |
			((in[21] - in[20]) << 12))
	out[19] = uint32(
		(in[21]-in[20])>>20 |
			((in[22] - in[21]) << 8))
	out[20] = uint32(
		(in[22]-in[21])>>24 |
			((in[23] - in[22]) << 4))
	out[21] = uint32(
		in[24] - in[23] |
			((in[25] - in[24]) << 28))
	out[22] = uint32(
		(in[25]-in[24])>>4 |
			((in[26] - in[25]) << 24))
	out[23] = uint32(
		(in[26]-in[25])>>8 |
			((in[27] - in[26]) << 20))
	out[24] = uint32(
		(in[27]-in[26])>>12 |
			((in[28] - in[27]) << 16))
	out[25] = uint32(
		(in[28]-in[27])>>16 |
			((in[29] - in[28]) << 12))
	out[26] = uint32(
		(in[29]-in[28])>>20 |
			((in[30] - in[29]) << 8))
	out[27] = uint32(
		(in[30]-in[29])>>24 |
			((in[31] - in[30]) << 4))
}

func deltapack32_29[T uint32 | int32](initoffset T, in *[32]T, out *[29]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 29))
	out[1] = uint32(
		(in[1]-in[0])>>3 |
			((in[2] - in[1]) << 26))
	out[2] = uint32(
		(in[2]-in[1])>>6 |
			((in[3] - in[2]) << 23))
	out[3] = uint32(
		(in[3]-in[2])>>9 |
			((in[4] - in[3]) << 20))
	out[4] = uint32(
		(in[4]-in[3])>>12 |
			((in[5] - in[4]) << 17))
	out[5] = uint32(
		(in[5]-in[4])>>15 |
			((in[6] - in[5]) << 14))
	out[6] = uint32(
		(in[6]-in[5])>>18 |
			((in[7] - in[6]) << 11))
	out[7] = uint32(
		(in[7]-in[6])>>21 |
			((in[8] - in[7]) << 8))
	out[8] = uint32(
		(in[8]-in[7])>>24 |
			((in[9] - in[8]) << 5))
	out[9] = uint32(
		(in[9]-in[8])>>27 |
			((in[10] - in[9]) << 2) |
			((in[11] - in[10]) << 31))
	out[10] = uint32(
		(in[11]-in[10])>>1 |
			((in[12] - in[11]) << 28))
	out[11] = uint32(
		(in[12]-in[11])>>4 |
			((in[13] - in[12]) << 25))
	out[12] = uint32(
		(in[13]-in[12])>>7 |
			((in[14] - in[13]) << 22))
	out[13] = uint32(
		(in[14]-in[13])>>10 |
			((in[15] - in[14]) << 19))
	out[14] = uint32(
		(in[15]-in[14])>>13 |
			((in[16] - in[15]) << 16))
	out[15] = uint32(
		(in[16]-in[15])>>16 |
			((in[17] - in[16]) << 13))
	out[16] = uint32(
		(in[17]-in[16])>>19 |
			((in[18] - in[17]) << 10))
	out[17] = uint32(
		(in[18]-in[17])>>22 |
			((in[19] - in[18]) << 7))
	out[18] = uint32(
		(in[19]-in[18])>>25 |
			((in[20] - in[19]) << 4))
	out[19] = uint32(
		(in[20]-in[19])>>28 |
			((in[21] - in[20]) << 1) |
			((in[22] - in[21]) << 30))
	out[20] = uint32(
		(in[22]-in[21])>>2 |
			((in[23] - in[22]) << 27))
	out[21] = uint32(
		(in[23]-in[22])>>5 |
			((in[24] - in[23]) << 24))
	out[22] = uint32(
		(in[24]-in[23])>>8 |
			((in[25] - in[24]) << 21))
	out[23] = uint32(
		(in[25]-in[24])>>11 |
			((in[26] - in[25]) << 18))
	out[24] = uint32(
		(in[26]-in[25])>>14 |
			((in[27] - in[26]) << 15))
	out[25] = uint32(
		(in[27]-in[26])>>17 |
			((in[28] - in[27]) << 12))
	out[26] = uint32(
		(in[28]-in[27])>>20 |
			((in[29] - in[28]) << 9))
	out[27] = uint32(
		(in[29]-in[28])>>23 |
			((in[30] - in[29]) << 6))
	out[28] = uint32(
		(in[30]-in[29])>>26 |
			((in[31] - in[30]) << 3))
}

func deltapack32_30[T uint32 | int32](initoffset T, in *[32]T, out *[30]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 30))
	out[1] = uint32(
		(in[1]-in[0])>>2 |
			((in[2] - in[1]) << 28))
	out[2] = uint32(
		(in[2]-in[1])>>4 |
			((in[3] - in[2]) << 26))
	out[3] = uint32(
		(in[3]-in[2])>>6 |
			((in[4] - in[3]) << 24))
	out[4] = uint32(
		(in[4]-in[3])>>8 |
			((in[5] - in[4]) << 22))
	out[5] = uint32(
		(in[5]-in[4])>>10 |
			((in[6] - in[5]) << 20))
	out[6] = uint32(
		(in[6]-in[5])>>12 |
			((in[7] - in[6]) << 18))
	out[7] = uint32(
		(in[7]-in[6])>>14 |
			((in[8] - in[7]) << 16))
	out[8] = uint32(
		(in[8]-in[7])>>16 |
			((in[9] - in[8]) << 14))
	out[9] = uint32(
		(in[9]-in[8])>>18 |
			((in[10] - in[9]) << 12))
	out[10] = uint32(
		(in[10]-in[9])>>20 |
			((in[11] - in[10]) << 10))
	out[11] = uint32(
		(in[11]-in[10])>>22 |
			((in[12] - in[11]) << 8))
	out[12] = uint32(
		(in[12]-in[11])>>24 |
			((in[13] - in[12]) << 6))
	out[13] = uint32(
		(in[13]-in[12])>>26 |
			((in[14] - in[13]) << 4))
	out[14] = uint32(
		(in[14]-in[13])>>28 |
			((in[15] - in[14]) << 2))
	out[15] = uint32(
		in[16] - in[15] |
			((in[17] - in[16]) << 30))
	out[16] = uint32(
		(in[17]-in[16])>>2 |
			((in[18] - in[17]) << 28))
	out[17] = uint32(
		(in[18]-in[17])>>4 |
			((in[19] - in[18]) << 26))
	out[18] = uint32(
		(in[19]-in[18])>>6 |
			((in[20] - in[19]) << 24))
	out[19] = uint32(
		(in[20]-in[19])>>8 |
			((in[21] - in[20]) << 22))
	out[20] = uint32(
		(in[21]-in[20])>>10 |
			((in[22] - in[21]) << 20))
	out[21] = uint32(
		(in[22]-in[21])>>12 |
			((in[23] - in[22]) << 18))
	out[22] = uint32(
		(in[23]-in[22])>>14 |
			((in[24] - in[23]) << 16))
	out[23] = uint32(
		(in[24]-in[23])>>16 |
			((in[25] - in[24]) << 14))
	out[24] = uint32(
		(in[25]-in[24])>>18 |
			((in[26] - in[25]) << 12))
	out[25] = uint32(
		(in[26]-in[25])>>20 |
			((in[27] - in[26]) << 10))
	out[26] = uint32(
		(in[27]-in[26])>>22 |
			((in[28] - in[27]) << 8))
	out[27] = uint32(
		(in[28]-in[27])>>24 |
			((in[29] - in[28]) << 6))
	out[28] = uint32(
		(in[29]-in[28])>>26 |
			((in[30] - in[29]) << 4))
	out[29] = uint32(
		(in[30]-in[29])>>28 |
			((in[31] - in[30]) << 2))
}

func deltapack32_31[T uint32 | int32](initoffset T, in *[32]T, out *[31]uint32) {
	out[0] = uint32(
		in[0] - initoffset |
			((in[1] - in[0]) << 31))
	out[1] = uint32(
		(in[1]-in[0])>>1 |
			((in[2] - in[1]) << 30))
	out[2] = uint32(
		(in[2]-in[1])>>2 |
			((in[3] - in[2]) << 29))
	out[3] = uint32(
		(in[3]-in[2])>>3 |
			((in[4] - in[3]) << 28))
	out[4] = uint32(
		(in[4]-in[3])>>4 |
			((in[5] - in[4]) << 27))
	out[5] = uint32(
		(in[5]-in[4])>>5 |
			((in[6] - in[5]) << 26))
	out[6] = uint32(
		(in[6]-in[5])>>6 |
			((in[7] - in[6]) << 25))
	out[7] = uint32(
		(in[7]-in[6])>>7 |
			((in[8] - in[7]) << 24))
	out[8] = uint32(
		(in[8]-in[7])>>8 |
			((in[9] - in[8]) << 23))
	out[9] = uint32(
		(in[9]-in[8])>>9 |
			((in[10] - in[9]) << 22))
	out[10] = uint32(
		(in[10]-in[9])>>10 |
			((in[11] - in[10]) << 21))
	out[11] = uint32(
		(in[11]-in[10])>>11 |
			((in[12] - in[11]) << 20))
	out[12] = uint32(
		(in[12]-in[11])>>12 |
			((in[13] - in[12]) << 19))
	out[13] = uint32(
		(in[13]-in[12])>>13 |
			((in[14] - in[13]) << 18))
	out[14] = uint32(
		(in[14]-in[13])>>14 |
			((in[15] - in[14]) << 17))
	out[15] = uint32(
		(in[15]-in[14])>>15 |
			((in[16] - in[15]) << 16))
	out[16] = uint32(
		(in[16]-in[15])>>16 |
			((in[17] - in[16]) << 15))
	out[17] = uint32(
		(in[17]-in[16])>>17 |
			((in[18] - in[17]) << 14))
	out[18] = uint32(
		(in[18]-in[17])>>18 |
			((in[19] - in[18]) << 13))
	out[19] = uint32(
		(in[19]-in[18])>>19 |
			((in[20] - in[19]) << 12))
	out[20] = uint32(
		(in[20]-in[19])>>20 |
			((in[21] - in[20]) << 11))
	out[21] = uint32(
		(in[21]-in[20])>>21 |
			((in[22] - in[21]) << 10))
	out[22] = uint32(
		(in[22]-in[21])>>22 |
			((in[23] - in[22]) << 9))
	out[23] = uint32(
		(in[23]-in[22])>>23 |
			((in[24] - in[23]) << 8))
	out[24] = uint32(
		(in[24]-in[23])>>24 |
			((in[25] - in[24]) << 7))
	out[25] = uint32(
		(in[25]-in[24])>>25 |
			((in[26] - in[25]) << 6))
	out[26] = uint32(
		(in[26]-in[25])>>26 |
			((in[27] - in[26]) << 5))
	out[27] = uint32(
		(in[27]-in[26])>>27 |
			((in[28] - in[27]) << 4))
	out[28] = uint32(
		(in[28]-in[27])>>28 |
			((in[29] - in[28]) << 3))
	out[29] = uint32(
		(in[29]-in[28])>>29 |
			((in[30] - in[29]) << 2))
	out[30] = uint32(
		(in[30]-in[29])>>30 |
			((in[31] - in[30]) << 1))
}

func deltaunpack32_0[T uint32 | int32](initoffset T, in *[0]uint32, out *[32]T) {
	out[0] = initoffset
	out[1] = initoffset
	out[2] = initoffset
	out[3] = initoffset
	out[4] = initoffset
	out[5] = initoffset
	out[6] = initoffset
	out[7] = initoffset
	out[8] = initoffset
	out[9] = initoffset
	out[10] = initoffset
	out[11] = initoffset
	out[12] = initoffset
	out[13] = initoffset
	out[14] = initoffset
	out[15] = initoffset
	out[16] = initoffset
	out[17] = initoffset
	out[18] = initoffset
	out[19] = initoffset
	out[20] = initoffset
	out[21] = initoffset
	out[22] = initoffset
	out[23] = initoffset
	out[24] = initoffset
	out[25] = initoffset
	out[26] = initoffset
	out[27] = initoffset
	out[28] = initoffset
	out[29] = initoffset
	out[30] = initoffset
	out[31] = initoffset
}

func deltaunpack32_1[T uint32 | int32](initoffset T, in *[1]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x1) + initoffset
	out[1] = T((in[0]>>1)&0x1) + out[0]
	out[2] = T((in[0]>>2)&0x1) + out[1]
	out[3] = T((in[0]>>3)&0x1) + out[2]
	out[4] = T((in[0]>>4)&0x1) + out[3]
	out[5] = T((in[0]>>5)&0x1) + out[4]
	out[6] = T((in[0]>>6)&0x1) + out[5]
	out[7] = T((in[0]>>7)&0x1) + out[6]
	out[8] = T((in[0]>>8)&0x1) + out[7]
	out[9] = T((in[0]>>9)&0x1) + out[8]
	out[10] = T((in[0]>>10)&0x1) + out[9]
	out[11] = T((in[0]>>11)&0x1) + out[10]
	out[12] = T((in[0]>>12)&0x1) + out[11]
	out[13] = T((in[0]>>13)&0x1) + out[12]
	out[14] = T((in[0]>>14)&0x1) + out[13]
	out[15] = T((in[0]>>15)&0x1) + out[14]
	out[16] = T((in[0]>>16)&0x1) + out[15]
	out[17] = T((in[0]>>17)&0x1) + out[16]
	out[18] = T((in[0]>>18)&0x1) + out[17]
	out[19] = T((in[0]>>19)&0x1) + out[18]
	out[20] = T((in[0]>>20)&0x1) + out[19]
	out[21] = T((in[0]>>21)&0x1) + out[20]
	out[22] = T((in[0]>>22)&0x1) + out[21]
	out[23] = T((in[0]>>23)&0x1) + out[22]
	out[24] = T((in[0]>>24)&0x1) + out[23]
	out[25] = T((in[0]>>25)&0x1) + out[24]
	out[26] = T((in[0]>>26)&0x1) + out[25]
	out[27] = T((in[0]>>27)&0x1) + out[26]
	out[28] = T((in[0]>>28)&0x1) + out[27]
	out[29] = T((in[0]>>29)&0x1) + out[28]
	out[30] = T((in[0]>>30)&0x1) + out[29]
	out[31] = T((in[0] >> 31)) + out[30]
}

func deltaunpack32_2[T uint32 | int32](initoffset T, in *[2]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x3) + initoffset
	out[1] = T((in[0]>>2)&0x3) + out[0]
	out[2] = T((in[0]>>4)&0x3) + out[1]
	out[3] = T((in[0]>>6)&0x3) + out[2]
	out[4] = T((in[0]>>8)&0x3) + out[3]
	out[5] = T((in[0]>>10)&0x3) + out[4]
	out[6] = T((in[0]>>12)&0x3) + out[5]
	out[7] = T((in[0]>>14)&0x3) + out[6]
	out[8] = T((in[0]>>16)&0x3) + out[7]
	out[9] = T((in[0]>>18)&0x3) + out[8]
	out[10] = T((in[0]>>20)&0x3) + out[9]
	out[11] = T((in[0]>>22)&0x3) + out[10]
	out[12] = T((in[0]>>24)&0x3) + out[11]
	out[13] = T((in[0]>>26)&0x3) + out[12]
	out[14] = T((in[0]>>28)&0x3) + out[13]
	out[15] = T((in[0] >> 30)) + out[14]
	out[16] = T((in[1]>>0)&0x3) + out[15]
	out[17] = T((in[1]>>2)&0x3) + out[16]
	out[18] = T((in[1]>>4)&0x3) + out[17]
	out[19] = T((in[1]>>6)&0x3) + out[18]
	out[20] = T((in[1]>>8)&0x3) + out[19]
	out[21] = T((in[1]>>10)&0x3) + out[20]
	out[22] = T((in[1]>>12)&0x3) + out[21]
	out[23] = T((in[1]>>14)&0x3) + out[22]
	out[24] = T((in[1]>>16)&0x3) + out[23]
	out[25] = T((in[1]>>18)&0x3) + out[24]
	out[26] = T((in[1]>>20)&0x3) + out[25]
	out[27] = T((in[1]>>22)&0x3) + out[26]
	out[28] = T((in[1]>>24)&0x3) + out[27]
	out[29] = T((in[1]>>26)&0x3) + out[28]
	out[30] = T((in[1]>>28)&0x3) + out[29]
	out[31] = T((in[1] >> 30)) + out[30]
}

func deltaunpack32_3[T uint32 | int32](initoffset T, in *[3]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x7) + initoffset
	out[1] = T((in[0]>>3)&0x7) + out[0]
	out[2] = T((in[0]>>6)&0x7) + out[1]
	out[3] = T((in[0]>>9)&0x7) + out[2]
	out[4] = T((in[0]>>12)&0x7) + out[3]
	out[5] = T((in[0]>>15)&0x7) + out[4]
	out[6] = T((in[0]>>18)&0x7) + out[5]
	out[7] = T((in[0]>>21)&0x7) + out[6]
	out[8] = T((in[0]>>24)&0x7) + out[7]
	out[9] = T((in[0]>>27)&0x7) + out[8]
	out[10] = T(((in[0] >> 30) | ((in[1] & 0x1) << 2))) + out[9]
	out[11] = T((in[1]>>1)&0x7) + out[10]
	out[12] = T((in[1]>>4)&0x7) + out[11]
	out[13] = T((in[1]>>7)&0x7) + out[12]
	out[14] = T((in[1]>>10)&0x7) + out[13]
	out[15] = T((in[1]>>13)&0x7) + out[14]
	out[16] = T((in[1]>>16)&0x7) + out[15]
	out[17] = T((in[1]>>19)&0x7) + out[16]
	out[18] = T((in[1]>>22)&0x7) + out[17]
	out[19] = T((in[1]>>25)&0x7) + out[18]
	out[20] = T((in[1]>>28)&0x7) + out[19]
	out[21] = T(((in[1] >> 31) | ((in[2] & 0x3) << 1))) + out[20]
	out[22] = T((in[2]>>2)&0x7) + out[21]
	out[23] = T((in[2]>>5)&0x7) + out[22]
	out[24] = T((in[2]>>8)&0x7) + out[23]
	out[25] = T((in[2]>>11)&0x7) + out[24]
	out[26] = T((in[2]>>14)&0x7) + out[25]
	out[27] = T((in[2]>>17)&0x7) + out[26]
	out[28] = T((in[2]>>20)&0x7) + out[27]
	out[29] = T((in[2]>>23)&0x7) + out[28]
	out[30] = T((in[2]>>26)&0x7) + out[29]
	out[31] = T((in[2] >> 29)) + out[30]
}

func deltaunpack32_4[T uint32 | int32](initoffset T, in *[4]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0xF) + initoffset
	out[1] = T((in[0]>>4)&0xF) + out[0]
	out[2] = T((in[0]>>8)&0xF) + out[1]
	out[3] = T((in[0]>>12)&0xF) + out[2]
	out[4] = T((in[0]>>16)&0xF) + out[3]
	out[5] = T((in[0]>>20)&0xF) + out[4]
	out[6] = T((in[0]>>24)&0xF) + out[5]
	out[7] = T((in[0] >> 28)) + out[6]
	out[8] = T((in[1]>>0)&0xF) + out[7]
	out[9] = T((in[1]>>4)&0xF) + out[8]
	out[10] = T((in[1]>>8)&0xF) + out[9]
	out[11] = T((in[1]>>12)&0xF) + out[10]
	out[12] = T((in[1]>>16)&0xF) + out[11]
	out[13] = T((in[1]>>20)&0xF) + out[12]
	out[14] = T((in[1]>>24)&0xF) + out[13]
	out[15] = T((in[1] >> 28)) + out[14]
	out[16] = T((in[2]>>0)&0xF) + out[15]
	out[17] = T((in[2]>>4)&0xF) + out[16]
	out[18] = T((in[2]>>8)&0xF) + out[17]
	out[19] = T((in[2]>>12)&0xF) + out[18]
	out[20] = T((in[2]>>16)&0xF) + out[19]
	out[21] = T((in[2]>>20)&0xF) + out[20]
	out[22] = T((in[2]>>24)&0xF) + out[21]
	out[23] = T((in[2] >> 28)) + out[22]
	out[24] = T((in[3]>>0)&0xF) + out[23]
	out[25] = T((in[3]>>4)&0xF) + out[24]
	out[26] = T((in[3]>>8)&0xF) + out[25]
	out[27] = T((in[3]>>12)&0xF) + out[26]
	out[28] = T((in[3]>>16)&0xF) + out[27]
	out[29] = T((in[3]>>20)&0xF) + out[28]
	out[30] = T((in[3]>>24)&0xF) + out[29]
	out[31] = T((in[3] >> 28)) + out[30]
}

func deltaunpack32_5[T uint32 | int32](initoffset T, in *[5]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x1F) + initoffset
	out[1] = T((in[0]>>5)&0x1F) + out[0]
	out[2] = T((in[0]>>10)&0x1F) + out[1]
	out[3] = T((in[0]>>15)&0x1F) + out[2]
	out[4] = T((in[0]>>20)&0x1F) + out[3]
	out[5] = T((in[0]>>25)&0x1F) + out[4]
	out[6] = T(((in[0] >> 30) | ((in[1] & 0x7) << 2))) + out[5]
	out[7] = T((in[1]>>3)&0x1F) + out[6]
	out[8] = T((in[1]>>8)&0x1F) + out[7]
	out[9] = T((in[1]>>13)&0x1F) + out[8]
	out[10] = T((in[1]>>18)&0x1F) + out[9]
	out[11] = T((in[1]>>23)&0x1F) + out[10]
	out[12] = T(((in[1] >> 28) | ((in[2] & 0x1) << 4))) + out[11]
	out[13] = T((in[2]>>1)&0x1F) + out[12]
	out[14] = T((in[2]>>6)&0x1F) + out[13]
	out[15] = T((in[2]>>11)&0x1F) + out[14]
	out[16] = T((in[2]>>16)&0x1F) + out[15]
	out[17] = T((in[2]>>21)&0x1F) + out[16]
	out[18] = T((in[2]>>26)&0x1F) + out[17]
	out[19] = T(((in[2] >> 31) | ((in[3] & 0xF) << 1))) + out[18]
	out[20] = T((in[3]>>4)&0x1F) + out[19]
	out[21] = T((in[3]>>9)&0x1F) + out[20]
	out[22] = T((in[3]>>14)&0x1F) + out[21]
	out[23] = T((in[3]>>19)&0x1F) + out[22]
	out[24] = T((in[3]>>24)&0x1F) + out[23]
	out[25] = T(((in[3] >> 29) | ((in[4] & 0x3) << 3))) + out[24]
	out[26] = T((in[4]>>2)&0x1F) + out[25]
	out[27] = T((in[4]>>7)&0x1F) + out[26]
	out[28] = T((in[4]>>12)&0x1F) + out[27]
	out[29] = T((in[4]>>17)&0x1F) + out[28]
	out[30] = T((in[4]>>22)&0x1F) + out[29]
	out[31] = T((in[4] >> 27)) + out[30]
}

func deltaunpack32_6[T uint32 | int32](initoffset T, in *[6]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x3F) + initoffset
	out[1] = T((in[0]>>6)&0x3F) + out[0]
	out[2] = T((in[0]>>12)&0x3F) + out[1]
	out[3] = T((in[0]>>18)&0x3F) + out[2]
	out[4] = T((in[0]>>24)&0x3F) + out[3]
	out[5] = T(((in[0] >> 30) | ((in[1] & 0xF) << 2))) + out[4]
	out[6] = T((in[1]>>4)&0x3F) + out[5]
	out[7] = T((in[1]>>10)&0x3F) + out[6]
	out[8] = T((in[1]>>16)&0x3F) + out[7]
	out[9] = T((in[1]>>22)&0x3F) + out[8]
	out[10] = T(((in[1] >> 28) | ((in[2] & 0x3) << 4))) + out[9]
	out[11] = T((in[2]>>2)&0x3F) + out[10]
	out[12] = T((in[2]>>8)&0x3F) + out[11]
	out[13] = T((in[2]>>14)&0x3F) + out[12]
	out[14] = T((in[2]>>20)&0x3F) + out[13]
	out[15] = T((in[2] >> 26)) + out[14]
	out[16] = T((in[3]>>0)&0x3F) + out[15]
	out[17] = T((in[3]>>6)&0x3F) + out[16]
	out[18] = T((in[3]>>12)&0x3F) + out[17]
	out[19] = T((in[3]>>18)&0x3F) + out[18]
	out[20] = T((in[3]>>24)&0x3F) + out[19]
	out[21] = T(((in[3] >> 30) | ((in[4] & 0xF) << 2))) + out[20]
	out[22] = T((in[4]>>4)&0x3F) + out[21]
	out[23] = T((in[4]>>10)&0x3F) + out[22]
	out[24] = T((in[4]>>16)&0x3F) + out[23]
	out[25] = T((in[4]>>22)&0x3F) + out[24]
	out[26] = T(((in[4] >> 28) | ((in[5] & 0x3) << 4))) + out[25]
	out[27] = T((in[5]>>2)&0x3F) + out[26]
	out[28] = T((in[5]>>8)&0x3F) + out[27]
	out[29] = T((in[5]>>14)&0x3F) + out[28]
	out[30] = T((in[5]>>20)&0x3F) + out[29]
	out[31] = T((in[5] >> 26)) + out[30]
}

func deltaunpack32_7[T uint32 | int32](initoffset T, in *[7]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x7F) + initoffset
	out[1] = T((in[0]>>7)&0x7F) + out[0]
	out[2] = T((in[0]>>14)&0x7F) + out[1]
	out[3] = T((in[0]>>21)&0x7F) + out[2]
	out[4] = T(((in[0] >> 28) | ((in[1] & 0x7) << 4))) + out[3]
	out[5] = T((in[1]>>3)&0x7F) + out[4]
	out[6] = T((in[1]>>10)&0x7F) + out[5]
	out[7] = T((in[1]>>17)&0x7F) + out[6]
	out[8] = T((in[1]>>24)&0x7F) + out[7]
	out[9] = T(((in[1] >> 31) | ((in[2] & 0x3F) << 1))) + out[8]
	out[10] = T((in[2]>>6)&0x7F) + out[9]
	out[11] = T((in[2]>>13)&0x7F) + out[10]
	out[12] = T((in[2]>>20)&0x7F) + out[11]
	out[13] = T(((in[2] >> 27) | ((in[3] & 0x3) << 5))) + out[12]
	out[14] = T((in[3]>>2)&0x7F) + out[13]
	out[15] = T((in[3]>>9)&0x7F) + out[14]
	out[16] = T((in[3]>>16)&0x7F) + out[15]
	out[17] = T((in[3]>>23)&0x7F) + out[16]
	out[18] = T(((in[3] >> 30) | ((in[4] & 0x1F) << 2))) + out[17]
	out[19] = T((in[4]>>5)&0x7F) + out[18]
	out[20] = T((in[4]>>12)&0x7F) + out[19]
	out[21] = T((in[4]>>19)&0x7F) + out[20]
	out[22] = T(((in[4] >> 26) | ((in[5] & 0x1) << 6))) + out[21]
	out[23] = T((in[5]>>1)&0x7F) + out[22]
	out[24] = T((in[5]>>8)&0x7F) + out[23]
	out[25] = T((in[5]>>15)&0x7F) + out[24]
	out[26] = T((in[5]>>22)&0x7F) + out[25]
	out[27] = T(((in[5] >> 29) | ((in[6] & 0xF) << 3))) + out[26]
	out[28] = T((in[6]>>4)&0x7F) + out[27]
	out[29] = T((in[6]>>11)&0x7F) + out[28]
	out[30] = T((in[6]>>18)&0x7F) + out[29]
	out[31] = T((in[6] >> 25)) + out[30]
}

func deltaunpack32_8[T uint32 | int32](initoffset T, in *[8]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0xFF) + initoffset
	out[1] = T((in[0]>>8)&0xFF) + out[0]
	out[2] = T((in[0]>>16)&0xFF) + out[1]
	out[3] = T((in[0] >> 24)) + out[2]
	out[4] = T((in[1]>>0)&0xFF) + out[3]
	out[5] = T((in[1]>>8)&0xFF) + out[4]
	out[6] = T((in[1]>>16)&0xFF) + out[5]
	out[7] = T((in[1] >> 24)) + out[6]
	out[8] = T((in[2]>>0)&0xFF) + out[7]
	out[9] = T((in[2]>>8)&0xFF) + out[8]
	out[10] = T((in[2]>>16)&0xFF) + out[9]
	out[11] = T((in[2] >> 24)) + out[10]
	out[12] = T((in[3]>>0)&0xFF) + out[11]
	out[13] = T((in[3]>>8)&0xFF) + out[12]
	out[14] = T((in[3]>>16)&0xFF) + out[13]
	out[15] = T((in[3] >> 24)) + out[14]
	out[16] = T((in[4]>>0)&0xFF) + out[15]
	out[17] = T((in[4]>>8)&0xFF) + out[16]
	out[18] = T((in[4]>>16)&0xFF) + out[17]
	out[19] = T((in[4] >> 24)) + out[18]
	out[20] = T((in[5]>>0)&0xFF) + out[19]
	out[21] = T((in[5]>>8)&0xFF) + out[20]
	out[22] = T((in[5]>>16)&0xFF) + out[21]
	out[23] = T((in[5] >> 24)) + out[22]
	out[24] = T((in[6]>>0)&0xFF) + out[23]
	out[25] = T((in[6]>>8)&0xFF) + out[24]
	out[26] = T((in[6]>>16)&0xFF) + out[25]
	out[27] = T((in[6] >> 24)) + out[26]
	out[28] = T((in[7]>>0)&0xFF) + out[27]
	out[29] = T((in[7]>>8)&0xFF) + out[28]
	out[30] = T((in[7]>>16)&0xFF) + out[29]
	out[31] = T((in[7] >> 24)) + out[30]
}

func deltaunpack32_9[T uint32 | int32](initoffset T, in *[9]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x1FF) + initoffset
	out[1] = T((in[0]>>9)&0x1FF) + out[0]
	out[2] = T((in[0]>>18)&0x1FF) + out[1]
	out[3] = T(((in[0] >> 27) | ((in[1] & 0xF) << 5))) + out[2]
	out[4] = T((in[1]>>4)&0x1FF) + out[3]
	out[5] = T((in[1]>>13)&0x1FF) + out[4]
	out[6] = T((in[1]>>22)&0x1FF) + out[5]
	out[7] = T(((in[1] >> 31) | ((in[2] & 0xFF) << 1))) + out[6]
	out[8] = T((in[2]>>8)&0x1FF) + out[7]
	out[9] = T((in[2]>>17)&0x1FF) + out[8]
	out[10] = T(((in[2] >> 26) | ((in[3] & 0x7) << 6))) + out[9]
	out[11] = T((in[3]>>3)&0x1FF) + out[10]
	out[12] = T((in[3]>>12)&0x1FF) + out[11]
	out[13] = T((in[3]>>21)&0x1FF) + out[12]
	out[14] = T(((in[3] >> 30) | ((in[4] & 0x7F) << 2))) + out[13]
	out[15] = T((in[4]>>7)&0x1FF) + out[14]
	out[16] = T((in[4]>>16)&0x1FF) + out[15]
	out[17] = T(((in[4] >> 25) | ((in[5] & 0x3) << 7))) + out[16]
	out[18] = T((in[5]>>2)&0x1FF) + out[17]
	out[19] = T((in[5]>>11)&0x1FF) + out[18]
	out[20] = T((in[5]>>20)&0x1FF) + out[19]
	out[21] = T(((in[5] >> 29) | ((in[6] & 0x3F) << 3))) + out[20]
	out[22] = T((in[6]>>6)&0x1FF) + out[21]
	out[23] = T((in[6]>>15)&0x1FF) + out[22]
	out[24] = T(((in[6] >> 24) | ((in[7] & 0x1) << 8))) + out[23]
	out[25] = T((in[7]>>1)&0x1FF) + out[24]
	out[26] = T((in[7]>>10)&0x1FF) + out[25]
	out[27] = T((in[7]>>19)&0x1FF) + out[26]
	out[28] = T(((in[7] >> 28) | ((in[8] & 0x1F) << 4))) + out[27]
	out[29] = T((in[8]>>5)&0x1FF) + out[28]
	out[30] = T((in[8]>>14)&0x1FF) + out[29]
	out[31] = T((in[8] >> 23)) + out[30]
}

func deltaunpack32_10[T uint32 | int32](initoffset T, in *[10]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x3FF) + initoffset
	out[1] = T((in[0]>>10)&0x3FF) + out[0]
	out[2] = T((in[0]>>20)&0x3FF) + out[1]
	out[3] = T(((in[0] >> 30) | ((in[1] & 0xFF) << 2))) + out[2]
	out[4] = T((in[1]>>8)&0x3FF) + out[3]
	out[5] = T((in[1]>>18)&0x3FF) + out[4]
	out[6] = T(((in[1] >> 28) | ((in[2] & 0x3F) << 4))) + out[5]
	out[7] = T((in[2]>>6)&0x3FF) + out[6]
	out[8] = T((in[2]>>16)&0x3FF) + out[7]
	out[9] = T(((in[2] >> 26) | ((in[3] & 0xF) << 6))) + out[8]
	out[10] = T((in[3]>>4)&0x3FF) + out[9]
	out[11] = T((in[3]>>14)&0x3FF) + out[10]
	out[12] = T(((in[3] >> 24) | ((in[4] & 0x3) << 8))) + out[11]
	out[13] = T((in[4]>>2)&0x3FF) + out[12]
	out[14] = T((in[4]>>12)&0x3FF) + out[13]
	out[15] = T((in[4] >> 22)) + out[14]
	out[16] = T((in[5]>>0)&0x3FF) + out[15]
	out[17] = T((in[5]>>10)&0x3FF) + out[16]
	out[18] = T((in[5]>>20)&0x3FF) + out[17]
	out[19] = T(((in[5] >> 30) | ((in[6] & 0xFF) << 2))) + out[18]
	out[20] = T((in[6]>>8)&0x3FF) + out[19]
	out[21] = T((in[6]>>18)&0x3FF) + out[20]
	out[22] = T(((in[6] >> 28) | ((in[7] & 0x3F) << 4))) + out[21]
	out[23] = T((in[7]>>6)&0x3FF) + out[22]
	out[24] = T((in[7]>>16)&0x3FF) + out[23]
	out[25] = T(((in[7] >> 26) | ((in[8] & 0xF) << 6))) + out[24]
	out[26] = T((in[8]>>4)&0x3FF) + out[25]
	out[27] = T((in[8]>>14)&0x3FF) + out[26]
	out[28] = T(((in[8] >> 24) | ((in[9] & 0x3) << 8))) + out[27]
	out[29] = T((in[9]>>2)&0x3FF) + out[28]
	out[30] = T((in[9]>>12)&0x3FF) + out[29]
	out[31] = T((in[9] >> 22)) + out[30]
}

func deltaunpack32_11[T uint32 | int32](initoffset T, in *[11]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x7FF) + initoffset
	out[1] = T((in[0]>>11)&0x7FF) + out[0]
	out[2] = T(((in[0] >> 22) | ((in[1] & 0x1) << 10))) + out[1]
	out[3] = T((in[1]>>1)&0x7FF) + out[2]
	out[4] = T((in[1]>>12)&0x7FF) + out[3]
	out[5] = T(((in[1] >> 23) | ((in[2] & 0x3) << 9))) + out[4]
	out[6] = T((in[2]>>2)&0x7FF) + out[5]
	out[7] = T((in[2]>>13)&0x7FF) + out[6]
	out[8] = T(((in[2] >> 24) | ((in[3] & 0x7) << 8))) + out[7]
	out[9] = T((in[3]>>3)&0x7FF) + out[8]
	out[10] = T((in[3]>>14)&0x7FF) + out[9]
	out[11] = T(((in[3] >> 25) | ((in[4] & 0xF) << 7))) + out[10]
	out[12] = T((in[4]>>4)&0x7FF) + out[11]
	out[13] = T((in[4]>>15)&0x7FF) + out[12]
	out[14] = T(((in[4] >> 26) | ((in[5] & 0x1F) << 6))) + out[13]
	out[15] = T((in[5]>>5)&0x7FF) + out[14]
	out[16] = T((in[5]>>16)&0x7FF) + out[15]
	out[17] = T(((in[5] >> 27) | ((in[6] & 0x3F) << 5))) + out[16]
	out[18] = T((in[6]>>6)&0x7FF) + out[17]
	out[19] = T((in[6]>>17)&0x7FF) + out[18]
	out[20] = T(((in[6] >> 28) | ((in[7] & 0x7F) << 4))) + out[19]
	out[21] = T((in[7]>>7)&0x7FF) + out[20]
	out[22] = T((in[7]>>18)&0x7FF) + out[21]
	out[23] = T(((in[7] >> 29) | ((in[8] & 0xFF) << 3))) + out[22]
	out[24] = T((in[8]>>8)&0x7FF) + out[23]
	out[25] = T((in[8]>>19)&0x7FF) + out[24]
	out[26] = T(((in[8] >> 30) | ((in[9] & 0x1FF) << 2))) + out[25]
	out[27] = T((in[9]>>9)&0x7FF) + out[26]
	out[28] = T((in[9]>>20)&0x7FF) + out[27]
	out[29] = T(((in[9] >> 31) | ((in[10] & 0x3FF) << 1))) + out[28]
	out[30] = T((in[10]>>10)&0x7FF) + out[29]
	out[31] = T((in[10] >> 21)) + out[30]
}

func deltaunpack32_12[T uint32 | int32](initoffset T, in *[12]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0xFFF) + initoffset
	out[1] = T((in[0]>>12)&0xFFF) + out[0]
	out[2] = T(((in[0] >> 24) | ((in[1] & 0xF) << 8))) + out[1]
	out[3] = T((in[1]>>4)&0xFFF) + out[2]
	out[4] = T((in[1]>>16)&0xFFF) + out[3]
	out[5] = T(((in[1] >> 28) | ((in[2] & 0xFF) << 4))) + out[4]
	out[6] = T((in[2]>>8)&0xFFF) + out[5]
	out[7] = T((in[2] >> 20)) + out[6]
	out[8] = T((in[3]>>0)&0xFFF) + out[7]
	out[9] = T((in[3]>>12)&0xFFF) + out[8]
	out[10] = T(((in[3] >> 24) | ((in[4] & 0xF) << 8))) + out[9]
	out[11] = T((in[4]>>4)&0xFFF) + out[10]
	out[12] = T((in[4]>>16)&0xFFF) + out[11]
	out[13] = T(((in[4] >> 28) | ((in[5] & 0xFF) << 4))) + out[12]
	out[14] = T((in[5]>>8)&0xFFF) + out[13]
	out[15] = T((in[5] >> 20)) + out[14]
	out[16] = T((in[6]>>0)&0xFFF) + out[15]
	out[17] = T((in[6]>>12)&0xFFF) + out[16]
	out[18] = T(((in[6] >> 24) | ((in[7] & 0xF) << 8))) + out[17]
	out[19] = T((in[7]>>4)&0xFFF) + out[18]
	out[20] = T((in[7]>>16)&0xFFF) + out[19]
	out[21] = T(((in[7] >> 28) | ((in[8] & 0xFF) << 4))) + out[20]
	out[22] = T((in[8]>>8)&0xFFF) + out[21]
	out[23] = T((in[8] >> 20)) + out[22]
	out[24] = T((in[9]>>0)&0xFFF) + out[23]
	out[25] = T((in[9]>>12)&0xFFF) + out[24]
	out[26] = T(((in[9] >> 24) | ((in[10] & 0xF) << 8))) + out[25]
	out[27] = T((in[10]>>4)&0xFFF) + out[26]
	out[28] = T((in[10]>>16)&0xFFF) + out[27]
	out[29] = T(((in[10] >> 28) | ((in[11] & 0xFF) << 4))) + out[28]
	out[30] = T((in[11]>>8)&0xFFF) + out[29]
	out[31] = T((in[11] >> 20)) + out[30]
}

func deltaunpack32_13[T uint32 | int32](initoffset T, in *[13]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x1FFF) + initoffset
	out[1] = T((in[0]>>13)&0x1FFF) + out[0]
	out[2] = T(((in[0] >> 26) | ((in[1] & 0x7F) << 6))) + out[1]
	out[3] = T((in[1]>>7)&0x1FFF) + out[2]
	out[4] = T(((in[1] >> 20) | ((in[2] & 0x1) << 12))) + out[3]
	out[5] = T((in[2]>>1)&0x1FFF) + out[4]
	out[6] = T((in[2]>>14)&0x1FFF) + out[5]
	out[7] = T(((in[2] >> 27) | ((in[3] & 0xFF) << 5))) + out[6]
	out[8] = T((in[3]>>8)&0x1FFF) + out[7]
	out[9] = T(((in[3] >> 21) | ((in[4] & 0x3) << 11))) + out[8]
	out[10] = T((in[4]>>2)&0x1FFF) + out[9]
	out[11] = T((in[4]>>15)&0x1FFF) + out[10]
	out[12] = T(((in[4] >> 28) | ((in[5] & 0x1FF) << 4))) + out[11]
	out[13] = T((in[5]>>9)&0x1FFF) + out[12]
	out[14] = T(((in[5] >> 22) | ((in[6] & 0x7) << 10))) + out[13]
	out[15] = T((in[6]>>3)&0x1FFF) + out[14]
	out[16] = T((in[6]>>16)&0x1FFF) + out[15]
	out[17] = T(((in[6] >> 29) | ((in[7] & 0x3FF) << 3))) + out[16]
	out[18] = T((in[7]>>10)&0x1FFF) + out[17]
	out[19] = T(((in[7] >> 23) | ((in[8] & 0xF) << 9))) + out[18]
	out[20] = T((in[8]>>4)&0x1FFF) + out[19]
	out[21] = T((in[8]>>17)&0x1FFF) + out[20]
	out[22] = T(((in[8] >> 30) | ((in[9] & 0x7FF) << 2))) + out[21]
	out[23] = T((in[9]>>11)&0x1FFF) + out[22]
	out[24] = T(((in[9] >> 24) | ((in[10] & 0x1F) << 8))) + out[23]
	out[25] = T((in[10]>>5)&0x1FFF) + out[24]
	out[26] = T((in[10]>>18)&0x1FFF) + out[25]
	out[27] = T(((in[10] >> 31) | ((in[11] & 0xFFF) << 1))) + out[26]
	out[28] = T((in[11]>>12)&0x1FFF) + out[27]
	out[29] = T(((in[11] >> 25) | ((in[12] & 0x3F) << 7))) + out[28]
	out[30] = T((in[12]>>6)&0x1FFF) + out[29]
	out[31] = T((in[12] >> 19)) + out[30]
}

func deltaunpack32_14[T uint32 | int32](initoffset T, in *[14]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x3FFF) + initoffset
	out[1] = T((in[0]>>14)&0x3FFF) + out[0]
	out[2] = T(((in[0] >> 28) | ((in[1] & 0x3FF) << 4))) + out[1]
	out[3] = T((in[1]>>10)&0x3FFF) + out[2]
	out[4] = T(((in[1] >> 24) | ((in[2] & 0x3F) << 8))) + out[3]
	out[5] = T((in[2]>>6)&0x3FFF) + out[4]
	out[6] = T(((in[2] >> 20) | ((in[3] & 0x3) << 12))) + out[5]
	out[7] = T((in[3]>>2)&0x3FFF) + out[6]
	out[8] = T((in[3]>>16)&0x3FFF) + out[7]
	out[9] = T(((in[3] >> 30) | ((in[4] & 0xFFF) << 2))) + out[8]
	out[10] = T((in[4]>>12)&0x3FFF) + out[9]
	out[11] = T(((in[4] >> 26) | ((in[5] & 0xFF) << 6))) + out[10]
	out[12] = T((in[5]>>8)&0x3FFF) + out[11]
	out[13] = T(((in[5] >> 22) | ((in[6] & 0xF) << 10))) + out[12]
	out[14] = T((in[6]>>4)&0x3FFF) + out[13]
	out[15] = T((in[6] >> 18)) + out[14]
	out[16] = T((in[7]>>0)&0x3FFF) + out[15]
	out[17] = T((in[7]>>14)&0x3FFF) + out[16]
	out[18] = T(((in[7] >> 28) | ((in[8] & 0x3FF) << 4))) + out[17]
	out[19] = T((in[8]>>10)&0x3FFF) + out[18]
	out[20] = T(((in[8] >> 24) | ((in[9] & 0x3F) << 8))) + out[19]
	out[21] = T((in[9]>>6)&0x3FFF) + out[20]
	out[22] = T(((in[9] >> 20) | ((in[10] & 0x3) << 12))) + out[21]
	out[23] = T((in[10]>>2)&0x3FFF) + out[22]
	out[24] = T((in[10]>>16)&0x3FFF) + out[23]
	out[25] = T(((in[10] >> 30) | ((in[11] & 0xFFF) << 2))) + out[24]
	out[26] = T((in[11]>>12)&0x3FFF) + out[25]
	out[27] = T(((in[11] >> 26) | ((in[12] & 0xFF) << 6))) + out[26]
	out[28] = T((in[12]>>8)&0x3FFF) + out[27]
	out[29] = T(((in[12] >> 22) | ((in[13] & 0xF) << 10))) + out[28]
	out[30] = T((in[13]>>4)&0x3FFF) + out[29]
	out[31] = T((in[13] >> 18)) + out[30]
}

func deltaunpack32_15[T uint32 | int32](initoffset T, in *[15]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x7FFF) + initoffset
	out[1] = T((in[0]>>15)&0x7FFF) + out[0]
	out[2] = T(((in[0] >> 30) | ((in[1] & 0x1FFF) << 2))) + out[1]
	out[3] = T((in[1]>>13)&0x7FFF) + out[2]
	out[4] = T(((in[1] >> 28) | ((in[2] & 0x7FF) << 4))) + out[3]
	out[5] = T((in[2]>>11)&0x7FFF) + out[4]
	out[6] = T(((in[2] >> 26) | ((in[3] & 0x1FF) << 6))) + out[5]
	out[7] = T((in[3]>>9)&0x7FFF) + out[6]
	out[8] = T(((in[3] >> 24) | ((in[4] & 0x7F) << 8))) + out[7]
	out[9] = T((in[4]>>7)&0x7FFF) + out[8]
	out[10] = T(((in[4] >> 22) | ((in[5] & 0x1F) << 10))) + out[9]
	out[11] = T((in[5]>>5)&0x7FFF) + out[10]
	out[12] = T(((in[5] >> 20) | ((in[6] & 0x7) << 12))) + out[11]
	out[13] = T((in[6]>>3)&0x7FFF) + out[12]
	out[14] = T(((in[6] >> 18) | ((in[7] & 0x1) << 14))) + out[13]
	out[15] = T((in[7]>>1)&0x7FFF) + out[14]
	out[16] = T((in[7]>>16)&0x7FFF) + out[15]
	out[17] = T(((in[7] >> 31) | ((in[8] & 0x3FFF) << 1))) + out[16]
	out[18] = T((in[8]>>14)&0x7FFF) + out[17]
	out[19] = T(((in[8] >> 29) | ((in[9] & 0xFFF) << 3))) + out[18]
	out[20] = T((in[9]>>12)&0x7FFF) + out[19]
	out[21] = T(((in[9] >> 27) | ((in[10] & 0x3FF) << 5))) + out[20]
	out[22] = T((in[10]>>10)&0x7FFF) + out[21]
	out[23] = T(((in[10] >> 25) | ((in[11] & 0xFF) << 7))) + out[22]
	out[24] = T((in[11]>>8)&0x7FFF) + out[23]
	out[25] = T(((in[11] >> 23) | ((in[12] & 0x3F) << 9))) + out[24]
	out[26] = T((in[12]>>6)&0x7FFF) + out[25]
	out[27] = T(((in[12] >> 21) | ((in[13] & 0xF) << 11))) + out[26]
	out[28] = T((in[13]>>4)&0x7FFF) + out[27]
	out[29] = T(((in[13] >> 19) | ((in[14] & 0x3) << 13))) + out[28]
	out[30] = T((in[14]>>2)&0x7FFF) + out[29]
	out[31] = T((in[14] >> 17)) + out[30]
}

func deltaunpack32_16[T uint32 | int32](initoffset T, in *[16]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0xFFFF) + initoffset
	out[1] = T((in[0] >> 16)) + out[0]
	out[2] = T((in[1]>>0)&0xFFFF) + out[1]
	out[3] = T((in[1] >> 16)) + out[2]
	out[4] = T((in[2]>>0)&0xFFFF) + out[3]
	out[5] = T((in[2] >> 16)) + out[4]
	out[6] = T((in[3]>>0)&0xFFFF) + out[5]
	out[7] = T((in[3] >> 16)) + out[6]
	out[8] = T((in[4]>>0)&0xFFFF) + out[7]
	out[9] = T((in[4] >> 16)) + out[8]
	out[10] = T((in[5]>>0)&0xFFFF) + out[9]
	out[11] = T((in[5] >> 16)) + out[10]
	out[12] = T((in[6]>>0)&0xFFFF) + out[11]
	out[13] = T((in[6] >> 16)) + out[12]
	out[14] = T((in[7]>>0)&0xFFFF) + out[13]
	out[15] = T((in[7] >> 16)) + out[14]
	out[16] = T((in[8]>>0)&0xFFFF) + out[15]
	out[17] = T((in[8] >> 16)) + out[16]
	out[18] = T((in[9]>>0)&0xFFFF) + out[17]
	out[19] = T((in[9] >> 16)) + out[18]
	out[20] = T((in[10]>>0)&0xFFFF) + out[19]
	out[21] = T((in[10] >> 16)) + out[20]
	out[22] = T((in[11]>>0)&0xFFFF) + out[21]
	out[23] = T((in[11] >> 16)) + out[22]
	out[24] = T((in[12]>>0)&0xFFFF) + out[23]
	out[25] = T((in[12] >> 16)) + out[24]
	out[26] = T((in[13]>>0)&0xFFFF) + out[25]
	out[27] = T((in[13] >> 16)) + out[26]
	out[28] = T((in[14]>>0)&0xFFFF) + out[27]
	out[29] = T((in[14] >> 16)) + out[28]
	out[30] = T((in[15]>>0)&0xFFFF) + out[29]
	out[31] = T((in[15] >> 16)) + out[30]
}

func deltaunpack32_17[T uint32 | int32](initoffset T, in *[17]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x1FFFF) + initoffset
	out[1] = T(((in[0] >> 17) | ((in[1] & 0x3) << 15))) + out[0]
	out[2] = T((in[1]>>2)&0x1FFFF) + out[1]
	out[3] = T(((in[1] >> 19) | ((in[2] & 0xF) << 13))) + out[2]
	out[4] = T((in[2]>>4)&0x1FFFF) + out[3]
	out[5] = T(((in[2] >> 21) | ((in[3] & 0x3F) << 11))) + out[4]
	out[6] = T((in[3]>>6)&0x1FFFF) + out[5]
	out[7] = T(((in[3] >> 23) | ((in[4] & 0xFF) << 9))) + out[6]
	out[8] = T((in[4]>>8)&0x1FFFF) + out[7]
	out[9] = T(((in[4] >> 25) | ((in[5] & 0x3FF) << 7))) + out[8]
	out[10] = T((in[5]>>10)&0x1FFFF) + out[9]
	out[11] = T(((in[5] >> 27) | ((in[6] & 0xFFF) << 5))) + out[10]
	out[12] = T((in[6]>>12)&0x1FFFF) + out[11]
	out[13] = T(((in[6] >> 29) | ((in[7] & 0x3FFF) << 3))) + out[12]
	out[14] = T((in[7]>>14)&0x1FFFF) + out[13]
	out[15] = T(((in[7] >> 31) | ((in[8] & 0xFFFF) << 1))) + out[14]
	out[16] = T(((in[8] >> 16) | ((in[9] & 0x1) << 16))) + out[15]
	out[17] = T((in[9]>>1)&0x1FFFF) + out[16]
	out[18] = T(((in[9] >> 18) | ((in[10] & 0x7) << 14))) + out[17]
	out[19] = T((in[10]>>3)&0x1FFFF) + out[18]
	out[20] = T(((in[10] >> 20) | ((in[11] & 0x1F) << 12))) + out[19]
	out[21] = T((in[11]>>5)&0x1FFFF) + out[20]
	out[22] = T(((in[11] >> 22) | ((in[12] & 0x7F) << 10))) + out[21]
	out[23] = T((in[12]>>7)&0x1FFFF) + out[22]
	out[24] = T(((in[12] >> 24) | ((in[13] & 0x1FF) << 8))) + out[23]
	out[25] = T((in[13]>>9)&0x1FFFF) + out[24]
	out[26] = T(((in[13] >> 26) | ((in[14] & 0x7FF) << 6))) + out[25]
	out[27] = T((in[14]>>11)&0x1FFFF) + out[26]
	out[28] = T(((in[14] >> 28) | ((in[15] & 0x1FFF) << 4))) + out[27]
	out[29] = T((in[15]>>13)&0x1FFFF) + out[28]
	out[30] = T(((in[15] >> 30) | ((in[16] & 0x7FFF) << 2))) + out[29]
	out[31] = T((in[16] >> 15)) + out[30]
}

func deltaunpack32_18[T uint32 | int32](initoffset T, in *[18]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x3FFFF) + initoffset
	out[1] = T(((in[0] >> 18) | ((in[1] & 0xF) << 14))) + out[0]
	out[2] = T((in[1]>>4)&0x3FFFF) + out[1]
	out[3] = T(((in[1] >> 22) | ((in[2] & 0xFF) << 10))) + out[2]
	out[4] = T((in[2]>>8)&0x3FFFF) + out[3]
	out[5] = T(((in[2] >> 26) | ((in[3] & 0xFFF) << 6))) + out[4]
	out[6] = T((in[3]>>12)&0x3FFFF) + out[5]
	out[7] = T(((in[3] >> 30) | ((in[4] & 0xFFFF) << 2))) + out[6]
	out[8] = T(((in[4] >> 16) | ((in[5] & 0x3) << 16))) + out[7]
	out[9] = T((in[5]>>2)&0x3FFFF) + out[8]
	out[10] = T(((in[5] >> 20) | ((in[6] & 0x3F) << 12))) + out[9]
	out[11] = T((in[6]>>6)&0x3FFFF) + out[10]
	out[12] = T(((in[6] >> 24) | ((in[7] & 0x3FF) << 8))) + out[11]
	out[13] = T((in[7]>>10)&0x3FFFF) + out[12]
	out[14] = T(((in[7] >> 28) | ((in[8] & 0x3FFF) << 4))) + out[13]
	out[15] = T((in[8] >> 14)) + out[14]
	out[16] = T((in[9]>>0)&0x3FFFF) + out[15]
	out[17] = T(((in[9] >> 18) | ((in[10] & 0xF) << 14))) + out[16]
	out[18] = T((in[10]>>4)&0x3FFFF) + out[17]
	out[19] = T(((in[10] >> 22) | ((in[11] & 0xFF) << 10))) + out[18]
	out[20] = T((in[11]>>8)&0x3FFFF) + out[19]
	out[21] = T(((in[11] >> 26) | ((in[12] & 0xFFF) << 6))) + out[20]
	out[22] = T((in[12]>>12)&0x3FFFF) + out[21]
	out[23] = T(((in[12] >> 30) | ((in[13] & 0xFFFF) << 2))) + out[22]
	out[24] = T(((in[13] >> 16) | ((in[14] & 0x3) << 16))) + out[23]
	out[25] = T((in[14]>>2)&0x3FFFF) + out[24]
	out[26] = T(((in[14] >> 20) | ((in[15] & 0x3F) << 12))) + out[25]
	out[27] = T((in[15]>>6)&0x3FFFF) + out[26]
	out[28] = T(((in[15] >> 24) | ((in[16] & 0x3FF) << 8))) + out[27]
	out[29] = T((in[16]>>10)&0x3FFFF) + out[28]
	out[30] = T(((in[16] >> 28) | ((in[17] & 0x3FFF) << 4))) + out[29]
	out[31] = T((in[17] >> 14)) + out[30]
}

func deltaunpack32_19[T uint32 | int32](initoffset T, in *[19]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x7FFFF) + initoffset
	out[1] = T(((in[0] >> 19) | ((in[1] & 0x3F) << 13))) + out[0]
	out[2] = T((in[1]>>6)&0x7FFFF) + out[1]
	out[3] = T(((in[1] >> 25) | ((in[2] & 0xFFF) << 7))) + out[2]
	out[4] = T((in[2]>>12)&0x7FFFF) + out[3]
	out[5] = T(((in[2] >> 31) | ((in[3] & 0x3FFFF) << 1))) + out[4]
	out[6] = T(((in[3] >> 18) | ((in[4] & 0x1F) << 14))) + out[5]
	out[7] = T((in[4]>>5)&0x7FFFF) + out[6]
	out[8] = T(((in[4] >> 24) | ((in[5] & 0x7FF) << 8))) + out[7]
	out[9] = T((in[5]>>11)&0x7FFFF) + out[8]
	out[10] = T(((in[5] >> 30) | ((in[6] & 0x1FFFF) << 2))) + out[9]
	out[11] = T(((in[6] >> 17) | ((in[7] & 0xF) << 15))) + out[10]
	out[12] = T((in[7]>>4)&0x7FFFF) + out[11]
	out[13] = T(((in[7] >> 23) | ((in[8] & 0x3FF) << 9))) + out[12]
	out[14] = T((in[8]>>10)&0x7FFFF) + out[13]
	out[15] = T(((in[8] >> 29) | ((in[9] & 0xFFFF) << 3))) + out[14]
	out[16] = T(((in[9] >> 16) | ((in[10] & 0x7) << 16))) + out[15]
	out[17] = T((in[10]>>3)&0x7FFFF) + out[16]
	out[18] = T(((in[10] >> 22) | ((in[11] & 0x1FF) << 10))) + out[17]
	out[19] = T((in[11]>>9)&0x7FFFF) + out[18]
	out[20] = T(((in[11] >> 28) | ((in[12] & 0x7FFF) << 4))) + out[19]
	out[21] = T(((in[12] >> 15) | ((in[13] & 0x3) << 17))) + out[20]
	out[22] = T((in[13]>>2)&0x7FFFF) + out[21]
	out[23] = T(((in[13] >> 21) | ((in[14] & 0xFF) << 11))) + out[22]
	out[24] = T((in[14]>>8)&0x7FFFF) + out[23]
	out[25] = T(((in[14] >> 27) | ((in[15] & 0x3FFF) << 5))) + out[24]
	out[26] = T(((in[15] >> 14) | ((in[16] & 0x1) << 18))) + out[25]
	out[27] = T((in[16]>>1)&0x7FFFF) + out[26]
	out[28] = T(((in[16] >> 20) | ((in[17] & 0x7F) << 12))) + out[27]
	out[29] = T((in[17]>>7)&0x7FFFF) + out[28]
	out[30] = T(((in[17] >> 26) | ((in[18] & 0x1FFF) << 6))) + out[29]
	out[31] = T((in[18] >> 13)) + out[30]
}

func deltaunpack32_20[T uint32 | int32](initoffset T, in *[20]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0xFFFFF) + initoffset
	out[1] = T(((in[0] >> 20) | ((in[1] & 0xFF) << 12))) + out[0]
	out[2] = T((in[1]>>8)&0xFFFFF) + out[1]
	out[3] = T(((in[1] >> 28) | ((in[2] & 0xFFFF) << 4))) + out[2]
	out[4] = T(((in[2] >> 16) | ((in[3] & 0xF) << 16))) + out[3]
	out[5] = T((in[3]>>4)&0xFFFFF) + out[4]
	out[6] = T(((in[3] >> 24) | ((in[4] & 0xFFF) << 8))) + out[5]
	out[7] = T((in[4] >> 12)) + out[6]
	out[8] = T((in[5]>>0)&0xFFFFF) + out[7]
	out[9] = T(((in[5] >> 20) | ((in[6] & 0xFF) << 12))) + out[8]
	out[10] = T((in[6]>>8)&0xFFFFF) + out[9]
	out[11] = T(((in[6] >> 28) | ((in[7] & 0xFFFF) << 4))) + out[10]
	out[12] = T(((in[7] >> 16) | ((in[8] & 0xF) << 16))) + out[11]
	out[13] = T((in[8]>>4)&0xFFFFF) + out[12]
	out[14] = T(((in[8] >> 24) | ((in[9] & 0xFFF) << 8))) + out[13]
	out[15] = T((in[9] >> 12)) + out[14]
	out[16] = T((in[10]>>0)&0xFFFFF) + out[15]
	out[17] = T(((in[10] >> 20) | ((in[11] & 0xFF) << 12))) + out[16]
	out[18] = T((in[11]>>8)&0xFFFFF) + out[17]
	out[19] = T(((in[11] >> 28) | ((in[12] & 0xFFFF) << 4))) + out[18]
	out[20] = T(((in[12] >> 16) | ((in[13] & 0xF) << 16))) + out[19]
	out[21] = T((in[13]>>4)&0xFFFFF) + out[20]
	out[22] = T(((in[13] >> 24) | ((in[14] & 0xFFF) << 8))) + out[21]
	out[23] = T((in[14] >> 12)) + out[22]
	out[24] = T((in[15]>>0)&0xFFFFF) + out[23]
	out[25] = T(((in[15] >> 20) | ((in[16] & 0xFF) << 12))) + out[24]
	out[26] = T((in[16]>>8)&0xFFFFF) + out[25]
	out[27] = T(((in[16] >> 28) | ((in[17] & 0xFFFF) << 4))) + out[26]
	out[28] = T(((in[17] >> 16) | ((in[18] & 0xF) << 16))) + out[27]
	out[29] = T((in[18]>>4)&0xFFFFF) + out[28]
	out[30] = T(((in[18] >> 24) | ((in[19] & 0xFFF) << 8))) + out[29]
	out[31] = T((in[19] >> 12)) + out[30]
}

func deltaunpack32_21[T uint32 | int32](initoffset T, in *[21]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x1FFFFF) + initoffset
	out[1] = T(((in[0] >> 21) | ((in[1] & 0x3FF) << 11))) + out[0]
	out[2] = T((in[1]>>10)&0x1FFFFF) + out[1]
	out[3] = T(((in[1] >> 31) | ((in[2] & 0xFFFFF) << 1))) + out[2]
	out[4] = T(((in[2] >> 20) | ((in[3] & 0x1FF) << 12))) + out[3]
	out[5] = T((in[3]>>9)&0x1FFFFF) + out[4]
	out[6] = T(((in[3] >> 30) | ((in[4] & 0x7FFFF) << 2))) + out[5]
	out[7] = T(((in[4] >> 19) | ((in[5] & 0xFF) << 13))) + out[6]
	out[8] = T((in[5]>>8)&0x1FFFFF) + out[7]
	out[9] = T(((in[5] >> 29) | ((in[6] & 0x3FFFF) << 3))) + out[8]
	out[10] = T(((in[6] >> 18) | ((in[7] & 0x7F) << 14))) + out[9]
	out[11] = T((in[7]>>7)&0x1FFFFF) + out[10]
	out[12] = T(((in[7] >> 28) | ((in[8] & 0x1FFFF) << 4))) + out[11]
	out[13] = T(((in[8] >> 17) | ((in[9] & 0x3F) << 15))) + out[12]
	out[14] = T((in[9]>>6)&0x1FFFFF) + out[13]
	out[15] = T(((in[9] >> 27) | ((in[10] & 0xFFFF) << 5))) + out[14]
	out[16] = T(((in[10] >> 16) | ((in[11] & 0x1F) << 16))) + out[15]
	out[17] = T((in[11]>>5)&0x1FFFFF) + out[16]
	out[18] = T(((in[11] >> 26) | ((in[12] & 0x7FFF) << 6))) + out[17]
	out[19] = T(((in[12] >> 15) | ((in[13] & 0xF) << 17))) + out[18]
	out[20] = T((in[13]>>4)&0x1FFFFF) + out[19]
	out[21] = T(((in[13] >> 25) | ((in[14] & 0x3FFF) << 7))) + out[20]
	out[22] = T(((in[14] >> 14) | ((in[15] & 0x7) << 18))) + out[21]
	out[23] = T((in[15]>>3)&0x1FFFFF) + out[22]
	out[24] = T(((in[15] >> 24) | ((in[16] & 0x1FFF) << 8))) + out[23]
	out[25] = T(((in[16] >> 13) | ((in[17] & 0x3) << 19))) + out[24]
	out[26] = T((in[17]>>2)&0x1FFFFF) + out[25]
	out[27] = T(((in[17] >> 23) | ((in[18] & 0xFFF) << 9))) + out[26]
	out[28] = T(((in[18] >> 12) | ((in[19] & 0x1) << 20))) + out[27]
	out[29] = T((in[19]>>1)&0x1FFFFF) + out[28]
	out[30] = T(((in[19] >> 22) | ((in[20] & 0x7FF) << 10))) + out[29]
	out[31] = T((in[20] >> 11)) + out[30]
}

func deltaunpack32_22[T uint32 | int32](initoffset T, in *[22]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x3FFFFF) + initoffset
	out[1] = T(((in[0] >> 22) | ((in[1] & 0xFFF) << 10))) + out[0]
	out[2] = T(((in[1] >> 12) | ((in[2] & 0x3) << 20))) + out[1]
	out[3] = T((in[2]>>2)&0x3FFFFF) + out[2]
	out[4] = T(((in[2] >> 24) | ((in[3] & 0x3FFF) << 8))) + out[3]
	out[5] = T(((in[3] >> 14) | ((in[4] & 0xF) << 18))) + out[4]
	out[6] = T((in[4]>>4)&0x3FFFFF) + out[5]
	out[7] = T(((in[4] >> 26) | ((in[5] & 0xFFFF) << 6))) + out[6]
	out[8] = T(((in[5] >> 16) | ((in[6] & 0x3F) << 16))) + out[7]
	out[9] = T((in[6]>>6)&0x3FFFFF) + out[8]
	out[10] = T(((in[6] >> 28) | ((in[7] & 0x3FFFF) << 4))) + out[9]
	out[11] = T(((in[7] >> 18) | ((in[8] & 0xFF) << 14))) + out[10]
	out[12] = T((in[8]>>8)&0x3FFFFF) + out[11]
	out[13] = T(((in[8] >> 30) | ((in[9] & 0xFFFFF) << 2))) + out[12]
	out[14] = T(((in[9] >> 20) | ((in[10] & 0x3FF) << 12))) + out[13]
	out[15] = T((in[10] >> 10)) + out[14]
	out[16] = T((in[11]>>0)&0x3FFFFF) + out[15]
	out[17] = T(((in[11] >> 22) | ((in[12] & 0xFFF) << 10))) + out[16]
	out[18] = T(((in[12] >> 12) | ((in[13] & 0x3) << 20))) + out[17]
	out[19] = T((in[13]>>2)&0x3FFFFF) + out[18]
	out[20] = T(((in[13] >> 24) | ((in[14] & 0x3FFF) << 8))) + out[19]
	out[21] = T(((in[14] >> 14) | ((in[15] & 0xF) << 18))) + out[20]
	out[22] = T((in[15]>>4)&0x3FFFFF) + out[21]
	out[23] = T(((in[15] >> 26) | ((in[16] & 0xFFFF) << 6))) + out[22]
	out[24] = T(((in[16] >> 16) | ((in[17] & 0x3F) << 16))) + out[23]
	out[25] = T((in[17]>>6)&0x3FFFFF) + out[24]
	out[26] = T(((in[17] >> 28) | ((in[18] & 0x3FFFF) << 4))) + out[25]
	out[27] = T(((in[18] >> 18) | ((in[19] & 0xFF) << 14))) + out[26]
	out[28] = T((in[19]>>8)&0x3FFFFF) + out[27]
	out[29] = T(((in[19] >> 30) | ((in[20] & 0xFFFFF) << 2))) + out[28]
	out[30] = T(((in[20] >> 20) | ((in[21] & 0x3FF) << 12))) + out[29]
	out[31] = T((in[21] >> 10)) + out[30]
}

func deltaunpack32_23[T uint32 | int32](initoffset T, in *[23]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x7FFFFF) + initoffset
	out[1] = T(((in[0] >> 23) | ((in[1] & 0x3FFF) << 9))) + out[0]
	out[2] = T(((in[1] >> 14) | ((in[2] & 0x1F) << 18))) + out[1]
	out[3] = T((in[2]>>5)&0x7FFFFF) + out[2]
	out[4] = T(((in[2] >> 28) | ((in[3] & 0x7FFFF) << 4))) + out[3]
	out[5] = T(((in[3] >> 19) | ((in[4] & 0x3FF) << 13))) + out[4]
	out[6] = T(((in[4] >> 10) | ((in[5] & 0x1) << 22))) + out[5]
	out[7] = T((in[5]>>1)&0x7FFFFF) + out[6]
	out[8] = T(((in[5] >> 24) | ((in[6] & 0x7FFF) << 8))) + out[7]
	out[9] = T(((in[6] >> 15) | ((in[7] & 0x3F) << 17))) + out[8]
	out[10] = T((in[7]>>6)&0x7FFFFF) + out[9]
	out[11] = T(((in[7] >> 29) | ((in[8] & 0xFFFFF) << 3))) + out[10]
	out[12] = T(((in[8] >> 20) | ((in[9] & 0x7FF) << 12))) + out[11]
	out[13] = T(((in[9] >> 11) | ((in[10] & 0x3) << 21))) + out[12]
	out[14] = T((in[10]>>2)&0x7FFFFF) + out[13]
	out[15] = T(((in[10] >> 25) | ((in[11] & 0xFFFF) << 7))) + out[14]
	out[16] = T(((in[11] >> 16) | ((in[12] & 0x7F) << 16))) + out[15]
	out[17] = T((in[12]>>7)&0x7FFFFF) + out[16]
	out[18] = T(((in[12] >> 30) | ((in[13] & 0x1FFFFF) << 2))) + out[17]
	out[19] = T(((in[13] >> 21) | ((in[14] & 0xFFF) << 11))) + out[18]
	out[20] = T(((in[14] >> 12) | ((in[15] & 0x7) << 20))) + out[19]
	out[21] = T((in[15]>>3)&0x7FFFFF) + out[20]
	out[22] = T(((in[15] >> 26) | ((in[16] & 0x1FFFF) << 6))) + out[21]
	out[23] = T(((in[16] >> 17) | ((in[17] & 0xFF) << 15))) + out[22]
	out[24] = T((in[17]>>8)&0x7FFFFF) + out[23]
	out[25] = T(((in[17] >> 31) | ((in[18] & 0x3FFFFF) << 1))) + out[24]
	out[26] = T(((in[18] >> 22) | ((in[19] & 0x1FFF) << 10))) + out[25]
	out[27] = T(((in[19] >> 13) | ((in[20] & 0xF) << 19))) + out[26]
	out[28] = T((in[20]>>4)&0x7FFFFF) + out[27]
	out[29] = T(((in[20] >> 27) | ((in[21] & 0x3FFFF) << 5))) + out[28]
	out[30] = T(((in[21] >> 18) | ((in[22] & 0x1FF) << 14))) + out[29]
	out[31] = T((in[22] >> 9)) + out[30]
}

func deltaunpack32_24[T uint32 | int32](initoffset T, in *[24]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0xFFFFFF) + initoffset
	out[1] = T(((in[0] >> 24) | ((in[1] & 0xFFFF) << 8))) + out[0]
	out[2] = T(((in[1] >> 16) | ((in[2] & 0xFF) << 16))) + out[1]
	out[3] = T((in[2] >> 8)) + out[2]
	out[4] = T((in[3]>>0)&0xFFFFFF) + out[3]
	out[5] = T(((in[3] >> 24) | ((in[4] & 0xFFFF) << 8))) + out[4]
	out[6] = T(((in[4] >> 16) | ((in[5] & 0xFF) << 16))) + out[5]
	out[7] = T((in[5] >> 8)) + out[6]
	out[8] = T((in[6]>>0)&0xFFFFFF) + out[7]
	out[9] = T(((in[6] >> 24) | ((in[7] & 0xFFFF) << 8))) + out[8]
	out[10] = T(((in[7] >> 16) | ((in[8] & 0xFF) << 16))) + out[9]
	out[11] = T((in[8] >> 8)) + out[10]
	out[12] = T((in[9]>>0)&0xFFFFFF) + out[11]
	out[13] = T(((in[9] >> 24) | ((in[10] & 0xFFFF) << 8))) + out[12]
	out[14] = T(((in[10] >> 16) | ((in[11] & 0xFF) << 16))) + out[13]
	out[15] = T((in[11] >> 8)) + out[14]
	out[16] = T((in[12]>>0)&0xFFFFFF) + out[15]
	out[17] = T(((in[12] >> 24) | ((in[13] & 0xFFFF) << 8))) + out[16]
	out[18] = T(((in[13] >> 16) | ((in[14] & 0xFF) << 16))) + out[17]
	out[19] = T((in[14] >> 8)) + out[18]
	out[20] = T((in[15]>>0)&0xFFFFFF) + out[19]
	out[21] = T(((in[15] >> 24) | ((in[16] & 0xFFFF) << 8))) + out[20]
	out[22] = T(((in[16] >> 16) | ((in[17] & 0xFF) << 16))) + out[21]
	out[23] = T((in[17] >> 8)) + out[22]
	out[24] = T((in[18]>>0)&0xFFFFFF) + out[23]
	out[25] = T(((in[18] >> 24) | ((in[19] & 0xFFFF) << 8))) + out[24]
	out[26] = T(((in[19] >> 16) | ((in[20] & 0xFF) << 16))) + out[25]
	out[27] = T((in[20] >> 8)) + out[26]
	out[28] = T((in[21]>>0)&0xFFFFFF) + out[27]
	out[29] = T(((in[21] >> 24) | ((in[22] & 0xFFFF) << 8))) + out[28]
	out[30] = T(((in[22] >> 16) | ((in[23] & 0xFF) << 16))) + out[29]
	out[31] = T((in[23] >> 8)) + out[30]
}

func deltaunpack32_25[T uint32 | int32](initoffset T, in *[25]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x1FFFFFF) + initoffset
	out[1] = T(((in[0] >> 25) | ((in[1] & 0x3FFFF) << 7))) + out[0]
	out[2] = T(((in[1] >> 18) | ((in[2] & 0x7FF) << 14))) + out[1]
	out[3] = T(((in[2] >> 11) | ((in[3] & 0xF) << 21))) + out[2]
	out[4] = T((in[3]>>4)&0x1FFFFFF) + out[3]
	out[5] = T(((in[3] >> 29) | ((in[4] & 0x3FFFFF) << 3))) + out[4]
	out[6] = T(((in[4] >> 22) | ((in[5] & 0x7FFF) << 10))) + out[5]
	out[7] = T(((in[5] >> 15) | ((in[6] & 0xFF) << 17))) + out[6]
	out[8] = T(((in[6] >> 8) | ((in[7] & 0x1) << 24))) + out[7]
	out[9] = T((in[7]>>1)&0x1FFFFFF) + out[8]
	out[10] = T(((in[7] >> 26) | ((in[8] & 0x7FFFF) << 6))) + out[9]
	out[11] = T(((in[8] >> 19) | ((in[9] & 0xFFF) << 13))) + out[10]
	out[12] = T(((in[9] >> 12) | ((in[10] & 0x1F) << 20))) + out[11]
	out[13] = T((in[10]>>5)&0x1FFFFFF) + out[12]
	out[14] = T(((in[10] >> 30) | ((in[11] & 0x7FFFFF) << 2))) + out[13]
	out[15] = T(((in[11] >> 23) | ((in[12] & 0xFFFF) << 9))) + out[14]
	out[16] = T(((in[12] >> 16) | ((in[13] & 0x1FF) << 16))) + out[15]
	out[17] = T(((in[13] >> 9) | ((in[14] & 0x3) << 23))) + out[16]
	out[18] = T((in[14]>>2)&0x1FFFFFF) + out[17]
	out[19] = T(((in[14] >> 27) | ((in[15] & 0xFFFFF) << 5))) + out[18]
	out[20] = T(((in[15] >> 20) | ((in[16] & 0x1FFF) << 12))) + out[19]
	out[21] = T(((in[16] >> 13) | ((in[17] & 0x3F) << 19))) + out[20]
	out[22] = T((in[17]>>6)&0x1FFFFFF) + out[21]
	out[23] = T(((in[17] >> 31) | ((in[18] & 0xFFFFFF) << 1))) + out[22]
	out[24] = T(((in[18] >> 24) | ((in[19] & 0x1FFFF) << 8))) + out[23]
	out[25] = T(((in[19] >> 17) | ((in[20] & 0x3FF) << 15))) + out[24]
	out[26] = T(((in[20] >> 10) | ((in[21] & 0x7) << 22))) + out[25]
	out[27] = T((in[21]>>3)&0x1FFFFFF) + out[26]
	out[28] = T(((in[21] >> 28) | ((in[22] & 0x1FFFFF) << 4))) + out[27]
	out[29] = T(((in[22] >> 21) | ((in[23] & 0x3FFF) << 11))) + out[28]
	out[30] = T(((in[23] >> 14) | ((in[24] & 0x7F) << 18))) + out[29]
	out[31] = T((in[24] >> 7)) + out[30]
}

func deltaunpack32_26[T uint32 | int32](initoffset T, in *[26]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x3FFFFFF) + initoffset
	out[1] = T(((in[0] >> 26) | ((in[1] & 0xFFFFF) << 6))) + out[0]
	out[2] = T(((in[1] >> 20) | ((in[2] & 0x3FFF) << 12))) + out[1]
	out[3] = T(((in[2] >> 14) | ((in[3] & 0xFF) << 18))) + out[2]
	out[4] = T(((in[3] >> 8) | ((in[4] & 0x3) << 24))) + out[3]
	out[5] = T((in[4]>>2)&0x3FFFFFF) + out[4]
	out[6] = T(((in[4] >> 28) | ((in[5] & 0x3FFFFF) << 4))) + out[5]
	out[7] = T(((in[5] >> 22) | ((in[6] & 0xFFFF) << 10))) + out[6]
	out[8] = T(((in[6] >> 16) | ((in[7] & 0x3FF) << 16))) + out[7]
	out[9] = T(((in[7] >> 10) | ((in[8] & 0xF) << 22))) + out[8]
	out[10] = T((in[8]>>4)&0x3FFFFFF) + out[9]
	out[11] = T(((in[8] >> 30) | ((in[9] & 0xFFFFFF) << 2))) + out[10]
	out[12] = T(((in[9] >> 24) | ((in[10] & 0x3FFFF) << 8))) + out[11]
	out[13] = T(((in[10] >> 18) | ((in[11] & 0xFFF) << 14))) + out[12]
	out[14] = T(((in[11] >> 12) | ((in[12] & 0x3F) << 20))) + out[13]
	out[15] = T((in[12] >> 6)) + out[14]
	out[16] = T((in[13]>>0)&0x3FFFFFF) + out[15]
	out[17] = T(((in[13] >> 26) | ((in[14] & 0xFFFFF) << 6))) + out[16]
	out[18] = T(((in[14] >> 20) | ((in[15] & 0x3FFF) << 12))) + out[17]
	out[19] = T(((in[15] >> 14) | ((in[16] & 0xFF) << 18))) + out[18]
	out[20] = T(((in[16] >> 8) | ((in[17] & 0x3) << 24))) + out[19]
	out[21] = T((in[17]>>2)&0x3FFFFFF) + out[20]
	out[22] = T(((in[17] >> 28) | ((in[18] & 0x3FFFFF) << 4))) + out[21]
	out[23] = T(((in[18] >> 22) | ((in[19] & 0xFFFF) << 10))) + out[22]
	out[24] = T(((in[19] >> 16) | ((in[20] & 0x3FF) << 16))) + out[23]
	out[25] = T(((in[20] >> 10) | ((in[21] & 0xF) << 22))) + out[24]
	out[26] = T((in[21]>>4)&0x3FFFFFF) + out[25]
	out[27] = T(((in[21] >> 30) | ((in[22] & 0xFFFFFF) << 2))) + out[26]
	out[28] = T(((in[22] >> 24) | ((in[23] & 0x3FFFF) << 8))) + out[27]
	out[29] = T(((in[23] >> 18) | ((in[24] & 0xFFF) << 14))) + out[28]
	out[30] = T(((in[24] >> 12) | ((in[25] & 0x3F) << 20))) + out[29]
	out[31] = T((in[25] >> 6)) + out[30]
}

func deltaunpack32_27[T uint32 | int32](initoffset T, in *[27]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x7FFFFFF) + initoffset
	out[1] = T(((in[0] >> 27) | ((in[1] & 0x3FFFFF) << 5))) + out[0]
	out[2] = T(((in[1] >> 22) | ((in[2] & 0x1FFFF) << 10))) + out[1]
	out[3] = T(((in[2] >> 17) | ((in[3] & 0xFFF) << 15))) + out[2]
	out[4] = T(((in[3] >> 12) | ((in[4] & 0x7F) << 20))) + out[3]
	out[5] = T(((in[4] >> 7) | ((in[5] & 0x3) << 25))) + out[4]
	out[6] = T((in[5]>>2)&0x7FFFFFF) + out[5]
	out[7] = T(((in[5] >> 29) | ((in[6] & 0xFFFFFF) << 3))) + out[6]
	out[8] = T(((in[6] >> 24) | ((in[7] & 0x7FFFF) << 8))) + out[7]
	out[9] = T(((in[7] >> 19) | ((in[8] & 0x3FFF) << 13))) + out[8]
	out[10] = T(((in[8] >> 14) | ((in[9] & 0x1FF) << 18))) + out[9]
	out[11] = T(((in[9] >> 9) | ((in[10] & 0xF) << 23))) + out[10]
	out[12] = T((in[10]>>4)&0x7FFFFFF) + out[11]
	out[13] = T(((in[10] >> 31) | ((in[11] & 0x3FFFFFF) << 1))) + out[12]
	out[14] = T(((in[11] >> 26) | ((in[12] & 0x1FFFFF) << 6))) + out[13]
	out[15] = T(((in[12] >> 21) | ((in[13] & 0xFFFF) << 11))) + out[14]
	out[16] = T(((in[13] >> 16) | ((in[14] & 0x7FF) << 16))) + out[15]
	out[17] = T(((in[14] >> 11) | ((in[15] & 0x3F) << 21))) + out[16]
	out[18] = T(((in[15] >> 6) | ((in[16] & 0x1) << 26))) + out[17]
	out[19] = T((in[16]>>1)&0x7FFFFFF) + out[18]
	out[20] = T(((in[16] >> 28) | ((in[17] & 0x7FFFFF) << 4))) + out[19]
	out[21] = T(((in[17] >> 23) | ((in[18] & 0x3FFFF) << 9))) + out[20]
	out[22] = T(((in[18] >> 18) | ((in[19] & 0x1FFF) << 14))) + out[21]
	out[23] = T(((in[19] >> 13) | ((in[20] & 0xFF) << 19))) + out[22]
	out[24] = T(((in[20] >> 8) | ((in[21] & 0x7) << 24))) + out[23]
	out[25] = T((in[21]>>3)&0x7FFFFFF) + out[24]
	out[26] = T(((in[21] >> 30) | ((in[22] & 0x1FFFFFF) << 2))) + out[25]
	out[27] = T(((in[22] >> 25) | ((in[23] & 0xFFFFF) << 7))) + out[26]
	out[28] = T(((in[23] >> 20) | ((in[24] & 0x7FFF) << 12))) + out[27]
	out[29] = T(((in[24] >> 15) | ((in[25] & 0x3FF) << 17))) + out[28]
	out[30] = T(((in[25] >> 10) | ((in[26] & 0x1F) << 22))) + out[29]
	out[31] = T((in[26] >> 5)) + out[30]
}

func deltaunpack32_28[T uint32 | int32](initoffset T, in *[28]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0xFFFFFFF) + initoffset
	out[1] = T(((in[0] >> 28) | ((in[1] & 0xFFFFFF) << 4))) + out[0]
	out[2] = T(((in[1] >> 24) | ((in[2] & 0xFFFFF) << 8))) + out[1]
	out[3] = T(((in[2] >> 20) | ((in[3] & 0xFFFF) << 12))) + out[2]
	out[4] = T(((in[3] >> 16) | ((in[4] & 0xFFF) << 16))) + out[3]
	out[5] = T(((in[4] >> 12) | ((in[5] & 0xFF) << 20))) + out[4]
	out[6] = T(((in[5] >> 8) | ((in[6] & 0xF) << 24))) + out[5]
	out[7] = T((in[6] >> 4)) + out[6]
	out[8] = T((in[7]>>0)&0xFFFFFFF) + out[7]
	out[9] = T(((in[7] >> 28) | ((in[8] & 0xFFFFFF) << 4))) + out[8]
	out[10] = T(((in[8] >> 24) | ((in[9] & 0xFFFFF) << 8))) + out[9]
	out[11] = T(((in[9] >> 20) | ((in[10] & 0xFFFF) << 12))) + out[10]
	out[12] = T(((in[10] >> 16) | ((in[11] & 0xFFF) << 16))) + out[11]
	out[13] = T(((in[11] >> 12) | ((in[12] & 0xFF) << 20))) + out[12]
	out[14] = T(((in[12] >> 8) | ((in[13] & 0xF) << 24))) + out[13]
	out[15] = T((in[13] >> 4)) + out[14]
	out[16] = T((in[14]>>0)&0xFFFFFFF) + out[15]
	out[17] = T(((in[14] >> 28) | ((in[15] & 0xFFFFFF) << 4))) + out[16]
	out[18] = T(((in[15] >> 24) | ((in[16] & 0xFFFFF) << 8))) + out[17]
	out[19] = T(((in[16] >> 20) | ((in[17] & 0xFFFF) << 12))) + out[18]
	out[20] = T(((in[17] >> 16) | ((in[18] & 0xFFF) << 16))) + out[19]
	out[21] = T(((in[18] >> 12) | ((in[19] & 0xFF) << 20))) + out[20]
	out[22] = T(((in[19] >> 8) | ((in[20] & 0xF) << 24))) + out[21]
	out[23] = T((in[20] >> 4)) + out[22]
	out[24] = T((in[21]>>0)&0xFFFFFFF) + out[23]
	out[25] = T(((in[21] >> 28) | ((in[22] & 0xFFFFFF) << 4))) + out[24]
	out[26] = T(((in[22] >> 24) | ((in[23] & 0xFFFFF) << 8))) + out[25]
	out[27] = T(((in[23] >> 20) | ((in[24] & 0xFFFF) << 12))) + out[26]
	out[28] = T(((in[24] >> 16) | ((in[25] & 0xFFF) << 16))) + out[27]
	out[29] = T(((in[25] >> 12) | ((in[26] & 0xFF) << 20))) + out[28]
	out[30] = T(((in[26] >> 8) | ((in[27] & 0xF) << 24))) + out[29]
	out[31] = T((in[27] >> 4)) + out[30]
}

func deltaunpack32_29[T uint32 | int32](initoffset T, in *[29]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x1FFFFFFF) + initoffset
	out[1] = T(((in[0] >> 29) | ((in[1] & 0x3FFFFFF) << 3))) + out[0]
	out[2] = T(((in[1] >> 26) | ((in[2] & 0x7FFFFF) << 6))) + out[1]
	out[3] = T(((in[2] >> 23) | ((in[3] & 0xFFFFF) << 9))) + out[2]
	out[4] = T(((in[3] >> 20) | ((in[4] & 0x1FFFF) << 12))) + out[3]
	out[5] = T(((in[4] >> 17) | ((in[5] & 0x3FFF) << 15))) + out[4]
	out[6] = T(((in[5] >> 14) | ((in[6] & 0x7FF) << 18))) + out[5]
	out[7] = T(((in[6] >> 11) | ((in[7] & 0xFF) << 21))) + out[6]
	out[8] = T(((in[7] >> 8) | ((in[8] & 0x1F) << 24))) + out[7]
	out[9] = T(((in[8] >> 5) | ((in[9] & 0x3) << 27))) + out[8]
	out[10] = T((in[9]>>2)&0x1FFFFFFF) + out[9]
	out[11] = T(((in[9] >> 31) | ((in[10] & 0xFFFFFFF) << 1))) + out[10]
	out[12] = T(((in[10] >> 28) | ((in[11] & 0x1FFFFFF) << 4))) + out[11]
	out[13] = T(((in[11] >> 25) | ((in[12] & 0x3FFFFF) << 7))) + out[12]
	out[14] = T(((in[12] >> 22) | ((in[13] & 0x7FFFF) << 10))) + out[13]
	out[15] = T(((in[13] >> 19) | ((in[14] & 0xFFFF) << 13))) + out[14]
	out[16] = T(((in[14] >> 16) | ((in[15] & 0x1FFF) << 16))) + out[15]
	out[17] = T(((in[15] >> 13) | ((in[16] & 0x3FF) << 19))) + out[16]
	out[18] = T(((in[16] >> 10) | ((in[17] & 0x7F) << 22))) + out[17]
	out[19] = T(((in[17] >> 7) | ((in[18] & 0xF) << 25))) + out[18]
	out[20] = T(((in[18] >> 4) | ((in[19] & 0x1) << 28))) + out[19]
	out[21] = T((in[19]>>1)&0x1FFFFFFF) + out[20]
	out[22] = T(((in[19] >> 30) | ((in[20] & 0x7FFFFFF) << 2))) + out[21]
	out[23] = T(((in[20] >> 27) | ((in[21] & 0xFFFFFF) << 5))) + out[22]
	out[24] = T(((in[21] >> 24) | ((in[22] & 0x1FFFFF) << 8))) + out[23]
	out[25] = T(((in[22] >> 21) | ((in[23] & 0x3FFFF) << 11))) + out[24]
	out[26] = T(((in[23] >> 18) | ((in[24] & 0x7FFF) << 14))) + out[25]
	out[27] = T(((in[24] >> 15) | ((in[25] & 0xFFF) << 17))) + out[26]
	out[28] = T(((in[25] >> 12) | ((in[26] & 0x1FF) << 20))) + out[27]
	out[29] = T(((in[26] >> 9) | ((in[27] & 0x3F) << 23))) + out[28]
	out[30] = T(((in[27] >> 6) | ((in[28] & 0x7) << 26))) + out[29]
	out[31] = T((in[28] >> 3)) + out[30]
}

func deltaunpack32_30[T uint32 | int32](initoffset T, in *[30]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x3FFFFFFF) + initoffset
	out[1] = T(((in[0] >> 30) | ((in[1] & 0xFFFFFFF) << 2))) + out[0]
	out[2] = T(((in[1] >> 28) | ((in[2] & 0x3FFFFFF) << 4))) + out[1]
	out[3] = T(((in[2] >> 26) | ((in[3] & 0xFFFFFF) << 6))) + out[2]
	out[4] = T(((in[3] >> 24) | ((in[4] & 0x3FFFFF) << 8))) + out[3]
	out[5] = T(((in[4] >> 22) | ((in[5] & 0xFFFFF) << 10))) + out[4]
	out[6] = T(((in[5] >> 20) | ((in[6] & 0x3FFFF) << 12))) + out[5]
	out[7] = T(((in[6] >> 18) | ((in[7] & 0xFFFF) << 14))) + out[6]
	out[8] = T(((in[7] >> 16) | ((in[8] & 0x3FFF) << 16))) + out[7]
	out[9] = T(((in[8] >> 14) | ((in[9] & 0xFFF) << 18))) + out[8]
	out[10] = T(((in[9] >> 12) | ((in[10] & 0x3FF) << 20))) + out[9]
	out[11] = T(((in[10] >> 10) | ((in[11] & 0xFF) << 22))) + out[10]
	out[12] = T(((in[11] >> 8) | ((in[12] & 0x3F) << 24))) + out[11]
	out[13] = T(((in[12] >> 6) | ((in[13] & 0xF) << 26))) + out[12]
	out[14] = T(((in[13] >> 4) | ((in[14] & 0x3) << 28))) + out[13]
	out[15] = T((in[14] >> 2)) + out[14]
	out[16] = T((in[15]>>0)&0x3FFFFFFF) + out[15]
	out[17] = T(((in[15] >> 30) | ((in[16] & 0xFFFFFFF) << 2))) + out[16]
	out[18] = T(((in[16] >> 28) | ((in[17] & 0x3FFFFFF) << 4))) + out[17]
	out[19] = T(((in[17] >> 26) | ((in[18] & 0xFFFFFF) << 6))) + out[18]
	out[20] = T(((in[18] >> 24) | ((in[19] & 0x3FFFFF) << 8))) + out[19]
	out[21] = T(((in[19] >> 22) | ((in[20] & 0xFFFFF) << 10))) + out[20]
	out[22] = T(((in[20] >> 20) | ((in[21] & 0x3FFFF) << 12))) + out[21]
	out[23] = T(((in[21] >> 18) | ((in[22] & 0xFFFF) << 14))) + out[22]
	out[24] = T(((in[22] >> 16) | ((in[23] & 0x3FFF) << 16))) + out[23]
	out[25] = T(((in[23] >> 14) | ((in[24] & 0xFFF) << 18))) + out[24]
	out[26] = T(((in[24] >> 12) | ((in[25] & 0x3FF) << 20))) + out[25]
	out[27] = T(((in[25] >> 10) | ((in[26] & 0xFF) << 22))) + out[26]
	out[28] = T(((in[26] >> 8) | ((in[27] & 0x3F) << 24))) + out[27]
	out[29] = T(((in[27] >> 6) | ((in[28] & 0xF) << 26))) + out[28]
	out[30] = T(((in[28] >> 4) | ((in[29] & 0x3) << 28))) + out[29]
	out[31] = T((in[29] >> 2)) + out[30]
}

func deltaunpack32_31[T uint32 | int32](initoffset T, in *[31]uint32, out *[32]T) {
	out[0] = T((in[0]>>0)&0x7FFFFFFF) + initoffset
	out[1] = T(((in[0] >> 31) | ((in[1] & 0x3FFFFFFF) << 1))) + out[0]
	out[2] = T(((in[1] >> 30) | ((in[2] & 0x1FFFFFFF) << 2))) + out[1]
	out[3] = T(((in[2] >> 29) | ((in[3] & 0xFFFFFFF) << 3))) + out[2]
	out[4] = T(((in[3] >> 28) | ((in[4] & 0x7FFFFFF) << 4))) + out[3]
	out[5] = T(((in[4] >> 27) | ((in[5] & 0x3FFFFFF) << 5))) + out[4]
	out[6] = T(((in[5] >> 26) | ((in[6] & 0x1FFFFFF) << 6))) + out[5]
	out[7] = T(((in[6] >> 25) | ((in[7] & 0xFFFFFF) << 7))) + out[6]
	out[8] = T(((in[7] >> 24) | ((in[8] & 0x7FFFFF) << 8))) + out[7]
	out[9] = T(((in[8] >> 23) | ((in[9] & 0x3FFFFF) << 9))) + out[8]
	out[10] = T(((in[9] >> 22) | ((in[10] & 0x1FFFFF) << 10))) + out[9]
	out[11] = T(((in[10] >> 21) | ((in[11] & 0xFFFFF) << 11))) + out[10]
	out[12] = T(((in[11] >> 20) | ((in[12] & 0x7FFFF) << 12))) + out[11]
	out[13] = T(((in[12] >> 19) | ((in[13] & 0x3FFFF) << 13))) + out[12]
	out[14] = T(((in[13] >> 18) | ((in[14] & 0x1FFFF) << 14))) + out[13]
	out[15] = T(((in[14] >> 17) | ((in[15] & 0xFFFF) << 15))) + out[14]
	out[16] = T(((in[15] >> 16) | ((in[16] & 0x7FFF) << 16))) + out[15]
	out[17] = T(((in[16] >> 15) | ((in[17] & 0x3FFF) << 17))) + out[16]
	out[18] = T(((in[17] >> 14) | ((in[18] & 0x1FFF) << 18))) + out[17]
	out[19] = T(((in[18] >> 13) | ((in[19] & 0xFFF) << 19))) + out[18]
	out[20] = T(((in[19] >> 12) | ((in[20] & 0x7FF) << 20))) + out[19]
	out[21] = T(((in[20] >> 11) | ((in[21] & 0x3FF) << 21))) + out[20]
	out[22] = T(((in[21] >> 10) | ((in[22] & 0x1FF) << 22))) + out[21]
	out[23] = T(((in[22] >> 9) | ((in[23] & 0xFF) << 23))) + out[22]
	out[24] = T(((in[23] >> 8) | ((in[24] & 0x7F) << 24))) + out[23]
	out[25] = T(((in[24] >> 7) | ((in[25] & 0x3F) << 25))) + out[24]
	out[26] = T(((in[25] >> 6) | ((in[26] & 0x1F) << 26))) + out[25]
	out[27] = T(((in[26] >> 5) | ((in[27] & 0xF) << 27))) + out[26]
	out[28] = T(((in[27] >> 4) | ((in[28] & 0x7) << 28))) + out[27]
	out[29] = T(((in[28] >> 3) | ((in[29] & 0x3) << 29))) + out[28]
	out[30] = T(((in[29] >> 2) | ((in[30] & 0x1) << 30))) + out[29]
	out[31] = T((in[30] >> 1)) + out[30]
}

// --- zigzag

// deltaPackZigzag_int32 Binary packing of one block of `in`, starting from `initoffset`
// to out. Differential coding is applied first, the difference is zigzag encoded.
//
//	Caller must give the proper `bitlen` of the block
func deltaPackZigzag_int32(initoffset int32, in []int32, out []uint32, bitlen int) {
	switch bitlen {
	case 0:
		deltapackzigzag32_0(initoffset, (*[32]int32)(in), (*[0]uint32)(out))
	case 1:
		deltapackzigzag32_1(initoffset, (*[32]int32)(in), (*[1]uint32)(out))
	case 2:
		deltapackzigzag32_2(initoffset, (*[32]int32)(in), (*[2]uint32)(out))
	case 3:
		deltapackzigzag32_3(initoffset, (*[32]int32)(in), (*[3]uint32)(out))
	case 4:
		deltapackzigzag32_4(initoffset, (*[32]int32)(in), (*[4]uint32)(out))
	case 5:
		deltapackzigzag32_5(initoffset, (*[32]int32)(in), (*[5]uint32)(out))
	case 6:
		deltapackzigzag32_6(initoffset, (*[32]int32)(in), (*[6]uint32)(out))
	case 7:
		deltapackzigzag32_7(initoffset, (*[32]int32)(in), (*[7]uint32)(out))
	case 8:
		deltapackzigzag32_8(initoffset, (*[32]int32)(in), (*[8]uint32)(out))
	case 9:
		deltapackzigzag32_9(initoffset, (*[32]int32)(in), (*[9]uint32)(out))
	case 10:
		deltapackzigzag32_10(initoffset, (*[32]int32)(in), (*[10]uint32)(out))
	case 11:
		deltapackzigzag32_11(initoffset, (*[32]int32)(in), (*[11]uint32)(out))
	case 12:
		deltapackzigzag32_12(initoffset, (*[32]int32)(in), (*[12]uint32)(out))
	case 13:
		deltapackzigzag32_13(initoffset, (*[32]int32)(in), (*[13]uint32)(out))
	case 14:
		deltapackzigzag32_14(initoffset, (*[32]int32)(in), (*[14]uint32)(out))
	case 15:
		deltapackzigzag32_15(initoffset, (*[32]int32)(in), (*[15]uint32)(out))
	case 16:
		deltapackzigzag32_16(initoffset, (*[32]int32)(in), (*[16]uint32)(out))
	case 17:
		deltapackzigzag32_17(initoffset, (*[32]int32)(in), (*[17]uint32)(out))
	case 18:
		deltapackzigzag32_18(initoffset, (*[32]int32)(in), (*[18]uint32)(out))
	case 19:
		deltapackzigzag32_19(initoffset, (*[32]int32)(in), (*[19]uint32)(out))
	case 20:
		deltapackzigzag32_20(initoffset, (*[32]int32)(in), (*[20]uint32)(out))
	case 21:
		deltapackzigzag32_21(initoffset, (*[32]int32)(in), (*[21]uint32)(out))
	case 22:
		deltapackzigzag32_22(initoffset, (*[32]int32)(in), (*[22]uint32)(out))
	case 23:
		deltapackzigzag32_23(initoffset, (*[32]int32)(in), (*[23]uint32)(out))
	case 24:
		deltapackzigzag32_24(initoffset, (*[32]int32)(in), (*[24]uint32)(out))
	case 25:
		deltapackzigzag32_25(initoffset, (*[32]int32)(in), (*[25]uint32)(out))
	case 26:
		deltapackzigzag32_26(initoffset, (*[32]int32)(in), (*[26]uint32)(out))
	case 27:
		deltapackzigzag32_27(initoffset, (*[32]int32)(in), (*[27]uint32)(out))
	case 28:
		deltapackzigzag32_28(initoffset, (*[32]int32)(in), (*[28]uint32)(out))
	case 29:
		deltapackzigzag32_29(initoffset, (*[32]int32)(in), (*[29]uint32)(out))
	case 30:
		deltapackzigzag32_30(initoffset, (*[32]int32)(in), (*[30]uint32)(out))
	case 31:
		deltapackzigzag32_31(initoffset, (*[32]int32)(in), (*[31]uint32)(out))
	case 32:
		*(*[32]uint32)(out) = *((*[32]uint32)(unsafe.Pointer((*[32]int32)(in))))
	default:
		panic("unsupported bitlen")
	}
}

// deltaUnpackZigzag_int32 Decoding operation for DeltaPackZigzag_int32
func deltaUnpackZigzag_int32(initoffset int32, in []uint32, out []int32, bitlen int) {
	switch bitlen {
	case 0:
		deltaunpackzigzag32_0(initoffset, (*[0]uint32)(in), (*[32]int32)(out))
	case 1:
		deltaunpackzigzag32_1(initoffset, (*[1]uint32)(in), (*[32]int32)(out))
	case 2:
		deltaunpackzigzag32_2(initoffset, (*[2]uint32)(in), (*[32]int32)(out))
	case 3:
		deltaunpackzigzag32_3(initoffset, (*[3]uint32)(in), (*[32]int32)(out))
	case 4:
		deltaunpackzigzag32_4(initoffset, (*[4]uint32)(in), (*[32]int32)(out))
	case 5:
		deltaunpackzigzag32_5(initoffset, (*[5]uint32)(in), (*[32]int32)(out))
	case 6:
		deltaunpackzigzag32_6(initoffset, (*[6]uint32)(in), (*[32]int32)(out))
	case 7:
		deltaunpackzigzag32_7(initoffset, (*[7]uint32)(in), (*[32]int32)(out))
	case 8:
		deltaunpackzigzag32_8(initoffset, (*[8]uint32)(in), (*[32]int32)(out))
	case 9:
		deltaunpackzigzag32_9(initoffset, (*[9]uint32)(in), (*[32]int32)(out))
	case 10:
		deltaunpackzigzag32_10(initoffset, (*[10]uint32)(in), (*[32]int32)(out))
	case 11:
		deltaunpackzigzag32_11(initoffset, (*[11]uint32)(in), (*[32]int32)(out))
	case 12:
		deltaunpackzigzag32_12(initoffset, (*[12]uint32)(in), (*[32]int32)(out))
	case 13:
		deltaunpackzigzag32_13(initoffset, (*[13]uint32)(in), (*[32]int32)(out))
	case 14:
		deltaunpackzigzag32_14(initoffset, (*[14]uint32)(in), (*[32]int32)(out))
	case 15:
		deltaunpackzigzag32_15(initoffset, (*[15]uint32)(in), (*[32]int32)(out))
	case 16:
		deltaunpackzigzag32_16(initoffset, (*[16]uint32)(in), (*[32]int32)(out))
	case 17:
		deltaunpackzigzag32_17(initoffset, (*[17]uint32)(in), (*[32]int32)(out))
	case 18:
		deltaunpackzigzag32_18(initoffset, (*[18]uint32)(in), (*[32]int32)(out))
	case 19:
		deltaunpackzigzag32_19(initoffset, (*[19]uint32)(in), (*[32]int32)(out))
	case 20:
		deltaunpackzigzag32_20(initoffset, (*[20]uint32)(in), (*[32]int32)(out))
	case 21:
		deltaunpackzigzag32_21(initoffset, (*[21]uint32)(in), (*[32]int32)(out))
	case 22:
		deltaunpackzigzag32_22(initoffset, (*[22]uint32)(in), (*[32]int32)(out))
	case 23:
		deltaunpackzigzag32_23(initoffset, (*[23]uint32)(in), (*[32]int32)(out))
	case 24:
		deltaunpackzigzag32_24(initoffset, (*[24]uint32)(in), (*[32]int32)(out))
	case 25:
		deltaunpackzigzag32_25(initoffset, (*[25]uint32)(in), (*[32]int32)(out))
	case 26:
		deltaunpackzigzag32_26(initoffset, (*[26]uint32)(in), (*[32]int32)(out))
	case 27:
		deltaunpackzigzag32_27(initoffset, (*[27]uint32)(in), (*[32]int32)(out))
	case 28:
		deltaunpackzigzag32_28(initoffset, (*[28]uint32)(in), (*[32]int32)(out))
	case 29:
		deltaunpackzigzag32_29(initoffset, (*[29]uint32)(in), (*[32]int32)(out))
	case 30:
		deltaunpackzigzag32_30(initoffset, (*[30]uint32)(in), (*[32]int32)(out))
	case 31:
		deltaunpackzigzag32_31(initoffset, (*[31]uint32)(in), (*[32]int32)(out))
	case 32:
		*(*[32]int32)(out) = *(*[32]int32)(unsafe.Pointer((*[32]uint32)(in)))
	default:
		panic("unsupported bitlen")
	}
}

func deltapackzigzag32_0[T uint32 | int32](initoffset T, in *[32]T, out *[0]uint32) {
}

func deltapackzigzag32_1[T uint32 | int32](initoffset T, in *[32]T, out *[1]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 1) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 2) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 3) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 4) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 5) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 6) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 7) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 8) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 9) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 10) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 11) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 12) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 13) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 14) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 15) |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 17) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 18) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 19) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 20) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 21) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 22) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 23) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 24) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 25) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 26) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 27) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 28) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 29) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 30) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 31))
}

func deltapackzigzag32_2[T uint32 | int32](initoffset T, in *[32]T, out *[2]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 2) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 4) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 6) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 8) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 10) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 12) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 14) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 16) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 18) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 20) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 22) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 24) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 26) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 28) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 30))
	out[1] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 2) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 4) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 6) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 8) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 10) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 12) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 14) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 16) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 18) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 20) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 22) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 24) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 26) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 28) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 30))
}

func deltapackzigzag32_3[T uint32 | int32](initoffset T, in *[32]T, out *[3]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 3) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 6) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 9) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 12) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 15) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 18) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 21) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 24) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 27) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 30))
	out[1] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>2 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 1) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 4) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 7) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 10) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 13) |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 19) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 22) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 25) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 28) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 31))
	out[2] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>1 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 2) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 5) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 8) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 11) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 14) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 17) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 20) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 23) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 26) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 29))
}

func deltapackzigzag32_4[T uint32 | int32](initoffset T, in *[32]T, out *[4]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 4) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 8) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 12) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 16) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 20) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 24) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 28))
	out[1] = uint32(
		((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 4) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 8) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 12) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 16) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 20) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 24) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 28))
	out[2] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 4) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 8) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 12) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 16) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 20) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 24) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 28))
	out[3] = uint32(
		((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 4) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 8) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 12) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 16) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 20) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 24) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 28))
}

func deltapackzigzag32_5[T uint32 | int32](initoffset T, in *[32]T, out *[5]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 5) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 10) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 15) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 20) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 25) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 30))
	out[1] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>2 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 3) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 8) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 13) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 18) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 23) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 28))
	out[2] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>4 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 1) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 6) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 11) |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 21) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 26) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 31))
	out[3] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>1 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 4) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 9) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 14) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 19) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 24) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 29))
	out[4] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>3 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 2) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 7) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 12) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 17) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 22) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 27))
}

func deltapackzigzag32_6[T uint32 | int32](initoffset T, in *[32]T, out *[6]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 6) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 12) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 18) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 24) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 30))
	out[1] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>2 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 4) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 10) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 16) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 22) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 28))
	out[2] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>4 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 2) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 8) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 14) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 20) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 26))
	out[3] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 6) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 12) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 18) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 24) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 30))
	out[4] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>2 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 4) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 10) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 16) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 22) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 28))
	out[5] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>4 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 2) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 8) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 14) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 20) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 26))
}

func deltapackzigzag32_7[T uint32 | int32](initoffset T, in *[32]T, out *[7]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 7) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 14) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 21) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 28))
	out[1] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>4 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 3) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 10) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 17) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 24) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 31))
	out[2] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>1 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 6) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 13) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 20) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 27))
	out[3] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>5 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 2) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 9) |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 23) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 30))
	out[4] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>2 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 5) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 12) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 19) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 26))
	out[5] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>6 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 1) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 8) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 15) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 22) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 29))
	out[6] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>3 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 4) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 11) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 18) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 25))
}

func deltapackzigzag32_8[T uint32 | int32](initoffset T, in *[32]T, out *[8]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 8) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 16) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 24))
	out[1] = uint32(
		((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 8) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 16) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 24))
	out[2] = uint32(
		((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 8) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 16) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 24))
	out[3] = uint32(
		((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 8) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 16) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 24))
	out[4] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 8) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 16) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 24))
	out[5] = uint32(
		((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 8) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 16) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 24))
	out[6] = uint32(
		((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 8) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 16) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 24))
	out[7] = uint32(
		((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 8) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 16) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 24))
}

func deltapackzigzag32_9[T uint32 | int32](initoffset T, in *[32]T, out *[9]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 9) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 18) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 27))
	out[1] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>5 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 4) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 13) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 22) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 31))
	out[2] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>1 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 8) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 17) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 26))
	out[3] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>6 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 3) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 12) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 21) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 30))
	out[4] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>2 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 7) |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 25))
	out[5] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>7 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 2) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 11) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 20) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 29))
	out[6] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>3 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 6) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 15) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 24))
	out[7] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>8 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 1) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 10) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 19) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 28))
	out[8] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>4 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 5) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 14) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 23))
}

func deltapackzigzag32_10[T uint32 | int32](initoffset T, in *[32]T, out *[10]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 10) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 20) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 30))
	out[1] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>2 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 8) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 18) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 28))
	out[2] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>4 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 6) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 16) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 26))
	out[3] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>6 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 4) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 14) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 24))
	out[4] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>8 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 2) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 12) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 22))
	out[5] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 10) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 20) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 30))
	out[6] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>2 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 8) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 18) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 28))
	out[7] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>4 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 6) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 16) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 26))
	out[8] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>6 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 4) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 14) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 24))
	out[9] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>8 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 2) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 12) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 22))
}

func deltapackzigzag32_11[T uint32 | int32](initoffset T, in *[32]T, out *[11]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 11) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 22))
	out[1] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>10 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 1) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 12) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 23))
	out[2] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>9 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 2) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 13) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 24))
	out[3] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>8 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 3) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 14) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 25))
	out[4] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>7 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 4) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 15) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 26))
	out[5] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>6 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 5) |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 27))
	out[6] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>5 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 6) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 17) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 28))
	out[7] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>4 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 7) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 18) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 29))
	out[8] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>3 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 8) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 19) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 30))
	out[9] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>2 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 9) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 20) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 31))
	out[10] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>1 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 10) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 21))
}

func deltapackzigzag32_12[T uint32 | int32](initoffset T, in *[32]T, out *[12]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 12) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 24))
	out[1] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>8 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 4) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 16) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 28))
	out[2] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>4 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 8) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 20))
	out[3] = uint32(
		((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 12) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 24))
	out[4] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>8 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 4) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 16) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 28))
	out[5] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>4 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 8) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 20))
	out[6] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 12) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 24))
	out[7] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>8 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 4) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 16) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 28))
	out[8] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>4 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 8) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 20))
	out[9] = uint32(
		((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 12) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 24))
	out[10] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>8 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 4) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 16) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 28))
	out[11] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>4 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 8) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 20))
}

func deltapackzigzag32_13[T uint32 | int32](initoffset T, in *[32]T, out *[13]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 13) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 26))
	out[1] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>6 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 7) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 20))
	out[2] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>12 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 1) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 14) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 27))
	out[3] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>5 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 8) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 21))
	out[4] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>11 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 2) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 15) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 28))
	out[5] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>4 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 9) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 22))
	out[6] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>10 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 3) |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 29))
	out[7] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>3 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 10) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 23))
	out[8] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>9 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 4) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 17) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 30))
	out[9] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>2 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 11) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 24))
	out[10] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>8 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 5) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 18) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 31))
	out[11] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>1 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 12) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 25))
	out[12] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>7 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 6) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 19))
}

func deltapackzigzag32_14[T uint32 | int32](initoffset T, in *[32]T, out *[14]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 14) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 28))
	out[1] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>4 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 10) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 24))
	out[2] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>8 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 6) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 20))
	out[3] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>12 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 2) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 16) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 30))
	out[4] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>2 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 12) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 26))
	out[5] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>6 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 8) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 22))
	out[6] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>10 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 4) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 18))
	out[7] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 14) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 28))
	out[8] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>4 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 10) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 24))
	out[9] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>8 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 6) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 20))
	out[10] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>12 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 2) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 16) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 30))
	out[11] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>2 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 12) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 26))
	out[12] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>6 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 8) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 22))
	out[13] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>10 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 4) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 18))
}

func deltapackzigzag32_15[T uint32 | int32](initoffset T, in *[32]T, out *[15]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 15) |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 30))
	out[1] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>2 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 13) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 28))
	out[2] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>4 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 11) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 26))
	out[3] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>6 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 9) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 24))
	out[4] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>8 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 7) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 22))
	out[5] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>10 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 5) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 20))
	out[6] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>12 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 3) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 18))
	out[7] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>14 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 1) |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 31))
	out[8] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>1 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 14) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 29))
	out[9] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>3 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 12) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 27))
	out[10] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>5 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 10) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 25))
	out[11] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>7 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 8) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 23))
	out[12] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>9 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 6) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 21))
	out[13] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>11 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 4) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 19))
	out[14] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>13 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 2) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 17))
}

func deltapackzigzag32_16[T uint32 | int32](initoffset T, in *[32]T, out *[16]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 16))
	out[1] = uint32(
		((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 16))
	out[2] = uint32(
		((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 16))
	out[3] = uint32(
		((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 16))
	out[4] = uint32(
		((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 16))
	out[5] = uint32(
		((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 16))
	out[6] = uint32(
		((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 16))
	out[7] = uint32(
		((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 16))
	out[8] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 16))
	out[9] = uint32(
		((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 16))
	out[10] = uint32(
		((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 16))
	out[11] = uint32(
		((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 16))
	out[12] = uint32(
		((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 16))
	out[13] = uint32(
		((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 16))
	out[14] = uint32(
		((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 16))
	out[15] = uint32(
		((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31) |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 16))
}

func deltapackzigzag32_17[T uint32 | int32](initoffset T, in *[32]T, out *[17]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 17))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>15 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 2) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 19))
	out[2] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>13 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 4) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 21))
	out[3] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>11 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 6) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 23))
	out[4] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>9 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 8) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 25))
	out[5] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>7 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 10) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 27))
	out[6] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>5 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 12) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 29))
	out[7] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>3 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 14) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 31))
	out[8] = uint32(
		(((int32(in[15]-in[14]))<<1)^((int32(in[15]-in[14]))>>31))>>1 |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16))
	out[9] = uint32(
		(((int32(in[16]-in[15]))<<1)^((int32(in[16]-in[15]))>>31))>>16 |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 1) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 18))
	out[10] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>14 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 3) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 20))
	out[11] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>12 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 5) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 22))
	out[12] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>10 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 7) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 24))
	out[13] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>8 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 9) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 26))
	out[14] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>6 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 11) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 28))
	out[15] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>4 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 13) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 30))
	out[16] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>2 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 15))
}

func deltapackzigzag32_18[T uint32 | int32](initoffset T, in *[32]T, out *[18]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 18))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>14 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 4) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 22))
	out[2] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>10 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 8) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 26))
	out[3] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>6 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 12) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 30))
	out[4] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>2 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 16))
	out[5] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>16 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 2) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 20))
	out[6] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>12 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 6) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 24))
	out[7] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>8 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 10) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 28))
	out[8] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>4 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 14))
	out[9] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 18))
	out[10] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>14 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 4) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 22))
	out[11] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>10 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 8) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 26))
	out[12] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>6 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 12) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 30))
	out[13] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>2 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 16))
	out[14] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>16 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 2) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 20))
	out[15] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>12 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 6) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 24))
	out[16] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>8 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 10) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 28))
	out[17] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>4 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 14))
}

func deltapackzigzag32_19[T uint32 | int32](initoffset T, in *[32]T, out *[19]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 19))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>13 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 6) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 25))
	out[2] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>7 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 12) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 31))
	out[3] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>1 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 18))
	out[4] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>14 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 5) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 24))
	out[5] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>8 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 11) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 30))
	out[6] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>2 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 17))
	out[7] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>15 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 4) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 23))
	out[8] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>9 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 10) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 29))
	out[9] = uint32(
		(((int32(in[15]-in[14]))<<1)^((int32(in[15]-in[14]))>>31))>>3 |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16))
	out[10] = uint32(
		(((int32(in[16]-in[15]))<<1)^((int32(in[16]-in[15]))>>31))>>16 |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 3) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 22))
	out[11] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>10 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 9) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 28))
	out[12] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>4 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 15))
	out[13] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>17 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 2) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 21))
	out[14] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>11 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 8) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 27))
	out[15] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>5 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 14))
	out[16] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>18 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 1) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 20))
	out[17] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>12 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 7) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 26))
	out[18] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>6 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 13))
}

func deltapackzigzag32_20[T uint32 | int32](initoffset T, in *[32]T, out *[20]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 20))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>12 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 8) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 28))
	out[2] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>4 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 16))
	out[3] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>16 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 4) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 24))
	out[4] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>8 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 12))
	out[5] = uint32(
		((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 20))
	out[6] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>12 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 8) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 28))
	out[7] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>4 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 16))
	out[8] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>16 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 4) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 24))
	out[9] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>8 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 12))
	out[10] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 20))
	out[11] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>12 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 8) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 28))
	out[12] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>4 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 16))
	out[13] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>16 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 4) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 24))
	out[14] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>8 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 12))
	out[15] = uint32(
		((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 20))
	out[16] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>12 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 8) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 28))
	out[17] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>4 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 16))
	out[18] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>16 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 4) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 24))
	out[19] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>8 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 12))
}

func deltapackzigzag32_21[T uint32 | int32](initoffset T, in *[32]T, out *[21]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 21))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>11 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 10) |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 31))
	out[2] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>1 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 20))
	out[3] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>12 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 9) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 30))
	out[4] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>2 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 19))
	out[5] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>13 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 8) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 29))
	out[6] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>3 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 18))
	out[7] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>14 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 7) |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 28))
	out[8] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>4 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 17))
	out[9] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>15 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 6) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 27))
	out[10] = uint32(
		(((int32(in[15]-in[14]))<<1)^((int32(in[15]-in[14]))>>31))>>5 |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16))
	out[11] = uint32(
		(((int32(in[16]-in[15]))<<1)^((int32(in[16]-in[15]))>>31))>>16 |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 5) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 26))
	out[12] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>6 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 15))
	out[13] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>17 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 4) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 25))
	out[14] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>7 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 14))
	out[15] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>18 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 3) |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 24))
	out[16] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>8 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 13))
	out[17] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>19 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 2) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 23))
	out[18] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>9 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 12))
	out[19] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>20 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 1) |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 22))
	out[20] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>10 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 11))
}

func deltapackzigzag32_22[T uint32 | int32](initoffset T, in *[32]T, out *[22]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 22))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>10 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 12))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>20 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 2) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 24))
	out[3] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>8 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 14))
	out[4] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>18 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 4) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 26))
	out[5] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>6 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 16))
	out[6] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>16 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 6) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 28))
	out[7] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>4 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 18))
	out[8] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>14 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 8) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 30))
	out[9] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>2 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 20))
	out[10] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>12 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 10))
	out[11] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 22))
	out[12] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>10 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 12))
	out[13] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>20 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 2) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 24))
	out[14] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>8 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 14))
	out[15] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>18 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 4) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 26))
	out[16] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>6 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 16))
	out[17] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>16 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 6) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 28))
	out[18] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>4 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 18))
	out[19] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>14 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 8) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 30))
	out[20] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>2 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 20))
	out[21] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>12 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 10))
}

func deltapackzigzag32_23[T uint32 | int32](initoffset T, in *[32]T, out *[23]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 23))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>9 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 14))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>18 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 5) |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 28))
	out[3] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>4 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 19))
	out[4] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>13 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 10))
	out[5] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>22 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 1) |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 24))
	out[6] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>8 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 15))
	out[7] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>17 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 6) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 29))
	out[8] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>3 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 20))
	out[9] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>12 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 11))
	out[10] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>21 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 2) |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 25))
	out[11] = uint32(
		(((int32(in[15]-in[14]))<<1)^((int32(in[15]-in[14]))>>31))>>7 |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16))
	out[12] = uint32(
		(((int32(in[16]-in[15]))<<1)^((int32(in[16]-in[15]))>>31))>>16 |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 7) |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 30))
	out[13] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>2 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 21))
	out[14] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>11 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 12))
	out[15] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>20 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 3) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 26))
	out[16] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>6 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 17))
	out[17] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>15 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 8) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 31))
	out[18] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>1 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 22))
	out[19] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>10 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 13))
	out[20] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>19 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 4) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 27))
	out[21] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>5 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 18))
	out[22] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>14 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 9))
}

func deltapackzigzag32_24[T uint32 | int32](initoffset T, in *[32]T, out *[24]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 24))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>8 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 16))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>16 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 8))
	out[3] = uint32(
		((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 24))
	out[4] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>8 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 16))
	out[5] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>16 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 8))
	out[6] = uint32(
		((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 24))
	out[7] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>8 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 16))
	out[8] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>16 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 8))
	out[9] = uint32(
		((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 24))
	out[10] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>8 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 16))
	out[11] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>16 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 8))
	out[12] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 24))
	out[13] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>8 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 16))
	out[14] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>16 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 8))
	out[15] = uint32(
		((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31) |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 24))
	out[16] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>8 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 16))
	out[17] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>16 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 8))
	out[18] = uint32(
		((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 24))
	out[19] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>8 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 16))
	out[20] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>16 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 8))
	out[21] = uint32(
		((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31) |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 24))
	out[22] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>8 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 16))
	out[23] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>16 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 8))
}

func deltapackzigzag32_25[T uint32 | int32](initoffset T, in *[32]T, out *[25]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 25))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>7 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 18))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>14 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 11))
	out[3] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>21 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 4) |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 29))
	out[4] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>3 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 22))
	out[5] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>10 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 15))
	out[6] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>17 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 8))
	out[7] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>24 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 1) |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 26))
	out[8] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>6 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 19))
	out[9] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>13 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 12))
	out[10] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>20 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 5) |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 30))
	out[11] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>2 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 23))
	out[12] = uint32(
		(((int32(in[15]-in[14]))<<1)^((int32(in[15]-in[14]))>>31))>>9 |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16))
	out[13] = uint32(
		(((int32(in[16]-in[15]))<<1)^((int32(in[16]-in[15]))>>31))>>16 |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 9))
	out[14] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>23 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 2) |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 27))
	out[15] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>5 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 20))
	out[16] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>12 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 13))
	out[17] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>19 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 6) |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 31))
	out[18] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>1 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 24))
	out[19] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>8 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 17))
	out[20] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>15 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 10))
	out[21] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>22 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 3) |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 28))
	out[22] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>4 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 21))
	out[23] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>11 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 14))
	out[24] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>18 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 7))
}

func deltapackzigzag32_26[T uint32 | int32](initoffset T, in *[32]T, out *[26]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 26))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>6 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 20))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>12 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 14))
	out[3] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>18 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 8))
	out[4] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>24 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 2) |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 28))
	out[5] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>4 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 22))
	out[6] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>10 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 16))
	out[7] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>16 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 10))
	out[8] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>22 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 4) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 30))
	out[9] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>2 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 24))
	out[10] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>8 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 18))
	out[11] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>14 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 12))
	out[12] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>20 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 6))
	out[13] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 26))
	out[14] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>6 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 20))
	out[15] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>12 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 14))
	out[16] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>18 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 8))
	out[17] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>24 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 2) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 28))
	out[18] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>4 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 22))
	out[19] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>10 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 16))
	out[20] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>16 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 10))
	out[21] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>22 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 4) |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 30))
	out[22] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>2 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 24))
	out[23] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>8 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 18))
	out[24] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>14 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 12))
	out[25] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>20 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 6))
}

func deltapackzigzag32_27[T uint32 | int32](initoffset T, in *[32]T, out *[27]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 27))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>5 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 22))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>10 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 17))
	out[3] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>15 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 12))
	out[4] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>20 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 7))
	out[5] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>25 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 2) |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 29))
	out[6] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>3 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 24))
	out[7] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>8 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 19))
	out[8] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>13 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 14))
	out[9] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>18 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 9))
	out[10] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>23 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 4) |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 31))
	out[11] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>1 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 26))
	out[12] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>6 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 21))
	out[13] = uint32(
		(((int32(in[15]-in[14]))<<1)^((int32(in[15]-in[14]))>>31))>>11 |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16))
	out[14] = uint32(
		(((int32(in[16]-in[15]))<<1)^((int32(in[16]-in[15]))>>31))>>16 |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 11))
	out[15] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>21 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 6))
	out[16] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>26 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 1) |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 28))
	out[17] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>4 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 23))
	out[18] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>9 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 18))
	out[19] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>14 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 13))
	out[20] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>19 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 8))
	out[21] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>24 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 3) |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 30))
	out[22] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>2 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 25))
	out[23] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>7 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 20))
	out[24] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>12 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 15))
	out[25] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>17 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 10))
	out[26] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>22 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 5))
}

func deltapackzigzag32_28[T uint32 | int32](initoffset T, in *[32]T, out *[28]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 28))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>4 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 24))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>8 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 20))
	out[3] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>12 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 16))
	out[4] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>16 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 12))
	out[5] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>20 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 8))
	out[6] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>24 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 4))
	out[7] = uint32(
		((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31) |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 28))
	out[8] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>4 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 24))
	out[9] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>8 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 20))
	out[10] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>12 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 16))
	out[11] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>16 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 12))
	out[12] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>20 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 8))
	out[13] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>24 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 4))
	out[14] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 28))
	out[15] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>4 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 24))
	out[16] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>8 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 20))
	out[17] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>12 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 16))
	out[18] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>16 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 12))
	out[19] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>20 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 8))
	out[20] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>24 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 4))
	out[21] = uint32(
		((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31) |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 28))
	out[22] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>4 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 24))
	out[23] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>8 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 20))
	out[24] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>12 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 16))
	out[25] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>16 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 12))
	out[26] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>20 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 8))
	out[27] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>24 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 4))
}

func deltapackzigzag32_29[T uint32 | int32](initoffset T, in *[32]T, out *[29]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 29))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>3 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 26))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>6 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 23))
	out[3] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>9 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 20))
	out[4] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>12 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 17))
	out[5] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>15 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 14))
	out[6] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>18 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 11))
	out[7] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>21 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 8))
	out[8] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>24 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 5))
	out[9] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>27 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 2) |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 31))
	out[10] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>1 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 28))
	out[11] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>4 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 25))
	out[12] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>7 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 22))
	out[13] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>10 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 19))
	out[14] = uint32(
		(((int32(in[15]-in[14]))<<1)^((int32(in[15]-in[14]))>>31))>>13 |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16))
	out[15] = uint32(
		(((int32(in[16]-in[15]))<<1)^((int32(in[16]-in[15]))>>31))>>16 |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 13))
	out[16] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>19 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 10))
	out[17] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>22 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 7))
	out[18] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>25 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 4))
	out[19] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>28 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 1) |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 30))
	out[20] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>2 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 27))
	out[21] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>5 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 24))
	out[22] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>8 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 21))
	out[23] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>11 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 18))
	out[24] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>14 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 15))
	out[25] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>17 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 12))
	out[26] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>20 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 9))
	out[27] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>23 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 6))
	out[28] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>26 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 3))
}

func deltapackzigzag32_30[T uint32 | int32](initoffset T, in *[32]T, out *[30]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 30))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>2 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 28))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>4 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 26))
	out[3] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>6 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 24))
	out[4] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>8 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 22))
	out[5] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>10 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 20))
	out[6] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>12 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 18))
	out[7] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>14 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 16))
	out[8] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>16 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 14))
	out[9] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>18 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 12))
	out[10] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>20 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 10))
	out[11] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>22 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 8))
	out[12] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>24 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 6))
	out[13] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>26 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 4))
	out[14] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>28 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 2))
	out[15] = uint32(
		((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31) |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 30))
	out[16] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>2 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 28))
	out[17] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>4 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 26))
	out[18] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>6 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 24))
	out[19] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>8 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 22))
	out[20] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>10 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 20))
	out[21] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>12 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 18))
	out[22] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>14 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 16))
	out[23] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>16 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 14))
	out[24] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>18 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 12))
	out[25] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>20 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 10))
	out[26] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>22 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 8))
	out[27] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>24 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 6))
	out[28] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>26 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 4))
	out[29] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>28 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 2))
}

func deltapackzigzag32_31[T uint32 | int32](initoffset T, in *[32]T, out *[31]uint32) {
	out[0] = uint32(
		((int32(in[0] - initoffset)) << 1) ^ ((int32(in[0] - initoffset)) >> 31) |
			((((int32(in[1] - in[0])) << 1) ^ ((int32(in[1] - in[0])) >> 31)) << 31))
	out[1] = uint32(
		(((int32(in[1]-in[0]))<<1)^((int32(in[1]-in[0]))>>31))>>1 |
			((((int32(in[2] - in[1])) << 1) ^ ((int32(in[2] - in[1])) >> 31)) << 30))
	out[2] = uint32(
		(((int32(in[2]-in[1]))<<1)^((int32(in[2]-in[1]))>>31))>>2 |
			((((int32(in[3] - in[2])) << 1) ^ ((int32(in[3] - in[2])) >> 31)) << 29))
	out[3] = uint32(
		(((int32(in[3]-in[2]))<<1)^((int32(in[3]-in[2]))>>31))>>3 |
			((((int32(in[4] - in[3])) << 1) ^ ((int32(in[4] - in[3])) >> 31)) << 28))
	out[4] = uint32(
		(((int32(in[4]-in[3]))<<1)^((int32(in[4]-in[3]))>>31))>>4 |
			((((int32(in[5] - in[4])) << 1) ^ ((int32(in[5] - in[4])) >> 31)) << 27))
	out[5] = uint32(
		(((int32(in[5]-in[4]))<<1)^((int32(in[5]-in[4]))>>31))>>5 |
			((((int32(in[6] - in[5])) << 1) ^ ((int32(in[6] - in[5])) >> 31)) << 26))
	out[6] = uint32(
		(((int32(in[6]-in[5]))<<1)^((int32(in[6]-in[5]))>>31))>>6 |
			((((int32(in[7] - in[6])) << 1) ^ ((int32(in[7] - in[6])) >> 31)) << 25))
	out[7] = uint32(
		(((int32(in[7]-in[6]))<<1)^((int32(in[7]-in[6]))>>31))>>7 |
			((((int32(in[8] - in[7])) << 1) ^ ((int32(in[8] - in[7])) >> 31)) << 24))
	out[8] = uint32(
		(((int32(in[8]-in[7]))<<1)^((int32(in[8]-in[7]))>>31))>>8 |
			((((int32(in[9] - in[8])) << 1) ^ ((int32(in[9] - in[8])) >> 31)) << 23))
	out[9] = uint32(
		(((int32(in[9]-in[8]))<<1)^((int32(in[9]-in[8]))>>31))>>9 |
			((((int32(in[10] - in[9])) << 1) ^ ((int32(in[10] - in[9])) >> 31)) << 22))
	out[10] = uint32(
		(((int32(in[10]-in[9]))<<1)^((int32(in[10]-in[9]))>>31))>>10 |
			((((int32(in[11] - in[10])) << 1) ^ ((int32(in[11] - in[10])) >> 31)) << 21))
	out[11] = uint32(
		(((int32(in[11]-in[10]))<<1)^((int32(in[11]-in[10]))>>31))>>11 |
			((((int32(in[12] - in[11])) << 1) ^ ((int32(in[12] - in[11])) >> 31)) << 20))
	out[12] = uint32(
		(((int32(in[12]-in[11]))<<1)^((int32(in[12]-in[11]))>>31))>>12 |
			((((int32(in[13] - in[12])) << 1) ^ ((int32(in[13] - in[12])) >> 31)) << 19))
	out[13] = uint32(
		(((int32(in[13]-in[12]))<<1)^((int32(in[13]-in[12]))>>31))>>13 |
			((((int32(in[14] - in[13])) << 1) ^ ((int32(in[14] - in[13])) >> 31)) << 18))
	out[14] = uint32(
		(((int32(in[14]-in[13]))<<1)^((int32(in[14]-in[13]))>>31))>>14 |
			((((int32(in[15] - in[14])) << 1) ^ ((int32(in[15] - in[14])) >> 31)) << 17))
	out[15] = uint32(
		(((int32(in[15]-in[14]))<<1)^((int32(in[15]-in[14]))>>31))>>15 |
			((((int32(in[16] - in[15])) << 1) ^ ((int32(in[16] - in[15])) >> 31)) << 16))
	out[16] = uint32(
		(((int32(in[16]-in[15]))<<1)^((int32(in[16]-in[15]))>>31))>>16 |
			((((int32(in[17] - in[16])) << 1) ^ ((int32(in[17] - in[16])) >> 31)) << 15))
	out[17] = uint32(
		(((int32(in[17]-in[16]))<<1)^((int32(in[17]-in[16]))>>31))>>17 |
			((((int32(in[18] - in[17])) << 1) ^ ((int32(in[18] - in[17])) >> 31)) << 14))
	out[18] = uint32(
		(((int32(in[18]-in[17]))<<1)^((int32(in[18]-in[17]))>>31))>>18 |
			((((int32(in[19] - in[18])) << 1) ^ ((int32(in[19] - in[18])) >> 31)) << 13))
	out[19] = uint32(
		(((int32(in[19]-in[18]))<<1)^((int32(in[19]-in[18]))>>31))>>19 |
			((((int32(in[20] - in[19])) << 1) ^ ((int32(in[20] - in[19])) >> 31)) << 12))
	out[20] = uint32(
		(((int32(in[20]-in[19]))<<1)^((int32(in[20]-in[19]))>>31))>>20 |
			((((int32(in[21] - in[20])) << 1) ^ ((int32(in[21] - in[20])) >> 31)) << 11))
	out[21] = uint32(
		(((int32(in[21]-in[20]))<<1)^((int32(in[21]-in[20]))>>31))>>21 |
			((((int32(in[22] - in[21])) << 1) ^ ((int32(in[22] - in[21])) >> 31)) << 10))
	out[22] = uint32(
		(((int32(in[22]-in[21]))<<1)^((int32(in[22]-in[21]))>>31))>>22 |
			((((int32(in[23] - in[22])) << 1) ^ ((int32(in[23] - in[22])) >> 31)) << 9))
	out[23] = uint32(
		(((int32(in[23]-in[22]))<<1)^((int32(in[23]-in[22]))>>31))>>23 |
			((((int32(in[24] - in[23])) << 1) ^ ((int32(in[24] - in[23])) >> 31)) << 8))
	out[24] = uint32(
		(((int32(in[24]-in[23]))<<1)^((int32(in[24]-in[23]))>>31))>>24 |
			((((int32(in[25] - in[24])) << 1) ^ ((int32(in[25] - in[24])) >> 31)) << 7))
	out[25] = uint32(
		(((int32(in[25]-in[24]))<<1)^((int32(in[25]-in[24]))>>31))>>25 |
			((((int32(in[26] - in[25])) << 1) ^ ((int32(in[26] - in[25])) >> 31)) << 6))
	out[26] = uint32(
		(((int32(in[26]-in[25]))<<1)^((int32(in[26]-in[25]))>>31))>>26 |
			((((int32(in[27] - in[26])) << 1) ^ ((int32(in[27] - in[26])) >> 31)) << 5))
	out[27] = uint32(
		(((int32(in[27]-in[26]))<<1)^((int32(in[27]-in[26]))>>31))>>27 |
			((((int32(in[28] - in[27])) << 1) ^ ((int32(in[28] - in[27])) >> 31)) << 4))
	out[28] = uint32(
		(((int32(in[28]-in[27]))<<1)^((int32(in[28]-in[27]))>>31))>>28 |
			((((int32(in[29] - in[28])) << 1) ^ ((int32(in[29] - in[28])) >> 31)) << 3))
	out[29] = uint32(
		(((int32(in[29]-in[28]))<<1)^((int32(in[29]-in[28]))>>31))>>29 |
			((((int32(in[30] - in[29])) << 1) ^ ((int32(in[30] - in[29])) >> 31)) << 2))
	out[30] = uint32(
		(((int32(in[30]-in[29]))<<1)^((int32(in[30]-in[29]))>>31))>>30 |
			((((int32(in[31] - in[30])) << 1) ^ ((int32(in[31] - in[30])) >> 31)) << 1))
}

func deltaunpackzigzag32_0[T uint32 | int32](initoffset T, in *[0]uint32, out *[32]T) {
	out[0] = initoffset
	out[1] = initoffset
	out[2] = initoffset
	out[3] = initoffset
	out[4] = initoffset
	out[5] = initoffset
	out[6] = initoffset
	out[7] = initoffset
	out[8] = initoffset
	out[9] = initoffset
	out[10] = initoffset
	out[11] = initoffset
	out[12] = initoffset
	out[13] = initoffset
	out[14] = initoffset
	out[15] = initoffset
	out[16] = initoffset
	out[17] = initoffset
	out[18] = initoffset
	out[19] = initoffset
	out[20] = initoffset
	out[21] = initoffset
	out[22] = initoffset
	out[23] = initoffset
	out[24] = initoffset
	out[25] = initoffset
	out[26] = initoffset
	out[27] = initoffset
	out[28] = initoffset
	out[29] = initoffset
	out[30] = initoffset
	out[31] = initoffset
}

func deltaunpackzigzag32_1[T uint32 | int32](initoffset T, in *[1]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x1) & 1)) ^ (((in[0] >> 0) & 0x1) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 1) & 0x1) & 1)) ^ (((in[0] >> 1) & 0x1) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 2) & 0x1) & 1)) ^ (((in[0] >> 2) & 0x1) >> 1))) + out[1]
	out[3] = T(((-(((in[0] >> 3) & 0x1) & 1)) ^ (((in[0] >> 3) & 0x1) >> 1))) + out[2]
	out[4] = T(((-(((in[0] >> 4) & 0x1) & 1)) ^ (((in[0] >> 4) & 0x1) >> 1))) + out[3]
	out[5] = T(((-(((in[0] >> 5) & 0x1) & 1)) ^ (((in[0] >> 5) & 0x1) >> 1))) + out[4]
	out[6] = T(((-(((in[0] >> 6) & 0x1) & 1)) ^ (((in[0] >> 6) & 0x1) >> 1))) + out[5]
	out[7] = T(((-(((in[0] >> 7) & 0x1) & 1)) ^ (((in[0] >> 7) & 0x1) >> 1))) + out[6]
	out[8] = T(((-(((in[0] >> 8) & 0x1) & 1)) ^ (((in[0] >> 8) & 0x1) >> 1))) + out[7]
	out[9] = T(((-(((in[0] >> 9) & 0x1) & 1)) ^ (((in[0] >> 9) & 0x1) >> 1))) + out[8]
	out[10] = T(((-(((in[0] >> 10) & 0x1) & 1)) ^ (((in[0] >> 10) & 0x1) >> 1))) + out[9]
	out[11] = T(((-(((in[0] >> 11) & 0x1) & 1)) ^ (((in[0] >> 11) & 0x1) >> 1))) + out[10]
	out[12] = T(((-(((in[0] >> 12) & 0x1) & 1)) ^ (((in[0] >> 12) & 0x1) >> 1))) + out[11]
	out[13] = T(((-(((in[0] >> 13) & 0x1) & 1)) ^ (((in[0] >> 13) & 0x1) >> 1))) + out[12]
	out[14] = T(((-(((in[0] >> 14) & 0x1) & 1)) ^ (((in[0] >> 14) & 0x1) >> 1))) + out[13]
	out[15] = T(((-(((in[0] >> 15) & 0x1) & 1)) ^ (((in[0] >> 15) & 0x1) >> 1))) + out[14]
	out[16] = T(((-(((in[0] >> 16) & 0x1) & 1)) ^ (((in[0] >> 16) & 0x1) >> 1))) + out[15]
	out[17] = T(((-(((in[0] >> 17) & 0x1) & 1)) ^ (((in[0] >> 17) & 0x1) >> 1))) + out[16]
	out[18] = T(((-(((in[0] >> 18) & 0x1) & 1)) ^ (((in[0] >> 18) & 0x1) >> 1))) + out[17]
	out[19] = T(((-(((in[0] >> 19) & 0x1) & 1)) ^ (((in[0] >> 19) & 0x1) >> 1))) + out[18]
	out[20] = T(((-(((in[0] >> 20) & 0x1) & 1)) ^ (((in[0] >> 20) & 0x1) >> 1))) + out[19]
	out[21] = T(((-(((in[0] >> 21) & 0x1) & 1)) ^ (((in[0] >> 21) & 0x1) >> 1))) + out[20]
	out[22] = T(((-(((in[0] >> 22) & 0x1) & 1)) ^ (((in[0] >> 22) & 0x1) >> 1))) + out[21]
	out[23] = T(((-(((in[0] >> 23) & 0x1) & 1)) ^ (((in[0] >> 23) & 0x1) >> 1))) + out[22]
	out[24] = T(((-(((in[0] >> 24) & 0x1) & 1)) ^ (((in[0] >> 24) & 0x1) >> 1))) + out[23]
	out[25] = T(((-(((in[0] >> 25) & 0x1) & 1)) ^ (((in[0] >> 25) & 0x1) >> 1))) + out[24]
	out[26] = T(((-(((in[0] >> 26) & 0x1) & 1)) ^ (((in[0] >> 26) & 0x1) >> 1))) + out[25]
	out[27] = T(((-(((in[0] >> 27) & 0x1) & 1)) ^ (((in[0] >> 27) & 0x1) >> 1))) + out[26]
	out[28] = T(((-(((in[0] >> 28) & 0x1) & 1)) ^ (((in[0] >> 28) & 0x1) >> 1))) + out[27]
	out[29] = T(((-(((in[0] >> 29) & 0x1) & 1)) ^ (((in[0] >> 29) & 0x1) >> 1))) + out[28]
	out[30] = T(((-(((in[0] >> 30) & 0x1) & 1)) ^ (((in[0] >> 30) & 0x1) >> 1))) + out[29]
	out[31] = T(((-((in[0] >> 31) & 1)) ^ ((in[0] >> 31) >> 1))) + out[30]
}

func deltaunpackzigzag32_2[T uint32 | int32](initoffset T, in *[2]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x3) & 1)) ^ (((in[0] >> 0) & 0x3) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 2) & 0x3) & 1)) ^ (((in[0] >> 2) & 0x3) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 4) & 0x3) & 1)) ^ (((in[0] >> 4) & 0x3) >> 1))) + out[1]
	out[3] = T(((-(((in[0] >> 6) & 0x3) & 1)) ^ (((in[0] >> 6) & 0x3) >> 1))) + out[2]
	out[4] = T(((-(((in[0] >> 8) & 0x3) & 1)) ^ (((in[0] >> 8) & 0x3) >> 1))) + out[3]
	out[5] = T(((-(((in[0] >> 10) & 0x3) & 1)) ^ (((in[0] >> 10) & 0x3) >> 1))) + out[4]
	out[6] = T(((-(((in[0] >> 12) & 0x3) & 1)) ^ (((in[0] >> 12) & 0x3) >> 1))) + out[5]
	out[7] = T(((-(((in[0] >> 14) & 0x3) & 1)) ^ (((in[0] >> 14) & 0x3) >> 1))) + out[6]
	out[8] = T(((-(((in[0] >> 16) & 0x3) & 1)) ^ (((in[0] >> 16) & 0x3) >> 1))) + out[7]
	out[9] = T(((-(((in[0] >> 18) & 0x3) & 1)) ^ (((in[0] >> 18) & 0x3) >> 1))) + out[8]
	out[10] = T(((-(((in[0] >> 20) & 0x3) & 1)) ^ (((in[0] >> 20) & 0x3) >> 1))) + out[9]
	out[11] = T(((-(((in[0] >> 22) & 0x3) & 1)) ^ (((in[0] >> 22) & 0x3) >> 1))) + out[10]
	out[12] = T(((-(((in[0] >> 24) & 0x3) & 1)) ^ (((in[0] >> 24) & 0x3) >> 1))) + out[11]
	out[13] = T(((-(((in[0] >> 26) & 0x3) & 1)) ^ (((in[0] >> 26) & 0x3) >> 1))) + out[12]
	out[14] = T(((-(((in[0] >> 28) & 0x3) & 1)) ^ (((in[0] >> 28) & 0x3) >> 1))) + out[13]
	out[15] = T(((-((in[0] >> 30) & 1)) ^ ((in[0] >> 30) >> 1))) + out[14]
	out[16] = T(((-(((in[1] >> 0) & 0x3) & 1)) ^ (((in[1] >> 0) & 0x3) >> 1))) + out[15]
	out[17] = T(((-(((in[1] >> 2) & 0x3) & 1)) ^ (((in[1] >> 2) & 0x3) >> 1))) + out[16]
	out[18] = T(((-(((in[1] >> 4) & 0x3) & 1)) ^ (((in[1] >> 4) & 0x3) >> 1))) + out[17]
	out[19] = T(((-(((in[1] >> 6) & 0x3) & 1)) ^ (((in[1] >> 6) & 0x3) >> 1))) + out[18]
	out[20] = T(((-(((in[1] >> 8) & 0x3) & 1)) ^ (((in[1] >> 8) & 0x3) >> 1))) + out[19]
	out[21] = T(((-(((in[1] >> 10) & 0x3) & 1)) ^ (((in[1] >> 10) & 0x3) >> 1))) + out[20]
	out[22] = T(((-(((in[1] >> 12) & 0x3) & 1)) ^ (((in[1] >> 12) & 0x3) >> 1))) + out[21]
	out[23] = T(((-(((in[1] >> 14) & 0x3) & 1)) ^ (((in[1] >> 14) & 0x3) >> 1))) + out[22]
	out[24] = T(((-(((in[1] >> 16) & 0x3) & 1)) ^ (((in[1] >> 16) & 0x3) >> 1))) + out[23]
	out[25] = T(((-(((in[1] >> 18) & 0x3) & 1)) ^ (((in[1] >> 18) & 0x3) >> 1))) + out[24]
	out[26] = T(((-(((in[1] >> 20) & 0x3) & 1)) ^ (((in[1] >> 20) & 0x3) >> 1))) + out[25]
	out[27] = T(((-(((in[1] >> 22) & 0x3) & 1)) ^ (((in[1] >> 22) & 0x3) >> 1))) + out[26]
	out[28] = T(((-(((in[1] >> 24) & 0x3) & 1)) ^ (((in[1] >> 24) & 0x3) >> 1))) + out[27]
	out[29] = T(((-(((in[1] >> 26) & 0x3) & 1)) ^ (((in[1] >> 26) & 0x3) >> 1))) + out[28]
	out[30] = T(((-(((in[1] >> 28) & 0x3) & 1)) ^ (((in[1] >> 28) & 0x3) >> 1))) + out[29]
	out[31] = T(((-((in[1] >> 30) & 1)) ^ ((in[1] >> 30) >> 1))) + out[30]
}

func deltaunpackzigzag32_3[T uint32 | int32](initoffset T, in *[3]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x7) & 1)) ^ (((in[0] >> 0) & 0x7) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 3) & 0x7) & 1)) ^ (((in[0] >> 3) & 0x7) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 6) & 0x7) & 1)) ^ (((in[0] >> 6) & 0x7) >> 1))) + out[1]
	out[3] = T(((-(((in[0] >> 9) & 0x7) & 1)) ^ (((in[0] >> 9) & 0x7) >> 1))) + out[2]
	out[4] = T(((-(((in[0] >> 12) & 0x7) & 1)) ^ (((in[0] >> 12) & 0x7) >> 1))) + out[3]
	out[5] = T(((-(((in[0] >> 15) & 0x7) & 1)) ^ (((in[0] >> 15) & 0x7) >> 1))) + out[4]
	out[6] = T(((-(((in[0] >> 18) & 0x7) & 1)) ^ (((in[0] >> 18) & 0x7) >> 1))) + out[5]
	out[7] = T(((-(((in[0] >> 21) & 0x7) & 1)) ^ (((in[0] >> 21) & 0x7) >> 1))) + out[6]
	out[8] = T(((-(((in[0] >> 24) & 0x7) & 1)) ^ (((in[0] >> 24) & 0x7) >> 1))) + out[7]
	out[9] = T(((-(((in[0] >> 27) & 0x7) & 1)) ^ (((in[0] >> 27) & 0x7) >> 1))) + out[8]
	out[10] = T(((-(((in[0] >> 30) | ((in[1] & 0x1) << 2)) & 1)) ^ (((in[0] >> 30) | ((in[1] & 0x1) << 2)) >> 1))) + out[9]
	out[11] = T(((-(((in[1] >> 1) & 0x7) & 1)) ^ (((in[1] >> 1) & 0x7) >> 1))) + out[10]
	out[12] = T(((-(((in[1] >> 4) & 0x7) & 1)) ^ (((in[1] >> 4) & 0x7) >> 1))) + out[11]
	out[13] = T(((-(((in[1] >> 7) & 0x7) & 1)) ^ (((in[1] >> 7) & 0x7) >> 1))) + out[12]
	out[14] = T(((-(((in[1] >> 10) & 0x7) & 1)) ^ (((in[1] >> 10) & 0x7) >> 1))) + out[13]
	out[15] = T(((-(((in[1] >> 13) & 0x7) & 1)) ^ (((in[1] >> 13) & 0x7) >> 1))) + out[14]
	out[16] = T(((-(((in[1] >> 16) & 0x7) & 1)) ^ (((in[1] >> 16) & 0x7) >> 1))) + out[15]
	out[17] = T(((-(((in[1] >> 19) & 0x7) & 1)) ^ (((in[1] >> 19) & 0x7) >> 1))) + out[16]
	out[18] = T(((-(((in[1] >> 22) & 0x7) & 1)) ^ (((in[1] >> 22) & 0x7) >> 1))) + out[17]
	out[19] = T(((-(((in[1] >> 25) & 0x7) & 1)) ^ (((in[1] >> 25) & 0x7) >> 1))) + out[18]
	out[20] = T(((-(((in[1] >> 28) & 0x7) & 1)) ^ (((in[1] >> 28) & 0x7) >> 1))) + out[19]
	out[21] = T(((-(((in[1] >> 31) | ((in[2] & 0x3) << 1)) & 1)) ^ (((in[1] >> 31) | ((in[2] & 0x3) << 1)) >> 1))) + out[20]
	out[22] = T(((-(((in[2] >> 2) & 0x7) & 1)) ^ (((in[2] >> 2) & 0x7) >> 1))) + out[21]
	out[23] = T(((-(((in[2] >> 5) & 0x7) & 1)) ^ (((in[2] >> 5) & 0x7) >> 1))) + out[22]
	out[24] = T(((-(((in[2] >> 8) & 0x7) & 1)) ^ (((in[2] >> 8) & 0x7) >> 1))) + out[23]
	out[25] = T(((-(((in[2] >> 11) & 0x7) & 1)) ^ (((in[2] >> 11) & 0x7) >> 1))) + out[24]
	out[26] = T(((-(((in[2] >> 14) & 0x7) & 1)) ^ (((in[2] >> 14) & 0x7) >> 1))) + out[25]
	out[27] = T(((-(((in[2] >> 17) & 0x7) & 1)) ^ (((in[2] >> 17) & 0x7) >> 1))) + out[26]
	out[28] = T(((-(((in[2] >> 20) & 0x7) & 1)) ^ (((in[2] >> 20) & 0x7) >> 1))) + out[27]
	out[29] = T(((-(((in[2] >> 23) & 0x7) & 1)) ^ (((in[2] >> 23) & 0x7) >> 1))) + out[28]
	out[30] = T(((-(((in[2] >> 26) & 0x7) & 1)) ^ (((in[2] >> 26) & 0x7) >> 1))) + out[29]
	out[31] = T(((-((in[2] >> 29) & 1)) ^ ((in[2] >> 29) >> 1))) + out[30]
}

func deltaunpackzigzag32_4[T uint32 | int32](initoffset T, in *[4]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0xF) & 1)) ^ (((in[0] >> 0) & 0xF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 4) & 0xF) & 1)) ^ (((in[0] >> 4) & 0xF) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 8) & 0xF) & 1)) ^ (((in[0] >> 8) & 0xF) >> 1))) + out[1]
	out[3] = T(((-(((in[0] >> 12) & 0xF) & 1)) ^ (((in[0] >> 12) & 0xF) >> 1))) + out[2]
	out[4] = T(((-(((in[0] >> 16) & 0xF) & 1)) ^ (((in[0] >> 16) & 0xF) >> 1))) + out[3]
	out[5] = T(((-(((in[0] >> 20) & 0xF) & 1)) ^ (((in[0] >> 20) & 0xF) >> 1))) + out[4]
	out[6] = T(((-(((in[0] >> 24) & 0xF) & 1)) ^ (((in[0] >> 24) & 0xF) >> 1))) + out[5]
	out[7] = T(((-((in[0] >> 28) & 1)) ^ ((in[0] >> 28) >> 1))) + out[6]
	out[8] = T(((-(((in[1] >> 0) & 0xF) & 1)) ^ (((in[1] >> 0) & 0xF) >> 1))) + out[7]
	out[9] = T(((-(((in[1] >> 4) & 0xF) & 1)) ^ (((in[1] >> 4) & 0xF) >> 1))) + out[8]
	out[10] = T(((-(((in[1] >> 8) & 0xF) & 1)) ^ (((in[1] >> 8) & 0xF) >> 1))) + out[9]
	out[11] = T(((-(((in[1] >> 12) & 0xF) & 1)) ^ (((in[1] >> 12) & 0xF) >> 1))) + out[10]
	out[12] = T(((-(((in[1] >> 16) & 0xF) & 1)) ^ (((in[1] >> 16) & 0xF) >> 1))) + out[11]
	out[13] = T(((-(((in[1] >> 20) & 0xF) & 1)) ^ (((in[1] >> 20) & 0xF) >> 1))) + out[12]
	out[14] = T(((-(((in[1] >> 24) & 0xF) & 1)) ^ (((in[1] >> 24) & 0xF) >> 1))) + out[13]
	out[15] = T(((-((in[1] >> 28) & 1)) ^ ((in[1] >> 28) >> 1))) + out[14]
	out[16] = T(((-(((in[2] >> 0) & 0xF) & 1)) ^ (((in[2] >> 0) & 0xF) >> 1))) + out[15]
	out[17] = T(((-(((in[2] >> 4) & 0xF) & 1)) ^ (((in[2] >> 4) & 0xF) >> 1))) + out[16]
	out[18] = T(((-(((in[2] >> 8) & 0xF) & 1)) ^ (((in[2] >> 8) & 0xF) >> 1))) + out[17]
	out[19] = T(((-(((in[2] >> 12) & 0xF) & 1)) ^ (((in[2] >> 12) & 0xF) >> 1))) + out[18]
	out[20] = T(((-(((in[2] >> 16) & 0xF) & 1)) ^ (((in[2] >> 16) & 0xF) >> 1))) + out[19]
	out[21] = T(((-(((in[2] >> 20) & 0xF) & 1)) ^ (((in[2] >> 20) & 0xF) >> 1))) + out[20]
	out[22] = T(((-(((in[2] >> 24) & 0xF) & 1)) ^ (((in[2] >> 24) & 0xF) >> 1))) + out[21]
	out[23] = T(((-((in[2] >> 28) & 1)) ^ ((in[2] >> 28) >> 1))) + out[22]
	out[24] = T(((-(((in[3] >> 0) & 0xF) & 1)) ^ (((in[3] >> 0) & 0xF) >> 1))) + out[23]
	out[25] = T(((-(((in[3] >> 4) & 0xF) & 1)) ^ (((in[3] >> 4) & 0xF) >> 1))) + out[24]
	out[26] = T(((-(((in[3] >> 8) & 0xF) & 1)) ^ (((in[3] >> 8) & 0xF) >> 1))) + out[25]
	out[27] = T(((-(((in[3] >> 12) & 0xF) & 1)) ^ (((in[3] >> 12) & 0xF) >> 1))) + out[26]
	out[28] = T(((-(((in[3] >> 16) & 0xF) & 1)) ^ (((in[3] >> 16) & 0xF) >> 1))) + out[27]
	out[29] = T(((-(((in[3] >> 20) & 0xF) & 1)) ^ (((in[3] >> 20) & 0xF) >> 1))) + out[28]
	out[30] = T(((-(((in[3] >> 24) & 0xF) & 1)) ^ (((in[3] >> 24) & 0xF) >> 1))) + out[29]
	out[31] = T(((-((in[3] >> 28) & 1)) ^ ((in[3] >> 28) >> 1))) + out[30]
}

func deltaunpackzigzag32_5[T uint32 | int32](initoffset T, in *[5]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x1F) & 1)) ^ (((in[0] >> 0) & 0x1F) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 5) & 0x1F) & 1)) ^ (((in[0] >> 5) & 0x1F) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 10) & 0x1F) & 1)) ^ (((in[0] >> 10) & 0x1F) >> 1))) + out[1]
	out[3] = T(((-(((in[0] >> 15) & 0x1F) & 1)) ^ (((in[0] >> 15) & 0x1F) >> 1))) + out[2]
	out[4] = T(((-(((in[0] >> 20) & 0x1F) & 1)) ^ (((in[0] >> 20) & 0x1F) >> 1))) + out[3]
	out[5] = T(((-(((in[0] >> 25) & 0x1F) & 1)) ^ (((in[0] >> 25) & 0x1F) >> 1))) + out[4]
	out[6] = T(((-(((in[0] >> 30) | ((in[1] & 0x7) << 2)) & 1)) ^ (((in[0] >> 30) | ((in[1] & 0x7) << 2)) >> 1))) + out[5]
	out[7] = T(((-(((in[1] >> 3) & 0x1F) & 1)) ^ (((in[1] >> 3) & 0x1F) >> 1))) + out[6]
	out[8] = T(((-(((in[1] >> 8) & 0x1F) & 1)) ^ (((in[1] >> 8) & 0x1F) >> 1))) + out[7]
	out[9] = T(((-(((in[1] >> 13) & 0x1F) & 1)) ^ (((in[1] >> 13) & 0x1F) >> 1))) + out[8]
	out[10] = T(((-(((in[1] >> 18) & 0x1F) & 1)) ^ (((in[1] >> 18) & 0x1F) >> 1))) + out[9]
	out[11] = T(((-(((in[1] >> 23) & 0x1F) & 1)) ^ (((in[1] >> 23) & 0x1F) >> 1))) + out[10]
	out[12] = T(((-(((in[1] >> 28) | ((in[2] & 0x1) << 4)) & 1)) ^ (((in[1] >> 28) | ((in[2] & 0x1) << 4)) >> 1))) + out[11]
	out[13] = T(((-(((in[2] >> 1) & 0x1F) & 1)) ^ (((in[2] >> 1) & 0x1F) >> 1))) + out[12]
	out[14] = T(((-(((in[2] >> 6) & 0x1F) & 1)) ^ (((in[2] >> 6) & 0x1F) >> 1))) + out[13]
	out[15] = T(((-(((in[2] >> 11) & 0x1F) & 1)) ^ (((in[2] >> 11) & 0x1F) >> 1))) + out[14]
	out[16] = T(((-(((in[2] >> 16) & 0x1F) & 1)) ^ (((in[2] >> 16) & 0x1F) >> 1))) + out[15]
	out[17] = T(((-(((in[2] >> 21) & 0x1F) & 1)) ^ (((in[2] >> 21) & 0x1F) >> 1))) + out[16]
	out[18] = T(((-(((in[2] >> 26) & 0x1F) & 1)) ^ (((in[2] >> 26) & 0x1F) >> 1))) + out[17]
	out[19] = T(((-(((in[2] >> 31) | ((in[3] & 0xF) << 1)) & 1)) ^ (((in[2] >> 31) | ((in[3] & 0xF) << 1)) >> 1))) + out[18]
	out[20] = T(((-(((in[3] >> 4) & 0x1F) & 1)) ^ (((in[3] >> 4) & 0x1F) >> 1))) + out[19]
	out[21] = T(((-(((in[3] >> 9) & 0x1F) & 1)) ^ (((in[3] >> 9) & 0x1F) >> 1))) + out[20]
	out[22] = T(((-(((in[3] >> 14) & 0x1F) & 1)) ^ (((in[3] >> 14) & 0x1F) >> 1))) + out[21]
	out[23] = T(((-(((in[3] >> 19) & 0x1F) & 1)) ^ (((in[3] >> 19) & 0x1F) >> 1))) + out[22]
	out[24] = T(((-(((in[3] >> 24) & 0x1F) & 1)) ^ (((in[3] >> 24) & 0x1F) >> 1))) + out[23]
	out[25] = T(((-(((in[3] >> 29) | ((in[4] & 0x3) << 3)) & 1)) ^ (((in[3] >> 29) | ((in[4] & 0x3) << 3)) >> 1))) + out[24]
	out[26] = T(((-(((in[4] >> 2) & 0x1F) & 1)) ^ (((in[4] >> 2) & 0x1F) >> 1))) + out[25]
	out[27] = T(((-(((in[4] >> 7) & 0x1F) & 1)) ^ (((in[4] >> 7) & 0x1F) >> 1))) + out[26]
	out[28] = T(((-(((in[4] >> 12) & 0x1F) & 1)) ^ (((in[4] >> 12) & 0x1F) >> 1))) + out[27]
	out[29] = T(((-(((in[4] >> 17) & 0x1F) & 1)) ^ (((in[4] >> 17) & 0x1F) >> 1))) + out[28]
	out[30] = T(((-(((in[4] >> 22) & 0x1F) & 1)) ^ (((in[4] >> 22) & 0x1F) >> 1))) + out[29]
	out[31] = T(((-((in[4] >> 27) & 1)) ^ ((in[4] >> 27) >> 1))) + out[30]
}

func deltaunpackzigzag32_6[T uint32 | int32](initoffset T, in *[6]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x3F) & 1)) ^ (((in[0] >> 0) & 0x3F) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 6) & 0x3F) & 1)) ^ (((in[0] >> 6) & 0x3F) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 12) & 0x3F) & 1)) ^ (((in[0] >> 12) & 0x3F) >> 1))) + out[1]
	out[3] = T(((-(((in[0] >> 18) & 0x3F) & 1)) ^ (((in[0] >> 18) & 0x3F) >> 1))) + out[2]
	out[4] = T(((-(((in[0] >> 24) & 0x3F) & 1)) ^ (((in[0] >> 24) & 0x3F) >> 1))) + out[3]
	out[5] = T(((-(((in[0] >> 30) | ((in[1] & 0xF) << 2)) & 1)) ^ (((in[0] >> 30) | ((in[1] & 0xF) << 2)) >> 1))) + out[4]
	out[6] = T(((-(((in[1] >> 4) & 0x3F) & 1)) ^ (((in[1] >> 4) & 0x3F) >> 1))) + out[5]
	out[7] = T(((-(((in[1] >> 10) & 0x3F) & 1)) ^ (((in[1] >> 10) & 0x3F) >> 1))) + out[6]
	out[8] = T(((-(((in[1] >> 16) & 0x3F) & 1)) ^ (((in[1] >> 16) & 0x3F) >> 1))) + out[7]
	out[9] = T(((-(((in[1] >> 22) & 0x3F) & 1)) ^ (((in[1] >> 22) & 0x3F) >> 1))) + out[8]
	out[10] = T(((-(((in[1] >> 28) | ((in[2] & 0x3) << 4)) & 1)) ^ (((in[1] >> 28) | ((in[2] & 0x3) << 4)) >> 1))) + out[9]
	out[11] = T(((-(((in[2] >> 2) & 0x3F) & 1)) ^ (((in[2] >> 2) & 0x3F) >> 1))) + out[10]
	out[12] = T(((-(((in[2] >> 8) & 0x3F) & 1)) ^ (((in[2] >> 8) & 0x3F) >> 1))) + out[11]
	out[13] = T(((-(((in[2] >> 14) & 0x3F) & 1)) ^ (((in[2] >> 14) & 0x3F) >> 1))) + out[12]
	out[14] = T(((-(((in[2] >> 20) & 0x3F) & 1)) ^ (((in[2] >> 20) & 0x3F) >> 1))) + out[13]
	out[15] = T(((-((in[2] >> 26) & 1)) ^ ((in[2] >> 26) >> 1))) + out[14]
	out[16] = T(((-(((in[3] >> 0) & 0x3F) & 1)) ^ (((in[3] >> 0) & 0x3F) >> 1))) + out[15]
	out[17] = T(((-(((in[3] >> 6) & 0x3F) & 1)) ^ (((in[3] >> 6) & 0x3F) >> 1))) + out[16]
	out[18] = T(((-(((in[3] >> 12) & 0x3F) & 1)) ^ (((in[3] >> 12) & 0x3F) >> 1))) + out[17]
	out[19] = T(((-(((in[3] >> 18) & 0x3F) & 1)) ^ (((in[3] >> 18) & 0x3F) >> 1))) + out[18]
	out[20] = T(((-(((in[3] >> 24) & 0x3F) & 1)) ^ (((in[3] >> 24) & 0x3F) >> 1))) + out[19]
	out[21] = T(((-(((in[3] >> 30) | ((in[4] & 0xF) << 2)) & 1)) ^ (((in[3] >> 30) | ((in[4] & 0xF) << 2)) >> 1))) + out[20]
	out[22] = T(((-(((in[4] >> 4) & 0x3F) & 1)) ^ (((in[4] >> 4) & 0x3F) >> 1))) + out[21]
	out[23] = T(((-(((in[4] >> 10) & 0x3F) & 1)) ^ (((in[4] >> 10) & 0x3F) >> 1))) + out[22]
	out[24] = T(((-(((in[4] >> 16) & 0x3F) & 1)) ^ (((in[4] >> 16) & 0x3F) >> 1))) + out[23]
	out[25] = T(((-(((in[4] >> 22) & 0x3F) & 1)) ^ (((in[4] >> 22) & 0x3F) >> 1))) + out[24]
	out[26] = T(((-(((in[4] >> 28) | ((in[5] & 0x3) << 4)) & 1)) ^ (((in[4] >> 28) | ((in[5] & 0x3) << 4)) >> 1))) + out[25]
	out[27] = T(((-(((in[5] >> 2) & 0x3F) & 1)) ^ (((in[5] >> 2) & 0x3F) >> 1))) + out[26]
	out[28] = T(((-(((in[5] >> 8) & 0x3F) & 1)) ^ (((in[5] >> 8) & 0x3F) >> 1))) + out[27]
	out[29] = T(((-(((in[5] >> 14) & 0x3F) & 1)) ^ (((in[5] >> 14) & 0x3F) >> 1))) + out[28]
	out[30] = T(((-(((in[5] >> 20) & 0x3F) & 1)) ^ (((in[5] >> 20) & 0x3F) >> 1))) + out[29]
	out[31] = T(((-((in[5] >> 26) & 1)) ^ ((in[5] >> 26) >> 1))) + out[30]
}

func deltaunpackzigzag32_7[T uint32 | int32](initoffset T, in *[7]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x7F) & 1)) ^ (((in[0] >> 0) & 0x7F) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 7) & 0x7F) & 1)) ^ (((in[0] >> 7) & 0x7F) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 14) & 0x7F) & 1)) ^ (((in[0] >> 14) & 0x7F) >> 1))) + out[1]
	out[3] = T(((-(((in[0] >> 21) & 0x7F) & 1)) ^ (((in[0] >> 21) & 0x7F) >> 1))) + out[2]
	out[4] = T(((-(((in[0] >> 28) | ((in[1] & 0x7) << 4)) & 1)) ^ (((in[0] >> 28) | ((in[1] & 0x7) << 4)) >> 1))) + out[3]
	out[5] = T(((-(((in[1] >> 3) & 0x7F) & 1)) ^ (((in[1] >> 3) & 0x7F) >> 1))) + out[4]
	out[6] = T(((-(((in[1] >> 10) & 0x7F) & 1)) ^ (((in[1] >> 10) & 0x7F) >> 1))) + out[5]
	out[7] = T(((-(((in[1] >> 17) & 0x7F) & 1)) ^ (((in[1] >> 17) & 0x7F) >> 1))) + out[6]
	out[8] = T(((-(((in[1] >> 24) & 0x7F) & 1)) ^ (((in[1] >> 24) & 0x7F) >> 1))) + out[7]
	out[9] = T(((-(((in[1] >> 31) | ((in[2] & 0x3F) << 1)) & 1)) ^ (((in[1] >> 31) | ((in[2] & 0x3F) << 1)) >> 1))) + out[8]
	out[10] = T(((-(((in[2] >> 6) & 0x7F) & 1)) ^ (((in[2] >> 6) & 0x7F) >> 1))) + out[9]
	out[11] = T(((-(((in[2] >> 13) & 0x7F) & 1)) ^ (((in[2] >> 13) & 0x7F) >> 1))) + out[10]
	out[12] = T(((-(((in[2] >> 20) & 0x7F) & 1)) ^ (((in[2] >> 20) & 0x7F) >> 1))) + out[11]
	out[13] = T(((-(((in[2] >> 27) | ((in[3] & 0x3) << 5)) & 1)) ^ (((in[2] >> 27) | ((in[3] & 0x3) << 5)) >> 1))) + out[12]
	out[14] = T(((-(((in[3] >> 2) & 0x7F) & 1)) ^ (((in[3] >> 2) & 0x7F) >> 1))) + out[13]
	out[15] = T(((-(((in[3] >> 9) & 0x7F) & 1)) ^ (((in[3] >> 9) & 0x7F) >> 1))) + out[14]
	out[16] = T(((-(((in[3] >> 16) & 0x7F) & 1)) ^ (((in[3] >> 16) & 0x7F) >> 1))) + out[15]
	out[17] = T(((-(((in[3] >> 23) & 0x7F) & 1)) ^ (((in[3] >> 23) & 0x7F) >> 1))) + out[16]
	out[18] = T(((-(((in[3] >> 30) | ((in[4] & 0x1F) << 2)) & 1)) ^ (((in[3] >> 30) | ((in[4] & 0x1F) << 2)) >> 1))) + out[17]
	out[19] = T(((-(((in[4] >> 5) & 0x7F) & 1)) ^ (((in[4] >> 5) & 0x7F) >> 1))) + out[18]
	out[20] = T(((-(((in[4] >> 12) & 0x7F) & 1)) ^ (((in[4] >> 12) & 0x7F) >> 1))) + out[19]
	out[21] = T(((-(((in[4] >> 19) & 0x7F) & 1)) ^ (((in[4] >> 19) & 0x7F) >> 1))) + out[20]
	out[22] = T(((-(((in[4] >> 26) | ((in[5] & 0x1) << 6)) & 1)) ^ (((in[4] >> 26) | ((in[5] & 0x1) << 6)) >> 1))) + out[21]
	out[23] = T(((-(((in[5] >> 1) & 0x7F) & 1)) ^ (((in[5] >> 1) & 0x7F) >> 1))) + out[22]
	out[24] = T(((-(((in[5] >> 8) & 0x7F) & 1)) ^ (((in[5] >> 8) & 0x7F) >> 1))) + out[23]
	out[25] = T(((-(((in[5] >> 15) & 0x7F) & 1)) ^ (((in[5] >> 15) & 0x7F) >> 1))) + out[24]
	out[26] = T(((-(((in[5] >> 22) & 0x7F) & 1)) ^ (((in[5] >> 22) & 0x7F) >> 1))) + out[25]
	out[27] = T(((-(((in[5] >> 29) | ((in[6] & 0xF) << 3)) & 1)) ^ (((in[5] >> 29) | ((in[6] & 0xF) << 3)) >> 1))) + out[26]
	out[28] = T(((-(((in[6] >> 4) & 0x7F) & 1)) ^ (((in[6] >> 4) & 0x7F) >> 1))) + out[27]
	out[29] = T(((-(((in[6] >> 11) & 0x7F) & 1)) ^ (((in[6] >> 11) & 0x7F) >> 1))) + out[28]
	out[30] = T(((-(((in[6] >> 18) & 0x7F) & 1)) ^ (((in[6] >> 18) & 0x7F) >> 1))) + out[29]
	out[31] = T(((-((in[6] >> 25) & 1)) ^ ((in[6] >> 25) >> 1))) + out[30]
}

func deltaunpackzigzag32_8[T uint32 | int32](initoffset T, in *[8]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0xFF) & 1)) ^ (((in[0] >> 0) & 0xFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 8) & 0xFF) & 1)) ^ (((in[0] >> 8) & 0xFF) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 16) & 0xFF) & 1)) ^ (((in[0] >> 16) & 0xFF) >> 1))) + out[1]
	out[3] = T(((-((in[0] >> 24) & 1)) ^ ((in[0] >> 24) >> 1))) + out[2]
	out[4] = T(((-(((in[1] >> 0) & 0xFF) & 1)) ^ (((in[1] >> 0) & 0xFF) >> 1))) + out[3]
	out[5] = T(((-(((in[1] >> 8) & 0xFF) & 1)) ^ (((in[1] >> 8) & 0xFF) >> 1))) + out[4]
	out[6] = T(((-(((in[1] >> 16) & 0xFF) & 1)) ^ (((in[1] >> 16) & 0xFF) >> 1))) + out[5]
	out[7] = T(((-((in[1] >> 24) & 1)) ^ ((in[1] >> 24) >> 1))) + out[6]
	out[8] = T(((-(((in[2] >> 0) & 0xFF) & 1)) ^ (((in[2] >> 0) & 0xFF) >> 1))) + out[7]
	out[9] = T(((-(((in[2] >> 8) & 0xFF) & 1)) ^ (((in[2] >> 8) & 0xFF) >> 1))) + out[8]
	out[10] = T(((-(((in[2] >> 16) & 0xFF) & 1)) ^ (((in[2] >> 16) & 0xFF) >> 1))) + out[9]
	out[11] = T(((-((in[2] >> 24) & 1)) ^ ((in[2] >> 24) >> 1))) + out[10]
	out[12] = T(((-(((in[3] >> 0) & 0xFF) & 1)) ^ (((in[3] >> 0) & 0xFF) >> 1))) + out[11]
	out[13] = T(((-(((in[3] >> 8) & 0xFF) & 1)) ^ (((in[3] >> 8) & 0xFF) >> 1))) + out[12]
	out[14] = T(((-(((in[3] >> 16) & 0xFF) & 1)) ^ (((in[3] >> 16) & 0xFF) >> 1))) + out[13]
	out[15] = T(((-((in[3] >> 24) & 1)) ^ ((in[3] >> 24) >> 1))) + out[14]
	out[16] = T(((-(((in[4] >> 0) & 0xFF) & 1)) ^ (((in[4] >> 0) & 0xFF) >> 1))) + out[15]
	out[17] = T(((-(((in[4] >> 8) & 0xFF) & 1)) ^ (((in[4] >> 8) & 0xFF) >> 1))) + out[16]
	out[18] = T(((-(((in[4] >> 16) & 0xFF) & 1)) ^ (((in[4] >> 16) & 0xFF) >> 1))) + out[17]
	out[19] = T(((-((in[4] >> 24) & 1)) ^ ((in[4] >> 24) >> 1))) + out[18]
	out[20] = T(((-(((in[5] >> 0) & 0xFF) & 1)) ^ (((in[5] >> 0) & 0xFF) >> 1))) + out[19]
	out[21] = T(((-(((in[5] >> 8) & 0xFF) & 1)) ^ (((in[5] >> 8) & 0xFF) >> 1))) + out[20]
	out[22] = T(((-(((in[5] >> 16) & 0xFF) & 1)) ^ (((in[5] >> 16) & 0xFF) >> 1))) + out[21]
	out[23] = T(((-((in[5] >> 24) & 1)) ^ ((in[5] >> 24) >> 1))) + out[22]
	out[24] = T(((-(((in[6] >> 0) & 0xFF) & 1)) ^ (((in[6] >> 0) & 0xFF) >> 1))) + out[23]
	out[25] = T(((-(((in[6] >> 8) & 0xFF) & 1)) ^ (((in[6] >> 8) & 0xFF) >> 1))) + out[24]
	out[26] = T(((-(((in[6] >> 16) & 0xFF) & 1)) ^ (((in[6] >> 16) & 0xFF) >> 1))) + out[25]
	out[27] = T(((-((in[6] >> 24) & 1)) ^ ((in[6] >> 24) >> 1))) + out[26]
	out[28] = T(((-(((in[7] >> 0) & 0xFF) & 1)) ^ (((in[7] >> 0) & 0xFF) >> 1))) + out[27]
	out[29] = T(((-(((in[7] >> 8) & 0xFF) & 1)) ^ (((in[7] >> 8) & 0xFF) >> 1))) + out[28]
	out[30] = T(((-(((in[7] >> 16) & 0xFF) & 1)) ^ (((in[7] >> 16) & 0xFF) >> 1))) + out[29]
	out[31] = T(((-((in[7] >> 24) & 1)) ^ ((in[7] >> 24) >> 1))) + out[30]
}

func deltaunpackzigzag32_9[T uint32 | int32](initoffset T, in *[9]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x1FF) & 1)) ^ (((in[0] >> 0) & 0x1FF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 9) & 0x1FF) & 1)) ^ (((in[0] >> 9) & 0x1FF) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 18) & 0x1FF) & 1)) ^ (((in[0] >> 18) & 0x1FF) >> 1))) + out[1]
	out[3] = T(((-(((in[0] >> 27) | ((in[1] & 0xF) << 5)) & 1)) ^ (((in[0] >> 27) | ((in[1] & 0xF) << 5)) >> 1))) + out[2]
	out[4] = T(((-(((in[1] >> 4) & 0x1FF) & 1)) ^ (((in[1] >> 4) & 0x1FF) >> 1))) + out[3]
	out[5] = T(((-(((in[1] >> 13) & 0x1FF) & 1)) ^ (((in[1] >> 13) & 0x1FF) >> 1))) + out[4]
	out[6] = T(((-(((in[1] >> 22) & 0x1FF) & 1)) ^ (((in[1] >> 22) & 0x1FF) >> 1))) + out[5]
	out[7] = T(((-(((in[1] >> 31) | ((in[2] & 0xFF) << 1)) & 1)) ^ (((in[1] >> 31) | ((in[2] & 0xFF) << 1)) >> 1))) + out[6]
	out[8] = T(((-(((in[2] >> 8) & 0x1FF) & 1)) ^ (((in[2] >> 8) & 0x1FF) >> 1))) + out[7]
	out[9] = T(((-(((in[2] >> 17) & 0x1FF) & 1)) ^ (((in[2] >> 17) & 0x1FF) >> 1))) + out[8]
	out[10] = T(((-(((in[2] >> 26) | ((in[3] & 0x7) << 6)) & 1)) ^ (((in[2] >> 26) | ((in[3] & 0x7) << 6)) >> 1))) + out[9]
	out[11] = T(((-(((in[3] >> 3) & 0x1FF) & 1)) ^ (((in[3] >> 3) & 0x1FF) >> 1))) + out[10]
	out[12] = T(((-(((in[3] >> 12) & 0x1FF) & 1)) ^ (((in[3] >> 12) & 0x1FF) >> 1))) + out[11]
	out[13] = T(((-(((in[3] >> 21) & 0x1FF) & 1)) ^ (((in[3] >> 21) & 0x1FF) >> 1))) + out[12]
	out[14] = T(((-(((in[3] >> 30) | ((in[4] & 0x7F) << 2)) & 1)) ^ (((in[3] >> 30) | ((in[4] & 0x7F) << 2)) >> 1))) + out[13]
	out[15] = T(((-(((in[4] >> 7) & 0x1FF) & 1)) ^ (((in[4] >> 7) & 0x1FF) >> 1))) + out[14]
	out[16] = T(((-(((in[4] >> 16) & 0x1FF) & 1)) ^ (((in[4] >> 16) & 0x1FF) >> 1))) + out[15]
	out[17] = T(((-(((in[4] >> 25) | ((in[5] & 0x3) << 7)) & 1)) ^ (((in[4] >> 25) | ((in[5] & 0x3) << 7)) >> 1))) + out[16]
	out[18] = T(((-(((in[5] >> 2) & 0x1FF) & 1)) ^ (((in[5] >> 2) & 0x1FF) >> 1))) + out[17]
	out[19] = T(((-(((in[5] >> 11) & 0x1FF) & 1)) ^ (((in[5] >> 11) & 0x1FF) >> 1))) + out[18]
	out[20] = T(((-(((in[5] >> 20) & 0x1FF) & 1)) ^ (((in[5] >> 20) & 0x1FF) >> 1))) + out[19]
	out[21] = T(((-(((in[5] >> 29) | ((in[6] & 0x3F) << 3)) & 1)) ^ (((in[5] >> 29) | ((in[6] & 0x3F) << 3)) >> 1))) + out[20]
	out[22] = T(((-(((in[6] >> 6) & 0x1FF) & 1)) ^ (((in[6] >> 6) & 0x1FF) >> 1))) + out[21]
	out[23] = T(((-(((in[6] >> 15) & 0x1FF) & 1)) ^ (((in[6] >> 15) & 0x1FF) >> 1))) + out[22]
	out[24] = T(((-(((in[6] >> 24) | ((in[7] & 0x1) << 8)) & 1)) ^ (((in[6] >> 24) | ((in[7] & 0x1) << 8)) >> 1))) + out[23]
	out[25] = T(((-(((in[7] >> 1) & 0x1FF) & 1)) ^ (((in[7] >> 1) & 0x1FF) >> 1))) + out[24]
	out[26] = T(((-(((in[7] >> 10) & 0x1FF) & 1)) ^ (((in[7] >> 10) & 0x1FF) >> 1))) + out[25]
	out[27] = T(((-(((in[7] >> 19) & 0x1FF) & 1)) ^ (((in[7] >> 19) & 0x1FF) >> 1))) + out[26]
	out[28] = T(((-(((in[7] >> 28) | ((in[8] & 0x1F) << 4)) & 1)) ^ (((in[7] >> 28) | ((in[8] & 0x1F) << 4)) >> 1))) + out[27]
	out[29] = T(((-(((in[8] >> 5) & 0x1FF) & 1)) ^ (((in[8] >> 5) & 0x1FF) >> 1))) + out[28]
	out[30] = T(((-(((in[8] >> 14) & 0x1FF) & 1)) ^ (((in[8] >> 14) & 0x1FF) >> 1))) + out[29]
	out[31] = T(((-((in[8] >> 23) & 1)) ^ ((in[8] >> 23) >> 1))) + out[30]
}

func deltaunpackzigzag32_10[T uint32 | int32](initoffset T, in *[10]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x3FF) & 1)) ^ (((in[0] >> 0) & 0x3FF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 10) & 0x3FF) & 1)) ^ (((in[0] >> 10) & 0x3FF) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 20) & 0x3FF) & 1)) ^ (((in[0] >> 20) & 0x3FF) >> 1))) + out[1]
	out[3] = T(((-(((in[0] >> 30) | ((in[1] & 0xFF) << 2)) & 1)) ^ (((in[0] >> 30) | ((in[1] & 0xFF) << 2)) >> 1))) + out[2]
	out[4] = T(((-(((in[1] >> 8) & 0x3FF) & 1)) ^ (((in[1] >> 8) & 0x3FF) >> 1))) + out[3]
	out[5] = T(((-(((in[1] >> 18) & 0x3FF) & 1)) ^ (((in[1] >> 18) & 0x3FF) >> 1))) + out[4]
	out[6] = T(((-(((in[1] >> 28) | ((in[2] & 0x3F) << 4)) & 1)) ^ (((in[1] >> 28) | ((in[2] & 0x3F) << 4)) >> 1))) + out[5]
	out[7] = T(((-(((in[2] >> 6) & 0x3FF) & 1)) ^ (((in[2] >> 6) & 0x3FF) >> 1))) + out[6]
	out[8] = T(((-(((in[2] >> 16) & 0x3FF) & 1)) ^ (((in[2] >> 16) & 0x3FF) >> 1))) + out[7]
	out[9] = T(((-(((in[2] >> 26) | ((in[3] & 0xF) << 6)) & 1)) ^ (((in[2] >> 26) | ((in[3] & 0xF) << 6)) >> 1))) + out[8]
	out[10] = T(((-(((in[3] >> 4) & 0x3FF) & 1)) ^ (((in[3] >> 4) & 0x3FF) >> 1))) + out[9]
	out[11] = T(((-(((in[3] >> 14) & 0x3FF) & 1)) ^ (((in[3] >> 14) & 0x3FF) >> 1))) + out[10]
	out[12] = T(((-(((in[3] >> 24) | ((in[4] & 0x3) << 8)) & 1)) ^ (((in[3] >> 24) | ((in[4] & 0x3) << 8)) >> 1))) + out[11]
	out[13] = T(((-(((in[4] >> 2) & 0x3FF) & 1)) ^ (((in[4] >> 2) & 0x3FF) >> 1))) + out[12]
	out[14] = T(((-(((in[4] >> 12) & 0x3FF) & 1)) ^ (((in[4] >> 12) & 0x3FF) >> 1))) + out[13]
	out[15] = T(((-((in[4] >> 22) & 1)) ^ ((in[4] >> 22) >> 1))) + out[14]
	out[16] = T(((-(((in[5] >> 0) & 0x3FF) & 1)) ^ (((in[5] >> 0) & 0x3FF) >> 1))) + out[15]
	out[17] = T(((-(((in[5] >> 10) & 0x3FF) & 1)) ^ (((in[5] >> 10) & 0x3FF) >> 1))) + out[16]
	out[18] = T(((-(((in[5] >> 20) & 0x3FF) & 1)) ^ (((in[5] >> 20) & 0x3FF) >> 1))) + out[17]
	out[19] = T(((-(((in[5] >> 30) | ((in[6] & 0xFF) << 2)) & 1)) ^ (((in[5] >> 30) | ((in[6] & 0xFF) << 2)) >> 1))) + out[18]
	out[20] = T(((-(((in[6] >> 8) & 0x3FF) & 1)) ^ (((in[6] >> 8) & 0x3FF) >> 1))) + out[19]
	out[21] = T(((-(((in[6] >> 18) & 0x3FF) & 1)) ^ (((in[6] >> 18) & 0x3FF) >> 1))) + out[20]
	out[22] = T(((-(((in[6] >> 28) | ((in[7] & 0x3F) << 4)) & 1)) ^ (((in[6] >> 28) | ((in[7] & 0x3F) << 4)) >> 1))) + out[21]
	out[23] = T(((-(((in[7] >> 6) & 0x3FF) & 1)) ^ (((in[7] >> 6) & 0x3FF) >> 1))) + out[22]
	out[24] = T(((-(((in[7] >> 16) & 0x3FF) & 1)) ^ (((in[7] >> 16) & 0x3FF) >> 1))) + out[23]
	out[25] = T(((-(((in[7] >> 26) | ((in[8] & 0xF) << 6)) & 1)) ^ (((in[7] >> 26) | ((in[8] & 0xF) << 6)) >> 1))) + out[24]
	out[26] = T(((-(((in[8] >> 4) & 0x3FF) & 1)) ^ (((in[8] >> 4) & 0x3FF) >> 1))) + out[25]
	out[27] = T(((-(((in[8] >> 14) & 0x3FF) & 1)) ^ (((in[8] >> 14) & 0x3FF) >> 1))) + out[26]
	out[28] = T(((-(((in[8] >> 24) | ((in[9] & 0x3) << 8)) & 1)) ^ (((in[8] >> 24) | ((in[9] & 0x3) << 8)) >> 1))) + out[27]
	out[29] = T(((-(((in[9] >> 2) & 0x3FF) & 1)) ^ (((in[9] >> 2) & 0x3FF) >> 1))) + out[28]
	out[30] = T(((-(((in[9] >> 12) & 0x3FF) & 1)) ^ (((in[9] >> 12) & 0x3FF) >> 1))) + out[29]
	out[31] = T(((-((in[9] >> 22) & 1)) ^ ((in[9] >> 22) >> 1))) + out[30]
}

func deltaunpackzigzag32_11[T uint32 | int32](initoffset T, in *[11]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x7FF) & 1)) ^ (((in[0] >> 0) & 0x7FF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 11) & 0x7FF) & 1)) ^ (((in[0] >> 11) & 0x7FF) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 22) | ((in[1] & 0x1) << 10)) & 1)) ^ (((in[0] >> 22) | ((in[1] & 0x1) << 10)) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 1) & 0x7FF) & 1)) ^ (((in[1] >> 1) & 0x7FF) >> 1))) + out[2]
	out[4] = T(((-(((in[1] >> 12) & 0x7FF) & 1)) ^ (((in[1] >> 12) & 0x7FF) >> 1))) + out[3]
	out[5] = T(((-(((in[1] >> 23) | ((in[2] & 0x3) << 9)) & 1)) ^ (((in[1] >> 23) | ((in[2] & 0x3) << 9)) >> 1))) + out[4]
	out[6] = T(((-(((in[2] >> 2) & 0x7FF) & 1)) ^ (((in[2] >> 2) & 0x7FF) >> 1))) + out[5]
	out[7] = T(((-(((in[2] >> 13) & 0x7FF) & 1)) ^ (((in[2] >> 13) & 0x7FF) >> 1))) + out[6]
	out[8] = T(((-(((in[2] >> 24) | ((in[3] & 0x7) << 8)) & 1)) ^ (((in[2] >> 24) | ((in[3] & 0x7) << 8)) >> 1))) + out[7]
	out[9] = T(((-(((in[3] >> 3) & 0x7FF) & 1)) ^ (((in[3] >> 3) & 0x7FF) >> 1))) + out[8]
	out[10] = T(((-(((in[3] >> 14) & 0x7FF) & 1)) ^ (((in[3] >> 14) & 0x7FF) >> 1))) + out[9]
	out[11] = T(((-(((in[3] >> 25) | ((in[4] & 0xF) << 7)) & 1)) ^ (((in[3] >> 25) | ((in[4] & 0xF) << 7)) >> 1))) + out[10]
	out[12] = T(((-(((in[4] >> 4) & 0x7FF) & 1)) ^ (((in[4] >> 4) & 0x7FF) >> 1))) + out[11]
	out[13] = T(((-(((in[4] >> 15) & 0x7FF) & 1)) ^ (((in[4] >> 15) & 0x7FF) >> 1))) + out[12]
	out[14] = T(((-(((in[4] >> 26) | ((in[5] & 0x1F) << 6)) & 1)) ^ (((in[4] >> 26) | ((in[5] & 0x1F) << 6)) >> 1))) + out[13]
	out[15] = T(((-(((in[5] >> 5) & 0x7FF) & 1)) ^ (((in[5] >> 5) & 0x7FF) >> 1))) + out[14]
	out[16] = T(((-(((in[5] >> 16) & 0x7FF) & 1)) ^ (((in[5] >> 16) & 0x7FF) >> 1))) + out[15]
	out[17] = T(((-(((in[5] >> 27) | ((in[6] & 0x3F) << 5)) & 1)) ^ (((in[5] >> 27) | ((in[6] & 0x3F) << 5)) >> 1))) + out[16]
	out[18] = T(((-(((in[6] >> 6) & 0x7FF) & 1)) ^ (((in[6] >> 6) & 0x7FF) >> 1))) + out[17]
	out[19] = T(((-(((in[6] >> 17) & 0x7FF) & 1)) ^ (((in[6] >> 17) & 0x7FF) >> 1))) + out[18]
	out[20] = T(((-(((in[6] >> 28) | ((in[7] & 0x7F) << 4)) & 1)) ^ (((in[6] >> 28) | ((in[7] & 0x7F) << 4)) >> 1))) + out[19]
	out[21] = T(((-(((in[7] >> 7) & 0x7FF) & 1)) ^ (((in[7] >> 7) & 0x7FF) >> 1))) + out[20]
	out[22] = T(((-(((in[7] >> 18) & 0x7FF) & 1)) ^ (((in[7] >> 18) & 0x7FF) >> 1))) + out[21]
	out[23] = T(((-(((in[7] >> 29) | ((in[8] & 0xFF) << 3)) & 1)) ^ (((in[7] >> 29) | ((in[8] & 0xFF) << 3)) >> 1))) + out[22]
	out[24] = T(((-(((in[8] >> 8) & 0x7FF) & 1)) ^ (((in[8] >> 8) & 0x7FF) >> 1))) + out[23]
	out[25] = T(((-(((in[8] >> 19) & 0x7FF) & 1)) ^ (((in[8] >> 19) & 0x7FF) >> 1))) + out[24]
	out[26] = T(((-(((in[8] >> 30) | ((in[9] & 0x1FF) << 2)) & 1)) ^ (((in[8] >> 30) | ((in[9] & 0x1FF) << 2)) >> 1))) + out[25]
	out[27] = T(((-(((in[9] >> 9) & 0x7FF) & 1)) ^ (((in[9] >> 9) & 0x7FF) >> 1))) + out[26]
	out[28] = T(((-(((in[9] >> 20) & 0x7FF) & 1)) ^ (((in[9] >> 20) & 0x7FF) >> 1))) + out[27]
	out[29] = T(((-(((in[9] >> 31) | ((in[10] & 0x3FF) << 1)) & 1)) ^ (((in[9] >> 31) | ((in[10] & 0x3FF) << 1)) >> 1))) + out[28]
	out[30] = T(((-(((in[10] >> 10) & 0x7FF) & 1)) ^ (((in[10] >> 10) & 0x7FF) >> 1))) + out[29]
	out[31] = T(((-((in[10] >> 21) & 1)) ^ ((in[10] >> 21) >> 1))) + out[30]
}

func deltaunpackzigzag32_12[T uint32 | int32](initoffset T, in *[12]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0xFFF) & 1)) ^ (((in[0] >> 0) & 0xFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 12) & 0xFFF) & 1)) ^ (((in[0] >> 12) & 0xFFF) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 24) | ((in[1] & 0xF) << 8)) & 1)) ^ (((in[0] >> 24) | ((in[1] & 0xF) << 8)) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 4) & 0xFFF) & 1)) ^ (((in[1] >> 4) & 0xFFF) >> 1))) + out[2]
	out[4] = T(((-(((in[1] >> 16) & 0xFFF) & 1)) ^ (((in[1] >> 16) & 0xFFF) >> 1))) + out[3]
	out[5] = T(((-(((in[1] >> 28) | ((in[2] & 0xFF) << 4)) & 1)) ^ (((in[1] >> 28) | ((in[2] & 0xFF) << 4)) >> 1))) + out[4]
	out[6] = T(((-(((in[2] >> 8) & 0xFFF) & 1)) ^ (((in[2] >> 8) & 0xFFF) >> 1))) + out[5]
	out[7] = T(((-((in[2] >> 20) & 1)) ^ ((in[2] >> 20) >> 1))) + out[6]
	out[8] = T(((-(((in[3] >> 0) & 0xFFF) & 1)) ^ (((in[3] >> 0) & 0xFFF) >> 1))) + out[7]
	out[9] = T(((-(((in[3] >> 12) & 0xFFF) & 1)) ^ (((in[3] >> 12) & 0xFFF) >> 1))) + out[8]
	out[10] = T(((-(((in[3] >> 24) | ((in[4] & 0xF) << 8)) & 1)) ^ (((in[3] >> 24) | ((in[4] & 0xF) << 8)) >> 1))) + out[9]
	out[11] = T(((-(((in[4] >> 4) & 0xFFF) & 1)) ^ (((in[4] >> 4) & 0xFFF) >> 1))) + out[10]
	out[12] = T(((-(((in[4] >> 16) & 0xFFF) & 1)) ^ (((in[4] >> 16) & 0xFFF) >> 1))) + out[11]
	out[13] = T(((-(((in[4] >> 28) | ((in[5] & 0xFF) << 4)) & 1)) ^ (((in[4] >> 28) | ((in[5] & 0xFF) << 4)) >> 1))) + out[12]
	out[14] = T(((-(((in[5] >> 8) & 0xFFF) & 1)) ^ (((in[5] >> 8) & 0xFFF) >> 1))) + out[13]
	out[15] = T(((-((in[5] >> 20) & 1)) ^ ((in[5] >> 20) >> 1))) + out[14]
	out[16] = T(((-(((in[6] >> 0) & 0xFFF) & 1)) ^ (((in[6] >> 0) & 0xFFF) >> 1))) + out[15]
	out[17] = T(((-(((in[6] >> 12) & 0xFFF) & 1)) ^ (((in[6] >> 12) & 0xFFF) >> 1))) + out[16]
	out[18] = T(((-(((in[6] >> 24) | ((in[7] & 0xF) << 8)) & 1)) ^ (((in[6] >> 24) | ((in[7] & 0xF) << 8)) >> 1))) + out[17]
	out[19] = T(((-(((in[7] >> 4) & 0xFFF) & 1)) ^ (((in[7] >> 4) & 0xFFF) >> 1))) + out[18]
	out[20] = T(((-(((in[7] >> 16) & 0xFFF) & 1)) ^ (((in[7] >> 16) & 0xFFF) >> 1))) + out[19]
	out[21] = T(((-(((in[7] >> 28) | ((in[8] & 0xFF) << 4)) & 1)) ^ (((in[7] >> 28) | ((in[8] & 0xFF) << 4)) >> 1))) + out[20]
	out[22] = T(((-(((in[8] >> 8) & 0xFFF) & 1)) ^ (((in[8] >> 8) & 0xFFF) >> 1))) + out[21]
	out[23] = T(((-((in[8] >> 20) & 1)) ^ ((in[8] >> 20) >> 1))) + out[22]
	out[24] = T(((-(((in[9] >> 0) & 0xFFF) & 1)) ^ (((in[9] >> 0) & 0xFFF) >> 1))) + out[23]
	out[25] = T(((-(((in[9] >> 12) & 0xFFF) & 1)) ^ (((in[9] >> 12) & 0xFFF) >> 1))) + out[24]
	out[26] = T(((-(((in[9] >> 24) | ((in[10] & 0xF) << 8)) & 1)) ^ (((in[9] >> 24) | ((in[10] & 0xF) << 8)) >> 1))) + out[25]
	out[27] = T(((-(((in[10] >> 4) & 0xFFF) & 1)) ^ (((in[10] >> 4) & 0xFFF) >> 1))) + out[26]
	out[28] = T(((-(((in[10] >> 16) & 0xFFF) & 1)) ^ (((in[10] >> 16) & 0xFFF) >> 1))) + out[27]
	out[29] = T(((-(((in[10] >> 28) | ((in[11] & 0xFF) << 4)) & 1)) ^ (((in[10] >> 28) | ((in[11] & 0xFF) << 4)) >> 1))) + out[28]
	out[30] = T(((-(((in[11] >> 8) & 0xFFF) & 1)) ^ (((in[11] >> 8) & 0xFFF) >> 1))) + out[29]
	out[31] = T(((-((in[11] >> 20) & 1)) ^ ((in[11] >> 20) >> 1))) + out[30]
}

func deltaunpackzigzag32_13[T uint32 | int32](initoffset T, in *[13]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x1FFF) & 1)) ^ (((in[0] >> 0) & 0x1FFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 13) & 0x1FFF) & 1)) ^ (((in[0] >> 13) & 0x1FFF) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 26) | ((in[1] & 0x7F) << 6)) & 1)) ^ (((in[0] >> 26) | ((in[1] & 0x7F) << 6)) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 7) & 0x1FFF) & 1)) ^ (((in[1] >> 7) & 0x1FFF) >> 1))) + out[2]
	out[4] = T(((-(((in[1] >> 20) | ((in[2] & 0x1) << 12)) & 1)) ^ (((in[1] >> 20) | ((in[2] & 0x1) << 12)) >> 1))) + out[3]
	out[5] = T(((-(((in[2] >> 1) & 0x1FFF) & 1)) ^ (((in[2] >> 1) & 0x1FFF) >> 1))) + out[4]
	out[6] = T(((-(((in[2] >> 14) & 0x1FFF) & 1)) ^ (((in[2] >> 14) & 0x1FFF) >> 1))) + out[5]
	out[7] = T(((-(((in[2] >> 27) | ((in[3] & 0xFF) << 5)) & 1)) ^ (((in[2] >> 27) | ((in[3] & 0xFF) << 5)) >> 1))) + out[6]
	out[8] = T(((-(((in[3] >> 8) & 0x1FFF) & 1)) ^ (((in[3] >> 8) & 0x1FFF) >> 1))) + out[7]
	out[9] = T(((-(((in[3] >> 21) | ((in[4] & 0x3) << 11)) & 1)) ^ (((in[3] >> 21) | ((in[4] & 0x3) << 11)) >> 1))) + out[8]
	out[10] = T(((-(((in[4] >> 2) & 0x1FFF) & 1)) ^ (((in[4] >> 2) & 0x1FFF) >> 1))) + out[9]
	out[11] = T(((-(((in[4] >> 15) & 0x1FFF) & 1)) ^ (((in[4] >> 15) & 0x1FFF) >> 1))) + out[10]
	out[12] = T(((-(((in[4] >> 28) | ((in[5] & 0x1FF) << 4)) & 1)) ^ (((in[4] >> 28) | ((in[5] & 0x1FF) << 4)) >> 1))) + out[11]
	out[13] = T(((-(((in[5] >> 9) & 0x1FFF) & 1)) ^ (((in[5] >> 9) & 0x1FFF) >> 1))) + out[12]
	out[14] = T(((-(((in[5] >> 22) | ((in[6] & 0x7) << 10)) & 1)) ^ (((in[5] >> 22) | ((in[6] & 0x7) << 10)) >> 1))) + out[13]
	out[15] = T(((-(((in[6] >> 3) & 0x1FFF) & 1)) ^ (((in[6] >> 3) & 0x1FFF) >> 1))) + out[14]
	out[16] = T(((-(((in[6] >> 16) & 0x1FFF) & 1)) ^ (((in[6] >> 16) & 0x1FFF) >> 1))) + out[15]
	out[17] = T(((-(((in[6] >> 29) | ((in[7] & 0x3FF) << 3)) & 1)) ^ (((in[6] >> 29) | ((in[7] & 0x3FF) << 3)) >> 1))) + out[16]
	out[18] = T(((-(((in[7] >> 10) & 0x1FFF) & 1)) ^ (((in[7] >> 10) & 0x1FFF) >> 1))) + out[17]
	out[19] = T(((-(((in[7] >> 23) | ((in[8] & 0xF) << 9)) & 1)) ^ (((in[7] >> 23) | ((in[8] & 0xF) << 9)) >> 1))) + out[18]
	out[20] = T(((-(((in[8] >> 4) & 0x1FFF) & 1)) ^ (((in[8] >> 4) & 0x1FFF) >> 1))) + out[19]
	out[21] = T(((-(((in[8] >> 17) & 0x1FFF) & 1)) ^ (((in[8] >> 17) & 0x1FFF) >> 1))) + out[20]
	out[22] = T(((-(((in[8] >> 30) | ((in[9] & 0x7FF) << 2)) & 1)) ^ (((in[8] >> 30) | ((in[9] & 0x7FF) << 2)) >> 1))) + out[21]
	out[23] = T(((-(((in[9] >> 11) & 0x1FFF) & 1)) ^ (((in[9] >> 11) & 0x1FFF) >> 1))) + out[22]
	out[24] = T(((-(((in[9] >> 24) | ((in[10] & 0x1F) << 8)) & 1)) ^ (((in[9] >> 24) | ((in[10] & 0x1F) << 8)) >> 1))) + out[23]
	out[25] = T(((-(((in[10] >> 5) & 0x1FFF) & 1)) ^ (((in[10] >> 5) & 0x1FFF) >> 1))) + out[24]
	out[26] = T(((-(((in[10] >> 18) & 0x1FFF) & 1)) ^ (((in[10] >> 18) & 0x1FFF) >> 1))) + out[25]
	out[27] = T(((-(((in[10] >> 31) | ((in[11] & 0xFFF) << 1)) & 1)) ^ (((in[10] >> 31) | ((in[11] & 0xFFF) << 1)) >> 1))) + out[26]
	out[28] = T(((-(((in[11] >> 12) & 0x1FFF) & 1)) ^ (((in[11] >> 12) & 0x1FFF) >> 1))) + out[27]
	out[29] = T(((-(((in[11] >> 25) | ((in[12] & 0x3F) << 7)) & 1)) ^ (((in[11] >> 25) | ((in[12] & 0x3F) << 7)) >> 1))) + out[28]
	out[30] = T(((-(((in[12] >> 6) & 0x1FFF) & 1)) ^ (((in[12] >> 6) & 0x1FFF) >> 1))) + out[29]
	out[31] = T(((-((in[12] >> 19) & 1)) ^ ((in[12] >> 19) >> 1))) + out[30]
}

func deltaunpackzigzag32_14[T uint32 | int32](initoffset T, in *[14]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x3FFF) & 1)) ^ (((in[0] >> 0) & 0x3FFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 14) & 0x3FFF) & 1)) ^ (((in[0] >> 14) & 0x3FFF) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 28) | ((in[1] & 0x3FF) << 4)) & 1)) ^ (((in[0] >> 28) | ((in[1] & 0x3FF) << 4)) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 10) & 0x3FFF) & 1)) ^ (((in[1] >> 10) & 0x3FFF) >> 1))) + out[2]
	out[4] = T(((-(((in[1] >> 24) | ((in[2] & 0x3F) << 8)) & 1)) ^ (((in[1] >> 24) | ((in[2] & 0x3F) << 8)) >> 1))) + out[3]
	out[5] = T(((-(((in[2] >> 6) & 0x3FFF) & 1)) ^ (((in[2] >> 6) & 0x3FFF) >> 1))) + out[4]
	out[6] = T(((-(((in[2] >> 20) | ((in[3] & 0x3) << 12)) & 1)) ^ (((in[2] >> 20) | ((in[3] & 0x3) << 12)) >> 1))) + out[5]
	out[7] = T(((-(((in[3] >> 2) & 0x3FFF) & 1)) ^ (((in[3] >> 2) & 0x3FFF) >> 1))) + out[6]
	out[8] = T(((-(((in[3] >> 16) & 0x3FFF) & 1)) ^ (((in[3] >> 16) & 0x3FFF) >> 1))) + out[7]
	out[9] = T(((-(((in[3] >> 30) | ((in[4] & 0xFFF) << 2)) & 1)) ^ (((in[3] >> 30) | ((in[4] & 0xFFF) << 2)) >> 1))) + out[8]
	out[10] = T(((-(((in[4] >> 12) & 0x3FFF) & 1)) ^ (((in[4] >> 12) & 0x3FFF) >> 1))) + out[9]
	out[11] = T(((-(((in[4] >> 26) | ((in[5] & 0xFF) << 6)) & 1)) ^ (((in[4] >> 26) | ((in[5] & 0xFF) << 6)) >> 1))) + out[10]
	out[12] = T(((-(((in[5] >> 8) & 0x3FFF) & 1)) ^ (((in[5] >> 8) & 0x3FFF) >> 1))) + out[11]
	out[13] = T(((-(((in[5] >> 22) | ((in[6] & 0xF) << 10)) & 1)) ^ (((in[5] >> 22) | ((in[6] & 0xF) << 10)) >> 1))) + out[12]
	out[14] = T(((-(((in[6] >> 4) & 0x3FFF) & 1)) ^ (((in[6] >> 4) & 0x3FFF) >> 1))) + out[13]
	out[15] = T(((-((in[6] >> 18) & 1)) ^ ((in[6] >> 18) >> 1))) + out[14]
	out[16] = T(((-(((in[7] >> 0) & 0x3FFF) & 1)) ^ (((in[7] >> 0) & 0x3FFF) >> 1))) + out[15]
	out[17] = T(((-(((in[7] >> 14) & 0x3FFF) & 1)) ^ (((in[7] >> 14) & 0x3FFF) >> 1))) + out[16]
	out[18] = T(((-(((in[7] >> 28) | ((in[8] & 0x3FF) << 4)) & 1)) ^ (((in[7] >> 28) | ((in[8] & 0x3FF) << 4)) >> 1))) + out[17]
	out[19] = T(((-(((in[8] >> 10) & 0x3FFF) & 1)) ^ (((in[8] >> 10) & 0x3FFF) >> 1))) + out[18]
	out[20] = T(((-(((in[8] >> 24) | ((in[9] & 0x3F) << 8)) & 1)) ^ (((in[8] >> 24) | ((in[9] & 0x3F) << 8)) >> 1))) + out[19]
	out[21] = T(((-(((in[9] >> 6) & 0x3FFF) & 1)) ^ (((in[9] >> 6) & 0x3FFF) >> 1))) + out[20]
	out[22] = T(((-(((in[9] >> 20) | ((in[10] & 0x3) << 12)) & 1)) ^ (((in[9] >> 20) | ((in[10] & 0x3) << 12)) >> 1))) + out[21]
	out[23] = T(((-(((in[10] >> 2) & 0x3FFF) & 1)) ^ (((in[10] >> 2) & 0x3FFF) >> 1))) + out[22]
	out[24] = T(((-(((in[10] >> 16) & 0x3FFF) & 1)) ^ (((in[10] >> 16) & 0x3FFF) >> 1))) + out[23]
	out[25] = T(((-(((in[10] >> 30) | ((in[11] & 0xFFF) << 2)) & 1)) ^ (((in[10] >> 30) | ((in[11] & 0xFFF) << 2)) >> 1))) + out[24]
	out[26] = T(((-(((in[11] >> 12) & 0x3FFF) & 1)) ^ (((in[11] >> 12) & 0x3FFF) >> 1))) + out[25]
	out[27] = T(((-(((in[11] >> 26) | ((in[12] & 0xFF) << 6)) & 1)) ^ (((in[11] >> 26) | ((in[12] & 0xFF) << 6)) >> 1))) + out[26]
	out[28] = T(((-(((in[12] >> 8) & 0x3FFF) & 1)) ^ (((in[12] >> 8) & 0x3FFF) >> 1))) + out[27]
	out[29] = T(((-(((in[12] >> 22) | ((in[13] & 0xF) << 10)) & 1)) ^ (((in[12] >> 22) | ((in[13] & 0xF) << 10)) >> 1))) + out[28]
	out[30] = T(((-(((in[13] >> 4) & 0x3FFF) & 1)) ^ (((in[13] >> 4) & 0x3FFF) >> 1))) + out[29]
	out[31] = T(((-((in[13] >> 18) & 1)) ^ ((in[13] >> 18) >> 1))) + out[30]
}

func deltaunpackzigzag32_15[T uint32 | int32](initoffset T, in *[15]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x7FFF) & 1)) ^ (((in[0] >> 0) & 0x7FFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 15) & 0x7FFF) & 1)) ^ (((in[0] >> 15) & 0x7FFF) >> 1))) + out[0]
	out[2] = T(((-(((in[0] >> 30) | ((in[1] & 0x1FFF) << 2)) & 1)) ^ (((in[0] >> 30) | ((in[1] & 0x1FFF) << 2)) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 13) & 0x7FFF) & 1)) ^ (((in[1] >> 13) & 0x7FFF) >> 1))) + out[2]
	out[4] = T(((-(((in[1] >> 28) | ((in[2] & 0x7FF) << 4)) & 1)) ^ (((in[1] >> 28) | ((in[2] & 0x7FF) << 4)) >> 1))) + out[3]
	out[5] = T(((-(((in[2] >> 11) & 0x7FFF) & 1)) ^ (((in[2] >> 11) & 0x7FFF) >> 1))) + out[4]
	out[6] = T(((-(((in[2] >> 26) | ((in[3] & 0x1FF) << 6)) & 1)) ^ (((in[2] >> 26) | ((in[3] & 0x1FF) << 6)) >> 1))) + out[5]
	out[7] = T(((-(((in[3] >> 9) & 0x7FFF) & 1)) ^ (((in[3] >> 9) & 0x7FFF) >> 1))) + out[6]
	out[8] = T(((-(((in[3] >> 24) | ((in[4] & 0x7F) << 8)) & 1)) ^ (((in[3] >> 24) | ((in[4] & 0x7F) << 8)) >> 1))) + out[7]
	out[9] = T(((-(((in[4] >> 7) & 0x7FFF) & 1)) ^ (((in[4] >> 7) & 0x7FFF) >> 1))) + out[8]
	out[10] = T(((-(((in[4] >> 22) | ((in[5] & 0x1F) << 10)) & 1)) ^ (((in[4] >> 22) | ((in[5] & 0x1F) << 10)) >> 1))) + out[9]
	out[11] = T(((-(((in[5] >> 5) & 0x7FFF) & 1)) ^ (((in[5] >> 5) & 0x7FFF) >> 1))) + out[10]
	out[12] = T(((-(((in[5] >> 20) | ((in[6] & 0x7) << 12)) & 1)) ^ (((in[5] >> 20) | ((in[6] & 0x7) << 12)) >> 1))) + out[11]
	out[13] = T(((-(((in[6] >> 3) & 0x7FFF) & 1)) ^ (((in[6] >> 3) & 0x7FFF) >> 1))) + out[12]
	out[14] = T(((-(((in[6] >> 18) | ((in[7] & 0x1) << 14)) & 1)) ^ (((in[6] >> 18) | ((in[7] & 0x1) << 14)) >> 1))) + out[13]
	out[15] = T(((-(((in[7] >> 1) & 0x7FFF) & 1)) ^ (((in[7] >> 1) & 0x7FFF) >> 1))) + out[14]
	out[16] = T(((-(((in[7] >> 16) & 0x7FFF) & 1)) ^ (((in[7] >> 16) & 0x7FFF) >> 1))) + out[15]
	out[17] = T(((-(((in[7] >> 31) | ((in[8] & 0x3FFF) << 1)) & 1)) ^ (((in[7] >> 31) | ((in[8] & 0x3FFF) << 1)) >> 1))) + out[16]
	out[18] = T(((-(((in[8] >> 14) & 0x7FFF) & 1)) ^ (((in[8] >> 14) & 0x7FFF) >> 1))) + out[17]
	out[19] = T(((-(((in[8] >> 29) | ((in[9] & 0xFFF) << 3)) & 1)) ^ (((in[8] >> 29) | ((in[9] & 0xFFF) << 3)) >> 1))) + out[18]
	out[20] = T(((-(((in[9] >> 12) & 0x7FFF) & 1)) ^ (((in[9] >> 12) & 0x7FFF) >> 1))) + out[19]
	out[21] = T(((-(((in[9] >> 27) | ((in[10] & 0x3FF) << 5)) & 1)) ^ (((in[9] >> 27) | ((in[10] & 0x3FF) << 5)) >> 1))) + out[20]
	out[22] = T(((-(((in[10] >> 10) & 0x7FFF) & 1)) ^ (((in[10] >> 10) & 0x7FFF) >> 1))) + out[21]
	out[23] = T(((-(((in[10] >> 25) | ((in[11] & 0xFF) << 7)) & 1)) ^ (((in[10] >> 25) | ((in[11] & 0xFF) << 7)) >> 1))) + out[22]
	out[24] = T(((-(((in[11] >> 8) & 0x7FFF) & 1)) ^ (((in[11] >> 8) & 0x7FFF) >> 1))) + out[23]
	out[25] = T(((-(((in[11] >> 23) | ((in[12] & 0x3F) << 9)) & 1)) ^ (((in[11] >> 23) | ((in[12] & 0x3F) << 9)) >> 1))) + out[24]
	out[26] = T(((-(((in[12] >> 6) & 0x7FFF) & 1)) ^ (((in[12] >> 6) & 0x7FFF) >> 1))) + out[25]
	out[27] = T(((-(((in[12] >> 21) | ((in[13] & 0xF) << 11)) & 1)) ^ (((in[12] >> 21) | ((in[13] & 0xF) << 11)) >> 1))) + out[26]
	out[28] = T(((-(((in[13] >> 4) & 0x7FFF) & 1)) ^ (((in[13] >> 4) & 0x7FFF) >> 1))) + out[27]
	out[29] = T(((-(((in[13] >> 19) | ((in[14] & 0x3) << 13)) & 1)) ^ (((in[13] >> 19) | ((in[14] & 0x3) << 13)) >> 1))) + out[28]
	out[30] = T(((-(((in[14] >> 2) & 0x7FFF) & 1)) ^ (((in[14] >> 2) & 0x7FFF) >> 1))) + out[29]
	out[31] = T(((-((in[14] >> 17) & 1)) ^ ((in[14] >> 17) >> 1))) + out[30]
}

func deltaunpackzigzag32_16[T uint32 | int32](initoffset T, in *[16]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0xFFFF) & 1)) ^ (((in[0] >> 0) & 0xFFFF) >> 1))) + initoffset
	out[1] = T(((-((in[0] >> 16) & 1)) ^ ((in[0] >> 16) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 0) & 0xFFFF) & 1)) ^ (((in[1] >> 0) & 0xFFFF) >> 1))) + out[1]
	out[3] = T(((-((in[1] >> 16) & 1)) ^ ((in[1] >> 16) >> 1))) + out[2]
	out[4] = T(((-(((in[2] >> 0) & 0xFFFF) & 1)) ^ (((in[2] >> 0) & 0xFFFF) >> 1))) + out[3]
	out[5] = T(((-((in[2] >> 16) & 1)) ^ ((in[2] >> 16) >> 1))) + out[4]
	out[6] = T(((-(((in[3] >> 0) & 0xFFFF) & 1)) ^ (((in[3] >> 0) & 0xFFFF) >> 1))) + out[5]
	out[7] = T(((-((in[3] >> 16) & 1)) ^ ((in[3] >> 16) >> 1))) + out[6]
	out[8] = T(((-(((in[4] >> 0) & 0xFFFF) & 1)) ^ (((in[4] >> 0) & 0xFFFF) >> 1))) + out[7]
	out[9] = T(((-((in[4] >> 16) & 1)) ^ ((in[4] >> 16) >> 1))) + out[8]
	out[10] = T(((-(((in[5] >> 0) & 0xFFFF) & 1)) ^ (((in[5] >> 0) & 0xFFFF) >> 1))) + out[9]
	out[11] = T(((-((in[5] >> 16) & 1)) ^ ((in[5] >> 16) >> 1))) + out[10]
	out[12] = T(((-(((in[6] >> 0) & 0xFFFF) & 1)) ^ (((in[6] >> 0) & 0xFFFF) >> 1))) + out[11]
	out[13] = T(((-((in[6] >> 16) & 1)) ^ ((in[6] >> 16) >> 1))) + out[12]
	out[14] = T(((-(((in[7] >> 0) & 0xFFFF) & 1)) ^ (((in[7] >> 0) & 0xFFFF) >> 1))) + out[13]
	out[15] = T(((-((in[7] >> 16) & 1)) ^ ((in[7] >> 16) >> 1))) + out[14]
	out[16] = T(((-(((in[8] >> 0) & 0xFFFF) & 1)) ^ (((in[8] >> 0) & 0xFFFF) >> 1))) + out[15]
	out[17] = T(((-((in[8] >> 16) & 1)) ^ ((in[8] >> 16) >> 1))) + out[16]
	out[18] = T(((-(((in[9] >> 0) & 0xFFFF) & 1)) ^ (((in[9] >> 0) & 0xFFFF) >> 1))) + out[17]
	out[19] = T(((-((in[9] >> 16) & 1)) ^ ((in[9] >> 16) >> 1))) + out[18]
	out[20] = T(((-(((in[10] >> 0) & 0xFFFF) & 1)) ^ (((in[10] >> 0) & 0xFFFF) >> 1))) + out[19]
	out[21] = T(((-((in[10] >> 16) & 1)) ^ ((in[10] >> 16) >> 1))) + out[20]
	out[22] = T(((-(((in[11] >> 0) & 0xFFFF) & 1)) ^ (((in[11] >> 0) & 0xFFFF) >> 1))) + out[21]
	out[23] = T(((-((in[11] >> 16) & 1)) ^ ((in[11] >> 16) >> 1))) + out[22]
	out[24] = T(((-(((in[12] >> 0) & 0xFFFF) & 1)) ^ (((in[12] >> 0) & 0xFFFF) >> 1))) + out[23]
	out[25] = T(((-((in[12] >> 16) & 1)) ^ ((in[12] >> 16) >> 1))) + out[24]
	out[26] = T(((-(((in[13] >> 0) & 0xFFFF) & 1)) ^ (((in[13] >> 0) & 0xFFFF) >> 1))) + out[25]
	out[27] = T(((-((in[13] >> 16) & 1)) ^ ((in[13] >> 16) >> 1))) + out[26]
	out[28] = T(((-(((in[14] >> 0) & 0xFFFF) & 1)) ^ (((in[14] >> 0) & 0xFFFF) >> 1))) + out[27]
	out[29] = T(((-((in[14] >> 16) & 1)) ^ ((in[14] >> 16) >> 1))) + out[28]
	out[30] = T(((-(((in[15] >> 0) & 0xFFFF) & 1)) ^ (((in[15] >> 0) & 0xFFFF) >> 1))) + out[29]
	out[31] = T(((-((in[15] >> 16) & 1)) ^ ((in[15] >> 16) >> 1))) + out[30]
}

func deltaunpackzigzag32_17[T uint32 | int32](initoffset T, in *[17]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x1FFFF) & 1)) ^ (((in[0] >> 0) & 0x1FFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 17) | ((in[1] & 0x3) << 15)) & 1)) ^ (((in[0] >> 17) | ((in[1] & 0x3) << 15)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 2) & 0x1FFFF) & 1)) ^ (((in[1] >> 2) & 0x1FFFF) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 19) | ((in[2] & 0xF) << 13)) & 1)) ^ (((in[1] >> 19) | ((in[2] & 0xF) << 13)) >> 1))) + out[2]
	out[4] = T(((-(((in[2] >> 4) & 0x1FFFF) & 1)) ^ (((in[2] >> 4) & 0x1FFFF) >> 1))) + out[3]
	out[5] = T(((-(((in[2] >> 21) | ((in[3] & 0x3F) << 11)) & 1)) ^ (((in[2] >> 21) | ((in[3] & 0x3F) << 11)) >> 1))) + out[4]
	out[6] = T(((-(((in[3] >> 6) & 0x1FFFF) & 1)) ^ (((in[3] >> 6) & 0x1FFFF) >> 1))) + out[5]
	out[7] = T(((-(((in[3] >> 23) | ((in[4] & 0xFF) << 9)) & 1)) ^ (((in[3] >> 23) | ((in[4] & 0xFF) << 9)) >> 1))) + out[6]
	out[8] = T(((-(((in[4] >> 8) & 0x1FFFF) & 1)) ^ (((in[4] >> 8) & 0x1FFFF) >> 1))) + out[7]
	out[9] = T(((-(((in[4] >> 25) | ((in[5] & 0x3FF) << 7)) & 1)) ^ (((in[4] >> 25) | ((in[5] & 0x3FF) << 7)) >> 1))) + out[8]
	out[10] = T(((-(((in[5] >> 10) & 0x1FFFF) & 1)) ^ (((in[5] >> 10) & 0x1FFFF) >> 1))) + out[9]
	out[11] = T(((-(((in[5] >> 27) | ((in[6] & 0xFFF) << 5)) & 1)) ^ (((in[5] >> 27) | ((in[6] & 0xFFF) << 5)) >> 1))) + out[10]
	out[12] = T(((-(((in[6] >> 12) & 0x1FFFF) & 1)) ^ (((in[6] >> 12) & 0x1FFFF) >> 1))) + out[11]
	out[13] = T(((-(((in[6] >> 29) | ((in[7] & 0x3FFF) << 3)) & 1)) ^ (((in[6] >> 29) | ((in[7] & 0x3FFF) << 3)) >> 1))) + out[12]
	out[14] = T(((-(((in[7] >> 14) & 0x1FFFF) & 1)) ^ (((in[7] >> 14) & 0x1FFFF) >> 1))) + out[13]
	out[15] = T(((-(((in[7] >> 31) | ((in[8] & 0xFFFF) << 1)) & 1)) ^ (((in[7] >> 31) | ((in[8] & 0xFFFF) << 1)) >> 1))) + out[14]
	out[16] = T(((-(((in[8] >> 16) | ((in[9] & 0x1) << 16)) & 1)) ^ (((in[8] >> 16) | ((in[9] & 0x1) << 16)) >> 1))) + out[15]
	out[17] = T(((-(((in[9] >> 1) & 0x1FFFF) & 1)) ^ (((in[9] >> 1) & 0x1FFFF) >> 1))) + out[16]
	out[18] = T(((-(((in[9] >> 18) | ((in[10] & 0x7) << 14)) & 1)) ^ (((in[9] >> 18) | ((in[10] & 0x7) << 14)) >> 1))) + out[17]
	out[19] = T(((-(((in[10] >> 3) & 0x1FFFF) & 1)) ^ (((in[10] >> 3) & 0x1FFFF) >> 1))) + out[18]
	out[20] = T(((-(((in[10] >> 20) | ((in[11] & 0x1F) << 12)) & 1)) ^ (((in[10] >> 20) | ((in[11] & 0x1F) << 12)) >> 1))) + out[19]
	out[21] = T(((-(((in[11] >> 5) & 0x1FFFF) & 1)) ^ (((in[11] >> 5) & 0x1FFFF) >> 1))) + out[20]
	out[22] = T(((-(((in[11] >> 22) | ((in[12] & 0x7F) << 10)) & 1)) ^ (((in[11] >> 22) | ((in[12] & 0x7F) << 10)) >> 1))) + out[21]
	out[23] = T(((-(((in[12] >> 7) & 0x1FFFF) & 1)) ^ (((in[12] >> 7) & 0x1FFFF) >> 1))) + out[22]
	out[24] = T(((-(((in[12] >> 24) | ((in[13] & 0x1FF) << 8)) & 1)) ^ (((in[12] >> 24) | ((in[13] & 0x1FF) << 8)) >> 1))) + out[23]
	out[25] = T(((-(((in[13] >> 9) & 0x1FFFF) & 1)) ^ (((in[13] >> 9) & 0x1FFFF) >> 1))) + out[24]
	out[26] = T(((-(((in[13] >> 26) | ((in[14] & 0x7FF) << 6)) & 1)) ^ (((in[13] >> 26) | ((in[14] & 0x7FF) << 6)) >> 1))) + out[25]
	out[27] = T(((-(((in[14] >> 11) & 0x1FFFF) & 1)) ^ (((in[14] >> 11) & 0x1FFFF) >> 1))) + out[26]
	out[28] = T(((-(((in[14] >> 28) | ((in[15] & 0x1FFF) << 4)) & 1)) ^ (((in[14] >> 28) | ((in[15] & 0x1FFF) << 4)) >> 1))) + out[27]
	out[29] = T(((-(((in[15] >> 13) & 0x1FFFF) & 1)) ^ (((in[15] >> 13) & 0x1FFFF) >> 1))) + out[28]
	out[30] = T(((-(((in[15] >> 30) | ((in[16] & 0x7FFF) << 2)) & 1)) ^ (((in[15] >> 30) | ((in[16] & 0x7FFF) << 2)) >> 1))) + out[29]
	out[31] = T(((-((in[16] >> 15) & 1)) ^ ((in[16] >> 15) >> 1))) + out[30]
}

func deltaunpackzigzag32_18[T uint32 | int32](initoffset T, in *[18]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x3FFFF) & 1)) ^ (((in[0] >> 0) & 0x3FFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 18) | ((in[1] & 0xF) << 14)) & 1)) ^ (((in[0] >> 18) | ((in[1] & 0xF) << 14)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 4) & 0x3FFFF) & 1)) ^ (((in[1] >> 4) & 0x3FFFF) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 22) | ((in[2] & 0xFF) << 10)) & 1)) ^ (((in[1] >> 22) | ((in[2] & 0xFF) << 10)) >> 1))) + out[2]
	out[4] = T(((-(((in[2] >> 8) & 0x3FFFF) & 1)) ^ (((in[2] >> 8) & 0x3FFFF) >> 1))) + out[3]
	out[5] = T(((-(((in[2] >> 26) | ((in[3] & 0xFFF) << 6)) & 1)) ^ (((in[2] >> 26) | ((in[3] & 0xFFF) << 6)) >> 1))) + out[4]
	out[6] = T(((-(((in[3] >> 12) & 0x3FFFF) & 1)) ^ (((in[3] >> 12) & 0x3FFFF) >> 1))) + out[5]
	out[7] = T(((-(((in[3] >> 30) | ((in[4] & 0xFFFF) << 2)) & 1)) ^ (((in[3] >> 30) | ((in[4] & 0xFFFF) << 2)) >> 1))) + out[6]
	out[8] = T(((-(((in[4] >> 16) | ((in[5] & 0x3) << 16)) & 1)) ^ (((in[4] >> 16) | ((in[5] & 0x3) << 16)) >> 1))) + out[7]
	out[9] = T(((-(((in[5] >> 2) & 0x3FFFF) & 1)) ^ (((in[5] >> 2) & 0x3FFFF) >> 1))) + out[8]
	out[10] = T(((-(((in[5] >> 20) | ((in[6] & 0x3F) << 12)) & 1)) ^ (((in[5] >> 20) | ((in[6] & 0x3F) << 12)) >> 1))) + out[9]
	out[11] = T(((-(((in[6] >> 6) & 0x3FFFF) & 1)) ^ (((in[6] >> 6) & 0x3FFFF) >> 1))) + out[10]
	out[12] = T(((-(((in[6] >> 24) | ((in[7] & 0x3FF) << 8)) & 1)) ^ (((in[6] >> 24) | ((in[7] & 0x3FF) << 8)) >> 1))) + out[11]
	out[13] = T(((-(((in[7] >> 10) & 0x3FFFF) & 1)) ^ (((in[7] >> 10) & 0x3FFFF) >> 1))) + out[12]
	out[14] = T(((-(((in[7] >> 28) | ((in[8] & 0x3FFF) << 4)) & 1)) ^ (((in[7] >> 28) | ((in[8] & 0x3FFF) << 4)) >> 1))) + out[13]
	out[15] = T(((-((in[8] >> 14) & 1)) ^ ((in[8] >> 14) >> 1))) + out[14]
	out[16] = T(((-(((in[9] >> 0) & 0x3FFFF) & 1)) ^ (((in[9] >> 0) & 0x3FFFF) >> 1))) + out[15]
	out[17] = T(((-(((in[9] >> 18) | ((in[10] & 0xF) << 14)) & 1)) ^ (((in[9] >> 18) | ((in[10] & 0xF) << 14)) >> 1))) + out[16]
	out[18] = T(((-(((in[10] >> 4) & 0x3FFFF) & 1)) ^ (((in[10] >> 4) & 0x3FFFF) >> 1))) + out[17]
	out[19] = T(((-(((in[10] >> 22) | ((in[11] & 0xFF) << 10)) & 1)) ^ (((in[10] >> 22) | ((in[11] & 0xFF) << 10)) >> 1))) + out[18]
	out[20] = T(((-(((in[11] >> 8) & 0x3FFFF) & 1)) ^ (((in[11] >> 8) & 0x3FFFF) >> 1))) + out[19]
	out[21] = T(((-(((in[11] >> 26) | ((in[12] & 0xFFF) << 6)) & 1)) ^ (((in[11] >> 26) | ((in[12] & 0xFFF) << 6)) >> 1))) + out[20]
	out[22] = T(((-(((in[12] >> 12) & 0x3FFFF) & 1)) ^ (((in[12] >> 12) & 0x3FFFF) >> 1))) + out[21]
	out[23] = T(((-(((in[12] >> 30) | ((in[13] & 0xFFFF) << 2)) & 1)) ^ (((in[12] >> 30) | ((in[13] & 0xFFFF) << 2)) >> 1))) + out[22]
	out[24] = T(((-(((in[13] >> 16) | ((in[14] & 0x3) << 16)) & 1)) ^ (((in[13] >> 16) | ((in[14] & 0x3) << 16)) >> 1))) + out[23]
	out[25] = T(((-(((in[14] >> 2) & 0x3FFFF) & 1)) ^ (((in[14] >> 2) & 0x3FFFF) >> 1))) + out[24]
	out[26] = T(((-(((in[14] >> 20) | ((in[15] & 0x3F) << 12)) & 1)) ^ (((in[14] >> 20) | ((in[15] & 0x3F) << 12)) >> 1))) + out[25]
	out[27] = T(((-(((in[15] >> 6) & 0x3FFFF) & 1)) ^ (((in[15] >> 6) & 0x3FFFF) >> 1))) + out[26]
	out[28] = T(((-(((in[15] >> 24) | ((in[16] & 0x3FF) << 8)) & 1)) ^ (((in[15] >> 24) | ((in[16] & 0x3FF) << 8)) >> 1))) + out[27]
	out[29] = T(((-(((in[16] >> 10) & 0x3FFFF) & 1)) ^ (((in[16] >> 10) & 0x3FFFF) >> 1))) + out[28]
	out[30] = T(((-(((in[16] >> 28) | ((in[17] & 0x3FFF) << 4)) & 1)) ^ (((in[16] >> 28) | ((in[17] & 0x3FFF) << 4)) >> 1))) + out[29]
	out[31] = T(((-((in[17] >> 14) & 1)) ^ ((in[17] >> 14) >> 1))) + out[30]
}

func deltaunpackzigzag32_19[T uint32 | int32](initoffset T, in *[19]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x7FFFF) & 1)) ^ (((in[0] >> 0) & 0x7FFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 19) | ((in[1] & 0x3F) << 13)) & 1)) ^ (((in[0] >> 19) | ((in[1] & 0x3F) << 13)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 6) & 0x7FFFF) & 1)) ^ (((in[1] >> 6) & 0x7FFFF) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 25) | ((in[2] & 0xFFF) << 7)) & 1)) ^ (((in[1] >> 25) | ((in[2] & 0xFFF) << 7)) >> 1))) + out[2]
	out[4] = T(((-(((in[2] >> 12) & 0x7FFFF) & 1)) ^ (((in[2] >> 12) & 0x7FFFF) >> 1))) + out[3]
	out[5] = T(((-(((in[2] >> 31) | ((in[3] & 0x3FFFF) << 1)) & 1)) ^ (((in[2] >> 31) | ((in[3] & 0x3FFFF) << 1)) >> 1))) + out[4]
	out[6] = T(((-(((in[3] >> 18) | ((in[4] & 0x1F) << 14)) & 1)) ^ (((in[3] >> 18) | ((in[4] & 0x1F) << 14)) >> 1))) + out[5]
	out[7] = T(((-(((in[4] >> 5) & 0x7FFFF) & 1)) ^ (((in[4] >> 5) & 0x7FFFF) >> 1))) + out[6]
	out[8] = T(((-(((in[4] >> 24) | ((in[5] & 0x7FF) << 8)) & 1)) ^ (((in[4] >> 24) | ((in[5] & 0x7FF) << 8)) >> 1))) + out[7]
	out[9] = T(((-(((in[5] >> 11) & 0x7FFFF) & 1)) ^ (((in[5] >> 11) & 0x7FFFF) >> 1))) + out[8]
	out[10] = T(((-(((in[5] >> 30) | ((in[6] & 0x1FFFF) << 2)) & 1)) ^ (((in[5] >> 30) | ((in[6] & 0x1FFFF) << 2)) >> 1))) + out[9]
	out[11] = T(((-(((in[6] >> 17) | ((in[7] & 0xF) << 15)) & 1)) ^ (((in[6] >> 17) | ((in[7] & 0xF) << 15)) >> 1))) + out[10]
	out[12] = T(((-(((in[7] >> 4) & 0x7FFFF) & 1)) ^ (((in[7] >> 4) & 0x7FFFF) >> 1))) + out[11]
	out[13] = T(((-(((in[7] >> 23) | ((in[8] & 0x3FF) << 9)) & 1)) ^ (((in[7] >> 23) | ((in[8] & 0x3FF) << 9)) >> 1))) + out[12]
	out[14] = T(((-(((in[8] >> 10) & 0x7FFFF) & 1)) ^ (((in[8] >> 10) & 0x7FFFF) >> 1))) + out[13]
	out[15] = T(((-(((in[8] >> 29) | ((in[9] & 0xFFFF) << 3)) & 1)) ^ (((in[8] >> 29) | ((in[9] & 0xFFFF) << 3)) >> 1))) + out[14]
	out[16] = T(((-(((in[9] >> 16) | ((in[10] & 0x7) << 16)) & 1)) ^ (((in[9] >> 16) | ((in[10] & 0x7) << 16)) >> 1))) + out[15]
	out[17] = T(((-(((in[10] >> 3) & 0x7FFFF) & 1)) ^ (((in[10] >> 3) & 0x7FFFF) >> 1))) + out[16]
	out[18] = T(((-(((in[10] >> 22) | ((in[11] & 0x1FF) << 10)) & 1)) ^ (((in[10] >> 22) | ((in[11] & 0x1FF) << 10)) >> 1))) + out[17]
	out[19] = T(((-(((in[11] >> 9) & 0x7FFFF) & 1)) ^ (((in[11] >> 9) & 0x7FFFF) >> 1))) + out[18]
	out[20] = T(((-(((in[11] >> 28) | ((in[12] & 0x7FFF) << 4)) & 1)) ^ (((in[11] >> 28) | ((in[12] & 0x7FFF) << 4)) >> 1))) + out[19]
	out[21] = T(((-(((in[12] >> 15) | ((in[13] & 0x3) << 17)) & 1)) ^ (((in[12] >> 15) | ((in[13] & 0x3) << 17)) >> 1))) + out[20]
	out[22] = T(((-(((in[13] >> 2) & 0x7FFFF) & 1)) ^ (((in[13] >> 2) & 0x7FFFF) >> 1))) + out[21]
	out[23] = T(((-(((in[13] >> 21) | ((in[14] & 0xFF) << 11)) & 1)) ^ (((in[13] >> 21) | ((in[14] & 0xFF) << 11)) >> 1))) + out[22]
	out[24] = T(((-(((in[14] >> 8) & 0x7FFFF) & 1)) ^ (((in[14] >> 8) & 0x7FFFF) >> 1))) + out[23]
	out[25] = T(((-(((in[14] >> 27) | ((in[15] & 0x3FFF) << 5)) & 1)) ^ (((in[14] >> 27) | ((in[15] & 0x3FFF) << 5)) >> 1))) + out[24]
	out[26] = T(((-(((in[15] >> 14) | ((in[16] & 0x1) << 18)) & 1)) ^ (((in[15] >> 14) | ((in[16] & 0x1) << 18)) >> 1))) + out[25]
	out[27] = T(((-(((in[16] >> 1) & 0x7FFFF) & 1)) ^ (((in[16] >> 1) & 0x7FFFF) >> 1))) + out[26]
	out[28] = T(((-(((in[16] >> 20) | ((in[17] & 0x7F) << 12)) & 1)) ^ (((in[16] >> 20) | ((in[17] & 0x7F) << 12)) >> 1))) + out[27]
	out[29] = T(((-(((in[17] >> 7) & 0x7FFFF) & 1)) ^ (((in[17] >> 7) & 0x7FFFF) >> 1))) + out[28]
	out[30] = T(((-(((in[17] >> 26) | ((in[18] & 0x1FFF) << 6)) & 1)) ^ (((in[17] >> 26) | ((in[18] & 0x1FFF) << 6)) >> 1))) + out[29]
	out[31] = T(((-((in[18] >> 13) & 1)) ^ ((in[18] >> 13) >> 1))) + out[30]
}

func deltaunpackzigzag32_20[T uint32 | int32](initoffset T, in *[20]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0xFFFFF) & 1)) ^ (((in[0] >> 0) & 0xFFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 20) | ((in[1] & 0xFF) << 12)) & 1)) ^ (((in[0] >> 20) | ((in[1] & 0xFF) << 12)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 8) & 0xFFFFF) & 1)) ^ (((in[1] >> 8) & 0xFFFFF) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 28) | ((in[2] & 0xFFFF) << 4)) & 1)) ^ (((in[1] >> 28) | ((in[2] & 0xFFFF) << 4)) >> 1))) + out[2]
	out[4] = T(((-(((in[2] >> 16) | ((in[3] & 0xF) << 16)) & 1)) ^ (((in[2] >> 16) | ((in[3] & 0xF) << 16)) >> 1))) + out[3]
	out[5] = T(((-(((in[3] >> 4) & 0xFFFFF) & 1)) ^ (((in[3] >> 4) & 0xFFFFF) >> 1))) + out[4]
	out[6] = T(((-(((in[3] >> 24) | ((in[4] & 0xFFF) << 8)) & 1)) ^ (((in[3] >> 24) | ((in[4] & 0xFFF) << 8)) >> 1))) + out[5]
	out[7] = T(((-((in[4] >> 12) & 1)) ^ ((in[4] >> 12) >> 1))) + out[6]
	out[8] = T(((-(((in[5] >> 0) & 0xFFFFF) & 1)) ^ (((in[5] >> 0) & 0xFFFFF) >> 1))) + out[7]
	out[9] = T(((-(((in[5] >> 20) | ((in[6] & 0xFF) << 12)) & 1)) ^ (((in[5] >> 20) | ((in[6] & 0xFF) << 12)) >> 1))) + out[8]
	out[10] = T(((-(((in[6] >> 8) & 0xFFFFF) & 1)) ^ (((in[6] >> 8) & 0xFFFFF) >> 1))) + out[9]
	out[11] = T(((-(((in[6] >> 28) | ((in[7] & 0xFFFF) << 4)) & 1)) ^ (((in[6] >> 28) | ((in[7] & 0xFFFF) << 4)) >> 1))) + out[10]
	out[12] = T(((-(((in[7] >> 16) | ((in[8] & 0xF) << 16)) & 1)) ^ (((in[7] >> 16) | ((in[8] & 0xF) << 16)) >> 1))) + out[11]
	out[13] = T(((-(((in[8] >> 4) & 0xFFFFF) & 1)) ^ (((in[8] >> 4) & 0xFFFFF) >> 1))) + out[12]
	out[14] = T(((-(((in[8] >> 24) | ((in[9] & 0xFFF) << 8)) & 1)) ^ (((in[8] >> 24) | ((in[9] & 0xFFF) << 8)) >> 1))) + out[13]
	out[15] = T(((-((in[9] >> 12) & 1)) ^ ((in[9] >> 12) >> 1))) + out[14]
	out[16] = T(((-(((in[10] >> 0) & 0xFFFFF) & 1)) ^ (((in[10] >> 0) & 0xFFFFF) >> 1))) + out[15]
	out[17] = T(((-(((in[10] >> 20) | ((in[11] & 0xFF) << 12)) & 1)) ^ (((in[10] >> 20) | ((in[11] & 0xFF) << 12)) >> 1))) + out[16]
	out[18] = T(((-(((in[11] >> 8) & 0xFFFFF) & 1)) ^ (((in[11] >> 8) & 0xFFFFF) >> 1))) + out[17]
	out[19] = T(((-(((in[11] >> 28) | ((in[12] & 0xFFFF) << 4)) & 1)) ^ (((in[11] >> 28) | ((in[12] & 0xFFFF) << 4)) >> 1))) + out[18]
	out[20] = T(((-(((in[12] >> 16) | ((in[13] & 0xF) << 16)) & 1)) ^ (((in[12] >> 16) | ((in[13] & 0xF) << 16)) >> 1))) + out[19]
	out[21] = T(((-(((in[13] >> 4) & 0xFFFFF) & 1)) ^ (((in[13] >> 4) & 0xFFFFF) >> 1))) + out[20]
	out[22] = T(((-(((in[13] >> 24) | ((in[14] & 0xFFF) << 8)) & 1)) ^ (((in[13] >> 24) | ((in[14] & 0xFFF) << 8)) >> 1))) + out[21]
	out[23] = T(((-((in[14] >> 12) & 1)) ^ ((in[14] >> 12) >> 1))) + out[22]
	out[24] = T(((-(((in[15] >> 0) & 0xFFFFF) & 1)) ^ (((in[15] >> 0) & 0xFFFFF) >> 1))) + out[23]
	out[25] = T(((-(((in[15] >> 20) | ((in[16] & 0xFF) << 12)) & 1)) ^ (((in[15] >> 20) | ((in[16] & 0xFF) << 12)) >> 1))) + out[24]
	out[26] = T(((-(((in[16] >> 8) & 0xFFFFF) & 1)) ^ (((in[16] >> 8) & 0xFFFFF) >> 1))) + out[25]
	out[27] = T(((-(((in[16] >> 28) | ((in[17] & 0xFFFF) << 4)) & 1)) ^ (((in[16] >> 28) | ((in[17] & 0xFFFF) << 4)) >> 1))) + out[26]
	out[28] = T(((-(((in[17] >> 16) | ((in[18] & 0xF) << 16)) & 1)) ^ (((in[17] >> 16) | ((in[18] & 0xF) << 16)) >> 1))) + out[27]
	out[29] = T(((-(((in[18] >> 4) & 0xFFFFF) & 1)) ^ (((in[18] >> 4) & 0xFFFFF) >> 1))) + out[28]
	out[30] = T(((-(((in[18] >> 24) | ((in[19] & 0xFFF) << 8)) & 1)) ^ (((in[18] >> 24) | ((in[19] & 0xFFF) << 8)) >> 1))) + out[29]
	out[31] = T(((-((in[19] >> 12) & 1)) ^ ((in[19] >> 12) >> 1))) + out[30]
}

func deltaunpackzigzag32_21[T uint32 | int32](initoffset T, in *[21]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x1FFFFF) & 1)) ^ (((in[0] >> 0) & 0x1FFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 21) | ((in[1] & 0x3FF) << 11)) & 1)) ^ (((in[0] >> 21) | ((in[1] & 0x3FF) << 11)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 10) & 0x1FFFFF) & 1)) ^ (((in[1] >> 10) & 0x1FFFFF) >> 1))) + out[1]
	out[3] = T(((-(((in[1] >> 31) | ((in[2] & 0xFFFFF) << 1)) & 1)) ^ (((in[1] >> 31) | ((in[2] & 0xFFFFF) << 1)) >> 1))) + out[2]
	out[4] = T(((-(((in[2] >> 20) | ((in[3] & 0x1FF) << 12)) & 1)) ^ (((in[2] >> 20) | ((in[3] & 0x1FF) << 12)) >> 1))) + out[3]
	out[5] = T(((-(((in[3] >> 9) & 0x1FFFFF) & 1)) ^ (((in[3] >> 9) & 0x1FFFFF) >> 1))) + out[4]
	out[6] = T(((-(((in[3] >> 30) | ((in[4] & 0x7FFFF) << 2)) & 1)) ^ (((in[3] >> 30) | ((in[4] & 0x7FFFF) << 2)) >> 1))) + out[5]
	out[7] = T(((-(((in[4] >> 19) | ((in[5] & 0xFF) << 13)) & 1)) ^ (((in[4] >> 19) | ((in[5] & 0xFF) << 13)) >> 1))) + out[6]
	out[8] = T(((-(((in[5] >> 8) & 0x1FFFFF) & 1)) ^ (((in[5] >> 8) & 0x1FFFFF) >> 1))) + out[7]
	out[9] = T(((-(((in[5] >> 29) | ((in[6] & 0x3FFFF) << 3)) & 1)) ^ (((in[5] >> 29) | ((in[6] & 0x3FFFF) << 3)) >> 1))) + out[8]
	out[10] = T(((-(((in[6] >> 18) | ((in[7] & 0x7F) << 14)) & 1)) ^ (((in[6] >> 18) | ((in[7] & 0x7F) << 14)) >> 1))) + out[9]
	out[11] = T(((-(((in[7] >> 7) & 0x1FFFFF) & 1)) ^ (((in[7] >> 7) & 0x1FFFFF) >> 1))) + out[10]
	out[12] = T(((-(((in[7] >> 28) | ((in[8] & 0x1FFFF) << 4)) & 1)) ^ (((in[7] >> 28) | ((in[8] & 0x1FFFF) << 4)) >> 1))) + out[11]
	out[13] = T(((-(((in[8] >> 17) | ((in[9] & 0x3F) << 15)) & 1)) ^ (((in[8] >> 17) | ((in[9] & 0x3F) << 15)) >> 1))) + out[12]
	out[14] = T(((-(((in[9] >> 6) & 0x1FFFFF) & 1)) ^ (((in[9] >> 6) & 0x1FFFFF) >> 1))) + out[13]
	out[15] = T(((-(((in[9] >> 27) | ((in[10] & 0xFFFF) << 5)) & 1)) ^ (((in[9] >> 27) | ((in[10] & 0xFFFF) << 5)) >> 1))) + out[14]
	out[16] = T(((-(((in[10] >> 16) | ((in[11] & 0x1F) << 16)) & 1)) ^ (((in[10] >> 16) | ((in[11] & 0x1F) << 16)) >> 1))) + out[15]
	out[17] = T(((-(((in[11] >> 5) & 0x1FFFFF) & 1)) ^ (((in[11] >> 5) & 0x1FFFFF) >> 1))) + out[16]
	out[18] = T(((-(((in[11] >> 26) | ((in[12] & 0x7FFF) << 6)) & 1)) ^ (((in[11] >> 26) | ((in[12] & 0x7FFF) << 6)) >> 1))) + out[17]
	out[19] = T(((-(((in[12] >> 15) | ((in[13] & 0xF) << 17)) & 1)) ^ (((in[12] >> 15) | ((in[13] & 0xF) << 17)) >> 1))) + out[18]
	out[20] = T(((-(((in[13] >> 4) & 0x1FFFFF) & 1)) ^ (((in[13] >> 4) & 0x1FFFFF) >> 1))) + out[19]
	out[21] = T(((-(((in[13] >> 25) | ((in[14] & 0x3FFF) << 7)) & 1)) ^ (((in[13] >> 25) | ((in[14] & 0x3FFF) << 7)) >> 1))) + out[20]
	out[22] = T(((-(((in[14] >> 14) | ((in[15] & 0x7) << 18)) & 1)) ^ (((in[14] >> 14) | ((in[15] & 0x7) << 18)) >> 1))) + out[21]
	out[23] = T(((-(((in[15] >> 3) & 0x1FFFFF) & 1)) ^ (((in[15] >> 3) & 0x1FFFFF) >> 1))) + out[22]
	out[24] = T(((-(((in[15] >> 24) | ((in[16] & 0x1FFF) << 8)) & 1)) ^ (((in[15] >> 24) | ((in[16] & 0x1FFF) << 8)) >> 1))) + out[23]
	out[25] = T(((-(((in[16] >> 13) | ((in[17] & 0x3) << 19)) & 1)) ^ (((in[16] >> 13) | ((in[17] & 0x3) << 19)) >> 1))) + out[24]
	out[26] = T(((-(((in[17] >> 2) & 0x1FFFFF) & 1)) ^ (((in[17] >> 2) & 0x1FFFFF) >> 1))) + out[25]
	out[27] = T(((-(((in[17] >> 23) | ((in[18] & 0xFFF) << 9)) & 1)) ^ (((in[17] >> 23) | ((in[18] & 0xFFF) << 9)) >> 1))) + out[26]
	out[28] = T(((-(((in[18] >> 12) | ((in[19] & 0x1) << 20)) & 1)) ^ (((in[18] >> 12) | ((in[19] & 0x1) << 20)) >> 1))) + out[27]
	out[29] = T(((-(((in[19] >> 1) & 0x1FFFFF) & 1)) ^ (((in[19] >> 1) & 0x1FFFFF) >> 1))) + out[28]
	out[30] = T(((-(((in[19] >> 22) | ((in[20] & 0x7FF) << 10)) & 1)) ^ (((in[19] >> 22) | ((in[20] & 0x7FF) << 10)) >> 1))) + out[29]
	out[31] = T(((-((in[20] >> 11) & 1)) ^ ((in[20] >> 11) >> 1))) + out[30]
}

func deltaunpackzigzag32_22[T uint32 | int32](initoffset T, in *[22]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x3FFFFF) & 1)) ^ (((in[0] >> 0) & 0x3FFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 22) | ((in[1] & 0xFFF) << 10)) & 1)) ^ (((in[0] >> 22) | ((in[1] & 0xFFF) << 10)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 12) | ((in[2] & 0x3) << 20)) & 1)) ^ (((in[1] >> 12) | ((in[2] & 0x3) << 20)) >> 1))) + out[1]
	out[3] = T(((-(((in[2] >> 2) & 0x3FFFFF) & 1)) ^ (((in[2] >> 2) & 0x3FFFFF) >> 1))) + out[2]
	out[4] = T(((-(((in[2] >> 24) | ((in[3] & 0x3FFF) << 8)) & 1)) ^ (((in[2] >> 24) | ((in[3] & 0x3FFF) << 8)) >> 1))) + out[3]
	out[5] = T(((-(((in[3] >> 14) | ((in[4] & 0xF) << 18)) & 1)) ^ (((in[3] >> 14) | ((in[4] & 0xF) << 18)) >> 1))) + out[4]
	out[6] = T(((-(((in[4] >> 4) & 0x3FFFFF) & 1)) ^ (((in[4] >> 4) & 0x3FFFFF) >> 1))) + out[5]
	out[7] = T(((-(((in[4] >> 26) | ((in[5] & 0xFFFF) << 6)) & 1)) ^ (((in[4] >> 26) | ((in[5] & 0xFFFF) << 6)) >> 1))) + out[6]
	out[8] = T(((-(((in[5] >> 16) | ((in[6] & 0x3F) << 16)) & 1)) ^ (((in[5] >> 16) | ((in[6] & 0x3F) << 16)) >> 1))) + out[7]
	out[9] = T(((-(((in[6] >> 6) & 0x3FFFFF) & 1)) ^ (((in[6] >> 6) & 0x3FFFFF) >> 1))) + out[8]
	out[10] = T(((-(((in[6] >> 28) | ((in[7] & 0x3FFFF) << 4)) & 1)) ^ (((in[6] >> 28) | ((in[7] & 0x3FFFF) << 4)) >> 1))) + out[9]
	out[11] = T(((-(((in[7] >> 18) | ((in[8] & 0xFF) << 14)) & 1)) ^ (((in[7] >> 18) | ((in[8] & 0xFF) << 14)) >> 1))) + out[10]
	out[12] = T(((-(((in[8] >> 8) & 0x3FFFFF) & 1)) ^ (((in[8] >> 8) & 0x3FFFFF) >> 1))) + out[11]
	out[13] = T(((-(((in[8] >> 30) | ((in[9] & 0xFFFFF) << 2)) & 1)) ^ (((in[8] >> 30) | ((in[9] & 0xFFFFF) << 2)) >> 1))) + out[12]
	out[14] = T(((-(((in[9] >> 20) | ((in[10] & 0x3FF) << 12)) & 1)) ^ (((in[9] >> 20) | ((in[10] & 0x3FF) << 12)) >> 1))) + out[13]
	out[15] = T(((-((in[10] >> 10) & 1)) ^ ((in[10] >> 10) >> 1))) + out[14]
	out[16] = T(((-(((in[11] >> 0) & 0x3FFFFF) & 1)) ^ (((in[11] >> 0) & 0x3FFFFF) >> 1))) + out[15]
	out[17] = T(((-(((in[11] >> 22) | ((in[12] & 0xFFF) << 10)) & 1)) ^ (((in[11] >> 22) | ((in[12] & 0xFFF) << 10)) >> 1))) + out[16]
	out[18] = T(((-(((in[12] >> 12) | ((in[13] & 0x3) << 20)) & 1)) ^ (((in[12] >> 12) | ((in[13] & 0x3) << 20)) >> 1))) + out[17]
	out[19] = T(((-(((in[13] >> 2) & 0x3FFFFF) & 1)) ^ (((in[13] >> 2) & 0x3FFFFF) >> 1))) + out[18]
	out[20] = T(((-(((in[13] >> 24) | ((in[14] & 0x3FFF) << 8)) & 1)) ^ (((in[13] >> 24) | ((in[14] & 0x3FFF) << 8)) >> 1))) + out[19]
	out[21] = T(((-(((in[14] >> 14) | ((in[15] & 0xF) << 18)) & 1)) ^ (((in[14] >> 14) | ((in[15] & 0xF) << 18)) >> 1))) + out[20]
	out[22] = T(((-(((in[15] >> 4) & 0x3FFFFF) & 1)) ^ (((in[15] >> 4) & 0x3FFFFF) >> 1))) + out[21]
	out[23] = T(((-(((in[15] >> 26) | ((in[16] & 0xFFFF) << 6)) & 1)) ^ (((in[15] >> 26) | ((in[16] & 0xFFFF) << 6)) >> 1))) + out[22]
	out[24] = T(((-(((in[16] >> 16) | ((in[17] & 0x3F) << 16)) & 1)) ^ (((in[16] >> 16) | ((in[17] & 0x3F) << 16)) >> 1))) + out[23]
	out[25] = T(((-(((in[17] >> 6) & 0x3FFFFF) & 1)) ^ (((in[17] >> 6) & 0x3FFFFF) >> 1))) + out[24]
	out[26] = T(((-(((in[17] >> 28) | ((in[18] & 0x3FFFF) << 4)) & 1)) ^ (((in[17] >> 28) | ((in[18] & 0x3FFFF) << 4)) >> 1))) + out[25]
	out[27] = T(((-(((in[18] >> 18) | ((in[19] & 0xFF) << 14)) & 1)) ^ (((in[18] >> 18) | ((in[19] & 0xFF) << 14)) >> 1))) + out[26]
	out[28] = T(((-(((in[19] >> 8) & 0x3FFFFF) & 1)) ^ (((in[19] >> 8) & 0x3FFFFF) >> 1))) + out[27]
	out[29] = T(((-(((in[19] >> 30) | ((in[20] & 0xFFFFF) << 2)) & 1)) ^ (((in[19] >> 30) | ((in[20] & 0xFFFFF) << 2)) >> 1))) + out[28]
	out[30] = T(((-(((in[20] >> 20) | ((in[21] & 0x3FF) << 12)) & 1)) ^ (((in[20] >> 20) | ((in[21] & 0x3FF) << 12)) >> 1))) + out[29]
	out[31] = T(((-((in[21] >> 10) & 1)) ^ ((in[21] >> 10) >> 1))) + out[30]
}

func deltaunpackzigzag32_23[T uint32 | int32](initoffset T, in *[23]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x7FFFFF) & 1)) ^ (((in[0] >> 0) & 0x7FFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 23) | ((in[1] & 0x3FFF) << 9)) & 1)) ^ (((in[0] >> 23) | ((in[1] & 0x3FFF) << 9)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 14) | ((in[2] & 0x1F) << 18)) & 1)) ^ (((in[1] >> 14) | ((in[2] & 0x1F) << 18)) >> 1))) + out[1]
	out[3] = T(((-(((in[2] >> 5) & 0x7FFFFF) & 1)) ^ (((in[2] >> 5) & 0x7FFFFF) >> 1))) + out[2]
	out[4] = T(((-(((in[2] >> 28) | ((in[3] & 0x7FFFF) << 4)) & 1)) ^ (((in[2] >> 28) | ((in[3] & 0x7FFFF) << 4)) >> 1))) + out[3]
	out[5] = T(((-(((in[3] >> 19) | ((in[4] & 0x3FF) << 13)) & 1)) ^ (((in[3] >> 19) | ((in[4] & 0x3FF) << 13)) >> 1))) + out[4]
	out[6] = T(((-(((in[4] >> 10) | ((in[5] & 0x1) << 22)) & 1)) ^ (((in[4] >> 10) | ((in[5] & 0x1) << 22)) >> 1))) + out[5]
	out[7] = T(((-(((in[5] >> 1) & 0x7FFFFF) & 1)) ^ (((in[5] >> 1) & 0x7FFFFF) >> 1))) + out[6]
	out[8] = T(((-(((in[5] >> 24) | ((in[6] & 0x7FFF) << 8)) & 1)) ^ (((in[5] >> 24) | ((in[6] & 0x7FFF) << 8)) >> 1))) + out[7]
	out[9] = T(((-(((in[6] >> 15) | ((in[7] & 0x3F) << 17)) & 1)) ^ (((in[6] >> 15) | ((in[7] & 0x3F) << 17)) >> 1))) + out[8]
	out[10] = T(((-(((in[7] >> 6) & 0x7FFFFF) & 1)) ^ (((in[7] >> 6) & 0x7FFFFF) >> 1))) + out[9]
	out[11] = T(((-(((in[7] >> 29) | ((in[8] & 0xFFFFF) << 3)) & 1)) ^ (((in[7] >> 29) | ((in[8] & 0xFFFFF) << 3)) >> 1))) + out[10]
	out[12] = T(((-(((in[8] >> 20) | ((in[9] & 0x7FF) << 12)) & 1)) ^ (((in[8] >> 20) | ((in[9] & 0x7FF) << 12)) >> 1))) + out[11]
	out[13] = T(((-(((in[9] >> 11) | ((in[10] & 0x3) << 21)) & 1)) ^ (((in[9] >> 11) | ((in[10] & 0x3) << 21)) >> 1))) + out[12]
	out[14] = T(((-(((in[10] >> 2) & 0x7FFFFF) & 1)) ^ (((in[10] >> 2) & 0x7FFFFF) >> 1))) + out[13]
	out[15] = T(((-(((in[10] >> 25) | ((in[11] & 0xFFFF) << 7)) & 1)) ^ (((in[10] >> 25) | ((in[11] & 0xFFFF) << 7)) >> 1))) + out[14]
	out[16] = T(((-(((in[11] >> 16) | ((in[12] & 0x7F) << 16)) & 1)) ^ (((in[11] >> 16) | ((in[12] & 0x7F) << 16)) >> 1))) + out[15]
	out[17] = T(((-(((in[12] >> 7) & 0x7FFFFF) & 1)) ^ (((in[12] >> 7) & 0x7FFFFF) >> 1))) + out[16]
	out[18] = T(((-(((in[12] >> 30) | ((in[13] & 0x1FFFFF) << 2)) & 1)) ^ (((in[12] >> 30) | ((in[13] & 0x1FFFFF) << 2)) >> 1))) + out[17]
	out[19] = T(((-(((in[13] >> 21) | ((in[14] & 0xFFF) << 11)) & 1)) ^ (((in[13] >> 21) | ((in[14] & 0xFFF) << 11)) >> 1))) + out[18]
	out[20] = T(((-(((in[14] >> 12) | ((in[15] & 0x7) << 20)) & 1)) ^ (((in[14] >> 12) | ((in[15] & 0x7) << 20)) >> 1))) + out[19]
	out[21] = T(((-(((in[15] >> 3) & 0x7FFFFF) & 1)) ^ (((in[15] >> 3) & 0x7FFFFF) >> 1))) + out[20]
	out[22] = T(((-(((in[15] >> 26) | ((in[16] & 0x1FFFF) << 6)) & 1)) ^ (((in[15] >> 26) | ((in[16] & 0x1FFFF) << 6)) >> 1))) + out[21]
	out[23] = T(((-(((in[16] >> 17) | ((in[17] & 0xFF) << 15)) & 1)) ^ (((in[16] >> 17) | ((in[17] & 0xFF) << 15)) >> 1))) + out[22]
	out[24] = T(((-(((in[17] >> 8) & 0x7FFFFF) & 1)) ^ (((in[17] >> 8) & 0x7FFFFF) >> 1))) + out[23]
	out[25] = T(((-(((in[17] >> 31) | ((in[18] & 0x3FFFFF) << 1)) & 1)) ^ (((in[17] >> 31) | ((in[18] & 0x3FFFFF) << 1)) >> 1))) + out[24]
	out[26] = T(((-(((in[18] >> 22) | ((in[19] & 0x1FFF) << 10)) & 1)) ^ (((in[18] >> 22) | ((in[19] & 0x1FFF) << 10)) >> 1))) + out[25]
	out[27] = T(((-(((in[19] >> 13) | ((in[20] & 0xF) << 19)) & 1)) ^ (((in[19] >> 13) | ((in[20] & 0xF) << 19)) >> 1))) + out[26]
	out[28] = T(((-(((in[20] >> 4) & 0x7FFFFF) & 1)) ^ (((in[20] >> 4) & 0x7FFFFF) >> 1))) + out[27]
	out[29] = T(((-(((in[20] >> 27) | ((in[21] & 0x3FFFF) << 5)) & 1)) ^ (((in[20] >> 27) | ((in[21] & 0x3FFFF) << 5)) >> 1))) + out[28]
	out[30] = T(((-(((in[21] >> 18) | ((in[22] & 0x1FF) << 14)) & 1)) ^ (((in[21] >> 18) | ((in[22] & 0x1FF) << 14)) >> 1))) + out[29]
	out[31] = T(((-((in[22] >> 9) & 1)) ^ ((in[22] >> 9) >> 1))) + out[30]
}

func deltaunpackzigzag32_24[T uint32 | int32](initoffset T, in *[24]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0xFFFFFF) & 1)) ^ (((in[0] >> 0) & 0xFFFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 24) | ((in[1] & 0xFFFF) << 8)) & 1)) ^ (((in[0] >> 24) | ((in[1] & 0xFFFF) << 8)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 16) | ((in[2] & 0xFF) << 16)) & 1)) ^ (((in[1] >> 16) | ((in[2] & 0xFF) << 16)) >> 1))) + out[1]
	out[3] = T(((-((in[2] >> 8) & 1)) ^ ((in[2] >> 8) >> 1))) + out[2]
	out[4] = T(((-(((in[3] >> 0) & 0xFFFFFF) & 1)) ^ (((in[3] >> 0) & 0xFFFFFF) >> 1))) + out[3]
	out[5] = T(((-(((in[3] >> 24) | ((in[4] & 0xFFFF) << 8)) & 1)) ^ (((in[3] >> 24) | ((in[4] & 0xFFFF) << 8)) >> 1))) + out[4]
	out[6] = T(((-(((in[4] >> 16) | ((in[5] & 0xFF) << 16)) & 1)) ^ (((in[4] >> 16) | ((in[5] & 0xFF) << 16)) >> 1))) + out[5]
	out[7] = T(((-((in[5] >> 8) & 1)) ^ ((in[5] >> 8) >> 1))) + out[6]
	out[8] = T(((-(((in[6] >> 0) & 0xFFFFFF) & 1)) ^ (((in[6] >> 0) & 0xFFFFFF) >> 1))) + out[7]
	out[9] = T(((-(((in[6] >> 24) | ((in[7] & 0xFFFF) << 8)) & 1)) ^ (((in[6] >> 24) | ((in[7] & 0xFFFF) << 8)) >> 1))) + out[8]
	out[10] = T(((-(((in[7] >> 16) | ((in[8] & 0xFF) << 16)) & 1)) ^ (((in[7] >> 16) | ((in[8] & 0xFF) << 16)) >> 1))) + out[9]
	out[11] = T(((-((in[8] >> 8) & 1)) ^ ((in[8] >> 8) >> 1))) + out[10]
	out[12] = T(((-(((in[9] >> 0) & 0xFFFFFF) & 1)) ^ (((in[9] >> 0) & 0xFFFFFF) >> 1))) + out[11]
	out[13] = T(((-(((in[9] >> 24) | ((in[10] & 0xFFFF) << 8)) & 1)) ^ (((in[9] >> 24) | ((in[10] & 0xFFFF) << 8)) >> 1))) + out[12]
	out[14] = T(((-(((in[10] >> 16) | ((in[11] & 0xFF) << 16)) & 1)) ^ (((in[10] >> 16) | ((in[11] & 0xFF) << 16)) >> 1))) + out[13]
	out[15] = T(((-((in[11] >> 8) & 1)) ^ ((in[11] >> 8) >> 1))) + out[14]
	out[16] = T(((-(((in[12] >> 0) & 0xFFFFFF) & 1)) ^ (((in[12] >> 0) & 0xFFFFFF) >> 1))) + out[15]
	out[17] = T(((-(((in[12] >> 24) | ((in[13] & 0xFFFF) << 8)) & 1)) ^ (((in[12] >> 24) | ((in[13] & 0xFFFF) << 8)) >> 1))) + out[16]
	out[18] = T(((-(((in[13] >> 16) | ((in[14] & 0xFF) << 16)) & 1)) ^ (((in[13] >> 16) | ((in[14] & 0xFF) << 16)) >> 1))) + out[17]
	out[19] = T(((-((in[14] >> 8) & 1)) ^ ((in[14] >> 8) >> 1))) + out[18]
	out[20] = T(((-(((in[15] >> 0) & 0xFFFFFF) & 1)) ^ (((in[15] >> 0) & 0xFFFFFF) >> 1))) + out[19]
	out[21] = T(((-(((in[15] >> 24) | ((in[16] & 0xFFFF) << 8)) & 1)) ^ (((in[15] >> 24) | ((in[16] & 0xFFFF) << 8)) >> 1))) + out[20]
	out[22] = T(((-(((in[16] >> 16) | ((in[17] & 0xFF) << 16)) & 1)) ^ (((in[16] >> 16) | ((in[17] & 0xFF) << 16)) >> 1))) + out[21]
	out[23] = T(((-((in[17] >> 8) & 1)) ^ ((in[17] >> 8) >> 1))) + out[22]
	out[24] = T(((-(((in[18] >> 0) & 0xFFFFFF) & 1)) ^ (((in[18] >> 0) & 0xFFFFFF) >> 1))) + out[23]
	out[25] = T(((-(((in[18] >> 24) | ((in[19] & 0xFFFF) << 8)) & 1)) ^ (((in[18] >> 24) | ((in[19] & 0xFFFF) << 8)) >> 1))) + out[24]
	out[26] = T(((-(((in[19] >> 16) | ((in[20] & 0xFF) << 16)) & 1)) ^ (((in[19] >> 16) | ((in[20] & 0xFF) << 16)) >> 1))) + out[25]
	out[27] = T(((-((in[20] >> 8) & 1)) ^ ((in[20] >> 8) >> 1))) + out[26]
	out[28] = T(((-(((in[21] >> 0) & 0xFFFFFF) & 1)) ^ (((in[21] >> 0) & 0xFFFFFF) >> 1))) + out[27]
	out[29] = T(((-(((in[21] >> 24) | ((in[22] & 0xFFFF) << 8)) & 1)) ^ (((in[21] >> 24) | ((in[22] & 0xFFFF) << 8)) >> 1))) + out[28]
	out[30] = T(((-(((in[22] >> 16) | ((in[23] & 0xFF) << 16)) & 1)) ^ (((in[22] >> 16) | ((in[23] & 0xFF) << 16)) >> 1))) + out[29]
	out[31] = T(((-((in[23] >> 8) & 1)) ^ ((in[23] >> 8) >> 1))) + out[30]
}

func deltaunpackzigzag32_25[T uint32 | int32](initoffset T, in *[25]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x1FFFFFF) & 1)) ^ (((in[0] >> 0) & 0x1FFFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 25) | ((in[1] & 0x3FFFF) << 7)) & 1)) ^ (((in[0] >> 25) | ((in[1] & 0x3FFFF) << 7)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 18) | ((in[2] & 0x7FF) << 14)) & 1)) ^ (((in[1] >> 18) | ((in[2] & 0x7FF) << 14)) >> 1))) + out[1]
	out[3] = T(((-(((in[2] >> 11) | ((in[3] & 0xF) << 21)) & 1)) ^ (((in[2] >> 11) | ((in[3] & 0xF) << 21)) >> 1))) + out[2]
	out[4] = T(((-(((in[3] >> 4) & 0x1FFFFFF) & 1)) ^ (((in[3] >> 4) & 0x1FFFFFF) >> 1))) + out[3]
	out[5] = T(((-(((in[3] >> 29) | ((in[4] & 0x3FFFFF) << 3)) & 1)) ^ (((in[3] >> 29) | ((in[4] & 0x3FFFFF) << 3)) >> 1))) + out[4]
	out[6] = T(((-(((in[4] >> 22) | ((in[5] & 0x7FFF) << 10)) & 1)) ^ (((in[4] >> 22) | ((in[5] & 0x7FFF) << 10)) >> 1))) + out[5]
	out[7] = T(((-(((in[5] >> 15) | ((in[6] & 0xFF) << 17)) & 1)) ^ (((in[5] >> 15) | ((in[6] & 0xFF) << 17)) >> 1))) + out[6]
	out[8] = T(((-(((in[6] >> 8) | ((in[7] & 0x1) << 24)) & 1)) ^ (((in[6] >> 8) | ((in[7] & 0x1) << 24)) >> 1))) + out[7]
	out[9] = T(((-(((in[7] >> 1) & 0x1FFFFFF) & 1)) ^ (((in[7] >> 1) & 0x1FFFFFF) >> 1))) + out[8]
	out[10] = T(((-(((in[7] >> 26) | ((in[8] & 0x7FFFF) << 6)) & 1)) ^ (((in[7] >> 26) | ((in[8] & 0x7FFFF) << 6)) >> 1))) + out[9]
	out[11] = T(((-(((in[8] >> 19) | ((in[9] & 0xFFF) << 13)) & 1)) ^ (((in[8] >> 19) | ((in[9] & 0xFFF) << 13)) >> 1))) + out[10]
	out[12] = T(((-(((in[9] >> 12) | ((in[10] & 0x1F) << 20)) & 1)) ^ (((in[9] >> 12) | ((in[10] & 0x1F) << 20)) >> 1))) + out[11]
	out[13] = T(((-(((in[10] >> 5) & 0x1FFFFFF) & 1)) ^ (((in[10] >> 5) & 0x1FFFFFF) >> 1))) + out[12]
	out[14] = T(((-(((in[10] >> 30) | ((in[11] & 0x7FFFFF) << 2)) & 1)) ^ (((in[10] >> 30) | ((in[11] & 0x7FFFFF) << 2)) >> 1))) + out[13]
	out[15] = T(((-(((in[11] >> 23) | ((in[12] & 0xFFFF) << 9)) & 1)) ^ (((in[11] >> 23) | ((in[12] & 0xFFFF) << 9)) >> 1))) + out[14]
	out[16] = T(((-(((in[12] >> 16) | ((in[13] & 0x1FF) << 16)) & 1)) ^ (((in[12] >> 16) | ((in[13] & 0x1FF) << 16)) >> 1))) + out[15]
	out[17] = T(((-(((in[13] >> 9) | ((in[14] & 0x3) << 23)) & 1)) ^ (((in[13] >> 9) | ((in[14] & 0x3) << 23)) >> 1))) + out[16]
	out[18] = T(((-(((in[14] >> 2) & 0x1FFFFFF) & 1)) ^ (((in[14] >> 2) & 0x1FFFFFF) >> 1))) + out[17]
	out[19] = T(((-(((in[14] >> 27) | ((in[15] & 0xFFFFF) << 5)) & 1)) ^ (((in[14] >> 27) | ((in[15] & 0xFFFFF) << 5)) >> 1))) + out[18]
	out[20] = T(((-(((in[15] >> 20) | ((in[16] & 0x1FFF) << 12)) & 1)) ^ (((in[15] >> 20) | ((in[16] & 0x1FFF) << 12)) >> 1))) + out[19]
	out[21] = T(((-(((in[16] >> 13) | ((in[17] & 0x3F) << 19)) & 1)) ^ (((in[16] >> 13) | ((in[17] & 0x3F) << 19)) >> 1))) + out[20]
	out[22] = T(((-(((in[17] >> 6) & 0x1FFFFFF) & 1)) ^ (((in[17] >> 6) & 0x1FFFFFF) >> 1))) + out[21]
	out[23] = T(((-(((in[17] >> 31) | ((in[18] & 0xFFFFFF) << 1)) & 1)) ^ (((in[17] >> 31) | ((in[18] & 0xFFFFFF) << 1)) >> 1))) + out[22]
	out[24] = T(((-(((in[18] >> 24) | ((in[19] & 0x1FFFF) << 8)) & 1)) ^ (((in[18] >> 24) | ((in[19] & 0x1FFFF) << 8)) >> 1))) + out[23]
	out[25] = T(((-(((in[19] >> 17) | ((in[20] & 0x3FF) << 15)) & 1)) ^ (((in[19] >> 17) | ((in[20] & 0x3FF) << 15)) >> 1))) + out[24]
	out[26] = T(((-(((in[20] >> 10) | ((in[21] & 0x7) << 22)) & 1)) ^ (((in[20] >> 10) | ((in[21] & 0x7) << 22)) >> 1))) + out[25]
	out[27] = T(((-(((in[21] >> 3) & 0x1FFFFFF) & 1)) ^ (((in[21] >> 3) & 0x1FFFFFF) >> 1))) + out[26]
	out[28] = T(((-(((in[21] >> 28) | ((in[22] & 0x1FFFFF) << 4)) & 1)) ^ (((in[21] >> 28) | ((in[22] & 0x1FFFFF) << 4)) >> 1))) + out[27]
	out[29] = T(((-(((in[22] >> 21) | ((in[23] & 0x3FFF) << 11)) & 1)) ^ (((in[22] >> 21) | ((in[23] & 0x3FFF) << 11)) >> 1))) + out[28]
	out[30] = T(((-(((in[23] >> 14) | ((in[24] & 0x7F) << 18)) & 1)) ^ (((in[23] >> 14) | ((in[24] & 0x7F) << 18)) >> 1))) + out[29]
	out[31] = T(((-((in[24] >> 7) & 1)) ^ ((in[24] >> 7) >> 1))) + out[30]
}

func deltaunpackzigzag32_26[T uint32 | int32](initoffset T, in *[26]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x3FFFFFF) & 1)) ^ (((in[0] >> 0) & 0x3FFFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 26) | ((in[1] & 0xFFFFF) << 6)) & 1)) ^ (((in[0] >> 26) | ((in[1] & 0xFFFFF) << 6)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 20) | ((in[2] & 0x3FFF) << 12)) & 1)) ^ (((in[1] >> 20) | ((in[2] & 0x3FFF) << 12)) >> 1))) + out[1]
	out[3] = T(((-(((in[2] >> 14) | ((in[3] & 0xFF) << 18)) & 1)) ^ (((in[2] >> 14) | ((in[3] & 0xFF) << 18)) >> 1))) + out[2]
	out[4] = T(((-(((in[3] >> 8) | ((in[4] & 0x3) << 24)) & 1)) ^ (((in[3] >> 8) | ((in[4] & 0x3) << 24)) >> 1))) + out[3]
	out[5] = T(((-(((in[4] >> 2) & 0x3FFFFFF) & 1)) ^ (((in[4] >> 2) & 0x3FFFFFF) >> 1))) + out[4]
	out[6] = T(((-(((in[4] >> 28) | ((in[5] & 0x3FFFFF) << 4)) & 1)) ^ (((in[4] >> 28) | ((in[5] & 0x3FFFFF) << 4)) >> 1))) + out[5]
	out[7] = T(((-(((in[5] >> 22) | ((in[6] & 0xFFFF) << 10)) & 1)) ^ (((in[5] >> 22) | ((in[6] & 0xFFFF) << 10)) >> 1))) + out[6]
	out[8] = T(((-(((in[6] >> 16) | ((in[7] & 0x3FF) << 16)) & 1)) ^ (((in[6] >> 16) | ((in[7] & 0x3FF) << 16)) >> 1))) + out[7]
	out[9] = T(((-(((in[7] >> 10) | ((in[8] & 0xF) << 22)) & 1)) ^ (((in[7] >> 10) | ((in[8] & 0xF) << 22)) >> 1))) + out[8]
	out[10] = T(((-(((in[8] >> 4) & 0x3FFFFFF) & 1)) ^ (((in[8] >> 4) & 0x3FFFFFF) >> 1))) + out[9]
	out[11] = T(((-(((in[8] >> 30) | ((in[9] & 0xFFFFFF) << 2)) & 1)) ^ (((in[8] >> 30) | ((in[9] & 0xFFFFFF) << 2)) >> 1))) + out[10]
	out[12] = T(((-(((in[9] >> 24) | ((in[10] & 0x3FFFF) << 8)) & 1)) ^ (((in[9] >> 24) | ((in[10] & 0x3FFFF) << 8)) >> 1))) + out[11]
	out[13] = T(((-(((in[10] >> 18) | ((in[11] & 0xFFF) << 14)) & 1)) ^ (((in[10] >> 18) | ((in[11] & 0xFFF) << 14)) >> 1))) + out[12]
	out[14] = T(((-(((in[11] >> 12) | ((in[12] & 0x3F) << 20)) & 1)) ^ (((in[11] >> 12) | ((in[12] & 0x3F) << 20)) >> 1))) + out[13]
	out[15] = T(((-((in[12] >> 6) & 1)) ^ ((in[12] >> 6) >> 1))) + out[14]
	out[16] = T(((-(((in[13] >> 0) & 0x3FFFFFF) & 1)) ^ (((in[13] >> 0) & 0x3FFFFFF) >> 1))) + out[15]
	out[17] = T(((-(((in[13] >> 26) | ((in[14] & 0xFFFFF) << 6)) & 1)) ^ (((in[13] >> 26) | ((in[14] & 0xFFFFF) << 6)) >> 1))) + out[16]
	out[18] = T(((-(((in[14] >> 20) | ((in[15] & 0x3FFF) << 12)) & 1)) ^ (((in[14] >> 20) | ((in[15] & 0x3FFF) << 12)) >> 1))) + out[17]
	out[19] = T(((-(((in[15] >> 14) | ((in[16] & 0xFF) << 18)) & 1)) ^ (((in[15] >> 14) | ((in[16] & 0xFF) << 18)) >> 1))) + out[18]
	out[20] = T(((-(((in[16] >> 8) | ((in[17] & 0x3) << 24)) & 1)) ^ (((in[16] >> 8) | ((in[17] & 0x3) << 24)) >> 1))) + out[19]
	out[21] = T(((-(((in[17] >> 2) & 0x3FFFFFF) & 1)) ^ (((in[17] >> 2) & 0x3FFFFFF) >> 1))) + out[20]
	out[22] = T(((-(((in[17] >> 28) | ((in[18] & 0x3FFFFF) << 4)) & 1)) ^ (((in[17] >> 28) | ((in[18] & 0x3FFFFF) << 4)) >> 1))) + out[21]
	out[23] = T(((-(((in[18] >> 22) | ((in[19] & 0xFFFF) << 10)) & 1)) ^ (((in[18] >> 22) | ((in[19] & 0xFFFF) << 10)) >> 1))) + out[22]
	out[24] = T(((-(((in[19] >> 16) | ((in[20] & 0x3FF) << 16)) & 1)) ^ (((in[19] >> 16) | ((in[20] & 0x3FF) << 16)) >> 1))) + out[23]
	out[25] = T(((-(((in[20] >> 10) | ((in[21] & 0xF) << 22)) & 1)) ^ (((in[20] >> 10) | ((in[21] & 0xF) << 22)) >> 1))) + out[24]
	out[26] = T(((-(((in[21] >> 4) & 0x3FFFFFF) & 1)) ^ (((in[21] >> 4) & 0x3FFFFFF) >> 1))) + out[25]
	out[27] = T(((-(((in[21] >> 30) | ((in[22] & 0xFFFFFF) << 2)) & 1)) ^ (((in[21] >> 30) | ((in[22] & 0xFFFFFF) << 2)) >> 1))) + out[26]
	out[28] = T(((-(((in[22] >> 24) | ((in[23] & 0x3FFFF) << 8)) & 1)) ^ (((in[22] >> 24) | ((in[23] & 0x3FFFF) << 8)) >> 1))) + out[27]
	out[29] = T(((-(((in[23] >> 18) | ((in[24] & 0xFFF) << 14)) & 1)) ^ (((in[23] >> 18) | ((in[24] & 0xFFF) << 14)) >> 1))) + out[28]
	out[30] = T(((-(((in[24] >> 12) | ((in[25] & 0x3F) << 20)) & 1)) ^ (((in[24] >> 12) | ((in[25] & 0x3F) << 20)) >> 1))) + out[29]
	out[31] = T(((-((in[25] >> 6) & 1)) ^ ((in[25] >> 6) >> 1))) + out[30]
}

func deltaunpackzigzag32_27[T uint32 | int32](initoffset T, in *[27]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x7FFFFFF) & 1)) ^ (((in[0] >> 0) & 0x7FFFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 27) | ((in[1] & 0x3FFFFF) << 5)) & 1)) ^ (((in[0] >> 27) | ((in[1] & 0x3FFFFF) << 5)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 22) | ((in[2] & 0x1FFFF) << 10)) & 1)) ^ (((in[1] >> 22) | ((in[2] & 0x1FFFF) << 10)) >> 1))) + out[1]
	out[3] = T(((-(((in[2] >> 17) | ((in[3] & 0xFFF) << 15)) & 1)) ^ (((in[2] >> 17) | ((in[3] & 0xFFF) << 15)) >> 1))) + out[2]
	out[4] = T(((-(((in[3] >> 12) | ((in[4] & 0x7F) << 20)) & 1)) ^ (((in[3] >> 12) | ((in[4] & 0x7F) << 20)) >> 1))) + out[3]
	out[5] = T(((-(((in[4] >> 7) | ((in[5] & 0x3) << 25)) & 1)) ^ (((in[4] >> 7) | ((in[5] & 0x3) << 25)) >> 1))) + out[4]
	out[6] = T(((-(((in[5] >> 2) & 0x7FFFFFF) & 1)) ^ (((in[5] >> 2) & 0x7FFFFFF) >> 1))) + out[5]
	out[7] = T(((-(((in[5] >> 29) | ((in[6] & 0xFFFFFF) << 3)) & 1)) ^ (((in[5] >> 29) | ((in[6] & 0xFFFFFF) << 3)) >> 1))) + out[6]
	out[8] = T(((-(((in[6] >> 24) | ((in[7] & 0x7FFFF) << 8)) & 1)) ^ (((in[6] >> 24) | ((in[7] & 0x7FFFF) << 8)) >> 1))) + out[7]
	out[9] = T(((-(((in[7] >> 19) | ((in[8] & 0x3FFF) << 13)) & 1)) ^ (((in[7] >> 19) | ((in[8] & 0x3FFF) << 13)) >> 1))) + out[8]
	out[10] = T(((-(((in[8] >> 14) | ((in[9] & 0x1FF) << 18)) & 1)) ^ (((in[8] >> 14) | ((in[9] & 0x1FF) << 18)) >> 1))) + out[9]
	out[11] = T(((-(((in[9] >> 9) | ((in[10] & 0xF) << 23)) & 1)) ^ (((in[9] >> 9) | ((in[10] & 0xF) << 23)) >> 1))) + out[10]
	out[12] = T(((-(((in[10] >> 4) & 0x7FFFFFF) & 1)) ^ (((in[10] >> 4) & 0x7FFFFFF) >> 1))) + out[11]
	out[13] = T(((-(((in[10] >> 31) | ((in[11] & 0x3FFFFFF) << 1)) & 1)) ^ (((in[10] >> 31) | ((in[11] & 0x3FFFFFF) << 1)) >> 1))) + out[12]
	out[14] = T(((-(((in[11] >> 26) | ((in[12] & 0x1FFFFF) << 6)) & 1)) ^ (((in[11] >> 26) | ((in[12] & 0x1FFFFF) << 6)) >> 1))) + out[13]
	out[15] = T(((-(((in[12] >> 21) | ((in[13] & 0xFFFF) << 11)) & 1)) ^ (((in[12] >> 21) | ((in[13] & 0xFFFF) << 11)) >> 1))) + out[14]
	out[16] = T(((-(((in[13] >> 16) | ((in[14] & 0x7FF) << 16)) & 1)) ^ (((in[13] >> 16) | ((in[14] & 0x7FF) << 16)) >> 1))) + out[15]
	out[17] = T(((-(((in[14] >> 11) | ((in[15] & 0x3F) << 21)) & 1)) ^ (((in[14] >> 11) | ((in[15] & 0x3F) << 21)) >> 1))) + out[16]
	out[18] = T(((-(((in[15] >> 6) | ((in[16] & 0x1) << 26)) & 1)) ^ (((in[15] >> 6) | ((in[16] & 0x1) << 26)) >> 1))) + out[17]
	out[19] = T(((-(((in[16] >> 1) & 0x7FFFFFF) & 1)) ^ (((in[16] >> 1) & 0x7FFFFFF) >> 1))) + out[18]
	out[20] = T(((-(((in[16] >> 28) | ((in[17] & 0x7FFFFF) << 4)) & 1)) ^ (((in[16] >> 28) | ((in[17] & 0x7FFFFF) << 4)) >> 1))) + out[19]
	out[21] = T(((-(((in[17] >> 23) | ((in[18] & 0x3FFFF) << 9)) & 1)) ^ (((in[17] >> 23) | ((in[18] & 0x3FFFF) << 9)) >> 1))) + out[20]
	out[22] = T(((-(((in[18] >> 18) | ((in[19] & 0x1FFF) << 14)) & 1)) ^ (((in[18] >> 18) | ((in[19] & 0x1FFF) << 14)) >> 1))) + out[21]
	out[23] = T(((-(((in[19] >> 13) | ((in[20] & 0xFF) << 19)) & 1)) ^ (((in[19] >> 13) | ((in[20] & 0xFF) << 19)) >> 1))) + out[22]
	out[24] = T(((-(((in[20] >> 8) | ((in[21] & 0x7) << 24)) & 1)) ^ (((in[20] >> 8) | ((in[21] & 0x7) << 24)) >> 1))) + out[23]
	out[25] = T(((-(((in[21] >> 3) & 0x7FFFFFF) & 1)) ^ (((in[21] >> 3) & 0x7FFFFFF) >> 1))) + out[24]
	out[26] = T(((-(((in[21] >> 30) | ((in[22] & 0x1FFFFFF) << 2)) & 1)) ^ (((in[21] >> 30) | ((in[22] & 0x1FFFFFF) << 2)) >> 1))) + out[25]
	out[27] = T(((-(((in[22] >> 25) | ((in[23] & 0xFFFFF) << 7)) & 1)) ^ (((in[22] >> 25) | ((in[23] & 0xFFFFF) << 7)) >> 1))) + out[26]
	out[28] = T(((-(((in[23] >> 20) | ((in[24] & 0x7FFF) << 12)) & 1)) ^ (((in[23] >> 20) | ((in[24] & 0x7FFF) << 12)) >> 1))) + out[27]
	out[29] = T(((-(((in[24] >> 15) | ((in[25] & 0x3FF) << 17)) & 1)) ^ (((in[24] >> 15) | ((in[25] & 0x3FF) << 17)) >> 1))) + out[28]
	out[30] = T(((-(((in[25] >> 10) | ((in[26] & 0x1F) << 22)) & 1)) ^ (((in[25] >> 10) | ((in[26] & 0x1F) << 22)) >> 1))) + out[29]
	out[31] = T(((-((in[26] >> 5) & 1)) ^ ((in[26] >> 5) >> 1))) + out[30]
}

func deltaunpackzigzag32_28[T uint32 | int32](initoffset T, in *[28]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0xFFFFFFF) & 1)) ^ (((in[0] >> 0) & 0xFFFFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 28) | ((in[1] & 0xFFFFFF) << 4)) & 1)) ^ (((in[0] >> 28) | ((in[1] & 0xFFFFFF) << 4)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 24) | ((in[2] & 0xFFFFF) << 8)) & 1)) ^ (((in[1] >> 24) | ((in[2] & 0xFFFFF) << 8)) >> 1))) + out[1]
	out[3] = T(((-(((in[2] >> 20) | ((in[3] & 0xFFFF) << 12)) & 1)) ^ (((in[2] >> 20) | ((in[3] & 0xFFFF) << 12)) >> 1))) + out[2]
	out[4] = T(((-(((in[3] >> 16) | ((in[4] & 0xFFF) << 16)) & 1)) ^ (((in[3] >> 16) | ((in[4] & 0xFFF) << 16)) >> 1))) + out[3]
	out[5] = T(((-(((in[4] >> 12) | ((in[5] & 0xFF) << 20)) & 1)) ^ (((in[4] >> 12) | ((in[5] & 0xFF) << 20)) >> 1))) + out[4]
	out[6] = T(((-(((in[5] >> 8) | ((in[6] & 0xF) << 24)) & 1)) ^ (((in[5] >> 8) | ((in[6] & 0xF) << 24)) >> 1))) + out[5]
	out[7] = T(((-((in[6] >> 4) & 1)) ^ ((in[6] >> 4) >> 1))) + out[6]
	out[8] = T(((-(((in[7] >> 0) & 0xFFFFFFF) & 1)) ^ (((in[7] >> 0) & 0xFFFFFFF) >> 1))) + out[7]
	out[9] = T(((-(((in[7] >> 28) | ((in[8] & 0xFFFFFF) << 4)) & 1)) ^ (((in[7] >> 28) | ((in[8] & 0xFFFFFF) << 4)) >> 1))) + out[8]
	out[10] = T(((-(((in[8] >> 24) | ((in[9] & 0xFFFFF) << 8)) & 1)) ^ (((in[8] >> 24) | ((in[9] & 0xFFFFF) << 8)) >> 1))) + out[9]
	out[11] = T(((-(((in[9] >> 20) | ((in[10] & 0xFFFF) << 12)) & 1)) ^ (((in[9] >> 20) | ((in[10] & 0xFFFF) << 12)) >> 1))) + out[10]
	out[12] = T(((-(((in[10] >> 16) | ((in[11] & 0xFFF) << 16)) & 1)) ^ (((in[10] >> 16) | ((in[11] & 0xFFF) << 16)) >> 1))) + out[11]
	out[13] = T(((-(((in[11] >> 12) | ((in[12] & 0xFF) << 20)) & 1)) ^ (((in[11] >> 12) | ((in[12] & 0xFF) << 20)) >> 1))) + out[12]
	out[14] = T(((-(((in[12] >> 8) | ((in[13] & 0xF) << 24)) & 1)) ^ (((in[12] >> 8) | ((in[13] & 0xF) << 24)) >> 1))) + out[13]
	out[15] = T(((-((in[13] >> 4) & 1)) ^ ((in[13] >> 4) >> 1))) + out[14]
	out[16] = T(((-(((in[14] >> 0) & 0xFFFFFFF) & 1)) ^ (((in[14] >> 0) & 0xFFFFFFF) >> 1))) + out[15]
	out[17] = T(((-(((in[14] >> 28) | ((in[15] & 0xFFFFFF) << 4)) & 1)) ^ (((in[14] >> 28) | ((in[15] & 0xFFFFFF) << 4)) >> 1))) + out[16]
	out[18] = T(((-(((in[15] >> 24) | ((in[16] & 0xFFFFF) << 8)) & 1)) ^ (((in[15] >> 24) | ((in[16] & 0xFFFFF) << 8)) >> 1))) + out[17]
	out[19] = T(((-(((in[16] >> 20) | ((in[17] & 0xFFFF) << 12)) & 1)) ^ (((in[16] >> 20) | ((in[17] & 0xFFFF) << 12)) >> 1))) + out[18]
	out[20] = T(((-(((in[17] >> 16) | ((in[18] & 0xFFF) << 16)) & 1)) ^ (((in[17] >> 16) | ((in[18] & 0xFFF) << 16)) >> 1))) + out[19]
	out[21] = T(((-(((in[18] >> 12) | ((in[19] & 0xFF) << 20)) & 1)) ^ (((in[18] >> 12) | ((in[19] & 0xFF) << 20)) >> 1))) + out[20]
	out[22] = T(((-(((in[19] >> 8) | ((in[20] & 0xF) << 24)) & 1)) ^ (((in[19] >> 8) | ((in[20] & 0xF) << 24)) >> 1))) + out[21]
	out[23] = T(((-((in[20] >> 4) & 1)) ^ ((in[20] >> 4) >> 1))) + out[22]
	out[24] = T(((-(((in[21] >> 0) & 0xFFFFFFF) & 1)) ^ (((in[21] >> 0) & 0xFFFFFFF) >> 1))) + out[23]
	out[25] = T(((-(((in[21] >> 28) | ((in[22] & 0xFFFFFF) << 4)) & 1)) ^ (((in[21] >> 28) | ((in[22] & 0xFFFFFF) << 4)) >> 1))) + out[24]
	out[26] = T(((-(((in[22] >> 24) | ((in[23] & 0xFFFFF) << 8)) & 1)) ^ (((in[22] >> 24) | ((in[23] & 0xFFFFF) << 8)) >> 1))) + out[25]
	out[27] = T(((-(((in[23] >> 20) | ((in[24] & 0xFFFF) << 12)) & 1)) ^ (((in[23] >> 20) | ((in[24] & 0xFFFF) << 12)) >> 1))) + out[26]
	out[28] = T(((-(((in[24] >> 16) | ((in[25] & 0xFFF) << 16)) & 1)) ^ (((in[24] >> 16) | ((in[25] & 0xFFF) << 16)) >> 1))) + out[27]
	out[29] = T(((-(((in[25] >> 12) | ((in[26] & 0xFF) << 20)) & 1)) ^ (((in[25] >> 12) | ((in[26] & 0xFF) << 20)) >> 1))) + out[28]
	out[30] = T(((-(((in[26] >> 8) | ((in[27] & 0xF) << 24)) & 1)) ^ (((in[26] >> 8) | ((in[27] & 0xF) << 24)) >> 1))) + out[29]
	out[31] = T(((-((in[27] >> 4) & 1)) ^ ((in[27] >> 4) >> 1))) + out[30]
}

func deltaunpackzigzag32_29[T uint32 | int32](initoffset T, in *[29]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x1FFFFFFF) & 1)) ^ (((in[0] >> 0) & 0x1FFFFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 29) | ((in[1] & 0x3FFFFFF) << 3)) & 1)) ^ (((in[0] >> 29) | ((in[1] & 0x3FFFFFF) << 3)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 26) | ((in[2] & 0x7FFFFF) << 6)) & 1)) ^ (((in[1] >> 26) | ((in[2] & 0x7FFFFF) << 6)) >> 1))) + out[1]
	out[3] = T(((-(((in[2] >> 23) | ((in[3] & 0xFFFFF) << 9)) & 1)) ^ (((in[2] >> 23) | ((in[3] & 0xFFFFF) << 9)) >> 1))) + out[2]
	out[4] = T(((-(((in[3] >> 20) | ((in[4] & 0x1FFFF) << 12)) & 1)) ^ (((in[3] >> 20) | ((in[4] & 0x1FFFF) << 12)) >> 1))) + out[3]
	out[5] = T(((-(((in[4] >> 17) | ((in[5] & 0x3FFF) << 15)) & 1)) ^ (((in[4] >> 17) | ((in[5] & 0x3FFF) << 15)) >> 1))) + out[4]
	out[6] = T(((-(((in[5] >> 14) | ((in[6] & 0x7FF) << 18)) & 1)) ^ (((in[5] >> 14) | ((in[6] & 0x7FF) << 18)) >> 1))) + out[5]
	out[7] = T(((-(((in[6] >> 11) | ((in[7] & 0xFF) << 21)) & 1)) ^ (((in[6] >> 11) | ((in[7] & 0xFF) << 21)) >> 1))) + out[6]
	out[8] = T(((-(((in[7] >> 8) | ((in[8] & 0x1F) << 24)) & 1)) ^ (((in[7] >> 8) | ((in[8] & 0x1F) << 24)) >> 1))) + out[7]
	out[9] = T(((-(((in[8] >> 5) | ((in[9] & 0x3) << 27)) & 1)) ^ (((in[8] >> 5) | ((in[9] & 0x3) << 27)) >> 1))) + out[8]
	out[10] = T(((-(((in[9] >> 2) & 0x1FFFFFFF) & 1)) ^ (((in[9] >> 2) & 0x1FFFFFFF) >> 1))) + out[9]
	out[11] = T(((-(((in[9] >> 31) | ((in[10] & 0xFFFFFFF) << 1)) & 1)) ^ (((in[9] >> 31) | ((in[10] & 0xFFFFFFF) << 1)) >> 1))) + out[10]
	out[12] = T(((-(((in[10] >> 28) | ((in[11] & 0x1FFFFFF) << 4)) & 1)) ^ (((in[10] >> 28) | ((in[11] & 0x1FFFFFF) << 4)) >> 1))) + out[11]
	out[13] = T(((-(((in[11] >> 25) | ((in[12] & 0x3FFFFF) << 7)) & 1)) ^ (((in[11] >> 25) | ((in[12] & 0x3FFFFF) << 7)) >> 1))) + out[12]
	out[14] = T(((-(((in[12] >> 22) | ((in[13] & 0x7FFFF) << 10)) & 1)) ^ (((in[12] >> 22) | ((in[13] & 0x7FFFF) << 10)) >> 1))) + out[13]
	out[15] = T(((-(((in[13] >> 19) | ((in[14] & 0xFFFF) << 13)) & 1)) ^ (((in[13] >> 19) | ((in[14] & 0xFFFF) << 13)) >> 1))) + out[14]
	out[16] = T(((-(((in[14] >> 16) | ((in[15] & 0x1FFF) << 16)) & 1)) ^ (((in[14] >> 16) | ((in[15] & 0x1FFF) << 16)) >> 1))) + out[15]
	out[17] = T(((-(((in[15] >> 13) | ((in[16] & 0x3FF) << 19)) & 1)) ^ (((in[15] >> 13) | ((in[16] & 0x3FF) << 19)) >> 1))) + out[16]
	out[18] = T(((-(((in[16] >> 10) | ((in[17] & 0x7F) << 22)) & 1)) ^ (((in[16] >> 10) | ((in[17] & 0x7F) << 22)) >> 1))) + out[17]
	out[19] = T(((-(((in[17] >> 7) | ((in[18] & 0xF) << 25)) & 1)) ^ (((in[17] >> 7) | ((in[18] & 0xF) << 25)) >> 1))) + out[18]
	out[20] = T(((-(((in[18] >> 4) | ((in[19] & 0x1) << 28)) & 1)) ^ (((in[18] >> 4) | ((in[19] & 0x1) << 28)) >> 1))) + out[19]
	out[21] = T(((-(((in[19] >> 1) & 0x1FFFFFFF) & 1)) ^ (((in[19] >> 1) & 0x1FFFFFFF) >> 1))) + out[20]
	out[22] = T(((-(((in[19] >> 30) | ((in[20] & 0x7FFFFFF) << 2)) & 1)) ^ (((in[19] >> 30) | ((in[20] & 0x7FFFFFF) << 2)) >> 1))) + out[21]
	out[23] = T(((-(((in[20] >> 27) | ((in[21] & 0xFFFFFF) << 5)) & 1)) ^ (((in[20] >> 27) | ((in[21] & 0xFFFFFF) << 5)) >> 1))) + out[22]
	out[24] = T(((-(((in[21] >> 24) | ((in[22] & 0x1FFFFF) << 8)) & 1)) ^ (((in[21] >> 24) | ((in[22] & 0x1FFFFF) << 8)) >> 1))) + out[23]
	out[25] = T(((-(((in[22] >> 21) | ((in[23] & 0x3FFFF) << 11)) & 1)) ^ (((in[22] >> 21) | ((in[23] & 0x3FFFF) << 11)) >> 1))) + out[24]
	out[26] = T(((-(((in[23] >> 18) | ((in[24] & 0x7FFF) << 14)) & 1)) ^ (((in[23] >> 18) | ((in[24] & 0x7FFF) << 14)) >> 1))) + out[25]
	out[27] = T(((-(((in[24] >> 15) | ((in[25] & 0xFFF) << 17)) & 1)) ^ (((in[24] >> 15) | ((in[25] & 0xFFF) << 17)) >> 1))) + out[26]
	out[28] = T(((-(((in[25] >> 12) | ((in[26] & 0x1FF) << 20)) & 1)) ^ (((in[25] >> 12) | ((in[26] & 0x1FF) << 20)) >> 1))) + out[27]
	out[29] = T(((-(((in[26] >> 9) | ((in[27] & 0x3F) << 23)) & 1)) ^ (((in[26] >> 9) | ((in[27] & 0x3F) << 23)) >> 1))) + out[28]
	out[30] = T(((-(((in[27] >> 6) | ((in[28] & 0x7) << 26)) & 1)) ^ (((in[27] >> 6) | ((in[28] & 0x7) << 26)) >> 1))) + out[29]
	out[31] = T(((-((in[28] >> 3) & 1)) ^ ((in[28] >> 3) >> 1))) + out[30]
}

func deltaunpackzigzag32_30[T uint32 | int32](initoffset T, in *[30]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x3FFFFFFF) & 1)) ^ (((in[0] >> 0) & 0x3FFFFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 30) | ((in[1] & 0xFFFFFFF) << 2)) & 1)) ^ (((in[0] >> 30) | ((in[1] & 0xFFFFFFF) << 2)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 28) | ((in[2] & 0x3FFFFFF) << 4)) & 1)) ^ (((in[1] >> 28) | ((in[2] & 0x3FFFFFF) << 4)) >> 1))) + out[1]
	out[3] = T(((-(((in[2] >> 26) | ((in[3] & 0xFFFFFF) << 6)) & 1)) ^ (((in[2] >> 26) | ((in[3] & 0xFFFFFF) << 6)) >> 1))) + out[2]
	out[4] = T(((-(((in[3] >> 24) | ((in[4] & 0x3FFFFF) << 8)) & 1)) ^ (((in[3] >> 24) | ((in[4] & 0x3FFFFF) << 8)) >> 1))) + out[3]
	out[5] = T(((-(((in[4] >> 22) | ((in[5] & 0xFFFFF) << 10)) & 1)) ^ (((in[4] >> 22) | ((in[5] & 0xFFFFF) << 10)) >> 1))) + out[4]
	out[6] = T(((-(((in[5] >> 20) | ((in[6] & 0x3FFFF) << 12)) & 1)) ^ (((in[5] >> 20) | ((in[6] & 0x3FFFF) << 12)) >> 1))) + out[5]
	out[7] = T(((-(((in[6] >> 18) | ((in[7] & 0xFFFF) << 14)) & 1)) ^ (((in[6] >> 18) | ((in[7] & 0xFFFF) << 14)) >> 1))) + out[6]
	out[8] = T(((-(((in[7] >> 16) | ((in[8] & 0x3FFF) << 16)) & 1)) ^ (((in[7] >> 16) | ((in[8] & 0x3FFF) << 16)) >> 1))) + out[7]
	out[9] = T(((-(((in[8] >> 14) | ((in[9] & 0xFFF) << 18)) & 1)) ^ (((in[8] >> 14) | ((in[9] & 0xFFF) << 18)) >> 1))) + out[8]
	out[10] = T(((-(((in[9] >> 12) | ((in[10] & 0x3FF) << 20)) & 1)) ^ (((in[9] >> 12) | ((in[10] & 0x3FF) << 20)) >> 1))) + out[9]
	out[11] = T(((-(((in[10] >> 10) | ((in[11] & 0xFF) << 22)) & 1)) ^ (((in[10] >> 10) | ((in[11] & 0xFF) << 22)) >> 1))) + out[10]
	out[12] = T(((-(((in[11] >> 8) | ((in[12] & 0x3F) << 24)) & 1)) ^ (((in[11] >> 8) | ((in[12] & 0x3F) << 24)) >> 1))) + out[11]
	out[13] = T(((-(((in[12] >> 6) | ((in[13] & 0xF) << 26)) & 1)) ^ (((in[12] >> 6) | ((in[13] & 0xF) << 26)) >> 1))) + out[12]
	out[14] = T(((-(((in[13] >> 4) | ((in[14] & 0x3) << 28)) & 1)) ^ (((in[13] >> 4) | ((in[14] & 0x3) << 28)) >> 1))) + out[13]
	out[15] = T(((-((in[14] >> 2) & 1)) ^ ((in[14] >> 2) >> 1))) + out[14]
	out[16] = T(((-(((in[15] >> 0) & 0x3FFFFFFF) & 1)) ^ (((in[15] >> 0) & 0x3FFFFFFF) >> 1))) + out[15]
	out[17] = T(((-(((in[15] >> 30) | ((in[16] & 0xFFFFFFF) << 2)) & 1)) ^ (((in[15] >> 30) | ((in[16] & 0xFFFFFFF) << 2)) >> 1))) + out[16]
	out[18] = T(((-(((in[16] >> 28) | ((in[17] & 0x3FFFFFF) << 4)) & 1)) ^ (((in[16] >> 28) | ((in[17] & 0x3FFFFFF) << 4)) >> 1))) + out[17]
	out[19] = T(((-(((in[17] >> 26) | ((in[18] & 0xFFFFFF) << 6)) & 1)) ^ (((in[17] >> 26) | ((in[18] & 0xFFFFFF) << 6)) >> 1))) + out[18]
	out[20] = T(((-(((in[18] >> 24) | ((in[19] & 0x3FFFFF) << 8)) & 1)) ^ (((in[18] >> 24) | ((in[19] & 0x3FFFFF) << 8)) >> 1))) + out[19]
	out[21] = T(((-(((in[19] >> 22) | ((in[20] & 0xFFFFF) << 10)) & 1)) ^ (((in[19] >> 22) | ((in[20] & 0xFFFFF) << 10)) >> 1))) + out[20]
	out[22] = T(((-(((in[20] >> 20) | ((in[21] & 0x3FFFF) << 12)) & 1)) ^ (((in[20] >> 20) | ((in[21] & 0x3FFFF) << 12)) >> 1))) + out[21]
	out[23] = T(((-(((in[21] >> 18) | ((in[22] & 0xFFFF) << 14)) & 1)) ^ (((in[21] >> 18) | ((in[22] & 0xFFFF) << 14)) >> 1))) + out[22]
	out[24] = T(((-(((in[22] >> 16) | ((in[23] & 0x3FFF) << 16)) & 1)) ^ (((in[22] >> 16) | ((in[23] & 0x3FFF) << 16)) >> 1))) + out[23]
	out[25] = T(((-(((in[23] >> 14) | ((in[24] & 0xFFF) << 18)) & 1)) ^ (((in[23] >> 14) | ((in[24] & 0xFFF) << 18)) >> 1))) + out[24]
	out[26] = T(((-(((in[24] >> 12) | ((in[25] & 0x3FF) << 20)) & 1)) ^ (((in[24] >> 12) | ((in[25] & 0x3FF) << 20)) >> 1))) + out[25]
	out[27] = T(((-(((in[25] >> 10) | ((in[26] & 0xFF) << 22)) & 1)) ^ (((in[25] >> 10) | ((in[26] & 0xFF) << 22)) >> 1))) + out[26]
	out[28] = T(((-(((in[26] >> 8) | ((in[27] & 0x3F) << 24)) & 1)) ^ (((in[26] >> 8) | ((in[27] & 0x3F) << 24)) >> 1))) + out[27]
	out[29] = T(((-(((in[27] >> 6) | ((in[28] & 0xF) << 26)) & 1)) ^ (((in[27] >> 6) | ((in[28] & 0xF) << 26)) >> 1))) + out[28]
	out[30] = T(((-(((in[28] >> 4) | ((in[29] & 0x3) << 28)) & 1)) ^ (((in[28] >> 4) | ((in[29] & 0x3) << 28)) >> 1))) + out[29]
	out[31] = T(((-((in[29] >> 2) & 1)) ^ ((in[29] >> 2) >> 1))) + out[30]
}

func deltaunpackzigzag32_31[T uint32 | int32](initoffset T, in *[31]uint32, out *[32]T) {
	out[0] = T(((-(((in[0] >> 0) & 0x7FFFFFFF) & 1)) ^ (((in[0] >> 0) & 0x7FFFFFFF) >> 1))) + initoffset
	out[1] = T(((-(((in[0] >> 31) | ((in[1] & 0x3FFFFFFF) << 1)) & 1)) ^ (((in[0] >> 31) | ((in[1] & 0x3FFFFFFF) << 1)) >> 1))) + out[0]
	out[2] = T(((-(((in[1] >> 30) | ((in[2] & 0x1FFFFFFF) << 2)) & 1)) ^ (((in[1] >> 30) | ((in[2] & 0x1FFFFFFF) << 2)) >> 1))) + out[1]
	out[3] = T(((-(((in[2] >> 29) | ((in[3] & 0xFFFFFFF) << 3)) & 1)) ^ (((in[2] >> 29) | ((in[3] & 0xFFFFFFF) << 3)) >> 1))) + out[2]
	out[4] = T(((-(((in[3] >> 28) | ((in[4] & 0x7FFFFFF) << 4)) & 1)) ^ (((in[3] >> 28) | ((in[4] & 0x7FFFFFF) << 4)) >> 1))) + out[3]
	out[5] = T(((-(((in[4] >> 27) | ((in[5] & 0x3FFFFFF) << 5)) & 1)) ^ (((in[4] >> 27) | ((in[5] & 0x3FFFFFF) << 5)) >> 1))) + out[4]
	out[6] = T(((-(((in[5] >> 26) | ((in[6] & 0x1FFFFFF) << 6)) & 1)) ^ (((in[5] >> 26) | ((in[6] & 0x1FFFFFF) << 6)) >> 1))) + out[5]
	out[7] = T(((-(((in[6] >> 25) | ((in[7] & 0xFFFFFF) << 7)) & 1)) ^ (((in[6] >> 25) | ((in[7] & 0xFFFFFF) << 7)) >> 1))) + out[6]
	out[8] = T(((-(((in[7] >> 24) | ((in[8] & 0x7FFFFF) << 8)) & 1)) ^ (((in[7] >> 24) | ((in[8] & 0x7FFFFF) << 8)) >> 1))) + out[7]
	out[9] = T(((-(((in[8] >> 23) | ((in[9] & 0x3FFFFF) << 9)) & 1)) ^ (((in[8] >> 23) | ((in[9] & 0x3FFFFF) << 9)) >> 1))) + out[8]
	out[10] = T(((-(((in[9] >> 22) | ((in[10] & 0x1FFFFF) << 10)) & 1)) ^ (((in[9] >> 22) | ((in[10] & 0x1FFFFF) << 10)) >> 1))) + out[9]
	out[11] = T(((-(((in[10] >> 21) | ((in[11] & 0xFFFFF) << 11)) & 1)) ^ (((in[10] >> 21) | ((in[11] & 0xFFFFF) << 11)) >> 1))) + out[10]
	out[12] = T(((-(((in[11] >> 20) | ((in[12] & 0x7FFFF) << 12)) & 1)) ^ (((in[11] >> 20) | ((in[12] & 0x7FFFF) << 12)) >> 1))) + out[11]
	out[13] = T(((-(((in[12] >> 19) | ((in[13] & 0x3FFFF) << 13)) & 1)) ^ (((in[12] >> 19) | ((in[13] & 0x3FFFF) << 13)) >> 1))) + out[12]
	out[14] = T(((-(((in[13] >> 18) | ((in[14] & 0x1FFFF) << 14)) & 1)) ^ (((in[13] >> 18) | ((in[14] & 0x1FFFF) << 14)) >> 1))) + out[13]
	out[15] = T(((-(((in[14] >> 17) | ((in[15] & 0xFFFF) << 15)) & 1)) ^ (((in[14] >> 17) | ((in[15] & 0xFFFF) << 15)) >> 1))) + out[14]
	out[16] = T(((-(((in[15] >> 16) | ((in[16] & 0x7FFF) << 16)) & 1)) ^ (((in[15] >> 16) | ((in[16] & 0x7FFF) << 16)) >> 1))) + out[15]
	out[17] = T(((-(((in[16] >> 15) | ((in[17] & 0x3FFF) << 17)) & 1)) ^ (((in[16] >> 15) | ((in[17] & 0x3FFF) << 17)) >> 1))) + out[16]
	out[18] = T(((-(((in[17] >> 14) | ((in[18] & 0x1FFF) << 18)) & 1)) ^ (((in[17] >> 14) | ((in[18] & 0x1FFF) << 18)) >> 1))) + out[17]
	out[19] = T(((-(((in[18] >> 13) | ((in[19] & 0xFFF) << 19)) & 1)) ^ (((in[18] >> 13) | ((in[19] & 0xFFF) << 19)) >> 1))) + out[18]
	out[20] = T(((-(((in[19] >> 12) | ((in[20] & 0x7FF) << 20)) & 1)) ^ (((in[19] >> 12) | ((in[20] & 0x7FF) << 20)) >> 1))) + out[19]
	out[21] = T(((-(((in[20] >> 11) | ((in[21] & 0x3FF) << 21)) & 1)) ^ (((in[20] >> 11) | ((in[21] & 0x3FF) << 21)) >> 1))) + out[20]
	out[22] = T(((-(((in[21] >> 10) | ((in[22] & 0x1FF) << 22)) & 1)) ^ (((in[21] >> 10) | ((in[22] & 0x1FF) << 22)) >> 1))) + out[21]
	out[23] = T(((-(((in[22] >> 9) | ((in[23] & 0xFF) << 23)) & 1)) ^ (((in[22] >> 9) | ((in[23] & 0xFF) << 23)) >> 1))) + out[22]
	out[24] = T(((-(((in[23] >> 8) | ((in[24] & 0x7F) << 24)) & 1)) ^ (((in[23] >> 8) | ((in[24] & 0x7F) << 24)) >> 1))) + out[23]
	out[25] = T(((-(((in[24] >> 7) | ((in[25] & 0x3F) << 25)) & 1)) ^ (((in[24] >> 7) | ((in[25] & 0x3F) << 25)) >> 1))) + out[24]
	out[26] = T(((-(((in[25] >> 6) | ((in[26] & 0x1F) << 26)) & 1)) ^ (((in[25] >> 6) | ((in[26] & 0x1F) << 26)) >> 1))) + out[25]
	out[27] = T(((-(((in[26] >> 5) | ((in[27] & 0xF) << 27)) & 1)) ^ (((in[26] >> 5) | ((in[27] & 0xF) << 27)) >> 1))) + out[26]
	out[28] = T(((-(((in[27] >> 4) | ((in[28] & 0x7) << 28)) & 1)) ^ (((in[27] >> 4) | ((in[28] & 0x7) << 28)) >> 1))) + out[27]
	out[29] = T(((-(((in[28] >> 3) | ((in[29] & 0x3) << 29)) & 1)) ^ (((in[28] >> 3) | ((in[29] & 0x3) << 29)) >> 1))) + out[28]
	out[30] = T(((-(((in[29] >> 2) | ((in[30] & 0x1) << 30)) & 1)) ^ (((in[29] >> 2) | ((in[30] & 0x1) << 30)) >> 1))) + out[29]
	out[31] = T(((-((in[30] >> 1) & 1)) ^ ((in[30] >> 1) >> 1))) + out[30]
}
