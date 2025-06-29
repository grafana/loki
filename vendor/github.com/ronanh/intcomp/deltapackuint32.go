// This file is generated, do not modify directly
// Use 'go generate' to regenerate.

package intcomp

import "unsafe"

// deltaPack_uint32 Binary packing of one block of `in`, starting from `initoffset`
// to out. Differential coding is applied first.
// Caller must give the proper `bitlen` of the block
func deltaPack_uint32[T uint32 | int32](initoffset T, in []T, out []uint32, bitlen int) {
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

// deltaUnpack_uint32 Decoding operation for DeltaPack_uint32
func deltaUnpack_uint32[T uint32 | int32](initoffset T, in []uint32, out []T, bitlen int) {
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

// --- zigzag

// deltaPackZigzag_uint32 Binary packing of one block of `in`, starting from `initoffset`
// to out. Differential coding is applied first, the difference is zigzag encoded.
//
//	Caller must give the proper `bitlen` of the block
func deltaPackZigzag_uint32(initoffset uint32, in []uint32, out []uint32, bitlen int) {
	switch bitlen {
	case 0:
		deltapackzigzag32_0(initoffset, (*[32]uint32)(in), (*[0]uint32)(out))
	case 1:
		deltapackzigzag32_1(initoffset, (*[32]uint32)(in), (*[1]uint32)(out))
	case 2:
		deltapackzigzag32_2(initoffset, (*[32]uint32)(in), (*[2]uint32)(out))
	case 3:
		deltapackzigzag32_3(initoffset, (*[32]uint32)(in), (*[3]uint32)(out))
	case 4:
		deltapackzigzag32_4(initoffset, (*[32]uint32)(in), (*[4]uint32)(out))
	case 5:
		deltapackzigzag32_5(initoffset, (*[32]uint32)(in), (*[5]uint32)(out))
	case 6:
		deltapackzigzag32_6(initoffset, (*[32]uint32)(in), (*[6]uint32)(out))
	case 7:
		deltapackzigzag32_7(initoffset, (*[32]uint32)(in), (*[7]uint32)(out))
	case 8:
		deltapackzigzag32_8(initoffset, (*[32]uint32)(in), (*[8]uint32)(out))
	case 9:
		deltapackzigzag32_9(initoffset, (*[32]uint32)(in), (*[9]uint32)(out))
	case 10:
		deltapackzigzag32_10(initoffset, (*[32]uint32)(in), (*[10]uint32)(out))
	case 11:
		deltapackzigzag32_11(initoffset, (*[32]uint32)(in), (*[11]uint32)(out))
	case 12:
		deltapackzigzag32_12(initoffset, (*[32]uint32)(in), (*[12]uint32)(out))
	case 13:
		deltapackzigzag32_13(initoffset, (*[32]uint32)(in), (*[13]uint32)(out))
	case 14:
		deltapackzigzag32_14(initoffset, (*[32]uint32)(in), (*[14]uint32)(out))
	case 15:
		deltapackzigzag32_15(initoffset, (*[32]uint32)(in), (*[15]uint32)(out))
	case 16:
		deltapackzigzag32_16(initoffset, (*[32]uint32)(in), (*[16]uint32)(out))
	case 17:
		deltapackzigzag32_17(initoffset, (*[32]uint32)(in), (*[17]uint32)(out))
	case 18:
		deltapackzigzag32_18(initoffset, (*[32]uint32)(in), (*[18]uint32)(out))
	case 19:
		deltapackzigzag32_19(initoffset, (*[32]uint32)(in), (*[19]uint32)(out))
	case 20:
		deltapackzigzag32_20(initoffset, (*[32]uint32)(in), (*[20]uint32)(out))
	case 21:
		deltapackzigzag32_21(initoffset, (*[32]uint32)(in), (*[21]uint32)(out))
	case 22:
		deltapackzigzag32_22(initoffset, (*[32]uint32)(in), (*[22]uint32)(out))
	case 23:
		deltapackzigzag32_23(initoffset, (*[32]uint32)(in), (*[23]uint32)(out))
	case 24:
		deltapackzigzag32_24(initoffset, (*[32]uint32)(in), (*[24]uint32)(out))
	case 25:
		deltapackzigzag32_25(initoffset, (*[32]uint32)(in), (*[25]uint32)(out))
	case 26:
		deltapackzigzag32_26(initoffset, (*[32]uint32)(in), (*[26]uint32)(out))
	case 27:
		deltapackzigzag32_27(initoffset, (*[32]uint32)(in), (*[27]uint32)(out))
	case 28:
		deltapackzigzag32_28(initoffset, (*[32]uint32)(in), (*[28]uint32)(out))
	case 29:
		deltapackzigzag32_29(initoffset, (*[32]uint32)(in), (*[29]uint32)(out))
	case 30:
		deltapackzigzag32_30(initoffset, (*[32]uint32)(in), (*[30]uint32)(out))
	case 31:
		deltapackzigzag32_31(initoffset, (*[32]uint32)(in), (*[31]uint32)(out))
	case 32:
		*(*[32]uint32)(out) = *((*[32]uint32)(unsafe.Pointer((*[32]uint32)(in))))
	default:
		panic("unsupported bitlen")
	}
}

// deltaUnpackZigzag_uint32 Decoding operation for DeltaPackZigzag_uint32
func deltaUnpackZigzag_uint32(initoffset uint32, in []uint32, out []uint32, bitlen int) {
	switch bitlen {
	case 0:
		deltaunpackzigzag32_0(initoffset, (*[0]uint32)(in), (*[32]uint32)(out))
	case 1:
		deltaunpackzigzag32_1(initoffset, (*[1]uint32)(in), (*[32]uint32)(out))
	case 2:
		deltaunpackzigzag32_2(initoffset, (*[2]uint32)(in), (*[32]uint32)(out))
	case 3:
		deltaunpackzigzag32_3(initoffset, (*[3]uint32)(in), (*[32]uint32)(out))
	case 4:
		deltaunpackzigzag32_4(initoffset, (*[4]uint32)(in), (*[32]uint32)(out))
	case 5:
		deltaunpackzigzag32_5(initoffset, (*[5]uint32)(in), (*[32]uint32)(out))
	case 6:
		deltaunpackzigzag32_6(initoffset, (*[6]uint32)(in), (*[32]uint32)(out))
	case 7:
		deltaunpackzigzag32_7(initoffset, (*[7]uint32)(in), (*[32]uint32)(out))
	case 8:
		deltaunpackzigzag32_8(initoffset, (*[8]uint32)(in), (*[32]uint32)(out))
	case 9:
		deltaunpackzigzag32_9(initoffset, (*[9]uint32)(in), (*[32]uint32)(out))
	case 10:
		deltaunpackzigzag32_10(initoffset, (*[10]uint32)(in), (*[32]uint32)(out))
	case 11:
		deltaunpackzigzag32_11(initoffset, (*[11]uint32)(in), (*[32]uint32)(out))
	case 12:
		deltaunpackzigzag32_12(initoffset, (*[12]uint32)(in), (*[32]uint32)(out))
	case 13:
		deltaunpackzigzag32_13(initoffset, (*[13]uint32)(in), (*[32]uint32)(out))
	case 14:
		deltaunpackzigzag32_14(initoffset, (*[14]uint32)(in), (*[32]uint32)(out))
	case 15:
		deltaunpackzigzag32_15(initoffset, (*[15]uint32)(in), (*[32]uint32)(out))
	case 16:
		deltaunpackzigzag32_16(initoffset, (*[16]uint32)(in), (*[32]uint32)(out))
	case 17:
		deltaunpackzigzag32_17(initoffset, (*[17]uint32)(in), (*[32]uint32)(out))
	case 18:
		deltaunpackzigzag32_18(initoffset, (*[18]uint32)(in), (*[32]uint32)(out))
	case 19:
		deltaunpackzigzag32_19(initoffset, (*[19]uint32)(in), (*[32]uint32)(out))
	case 20:
		deltaunpackzigzag32_20(initoffset, (*[20]uint32)(in), (*[32]uint32)(out))
	case 21:
		deltaunpackzigzag32_21(initoffset, (*[21]uint32)(in), (*[32]uint32)(out))
	case 22:
		deltaunpackzigzag32_22(initoffset, (*[22]uint32)(in), (*[32]uint32)(out))
	case 23:
		deltaunpackzigzag32_23(initoffset, (*[23]uint32)(in), (*[32]uint32)(out))
	case 24:
		deltaunpackzigzag32_24(initoffset, (*[24]uint32)(in), (*[32]uint32)(out))
	case 25:
		deltaunpackzigzag32_25(initoffset, (*[25]uint32)(in), (*[32]uint32)(out))
	case 26:
		deltaunpackzigzag32_26(initoffset, (*[26]uint32)(in), (*[32]uint32)(out))
	case 27:
		deltaunpackzigzag32_27(initoffset, (*[27]uint32)(in), (*[32]uint32)(out))
	case 28:
		deltaunpackzigzag32_28(initoffset, (*[28]uint32)(in), (*[32]uint32)(out))
	case 29:
		deltaunpackzigzag32_29(initoffset, (*[29]uint32)(in), (*[32]uint32)(out))
	case 30:
		deltaunpackzigzag32_30(initoffset, (*[30]uint32)(in), (*[32]uint32)(out))
	case 31:
		deltaunpackzigzag32_31(initoffset, (*[31]uint32)(in), (*[32]uint32)(out))
	case 32:
		*(*[32]uint32)(out) = *(*[32]uint32)(unsafe.Pointer((*[32]uint32)(in)))
	default:
		panic("unsupported bitlen")
	}
}
