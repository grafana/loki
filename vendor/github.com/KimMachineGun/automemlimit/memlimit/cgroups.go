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

const (
	procSelfMountInfoPath = "/proc/self/mountinfo"
	procSelfCgroupPath    = "/proc/self/cgroup"
)

var (
	// ErrNoCgroup is returned when the process is not assigned to a cgroup.
	ErrNoCgroup = errors.New("process is not in cgroup")
	// ErrCgroupsNotSupported is returned when the system does not support cgroups.
	ErrCgroupsNotSupported = errors.New("cgroups is not supported on this system")
)

func fromCgroup(mountInfoPath, cgroupPath string) (uint64, error) {
	mf, err := os.Open(mountInfoPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open %s: %w", mountInfoPath, err)
	}
	defer mf.Close()

	mis, err := parseMountInfo(mf)
	if err != nil {
		return 0, fmt.Errorf("failed to parse mountinfo: %w", err)
	}

	cf, err := os.Open(cgroupPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open %s: %w", cgroupPath, err)
	}
	defer cf.Close()

	chs, err := parseCgroupFile(cf)
	if err != nil {
		return 0, fmt.Errorf("failed to parse cgroup file: %w", err)
	}

	controller, err := selectMemoryController(chs, mis)
	if err != nil {
		return 0, err
	}
	if controller.isV2 {
		return getMemoryLimitV2FromControllerPath(controller.path, mis)
	}

	return getMemoryLimitV1FromControllerPath(controller.path, mis)
}

type memoryController struct {
	path string
	isV2 bool
}

// selectMemoryController selects v1 when both v1 and v2 are available.
func selectMemoryController(chs []cgroupHierarchy, mis []mountInfo) (memoryController, error) {
	var (
		v1Path     string
		v2Path     string
		hasV1Entry bool
		hasV2Entry bool
		hasV1Mount bool
		hasV2Mount bool
	)

	for _, ch := range chs {
		if !hasV1Entry && ch.HierarchyID != "0" && slices.Contains(strings.Split(ch.ControllerList, ","), "memory") {
			v1Path = ch.CgroupPath
			hasV1Entry = true
		} else if !hasV2Entry && ch.HierarchyID == "0" && ch.ControllerList == "" {
			v2Path = ch.CgroupPath
			hasV2Entry = true
		}
	}

	for _, mi := range mis {
		if !hasV1Mount && mi.FilesystemType == "cgroup" && slices.Contains(strings.Split(mi.SuperOptions, ","), "memory") {
			hasV1Mount = true
		} else if !hasV2Mount && mi.FilesystemType == "cgroup2" {
			// cgroup v2 uses a unified hierarchy, so the filesystem type is sufficient
			hasV2Mount = true
		}
	}

	switch {
	case hasV1Entry:
		if !hasV1Mount {
			return memoryController{}, errors.New("memory controller found in /proc/self/cgroup but no cgroup v1 memory mount found")
		}
		return memoryController{path: v1Path}, nil
	case hasV2Entry:
		if !hasV2Mount {
			return memoryController{}, errors.New("cgroup v2 hierarchy found in /proc/self/cgroup but no cgroup2 mount found")
		}
		return memoryController{path: v2Path, isV2: true}, nil
	default:
		return memoryController{}, ErrNoCgroup
	}
}

// readMemoryLimitV2FromPath reads a memory limit from a cgroup v2 memory.max file.
// It returns [ErrNoLimit] for "max" and preserves [os.ErrNotExist] for a missing file.
func readMemoryLimitV2FromPath(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, err
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

// walkCgroupV2Hierarchy returns the smallest limit between cgroupPath and mountPoint.
// It skips missing and unlimited values, returning [ErrNoLimit] if no concrete limit remains.
func walkCgroupV2Hierarchy(cgroupPath, mountPoint string) (uint64, error) {
	var (
		found           = false
		minLimit uint64 = math.MaxUint64
	)
	for currentPath := cgroupPath; ; {
		limit, err := readMemoryLimitV2FromPath(filepath.Join(currentPath, "memory.max"))
		if err == nil {
			found = true
			minLimit = min(minLimit, limit)
		} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, ErrNoLimit) {
			return 0, err
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

// getMemoryLimitV2FromControllerPath prefers mounts rooted closest to the cgroup hierarchy root.
func getMemoryLimitV2FromControllerPath(relPath string, mis []mountInfo) (uint64, error) {
	rootClosestMounts, err := getRootClosestMountCandidates(
		relPath,
		mis,
		func(mi mountInfo) bool {
			return mi.FilesystemType == "cgroup2"
		},
	)
	if err != nil {
		return 0, err
	}

	limit, found, err := getMemoryLimitFromMountCandidates(
		rootClosestMounts,
		func(mi mountInfo) (uint64, bool, error) {
			cgroupPath, err := resolveCgroupPath(mi.MountPoint, mi.Root, relPath)
			if err != nil {
				return 0, false, err
			}

			stat, err := os.Stat(cgroupPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return 0, false, nil
				}
				return 0, false, err
			}
			if !stat.IsDir() {
				return 0, false, fmt.Errorf("cgroup v2 path %s is not a directory", cgroupPath)
			}

			limit, err := walkCgroupV2Hierarchy(cgroupPath, mi.MountPoint)
			if err != nil {
				return 0, false, err
			}

			return limit, true, nil
		},
	)
	if err != nil {
		return 0, err
	} else if found {
		return limit, nil
	}

	return 0, errors.New("no usable cgroup v2 memory mount found")
}

// getMemoryLimitV1FromControllerPath prefers mounts rooted closest to the cgroup hierarchy root.
func getMemoryLimitV1FromControllerPath(relPath string, mis []mountInfo) (uint64, error) {
	rootClosestMounts, err := getRootClosestMountCandidates(
		relPath,
		mis,
		func(mi mountInfo) bool {
			return mi.FilesystemType == "cgroup" && slices.Contains(strings.Split(mi.SuperOptions, ","), "memory")
		},
	)
	if err != nil {
		return 0, err
	}

	limit, found, err := getMemoryLimitFromMountCandidates(
		rootClosestMounts,
		func(mi mountInfo) (uint64, bool, error) {
			cgroupPath, err := resolveCgroupPath(mi.MountPoint, mi.Root, relPath)
			if err != nil {
				return 0, false, err
			}

			return readMemoryLimitV1FromPath(cgroupPath)
		},
	)
	if err != nil {
		return 0, err
	} else if found {
		return limit, nil
	}

	return 0, errors.New("no usable cgroup v1 memory mount found")
}

func getRootClosestMountCandidates(
	relPath string,
	mis []mountInfo,
	isCandidate func(mi mountInfo) bool,
) ([]mountInfo, error) {
	var (
		rootClosestMounts []mountInfo
		maxDepth          = -1
	)
	for _, mi := range mis {
		if !isCandidate(mi) {
			continue
		}

		cgroupPath, err := resolveCgroupPath(mi.MountPoint, mi.Root, relPath)
		if err != nil {
			return nil, err
		} else if cgroupPath == "" {
			continue
		}
		if _, err := os.Stat(cgroupPath); errors.Is(err, os.ErrNotExist) {
			continue
		}

		rel, err := filepath.Rel(mi.MountPoint, cgroupPath)
		if err != nil {
			return nil, err
		}

		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}

		switch {
		case depth > maxDepth:
			rootClosestMounts = []mountInfo{mi}
			maxDepth = depth
		case depth == maxDepth:
			rootClosestMounts = append(rootClosestMounts, mi)
		}
	}

	return rootClosestMounts, nil
}

// getLimit may return found=false to skip a candidate or [ErrNoLimit] for no usable limit.
// Permission errors are returned only if no candidate returns a limit or [ErrNoLimit].
// Other errors and conflicting concrete limits are returned.
func getMemoryLimitFromMountCandidates(
	mis []mountInfo,
	getLimit func(mi mountInfo) (uint64, bool, error),
) (uint64, bool, error) {
	var (
		firstErr, permissionErr, conflictErr error
		sawNoLimit, limitFound               bool
		firstLimit                           uint64
	)
	for _, mi := range mis {
		limit, found, err := getLimit(mi)
		if err != nil {
			if errors.Is(err, ErrNoLimit) {
				sawNoLimit = true
			} else if errors.Is(err, os.ErrPermission) {
				if permissionErr == nil {
					permissionErr = err
				}
			} else if firstErr == nil {
				firstErr = err
			}
			continue
		} else if !found {
			continue
		}

		if !limitFound {
			limitFound = true
			firstLimit = limit
		} else if limit != firstLimit && conflictErr == nil {
			conflictErr = fmt.Errorf("conflicting memory limits from cgroup mount candidates: %d and %d", firstLimit, limit)
		}
	}
	if firstErr != nil {
		return 0, false, firstErr
	}
	if conflictErr != nil {
		return 0, false, conflictErr
	}

	if limitFound {
		return firstLimit, true, nil
	}
	if sawNoLimit {
		return 0, false, ErrNoLimit
	}
	if permissionErr != nil {
		return 0, false, permissionErr
	}

	return 0, false, nil
}

// cgroup v1 uses the kernel's maximum page counter value to represent no limit
func isCgroupV1NoLimit(limit uint64) bool {
	pageSize := uint64(os.Getpagesize())
	if limit >= math.MaxInt64/pageSize*pageSize {
		return true
	}

	// strconv.IntSize reflects the Go binary width, not the kernel bitness
	if strconv.IntSize == 32 {
		return limit == math.MaxInt32*pageSize
	}

	return false
}

// readMemoryLimitV1FromPath reads the effective memory limit from a cgroup v1 directory.
// It returns [ErrNoLimit] for a no-limit sentinel.
// The bool reports whether a limit value was found.
func readMemoryLimitV1FromPath(cgroupPath string) (uint64, bool, error) {
	// use math.MaxUint64 as a neutral fallback so memory.limit_in_bytes determines the result
	hml, hmlFound, err := readHierarchicalMemoryLimit(filepath.Join(cgroupPath, "memory.stat"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, false, fmt.Errorf("failed to read hierarchical_memory_limit: %w", err)
	} else if !hmlFound {
		hml = math.MaxUint64
	}

	var libFound bool
	lib, err := readMemoryLimitInBytes(filepath.Join(cgroupPath, "memory.limit_in_bytes"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, false, err
	} else if errors.Is(err, os.ErrNotExist) {
		lib = math.MaxUint64
	} else {
		libFound = true
	}

	if !hmlFound && !libFound {
		return 0, false, nil
	}

	limit := min(hml, lib)
	if isCgroupV1NoLimit(limit) {
		return 0, true, ErrNoLimit
	}

	return limit, true, nil
}

// readHierarchicalMemoryLimit returns false if memory.stat has no hierarchical_memory_limit field.
func readHierarchicalMemoryLimit(path string) (uint64, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "hierarchical_memory_limit" {
			continue
		}
		if len(fields) < 2 {
			return 0, false, fmt.Errorf("failed to parse memory.stat %q: not enough fields", line)
		} else if len(fields) > 2 {
			return 0, false, fmt.Errorf("failed to parse memory.stat %q: too many fields for hierarchical_memory_limit", line)
		}

		limit, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("failed to parse hierarchical_memory_limit value: %w", err)
		}

		return limit, true, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, false, err
	}

	return 0, false, nil
}

func readMemoryLimitInBytes(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read memory.limit_in_bytes: %w", err)
	}

	limit, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory.limit_in_bytes value: %w", err)
	}

	return limit, nil
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
		Root:           unescapeMountInfoPath(fields1[3]),
		MountPoint:     unescapeMountInfoPath(fields1[4]),
		FilesystemType: fields2[0],
		SuperOptions:   fields2[2],
	}, nil
}

// unescapeMountInfoPath decodes path escapes written to /proc/<pid>/mountinfo.
// https://github.com/torvalds/linux/blob/master/fs/proc_namespace.c
func unescapeMountInfoPath(path string) string {
	if strings.IndexByte(path, '\\') == -1 {
		return path
	}

	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); i++ {
		if path[i] == '\\' && i+3 < len(path) {
			switch path[i : i+4] {
			case `\040`:
				b.WriteByte(' ')
				i += 3
				continue
			case `\011`:
				b.WriteByte('\t')
				i += 3
				continue
			case `\012`:
				b.WriteByte('\n')
				i += 3
				continue
			case `\134`:
				b.WriteByte('\\')
				i += 3
				continue
			}
		}

		b.WriteByte(path[i])
	}

	return b.String()
}

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

func parseCgroupHierarchyLine(line string) (cgroupHierarchy, error) {
	if line == "" {
		return cgroupHierarchy{}, errors.New("empty line")
	}

	fields := strings.SplitN(line, ":", 3)
	if len(fields) < 3 {
		return cgroupHierarchy{}, fmt.Errorf("not enough fields: %v", fields)
	}

	return cgroupHierarchy{
		HierarchyID:    fields[0],
		ControllerList: fields[1],
		CgroupPath:     fields[2],
	}, nil
}

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

// resolveCgroupPath maps cgroupRelPath from root into mountPoint.
// It returns an empty path when cgroupRelPath lies outside root.
func resolveCgroupPath(mountPoint, root, cgroupRelPath string) (string, error) {
	if !strings.HasPrefix(root, "/") || !strings.HasPrefix(cgroupRelPath, "/") {
		return "", errors.New("cgroup root and path must be absolute")
	}

	if root == cgroupRelPath {
		return mountPoint, nil
	}

	prefix := root
	if root != "/" {
		prefix += "/"
	}

	rel, ok := strings.CutPrefix(cgroupRelPath, prefix)
	if !ok || !filepath.IsLocal(rel) {
		return "", nil
	}

	return filepath.Join(mountPoint, rel), nil
}
