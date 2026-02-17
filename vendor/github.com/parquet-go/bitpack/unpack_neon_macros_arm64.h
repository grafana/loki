// Macro definitions for unsupported NEON instructions using WORD encodings
// USHLL Vd.8H, Vn.8B, #0 - widen 8x8-bit to 8x16-bit
#define USHLL_8H_8B(vd, vn) WORD $(0x2f08a400 | (vd) | ((vn)<<5))

// USHLL2 Vd.8H, Vn.16B, #0 - widen upper 8x8-bit to 8x16-bit
#define USHLL2_8H_16B(vd, vn) WORD $(0x6f08a400 | (vd) | ((vn)<<5))

// USHLL Vd.4S, Vn.4H, #0 - widen 4x16-bit to 4x32-bit
#define USHLL_4S_4H(vd, vn) WORD $(0x2f10a400 | (vd) | ((vn)<<5))

// USHLL2 Vd.4S, Vn.8H, #0 - widen upper 4x16-bit to 4x32-bit
#define USHLL2_4S_8H(vd, vn) WORD $(0x6f10a400 | (vd) | ((vn)<<5))

// USHLL Vd.2D, Vn.2S, #0 - widen 2x32-bit to 2x64-bit
#define USHLL_2D_2S(vd, vn) WORD $(0x2f20a400 | (vd) | ((vn)<<5))

// USHLL2 Vd.2D, Vn.4S, #0 - widen upper 2x32-bit to 2x64-bit
#define USHLL2_2D_4S(vd, vn) WORD $(0x6f20a400 | (vd) | ((vn)<<5))
