//go:build linux

package memlimit

// FromCgroup returns the memory limit for the current process's cgroup.
func FromCgroup() (uint64, error) {
	return fromCgroup(procSelfMountInfoPath, procSelfCgroupPath)
}
