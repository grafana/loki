//go:build !go1.19
// +build !go1.19

package kgo

import "sync/atomic"

type atomicBool uint32

func (b *atomicBool) Store(v bool) {
	if v {
		atomic.StoreUint32((*uint32)(b), 1)
	} else {
		atomic.StoreUint32((*uint32)(b), 0)
	}
}

func (b *atomicBool) Load() bool { return atomic.LoadUint32((*uint32)(b)) == 1 }

func (b *atomicBool) Swap(v bool) bool {
	var swap uint32
	if v {
		swap = 1
	}
	return atomic.SwapUint32((*uint32)(b), swap) == 1
}

type atomicI32 int32

func (v *atomicI32) Add(s int32) int32  { return atomic.AddInt32((*int32)(v), s) }
func (v *atomicI32) Store(s int32)      { atomic.StoreInt32((*int32)(v), s) }
func (v *atomicI32) Load() int32        { return atomic.LoadInt32((*int32)(v)) }
func (v *atomicI32) Swap(s int32) int32 { return atomic.SwapInt32((*int32)(v), s) }

type atomicU32 uint32

func (v *atomicU32) Add(s uint32) uint32  { return atomic.AddUint32((*uint32)(v), s) }
func (v *atomicU32) Store(s uint32)       { atomic.StoreUint32((*uint32)(v), s) }
func (v *atomicU32) Load() uint32         { return atomic.LoadUint32((*uint32)(v)) }
func (v *atomicU32) Swap(s uint32) uint32 { return atomic.SwapUint32((*uint32)(v), s) }
func (v *atomicU32) CompareAndSwap(old, new uint32) bool {
	return atomic.CompareAndSwapUint32((*uint32)(v), old, new)
}

type atomicI64 int64

func (v *atomicI64) Add(s int64) int64  { return atomic.AddInt64((*int64)(v), s) }
func (v *atomicI64) Store(s int64)      { atomic.StoreInt64((*int64)(v), s) }
func (v *atomicI64) Load() int64        { return atomic.LoadInt64((*int64)(v)) }
func (v *atomicI64) Swap(s int64) int64 { return atomic.SwapInt64((*int64)(v), s) }

type atomicU64 uint64

func (v *atomicU64) Add(s uint64) uint64  { return atomic.AddUint64((*uint64)(v), s) }
func (v *atomicU64) Store(s uint64)       { atomic.StoreUint64((*uint64)(v), s) }
func (v *atomicU64) Load() uint64         { return atomic.LoadUint64((*uint64)(v)) }
func (v *atomicU64) Swap(s uint64) uint64 { return atomic.SwapUint64((*uint64)(v), s) }
