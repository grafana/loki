// Copyright 2019 The Prometheus Authors
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
	"log"
	"strconv"
	"strings"

	"github.com/prometheus/procfs/internal/util"
)

// Meminfo represents memory statistics.
type Meminfo struct {
	// Total usable ram (i.e. physical ram minus a few reserved
	// bits and the kernel binary code)
	MemTotal uint64
	// The sum of LowFree+HighFree
	MemFree uint64
	// An estimate of how much memory is available for starting
	// new applications, without swapping. Calculated from
	// MemFree, SReclaimable, the size of the file LRU lists, and
	// the low watermarks in each zone.  The estimate takes into
	// account that the system needs some page cache to function
	// well, and that not all reclaimable slab will be
	// reclaimable, due to items being in use. The impact of those
	// factors will vary from system to system.
	MemAvailable uint64
	// Relatively temporary storage for raw disk blocks shouldn't
	// get tremendously large (20MB or so)
	Buffers uint64
	Cached  uint64
	// Memory that once was swapped out, is swapped back in but
	// still also is in the swapfile (if memory is needed it
	// doesn't need to be swapped out AGAIN because it is already
	// in the swapfile. This saves I/O)
	SwapCached uint64
	// Memory that has been used more recently and usually not
	// reclaimed unless absolutely necessary.
	Active uint64
	// Memory which has been less recently used.  It is more
	// eligible to be reclaimed for other purposes
	Inactive     uint64
	ActiveAnon   uint64
	InactiveAnon uint64
	ActiveFile   uint64
	InactiveFile uint64
	Unevictable  uint64
	Mlocked      uint64
	// total amount of swap space available
	SwapTotal uint64
	// Memory which has been evicted from RAM, and is temporarily
	// on the disk
	SwapFree uint64
	// Memory which is waiting to get written back to the disk
	Dirty uint64
	// Memory which is actively being written back to the disk
	Writeback uint64
	// Non-file backed pages mapped into userspace page tables
	AnonPages uint64
	// files which have been mapped, such as libraries
	Mapped uint64
	Shmem  uint64
	// in-kernel data structures cache
	Slab uint64
	// Part of Slab, that might be reclaimed, such as caches
	SReclaimable uint64
	// Part of Slab, that cannot be reclaimed on memory pressure
	SUnreclaim  uint64
	KernelStack uint64
	// amount of memory dedicated to the lowest level of page
	// tables.
	PageTables uint64
	// NFS pages sent to the server, but not yet committed to
	// stable storage
	NFSUnstable uint64
	// Memory used for block device "bounce buffers"
	Bounce uint64
	// Memory used by FUSE for temporary writeback buffers
	WritebackTmp uint64
	// Based on the overcommit ratio ('vm.overcommit_ratio'),
	// this is the total amount of  memory currently available to
	// be allocated on the system. This limit is only adhered to
	// if strict overcommit accounting is enabled (mode 2 in
	// 'vm.overcommit_memory').
	// The CommitLimit is calculated with the following formula:
	// CommitLimit = ([total RAM pages] - [total huge TLB pages]) *
	//                overcommit_ratio / 100 + [total swap pages]
	// For example, on a system with 1G of physical RAM and 7G
	// of swap with a `vm.overcommit_ratio` of 30 it would
	// yield a CommitLimit of 7.3G.
	// For more details, see the memory overcommit documentation
	// in vm/overcommit-accounting.
	CommitLimit uint64
	// The amount of memory presently allocated on the system.
	// The committed memory is a sum of all of the memory which
	// has been allocated by processes, even if it has not been
	// "used" by them as of yet. A process which malloc()'s 1G
	// of memory, but only touches 300M of it will show up as
	// using 1G. This 1G is memory which has been "committed" to
	// by the VM and can be used at any time by the allocating
	// application. With strict overcommit enabled on the system
	// (mode 2 in 'vm.overcommit_memory'),allocations which would
	// exceed the CommitLimit (detailed above) will not be permitted.
	// This is useful if one needs to guarantee that processes will
	// not fail due to lack of memory once that memory has been
	// successfully allocated.
	CommittedAS uint64
	// total size of vmalloc memory area
	VmallocTotal uint64
	// amount of vmalloc area which is used
	VmallocUsed uint64
	// largest contiguous block of vmalloc area which is free
	VmallocChunk      uint64
	HardwareCorrupted uint64
	AnonHugePages     uint64
	ShmemHugePages    uint64
	ShmemPmdMapped    uint64
	CmaTotal          uint64
	CmaFree           uint64
	HugePagesTotal    uint64
	HugePagesFree     uint64
	HugePagesRsvd     uint64
	HugePagesSurp     uint64
	Hugepagesize      uint64
	DirectMap4k       uint64
	DirectMap2M       uint64
	DirectMap1G       uint64
}

// Meminfo returns an information about current kernel/system memory statistics.
// See https://www.kernel.org/doc/Documentation/filesystems/proc.txt
func (fs FS) Meminfo() (Meminfo, error) {
	data, err := util.ReadFileNoStat(fs.proc.Path("meminfo"))
	if err != nil {
		return Meminfo{}, err
	}
	return parseMemInfo(data)
}

func parseMemInfo(info []byte) (m Meminfo, err error) {
	scanner := bufio.NewScanner(bytes.NewReader(info))

	var line string
	for scanner.Scan() {
		line = scanner.Text()
		log.Println(line)

		field := strings.Fields(line)
		log.Println(field[0])
		switch field[0] {
		case "MemTotal:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.MemTotal = v
		case "MemFree:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.MemFree = v
		case "MemAvailable:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.MemAvailable = v
		case "Buffers:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Buffers = v
		case "Cached:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Cached = v
		case "SwapCached:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.SwapCached = v
		case "Active:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Active = v
		case "Inactive:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Inactive = v
		case "Active(anon):":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.ActiveAnon = v
		case "Inactive(anon):":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.InactiveAnon = v
		case "Active(file):":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.ActiveFile = v
		case "Inactive(file):":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.InactiveFile = v
		case "Unevictable:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Unevictable = v
		case "Mlocked:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Mlocked = v
		case "SwapTotal:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.SwapTotal = v
		case "SwapFree:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.SwapFree = v
		case "Dirty:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Dirty = v
		case "Writeback:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Writeback = v
		case "AnonPages:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.AnonPages = v
		case "Mapped:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Mapped = v
		case "Shmem:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Shmem = v
		case "Slab:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Slab = v
		case "SReclaimable:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.SReclaimable = v
		case "SUnreclaim:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.SUnreclaim = v
		case "KernelStack:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.KernelStack = v
		case "PageTables:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.PageTables = v
		case "NFS_Unstable:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.NFSUnstable = v
		case "Bounce:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Bounce = v
		case "WritebackTmp:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.WritebackTmp = v
		case "CommitLimit:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.CommitLimit = v
		case "Committed_AS:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.CommittedAS = v
		case "VmallocTotal:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.VmallocTotal = v
		case "VmallocUsed:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.VmallocUsed = v
		case "VmallocChunk:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.VmallocChunk = v
		case "HardwareCorrupted:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.HardwareCorrupted = v
		case "AnonHugePages:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.AnonHugePages = v
		case "ShmemHugePages:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.ShmemHugePages = v
		case "ShmemPmdMapped:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.ShmemPmdMapped = v
		case "CmaTotal:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.CmaTotal = v
		case "CmaFree:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.CmaFree = v
		case "HugePages_Total:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.HugePagesTotal = v
		case "HugePages_Free:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.HugePagesFree = v
		case "HugePages_Rsvd:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.HugePagesRsvd = v
		case "HugePages_Surp:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.HugePagesSurp = v
		case "Hugepagesize:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.Hugepagesize = v
		case "DirectMap4k:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.DirectMap4k = v
		case "DirectMap2M:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.DirectMap2M = v
		case "DirectMap1G:":
			v, err := strconv.ParseUint(field[1], 0, 64)
			if err != nil {
				return Meminfo{}, err
			}
			m.DirectMap1G = v
		}
	}
	return m, nil
}
