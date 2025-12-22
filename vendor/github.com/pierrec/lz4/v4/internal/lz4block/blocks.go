// Package lz4block provides LZ4 BlockSize types and pools of buffers.
package lz4block

import (
	"fmt"
	"sync"
)

const (
	Block64Kb uint32 = 1 << (16 + iota*2)
	Block256Kb
	Block1Mb
	Block4Mb
	Block8Mb = 2 * Block4Mb
)

var (
	blockPool64K  = sync.Pool{New: func() interface{} { return &[Block64Kb]byte{} }}
	blockPool256K = sync.Pool{New: func() interface{} { return &[Block256Kb]byte{} }}
	blockPool1M   = sync.Pool{New: func() interface{} { return &[Block1Mb]byte{} }}
	blockPool4M   = sync.Pool{New: func() interface{} { return &[Block4Mb]byte{} }}
	blockPool8M   = sync.Pool{New: func() interface{} { return &[Block8Mb]byte{} }}
)

func Index(b uint32) BlockSizeIndex {
	switch b {
	case Block64Kb:
		return 4
	case Block256Kb:
		return 5
	case Block1Mb:
		return 6
	case Block4Mb:
		return 7
	case Block8Mb: // only valid in legacy mode
		return 3
	}
	return 0
}

func IsValid(b uint32) bool {
	return Index(b) > 0
}

type BlockSizeIndex uint8

func (b BlockSizeIndex) IsValid() bool {
	switch b {
	case 4, 5, 6, 7:
		return true
	}
	return false
}

func (b BlockSizeIndex) Get() []byte {
	switch b {
	case 4:
		return blockPool64K.Get().(*[Block64Kb]byte)[:]
	case 5:
		return blockPool256K.Get().(*[Block256Kb]byte)[:]
	case 6:
		return blockPool1M.Get().(*[Block1Mb]byte)[:]
	case 7:
		return blockPool4M.Get().(*[Block4Mb]byte)[:]
	case 3:
		return blockPool8M.Get().(*[Block8Mb]byte)[:]
	default:
		panic(fmt.Errorf("invalid block index %d", b))
	}
}

func Put(buf []byte) {
	// Safeguard: do not allow invalid buffers.
	switch c := cap(buf); uint32(c) {
	case Block64Kb:
		blockPool64K.Put((*[Block64Kb]byte)(buf[:c]))
	case Block256Kb:
		blockPool256K.Put((*[Block256Kb]byte)(buf[:c]))
	case Block1Mb:
		blockPool1M.Put((*[Block1Mb]byte)(buf[:c]))
	case Block4Mb:
		blockPool4M.Put((*[Block4Mb]byte)(buf[:c]))
	case Block8Mb:
		blockPool8M.Put((*[Block8Mb]byte)(buf[:c]))
	default:
		panic(fmt.Errorf("invalid block size %d", c))
	}
}

type CompressionLevel uint32

const Fast CompressionLevel = 0
