//go:build !linux

package memlimit

// FromCgroup returns the memory limit for the current process's cgroup.
// On non-Linux platforms, it always returns [ErrCgroupsNotSupported].
func FromCgroup() (uint64, error) {
	return 0, ErrCgroupsNotSupported
}
