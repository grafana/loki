package kgo

import (
	"context"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/twmb/franz-go/pkg/kmsg"
)

////////////////////////////////////////////////////////////////
// NOTE:                                                      //
// NOTE: Make sure new hooks are checked in implementsAnyPool //
// NOTE:                                                      //
////////////////////////////////////////////////////////////////

// Pool is a memory pool to be used where relevant.
//
// The base Pool interface is meaningless, but wherever a type can be pooled in
// kgo, the client checks if your pool implements an appropriate pool interface.
// If so, the pool is received from (Get), and when the data is done being used,
// the pool is put back into (Put).
//
// All pool interfaces in this package have Pool in the name. Pools must be safe
// for concurrent use.
type Pool any

type pools []Pool

func (ps pools) each(fn func(Pool) bool) {
	for _, p := range ps {
		found := fn(p)
		if found {
			return
		}
	}
}

// PoolDecompressBytes is a pool that returns a slice that decompressed data
// will be decoded into.
type PoolDecompressBytes interface {
	// GetDecompressBytes returns a slice to decompress into. This
	// interface is given the compressed data and the codec that will be
	// used for decompressing.
	//
	// For many decompression algorithms, it is not possible to accurately
	// know the size that data will be once decompressed. You can guess a
	// multiplier, or you can use rolling statistics via FetchBatchMetrics.
	// If the slice is not large enough, it is grown and the grown slice is
	// put back into the pool (not the original slice, since decompression
	// libraries often internally discard the original slice).
	//
	// NOTE: If you provide your own Decompressor, this function will not
	// be called. However, PutDecompressBytes will still be called with the
	// slice that is returned from Decompress if Decompress was called. It
	// is expected that you use your own GetDecompressBytes in your own
	// Decompress if you provide this pool, however, if you do not, you'll
	// just have extra data slices put back into your pool that you never
	// created.
	GetDecompressBytes(compressed []byte, codec CompressionCodecType) []byte
	// PutDecompressBytes puts a slice of that was used for decompression
	// back into the pool. The slice is zeroed before it is put back.
	PutDecompressBytes([]byte)
}

// PoolKRecords is a pool that returns a slice that raw kmsg.Record's are
// decoded into.
type PoolKRecords interface {
	// GetKRecords returns a slice with capacity n.
	GetKRecords(n int) []kmsg.Record
	// PutKRecords puts a slice back into the pool.
	PutKRecords([]kmsg.Record)
}

// PoolRecords is a pool that returns a slice of Record's.
type PoolRecords interface {
	// GetRecords returns a slice with capacity n.
	GetRecords(n int) []Record
	// PutRecords puts a slice back into the pool.
	PutRecords([]Record)
}

func strp(s string) *string { return &s }

var ctxRecRecycle = strp("rec-recycle")

const recSize = unsafe.Sizeof(Record{})

func recordPoolsCtx(pools []Pool, decompressBytes []byte, recs []Record) (*recordPools, context.Context) {
	if len(recs) == 0 { // ...just in case
		return nil, nil
	}
	p := &recordPools{
		pools:           pools,
		decompressBytes: decompressBytes,
		recs:            recs,
	}
	return p, context.WithValue(context.Background(), ctxRecRecycle, p)
}

// Recycle "recycles" this record if it was taken from a pool, and frees its
// attachment to any underlying pooled slices. If the pooled slice no longer
// has any records attached, the slices are put back into their pools.
//
// This method is only relevant if you are using the [WithPools] option.
//
// NOTE: It is invalid to continue using the record after calling recycle;
// doing so may result in corruption and data races. If you use
// PoolDecompressBytes, you cannot continue to use a shallow copy of any
// fields, you must clone them!
func (r *Record) Recycle() {
	if r.Context == nil {
		return
	}
	v := r.Context.Value(ctxRecRecycle)
	if v == nil {
		return
	}
	*r = Record{} // prevent the Record from hanging onto anything; *kmsg.Record is already cleared during processing
	ps := v.(*recordPools)

	r0 := &ps.recs[0]

	idx := int((uintptr(unsafe.Pointer(r)) - uintptr(unsafe.Pointer(r0))) / recSize) //nolint:gosec // unsafe rule (3) roughly (we don't need to convert back)
	if idx < 0 || idx >= len(ps.recs) {
		panic(fmt.Sprintf("recycling a record and the index %v is invalid (max pool len %v)", idx, len(ps.recs)))
	}
	if &ps.recs[idx] != r {
		panic("sanity check pointer comparison index for recycling record is not the same!")
	}

	rem := ps.n.Add(-1)
	if rem > 0 {
		return
	}

	// Reset the length of slices to max to ensure we zero things and so
	// that users can avoid this resetting.
	ps.decompressBytes = ps.decompressBytes[:cap(ps.decompressBytes)]
	ps.recs = ps.recs[:cap(ps.recs)]

	for i := range ps.decompressBytes {
		ps.decompressBytes[i] = 0 // this loop is optimized in the compiler to a memclr: https://go.dev/wiki/CompilerOptimizations#optimized-memclr
	}

	// We use len checks below because we should NOT put empty slices back
	// into the pool (especially since the decompressBytes may never have
	// been created!).
	pools(ps.pools).each(func(p Pool) bool {
		if pdecom, ok := p.(PoolDecompressBytes); ok {
			if len(ps.decompressBytes) > 0 {
				pdecom.PutDecompressBytes(ps.decompressBytes)
				ps.decompressBytes = nil
			}
		}
		if precs, ok := p.(PoolRecords); ok {
			if len(ps.recs) > 0 {
				precs.PutRecords(ps.recs)
				ps.recs = nil
			}
		}
		return false // always loop through all pools; we put each slice into a max of one pool
	})
}

type recordPools struct {
	pools []Pool
	n     atomic.Int64

	decompressBytes []byte
	recs            []Record
}

// implementsAnyPool will check the incoming Pool for any Pool implementation
func implementsAnyPool(p Pool) bool {
	switch p.(type) {
	case /*PoolRequestBuffer,*/
		PoolDecompressBytes,
		PoolKRecords,
		PoolRecords:
		return true
	}
	return false
}

func ensureLen[S ~[]E, E any](s S, n int) S {
	s = s[:cap(s)]
	if len(s) >= n {
		return s[:n]
	}
	return append(s, make([]E, n-len(s))...)
}
