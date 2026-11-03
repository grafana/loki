// Copyright The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package procfs

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// A MountInfo is a type that describes the details, options
// for each mount, parsed from /proc/self/mountinfo.
// The fields described in each entry of /proc/self/mountinfo
// is described in the following man page.
// http://man7.org/linux/man-pages/man5/proc.5.html
type MountInfo struct {
	// Unique ID for the mount
	MountID int
	// The ID of the parent mount
	ParentID int
	// The value of `st_dev` for the files on this FS
	MajorMinorVer string
	// The pathname of the directory in the FS that forms
	// the root for this mount
	Root string
	// The pathname of the mount point relative to the root
	MountPoint string
	// Mount options
	Options map[string]string
	// Zero or more optional fields
	OptionalFields map[string]string
	// The Filesystem type
	FSType string
	// FS specific information or "none"
	Source string
	// Superblock options
	SuperOptions map[string]string
}

// Reads each line of the mountinfo file, and returns a list of formatted MountInfo structs.
func parseMountInfo(info []byte) ([]*MountInfo, error) {
	mounts := []*MountInfo{}
	scanner := bufio.NewScanner(bytes.NewReader(info))
	for scanner.Scan() {
		mountString := scanner.Text()
		parsedMounts, err := parseMountInfoString(mountString)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, parsedMounts)
	}

	err := scanner.Err()
	return mounts, err
}

// Parses a mountinfo file line, and converts it to a MountInfo struct.
// An important check here is to see if the hyphen separator, as if it does not exist,
// it means that the line is malformed.
// See: https://man7.org/linux/man-pages/man5/proc_pid_mountinfo.5.html
func parseMountInfoString(mountString string) (*MountInfo, error) {
	var err error

	fields := strings.Split(mountString, " - ")
	if len(fields) != 2 {
		return nil, fmt.Errorf("%w: Could not split hyphen separator: %s", ErrFileParse, mountString)
	}

	mountInfo := strings.Split(fields[0], " ")
	if len(mountInfo) < 6 {
		return nil, fmt.Errorf("%w: Too few fields in mount string: %s", ErrFileParse, mountString)
	}

	mount := &MountInfo{
		MajorMinorVer:  mountInfo[2],
		Root:           mountInfo[3],
		MountPoint:     mountInfo[4],
		Options:        mountOptionsParser(mountInfo[5]),
		OptionalFields: map[string]string{},
	}

	mount.MountID, err = strconv.Atoi(mountInfo[0])
	if err != nil {
		return nil, fmt.Errorf("%w: mount ID: %q", ErrFileParse, mountInfo[0])
	}
	mount.ParentID, err = strconv.Atoi(mountInfo[1])
	if err != nil {
		return nil, fmt.Errorf("%w: parent ID: %q", ErrFileParse, mountInfo[1])
	}

	// Has optional fields, which is a space separated list of values.
	// Example: shared:2 master:7
	if len(mountInfo) > 6 {
		mount.OptionalFields, err = mountOptionsParseOptionalFields(mountInfo[6:])
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFileParse, err)
		}
	}

	mountInfo = strings.SplitN(fields[1], " ", 3)
	if len(mountInfo) != 3 {
		return nil, fmt.Errorf("%w: Too few fields after separator: %s", ErrFileParse, mountString)
	}

	mount.FSType = mountInfo[0]
	mount.Source = mountInfo[1]
	mount.SuperOptions = mountOptionsParser(mountInfo[2])

	return mount, nil
}

// mountOptionsIsValidField checks a string against a valid list of optional fields keys.
func mountOptionsIsValidField(s string) bool {
	switch s {
	case
		"shared",
		"master",
		"propagate_from",
		"unbindable":
		return true
	}
	return false
}

// mountOptionsParseOptionalFields parses a list of optional fields strings into a double map of strings.
func mountOptionsParseOptionalFields(o []string) (map[string]string, error) {
	optionalFields := make(map[string]string)
	for _, field := range o {
		optionSplit := strings.SplitN(field, ":", 2)
		value := ""
		if len(optionSplit) == 2 {
			value = optionSplit[1]
		}
		if mountOptionsIsValidField(optionSplit[0]) {
			optionalFields[optionSplit[0]] = value
		}
	}
	return optionalFields, nil
}

// mountOptionsParser parses the mount options, superblock options.
func mountOptionsParser(mountOptions string) map[string]string {
	opts := make(map[string]string)
	for opt := range strings.SplitSeq(mountOptions, ",") {
		splitOption := strings.SplitN(opt, "=", 2)
		if len(splitOption) < 2 {
			opts[splitOption[0]] = ""
		} else {
			opts[splitOption[0]] = splitOption[1]
		}
	}
	return opts
}

// readMountInfo reads a full mountinfo file (no 1 MiB cap, unlike parsers.ReadFileNoStat).
func readMountInfo(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// GetMounts retrieves mountinfo information from `/proc/self/mountinfo`.
func GetMounts() ([]*MountInfo, error) {
	data, err := readMountInfo("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	return parseMountInfo(data)
}

// GetProcMounts retrieves mountinfo information from a processes' `/proc/<pid>/mountinfo`.
func GetProcMounts(pid int) ([]*MountInfo, error) {
	data, err := readMountInfo(fmt.Sprintf("/proc/%d/mountinfo", pid))
	if err != nil {
		return nil, err
	}
	return parseMountInfo(data)
}

// GetMounts retrieves mountinfo information from `/proc/self/mountinfo`.
func (fs FS) GetMounts() ([]*MountInfo, error) {
	data, err := readMountInfo(fs.proc.Path("self/mountinfo"))
	if err != nil {
		return nil, err
	}
	return parseMountInfo(data)
}

// GetProcMounts retrieves mountinfo information from a processes' `/proc/<pid>/mountinfo`.
func (fs FS) GetProcMounts(pid int) ([]*MountInfo, error) {
	data, err := readMountInfo(fs.proc.Path(fmt.Sprintf("%d/mountinfo", pid)))
	if err != nil {
		return nil, err
	}
	return parseMountInfo(data)
}
