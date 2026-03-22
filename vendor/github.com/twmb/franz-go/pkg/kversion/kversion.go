// Package kversion specifies versions for Kafka request keys.
//
// Kafka technically has internal broker versions that bump multiple times per
// release. This package only defines releases and tip.
package kversion

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sync"
	"text/tabwriter"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// Versions is a list of versions, with each item corresponding to a Kafka key
// and each item's value corresponding to the max version supported.
type Versions struct {
	reqs map[int16]req
}

func (vs *Versions) lazyInit() {
	if vs.reqs == nil {
		vs.reqs = make(map[int16]req)
	}
}

var (
	reFromString     *regexp.Regexp
	reFromStringOnce sync.Once
)

// VersionStrings returns all recognized versions, minus any patch, that can be
// used as input to FromString.
func VersionStrings() []string {
	var vs []string
	b := btip()
	for b != nil && b.major >= 4 {
		vs = append(vs, b.name())
		b = b.prior
	}
	zk := ztip()
	for zk != nil {
		vs = append(vs, zk.name())
		zk = zk.prior
	}
	return vs
}

// FromString returns a Versions from v.
// The expected input is:
//   - for v0, v0.#.# or v0.#.#.#
//   - for v1, v1.# or v1.#.#
//
// The "v" is optional.
func FromString(v string) *Versions {
	reFromStringOnce.Do(func() {
		// 0: entire string
		// 1: v1+ match, minus patch
		// 2: v0 match, minus subpatch
		reFromString = regexp.MustCompile(`^(?:(v?[1-9]+\.\d+)(?:\.\d+)?|(v?0\.\d+\.\d+)(?:\.\d+)?)$`)
	})
	m := reFromString.FindStringSubmatch(v)
	if m == nil {
		return nil
	}
	v = m[1]
	if m[2] != "" {
		v = m[2]
	}

	withv := "v" + v
	b := btip()
	for b != nil && b.major >= 4 {
		if n := b.name(); n == v || n == withv {
			return &Versions{reqs: b.reqs}
		}
		b = b.prior
	}
	zk := ztip()
	for zk != nil {
		if n := zk.name(); n == v || n == withv {
			return &Versions{reqs: zk.reqs}
		}
		zk = zk.prior
	}
	return nil
}

// FromApiVersionsResponse returns a Versions from a kmsg.ApiVersionsResponse.
func FromApiVersionsResponse(r *kmsg.ApiVersionsResponse) *Versions {
	return &Versions{reqs: reqsFromApiVersions(r)}
}

// HasKey returns true if the versions contains the given key.
func (vs *Versions) HasKey(k int16) bool {
	_, has := vs.LookupMaxKeyVersion(k)
	return has
}

// LookupMaxKeyVersion returns the version for the given key and whether the
// key exists. If the key does not exist, this returns (-1, false).
func (vs *Versions) LookupMaxKeyVersion(k int16) (int16, bool) {
	vs.lazyInit()
	req, ok := vs.reqs[k]
	if !ok {
		return -1, false
	}
	return req.vmax, true
}

// SetMaxKeyVersion sets the max version for the given key. Setting a version
// to -1 removes the key entirely.
func (vs *Versions) SetMaxKeyVersion(k, v int16) {
	vs.lazyInit()
	if k < 0 || v < 0 {
		delete(vs.reqs, k)
		return
	}
	req := vs.reqs[k]
	req.vmax = v
	req.key = k // in case the key did not exist
	vs.reqs[k] = req
}

// Equal returns whether two versions are equal.
func (vs *Versions) Equal(other *Versions) bool {
	vs.lazyInit()
	mereqs := maps.Clone(vs.reqs)
	for k, oreq := range other.reqs {
		mreq, ok := mereqs[k]
		if !ok || mreq != oreq {
			return false
		}
		delete(mereqs, k)
	}
	return len(mereqs) == 0
}

// EachMaxKeyVersion calls fn for each key and max version
func (vs *Versions) EachMaxKeyVersion(fn func(k, v int16)) {
	vs.lazyInit()
	keys := make([]int16, 0, len(vs.reqs))
	for k := range vs.reqs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fn(k, vs.reqs[k].vmax)
	}
}

// VersionGuessOpt is an option to change how version guessing is done.
type VersionGuessOpt interface {
	apply(*guessCfg)
}

type guessOpt struct{ fn func(*guessCfg) }

func (opt guessOpt) apply(cfg *guessCfg) { opt.fn(cfg) }

// SkipKeys skips the given keys while guessing versions.
func SkipKeys(keys ...int16) VersionGuessOpt {
	return guessOpt{func(cfg *guessCfg) { cfg.skipKeys = keys }}
}

// TryRaftBroker previously attempted to version guess selecting for a raft
// broker.
//
// Deprecated: Zookeeper, KRaft broker, and KRaft controller are all checked
// and the best pick is chosen.
func TryRaftBroker() VersionGuessOpt {
	return guessOpt{func(*guessCfg) {}}
}

// TryRaftController previously attempted to version guest selecting for a
// raft controller.
//
// Deprecated: Zookeeper, KRaft broker, and KRaft controller are all checked
// and the best pick is chosen.
func TryRaftController() VersionGuessOpt {
	return guessOpt{func(*guessCfg) {}}
}

type guessCfg struct {
	skipKeys []int16
}

// VersionGuess attempts to guess which version of Kafka these versions belong
// to. If an exact match can be determined, this returns a string in the format
// v0.#.# or v#.# (depending on whether Kafka is pre-1.0 or post). For
// example, v0.8.0 or v2.7.
//
// Patch numbers are not included in the guess as it is not possible to
// determine the Kafka patch version being used as a client.
//
// If the version is determined to be higher than kversion knows of or is tip,
// this package returns "at least v#.#".
//
// Custom versions, or in-between versions, are detected and return slightly
// more verbose strings.
//
// Options can be specified to change how version guessing is performed, for
// example, certain keys can be skipped, or the guessing can try evaluating the
// versions as Raft broker based versions.
//
// Internally, this function tries guessing the version against both KRaft and
// Kafka APIs. The more exact match is returned.
func (vs *Versions) VersionGuess(opts ...VersionGuessOpt) string {
	zk := vs.versionGuess2(ztip(), opts...)
	broker := vs.versionGuess2(btip(), opts...)
	controller := vs.versionGuess2(ctip(), opts...)

	ord := []guess{broker, zk, controller}

	for _, g := range ord {
		if g.how == guessExact {
			return g.String()
		}
	}
	for _, g := range ord {
		if g.how == guessAtLeast {
			return g.String()
		}
	}

	// This is a custom version. We could do some advanced logic to try to
	// return highest of all three guesses, but that may be inaccurate:
	// KRaft may detect a higher guess because not all requests exist in
	// KRaft. Instead, we just return our standard guess.
	return zk.String()
}

type guess struct {
	v1  string
	v2  string // for between
	how int8
}

const (
	guessExact = iota
	guessAtLeast
	guessCustomUnknown
	guessCustomAtLeast
	guessBetween
	guessNotEven
)

func (g guess) String() string {
	switch g.how {
	case guessExact:
		return g.v1
	case guessAtLeast:
		return "at least " + g.v1
	case guessCustomUnknown:
		return "unknown custom version"
	case guessCustomAtLeast:
		return "unknown custom version at least " + g.v1
	case guessBetween:
		return "between " + g.v1 + " and " + g.v2
	case guessNotEven:
		return "not even " + g.v1
	}
	return g.v1
}

// String returns a string representation of the versions; the format may
// change.
func (vs *Versions) String() string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	keys := make([]int16, 0, len(vs.reqs))
	for k := range vs.reqs {
		keys = append(keys, k)
	}
	for _, k := range keys {
		name := kmsg.NameForKey(k)
		if name == "" {
			name = "Unknown"
		}
		fmt.Fprintf(w, "%s\t%d\n", name, vs.reqs[k].vmax)
	}
	w.Flush()
	return buf.String()
}

func relversion(fns ...func() *release) *Versions {
	// For 4.0+, we merge the Raft broker, Raft controller, and Zk broker
	// requests. We merge *in order*: any key that exists is kept, any key
	// that does not exist is merged.
	var reqs map[int16]req
	for _, fn := range fns {
		if reqs == nil {
			reqs = fn().reqs
			continue
		}
		merge := fn().reqs
		for k, req := range merge {
			if _, ok := reqs[k]; ok {
				continue
			}
			reqs[k] = req
		}
	}
	return &Versions{reqs: reqs}
}

// Stable is a shortcut for the latest _released_ Kafka versions.
//
// This is the default version used in kgo to avoid breaking tip changes.
// The stable version is only bumped once kgo internally supports all
// features in the release.
func Stable() *Versions { return relversion(b41, c41, z39) }

// Tip is the latest defined Kafka key versions; this may be slightly out of date.
func Tip() *Versions { return relversion(ztip) }

func V0_8_0() *Versions  { return relversion(z080) }
func V0_8_1() *Versions  { return relversion(z081) }
func V0_8_2() *Versions  { return relversion(z082) }
func V0_9_0() *Versions  { return relversion(z090) }
func V0_10_0() *Versions { return relversion(z0100) }
func V0_10_1() *Versions { return relversion(z0101) }
func V0_10_2() *Versions { return relversion(z0102) }
func V0_11_0() *Versions { return relversion(z0110) }
func V1_0_0() *Versions  { return relversion(z10) }
func V1_1_0() *Versions  { return relversion(z11) }
func V2_0_0() *Versions  { return relversion(z20) }
func V2_1_0() *Versions  { return relversion(z21) }
func V2_2_0() *Versions  { return relversion(z22) }
func V2_3_0() *Versions  { return relversion(z23) }
func V2_4_0() *Versions  { return relversion(z24) }
func V2_5_0() *Versions  { return relversion(z25) }
func V2_6_0() *Versions  { return relversion(z26) }
func V2_7_0() *Versions  { return relversion(z27) }
func V2_8_0() *Versions  { return relversion(z28) }
func V3_0_0() *Versions  { return relversion(z30) }
func V3_1_0() *Versions  { return relversion(z31) }
func V3_2_0() *Versions  { return relversion(z32) }
func V3_3_0() *Versions  { return relversion(z33) }
func V3_4_0() *Versions  { return relversion(z34) }
func V3_5_0() *Versions  { return relversion(z35) }
func V3_6_0() *Versions  { return relversion(z36) }
func V3_7_0() *Versions  { return relversion(z37) }
func V3_8_0() *Versions  { return relversion(z38) }
func V3_9_0() *Versions  { return relversion(z39) }
func V4_0_0() *Versions  { return relversion(b40) }
func V4_1_0() *Versions  { return relversion(b41) }
