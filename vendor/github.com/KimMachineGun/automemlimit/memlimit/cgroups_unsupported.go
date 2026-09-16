//go:build !linux
// +build !linux

package memlimit

func FromCgroup() (uint64, error) {
	return 0, ErrCgroupsNotSupported
}

func FromCgroupV1() (uint64, error) {
	return 0, ErrCgroupsNotSupported
}

func FromCgroupHybrid() (uint64, error) {
	return 0, ErrCgroupsNotSupported
}

func FromCgroupV2() (uint64, error) {
	return 0, ErrCgroupsNotSupported
}
