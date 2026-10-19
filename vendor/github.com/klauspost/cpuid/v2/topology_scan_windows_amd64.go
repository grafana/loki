// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

//go:build windows && amd64 && !noasm && !appengine && !gccgo

package cpuid

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentThread           = kernel32.NewProc("GetCurrentThread")
	procGetCurrentProcess          = kernel32.NewProc("GetCurrentProcess")
	procGetProcessAffinityMask     = kernel32.NewProc("GetProcessAffinityMask")
	procSetThreadAffinityMask      = kernel32.NewProc("SetThreadAffinityMask")
	procGetCurrentProcessorNumber  = kernel32.NewProc("GetCurrentProcessorNumber")
	procGetActiveProcessorGroupCnt = kernel32.NewProc("GetActiveProcessorGroupCount")
)

// scanLogicalCPUs samples CPUID on every CPU in the process affinity mask by
// pinning the current thread to each in turn. It covers a single processor
// group (up to 64 logical CPUs); larger systems are not fully enumerated.
func scanLogicalCPUs() ([]logicalCPU, error) {
	// A single process affinity mask only covers one processor group (<=64
	// CPUs). Rather than record a partial tree on a machine with more than one
	// group, bail out and let ScanTopology fall back to the snapshot, whose
	// counts come from the (group-aware) OS totals.
	if n, _, _ := procGetActiveProcessorGroupCnt.Call(); n > 1 {
		return nil, ErrTopologyScanUnavailable
	}

	proc, _, _ := procGetCurrentProcess.Call()
	var procMask, sysMask uintptr
	if ret, _, _ := procGetProcessAffinityMask.Call(proc, uintptr(unsafe.Pointer(&procMask)), uintptr(unsafe.Pointer(&sysMask))); ret == 0 || procMask == 0 {
		return nil, ErrTopologyScanUnavailable
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	th, _, _ := procGetCurrentThread.Call()
	defer procSetThreadAffinityMask.Call(th, procMask)

	var cpus []logicalCPU
	for i := range 64 {
		bit := uintptr(1) << i
		if procMask&bit == 0 {
			continue
		}
		if prev, _, _ := procSetThreadAffinityMask.Call(th, bit); prev == 0 {
			continue
		}
		// SetThreadAffinityMask may defer migration; wait until we are on CPU i.
		// If it never lands there, skip it rather than record another CPU's data.
		migrated := false
		for range 10000 {
			if cur, _, _ := procGetCurrentProcessorNumber.Call(); uint32(cur) == uint32(i) {
				migrated = true
				break
			}
			runtime.Gosched()
		}
		if !migrated {
			continue
		}
		cpus = append(cpus, parseLogicalCPU())
	}
	if len(cpus) == 0 {
		return nil, ErrTopologyScanUnavailable
	}
	return cpus, nil
}
