//go:build purego && parquet.bloom.no_unroll

package bloom

// This file contains direct translation of the algorithms described in the
// parquet bloom filter spec:
// https://github.com/apache/parquet-format/blob/master/BloomFilter.md
//
// There are no practical reasons to eable the parquet.bloom.no_unroll build
// tag, the code is left here as a reference to ensure that the optimized
// implementations of block operations behave the same as the functions in this
// file.

var salt = [8]uint32{
	0: salt0,
	1: salt1,
	2: salt2,
	3: salt3,
	4: salt4,
	5: salt5,
	6: salt6,
	7: salt7,
}

func (w *Word) set(i uint) {
	*w |= Word(1 << i)
}

func (w Word) has(i uint) bool {
	return ((w >> Word(i)) & 1) != 0
}

func mask(x uint32) Block {
	var b Block
	for i := uint(0); i < 8; i++ {
		y := x * salt[i]
		b[i].set(uint(y) >> 27)
	}
	return b
}

func (b *Block) Insert(x uint32) {
	masked := mask(x)
	for i := uint(0); i < 8; i++ {
		for j := uint(0); j < 32; j++ {
			if masked[i].has(j) {
				b[i].set(j)
			}
		}
	}
}

func (b *Block) Check(x uint32) bool {
	masked := mask(x)
	for i := uint(0); i < 8; i++ {
		for j := uint(0); j < 32; j++ {
			if masked[i].has(j) {
				if !b[i].has(j) {
					return false
				}
			}
		}
	}
	return true
}
