// +build darwin

package memory

import (
	"os/exec"
	"regexp"
	"strconv"
)

func sysTotalMemory() uint64 {
	s, err := sysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return s
}

func sysFreeMemory() uint64 {
	cmd := exec.Command("vm_stat")
	outBytes, err := cmd.Output()
	if err != nil {
		return 0
	}

	rePageSize := regexp.MustCompile("page size of ([0-9]*) bytes")
	reFreePages := regexp.MustCompile("Pages free: *([0-9]*)\\.")

	// default: page size of 4096 bytes
	matches := rePageSize.FindSubmatchIndex(outBytes)
	pageSize := uint64(4096)
	if len(matches) == 4 {
		pageSize, err = strconv.ParseUint(string(outBytes[matches[2]:matches[3]]), 10, 64)
		if err != nil {
			return 0
		}
	}

	// ex: Pages free:                             1126961.
	matches = reFreePages.FindSubmatchIndex(outBytes)
	freePages := uint64(0)
	if len(matches) == 4 {
		freePages, err = strconv.ParseUint(string(outBytes[matches[2]:matches[3]]), 10, 64)
		if err != nil {
			return 0
		}
	}
	return freePages * pageSize
}
