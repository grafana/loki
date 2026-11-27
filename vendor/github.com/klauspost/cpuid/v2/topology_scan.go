// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

package cpuid

import (
	"errors"

	"github.com/klauspost/cpuid/v2/topology"
)

// ErrTopologyScanUnavailable is returned by ScanTopology when a per-core scan
// is not possible on the current platform, or CPU affinity could not be set.
// The returned topology is then the extrapolated snapshot from CPU.Topology.
var ErrTopologyScanUnavailable = errors.New("cpuid: per-core topology scan unavailable; returning extrapolated snapshot")

// ScanTopology returns the exact CPU topology by reading CPUID on every online
// logical processor.
//
// To sample each processor it locks the calling goroutine to an OS thread and
// pins that thread to each CPU in turn, so it has transient scheduling side
// effects. Call it explicitly when you need a precise tree (real
// efficiency/performance core mapping, CCD membership); it is not run
// automatically.
//
// It is supported on linux/amd64 and windows/amd64. On any other platform, or
// if affinity cannot be set (e.g. a restrictive cgroup cpuset), it returns
// CPU.Topology() together with ErrTopologyScanUnavailable.
func ScanTopology() (*topology.System, error) {
	cpus, err := scanLogicalCPUs()
	if err != nil {
		return CPU.Topology(), err
	}
	return assembleTopology(cpus), nil
}
