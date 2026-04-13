//go:build linux
// +build linux

package memlimit

// FromCgroup retrieves the memory limit from the cgroup.
func FromCgroup() (uint64, error) {
	return fromCgroup(detectCgroupVersion)
}

// FromCgroupV1 retrieves the memory limit from the cgroup v1 controller.
// After v1.0.0, this function could be removed and FromCgroup should be used instead.
func FromCgroupV1() (uint64, error) {
	return fromCgroup(func(_ []mountInfo) (bool, bool) {
		return true, false
	})
}

// FromCgroupHybrid retrieves the memory limit from the cgroup v2 and v1 controller sequentially,
// basically, it is equivalent to FromCgroup.
// After v1.0.0, this function could be removed and FromCgroup should be used instead.
func FromCgroupHybrid() (uint64, error) {
	return FromCgroup()
}

// FromCgroupV2 retrieves the memory limit from the cgroup v2 controller.
// After v1.0.0, this function could be removed and FromCgroup should be used instead.
func FromCgroupV2() (uint64, error) {
	return fromCgroup(func(_ []mountInfo) (bool, bool) {
		return false, true
	})
}
