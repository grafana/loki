//go:build !purego

package xxhash

import "golang.org/x/sys/cpu"

// This file contains the declaration of signatures for the multi hashing
// functions implemented in sum64uint_amd64.s, which provides vectorized
// versions of the algorithms written in sum64uint_purego.go.
//
// The use of SIMD optimization yields measurable throughput increases when
// computing multiple hash values in parallel compared to hashing values
// individually in loops:
//
// name                   old speed      new speed      delta
// MultiSum64Uint8/4KB    4.94GB/s ± 2%  6.82GB/s ± 5%  +38.00%  (p=0.000 n=10+10)
// MultiSum64Uint16/4KB   3.44GB/s ± 2%  4.63GB/s ± 4%  +34.56%  (p=0.000 n=10+10)
// MultiSum64Uint32/4KB   4.84GB/s ± 2%  6.39GB/s ± 4%  +31.94%  (p=0.000 n=10+10)
// MultiSum64Uint64/4KB   3.77GB/s ± 2%  4.95GB/s ± 2%  +31.14%  (p=0.000 n=9+10)
// MultiSum64Uint128/4KB  1.84GB/s ± 2%  3.11GB/s ± 4%  +68.70%  (p=0.000 n=9+10)
//
// name                   old hash/s     new hash/s     delta
// MultiSum64Uint8/4KB        617M ± 2%      852M ± 5%  +38.00%  (p=0.000 n=10+10)
// MultiSum64Uint16/4KB       431M ± 2%      579M ± 4%  +34.56%  (p=0.000 n=10+10)
// MultiSum64Uint32/4KB       605M ± 2%      799M ± 4%  +31.94%  (p=0.000 n=10+10)
// MultiSum64Uint64/4KB       471M ± 2%      618M ± 2%  +31.14%  (p=0.000 n=9+10)
// MultiSum64Uint128/4KB      231M ± 2%      389M ± 4%  +68.70%  (p=0.000 n=9+10)
//
// The benchmarks measure the throughput of hashes produced, as a rate of values
// and bytes.

var hasAVX512 = cpu.X86.HasAVX512 &&
	cpu.X86.HasAVX512F &&
	cpu.X86.HasAVX512CD

//go:noescape
func MultiSum64Uint8(h []uint64, v []uint8) int

//go:noescape
func MultiSum64Uint16(h []uint64, v []uint16) int

//go:noescape
func MultiSum64Uint32(h []uint64, v []uint32) int

//go:noescape
func MultiSum64Uint64(h []uint64, v []uint64) int

//go:noescape
func MultiSum64Uint128(h []uint64, v [][16]byte) int
