package ingester

import (
	"sync"
	"unsafe"

	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/util"
)

const (
	cacheLineSize = 64
)

// Avoid false sharing when using array of mutexes.
type paddedMutex struct {
	sync.Mutex
	//nolint:structcheck,unused
	pad [cacheLineSize - unsafe.Sizeof(sync.Mutex{})]byte
}

// fingerprintLocker allows locking individual fingerprints. To limit the number
// of mutexes needed for that, only a fixed number of mutexes are
// allocated. Fingerprints to be locked are assigned to those pre-allocated
// mutexes by their value. Collisions are not detected. If two fingerprints get
// assigned to the same mutex, only one of them can be locked at the same
// time. As long as the number of pre-allocated mutexes is much larger than the
// number of goroutines requiring a fingerprint lock concurrently, the loss in
// efficiency is small. However, a goroutine must never lock more than one
// fingerprint at the same time. (In that case a collision would try to acquire
// the same mutex twice).
type fingerprintLocker struct {
	fpMtxs    []paddedMutex
	numFpMtxs uint32
}

// newFingerprintLocker returns a new fingerprintLocker ready for use.  At least
// 1024 preallocated mutexes are used, even if preallocatedMutexes is lower.
func newFingerprintLocker(preallocatedMutexes int) *fingerprintLocker {
	if preallocatedMutexes < 1024 {
		preallocatedMutexes = 1024
	}
	return &fingerprintLocker{
		make([]paddedMutex, preallocatedMutexes),
		uint32(preallocatedMutexes),
	}
}

// Lock locks the given fingerprint.
func (l *fingerprintLocker) Lock(fp model.Fingerprint) {
	l.fpMtxs[util.HashFP(fp)%l.numFpMtxs].Lock()
}

// Unlock unlocks the given fingerprint.
func (l *fingerprintLocker) Unlock(fp model.Fingerprint) {
	l.fpMtxs[util.HashFP(fp)%l.numFpMtxs].Unlock()
}
