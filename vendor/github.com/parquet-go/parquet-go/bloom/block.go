package bloom

import "unsafe"

// Word represents 32 bits words of bloom filter blocks.
type Word uint32

// Block represents bloom filter blocks which contain eight 32 bits words.
type Block [8]Word

// Bytes returns b as a byte slice.
func (b *Block) Bytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(b)), BlockSize)
}

const (
	// BlockSize is the size of bloom filter blocks in bytes.
	BlockSize = 32

	salt0 = 0x47b6137b
	salt1 = 0x44974d91
	salt2 = 0x8824ad5b
	salt3 = 0xa2b7289d
	salt4 = 0x705495c7
	salt5 = 0x2df1424b
	salt6 = 0x9efc4947
	salt7 = 0x5c6bfb31
)
