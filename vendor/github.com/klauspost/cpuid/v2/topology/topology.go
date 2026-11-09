// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

// Package topology describes the layout of a system's CPUs as a fixed
// hierarchy: System → Package → Group → Core → Thread.
//
// A Package is a socket (a physical "CPU"). A Group is a set of cores that
// share a last-level cache — an AMD CCX/CCD or an Intel LLC tile; there is
// always at least one, so a monolithic chip has a single Group holding the
// whole L3. A Core carries its type (performance/efficiency/…), its shared
// per-core caches and its logical Threads.
//
// Caches are attached to the level that owns them: L1/L2 on a Core, the
// shared L3 on a Group, a package-wide last-level cache on a Package. Because
// every instance is stored exactly once, the TotalCache aggregate never
// double-counts: System.TotalCache(3, Unified) is the combined L3 across all
// groups (e.g. all CCDs).
//
// The package holds types and queries only; it is filled in by the cpuid
// package and has no dependencies of its own.
package topology

import (
	"fmt"
	"strings"
)

// CacheType identifies what a cache stores. The values match the type field
// of CPUID leaves 0x04 / 0x8000001D.
type CacheType uint8

const (
	Data        CacheType = iota + 1 // Data cache
	Instruction                      // Instruction cache
	Unified                          // Unified cache (holds both)
)

func (t CacheType) String() string {
	switch t {
	case Data:
		return "Data"
	case Instruction:
		return "Instruction"
	case Unified:
		return "Unified"
	}
	return "Unknown"
}

// MarshalText renders the cache type as its name, so it serializes as a string
// in JSON and other text encodings rather than a number.
func (t CacheType) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// CoreType is the microarchitectural class of a core. On heterogeneous
// ("hybrid") CPUs this distinguishes performance from efficiency cores.
type CoreType uint8

const (
	Unknown      CoreType = iota // Type could not be determined
	Performance                  // Intel P-core (leaf 0x1A == 0x40), AMD classic Zen core
	Efficiency                   // Intel E-core (leaf 0x1A == 0x20)
	LPEfficiency                 // Intel low-power E-core on the SoC tile
	Dense                        // AMD dense core (Zen 4c / 5c)
)

func (t CoreType) String() string {
	switch t {
	case Performance:
		return "Performance"
	case Efficiency:
		return "Efficiency"
	case LPEfficiency:
		return "LP-Efficiency"
	case Dense:
		return "Dense"
	}
	return "Unknown"
}

// MarshalText renders the core type as its name for JSON and other text
// encodings.
func (t CoreType) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// Cache describes a single cache instance.
type Cache struct {
	Level         int       // 1, 2, 3, …
	Type          CacheType // Data, Instruction or Unified
	Size          int       // Size in bytes of this instance
	LineSize      int       // Line size in bytes
	Ways          int       // Associativity
	Sets          int       // Number of sets
	SharedThreads int       // Logical processors that share this instance
	Inclusive     bool      // Inclusive of lower cache levels
}

// Thread is a single logical processor.
type Thread struct {
	APICID uint32 // (x2)APIC id
	ID     int    // OS logical CPU number if known from a scan, else the enumeration index
}

// Core is a single physical core and the threads it runs.
type Core struct {
	ID      int
	Type    CoreType
	Model   string   // Microarchitecture label if known, e.g. "Golden Cove"
	Threads []Thread // More than one indicates SMT / hyper-threading
	Caches  []Cache  // Caches private to this core, typically L1 and L2
}

// NumThreads returns the number of logical processors on the core.
func (c *Core) NumThreads() int { return len(c.Threads) }

// TotalCache returns the size in bytes of the core-private caches of the
// given level and type.
func (c *Core) TotalCache(level int, t CacheType) int { return sumCaches(c.Caches, level, t) }

// Group is a set of cores sharing a last-level cache — an AMD CCX/CCD or an
// Intel LLC tile. There is always at least one per package.
type Group struct {
	ID       int
	NUMANode int     // AMD node id (leaf 0x8000001E ECX); 0 when unknown or single-node
	Cores    []Core  // Cores in the group
	Caches   []Cache // Caches shared by the whole group, typically L3
}

// NumCores returns the number of cores in the group.
func (g *Group) NumCores() int { return len(g.Cores) }

// NumThreads returns the number of logical processors below the group.
func (g *Group) NumThreads() int { return countThreads(g.Cores) }

// CoresByType returns a count of cores per CoreType within the group.
func (g *Group) CoresByType() map[CoreType]int { return coresByType(g.Cores) }

// TotalCache sums the caches of the given level and type at and below the
// group, counting each instance once.
func (g *Group) TotalCache(level int, t CacheType) int {
	return sumCaches(g.Caches, level, t) + coresCache(g.Cores, level, t)
}

// Package is a socket / physical CPU.
type Package struct {
	ID     int
	Groups []Group
	Caches []Cache // Package-wide caches, if any
}

// NumCores returns the number of cores in the package.
func (p *Package) NumCores() int {
	n := 0
	for i := range p.Groups {
		n += len(p.Groups[i].Cores)
	}
	return n
}

// NumThreads returns the number of logical processors in the package.
func (p *Package) NumThreads() int {
	n := 0
	for i := range p.Groups {
		n += p.Groups[i].NumThreads()
	}
	return n
}

// CoresByType returns a count of cores per CoreType within the package.
func (p *Package) CoresByType() map[CoreType]int {
	m := map[CoreType]int{}
	for i := range p.Groups {
		mergeCounts(m, p.Groups[i].Cores)
	}
	return m
}

// Cores returns every core in the package.
func (p *Package) Cores() []Core {
	var out []Core
	for i := range p.Groups {
		out = append(out, p.Groups[i].Cores...)
	}
	return out
}

// TotalCache sums the caches of the given level and type at and below the
// package, counting each instance once.
func (p *Package) TotalCache(level int, t CacheType) int {
	n := sumCaches(p.Caches, level, t)
	for i := range p.Groups {
		n += p.Groups[i].TotalCache(level, t)
	}
	return n
}

// System is the root of the topology: the whole machine.
type System struct {
	Vendor   string    // CPU vendor string
	Scanned  bool      // true if built from a per-core scan, false if extrapolated from one core
	Online   int       // Logical processors observed
	Packages []Package // Physical packages / sockets
}

// NumPackages returns the number of packages (sockets).
func (s *System) NumPackages() int { return len(s.Packages) }

// NumCores returns the total number of physical cores in the system.
func (s *System) NumCores() int {
	n := 0
	for i := range s.Packages {
		n += s.Packages[i].NumCores()
	}
	return n
}

// NumThreads returns the total number of logical processors in the system.
func (s *System) NumThreads() int {
	n := 0
	for i := range s.Packages {
		n += s.Packages[i].NumThreads()
	}
	return n
}

// CoresByType returns a count of cores per CoreType across the system.
func (s *System) CoresByType() map[CoreType]int {
	m := map[CoreType]int{}
	for i := range s.Packages {
		for j := range s.Packages[i].Groups {
			mergeCounts(m, s.Packages[i].Groups[j].Cores)
		}
	}
	return m
}

// Cores returns every core in the system.
func (s *System) Cores() []Core {
	var out []Core
	for i := range s.Packages {
		out = append(out, s.Packages[i].Cores()...)
	}
	return out
}

// TotalCache sums the caches of the given level and type across the whole
// system, counting each instance once. For example TotalCache(3, Unified)
// returns the combined L3 across all groups.
func (s *System) TotalCache(level int, t CacheType) int {
	n := 0
	for i := range s.Packages {
		n += s.Packages[i].TotalCache(level, t)
	}
	return n
}

func sumCaches(cs []Cache, level int, t CacheType) int {
	n := 0
	for _, c := range cs {
		if c.Level == level && c.Type == t {
			n += c.Size
		}
	}
	return n
}

func coresCache(cs []Core, level int, t CacheType) int {
	n := 0
	for i := range cs {
		n += sumCaches(cs[i].Caches, level, t)
	}
	return n
}

func countThreads(cs []Core) int {
	n := 0
	for i := range cs {
		n += len(cs[i].Threads)
	}
	return n
}

func coresByType(cs []Core) map[CoreType]int {
	m := map[CoreType]int{}
	mergeCounts(m, cs)
	return m
}

func mergeCounts(m map[CoreType]int, cs []Core) {
	for i := range cs {
		m[cs[i].Type]++
	}
}

// String renders the topology as an indented tree, followed by a summary.
func (s *System) String() string {
	var b strings.Builder
	kind := "extrapolated from one core"
	if s.Scanned {
		kind = "scanned"
	}
	fmt.Fprintf(&b, "System: %s (%s) — %d package(s), %d core(s), %d thread(s)\n",
		s.Vendor, kind, s.NumPackages(), s.NumCores(), s.NumThreads())
	for pi := range s.Packages {
		p := &s.Packages[pi]
		fmt.Fprintf(&b, "  Package #%d%s\n", p.ID, cacheSuffix(p.Caches))
		for gi := range p.Groups {
			g := &p.Groups[gi]
			numa := ""
			if g.NUMANode != 0 {
				numa = fmt.Sprintf(" NUMA#%d", g.NUMANode)
			}
			fmt.Fprintf(&b, "    Group #%d%s%s\n", g.ID, numa, cacheSuffix(g.Caches))
			for ci := range g.Cores {
				c := &g.Cores[ci]
				model := ""
				if c.Model != "" {
					model = " " + c.Model
				}
				fmt.Fprintf(&b, "      Core #%d (%s%s) threads:%s%s\n",
					c.ID, c.Type, model, threadList(c.Threads), cacheSuffix(c.Caches))
			}
		}
	}
	if types := s.CoresByType(); len(types) > 1 || !hasOnly(types, Performance) {
		b.WriteString("  Cores by type:")
		for _, t := range []CoreType{Performance, Efficiency, LPEfficiency, Dense, Unknown} {
			if n := types[t]; n > 0 {
				fmt.Fprintf(&b, " %s=%d", t, n)
			}
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "  Total cache: L1D %s, L1I %s, L2 %s, L3 %s\n",
		humanBytes(s.TotalCache(1, Data)), humanBytes(s.TotalCache(1, Instruction)),
		humanBytes(s.TotalCache(2, Unified)), humanBytes(s.TotalCache(3, Unified)))
	return b.String()
}

func hasOnly(m map[CoreType]int, t CoreType) bool {
	for k := range m {
		if k != t {
			return false
		}
	}
	return true
}

func cacheSuffix(cs []Cache) string {
	if len(cs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, fmt.Sprintf("L%d%s %s", c.Level, cacheShort(c.Type), humanBytes(c.Size)))
	}
	return "  [" + strings.Join(parts, " ") + "]"
}

func cacheShort(t CacheType) string {
	switch t {
	case Data:
		return "d"
	case Instruction:
		return "i"
	}
	return ""
}

func threadList(ts []Thread) string {
	var b strings.Builder
	for i, t := range ts {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, " %d", t.ID)
	}
	return b.String()
}

func humanBytes(b int) string {
	switch {
	case b <= 0:
		return "0"
	case b%(1<<20) == 0:
		return fmt.Sprintf("%dMB", b>>20)
	case b%(1<<10) == 0:
		return fmt.Sprintf("%dKB", b>>10)
	default:
		return fmt.Sprintf("%dB", b)
	}
}
