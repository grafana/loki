// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

//go:build linux && amd64 && !noasm && !appengine && !gccgo

package cpuid

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// scanLogicalCPUs samples CPUID on every CPU in the calling thread's affinity
// mask by pinning the thread to each in turn. sched_setaffinity migrates the
// thread synchronously when its current CPU leaves the mask, so CPUID reflects
// the intended processor once the call returns.
func scanLogicalCPUs() ([]logicalCPU, error) {
	var orig unix.CPUSet
	if err := unix.SchedGetaffinity(0, &orig); err != nil {
		return nil, ErrTopologyScanUnavailable
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer unix.SchedSetaffinity(0, &orig)

	var cpus []logicalCPU
	for i := range len(orig) * 64 {
		if !orig.IsSet(i) {
			continue
		}
		var set unix.CPUSet
		set.Set(i)
		if unix.SchedSetaffinity(0, &set) != nil {
			continue
		}
		cpus = append(cpus, parseLogicalCPU())
	}
	if len(cpus) == 0 {
		return nil, ErrTopologyScanUnavailable
	}
	return cpus, nil
}
