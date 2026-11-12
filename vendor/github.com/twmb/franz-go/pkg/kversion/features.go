package kversion

import (
	"fmt"
	"maps"
)

// This file is the KIP-584 feature table: for each release since 3.3, the
// range of levels a broker can run for each feature, and the level a new
// cluster is formatted with. The numbers come from the Kafka source at the
// release tag: MetadataVersion.java (MINIMUM_VERSION, LATEST_PRODUCTION) and
// the enums under server-common/src/main/java/org/apache/kafka/server/common/
// (KRaftVersion, TransactionVersion, GroupVersion,
// EligibleLeaderReplicasVersion, ShareVersion, StreamsVersion). A feature is
// in a release only if Feature.PRODUCTION_FEATURES lists it and its latest
// production level is above 0, which is what BrokerFeatures advertises. The
// finalized level is defaultLevel(LATEST_PRODUCTION): the highest level whose
// bootstrapMetadataVersion is at or below the release's latest production
// metadata.version.
//
// For a new Kafka release, add fNN cloning the previous release, read
// MetadataVersion.java and the feature enums at the release tag, add one
// commented line per new level naming the KAFKA- issue, the commit, and the
// KIP (git log -S'IBP_4_5_IV0' on MetadataVersion.java finds the commit),
// finalize any small feature whose bootstrap level the release now reaches,
// add the new levels to featureLevelDescriptions, and point ftip at fNN.
// V4_5_0 and FromString find fNN by major and minor.

type featureRange struct {
	min int16
	max int16
}

type features struct {
	major     uint8
	minor     uint8
	supported map[string]featureRange
	finalized map[string]int16
	prior     *features
}

func newFeatures(major, minor uint8) *features {
	return &features{
		major:     major,
		minor:     minor,
		supported: make(map[string]featureRange),
		finalized: make(map[string]int16),
	}
}

func (f *features) clone(nextMajor, nextMinor uint8) *features {
	return &features{
		major:     nextMajor,
		minor:     nextMinor,
		supported: maps.Clone(f.supported),
		finalized: maps.Clone(f.finalized),
		prior:     f,
	}
}

// add adds a feature with the given supported range, finalized at max.
func (f *features) add(name string, min, max int16) {
	if _, ok := f.supported[name]; ok {
		panic(fmt.Sprintf("feature %s already exists", name))
	}
	f.supported[name] = featureRange{min, max}
	f.finalized[name] = max
}

// inc bumps a feature's max level by one and finalizes it, the way a release
// that raises LATEST_PRODUCTION does.
func (f *features) inc(name string, max int16) {
	r, ok := f.supported[name]
	if !ok {
		panic(fmt.Sprintf("feature %s does not yet exist to inc", name))
	}
	if r.max+1 != max {
		panic(fmt.Sprintf("feature %s next max %d != exp %d", name, r.max+1, max))
	}
	r.max++
	f.supported[name] = r
	f.finalized[name] = max
}

func (f *features) setmin(name string, min int16) {
	r, ok := f.supported[name]
	if !ok {
		panic(fmt.Sprintf("setmin on non-existent feature %s", name))
	}
	r.min = min
	f.supported[name] = r
}

// finalize sets the level a new cluster is formatted with, for a feature
// whose default trails its max.
func (f *features) finalize(name string, level int16) {
	r, ok := f.supported[name]
	if !ok {
		panic(fmt.Sprintf("finalize on non-existent feature %s", name))
	}
	if level < r.min || level > r.max {
		panic(fmt.Sprintf("feature %s finalized %d outside %d-%d", name, level, r.min, r.max))
	}
	f.finalized[name] = level
}

// featuresFor returns the feature table for a release, or nil for a release
// before 3.3, which advertised none.
func featuresFor(major, minor uint8) *features {
	for f := ftip(); f != nil; f = f.prior {
		if f.major == major && f.minor == minor {
			return f
		}
	}
	return nil
}

func (vs *Versions) setFeatures(f *features) {
	if f == nil {
		vs.supported, vs.finalized = nil, nil
		return
	}
	vs.supported, vs.finalized = f.supported, f.finalized
}

func f33() *features {
	now := newFeatures(3, 3)

	// 3.3 is the first release that writes metadata.version to the log
	// (KIP-778). The supported range starts at the first KRaft level,
	// MINIMUM_KRAFT_VERSION; a new cluster formats at the last level.
	// Levels 1 through 7 were numbered in KAFKA-13935 cc384054c6e, when
	// 3.0-IV0 lost its level, and left the enum in KAFKA-18601 3dba3125e9c.
	now.add("metadata.version", 1, 1) // 1 3.0-IV1 ListOffsets v7 by max timestamp KAFKA-12541 bd72ef1bf1e KIP-734, message format 3.0 KIP-724
	now.inc("metadata.version", 2)    // 2 3.1-IV0 Fetch topic ids KAFKA-10580 2b8aff58b57 KIP-516
	now.inc("metadata.version", 3)    // 3 3.2-IV0 leader recovery after unclean election KAFKA-13587 52621613fd3 KIP-704
	now.inc("metadata.version", 4)    // 4 3.3-IV0 metadata.version feature KAFKA-13830 1135f22eaf4 KIP-778; min level dropped KAFKA-13833 54d60ced869
	now.inc("metadata.version", 5)    // 5 3.3-IV1 NoopRecord KAFKA-13883 7d1b0926fab KIP-835
	now.inc("metadata.version", 6)    // 6 3.3-IV2 BrokerRegistrationChangeRecord 65b43742036 apache/kafka#12195, no KAFKA- issue
	now.inc("metadata.version", 7)    // 7 3.3-IV3 InControlledShutdown KAFKA-13916 151ca12a56c KIP-841
	return now
}

func f34() *features {
	now := f33().clone(3, 4)

	now.inc("metadata.version", 8) // 8 3.4-IV0 ZooKeeper to KRaft migration KAFKA-14304 7b7e40a536a KIP-866
	return now
}

func f35() *features {
	now := f34().clone(3, 5)

	now.inc("metadata.version", 9)  // 9 3.5-IV0 tiered storage KAFKA-13369 7146ac57ba9 KIP-405; added as 3.4-IV1, renamed 6d11261d5de
	now.inc("metadata.version", 10) // 10 3.5-IV1 Fetch replica epoch KAFKA-14617 79b5f7f1ce2 KIP-903
	now.inc("metadata.version", 11) // 11 3.5-IV2 SCRAM in KRaft KAFKA-14881 abca86511ec
	return now
}

func f36() *features {
	now := f35().clone(3, 6)

	now.inc("metadata.version", 12) // 12 3.6-IV0 no epoch bump on ISR shrink KAFKA-15021 8ad0ed3e618
	now.inc("metadata.version", 13) // 13 3.6-IV1 metadata transactions KAFKA-14538 adc16d0f310 KIP-868
	now.inc("metadata.version", 14) // 14 3.6-IV2 delegation tokens in KRaft KAFKA-15219 c2759df0676; moved to IV2 in 8394ddc0d26
	return now
}

func f37() *features {
	now := f36().clone(3, 7)

	// 3.7 is the first release with LATEST_PRODUCTION; before it, the last
	// level in the enum was the default.
	now.inc("metadata.version", 15) // 15 3.7-IV0 controller registration KAFKA-15369 41b695b6e30 KIP-919
	now.inc("metadata.version", 16) // 16 3.7-IV1 reserved; was ELR KAFKA-15581 af747fbfed7 KIP-966, freed in a94bc8d6d52
	now.inc("metadata.version", 17) // 17 3.7-IV2 JBOD in KRaft KAFKA-15922 a94bc8d6d52 KIP-858
	now.inc("metadata.version", 18) // 18 3.7-IV3 reserved; was ELR again in a94bc8d6d52, freed in b0e99b55934
	now.inc("metadata.version", 19) // 19 3.7-IV4 replica fetcher Fetch v16 KAFKA-15922 b0e99b55934 KIP-951
	return now
}

func f38() *features {
	now := f37().clone(3, 8)

	now.inc("metadata.version", 20) // 20 3.8-IV0 release marker KAFKA-15922 b0e99b55934, kept by KAFKA-16968 ebaa108967f
	return now
}

func f39() *features {
	now := f38().clone(3, 9)

	now.inc("metadata.version", 21) // 21 3.9-IV0 ListOffsets v9 KAFKA-16968 ebaa108967f KIP-1005

	// KRAFT_VERSION_1 bootstraps at 3.9-IV0, so defaultLevel is 1. The
	// formatter writes 1 only for --standalone or --initial-controllers
	// and 0 for a static controller.quorum.voters, which the docker image
	// uses; a broker lists kraft.version as finalized only when it is 1.
	now.add("kraft.version", 0, 1) // 1 dynamic quorum KAFKA-16772 4d3e366bc24 KIP-853

	// transaction.version is in PRODUCTION_FEATURES at 3.9 but TV_1 and
	// TV_2 bootstrap at 4.0-IV0, so its latest production level is 0 and
	// BrokerFeatures does not advertise it. GroupVersion exists at 3.9 but
	// is not in the Features enum.
	return now
}

func f40() *features {
	now := f39().clone(4, 0)

	now.setmin("metadata.version", 7) // KAFKA-18601 3dba3125e9c: 3.3-IV3 is the baseline

	now.inc("metadata.version", 22) // 22 4.0-IV0 bootstraps group.version 1 KAFKA-16860 ba61ff0cd94, KAFKA-17413 c977bfdd3cd KIP-848
	now.inc("metadata.version", 23) // 23 4.0-IV1 ELR records KAFKA-18634 e7a2af8414c KIP-966
	now.inc("metadata.version", 24) // 24 4.0-IV2 bootstraps transaction.version 1 and 2 KAFKA-17413 c977bfdd3cd KIP-890
	now.inc("metadata.version", 25) // 25 4.0-IV3 async remote ListOffsets KAFKA-15859 560076ba9e8 KIP-1075

	now.add("transaction.version", 0, 2)                // 2 epoch bump per transaction KAFKA-16192 a0f6e6f816c KIP-890
	now.add("group.version", 0, 1)                      // 1 consumer rebalance protocol KAFKA-16860 ba61ff0cd94, KAFKA-17413 c977bfdd3cd KIP-848
	now.add("eligible.leader.replicas.version", 0, 1)   // 1 ELR KAFKA-18062 2b2b3cd355c KIP-966; production in KAFKA-16540 6235a73622d
	now.finalize("eligible.leader.replicas.version", 0) // ELRV_1 bootstraps at 4.1-IV0, 8f13e7c2073
	return now
}

func f41() *features {
	now := f40().clone(4, 1)

	now.inc("metadata.version", 26) // 26 4.1-IV0 ELR by default 8f13e7c2073 KIP-966; level added in aec0e555be2
	now.inc("metadata.version", 27) // 27 4.1-IV1 replica fetcher Fetch v18 KAFKA-14145 742b327025f KIP-1166

	now.finalize("eligible.leader.replicas.version", 1) // 4.1-IV0 reaches ELRV_1's bootstrap, 9412051dc6b

	now.add("share.version", 0, 1)   // 1 share groups KAFKA-16894 21a080f08ca KIP-932; production in 223684bad11
	now.finalize("share.version", 0) // SV_1 bootstraps at 4.2-IV0

	// streams.version exists at 4.1 with LATEST_PRODUCTION SV_0, so
	// BrokerFeatures does not advertise it.
	return now
}

func f42() *features {
	now := f41().clone(4, 2)

	now.inc("metadata.version", 28) // 28 4.2-IV0 share groups by default KAFKA-16894 21a080f08ca KIP-932
	now.inc("metadata.version", 29) // 29 4.2-IV1 streams groups by default KAFKA-19173 b0a26bc2f48 KIP-1071

	now.finalize("share.version", 1) // 4.2-IV0 reaches SV_1's bootstrap, fb68ada1a23

	now.add("streams.version", 0, 1) // 1 streams groups KAFKA-19173 b0a26bc2f48 KIP-1071; production in KAFKA-19869 497072a5644
	return now
}

func f43() *features {
	now := f42().clone(4, 3)

	now.inc("metadata.version", 30) // 30 4.3-IV0 cordoned log dirs KAFKA-19774 a45d36ca5d5 KIP-1066
	return now
}

func f44() *features {
	now := f43().clone(4, 4)

	now.inc("metadata.version", 31) // 31 4.4-IV0 share group dead letter queue KAFKA-20415 06f699664bd KIP-1191; level added in a45d36ca5d5
	now.inc("metadata.version", 32) // 32 4.4-IV1 CIDR ACL hosts KAFKA-20088 82b804fe272 KIP-1276
	now.inc("metadata.version", 33) // 33 4.4-IV2 controller unregistration KAFKA-20395 c274a7348fb KIP-1312

	now.inc("share.version", 2) // 2 dead letter queue KAFKA-20415 06f699664bd KIP-1191; production in KAFKA-20793 ef4b7904be3
	return now
}

func ftip() *features {
	return f44()
}

// featureLevelDescriptions is indexed by level. Level 34 is unstable at 4.4
// and is not in any release's table.
var featureLevelDescriptions = map[string][]string{
	"metadata.version": {
		1:  "3.0-IV1: ListOffsets v7 by max timestamp (KIP-734); message format 3.0 assumed (KIP-724)",
		2:  "3.1-IV0: Fetch carries topic ids (KIP-516)",
		3:  "3.2-IV0: leader recovery after unclean election (KIP-704)",
		4:  "3.3-IV0: metadata.version is itself a feature; finalized ranges drop the min level (KIP-778)",
		5:  "3.3-IV1: NoopRecord in the metadata log (KIP-835)",
		6:  "3.3-IV2: BrokerRegistrationChangeRecord replaces the fence and unfence records (apache/kafka#12195)",
		7:  "3.3-IV3: InControlledShutdown state in broker records (KIP-841); the minimum for 4.x",
		8:  "3.4-IV0: ZooKeeper to KRaft migration records (KIP-866)",
		9:  "3.5-IV0: tiered storage (KIP-405)",
		10: "3.5-IV1: Fetch carries the replica epoch (KIP-903)",
		11: "3.5-IV2: SCRAM credentials in KRaft (KAFKA-14881)",
		12: "3.6-IV0: no leader epoch bump when the controller shrinks the ISR (KAFKA-15021)",
		13: "3.6-IV1: metadata transactions (KAFKA-14538)",
		14: "3.6-IV2: delegation tokens in KRaft (KAFKA-15219)",
		15: "3.7-IV0: controller registration (KIP-919)",
		16: "3.7-IV1: reserved",
		17: "3.7-IV2: JBOD in KRaft (KAFKA-15922)",
		18: "3.7-IV3: reserved; was ELR, moved to 4.0-IV1",
		19: "3.7-IV4: replica fetcher sends the KIP-951 Fetch version (KIP-951)",
		20: "3.8-IV0: release marker, gates nothing",
		21: "3.9-IV0: ListOffsets v9 (KIP-1005)",
		22: "4.0-IV0: bootstraps group.version 1 (KIP-848)",
		23: "4.0-IV1: ELR records in the metadata log, preview (KIP-966)",
		24: "4.0-IV2: bootstraps transaction.version 1 and 2 (KIP-890)",
		25: "4.0-IV3: async remote ListOffsets (KIP-1075)",
		26: "4.1-IV0: ELR on by default for new clusters (KIP-966)",
		27: "4.1-IV1: replica fetcher sends Fetch v18 (KIP-1166)",
		28: "4.2-IV0: share groups on by default for new clusters (KIP-932)",
		29: "4.2-IV1: streams groups on by default for new clusters (KIP-1071)",
		30: "4.3-IV0: cordoned log dirs in broker records (KAFKA-19774)",
		31: "4.4-IV0: dead letter queue for share groups (KIP-1191)",
		32: "4.4-IV1: CIDR host patterns in ACLs (KIP-1276)",
		33: "4.4-IV2: controller unregistration (KIP-1312)",
		34: "4.5-IV0: release marker, unstable",
	},
	"kraft.version": {
		0: "the original KRaft quorum",
		1: "dynamic quorum membership (KIP-853)",
	},
	"transaction.version": {
		0: "the original transaction coordinator",
		1: "flexible transaction state records (KIP-890)",
		2: "epoch bump per transaction (KIP-890)",
	},
	"group.version": {
		0: "the classic rebalance protocol only",
		1: "the consumer rebalance protocol (KIP-848)",
	},
	"eligible.leader.replicas.version": {
		0: "ELR off",
		1: "ELR on; needs metadata.version 23 (KIP-966)",
	},
	"share.version": {
		0: "share groups off",
		1: "share groups (KIP-932)",
		2: "dead letter queue for share groups (KIP-1191)",
	},
	"streams.version": {
		0: "streams groups off",
		1: "streams groups (KIP-1071)",
	},
}

// FeatureLevelDescription describes one level of a feature in a sentence
// with the KIP or issue to search for, or returns "" for a level kversion
// does not know. The wording carries no stability guarantee.
func FeatureLevelDescription(name string, level int16) string {
	levels := featureLevelDescriptions[name]
	if level < 0 || int(level) >= len(levels) {
		return ""
	}
	return levels[level]
}
