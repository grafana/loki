// SPDX-License-Identifier: BSD-3-Clause
//go:build aix

package mem

import (
	"context"
)

func VirtualMemory() (*VirtualMemoryStat, error) {
	return VirtualMemoryWithContext(context.Background())
}

func SwapMemory() (*SwapMemoryStat, error) {
	return SwapMemoryWithContext(context.Background())
}
