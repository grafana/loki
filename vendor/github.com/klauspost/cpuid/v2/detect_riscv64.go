// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

package cpuid

func getVectorLength() (vl, pl uint64) { return 0, 0 }

func initCPU() {
	cpuid = func(uint32) (a, b, c, d uint32) { return 0, 0, 0, 0 }
	cpuidex = func(x, y uint32) (a, b, c, d uint32) { return 0, 0, 0, 0 }
	xgetbv = func(uint32) (a, b uint32) { return 0, 0 }
	rdtscpAsm = func() (a, b, c, d uint32) { return 0, 0, 0, 0 }
}

func addInfo(c *CPUInfo, safe bool) {
	detectOS(c)
}
