//go:build go1.27
// +build go1.27

package pprof

import _ "unsafe"

// MemProfileRecord mirrors internal/profilerecord.MemProfileRecord layout
// for Go 1.27+. The runtime CL "remove redundant fields from memory profile
// records" replaced AllocBytes/FreeBytes with a single ObjectSize.
// The runtime writes into these via //go:linkname to pprof_memProfileInternal,
// so the field layout MUST match the runtime's definition exactly.
type MemProfileRecord struct {
	ObjectSize                int64
	AllocObjects, FreeObjects int64
	Stack                     []uintptr
}

func (r *MemProfileRecord) InUseObjects() int64 { return r.AllocObjects - r.FreeObjects }

type BlockProfileRecord struct {
	Count  int64
	Cycles int64
	Stack  []uintptr
}

func memRecordStack(r *MemProfileRecord) []uintptr     { return r.Stack }
func blockRecordStack(r *BlockProfileRecord) []uintptr { return r.Stack }

func memRecordIsFresh(r *MemProfileRecord) bool {
	return r.AllocObjects == 0 && r.FreeObjects == 0
}

func memRecordBlockSize(r *MemProfileRecord) int64 { return r.ObjectSize }

//go:linkname pprof_memProfileInternal runtime.pprof_memProfileInternal
func pprof_memProfileInternal(p []MemProfileRecord, inuseZero bool) (n int, ok bool)

//go:linkname pprof_blockProfileInternal runtime.pprof_blockProfileInternal
func pprof_blockProfileInternal(p []BlockProfileRecord) (n int, ok bool)

//go:linkname pprof_mutexProfileInternal runtime.pprof_mutexProfileInternal
func pprof_mutexProfileInternal(p []BlockProfileRecord) (n int, ok bool)

func MemProfile(inuseZero bool) []MemProfileRecord {
	var p []MemProfileRecord
	n, _ := pprof_memProfileInternal(nil, inuseZero)
	for {
		p = make([]MemProfileRecord, n+50)
		var ok bool
		n, ok = pprof_memProfileInternal(p, inuseZero)
		if ok {
			return p[:n]
		}
	}
}

func BlockProfile() []BlockProfileRecord { return fetchBlockLike(pprof_blockProfileInternal) }
func MutexProfile() []BlockProfileRecord { return fetchBlockLike(pprof_mutexProfileInternal) }

func fetchBlockLike(f func([]BlockProfileRecord) (int, bool)) []BlockProfileRecord {
	var p []BlockProfileRecord
	n, _ := f(nil)
	for {
		p = make([]BlockProfileRecord, n+50)
		var ok bool
		n, ok = f(p)
		if ok {
			return p[:n]
		}
	}
}
