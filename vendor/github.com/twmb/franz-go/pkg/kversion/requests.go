package kversion

import (
	"fmt"
	"maps"

	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	maj0_8  uint8 = 253
	maj0_9  uint8 = 254
	maj0_10 uint8 = 255
	maj0_11 uint8 = 0
)

type releaseKind uint8

const (
	kindBroker     releaseKind = 0
	kindController releaseKind = 1
	kindZk         releaseKind = 2
)

type release struct {
	major uint8
	minor uint8
	kind  releaseKind
	reqs  map[int16]req
	prior *release
}

func (r *release) clone(nextMajor, nextMinor uint8) *release {
	return &release{
		major: nextMajor,
		minor: nextMinor,
		kind:  r.kind,
		reqs:  maps.Clone(r.reqs),
		prior: r,
	}
}

func (r *release) name() string {
	switch r.major {
	case maj0_8, maj0_9, maj0_10, maj0_11:
		return fmt.Sprintf("v0.%d.%d", r.major+11, r.minor)
	default:
		return fmt.Sprintf("v%d.%d", r.major, r.minor)
	}
}

type req struct {
	key  int16
	vmin int16
	vmax int16
}

func (r *release) incmax(key, vmax int16) {
	req, ok := r.reqs[key]
	if !ok {
		panic(fmt.Sprintf("key %d does not yet exist to incmax", key))
	}
	if req.vmax+1 != vmax {
		panic(fmt.Sprintf("key %d next max %d != exp %d", key, req.vmax+1, vmax))
	}
	req.vmax++
	r.reqs[key] = req
}

func (r *release) addkey(key int16) {
	r.addkeyver(key, 0)
}

func (r *release) addkeyver(key, vmax int16) {
	if _, ok := r.reqs[key]; ok {
		panic(fmt.Sprintf("key %d already exists", key))
	}
	r.reqs[key] = req{key: key, vmax: vmax}
}

func reqsFromApiVersions(r *kmsg.ApiVersionsResponse) map[int16]req {
	m := make(map[int16]req, len(r.ApiKeys))
	for _, k := range r.ApiKeys {
		m[k.ApiKey] = req{
			key:  k.ApiKey,
			vmin: k.MinVersion,
			vmax: k.MaxVersion,
		}
	}
	return m
}

func (vs *Versions) versionGuess2(cmp *release, opts ...VersionGuessOpt) guess {
	// KRaft at 2.8 had two requests that were not in 3.0, which makes
	// version detection of 2.8 more difficult. We don't handle it
	// properly; KRaft 2.8 was feature preview only.
	cfg := guessCfg{
		// We skip:
		//  * (4)  LeaderAndISR
		//  * (5)  StopReplica
		//  * (6)  UpdateMetadata
		//  * (7)  ControlledShutdown
		//  * (27) WriteTxnMarkers
		//  * (56) AlterISR
		//  * (57) UpdateFeatures
		//  * (58) Envelope
		//  * (67) AllocateProducerIDs
		//  * (71) GetTelemetrySubscriptions
		//  * (72) PushTelemetry
		//
		// Most of these keys are broker-to-broker only requests, and
		// most non-Kafka implementations do not implement them.
		//
		// We skip 71 and 72 because telemetry requests are only
		// advertised if the broker is configured to support it.
		skipKeys: []int16{4, 5, 6, 7, 27, 56, 57, 58, 67, 71, 72},
	}
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	// For comparison checking, we only check the max key version.
	var higher *release
	for {
		for _, k := range cfg.skipKeys {
			delete(cmp.reqs, k)
			delete(vs.reqs, k)
		}

		var under, equal, over bool

		for k, req := range vs.reqs {
			cmpreq, ok := cmp.reqs[k]
			if ok {
				if req.vmax < cmpreq.vmax {
					under = true
				} else if req.vmax > cmpreq.vmax {
					over = true
				} else {
					equal = true
				}
				delete(cmp.reqs, k)
			} else {
				over = true // key we do not recognize: by definition the broker is higher than this cmp version
			}
		}

		// If our versions did not clear out what we are comparing against, we
		// do not have all keys that we need for this version.
		if len(cmp.reqs) > 0 {
			under = true
		}

		switch {
		case under && over:
			if cmp.prior == nil {
				return guess{v1: cmp.name(), how: guessCustomUnknown}
			}

		case under:
			if cmp.prior == nil {
				return guess{v1: cmp.name(), how: guessNotEven}
			}

		case over:
			if higher != nil {
				return guess{v1: cmp.name(), v2: higher.name(), how: guessBetween}
			}
			return guess{v1: cmp.name(), how: guessAtLeast}

		case equal:
			return guess{v1: cmp.name(), how: guessExact}
		}
		higher = cmp
		cmp = cmp.prior
	}
}

////////////////////////
// ZOOKEEPER VERSIONS //
////////////////////////

func z080() *release {
	return &release{
		major: maj0_8,
		minor: 0,
		kind:  kindZk,
		reqs: map[int16]req{
			0: {key: 0}, // 0 produce
			1: {key: 1}, // 1 fetch
			2: {key: 2}, // 2 list offset
			3: {key: 3}, // 3 metadata
			4: {key: 4}, // 4 leader and isr
			5: {key: 5}, // 5 stop replica
			6: {key: 6}, // 6 update metadata (actually not supported for a bit)
			7: {key: 7}, // 7 controlled shutdown, actually not supported for a bit
		},
	}
}

func z081() *release {
	prior := z080()
	now := prior.clone(maj0_8, 1)

	now.addkey(8) // 8 offset commit KAFKA-965 db37ed0054
	now.addkey(9) // 9 offset fetch (same)
	return now
}

func z082() *release {
	now := z081().clone(maj0_8, 2)

	now.incmax(8, 1) // 1 offset commit KAFKA-1462
	now.incmax(9, 1) // 1 offset fetch KAFKA-1841 161b1aa16e I think?

	now.addkey(10) // 10 find coordinator KAFKA-1012 a670537aa3
	now.addkey(11) // 11 join group (same)
	now.addkey(12) // 12 heartbeat (same)
	return now
}

func z090() *release {
	now := z082().clone(maj0_9, 0)

	now.incmax(0, 1) // 1 produce KAFKA-2136 436b7ddc38; KAFKA-2083 ?? KIP-13
	now.incmax(1, 1) // 1 fetch (same)
	now.incmax(6, 1) // 1 update metadata KAFKA-2411 d02ca36ca1
	now.incmax(7, 1) // 1 controlled shutdown (same)
	now.incmax(8, 2) // 2 offset commit KAFKA-1634

	now.addkey(13) // 13 leave group KAFKA-2397 636e14a991
	now.addkey(14) // 14 sync group KAFKA-2464 86eb74d923
	now.addkey(15) // 15 describe groups KAFKA-2687 596c203af1
	now.addkey(16) // 16 list groups KAFKA-2687 596c203af1
	return now
}

func z0100() *release {
	now := z090().clone(maj0_10, 0)

	now.incmax(0, 2) // 2 produce KAFKA-3025 45c8195fa1 KIP-31 KIP-32
	now.incmax(1, 2) // 2 fetch (same)
	now.incmax(3, 1) // 1 metadata KAFKA-3306 33d745e2dc
	now.incmax(6, 2) // 2 update metadata KAFKA-1215 951e30adc6

	now.addkey(17) // 17 sasl handshake KAFKA-3149 5b375d7bf9
	now.addkey(18) // 18 api versions KAFKA-3307 8407dac6ee
	return now
}

func z0101() *release {
	now := z0100().clone(maj0_10, 1)

	now.incmax(1, 3)  // 3 fetch KAFKA-2063 d04b0998c0 KIP-74
	now.incmax(2, 1)  // 1 list offset KAFKA-4148 eaaa433fc9 KIP-79
	now.incmax(3, 2)  // 2 metadata KAFKA-4093 ecc1fb10fa KIP-78
	now.incmax(11, 1) // 1 join group KAFKA-3888 40b1dd3f49 KIP-62

	now.addkey(19) // 19 create topics KAFKA-2945 fc47b9fa6b
	now.addkey(20) // 20 delete topics KAFKA-2946 539633ba0e
	return now
}

func z0102() *release {
	now := z0101().clone(maj0_10, 2)

	now.incmax(6, 3)  // 3 update metadata KAFKA-4565 d25671884b KIP-103
	now.incmax(19, 1) // 1 create topics KAFKA-4591 da57bc27e7 KIP-108
	return now
}

func z0110() *release {
	now := z0102().clone(maj0_11, 0)

	now.incmax(0, 3)  // 3 produce KAFKA-4816 5bd06f1d54 KIP-98
	now.incmax(1, 4)  // 4 fetch (same)
	now.incmax(1, 5)  // 5 fetch KAFKA-4586 8b05ad406d KIP-107
	now.incmax(9, 2)  // 2 offset fetch KAFKA-3853 c2d9b95f36 KIP-98
	now.incmax(10, 1) // 1 find coordinator KAFKA-5043 d0e7c6b930 KIP-98

	now.addkey(21) // 21 delete records KAFKA-4586 see above
	now.addkey(22) // 22 init producer id KAFKA-4817 bdf4cba047 KIP-98 (raft added in KAFKA-12620 e97cff2702b6ba836c7925caa36ab18066a7c95d KIP-730)
	now.addkey(23) // 23 offset for leader epoch KAFKA-1211 0baea2ac13 KIP-101

	now.addkey(24) // 24 add partitions to txn KAFKA-4990 865d82af2c KIP-98 (raft 3.0 6e857c531f14d07d5b05f174e6063a124c917324)
	now.addkey(25) // 25 add offsets to txn (same, same raft)
	now.addkey(26) // 26 end txn (same, same raft)
	now.addkey(27) // 27 write txn markers (same)
	now.addkey(28) // 28 txn offset commit (same, same raft)

	// raft broker / controller added in 5b0c58ed53c420e93957369516f34346580dac95
	now.addkey(29) // 29 describe acls KAFKA-3266 9815e18fef KIP-140
	now.addkey(30) // 30 create acls (same)
	now.addkey(31) // 31 delete acls (same)

	now.addkey(32) // 32 describe configs KAFKA-3267 972b754536 KIP-133
	now.addkey(33) // 33 alter configs (same) (raft broker 3.0 6e857c531f14d07d5b05f174e6063a124c917324, controller 273d66479dbee2398b09e478ffaf996498d1ab34)

	// KAFKA-4954 0104b657a1 KIP-124
	now.incmax(2, 2)  // 2 list offset (reused in e71dce89c0 KIP-98)
	now.incmax(3, 3)  // 3 metadata
	now.incmax(8, 3)  // 3 offset commit
	now.incmax(9, 3)  // 3 offset fetch
	now.incmax(11, 2) // 2 join group
	now.incmax(12, 1) // 1 heartbeat
	now.incmax(13, 1) // 1 leave group
	now.incmax(14, 1) // 1 sync group
	now.incmax(15, 1) // 1 describe groups
	now.incmax(16, 1) // 1 list group
	now.incmax(18, 1) // 1 api versions
	now.incmax(19, 2) // 2 create topics
	now.incmax(20, 1) // 1 delete topics

	now.incmax(3, 4) // 4 metadata KAFKA-5291 7311dcbc53

	return now
}

func z10() *release {
	now := z0110().clone(1, 0)

	now.incmax(0, 4) // 4 produce KAFKA-4763 fc93fb4b61 KIP-112
	now.incmax(1, 6) // 6 fetch (same)
	now.incmax(3, 5) // 5 metadata (same)
	now.incmax(4, 1) // 1 leader and isr (same)
	now.incmax(6, 4) // 4 update metadata (same)

	now.incmax(0, 5)  // 5 produce KAFKA-5793 94692288be
	now.incmax(17, 1) // 1 sasl handshake KAFKA-4764 8fca432223 KIP-152

	now.addkey(34) // 34 alter replica log dirs KAFKA-5694 adefc8ea07 KIP-113
	now.addkey(35) // 35 describe log dirs (same)
	now.addkey(36) // 36 sasl authenticate KAFKA-4764 (see above)
	now.addkey(37) // 37 create partitions KAFKA-5856 5f6393f9b1 KIP-195 (raft 3.0 6e857c531f14d07d5b05f174e6063a124c917324)
	return now
}

func z11() *release {
	now := z10().clone(1, 1)

	now.addkey(38) // 38 create delegation token KAFKA-4541 27a8d0f9e7 under KAFKA-1696 KIP-48
	now.addkey(39) // 39 renew delegation token (same)
	now.addkey(40) // 40 expire delegation token (same)
	now.addkey(41) // 41 describe delegation token (same)
	now.addkey(42) // 42 delete groups KAFKA-6275 1ed6da7cc8 KIP-229

	now.incmax(1, 7)  // 7 fetch KAFKA-6254 7fe1c2b3d3 KIP-227
	now.incmax(32, 1) // 1 describe configs KAFKA-6241 b814a16b96 KIP-226

	return now
}

func z20() *release {
	now := z11().clone(2, 0)

	now.incmax(0, 6)  // 6 produce KAFKA-6028 1facab387f KIP-219
	now.incmax(1, 8)  // 8 fetch (same)
	now.incmax(2, 3)  // 3 list offset (same)
	now.incmax(3, 6)  // 6 metadata (same)
	now.incmax(8, 4)  // 4 offset commit (same)
	now.incmax(9, 4)  // 4 offset fetch (same)
	now.incmax(10, 2) // 2 find coordinator (same)
	now.incmax(11, 3) // 3 join group (same)
	now.incmax(12, 2) // 2 heartbeat (same)
	now.incmax(13, 2) // 2 leave group (same)
	now.incmax(14, 2) // 2 sync group (same)
	now.incmax(15, 2) // 2 describe groups (same)
	now.incmax(16, 2) // 2 list group (same)
	now.incmax(18, 2) // 2 api versions (same)
	now.incmax(19, 3) // 3 create topics (same)
	now.incmax(20, 2) // 2 delete topics (same)
	now.incmax(21, 1) // 1 delete records (same)
	now.incmax(22, 1) // 1 init producer id (same)
	now.incmax(24, 1) // 1 add partitions to txn (same)
	now.incmax(25, 1) // 1 add offsets to txn (same)
	now.incmax(26, 1) // 1 end txn (same)
	now.incmax(28, 1) // 1 txn offset commit (same)
	// 29, 30, 31 bumped below, but also had throttle changes
	now.incmax(32, 2) // 2 describe configs (same)
	now.incmax(33, 1) // 1 alter configs (same)
	now.incmax(34, 1) // 1 alter replica log dirs (same)
	now.incmax(35, 1) // 1 describe log dirs (same)
	now.incmax(37, 1) // 1 create partitions (same)
	now.incmax(38, 1) // 1 create delegation token (same)
	now.incmax(39, 1) // 1 renew delegation token (same)
	now.incmax(40, 1) // 1 expire delegation token (same)
	now.incmax(41, 1) // 1 describe delegation token (same)
	now.incmax(42, 1) // 1 delete groups (same)

	now.incmax(29, 1) // 1 describe acls KAFKA-6841 b3aa655a70 KIP-290
	now.incmax(30, 1) // 1 create acls (same)
	now.incmax(31, 1) // 1 delete acls (same)

	now.incmax(23, 1) // 1 offset for leader epoch KAFKA-6361 9679c44d2b KIP-279
	return now
}

func z21() *release {
	now := z20().clone(2, 1)

	now.incmax(8, 5) // 5 offset commit KAFKA-4682 418a91b5d4 KIP-211

	now.incmax(20, 3) // 3 delete topics KAFKA-5975 04770916a7 KIP-322

	now.incmax(1, 9)  // 9 fetch KAFKA-7333 05ba5aa008 KIP-320
	now.incmax(2, 4)  // 4 list offset (same)
	now.incmax(3, 7)  // 7 metadata (same)
	now.incmax(8, 6)  // 6 offset commit (same)
	now.incmax(9, 5)  // 5 offset fetch (same)
	now.incmax(23, 2) // 2 offset for leader epoch (same, also in Kafka PR #5635 79ad9026a6)
	now.incmax(28, 2) // 2 txn offset commit (same)

	now.incmax(0, 7)  // 7 produce KAFKA-4514 741cb761c5 KIP-110
	now.incmax(1, 10) // 10 fetch (same)

	return now
}

func z22() *release {
	now := z21().clone(2, 2)

	now.incmax(2, 5)  // 5 list offset KAFKA-2334 152292994e KIP-207
	now.incmax(11, 4) // 4 join group KAFKA-7824 9a9310d074 KIP-394
	now.incmax(36, 1) // 1 sasl authenticate KAFKA-7352 e8a3bc7425 KIP-368

	now.incmax(4, 2) // 2 leader and isr KAFKA-7235 2155c6d54b KIP-380
	now.incmax(5, 1) // 1 stop replica (same)
	now.incmax(6, 5) // 5 update metadata (same)
	now.incmax(7, 2) // 2 controlled shutdown (same)

	now.addkey(43) // 43 elect preferred leaders KAFKA-5692 269b65279c KIP-183 (raft 3.0 6e857c531f14d07d5b05f174e6063a124c917324)
	return now
}

func z23() *release {
	now := z22().clone(2, 3)

	now.incmax(3, 8)  // 8 metadata KAFKA-7922 a42f16f980 KIP-430
	now.incmax(15, 3) // 3 describe groups KAFKA-7922 f11fa5ef40 KIP-430

	now.incmax(1, 11) // 11 fetch KAFKA-8365 e2847e8603 KIP-392
	now.incmax(23, 3) // 3 offset for leader epoch (same)

	now.incmax(11, 5) // 5 join group KAFKA-7862 0f995ba6be KIP-345
	now.incmax(8, 7)  // 7 offset commit KAFKA-8225 9fa331b811 KIP-345
	now.incmax(12, 3) // 3 heartbeat (same)
	now.incmax(14, 3) // 3 sync group (same)

	now.addkey(44) // 44 incremental alter configs KAFKA-7466 3b1524c5df KIP-339
	return now
}

func z24() *release {
	now := z23().clone(2, 4)

	now.incmax(4, 3)  // 3 leader and isr KAFKA-8345 81900d0ba0 KIP-455
	now.incmax(15, 4) // 4 describe groups KAFKA-8538 f8db022b08 KIP-345
	now.incmax(19, 4) // 4 create topics KAFKA-8305 8e161580b8 KIP-464
	now.incmax(43, 1) // 1 elect preferred leaders KAFKA-8286 121308cc7a KIP-460

	// raft added in e07de97a4ce730a2755db7eeacb9b3e1f69a12c8 for the following two
	now.addkey(45) // 45 alter partition reassignments KAFKA-8345 81900d0ba0 KIP-455
	now.addkey(46) // 46 list partition reassignments (same)
	now.addkey(47) // 47 offset delete KAFKA-8730 e24d0e22ab KIP-496

	now.incmax(13, 3) // 3 leave group KAFKA-8221 74c90f46c3 KIP-345

	// introducing flexible versions; 24 were bumped
	now.incmax(3, 9)  // 9 metadata KAFKA-8885 apache/kafka#7325 KIP-482
	now.incmax(4, 4)  // 4 leader and isr (same)
	now.incmax(5, 2)  // 2 stop replica (same)
	now.incmax(6, 6)  // 6 update metadata (same)
	now.incmax(7, 3)  // 3 controlled shutdown (same)
	now.incmax(8, 8)  // 8 offset commit (same)
	now.incmax(9, 6)  // 6 offset fetch (same)
	now.incmax(10, 3) // 3 find coordinator (same)
	now.incmax(11, 6) // 6 join group (same)
	now.incmax(12, 4) // 4 heartbeat (same)
	now.incmax(13, 4) // 4 leave group (same)
	now.incmax(14, 4) // 4 sync group (same)
	now.incmax(15, 5) // 5 describe groups (same)
	now.incmax(16, 3) // 3 list group (same)
	now.incmax(18, 3) // 3 api versions (same, also KIP-511 [non-flexible fields added])
	now.incmax(19, 5) // 5 create topics (same)
	now.incmax(20, 4) // 4 delete topics (same)
	now.incmax(22, 2) // 2 init producer id (same)
	now.incmax(38, 2) // 2 create delegation token (same)
	now.incmax(42, 2) // 2 delete groups (same)
	now.incmax(43, 2) // 2 elect preferred leaders (same)
	now.incmax(44, 1) // 1 incremental alter configs (same)
	// also 45, 46; not bumped since in same release

	// Create topics (19) was bumped up to 5 in KAFKA-8907 5d0052fe00
	// KIP-525, then 6 in the above bump, then back down to 5 once the
	// tagged PR was merged (KAFKA-8932 1f1179ea64 for the bump down).

	now.incmax(0, 8) // 8 produce KAFKA-8729 f6f24c4700 KIP-467

	return now
}

func z25() *release {
	now := z24().clone(2, 5)

	now.incmax(22, 3) // 3 init producer id KAFKA-8710 fecb977b25 KIP-360
	now.incmax(9, 7)  // 7 offset fetch KAFKA-9346 6da70f9b95 KIP-447

	// more flexible versions, KAFKA-9420 0a2569e2b99 KIP-482
	// 6 bumped, then sasl handshake reverted later in 1a8dcffe4
	now.incmax(36, 2) // 2 sasl authenticate
	now.incmax(37, 2) // 2 create partitions
	now.incmax(39, 2) // 2 renew delegation token
	now.incmax(40, 2) // 2 expire delegation token
	now.incmax(41, 2) // 2 describe delegation token

	now.incmax(28, 3) // 3 txn offset commit KAFKA-9365 ed7c071e07f KIP-447

	now.incmax(29, 2) // 2 describe acls KAFKA-9026 40b35178e5 KIP-482 (for flexible versions)
	now.incmax(30, 2) // 2 create acls KAFKA-9027 738e14edb KIP-482 (flexible)
	now.incmax(31, 2) // 2 delete acls KAFKA-9028 738e14edb KIP-482 (flexible)

	now.incmax(11, 7) // 7 join group KAFKA-9437 96c4ce480 KIP-559
	now.incmax(14, 5) // 5 sync group (same)

	return now
}

func z26() *release {
	now := z25().clone(2, 6)

	now.incmax(21, 2) // 2 delete records KAFKA-8768 f869e33ab KIP-482 (opportunistic bump for flexible versions)
	now.incmax(35, 2) // 2 describe log dirs KAFKA-9435 4f1e8331ff9 KIP-482 (same)

	now.addkey(48) // 48 describe client quotas KAFKA-7740 227a7322b KIP-546 (raft in 5964401bf9aab611bd4a072941bd1c927e044258)
	now.addkey(49) // 49 alter client quotas (same)

	now.incmax(5, 3) // 3 stop replica KAFKA-9539 7c7d55dbd KIP-570

	now.incmax(16, 4) // 4 list group KAFKA-9130 fe948d39e KIP-518
	now.incmax(32, 3) // 3 describe configs KAFKA-9494 af3b8b50f2 KIP-569

	return now
}

func z27() *release {
	now := z26().clone(2, 7)

	// KAFKA-10163 a5ffd1ca44c KIP-599
	now.incmax(37, 3) // 3 create partitions
	now.incmax(19, 6) // 6 create topics (same)
	now.incmax(20, 5) // 5 delete topics (same)

	// KAFKA-9911 b937ec7567 KIP-588
	now.incmax(22, 4) // 4 init producer id
	now.incmax(24, 2) // 2 add partitions to txn
	now.incmax(25, 2) // 2 add offsets to txn
	now.incmax(26, 2) // 2 end txn

	now.addkey(50) // 50 describe user scram creds, KAFKA-10259 e8524ccd8fca0caac79b844d87e98e9c055f76fb KIP-554; 38c409cf33c kraft
	now.addkey(51) // 51 alter user scram creds, same

	// KAFKA-10435 634c9175054cc69d10b6da22ea1e95edff6a4747 KIP-595
	// This opted in fetch request to flexible versions.
	//
	// KAFKA-10487: further change in aa5263fba903c85812c0c31443f7d49ee371e9db
	now.incmax(1, 12) // 12 fetch

	// KAFKA-8836 57de67db22eb373f92ec5dd449d317ed2bc8b8d1 KIP-497
	now.addkey(56) // 56 alter isr

	// KAFKA-10028 fb4f297207ef62f71e4a6d2d0dac75752933043d KIP-584
	now.addkey(57) // 57 update features (rbroker 3.0 6e857c531f14d07d5b05f174e6063a124c917324; rcontroller 3.2 55ff5d360381af370fe5b3a215831beac49571a4 KIP-778  KAFKA-13823)
	return now
}

func z28() *release {
	now := z27().clone(2, 8)

	// KAFKA-10729 85f94d50271c952c3e9ee49c4fc814c0da411618 KIP-482
	// (flexible bumps)
	now.incmax(0, 9)  // 9 produce
	now.incmax(2, 6)  // 6 list offsets
	now.incmax(23, 4) // 4 offset for leader epoch
	now.incmax(24, 3) // 3 add partitions to txn
	now.incmax(25, 3) // 3 add offsets to txn
	now.incmax(26, 3) // 3 end txn
	now.incmax(27, 1) // 1 write txn markers
	now.incmax(32, 4) // 4 describe configs
	now.incmax(33, 2) // 2 alter configs
	now.incmax(34, 2) // 2 alter replica log dirs
	now.incmax(48, 1) // 1 describe client quotas
	now.incmax(49, 1) // 1 alter client quotas

	// KAFKA-10547 5c921afa4a593478f7d1c49e5db9d787558d0d5e KIP-516
	now.incmax(3, 10) // 10 metadata
	now.incmax(6, 7)  // 7 update metadata

	// KAFKA-10545 1dd1e7f945d7a8c1dc177223cd88800680f1ff46 KIP-516
	now.incmax(4, 5) // 5 leader and isr

	// KAFKA-12204 / KAFKA-10851 302eee63c479fd4b955c44f1058a5e5d111acb57 KIP-700
	now.addkey(60) // 60 describe cluster; rController in KAFKA-15396 41b695b6e30baa4243d9ca4f359b833e17ed0e77 KIP-919

	// KAFKA-12212 7a1d1d9a69a241efd68e572badee999229b3942f KIP-700
	now.incmax(3, 11) // 11 metadata

	// KAFKA-10764 4f588f7ca2a1c5e8dd845863da81425ac69bac92 KIP-516
	now.incmax(19, 7) // 7 create topics
	now.incmax(20, 6) // 6 delete topics

	// KAFKA-12238 e9edf104866822d9e6c3b637ffbf338767b5bf27 KIP-664
	now.addkey(61) // 61 describe producers
	return now
}

func z30() *release {
	now := z28().clone(3, 0)

	// KAFKA-12267 3f09fb97b6943c0612488dfa8e5eab8078fd7ca0 KIP-664
	now.addkey(65) // 65 describe transactions

	// KAFKA-12369 3708a7c6c1ecf1304f091dda1e79ae53ba2df489 KIP-664
	now.addkey(66) // 66 list transactions

	// KAFKA-12620 72d108274c98dca44514007254552481c731c958 KIP-730
	// raft broker added in  e97cff2702b6ba836c7925caa36ab18066a7c95d
	now.addkey(67) // 67 allocate producer ids

	// KAFKA-12541 bd72ef1bf1e40feb3bc17349a385b479fa5fa530 KIP-734
	now.incmax(2, 7) // 7 list offsets

	// KAFKA-12663 f5d5f654db359af077088685e29fbe5ea69616cf KIP-699
	now.incmax(10, 4) // 4 find coordinator

	// KAFKA-12234 e00c0f3719ad0803620752159ef8315d668735d6 KIP-709
	now.incmax(9, 8) // 8 offset fetch

	return now
}

func z31() *release {
	now := z30().clone(3, 1)

	// KAFKA-10580 2b8aff58b575c199ee8372e5689420c9d77357a5 KIP-516
	now.incmax(1, 13) // 13 fetch

	// KAFKA-10744 1d22b0d70686aef5689b775ea2ea7610a37f3e8c KIP-516
	now.incmax(3, 12) // 12 metadata

	return now
}

func z32() *release {
	now := z31().clone(3, 2)

	// KAFKA-13495 69645f1fe5103adb00de6fa43152e7df989f3aea KIP-800
	now.incmax(11, 8) // 8 join group

	// KAFKA-13496 bf609694f83931990ce63e0123f811e6475820c5 KIP-800
	now.incmax(13, 5) // 5 leave group

	// KAFKA-13527 31fca1611a6780e8a8aa3ac21618135201718e32 KIP-784
	now.incmax(35, 3) // 3 describe log dirs

	// KAFKA-13435 c8fbe26f3bd3a7c018e7619deba002ee454208b9 KIP-814
	now.incmax(11, 9) // 9 join group

	// KAFKA-13587 52621613fd386203773ba93903abd50b46fa093a KIP-704
	now.incmax(4, 6)  // 6 leader and isr
	now.incmax(56, 1) // 1 alter isr => alter partition

	return now
}

func z33() *release {
	now := z32().clone(3, 3)

	// KAFKA-13823 55ff5d360381af370fe5b3a215831beac49571a4 KIP-778
	now.incmax(57, 1) // 1 update features

	// KAFKA-13958 4fcfd9ddc4a8da3d4cfbb69268c06763352e29a9 KIP-827
	now.incmax(35, 4) // 4 describe log dirs

	// KAFKA-841 f83d95d9a28 KIP-841
	now.incmax(56, 2) // 2 alter partition

	// KAFKA-6945 d65d8867983 KIP-373
	now.incmax(29, 3) // 3 describe acls
	now.incmax(30, 3) // 3 create acls
	now.incmax(31, 3) // 3 delete acls
	now.incmax(38, 3) // 3 create delegation token
	now.incmax(41, 3) // 3 describe delegation token

	return now
}

func z34() *release {
	now := z33().clone(3, 4)

	// KAFKA-14304 7b7e40a536a79cebf35cc278b9375c8352d342b9 KIP-866
	// KAFKA-14448 67c72596afe58363eceeb32084c5c04637a33831 added BrokerRegistration
	// KAFKA-14493 db490707606855c265bc938e1b236070e0e2eba5 changed BrokerRegistration
	// KAFKA-14304 0bb05d8679b684ad8fbb2eb40dfc00066186a75a changed BrokerRegistration back to a bool...
	// 5b521031edea8ea7cbcca7dc24a58429423740ff added tag to ApiVersions
	now.incmax(4, 7) // 7 leader and isr
	now.incmax(5, 4) // 4 stop replica
	now.incmax(6, 8) // 8 update metadata

	// KAFKA-14446 8b045dcbf6b89e1a9594ff95642d4882765e4b0d KIP-866 Kafka 3.4
	now.addkey(58) // 58 envelope

	return now
}

func z35() *release {
	now := z34().clone(3, 5)

	// KAFKA-13369 7146ac57ba9ddd035dac992b9f188a8e7677c08d KIP-405
	now.incmax(1, 14) // 14 fetch
	now.incmax(2, 8)  // 8 list offsets

	now.incmax(1, 15) // 15 fetch // KAFKA-14617 79b5f7f1ce2 KIP-903
	now.incmax(56, 3) // 3 alter partition // KAFKA-14617 8c88cdb7186b1d594f991eb324356dcfcabdf18a KIP-903
	return now
}

func z36() *release {
	now := z35().clone(3, 6)

	// KAFKA-14402 29a1a16668d76a1cc04ec9e39ea13026f2dce1de KIP-890
	// Later commit swapped to stable
	now.incmax(24, 4) // 4 add partitions to txn
	return now
}

func z37() *release {
	now := z36().clone(3, 7)

	// KAFKA-15661 c8f687ac1505456cb568de2b60df235eb1ceb5f0 KIP-951
	now.incmax(0, 10) // 10 produce
	now.incmax(1, 16) // 16 fetch

	// 7826d5fc8ab695a5ad927338469ddc01b435a298 KIP-848
	// (change introduced in 3.6 but was marked unstable and not visible)
	now.incmax(8, 9) // 9 offset commit
	// KAFKA-14499 7054625c45dc6edb3c07271fe4a6c24b4638424f KIP-848 (and prior)
	now.incmax(9, 9) // 9 offset fetch

	// KAFKA-15368 41b695b6e30baa4243d9ca4f359b833e17ed0e77 KIP-919
	// (added rController as well, see above)
	now.incmax(60, 1) // 1 describe cluster

	// KAFKA-14391 3be7f7d611d0786f2f98159d5c7492b0d94a2bb7 KIP-848
	// as well as some patches following
	now.addkey(68) // 68 consumer group heartbeat

	return now
}

func z38() *release {
	now := z37().clone(3, 8)

	// KAFKA-16314 2e8d69b78ca52196decd851c8520798aa856c073 KIP-890
	// Then error rename in cf1ba099c0723f9cf65dda4cd334d36b7ede6327
	now.incmax(0, 11) // 11 produce
	now.incmax(10, 5) // 5 find coordinator
	now.incmax(22, 5) // 5 init producer id
	now.incmax(24, 5) // 5 add partitions to txn
	now.incmax(25, 4) // 4 add offsets to txn
	now.incmax(26, 4) // 4 end txn
	now.incmax(28, 4) // 4 txn offset commit

	// KAFKA-15460 68745ef21a9d8fe0f37a8c5fbc7761a598718d46 KIP-848
	now.incmax(16, 5) // 5 list groups

	// KAFKA-14509 90e646052a17e3f6ec1a013d76c1e6af2fbb756e KIP-848 added
	// 7b0352f1bd9b923b79e60b18b40f570d4bfafcc0
	// b7c99e22a77392d6053fe231209e1de32b50a98b
	// 68389c244e720566aaa8443cd3fc0b9d2ec4bb7a
	// 5f410ceb04878ca44d2d007655155b5303a47907 stabilized
	now.addkey(69) // 69 consumer group describe

	// KAFKA-16265 b4e96913cc6c827968e47a31261e0bd8fdf677b5 KIP-994 (part 1)
	now.incmax(66, 1) // 1 list transactions

	return now
}

func z39() *release {
	now := z38().clone(3, 9)

	// KAFKA-16527 adee6f0cc11 KIP-853
	// This introduces a tag only, which does not require bumping the
	// version bump. My skepticism on on the entire tag concept years
	// ago continues to play out.
	now.incmax(1, 17) // 17 fetch

	// ebaa108967f KIP-1005
	// This allows sending -5 to the broker as the timestamp.
	// No end user changes otherwise.
	now.incmax(2, 9) // 9 list offsets

	// KAFKA-16713 8f82f14a483 KIP-932
	now.incmax(10, 6) // 6 find coordinator

	// KAFKA-17011 ede289db93f
	now.incmax(18, 4) // 4 api versions

	return now
}

func ztip() *release {
	return z39()
}

///////////////////////////
// KRAFT BROKER VERSIONS //
///////////////////////////

func b28() *release {
	return &release{
		major: 2,
		minor: 8,
		kind:  kindBroker,
		reqs: map[int16]req{
			0:  {key: 0, vmax: 9},  // produce
			1:  {key: 1, vmax: 12}, // fetch
			2:  {key: 2, vmax: 6},  // list offsets
			3:  {key: 3, vmax: 11}, // metadata
			8:  {key: 8, vmax: 8},  // offset fetch
			9:  {key: 9, vmax: 7},  // offset commit
			10: {key: 10, vmax: 3}, // find coordinator
			11: {key: 11, vmax: 7}, // join group
			12: {key: 12, vmax: 4}, // heartbeat
			13: {key: 13, vmax: 4}, // leave group
			14: {key: 14, vmax: 5}, // sync group
			15: {key: 15, vmax: 5}, // describe groups
			16: {key: 16, vmax: 4}, // list groups
			17: {key: 17, vmax: 1}, // sasl handshake
			18: {key: 18, vmax: 3}, // api versions
			19: {key: 19, vmax: 7}, // create topics
			20: {key: 20, vmax: 6}, // delete topics
			21: {key: 21, vmax: 2}, // delete records
			23: {key: 23, vmax: 4}, // offset for leader epoch
			27: {key: 27, vmax: 1}, // write txn markers
			32: {key: 32, vmax: 4}, // describe configs
			34: {key: 34, vmax: 2}, // alter replica log dirs
			35: {key: 35, vmax: 2}, // describe log dirs
			36: {key: 36, vmax: 2}, // sasl authenticate
			42: {key: 42, vmax: 2}, // delete groups
			44: {key: 44, vmax: 1}, // incremental alter configs
			47: {key: 47, vmax: 0}, // offset delete
			49: {key: 49, vmax: 1}, // alter client quotas

			// KAFKA-10492 b7c8490cf47b0c18253d6a776b2b35c76c71c65d KIP-595
			// (described below)
			55: {key: 55, vmax: 0}, // describe quorum

			60: {key: 60, vmax: 0}, // describe cluster
			61: {key: 61, vmax: 0}, // describe producers

			// KAFKA-10181 KAFKA-10181 KIP-590
			58: {key: 58, vmax: 0}, // envelope
		},
	}
}

func b30() *release {
	now := b28().clone(3, 0)

	delete(now.reqs, 55) // describe quorum not present in 3.0

	now.incmax(2, 7)
	now.incmax(9, 8)
	now.incmax(10, 4)

	now.addkeyver(22, 4)
	now.addkeyver(24, 3)
	now.addkeyver(25, 3)
	now.addkeyver(26, 3)
	now.addkeyver(28, 3)
	now.addkeyver(29, 2)
	now.addkeyver(30, 2)
	now.addkeyver(31, 2)
	now.addkeyver(33, 2)
	now.addkeyver(37, 3)
	now.addkeyver(43, 2)
	now.addkey(45)
	now.addkey(46)
	now.addkeyver(48, 1)
	now.addkey(57)
	now.addkey(65)
	now.addkey(66)

	return now
}

func b31() *release {
	now := b30().clone(3, 1)
	now.incmax(1, 13)
	now.incmax(3, 12)
	return now
}

func b32() *release {
	now := b31().clone(3, 2)
	now.incmax(11, 8)
	now.incmax(11, 9)
	now.incmax(13, 5)
	now.incmax(35, 3)
	delete(now.reqs, 58)
	return now
}

func b33() *release {
	now := b32().clone(3, 3)
	now.incmax(29, 3)
	now.incmax(30, 3)
	now.incmax(31, 3)
	now.incmax(35, 4)
	now.addkeyver(55, 1)
	now.incmax(57, 1)
	now.addkey(64)
	return now
}

func b34() *release {
	now := b33().clone(3, 4) // no change for broker versions
	return now
}

func b35() *release {
	now := b34().clone(3, 5)
	now.incmax(1, 14)
	now.incmax(1, 15)
	now.incmax(2, 8)
	now.addkey(50)
	now.addkey(51)
	return now
}

func b36() *release {
	now := b35().clone(3, 6)
	now.incmax(24, 4)
	now.addkeyver(38, 3)
	now.addkeyver(39, 2)
	now.addkeyver(40, 2)
	now.addkeyver(41, 3)
	return now
}

func b37() *release {
	now := b36().clone(3, 7)
	now.incmax(0, 10)
	now.incmax(1, 16)
	now.incmax(8, 9)
	now.incmax(9, 9)
	now.incmax(60, 1)

	// KAFKA-15604 36abc8dcea1 KIP-714, only exposed if broker is configured
	now.addkey(71)
	now.addkey(72)

	now.addkey(68)
	// KAFKA-15831 587f50d48f8 KIP-1000
	now.addkey(74) // 74 list client metrics
	return now
}

func b38() *release {
	now := b37().clone(3, 8)
	now.incmax(0, 11)
	now.incmax(10, 5)
	now.incmax(16, 5)
	now.incmax(22, 5)
	now.incmax(24, 5)
	now.incmax(25, 4)
	now.incmax(26, 4)
	now.incmax(28, 4)
	now.incmax(66, 1)

	now.addkey(69)
	// KAFKA-15585 7e5ef9b509a KIP-966
	now.addkey(75) // 75 describe topic partition
	return now
}

func b39() *release {
	now := b38().clone(3, 9)
	// All version bumps here are commented above in zk or below in c.
	now.incmax(1, 17)
	now.incmax(2, 9)
	now.incmax(10, 6)
	now.incmax(18, 4)
	now.incmax(55, 2)
	now.addkey(80)
	now.addkey(81)
	return now
}

func b40() *release {
	now := b39().clone(4, 0)

	// KIP-896
	setmin := func(key, vmin int16) {
		req, ok := now.reqs[key]
		if !ok {
			panic(fmt.Sprintf("setmin on non-existent key %d", key))
		}
		req.vmin = vmin
		now.reqs[key] = req
	}
	setmin(1, 4)
	setmin(2, 1)
	setmin(8, 2)
	setmin(9, 1)
	setmin(11, 2)
	setmin(19, 2)
	setmin(20, 1)
	setmin(23, 2)
	setmin(27, 1)
	setmin(29, 1)
	setmin(30, 1)
	setmin(31, 1)
	setmin(32, 1)
	setmin(34, 1)
	setmin(35, 1)
	setmin(38, 1)
	setmin(39, 1)
	setmin(40, 1)
	setmin(41, 1)

	now.incmax(0, 12) // 12 produce KAFKA-14563 755adf8a566 KIP-890
	now.incmax(2, 10) // 10 list offsets KAFKA-15859 560076ba9e8 KIP-1075
	now.incmax(3, 13) // 13 metadata KAFKA-17885 52d2fa5c8b3 KIP-1102
	now.incmax(15, 6) // 6 describe groups KAFKA-17550 e7d986e48c2 KIP-1043
	now.incmax(26, 5) // 5 end txn KAFKA-14562 ede0c94aaae KIP-890
	now.incmax(28, 5) // 5 txn offset commit KAFKA-14563 755adf8a566 KIP-890
	now.incmax(57, 2) // documented on controller
	now.incmax(60, 2) // documented on controller
	now.incmax(68, 1) // 1 consumer group heartbeat KAFKA-17592 ab0df20489a KIP-848; includes KAFKA-17116 6f040cabc7c KIP-1082 in same release
	now.incmax(69, 1) // 1 consumer group describe KAFKA-17750 fe88232b07c KIP-858

	return now
}

func b41() *release {
	now := b40().clone(4, 1)

	now.incmax(0, 13) // 13 produce KAFKA-10551 6f783f85362 KIP-516
	now.incmax(1, 18) // 18 fetch KAFKA-14145 742b327025f KIP-1166
	now.incmax(45, 1) // 1 alter partition assignments KAFKA-14121 cbd72cc216e KIP-860
	now.incmax(66, 2) // 2 list transactions KAFKA-19073 0c1fbf3aebb KIP-1152
	now.incmax(74, 1) // 1 list config resources (rename & extend) KAFKA-18904 c26b09c6092 KIP-1142

	// 8f82f14a483 for v0, KAFKA-16713 66147d5de7c KIP-932 for v1
	now.addkeyver(76, 1) // 1 share group heartbeat
	now.addkeyver(77, 1) // 1 share group describe
	now.addkeyver(78, 1) // 1 share fetch
	now.addkeyver(79, 1) // 1 share acknowledge

	// KAFKA-16950 fecbfb81332 KIP-932
	now.addkey(83) // 0 initialize share group state
	now.addkey(84) // 0 read share group state
	now.addkey(85) // 0 write share group state
	now.addkey(86) // 0 delete share group state
	now.addkey(87) // 0 read share group state summary

	now.addkey(90) // 0 describe share group offsets e3e4c179592, then KAFKA-16720 952113e8e0e KIP-932
	now.addkey(91) // 0 alter share group offsets KAFKA-16717 6a6b80215d8 KIP-932
	now.addkey(92) // 0 delete share group offsets KAFKA-16718 63229a768ce KIP-932

	return now
}

func btip() *release {
	return b41()
}

///////////////////////////////
// KRAFT CONTROLLER VERSIONS //
///////////////////////////////

func c28() *release {
	return &release{
		major: 2,
		minor: 8,
		kind:  kindController,
		reqs: map[int16]req{
			1:  {key: 1, vmax: 12}, // fetch
			3:  {key: 3, vmax: 11}, // metadata
			7:  {key: 7, vmax: 3},  // controlled shutdown
			17: {key: 17, vmax: 1}, // sasl handshake
			18: {key: 18, vmax: 3}, // api versions
			19: {key: 19, vmax: 7}, // create topics
			20: {key: 20, vmax: 6}, // delete topics
			36: {key: 36, vmax: 2}, // sasl authenticate
			44: {key: 44, vmax: 1}, // incremental alter configs
			49: {key: 49, vmax: 1}, // alter client quotas

			// KAFKA-10492 b7c8490cf47b0c18253d6a776b2b35c76c71c65d KIP-595
			//
			// These are the first requests that are raft only. The commits
			// showed up in the 2.7 branch, but KRaft as preview is only
			// available at 2.8.
			52: {key: 52, vmax: 0}, // vote
			53: {key: 53, vmax: 0}, // begin quorum epoch
			54: {key: 54, vmax: 0}, // end quorum epoch
			55: {key: 55, vmax: 0}, // describe quorum
			56: {key: 56, vmax: 0}, // alter partition

			// KAFKA-10181 KAFKA-10181 KIP-590
			58: {key: 58, vmax: 0}, // 58 envelope
			59: {key: 59, vmax: 0}, // 59 fetch snapshot

			// KAFKA-12248 a022072df3c8175950c03263d2bbf2e3ea7a7a5d KIP-500
			// (commit mentions KIP-500, these are actually described in KIP-631)
			// Broker registration was later updated in d9bb2ef596343da9402bff4903b129cff1f7c22b
			62: {key: 62, vmax: 0}, // 62 broker registration
			63: {key: 63, vmax: 0}, // 63 broker heartbeat

			// KAFKA-12249 3f36f9a7ca153a9d221f6bedeb7d1503aa18eff1 KIP-500 / KIP-631
			// Renamed from Decommission to Unregister in 06dce721ec0185d49fac37775dbf191d0e80e687
			64: {key: 64, vmax: 0}, // 64 unregister broker
		},
	}
}

func c30() *release {
	now := c28().clone(3, 0)

	delete(now.reqs, 3) // metadata not present in controller 3.0

	now.addkeyver(29, 2) // describe acls
	now.addkeyver(30, 2) // create acls
	now.addkeyver(31, 2) // delete acls
	now.addkeyver(33, 2) // alter configs

	now.addkeyver(37, 3) // create partitions
	now.addkeyver(43, 2) // elect leaders
	now.addkey(45)       // alter partition assignments
	now.addkey(46)       // list partition reassignments

	// KAFKA-12620 72d108274c98dca44514007254552481c731c958 KIP-730
	// raft broker added in e97cff2702b6ba836c7925caa36ab18066a7c95d
	now.addkey(67)

	return now
}

func c31() *release {
	now := c30().clone(3, 1)
	now.incmax(1, 13)
	return now
}

func c32() *release {
	now := c31().clone(3, 2)
	now.incmax(56, 1)
	return now
}

func c33() *release {
	now := c32().clone(3, 3)
	now.incmax(29, 3)
	now.incmax(30, 3)
	now.incmax(31, 3)
	now.incmax(55, 1)
	now.incmax(56, 2)
	now.addkeyver(57, 1)
	return now
}

func c34() *release {
	now := c33().clone(3, 4)
	now.incmax(62, 1) // KAFKA-14304 7b7e40a536a (type change in 0bb05d8679b) KIP-866
	return now
}

func c35() *release {
	now := c34().clone(3, 5)
	now.incmax(1, 14)
	now.incmax(1, 15)
	now.addkey(50)
	now.addkey(51)
	now.incmax(56, 3)
	return now
}

func c36() *release {
	now := c35().clone(3, 6)
	now.addkeyver(38, 3)
	now.addkeyver(39, 2)
	now.addkeyver(40, 2)
	now.addkeyver(41, 3)
	return now
}

func c37() *release {
	now := c36().clone(3, 7)
	now.incmax(1, 16)
	now.addkeyver(32, 4)
	now.addkeyver(60, 1)
	now.incmax(62, 2) // KAFKA-15355 a94bc8d6d52 KIP-858
	now.incmax(62, 3) // KAFKA-15582 14029e2ddd1 KIP-966 (some later commit swapped the docs on which came first between this and prior line)
	now.incmax(63, 1) // KAFKA-15355 0390d5b1a24 KIP-858 (adds tag... no version bump actually needed...)

	// KAFKA-15369 41b695b6e30 KIP-919
	now.addkey(70) // 70 controller registration
	// KAFKA-15355 0390d5b1a24 KIP-858
	now.addkey(73) // 73 assign replica to dirs
	return now
}

func c38() *release {
	now := c37().clone(3, 8) // no changes for controller broker this version
	return now
}

func c39() *release {
	now := c38().clone(3, 9)
	now.incmax(1, 17)
	now.incmax(18, 4)

	// KAFKA-16527 adee6f0cc11 KIP-853 adds a bunch of fields to Vote/{Begin,End}QuorumEpoch
	now.incmax(52, 1)
	now.incmax(53, 1)
	now.incmax(54, 1)
	now.incmax(59, 1)
	now.incmax(55, 2) // KAFKA-16520 aecaf444756 KIP-853

	// KAFKA-17001 ede289db93f -- no kip, this is a bugfix
	now.incmax(62, 4)

	// KAFKA-16535 9ceed8f18f4 KIP-853
	now.addkey(80) // 80 add raft voter
	now.addkey(81) // 81 remove raft voter
	now.addkey(82) // 82 update raft voter
	return now
}

func c40() *release {
	now := c39().clone(4, 0)

	// KIP-896
	setmin := func(key, vmin int16) {
		req, ok := now.reqs[key]
		if !ok {
			panic(fmt.Sprintf("setmin on non-existent key %d", key))
		}
		req.vmin = vmin
		now.reqs[key] = req
	}
	setmin(1, 4)
	setmin(19, 2)
	setmin(20, 1)
	setmin(29, 1)
	setmin(30, 1)
	setmin(31, 1)
	setmin(32, 1)
	setmin(38, 1)
	setmin(39, 1)
	setmin(40, 1)
	setmin(41, 1)
	setmin(56, 2)
	delete(now.reqs, 7)

	now.incmax(52, 2) // KAFKA-17641 b73e31eb159 KIP-996 adds PreVote to Vote
	now.incmax(57, 2) // KAFKA-16308 49d7ea6c6a2; no kip referenced
	now.incmax(60, 2) // KAFKA-17094 747dc172e87 KIP-1073

	return now
}

func c41() *release {
	now := c40().clone(4, 1)

	now.incmax(1, 18)
	now.incmax(45, 1)

	return now
}

func ctip() *release {
	return c41()
}
