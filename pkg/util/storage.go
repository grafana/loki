package util

import (
	"syscall"
)

// Creating structure for DiskStatus
type DiskStatus struct {
	All         uint64  `json:"All"`
	Used        uint64  `json:"Used"`
	Free        uint64  `json:"Free"`
	UsedPercent float64 `json:"UsedPercent"`
}

// Function to get
// disk usage of path/disk
func DiskUsage(path string) (disk DiskStatus, err error) {
	fs := syscall.Statfs_t{}
	error := syscall.Statfs(path, &fs)
	if error != nil {
		return disk, error
	}
	disk.All = fs.Blocks * uint64(fs.Bsize)
	disk.Free = fs.Bfree * uint64(fs.Bsize)
	disk.Used = disk.All - disk.Free
	disk.UsedPercent = (float64(disk.Used) / float64(disk.All)) * float64(100)
	return disk, nil
}
