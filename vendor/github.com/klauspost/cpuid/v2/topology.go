// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

package cpuid

import (
	"cmp"
	"math/bits"
	"slices"
	"sync/atomic"

	"github.com/klauspost/cpuid/v2/topology"
)

// logicalCPU holds the topology-relevant CPUID data of a single logical
// processor. It is produced by parseLogicalCPU on whichever CPU the calling
// goroutine currently runs, so a full tree needs one per online CPU (see
// ScanTopology); a single one drives the extrapolated snapshot.
type logicalCPU struct {
	apicID   uint32            // (x2)APIC id
	vendor   Vendor            //
	smtShift uint8             // apicID>>smtShift == core id
	pkgShift uint8             // apicID>>pkgShift == package id
	l3Shift  uint8             // apicID>>l3Shift  == L3 (group) id; 0 if no L3
	nodeID   int               // AMD node id (leaf 0x8000001E ECX)
	coreType topology.CoreType //
	model    string            //
	caches   []cpuCache        //
}

// cpuCache pairs a decoded cache with the addressable number of logical
// processors that share it, which is what determines its sharing domain.
type cpuCache struct {
	c        topology.Cache
	numShare int
}

// topoSnapshot caches the extrapolated single-CPU topology; Detect resets it.
var topoSnapshot atomic.Pointer[topology.System]

// Topology returns the CPU topology as a fixed hierarchy of
// System → Package → Group → Core → Thread.
//
// It is extrapolated from the current core's CPUID and the detected core
// counts, so counts and cache totals are accurate for homogeneous CPUs but
// individual cores of a hybrid CPU cannot be classified. For an exact,
// per-core tree (real efficiency/performance mapping and CCD membership) use
// ScanTopology, which briefly pins the current goroutine to each CPU.
func (c CPUInfo) Topology() *topology.System {
	if t := topoSnapshot.Load(); t != nil {
		return t
	}
	t := snapshotTopology()
	topoSnapshot.Store(t)
	return t
}

// parseLogicalCPU reads the topology leaves of the current logical processor.
func parseLogicalCPU() logicalCPU {
	var lc logicalCPU
	lc.vendor, _ = vendorID()
	mfi := maxFunctionID()
	mef := maxExtendedFunction()

	// Extended topology enumeration: leaf 0x1F (V2) is preferred over 0x0B.
	// EAX[4:0] is the right-shift of the x2APIC id (EDX) that yields the id of
	// the next coarser level; ECX[15:8] is the level type (1=SMT, 2=Core, …).
	topoLeaf := uint32(0)
	if mfi >= 0x1f {
		if _, ebx, _, _ := cpuidex(0x1f, 0); ebx&0xffff != 0 {
			topoLeaf = 0x1f
		}
	}
	if topoLeaf == 0 && mfi >= 0xb {
		if _, ebx, _, _ := cpuidex(0xb, 0); ebx&0xffff != 0 {
			topoLeaf = 0xb
		}
	}
	switch {
	case topoLeaf != 0:
		var maxShift uint8
		for sub := range uint32(16) {
			eax, ebx, ecx, edx := cpuidex(topoLeaf, sub)
			if ebx&0xffff == 0 {
				break
			}
			lc.apicID = edx
			shift := uint8(eax & 0x1f)
			if (ecx>>8)&0xff == 1 { // SMT level
				lc.smtShift = shift
			}
			if shift > maxShift {
				maxShift = shift
			}
		}
		lc.pkgShift = maxShift
	case mfi >= 1:
		// Legacy 8-bit initial APIC id.
		_, ebx, _, _ := cpuid(1)
		lc.apicID = (ebx >> 24) & 0xff
		lc.smtShift = ceilLog2(threadsPerCore())
		lc.pkgShift = 8
	}

	if lc.vendor == AMD || lc.vendor == Hygon {
		if mef >= 0x8000001e {
			eax, ebx, ecx, _ := cpuid(0x8000001e)
			if topoLeaf == 0 {
				lc.apicID = eax // Extended APIC id, authoritative on AMD.
			}
			// EBX[15:8]+1 == threads per core, but only on Zen (fam>=0x17) and
			// Hygon (fam 0x18); pre-Zen "modules" (fam 0x15/0x16) are not SMT.
			if fam, _, _ := familyModel(); lc.smtShift == 0 && fam >= 0x17 {
				lc.smtShift = ceilLog2(int((ebx>>8)&0xff) + 1)
			}
			lc.nodeID = int(ecx & 0xff)
		}
	}

	lc.caches = parseCaches(lc.vendor, mfi, mef)
	for _, cc := range lc.caches {
		if cc.c.Level == 3 {
			lc.l3Shift = ceilLog2(cc.numShare)
		}
	}
	lc.coreType, lc.model = parseCoreType(lc.vendor, mfi)
	return lc
}

// parseCaches decodes the deterministic cache leaves (Intel 0x04 / AMD
// 0x8000001D). AMD only populates 0x8000001D when TopologyExtensions is set.
func parseCaches(vendor Vendor, mfi, mef uint32) []cpuCache {
	var out []cpuCache
	switch vendor {
	case Intel:
		if mfi < 4 {
			return nil
		}
		for i := range uint32(16) {
			eax, ebx, ecx, edx := cpuidex(4, i)
			if eax&0xf == 0 {
				break
			}
			out = append(out, decodeCache(eax, ebx, ecx, edx))
		}
	case AMD, Hygon:
		topext := false
		if mef >= 0x80000001 {
			_, _, ecx, _ := cpuid(0x80000001)
			topext = ecx&(1<<22) != 0
		}
		if mef >= 0x8000001d && topext {
			for i := range uint32(16) {
				eax, ebx, ecx, edx := cpuidex(0x8000001d, i)
				if eax&0xf == 0 {
					break
				}
				out = append(out, decodeCache(eax, ebx, ecx, edx))
			}
		}
	}
	return out
}

// decodeCache reads one subleaf of leaf 0x04 / 0x8000001D, which share layout.
func decodeCache(eax, ebx, ecx, edx uint32) cpuCache {
	line := int(ebx&0xfff) + 1
	parts := int((ebx>>12)&0x3ff) + 1
	ways := int((ebx>>22)&0x3ff) + 1
	sets := int(ecx) + 1
	return cpuCache{
		c: topology.Cache{
			Level:     int((eax >> 5) & 7),
			Type:      topology.CacheType(eax & 0xf), // 1=Data, 2=Instruction, 3=Unified
			Size:      line * parts * ways * sets,
			LineSize:  line,
			Ways:      ways,
			Sets:      sets,
			Inclusive: edx&2 != 0,
		},
		numShare: int((eax>>14)&0xfff) + 1,
	}
}

// parseCoreType classifies the current core from Intel's hybrid leaf 0x1A.
// The leaf reads 0 on non-hybrid Intel parts (so they fall through to
// performance), but all-efficiency parts populate it without the hybrid flag,
// so it is read whenever present rather than gated on that flag.
func parseCoreType(vendor Vendor, mfi uint32) (topology.CoreType, string) {
	if vendor == Intel && mfi >= 0x1a {
		switch eax, _, _, _ := cpuid(0x1a); eax >> 24 {
		case 0x40:
			return topology.Performance, ""
		case 0x20:
			return topology.Efficiency, ""
		}
	}
	if vendor == Intel || vendor == AMD || vendor == Hygon {
		return topology.Performance, ""
	}
	return topology.Unknown, ""
}

// assembleTopology builds the exact tree from one record per logical CPU.
// Package/group/core membership is derived by masking the x2APIC id, and each
// cache instance is attached to the deepest node that contains every logical
// processor sharing it, so aggregate sizes never double-count.
func assembleTopology(cpus []logicalCPU) *topology.System {
	sys := &topology.System{Scanned: true, Online: len(cpus)}
	if len(cpus) == 0 {
		return sys
	}
	sys.Vendor = cpus[0].vendor.String()

	groupShiftOf := func(lc logicalCPU) uint8 {
		s := lc.l3Shift
		if s == 0 {
			s = lc.pkgShift // no L3: one group spans the package
		}
		if s < lc.smtShift {
			s = lc.smtShift
		}
		if s == 0 {
			s = 32
		}
		return s
	}

	type coreData struct {
		typ     topology.CoreType
		model   string
		threads []topology.Thread
	}
	cores := map[uint32]*coreData{}
	groupOf := map[uint32]uint32{} // coreKey -> groupKey
	pkgOf := map[uint32]uint32{}   // groupKey -> pkgKey
	nodeOf := map[uint32]int{}     // groupKey -> NUMA node
	pkgSet := map[uint32]bool{}
	var pkgKeys, groupKeys, coreKeys []uint32

	for i, lc := range cpus {
		pk := lc.apicID >> lc.pkgShift
		gk := lc.apicID >> groupShiftOf(lc)
		ck := lc.apicID >> lc.smtShift
		cd := cores[ck]
		if cd == nil {
			cd = &coreData{typ: lc.coreType, model: lc.model}
			cores[ck] = cd
			coreKeys = append(coreKeys, ck)
			groupOf[ck] = gk
		}
		if _, ok := pkgOf[gk]; !ok {
			pkgOf[gk] = pk
			groupKeys = append(groupKeys, gk)
		}
		nodeOf[gk] = lc.nodeID
		if !pkgSet[pk] {
			pkgSet[pk] = true
			pkgKeys = append(pkgKeys, pk)
		}
		cd.threads = append(cd.threads, topology.Thread{APICID: lc.apicID, ID: i})
	}

	coreCaches, groupCaches, pkgCaches := attachCaches(cpus, groupShiftOf)

	slices.Sort(pkgKeys)
	slices.Sort(groupKeys)
	slices.Sort(coreKeys)

	coreID, groupID := 0, 0
	for pi, pk := range pkgKeys {
		p := topology.Package{ID: pi, Caches: sortCaches(pkgCaches[pk])}
		for _, gk := range groupKeys {
			if pkgOf[gk] != pk {
				continue
			}
			g := topology.Group{ID: groupID, NUMANode: nodeOf[gk], Caches: sortCaches(groupCaches[gk])}
			groupID++
			for _, ck := range coreKeys {
				if groupOf[ck] != gk {
					continue
				}
				cd := cores[ck]
				slices.SortFunc(cd.threads, func(a, b topology.Thread) int { return cmp.Compare(a.APICID, b.APICID) })
				g.Cores = append(g.Cores, topology.Core{
					ID:      coreID,
					Type:    cd.typ,
					Model:   cd.model,
					Threads: cd.threads,
					Caches:  sortCaches(coreCaches[ck]),
				})
				coreID++
			}
			p.Groups = append(p.Groups, g)
		}
		sys.Packages = append(sys.Packages, p)
	}
	return sys
}

// attachCaches groups every reported cache into instances by sharing domain
// and returns them keyed by the core/group/package they belong to.
func attachCaches(cpus []logicalCPU, groupShiftOf func(logicalCPU) uint8) (core, group, pkg map[uint32][]topology.Cache) {
	type cacheKey struct {
		level int
		typ   topology.CacheType
		dom   uint32
	}
	type cacheAgg struct {
		c      topology.Cache
		cores  map[uint32]bool
		groups map[uint32]bool
		pkgs   map[uint32]bool
		shared int
	}
	insts := map[cacheKey]*cacheAgg{}
	for _, lc := range cpus {
		pk := lc.apicID >> lc.pkgShift
		gk := lc.apicID >> groupShiftOf(lc)
		ck := lc.apicID >> lc.smtShift
		for _, cc := range lc.caches {
			key := cacheKey{cc.c.Level, cc.c.Type, lc.apicID >> ceilLog2(cc.numShare)}
			a := insts[key]
			if a == nil {
				a = &cacheAgg{c: cc.c, cores: map[uint32]bool{}, groups: map[uint32]bool{}, pkgs: map[uint32]bool{}}
				insts[key] = a
			}
			a.cores[ck], a.groups[gk], a.pkgs[pk] = true, true, true
			a.shared++
		}
	}
	core, group, pkg = map[uint32][]topology.Cache{}, map[uint32][]topology.Cache{}, map[uint32][]topology.Cache{}
	for _, a := range insts {
		c := a.c
		c.SharedThreads = a.shared
		switch {
		case len(a.cores) == 1:
			k := onlyKey(a.cores)
			core[k] = append(core[k], c)
		case len(a.groups) == 1:
			k := onlyKey(a.groups)
			group[k] = append(group[k], c)
		default:
			k := onlyKey(a.pkgs)
			pkg[k] = append(pkg[k], c)
		}
	}
	return core, group, pkg
}

// snapshotTopology extrapolates a homogeneous tree from the current core and
// the detected core counts, without pinning to other CPUs.
func snapshotTopology() *topology.System {
	if cpuid == nil {
		return countsTopology() // Non-x86: no CPUID, build from detected counts.
	}
	lc := parseLogicalCPU()
	tpc := max(threadsPerCore(), 1)
	physical := max(physicalCores(), 1)
	logical := max(logicalCores(), physical*tpc)

	var l1d, l1i, l2, l3 *topology.Cache
	l3Share := 0
	for i := range lc.caches {
		cc := &lc.caches[i]
		switch {
		case cc.c.Level == 1 && cc.c.Type == topology.Instruction:
			l1i = &cc.c
		case cc.c.Level == 1:
			l1d = &cc.c
		case cc.c.Level == 2:
			l2 = &cc.c
		case cc.c.Level == 3:
			l3 = &cc.c
			l3Share = cc.numShare
		}
	}

	coresPerGroup := physical
	if l3Share > 0 && l3Share/tpc > 0 {
		coresPerGroup = l3Share / tpc
	}
	coresPerGroup = min(coresPerGroup, physical)
	groups := 1
	if coresPerGroup > 0 {
		groups = (physical + coresPerGroup - 1) / coresPerGroup
	}

	sys := &topology.System{Vendor: lc.vendor.String(), Online: logical}
	pkg := topology.Package{}
	coreID, apic, remaining := 0, uint32(0), physical
	for g := range groups {
		grp := topology.Group{ID: g, NUMANode: lc.nodeID}
		n := min(coresPerGroup, remaining)
		for range n {
			core := topology.Core{ID: coreID, Type: lc.coreType, Model: lc.model}
			for range tpc {
				core.Threads = append(core.Threads, topology.Thread{APICID: apic, ID: int(apic)})
				apic++
			}
			addCache(&core.Caches, l1d, tpc)
			addCache(&core.Caches, l1i, tpc)
			addCache(&core.Caches, l2, tpc)
			grp.Cores = append(grp.Cores, core)
			coreID++
		}
		addCache(&grp.Caches, l3, n*tpc)
		remaining -= n
		pkg.Groups = append(pkg.Groups, grp)
	}
	sys.Packages = []topology.Package{pkg}
	return sys
}

func addCache(dst *[]topology.Cache, c *topology.Cache, shared int) {
	if c == nil || c.Size == 0 {
		return
	}
	cc := *c
	cc.SharedThreads = shared
	*dst = append(*dst, cc)
}

// countsTopology builds a minimal tree from the already-detected counts and
// cache sizes, used where CPUID is not available (non-x86, or noasm builds).
func countsTopology() *topology.System {
	tpc := max(CPU.ThreadsPerCore, 1)
	physical := CPU.PhysicalCores
	if physical < 1 {
		physical = max(1, CPU.LogicalCores/tpc)
	}
	sys := &topology.System{Vendor: CPU.VendorString, Online: CPU.LogicalCores}
	var grp topology.Group
	id := 0
	for c := range physical {
		core := topology.Core{ID: c, Type: topology.Performance}
		for range tpc {
			core.Threads = append(core.Threads, topology.Thread{ID: id})
			id++
		}
		addCacheVal(&core.Caches, 1, topology.Data, CPU.Cache.L1D, tpc)
		addCacheVal(&core.Caches, 1, topology.Instruction, CPU.Cache.L1I, tpc)
		addCacheVal(&core.Caches, 2, topology.Unified, CPU.Cache.L2, tpc)
		grp.Cores = append(grp.Cores, core)
	}
	addCacheVal(&grp.Caches, 3, topology.Unified, CPU.Cache.L3, physical*tpc)
	sys.Packages = []topology.Package{{Groups: []topology.Group{grp}}}
	return sys
}

func addCacheVal(dst *[]topology.Cache, level int, t topology.CacheType, size, shared int) {
	if size <= 0 {
		return
	}
	*dst = append(*dst, topology.Cache{Level: level, Type: t, Size: size, SharedThreads: shared})
}

func sortCaches(cs []topology.Cache) []topology.Cache {
	slices.SortFunc(cs, func(a, b topology.Cache) int {
		if a.Level != b.Level {
			return cmp.Compare(a.Level, b.Level)
		}
		return cmp.Compare(a.Type, b.Type)
	})
	return cs
}

func onlyKey(m map[uint32]bool) uint32 {
	for k := range m {
		return k
	}
	return 0
}

// ceilLog2 returns the smallest s such that 1<<s >= n.
func ceilLog2(n int) uint8 {
	if n <= 1 {
		return 0
	}
	return uint8(bits.Len(uint(n - 1)))
}
