//go:build go1.19
// +build go1.19

package kgo

import "sync/atomic"

type (
	atomicBool struct{ atomic.Bool }
	atomicI32  struct{ atomic.Int32 }
	atomicU32  struct{ atomic.Uint32 }
	atomicI64  struct{ atomic.Int64 }
	atomicU64  struct{ atomic.Uint64 }
)
