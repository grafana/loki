//go:build !purego

package bytealg

// This function is similar to using the standard bytes.Count function with a
// one-byte separator. However, the implementation makes use of AVX-512 when
// possible, which yields measurable throughput improvements:
//
// name       old time/op    new time/op    delta
// CountByte    82.5ns ± 0%    43.9ns ± 0%  -46.74%  (p=0.000 n=10+10)
//
// name       old speed      new speed      delta
// CountByte  49.6GB/s ± 0%  93.2GB/s ± 0%  +87.74%  (p=0.000 n=10+10)
//
// On systems that do not have AVX-512, the AVX2 version of the code is also
// optimized to make use of multiple register lanes, which gives a bit better
// throughput than the standard library function:
//
// name       old time/op    new time/op    delta
// CountByte    82.5ns ± 0%    61.0ns ± 0%  -26.04%  (p=0.000 n=10+10)
//
// name       old speed      new speed      delta
// CountByte  49.6GB/s ± 0%  67.1GB/s ± 0%  +35.21%  (p=0.000 n=10+10)
//
//go:noescape
func Count(data []byte, value byte) int
