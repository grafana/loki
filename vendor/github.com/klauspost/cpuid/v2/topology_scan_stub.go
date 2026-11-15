// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

//go:build !amd64 || noasm || appengine || gccgo || (!linux && !windows)

package cpuid

// scanLogicalCPUs is unavailable on this platform; ScanTopology falls back to
// the extrapolated snapshot.
func scanLogicalCPUs() ([]logicalCPU, error) {
	return nil, ErrTopologyScanUnavailable
}
