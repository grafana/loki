// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

//go:build riscv64 && !linux

package cpuid

import "runtime"

func detectOS(c *CPUInfo) bool {
	c.PhysicalCores = runtime.NumCPU()
	c.ThreadsPerCore = 1
	c.LogicalCores = c.PhysicalCores
	return false
}
