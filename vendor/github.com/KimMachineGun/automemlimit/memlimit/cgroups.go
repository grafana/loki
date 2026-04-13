package memlimit

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

var (
	// ErrNoCgroup is returned when the process is not in cgroup.
	ErrNoCgroup = errors.New("process is not in cgroup")
	// ErrCgroupsNotSupported is returned when the system does not support cgroups.
	ErrCgroupsNotSupported = errors.New("cgroups is not supported on this system")
)

// fromCgroup retrieves the memory limit from the cgroup.
// The versionDetector function is used to detect the cgroup version from the mountinfo.
func fromCgroup(versionDetector func(mis []mountInfo) (bool, bool)) (uint64, error) {
	mf, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return 0, fmt.Errorf("failed to open /proc/self/mountinfo: %w", err)
	}
	defer mf.Close()

	mis, err := parseMountInfo(mf)
	if err != nil {
		return 0, fmt.Errorf("failed to parse mountinfo: %w", err)
	}

	v1, v2 := versionDetector(mis)
	if !(v1 || v2) {
		return 0, ErrNoCgroup
	}

	cf, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return 0, fmt.Errorf("failed to open /proc/self/cgroup: %w", err)
	}
	defer cf.Close()

	chs, err := parseCgroupFile(cf)
	if err != nil {
		return 0, fmt.Errorf("failed to parse cgroup file: %w", err)
	}

	if v2 {
		limit, err := getMemoryLimitV2(chs, mis)
		if err == nil {
			return limit, nil
		} else if !v1 {
			return 0, err
		}
	}

	return getMemoryLimitV1(chs, mis)
}

// detectCgroupVersion detects the cgroup version from the mountinfo.
func detectCgroupVersion(mis []mountInfo) (bool, bool) {
	var v1, v2 bool
	for _, mi := range mis {
		switch mi.FilesystemType {
		case "cgroup":
			v1 = true
		case "cgroup2":
			v2 = true
		}
	}
	return v1, v2
}

// getMemoryLimitV2 retrieves the memory limit from the cgroup v2 controller.
func getMemoryLimitV2(chs []cgroupHierarchy, mis []mountInfo) (uint64, error) {
	// find the cgroup v2 path for the memory controller.
	// in cgroup v2, the paths are unified and the controller list is empty.
	idx := slices.IndexFunc(chs, func(ch cgroupHierarchy) bool {
		return ch.HierarchyID == "0" && ch.ControllerList == ""
	})
	if idx == -1 {
		return 0, errors.New("cgroup v2 path not found")
	}
	relPath := chs[idx].CgroupPath

	// find the mountpoint for the cgroup v2 controller.
	idx = slices.IndexFunc(mis, func(mi mountInfo) bool {
		return mi.FilesystemType == "cgroup2"
	})
	if idx == -1 {
		return 0, errors.New("cgroup v2 mountpoint not found")
	}
	root, mountPoint := mis[idx].Root, mis[idx].MountPoint

	// resolve the actual cgroup path
	cgroupPath, err := resolveCgroupPath(mountPoint, root, relPath)
	if err != nil {
		return 0, err
	}

	// retrieve the memory limit from the memory.max recursively.
	return walkCgroupV2Hierarchy(cgroupPath, mountPoint)
}

// readMemoryLimitV2FromPath reads the memory limit for cgroup v2 from the given path.
// this function expects the path to be memory.max file.
func readMemoryLimitV2FromPath(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, ErrNoLimit
		}
		return 0, fmt.Errorf("failed to read memory.max: %w", err)
	}

	slimit := strings.TrimSpace(string(b))
	if slimit == "max" {
		return 0, ErrNoLimit
	}

	limit, err := strconv.ParseUint(slimit, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory.max value: %w", err)
	}

	return limit, nil
}

// walkCgroupV2Hierarchy walks up the cgroup v2 hierarchy to find the most restrictive memory limit.
func walkCgroupV2Hierarchy(cgroupPath, mountPoint string) (uint64, error) {
	var (
		found              = false
		minLimit    uint64 = math.MaxUint64
		currentPath        = cgroupPath
	)
	for {
		limit, err := readMemoryLimitV2FromPath(filepath.Join(currentPath, "memory.max"))
		if err != nil && !errors.Is(err, ErrNoLimit) {
			return 0, err
		} else if err == nil {
			found = true
			minLimit = min(minLimit, limit)
		}

		if currentPath == mountPoint {
			break
		}

		parent := filepath.Dir(currentPath)
		if parent == currentPath {
			break
		}
		currentPath = parent
	}
	if !found {
		return 0, ErrNoLimit
	}

	return minLimit, nil
}

// getMemoryLimitV1 retrieves the memory limit from the cgroup v1 controller.
func getMemoryLimitV1(chs []cgroupHierarchy, mis []mountInfo) (uint64, error) {
	// find the cgroup v1 path for the memory controller.
	idx := slices.IndexFunc(chs, func(ch cgroupHierarchy) bool {
		return slices.Contains(strings.Split(ch.ControllerList, ","), "memory")
	})
	if idx == -1 {
		return 0, errors.New("cgroup v1 path for memory controller not found")
	}
	relPath := chs[idx].CgroupPath

	// find the mountpoint for the cgroup v1 controller.
	idx = slices.IndexFunc(mis, func(mi mountInfo) bool {
		return mi.FilesystemType == "cgroup" && slices.Contains(strings.Split(mi.SuperOptions, ","), "memory")
	})
	if idx == -1 {
		return 0, errors.New("cgroup v1 mountpoint for memory controller not found")
	}
	root, mountPoint := mis[idx].Root, mis[idx].MountPoint

	// resolve the actual cgroup path
	cgroupPath, err := resolveCgroupPath(mountPoint, root, relPath)
	if err != nil {
		return 0, err
	}

	// retrieve the memory limit from the memory.stat and memory.limit_in_bytes files.
	return readMemoryLimitV1FromPath(cgroupPath)
}

// getCgroupV1NoLimit returns the maximum value that is used to represent no limit in cgroup v1.
// the max memory limit is max int64, but it should be multiple of the page size.
func getCgroupV1NoLimit() uint64 {
	ps := uint64(os.Getpagesize())
	return math.MaxInt64 / ps * ps
}

// readMemoryLimitV1FromPath reads the memory limit for cgroup v1 from the given path.
// this function expects the path to be the cgroup directory.
func readMemoryLimitV1FromPath(cgroupPath string) (uint64, error) {
	// read hierarchical_memory_limit and memory.limit_in_bytes files.
	// but if hierarchical_memory_limit is not available, then use the max value as a fallback.
	hml, err := readHierarchicalMemoryLimit(filepath.Join(cgroupPath, "memory.stat"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("failed to read hierarchical_memory_limit: %w", err)
	} else if hml == 0 {
		hml = math.MaxUint64
	}

	// read memory.limit_in_bytes file.
	b, err := os.ReadFile(filepath.Join(cgroupPath, "memory.limit_in_bytes"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("failed to read memory.limit_in_bytes: %w", err)
	}
	lib, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory.limit_in_bytes value: %w", err)
	} else if lib == 0 {
		hml = math.MaxUint64
	}

	// use the minimum value between hierarchical_memory_limit and memory.limit_in_bytes.
	// if the limit is the maximum value, then it is considered as no limit.
	limit := min(hml, lib)
	if limit >= getCgroupV1NoLimit() {
		return 0, ErrNoLimit
	}

	return limit, nil
}

// readHierarchicalMemoryLimit extracts hierarchical_memory_limit from memory.stat.
// this function expects the path to be memory.stat file.
func readHierarchicalMemoryLimit(path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		fields := strings.Split(line, " ")
		if len(fields) < 2 {
			return 0, fmt.Errorf("failed to parse memory.stat %q: not enough fields", line)
		}

		if fields[0] == "hierarchical_memory_limit" {
			if len(fields) > 2 {
				return 0, fmt.Errorf("failed to parse memory.stat %q: too many fields for hierarchical_memory_limit", line)
			}
			return strconv.ParseUint(fields[1], 10, 64)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return 0, nil
}

// https://www.man7.org/linux/man-pages/man5/proc_pid_mountinfo.5.html
// 731 771 0:59 /sysrq-trigger /proc/sysrq-trigger ro,nosuid,nodev,noexec,relatime - proc proc rw
//
// 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
// (1)(2)(3)   (4)   (5)      (6)      (7)   (8) (9)   (10)         (11)
//
// (1)  mount ID: a unique ID for the mount (may be reused after umount(2)).
// (2)  parent ID: the ID of the parent mount (or of self for the root of this mount namespace's mount tree).
// (3)  major:minor: the value of st_dev for files on this filesystem (see stat(2)).
// (4)  root: the pathname of the directory in the filesystem which forms the root of this mount.
// (5)  mount point: the pathname of the mount point relative to the process's root directory.
// (6)  mount options: per-mount options (see mount(2)).
// (7)  optional fields: zero or more fields of the form "tag[:value]"; see below.
// (8)  separator: the end of the optional fields is marked by a single hyphen.
// (9)  filesystem type: the filesystem type in the form "type[.subtype]".
// (10) mount source: filesystem-specific information or "none".
// (11) super options: per-superblock options (see mount(2)).
type mountInfo struct {
	Root           string
	MountPoint     string
	FilesystemType string
	SuperOptions   string
}

// parseMountInfoLine parses a line from the mountinfo file.
func parseMountInfoLine(line string) (mountInfo, error) {
	if line == "" {
		return mountInfo{}, errors.New("empty line")
	}

	fieldss := strings.SplitN(line, " - ", 2)
	if len(fieldss) != 2 {
		return mountInfo{}, fmt.Errorf("invalid separator")
	}

	fields1 := strings.SplitN(fieldss[0], " ", 7)
	if len(fields1) < 6 {
		return mountInfo{}, fmt.Errorf("not enough fields before separator: %v", fields1)
	} else if len(fields1) == 6 {
		fields1 = append(fields1, "")
	}

	fields2 := strings.SplitN(fieldss[1], " ", 3)
	if len(fields2) < 3 {
		return mountInfo{}, fmt.Errorf("not enough fields after separator: %v", fields2)
	}

	return mountInfo{
		Root:           fields1[3],
		MountPoint:     fields1[4],
		FilesystemType: fields2[0],
		SuperOptions:   fields2[2],
	}, nil
}

// parseMountInfo parses the mountinfo file.
func parseMountInfo(r io.Reader) ([]mountInfo, error) {
	var (
		s   = bufio.NewScanner(r)
		mis []mountInfo
	)
	for s.Scan() {
		line := s.Text()

		mi, err := parseMountInfoLine(line)
		if err != nil {
			return nil, fmt.Errorf("failed to parse mountinfo file %q: %w", line, err)
		}

		mis = append(mis, mi)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	return mis, nil
}

// https://www.man7.org/linux/man-pages/man7/cgroups.7.html
//
// 5:cpuacct,cpu,cpuset:/daemons
// (1)       (2)           (3)
//
// (1) hierarchy ID:
//
//	cgroups version 1 hierarchies, this field
//	contains a unique hierarchy ID number that can be
//	matched to a hierarchy ID in /proc/cgroups.  For the
//	cgroups version 2 hierarchy, this field contains the
//	value 0.
//
// (2) controller list:
//
//	For cgroups version 1 hierarchies, this field
//	contains a comma-separated list of the controllers
//	bound to the hierarchy.  For the cgroups version 2
//	hierarchy, this field is empty.
//
// (3) cgroup path:
//
//	This field contains the pathname of the control group
//	in the hierarchy to which the process belongs.  This
//	pathname is relative to the mount point of the
//	hierarchy.
type cgroupHierarchy struct {
	HierarchyID    string
	ControllerList string
	CgroupPath     string
}

// parseCgroupHierarchyLine parses a line from the cgroup file.
func parseCgroupHierarchyLine(line string) (cgroupHierarchy, error) {
	if line == "" {
		return cgroupHierarchy{}, errors.New("empty line")
	}

	fields := strings.Split(line, ":")
	if len(fields) < 3 {
		return cgroupHierarchy{}, fmt.Errorf("not enough fields: %v", fields)
	} else if len(fields) > 3 {
		return cgroupHierarchy{}, fmt.Errorf("too many fields: %v", fields)
	}

	return cgroupHierarchy{
		HierarchyID:    fields[0],
		ControllerList: fields[1],
		CgroupPath:     fields[2],
	}, nil
}

// parseCgroupFile parses the cgroup file.
func parseCgroupFile(r io.Reader) ([]cgroupHierarchy, error) {
	var (
		s   = bufio.NewScanner(r)
		chs []cgroupHierarchy
	)
	for s.Scan() {
		line := s.Text()

		ch, err := parseCgroupHierarchyLine(line)
		if err != nil {
			return nil, fmt.Errorf("failed to parse cgroup file %q: %w", line, err)
		}

		chs = append(chs, ch)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	return chs, nil
}

// resolveCgroupPath resolves the actual cgroup path from the mountpoint, root, and cgroupRelPath.
func resolveCgroupPath(mountpoint, root, cgroupRelPath string) (string, error) {
	rel, err := filepath.Rel(root, cgroupRelPath)
	if err != nil {
		return "", err
	}

	// if the relative path is ".", then the cgroupRelPath is the root itself.
	if rel == "." {
		return mountpoint, nil
	}

	// if the relative path starts with "..", then it is outside the root.
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid cgroup path: %s is not under root %s", cgroupRelPath, root)
	}

	return filepath.Join(mountpoint, rel), nil
}
