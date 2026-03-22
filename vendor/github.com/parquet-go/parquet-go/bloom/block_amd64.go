//go:build !purego

package bloom

import "golang.org/x/sys/cpu"

// The functions in this file are SIMD-optimized versions of the functions
// declared in block_optimized.go for x86 targets.
//
// The optimization yields measurable improvements over the pure Go versions:
//
// goos: darwin
// goarch: amd64
// pkg: github.com/parquet-go/parquet-go/bloom
// cpu: Intel(R) Core(TM) i9-8950HK CPU @ 2.90GHz
//
// name         old time/op    new time/op     delta
// BlockInsert    11.6ns ± 4%      2.0ns ± 3%   -82.37%  (p=0.000 n=8+8)
// BlockCheck     12.6ns ±28%      2.1ns ± 4%   -83.12%  (p=0.000 n=10+8)
//
// name         old speed      new speed       delta
// BlockInsert  2.73GB/s ±13%  15.70GB/s ± 3%  +475.96%  (p=0.000 n=9+8)
// BlockCheck   2.59GB/s ±23%  15.06GB/s ± 4%  +482.25%  (p=0.000 n=10+8)
//
// Note that the numbers above are a comparison to the routines implemented in
// block_optimized.go; the delta comparing to functions in block_default.go is
// significantly larger but not very interesting since those functions have no
// practical use cases.
var hasAVX2 = cpu.X86.HasAVX2

//go:noescape
func blockInsert(b *Block, x uint32)

//go:noescape
func blockCheck(b *Block, x uint32) bool

func (b *Block) Insert(x uint32) { blockInsert(b, x) }

func (b *Block) Check(x uint32) bool { return blockCheck(b, x) }
