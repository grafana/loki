// Package kversion specifies versions for Kafka request keys.
//
// Kafka technically has internal broker versions that bump multiple times per
// release. This package only defines releases and tip.
package kversion

import (
	"bytes"
	"fmt"
	"regexp"
	"sync"
	"text/tabwriter"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// Versions is a list of versions, with each item corresponding to a Kafka key
// and each item's value corresponding to the max version supported.
//
// Minimum versions are not currently tracked because all keys have a minimum
// version of zero. The internals of a Versions may change in the future to
// support minimum versions; the outward facing API of Versions should not
// change to support this.
//
// As well, supported features may be added in the future.
type Versions struct {
	// If any version is -1, then it is left out in that version.
	// This was first done in version 2.7.0, where Kafka added support
	// for 52, 53, 54, 55, but it was not a part of the 2.7.0 release,
	// so ApiVersionsResponse goes from 51 to 56.
	k2v []int16
}

var (
	reFromString     *regexp.Regexp
	reFromStringOnce sync.Once
)

var versions = []struct {
	name string
	v    *Versions
}{
	{"v0.8.0", V0_8_0()},
	{"v0.8.1", V0_8_1()},
	{"v0.8.2", V0_8_2()},
	{"v0.9.0", V0_9_0()},
	{"v0.10.0", V0_10_0()},
	{"v0.10.1", V0_10_1()},
	{"v0.10.2", V0_10_2()},
	{"v0.11.0", V0_11_0()},
	{"v1.0", V1_0_0()},
	{"v1.1", V1_1_0()},
	{"v2.0", V2_0_0()},
	{"v2.1", V2_1_0()},
	{"v2.2", V2_2_0()},
	{"v2.3", V2_3_0()},
	{"v2.4", V2_4_0()},
	{"v2.5", V2_5_0()},
	{"v2.6", V2_6_0()},
	{"v2.7", V2_7_0()},
	{"v2.8", V2_8_0()},
	{"v3.0", V3_0_0()},
	{"v3.1", V3_1_0()},
	{"v3.2", V3_2_0()},
	{"v3.3", V3_3_0()},
	{"v3.4", V3_4_0()},
	{"v3.5", V3_5_0()},
	{"v3.6", V3_6_0()},
	{"v3.7", V3_7_0()},
	{"v3.8", V3_8_0()},
}

// VersionStrings returns all recognized versions, minus any patch, that can be
// used as input to FromString.
func VersionStrings() []string {
	var vs []string
	for _, v := range versions {
		vs = append(vs, v.name)
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
	for _, v2 := range versions {
		if v2.name == v || v2.name == withv {
			return v2.v
		}
	}
	return nil
}

// FromApiVersionsResponse returns a Versions from a kmsg.ApiVersionsResponse.
func FromApiVersionsResponse(r *kmsg.ApiVersionsResponse) *Versions {
	var v Versions
	for _, key := range r.ApiKeys {
		v.SetMaxKeyVersion(key.ApiKey, key.MaxVersion)
	}
	return &v
}

// HasKey returns true if the versions contains the given key.
func (vs *Versions) HasKey(k int16) bool {
	_, has := vs.LookupMaxKeyVersion(k)
	return has
}

// LookupMaxKeyVersion returns the version for the given key and whether the
// key exists. If the key does not exist, this returns (-1, false).
func (vs *Versions) LookupMaxKeyVersion(k int16) (int16, bool) {
	if k < 0 {
		return -1, false
	}
	if int(k) >= len(vs.k2v) {
		return -1, false
	}
	version := vs.k2v[k]
	if version < 0 {
		return -1, false
	}
	return version, true
}

// SetMaxKeyVersion sets the max version for the given key.
//
// Setting a version to -1 unsets the key.
//
// Versions are backed by a slice; if the slice is not long enough, it is
// extended to fit the key.
func (vs *Versions) SetMaxKeyVersion(k, v int16) {
	if v < 0 {
		v = -1
	}
	// If the version is < 0, we are unsetting a version. If we are
	// unsetting a version that is more than the amount of keys we already
	// have, we have no reason to unset.
	if k < 0 || v < 0 && int(k) >= len(vs.k2v)+1 {
		return
	}
	needLen := int(k) + 1
	for len(vs.k2v) < needLen {
		vs.k2v = append(vs.k2v, -1)
	}
	vs.k2v[k] = v
}

// Equal returns whether two versions are equal.
func (vs *Versions) Equal(other *Versions) bool {
	// We allow the version slices to be of different lengths, so long as
	// the versions for keys in one and not the other are -1.
	//
	// Basically, all non-negative-one keys must be equal.
	long, short := vs.k2v, other.k2v
	if len(short) > len(long) {
		long, short = short, long
	}
	for i, v := range short {
		if v != long[i] {
			return false
		}
	}
	for _, v := range long[len(short):] {
		if v >= 0 {
			return false
		}
	}
	return true
}

// EachMaxKeyVersion calls fn for each key and max version
func (vs *Versions) EachMaxKeyVersion(fn func(k, v int16)) {
	for k, v := range vs.k2v {
		if v >= 0 {
			fn(int16(k), v)
		}
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

// TryRaftBroker changes from guessing the version for a classical ZooKeeper
// based broker to guessing for a raft based broker (v2.8+).
//
// Note that with raft, there can be a TryRaftController attempt as well.
func TryRaftBroker() VersionGuessOpt {
	return guessOpt{func(cfg *guessCfg) { cfg.listener = rBroker }}
}

// TryRaftController changes from guessing the version for a classical
// ZooKeeper based broker to guessing for a raft based controller broker
// (v2.8+).
//
// Note that with raft, there can be a TryRaftBroker attempt as well. Odds are
// that if you are an end user speaking to a raft based Kafka cluster, you are
// speaking to a raft broker. The controller is specifically for broker to
// broker communication.
func TryRaftController() VersionGuessOpt {
	return guessOpt{func(cfg *guessCfg) { cfg.listener = rController }}
}

type guessCfg struct {
	skipKeys []int16
	listener listener
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
	standard := vs.versionGuess(opts...)
	raftBroker := vs.versionGuess(append(opts, TryRaftBroker())...)
	raftController := vs.versionGuess(append(opts, TryRaftController())...)

	// If any of these are exact, return the exact guess.
	for _, g := range []guess{
		standard,
		raftBroker,
		raftController,
	} {
		if g.how == guessExact {
			return g.String()
		}
	}

	// If any are atLeast, that means it is newer than we can guess and we
	// return the highest version.
	for _, g := range []guess{
		standard,
		raftBroker,
		raftController,
	} {
		if g.how == guessAtLeast {
			return g.String()
		}
	}

	// This is a custom version. We could do some advanced logic to try to
	// return highest of all three guesses, but that may be inaccurate:
	// KRaft may detect a higher guess because not all requests exist in
	// KRaft. Instead, we just return our standard guess.
	return standard.String()
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

func (vs *Versions) versionGuess(opts ...VersionGuessOpt) guess {
	cfg := guessCfg{
		listener: zkBroker,
		// Envelope was added in 2.7 for kraft and zkBroker in 3.4; we
		// need to skip it for 2.7 through 3.4 otherwise the version
		// detection fails. We can just skip it generally since there
		// are enough differentiating factors that accurately detecting
		// envelope doesn't matter.
		//
		// TODO: add introduced-version to differentiate some specific
		// keys.
		skipKeys: []int16{4, 5, 6, 7, 27, 52, 53, 54, 55, 56, 57, 58, 59, 62, 63, 64, 67, 74, 75},
	}
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	skip := make(map[int16]bool, len(cfg.skipKeys))
	for _, k := range cfg.skipKeys {
		skip[k] = true
	}

	var last string
	cmp := make(map[int16]int16, len(maxTip))
	cmpskip := make(map[int16]int16)
	for _, comparison := range []struct {
		cmp  listenerKeys
		name string
	}{
		{max080, "v0.8.0"},
		{max081, "v0.8.1"},
		{max082, "v0.8.2"},
		{max090, "v0.9.0"},
		{max0100, "v0.10.0"},
		{max0101, "v0.10.1"},
		{max0102, "v0.10.2"},
		{max0110, "v0.11.0"},
		{max100, "v1.0"},
		{max110, "v1.1"},
		{max200, "v2.0"},
		{max210, "v2.1"},
		{max220, "v2.2"},
		{max230, "v2.3"},
		{max240, "v2.4"},
		{max250, "v2.5"},
		{max260, "v2.6"},
		{max270, "v2.7"},
		{max280, "v2.8"},
		{max300, "v3.0"},
		{max310, "v3.1"},
		{max320, "v3.2"},
		{max330, "v3.3"},
		{max340, "v3.4"},
		{max350, "v3.5"},
		{max360, "v3.6"},
		{max370, "v3.7"},
		{max380, "v3.8"},
	} {
		for k, v := range comparison.cmp.filter(cfg.listener) {
			if v == -1 {
				continue
			}
			k16 := int16(k)
			if skip[k16] {
				cmpskip[k16] = v
			} else {
				cmp[k16] = v
			}
		}

		var under, equal, over bool

		for k, v := range vs.k2v {
			k16 := int16(k)
			if skip[k16] {
				skipv, ok := cmpskip[k16]
				if v == -1 || !ok {
					continue
				}
				cmp[k16] = skipv
			}
			cmpv, has := cmp[k16]
			if has {
				// If our version for this key is less than the
				// comparison versions, then we are less than what we
				// are comparing.
				if v < cmpv {
					under = true
				} else if v > cmpv {
					// Similarly, if our version is more, then we
					// are over what we are comparing.
					over = true
				} else {
					equal = true
				}
				delete(cmp, k16)
			} else if v >= 0 {
				// If what we are comparing to does not even have this
				// key **and** our version is larger non-zero, then our
				// version is larger than what we are comparing to.
				//
				// We can have a negative version if a key was manually
				// unset.
				over = true
			}
			// If the version is < 0, the key is unset.
		}

		// If our versions did not clear out what we are comparing against, we
		// do not have all keys that we need for this version.
		if len(cmp) > 0 {
			under = true
		}

		current := comparison.name
		switch {
		case under && over:
			// Regardless of equal being true or not, this is a custom version.
			if last != "" {
				return guess{v1: last, how: guessCustomAtLeast}
			}
			return guess{v1: last, how: guessCustomUnknown}

		case under:
			// Regardless of equal being true or not, we have not yet hit
			// this version.
			if last != "" {
				return guess{v1: last, v2: current, how: guessBetween}
			}
			return guess{v1: current, how: guessNotEven}

		case over:
			// Regardless of equal being true or not, we try again.
			last = current

		case equal:
			return guess{v1: current, how: guessExact}
		}
		// At least one of under, equal, or over must be true, so there
		// is no default case.
	}

	return guess{v1: last, how: guessAtLeast}
}

// String returns a string representation of the versions; the format may
// change.
func (vs *Versions) String() string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	for k, v := range vs.k2v {
		if v < 0 {
			continue
		}
		name := kmsg.NameForKey(int16(k))
		if name == "" {
			name = "Unknown"
		}
		fmt.Fprintf(w, "%s\t%d\n", name, v)
	}
	w.Flush()
	return buf.String()
}

// Stable is a shortcut for the latest _released_ Kafka versions.
//
// This is the default version used in kgo to avoid breaking tip changes.
func Stable() *Versions { return zkBrokerOf(maxStable) }

// Tip is the latest defined Kafka key versions; this may be slightly out of date.
func Tip() *Versions { return zkBrokerOf(maxTip) }

func V0_8_0() *Versions  { return zkBrokerOf(max080) }
func V0_8_1() *Versions  { return zkBrokerOf(max081) }
func V0_8_2() *Versions  { return zkBrokerOf(max082) }
func V0_9_0() *Versions  { return zkBrokerOf(max090) }
func V0_10_0() *Versions { return zkBrokerOf(max0100) }
func V0_10_1() *Versions { return zkBrokerOf(max0101) }
func V0_10_2() *Versions { return zkBrokerOf(max0102) }
func V0_11_0() *Versions { return zkBrokerOf(max0110) }
func V1_0_0() *Versions  { return zkBrokerOf(max100) }
func V1_1_0() *Versions  { return zkBrokerOf(max110) }
func V2_0_0() *Versions  { return zkBrokerOf(max200) }
func V2_1_0() *Versions  { return zkBrokerOf(max210) }
func V2_2_0() *Versions  { return zkBrokerOf(max220) }
func V2_3_0() *Versions  { return zkBrokerOf(max230) }
func V2_4_0() *Versions  { return zkBrokerOf(max240) }
func V2_5_0() *Versions  { return zkBrokerOf(max250) }
func V2_6_0() *Versions  { return zkBrokerOf(max260) }
func V2_7_0() *Versions  { return zkBrokerOf(max270) }
func V2_8_0() *Versions  { return zkBrokerOf(max280) }
func V3_0_0() *Versions  { return zkBrokerOf(max300) }
func V3_1_0() *Versions  { return zkBrokerOf(max310) }
func V3_2_0() *Versions  { return zkBrokerOf(max320) }
func V3_3_0() *Versions  { return zkBrokerOf(max330) }
func V3_4_0() *Versions  { return zkBrokerOf(max340) }
func V3_5_0() *Versions  { return zkBrokerOf(max350) }
func V3_6_0() *Versions  { return zkBrokerOf(max360) }
func V3_7_0() *Versions  { return zkBrokerOf(max370) }
func V3_8_0() *Versions  { return zkBrokerOf(max380) }

func zkBrokerOf(lks listenerKeys) *Versions {
	return &Versions{lks.filter(zkBroker)}
}

type listener uint8

func (l listener) has(target listener) bool {
	return l&target != 0
}

const (
	zkBroker listener = 1 << iota
	rBroker
	rController
)

type listenerKey struct {
	listener listener
	version  int16
}

type listenerKeys []listenerKey

func (lks listenerKeys) filter(listener listener) []int16 {
	r := make([]int16, 0, len(lks))
	for _, lk := range lks {
		if lk.listener.has(listener) {
			r = append(r, lk.version)
		} else {
			r = append(r, -1)
		}
	}
	return r
}

// All requests before KRaft started being introduced support the zkBroker, but
// KRaft changed that. Kafka commit 698319b8e2c1f6cb574f339eede6f2a5b1919b55
// added which listeners support which API keys.
func k(listeners ...listener) listenerKey {
	var k listenerKey
	for _, listener := range listeners {
		k.listener |= listener
	}
	return k
}

func (l *listenerKey) inc() {
	l.version++
}

// For the comments below, appends are annotated with the key being introduced,
// while incs are annotated with the version the inc results in.

func nextMax(prev listenerKeys, do func(listenerKeys) listenerKeys) listenerKeys {
	return do(append(listenerKeys(nil), prev...))
}

var max080 = nextMax(nil, func(listenerKeys) listenerKeys {
	return listenerKeys{
		k(zkBroker, rBroker),              // 0 produce
		k(zkBroker, rBroker, rController), // 1 fetch
		k(zkBroker, rBroker),              // 2 list offset
		k(zkBroker, rBroker),              // 3 metadata
		k(zkBroker),                       // 4 leader and isr
		k(zkBroker),                       // 5 stop replica
		k(zkBroker),                       // 6 update metadata, actually not supported for a bit
		k(zkBroker, rController),          // 7 controlled shutdown, actually not supported for a bit
	}
})

var max081 = nextMax(max080, func(v listenerKeys) listenerKeys {
	return append(v,
		k(zkBroker, rBroker), // 8 offset commit KAFKA-965 db37ed0054
		k(zkBroker, rBroker), // 9 offset fetch (same)
	)
})

var max082 = nextMax(max081, func(v listenerKeys) listenerKeys {
	v[8].inc() // 1 offset commit KAFKA-1462
	v[9].inc() // 1 offset fetch KAFKA-1841 161b1aa16e I think?
	return append(v,
		k(zkBroker, rBroker), // 10 find coordinator KAFKA-1012 a670537aa3
		k(zkBroker, rBroker), // 11 join group (same)
		k(zkBroker, rBroker), // 12 heartbeat (same)
	)
})

var max090 = nextMax(max082, func(v listenerKeys) listenerKeys {
	v[0].inc() // 1 produce KAFKA-2136 436b7ddc38; KAFKA-2083 ?? KIP-13
	v[1].inc() // 1 fetch (same)
	v[6].inc() // 1 update metadata KAFKA-2411 d02ca36ca1
	v[7].inc() // 1 controlled shutdown (same)
	v[8].inc() // 2 offset commit KAFKA-1634
	return append(v,
		k(zkBroker, rBroker), // 13 leave group KAFKA-2397 636e14a991
		k(zkBroker, rBroker), // 14 sync group KAFKA-2464 86eb74d923
		k(zkBroker, rBroker), // 15 describe groups KAFKA-2687 596c203af1
		k(zkBroker, rBroker), // 16 list groups KAFKA-2687 596c203af1
	)
})

var max0100 = nextMax(max090, func(v listenerKeys) listenerKeys {
	v[0].inc() // 2 produce KAFKA-3025 45c8195fa1 KIP-31 KIP-32
	v[1].inc() // 2 fetch (same)
	v[3].inc() // 1 metadata KAFKA-3306 33d745e2dc
	v[6].inc() // 2 update metadata KAFKA-1215 951e30adc6
	return append(v,
		k(zkBroker, rBroker, rController), // 17 sasl handshake KAFKA-3149 5b375d7bf9
		k(zkBroker, rBroker, rController), // 18 api versions KAFKA-3307 8407dac6ee
	)
})

var max0101 = nextMax(max0100, func(v listenerKeys) listenerKeys {
	v[1].inc()  // 3 fetch KAFKA-2063 d04b0998c0 KIP-74
	v[2].inc()  // 1 list offset KAFKA-4148 eaaa433fc9 KIP-79
	v[3].inc()  // 2 metadata KAFKA-4093 ecc1fb10fa KIP-78
	v[11].inc() // 1 join group KAFKA-3888 40b1dd3f49 KIP-62
	return append(v,
		k(zkBroker, rBroker, rController), // 19 create topics KAFKA-2945 fc47b9fa6b
		k(zkBroker, rBroker, rController), // 20 delete topics KAFKA-2946 539633ba0e
	)
})

var max0102 = nextMax(max0101, func(v listenerKeys) listenerKeys {
	v[6].inc()  // 3 update metadata KAFKA-4565 d25671884b KIP-103
	v[19].inc() // 1 create topics KAFKA-4591 da57bc27e7 KIP-108
	return v
})

var max0110 = nextMax(max0102, func(v listenerKeys) listenerKeys {
	v[0].inc()  // 3 produce KAFKA-4816 5bd06f1d54 KIP-98
	v[1].inc()  // 4 fetch (same)
	v[1].inc()  // 5 fetch KAFKA-4586 8b05ad406d KIP-107
	v[3].inc()  // 4 metadata KAFKA-5291 7311dcbc53 (3 below)
	v[9].inc()  // 2 offset fetch KAFKA-3853 c2d9b95f36 KIP-98
	v[10].inc() // 1 find coordinator KAFKA-5043 d0e7c6b930 KIP-98
	v = append(v,
		k(zkBroker, rBroker), // 21 delete records KAFKA-4586 see above
		k(zkBroker, rBroker), // 22 init producer id KAFKA-4817 bdf4cba047 KIP-98 (raft added in KAFKA-12620 e97cff2702b6ba836c7925caa36ab18066a7c95d KIP-730)
		k(zkBroker, rBroker), // 23 offset for leader epoch KAFKA-1211 0baea2ac13 KIP-101

		k(zkBroker, rBroker), // 24 add partitions to txn KAFKA-4990 865d82af2c KIP-98 (raft 3.0 6e857c531f14d07d5b05f174e6063a124c917324)
		k(zkBroker, rBroker), // 25 add offsets to txn (same, same raft)
		k(zkBroker, rBroker), // 26 end txn (same, same raft)
		k(zkBroker, rBroker), // 27 write txn markers (same)
		k(zkBroker, rBroker), // 28 txn offset commit (same, same raft)

		// raft broker / controller added in 5b0c58ed53c420e93957369516f34346580dac95
		k(zkBroker, rBroker, rController), // 29 describe acls KAFKA-3266 9815e18fef KIP-140
		k(zkBroker, rBroker, rController), // 30 create acls (same)
		k(zkBroker, rBroker, rController), // 31 delete acls (same)

		k(zkBroker, rBroker),              // 32 describe configs KAFKA-3267 972b754536 KIP-133
		k(zkBroker, rBroker, rController), // 33 alter configs (same) (raft broker 3.0 6e857c531f14d07d5b05f174e6063a124c917324, controller 273d66479dbee2398b09e478ffaf996498d1ab34)
	)

	// KAFKA-4954 0104b657a1 KIP-124
	v[2].inc()  // 2 list offset (reused in e71dce89c0 KIP-98)
	v[3].inc()  // 3 metadata
	v[8].inc()  // 3 offset commit
	v[9].inc()  // 3 offset fetch
	v[11].inc() // 2 join group
	v[12].inc() // 1 heartbeat
	v[13].inc() // 1 leave group
	v[14].inc() // 1 sync group
	v[15].inc() // 1 describe groups
	v[16].inc() // 1 list group
	v[18].inc() // 1 api versions
	v[19].inc() // 2 create topics
	v[20].inc() // 1 delete topics

	return v
})

var max100 = nextMax(max0110, func(v listenerKeys) listenerKeys {
	v[0].inc() // 4 produce KAFKA-4763 fc93fb4b61 KIP-112
	v[1].inc() // 6 fetch (same)
	v[3].inc() // 5 metadata (same)
	v[4].inc() // 1 leader and isr (same)
	v[6].inc() // 4 update metadata (same)

	v[0].inc()  // 5 produce KAFKA-5793 94692288be
	v[17].inc() // 1 sasl handshake KAFKA-4764 8fca432223 KIP-152

	return append(v,
		k(zkBroker, rBroker),              // 34 alter replica log dirs KAFKA-5694 adefc8ea07 KIP-113
		k(zkBroker, rBroker),              // 35 describe log dirs (same)
		k(zkBroker, rBroker, rController), // 36 sasl authenticate KAFKA-4764 (see above)
		k(zkBroker, rBroker, rController), // 37 create partitions KAFKA-5856 5f6393f9b1 KIP-195 (raft 3.0 6e857c531f14d07d5b05f174e6063a124c917324)
	)
})

var max110 = nextMax(max100, func(v listenerKeys) listenerKeys {
	v = append(v,
		k(zkBroker),          // 38 create delegation token KAFKA-4541 27a8d0f9e7 under KAFKA-1696 KIP-48
		k(zkBroker),          // 39 renew delegation token (same)
		k(zkBroker),          // 40 expire delegation token (same)
		k(zkBroker),          // 41 describe delegation token (same)
		k(zkBroker, rBroker), // 42 delete groups KAFKA-6275 1ed6da7cc8 KIP-229
	)

	v[1].inc()  // 7 fetch KAFKA-6254 7fe1c2b3d3 KIP-227
	v[32].inc() // 1 describe configs KAFKA-6241 b814a16b96 KIP-226

	return v
})

var max200 = nextMax(max110, func(v listenerKeys) listenerKeys {
	v[0].inc()  // 6 produce KAFKA-6028 1facab387f KIP-219
	v[1].inc()  // 8 fetch (same)
	v[2].inc()  // 3 list offset (same)
	v[3].inc()  // 6 metadata (same)
	v[8].inc()  // 4 offset commit (same)
	v[9].inc()  // 4 offset fetch (same)
	v[10].inc() // 2 find coordinator (same)
	v[11].inc() // 3 join group (same)
	v[12].inc() // 2 heartbeat (same)
	v[13].inc() // 2 leave group (same)
	v[14].inc() // 2 sync group (same)
	v[15].inc() // 2 describe groups (same)
	v[16].inc() // 2 list group (same)
	v[18].inc() // 2 api versions (same)
	v[19].inc() // 3 create topics (same)
	v[20].inc() // 2 delete topics (same)
	v[21].inc() // 1 delete records (same)
	v[22].inc() // 1 init producer id (same)
	v[24].inc() // 1 add partitions to txn (same)
	v[25].inc() // 1 add offsets to txn (same)
	v[26].inc() // 1 end txn (same)
	v[28].inc() // 1 txn offset commit (same)
	// 29, 30, 31 bumped below, but also had throttle changes
	v[32].inc() // 2 describe configs (same)
	v[33].inc() // 1 alter configs (same)
	v[34].inc() // 1 alter replica log dirs (same)
	v[35].inc() // 1 describe log dirs (same)
	v[37].inc() // 1 create partitions (same)
	v[38].inc() // 1 create delegation token (same)
	v[39].inc() // 1 renew delegation token (same)
	v[40].inc() // 1 expire delegation token (same)
	v[41].inc() // 1 describe delegation token (same)
	v[42].inc() // 1 delete groups (same)

	v[29].inc() // 1 describe acls KAFKA-6841 b3aa655a70 KIP-290
	v[30].inc() // 1 create acls (same)
	v[31].inc() // 1 delete acls (same)

	v[23].inc() // 1 offset for leader epoch KAFKA-6361 9679c44d2b KIP-279
	return v
})

var max210 = nextMax(max200, func(v listenerKeys) listenerKeys {
	v[8].inc() // 5 offset commit KAFKA-4682 418a91b5d4 KIP-211

	v[20].inc() // 3 delete topics KAFKA-5975 04770916a7 KIP-322

	v[1].inc()  // 9 fetch KAFKA-7333 05ba5aa008 KIP-320
	v[2].inc()  // 4 list offset (same)
	v[3].inc()  // 7 metadata (same)
	v[8].inc()  // 6 offset commit (same)
	v[9].inc()  // 5 offset fetch (same)
	v[23].inc() // 2 offset for leader epoch (same, also in Kafka PR #5635 79ad9026a6)
	v[28].inc() // 2 txn offset commit (same)

	v[0].inc() // 7 produce KAFKA-4514 741cb761c5 KIP-110
	v[1].inc() // 10 fetch (same)
	return v
})

var max220 = nextMax(max210, func(v listenerKeys) listenerKeys {
	v[2].inc()  // 5 list offset KAFKA-2334 152292994e KIP-207
	v[11].inc() // 4 join group KAFKA-7824 9a9310d074 KIP-394
	v[36].inc() // 1 sasl authenticate KAFKA-7352 e8a3bc7425 KIP-368

	v[4].inc() // 2 leader and isr KAFKA-7235 2155c6d54b KIP-380
	v[5].inc() // 1 stop replica (same)
	v[6].inc() // 5 update metadata (same)
	v[7].inc() // 2 controlled shutdown (same)

	return append(v,
		k(zkBroker, rBroker, rController), // 43 elect preferred leaders KAFKA-5692 269b65279c KIP-183 (raft 3.0 6e857c531f14d07d5b05f174e6063a124c917324)
	)
})

var max230 = nextMax(max220, func(v listenerKeys) listenerKeys {
	v[3].inc()  // 8 metadata KAFKA-7922 a42f16f980 KIP-430
	v[15].inc() // 3 describe groups KAFKA-7922 f11fa5ef40 KIP-430

	v[1].inc()  // 11 fetch KAFKA-8365 e2847e8603 KIP-392
	v[23].inc() // 3 offset for leader epoch (same)

	v[11].inc() // 5 join group KAFKA-7862 0f995ba6be KIP-345
	v[8].inc()  // 7 offset commit KAFKA-8225 9fa331b811 KIP-345
	v[12].inc() // 3 heartbeat (same)
	v[14].inc() // 3 sync group (same)

	return append(v,
		k(zkBroker, rBroker, rController), // 44 incremental alter configs KAFKA-7466 3b1524c5df KIP-339
	)
})

var max240 = nextMax(max230, func(v listenerKeys) listenerKeys {
	v[4].inc()  // 3 leader and isr KAFKA-8345 81900d0ba0 KIP-455
	v[15].inc() // 4 describe groups KAFKA-8538 f8db022b08 KIP-345
	v[19].inc() // 4 create topics KAFKA-8305 8e161580b8 KIP-464
	v[43].inc() // 1 elect preferred leaders KAFKA-8286 121308cc7a KIP-460
	v = append(v,
		// raft added in e07de97a4ce730a2755db7eeacb9b3e1f69a12c8 for the following two
		k(zkBroker, rBroker, rController), // 45 alter partition reassignments KAFKA-8345 81900d0ba0 KIP-455
		k(zkBroker, rBroker, rController), // 46 list partition reassignments (same)

		k(zkBroker, rBroker), // 47 offset delete KAFKA-8730 e24d0e22ab KIP-496
	)

	v[13].inc() // 3 leave group KAFKA-8221 74c90f46c3 KIP-345

	// introducing flexible versions; 24 were bumped
	v[3].inc()  // 9 metadata KAFKA-8885 apache/kafka#7325 KIP-482
	v[4].inc()  // 4 leader and isr (same)
	v[5].inc()  // 2 stop replica (same)
	v[6].inc()  // 6 update metadata (same)
	v[7].inc()  // 3 controlled shutdown (same)
	v[8].inc()  // 8 offset commit (same)
	v[9].inc()  // 6 offset fetch (same)
	v[10].inc() // 3 find coordinator (same)
	v[11].inc() // 6 join group (same)
	v[12].inc() // 4 heartbeat (same)
	v[13].inc() // 4 leave group (same)
	v[14].inc() // 4 sync group (same)
	v[15].inc() // 5 describe groups (same)
	v[16].inc() // 3 list group (same)
	v[18].inc() // 3 api versions (same, also KIP-511 [non-flexible fields added])
	v[19].inc() // 5 create topics (same)
	v[20].inc() // 4 delete topics (same)
	v[22].inc() // 2 init producer id (same)
	v[38].inc() // 2 create delegation token (same)
	v[42].inc() // 2 delete groups (same)
	v[43].inc() // 2 elect preferred leaders (same)
	v[44].inc() // 1 incremental alter configs (same)
	// also 45, 46; not bumped since in same release

	// Create topics (19) was bumped up to 5 in KAFKA-8907 5d0052fe00
	// KIP-525, then 6 in the above bump, then back down to 5 once the
	// tagged PR was merged (KAFKA-8932 1f1179ea64 for the bump down).

	v[0].inc() // 8 produce KAFKA-8729 f6f24c4700 KIP-467

	return v
})

var max250 = nextMax(max240, func(v listenerKeys) listenerKeys {
	v[22].inc() // 3 init producer id KAFKA-8710 fecb977b25 KIP-360
	v[9].inc()  // 7 offset fetch KAFKA-9346 6da70f9b95 KIP-447

	// more flexible versions, KAFKA-9420 0a2569e2b99 KIP-482
	// 6 bumped, then sasl handshake reverted later in 1a8dcffe4
	v[36].inc() // 2 sasl authenticate
	v[37].inc() // 2 create partitions
	v[39].inc() // 2 renew delegation token
	v[40].inc() // 2 expire delegation token
	v[41].inc() // 2 describe delegation token

	v[28].inc() // 3 txn offset commit KAFKA-9365 ed7c071e07f KIP-447

	v[29].inc() // 2 describe acls KAFKA-9026 40b35178e5 KIP-482 (for flexible versions)
	v[30].inc() // 2 create acls KAFKA-9027 738e14edb KIP-482 (flexible)
	v[31].inc() // 2 delete acls KAFKA-9028 738e14edb KIP-482 (flexible)

	v[11].inc() // 7 join group KAFKA-9437 96c4ce480 KIP-559
	v[14].inc() // 5 sync group (same)

	return v
})

var max260 = nextMax(max250, func(v listenerKeys) listenerKeys {
	v[21].inc() // 2 delete records KAFKA-8768 f869e33ab KIP-482 (opportunistic bump for flexible versions)
	v[35].inc() // 2 describe log dirs KAFKA-9435 4f1e8331ff9 KIP-482 (same)

	v = append(v,
		k(zkBroker, rBroker),              // 48 describe client quotas KAFKA-7740 227a7322b KIP-546 (raft in 5964401bf9aab611bd4a072941bd1c927e044258)
		k(zkBroker, rBroker, rController), // 49 alter client quotas (same)
	)

	v[5].inc() // 3 stop replica KAFKA-9539 7c7d55dbd KIP-570

	v[16].inc() // 4 list group KAFKA-9130 fe948d39e KIP-518
	v[32].inc() // 3 describe configs KAFKA-9494 af3b8b50f2 KIP-569

	return v
})

var max270 = nextMax(max260, func(v listenerKeys) listenerKeys {
	// KAFKA-10163 a5ffd1ca44c KIP-599
	v[37].inc() // 3 create partitions
	v[19].inc() // 6 create topics (same)
	v[20].inc() // 5 delete topics (same)

	// KAFKA-9911 b937ec7567 KIP-588
	v[22].inc() // 4 init producer id
	v[24].inc() // 2 add partitions to txn
	v[25].inc() // 2 add offsets to txn
	v[26].inc() // 2 end txn

	v = append(v,
		k(zkBroker, rBroker, rController), // 50 describe user scram creds, KAFKA-10259 e8524ccd8fca0caac79b844d87e98e9c055f76fb KIP-554; 38c409cf33c kraft
		k(zkBroker, rBroker, rController), // 51 alter user scram creds, same
	)

	// KAFKA-10435 634c9175054cc69d10b6da22ea1e95edff6a4747 KIP-595
	// This opted in fetch request to flexible versions.
	//
	// KAFKA-10487: further change in aa5263fba903c85812c0c31443f7d49ee371e9db
	v[1].inc() // 12 fetch

	// KAFKA-10492 b7c8490cf47b0c18253d6a776b2b35c76c71c65d KIP-595
	//
	// These are the first requests that are raft only.
	v = append(v,
		k(rController),          // 52 vote
		k(rController),          // 53 begin quorum epoch
		k(rController),          // 54 end quorum epoch
		k(rBroker, rController), // 55 describe quorum
	)

	// KAFKA-8836 57de67db22eb373f92ec5dd449d317ed2bc8b8d1 KIP-497
	v = append(v,
		k(zkBroker, rController), // 56 alter isr
	)

	// KAFKA-10028 fb4f297207ef62f71e4a6d2d0dac75752933043d KIP-584
	return append(v,
		k(zkBroker, rBroker, rController), // 57 update features (rbroker 3.0 6e857c531f14d07d5b05f174e6063a124c917324; rcontroller 3.2 55ff5d360381af370fe5b3a215831beac49571a4 KIP-778  KAFKA-13823)
	)
})

var max280 = nextMax(max270, func(v listenerKeys) listenerKeys {
	// KAFKA-10181 KAFKA-10181 KIP-590
	v = append(v,
		k(zkBroker, rController), // 58 envelope, controller first, zk in KAFKA-14446 8b045dcbf6b89e1a9594ff95642d4882765e4b0d KIP-866 Kafka 3.4
	)

	// KAFKA-10729 85f94d50271c952c3e9ee49c4fc814c0da411618 KIP-482
	// (flexible bumps)
	v[0].inc()  // 9 produce
	v[2].inc()  // 6 list offsets
	v[23].inc() // 4 offset for leader epoch
	v[24].inc() // 3 add partitions to txn
	v[25].inc() // 3 add offsets to txn
	v[26].inc() // 3 end txn
	v[27].inc() // 1 write txn markers
	v[32].inc() // 4 describe configs
	v[33].inc() // 2 alter configs
	v[34].inc() // 2 alter replica log dirs
	v[48].inc() // 1 describe client quotas
	v[49].inc() // 1 alter client quotas

	// KAFKA-10547 5c921afa4a593478f7d1c49e5db9d787558d0d5e KIP-516
	v[3].inc() // 10 metadata
	v[6].inc() // 7 update metadata

	// KAFKA-10545 1dd1e7f945d7a8c1dc177223cd88800680f1ff46 KIP-516
	v[4].inc() // 5 leader and isr

	// KAFKA-10427 2023aed59d863278a6302e03066d387f994f085c KIP-630
	v = append(v,
		k(rController), // 59 fetch snapshot
	)

	// KAFKA-12204 / KAFKA-10851 302eee63c479fd4b955c44f1058a5e5d111acb57 KIP-700
	v = append(v,
		k(zkBroker, rBroker, rController), // 60 describe cluster; rController in KAFKA-15396 41b695b6e30baa4243d9ca4f359b833e17ed0e77 KIP-919
	)

	// KAFKA-12212 7a1d1d9a69a241efd68e572badee999229b3942f KIP-700
	v[3].inc() // 11 metadata

	// KAFKA-10764 4f588f7ca2a1c5e8dd845863da81425ac69bac92 KIP-516
	v[19].inc() // 7 create topics
	v[20].inc() // 6 delete topics

	// KAFKA-12238 e9edf104866822d9e6c3b637ffbf338767b5bf27 KIP-664
	v = append(v,
		k(zkBroker, rBroker), // 61 describe producers
	)

	// KAFKA-12248 a022072df3c8175950c03263d2bbf2e3ea7a7a5d KIP-500
	// (commit mentions KIP-500, these are actually described in KIP-631)
	// Broker registration was later updated in d9bb2ef596343da9402bff4903b129cff1f7c22b
	v = append(v,
		k(rController), // 62 broker registration
		k(rController), // 63 broker heartbeat
	)

	// KAFKA-12249 3f36f9a7ca153a9d221f6bedeb7d1503aa18eff1 KIP-500 / KIP-631
	// Renamed from Decommission to Unregister in 06dce721ec0185d49fac37775dbf191d0e80e687
	v = append(v,
		// kraft broker added in 7143267f71ca0c14957d8560fbc42a5f8aac564d
		k(rBroker, rController), // 64 unregister broker
	)
	return v
})

var max300 = nextMax(max280, func(v listenerKeys) listenerKeys {
	// KAFKA-12267 3f09fb97b6943c0612488dfa8e5eab8078fd7ca0 KIP-664
	v = append(v,
		k(zkBroker, rBroker), // 65 describe transactions
	)

	// KAFKA-12369 3708a7c6c1ecf1304f091dda1e79ae53ba2df489 KIP-664
	v = append(v,
		k(zkBroker, rBroker), // 66 list transactions
	)

	// KAFKA-12620 72d108274c98dca44514007254552481c731c958 KIP-730
	// raft broker added in  e97cff2702b6ba836c7925caa36ab18066a7c95d
	v = append(v,
		k(zkBroker, rController), // 67 allocate producer ids
	)

	// KAFKA-12541 bd72ef1bf1e40feb3bc17349a385b479fa5fa530 KIP-734
	v[2].inc() // 7 list offsets

	// KAFKA-12663 f5d5f654db359af077088685e29fbe5ea69616cf KIP-699
	v[10].inc() // 4 find coordinator

	// KAFKA-12234 e00c0f3719ad0803620752159ef8315d668735d6 KIP-709
	v[9].inc() // 8 offset fetch

	return v
})

var max310 = nextMax(max300, func(v listenerKeys) listenerKeys {
	// KAFKA-10580 2b8aff58b575c199ee8372e5689420c9d77357a5 KIP-516
	v[1].inc() // 13 fetch

	// KAFKA-10744 1d22b0d70686aef5689b775ea2ea7610a37f3e8c KIP-516
	v[3].inc() // 12 metadata

	return v
})

var max320 = nextMax(max310, func(v listenerKeys) listenerKeys {
	// KAFKA-13495 69645f1fe5103adb00de6fa43152e7df989f3aea KIP-800
	v[11].inc() // 8 join group

	// KAFKA-13496 bf609694f83931990ce63e0123f811e6475820c5 KIP-800
	v[13].inc() // 5 leave group

	// KAFKA-13527 31fca1611a6780e8a8aa3ac21618135201718e32 KIP-784
	v[35].inc() // 3 describe log dirs

	// KAFKA-13435 c8fbe26f3bd3a7c018e7619deba002ee454208b9 KIP-814
	v[11].inc() // 9 join group

	// KAFKA-13587 52621613fd386203773ba93903abd50b46fa093a KIP-704
	v[4].inc()  // 6 leader and isr
	v[56].inc() // 1 alter isr => alter partition

	return v
})

var max330 = nextMax(max320, func(v listenerKeys) listenerKeys {
	// KAFKA-13823 55ff5d360381af370fe5b3a215831beac49571a4 KIP-778
	v[57].inc() // 1 update features

	// KAFKA-13958 4fcfd9ddc4a8da3d4cfbb69268c06763352e29a9 KIP-827
	v[35].inc() // 4 describe log dirs

	// KAFKA-841 f83d95d9a28 KIP-841
	v[56].inc() // 2 alter partition

	// KAFKA-13888 a126e3a622f KIP-836
	v[55].inc() // 1 describe quorum

	// KAFKA-6945 d65d8867983 KIP-373
	v[29].inc() // 3 describe acls
	v[30].inc() // 3 create acls
	v[31].inc() // 3 delete acls
	v[38].inc() // 3 create delegation token
	v[41].inc() // 3 describe delegation token

	return v
})

var max340 = nextMax(max330, func(v listenerKeys) listenerKeys {
	// KAFKA-14304 7b7e40a536a79cebf35cc278b9375c8352d342b9 KIP-866
	// KAFKA-14448 67c72596afe58363eceeb32084c5c04637a33831 added BrokerRegistration
	// KAFKA-14493 db490707606855c265bc938e1b236070e0e2eba5 changed BrokerRegistration
	// KAFKA-14304 0bb05d8679b684ad8fbb2eb40dfc00066186a75a changed BrokerRegistration back to a bool...
	// 5b521031edea8ea7cbcca7dc24a58429423740ff added tag to ApiVersions
	v[4].inc()  // 7 leader and isr
	v[5].inc()  // 4 stop replica
	v[6].inc()  // 8 update metadata
	v[62].inc() // 1 broker registration
	return v
})

var max350 = nextMax(max340, func(v listenerKeys) listenerKeys {
	// KAFKA-13369 7146ac57ba9ddd035dac992b9f188a8e7677c08d KIP-405
	v[1].inc() // 14 fetch
	v[2].inc() // 8 list offsets

	v[1].inc()  // 15 fetch // KAFKA-14617 79b5f7f1ce2 KIP-903
	v[56].inc() // 3 alter partition // KAFKA-14617 8c88cdb7186b1d594f991eb324356dcfcabdf18a KIP-903
	return v
})

var max360 = nextMax(max350, func(v listenerKeys) listenerKeys {
	// KAFKA-14402 29a1a16668d76a1cc04ec9e39ea13026f2dce1de KIP-890
	// Later commit swapped to stable
	v[24].inc() // 4 add partitions to txn
	return v
})

var max370 = nextMax(max360, func(v listenerKeys) listenerKeys {
	// KAFKA-15661 c8f687ac1505456cb568de2b60df235eb1ceb5f0 KIP-951
	v[0].inc() // 10 produce
	v[1].inc() // 16 fetch

	// 7826d5fc8ab695a5ad927338469ddc01b435a298 KIP-848
	// (change introduced in 3.6 but was marked unstable and not visible)
	v[8].inc() // 9 offset commit
	// KAFKA-14499 7054625c45dc6edb3c07271fe4a6c24b4638424f KIP-848 (and prior)
	v[9].inc() // 9 offset fetch

	// KAFKA-15368 41b695b6e30baa4243d9ca4f359b833e17ed0e77 KIP-919
	// (added rController as well, see above)
	v[60].inc() // 1 describe cluster

	// KAFKA-14391 3be7f7d611d0786f2f98159d5c7492b0d94a2bb7 KIP-848
	// as well as some patches following
	v = append(v,
		k(zkBroker, rBroker), // 68 consumer group heartbeat
	)

	return v
})

var max380 = nextMax(max370, func(v listenerKeys) listenerKeys {
	// KAFKA-16314 2e8d69b78ca52196decd851c8520798aa856c073 KIP-890
	// Then error rename in cf1ba099c0723f9cf65dda4cd334d36b7ede6327
	v[0].inc()  // 11 produce
	v[10].inc() // 5 find coordinator
	v[22].inc() // 5 init producer id
	v[24].inc() // 5 add partitions to txn
	v[25].inc() // 4 add offsets to txn
	v[26].inc() // 4 end txn
	v[28].inc() // 4 txn offset commit

	// KAFKA-15460 68745ef21a9d8fe0f37a8c5fbc7761a598718d46 KIP-848
	v[16].inc() // 5 list groups

	// KAFKA-14509 90e646052a17e3f6ec1a013d76c1e6af2fbb756e KIP-848 added
	// 7b0352f1bd9b923b79e60b18b40f570d4bfafcc0
	// b7c99e22a77392d6053fe231209e1de32b50a98b
	// 68389c244e720566aaa8443cd3fc0b9d2ec4bb7a
	// 5f410ceb04878ca44d2d007655155b5303a47907 stabilized
	v = append(v,
		k(zkBroker, rBroker), // 69 consumer group describe
	)

	// KAFKA-16265 b4e96913cc6c827968e47a31261e0bd8fdf677b5 KIP-994 (part 1)
	v[66].inc()

	return v
})

var (
	maxStable = max380
	maxTip    = nextMax(maxStable, func(v listenerKeys) listenerKeys {
		return v
	})
)
