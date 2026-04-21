package ring

import (
	"bytes"
	"fmt"
	"math"
	"math/bits"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	shardUtil "github.com/grafana/dskit/ring/shard"
)

// splitmix64Step advances a splitmix64 PRNG state and returns the next uint32.
// splitmix64 has a 64-bit state, period 2^64, and excellent statistical quality.
// Used in shuffleShard to replace math/rand (which requires initializing a 607-element
// state vector from the seed, costing ~6 µs per call).
func splitmix64Step(state *uint64) uint32 {
	*state += 0x9e3779b97f4a7c15
	z := *state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return uint32(z >> 32)
}

var ErrNoActivePartitionFound = fmt.Errorf("no active partition found")

// PartitionRing holds an immutable view of the partitions ring.
//
// Design principles:
//   - Immutable: the PartitionRingDesc hold by PartitionRing is immutable. When PartitionRingDesc changes
//     a new instance of PartitionRing should be created. The  partitions ring is expected to change infrequently
//     (e.g. there's no heartbeat), so creating a new PartitionRing each time the partitions ring changes is
//     not expected to have a significant overhead.
type PartitionRing struct {
	// desc is a snapshot of the partition ring. This data is immutable and MUST NOT be modified.
	desc PartitionRingDesc

	// ringTokens is a sorted list of all tokens registered by all partitions.
	// Nil for compact subrings (parent != nil); use tokenLen/partitionAt/searchRingToken accessors.
	ringTokens Tokens

	// ringPartitionIDs is parallel to ringTokens: ringPartitionIDs[i] is the partition ID owning ringTokens[i].
	// Nil for compact subrings (parent != nil).
	ringPartitionIDs []int32

	// Compact subring fields — set only when this ring was created by newSubring.
	// When parent != nil, ringTokens and ringPartitionIDs are nil.
	// ringTokenIndices is lazily initialised on first use: subrings returned by ShuffleShard are
	// typically cached and never queried for token-level operations (ActivePartitionForKey, nested
	// ShuffleShard), so deferring the 8 KB allocation eliminates it entirely for the common case.
	parent           *PartitionRing
	selectedBits     uint64     // bitset of selected PIDs (0-63); 0 means "use desc map" for PIDs ≥ 64
	tokenCountVal    int        // pre-computed; len(ringTokenIndices) once built
	indicesOnce      sync.Once  // guards lazy init of ringTokenIndices
	ringTokenIndices []uint16   // indices into parent.ringTokens; nil until first token access

	// ownersByPartition is a map where the key is the partition ID and the value is a list of owner IDs.
	ownersByPartition map[int32][]string

	// shuffleShardCache is used to cache subrings generated with shuffle sharding.
	shuffleShardCache *partitionRingShuffleShardCache

	// activePartitionsCount is a saved count of active partitions to avoid recomputing it.
	activePartitionsCount int

	// pidTokenCounts holds the number of ring tokens per partition ID for PIDs 0-63.
	// Only populated for root rings (parent == nil) as a precomputed lookup table that lets
	// newSubring compute the subring token count in O(k) instead of O(total_tokens).
	pidTokenCounts [64]uint16

	// tokenBuckets is a 257-entry prefix-sum table dividing the uint32 keyspace into 256
	// equal-width buckets by top 8 bits.  tokenBuckets[i] is the index of the first token
	// in ringTokens with (token >> 24) >= i.  Non-nil only for root rings with ≤65535 tokens.
	// Lets searchRingToken restrict the binary search to ~64 tokens instead of all 16K,
	// reducing comparisons from log2(16384)=14 to log2(64)=6.
	tokenBuckets *[257]uint16

	// allPIDsFitBitset is true when every partition ID in desc is in [0, 63], enabling the
	// zero-alloc bitset fast path in shuffleShard.
	allPIDsFitBitset bool

	// opts is used to propagate the options to sub rings when shuffle sharding.
	opts PartitionRingOptions
}

// tokenLen returns the number of ring tokens (works for both full and compact subrings).
func (r *PartitionRing) tokenLen() int {
	if r.parent != nil {
		return r.tokenCountVal
	}
	return len(r.ringTokens)
}

// ensureIndices builds ringTokenIndices lazily on first call.  Subsequent calls are a
// single atomic load via sync.Once and cost ~1 ns.
func (r *PartitionRing) ensureIndices() {
	r.indicesOnce.Do(func() {
		indices := make([]uint16, 0, r.tokenCountVal)
		if bits := r.selectedBits; bits != 0 {
			for i, pid := range r.parent.ringPartitionIDs {
				if bits>>uint(pid)&1 != 0 {
					indices = append(indices, uint16(i))
				}
			}
		} else {
			// selectedBits == 0: some PID ≥ 64 — fall back to desc.Partitions map lookup.
			for i, pid := range r.parent.ringPartitionIDs {
				if _, ok := r.desc.Partitions[pid]; ok {
					indices = append(indices, uint16(i))
				}
			}
		}
		r.ringTokenIndices = indices
	})
}

// partitionAt returns the partition ID owning the i-th token slot.
func (r *PartitionRing) partitionAt(i int) int32 {
	if r.parent != nil {
		r.ensureIndices()
		return r.parent.ringPartitionIDs[r.ringTokenIndices[i]]
	}
	return r.ringPartitionIDs[i]
}

// tokenAt returns the i-th token value.
func (r *PartitionRing) tokenAt(i int) uint32 {
	if r.parent != nil {
		r.ensureIndices()
		return r.parent.ringTokens[r.ringTokenIndices[i]]
	}
	return r.ringTokens[i]
}

// searchRingToken returns the insertion point for key in the sorted token sequence,
// wrapping to 0 if key is past the last token.
func (r *PartitionRing) searchRingToken(key uint32) int {
	if r.parent != nil {
		r.ensureIndices()
		return searchTokenIndirect(r.ringTokenIndices, r.parent.ringTokens, key)
	}
	if r.tokenBuckets != nil {
		tokens := r.ringTokens
		n := len(tokens)
		bucket := key >> 24
		lo := int(r.tokenBuckets[bucket])
		hi := int(r.tokenBuckets[bucket+1])
		for lo < hi {
			mid := int(uint(lo+hi) >> 1)
			if tokens[mid] < key {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo < n && tokens[lo] == key {
			lo++
		}
		return lo % n
	}
	return searchToken(r.ringTokens, key)
}

// searchTokenIndirect is searchToken for compact subrings: binary-searches over
// parent.ringTokens values addressed through the indices slice.
func searchTokenIndirect(indices []uint16, tokens Tokens, key uint32) int {
	n := len(indices)
	lo, hi := 0, n
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if tokens[indices[mid]] < key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo % n
}

// resolveRingTokens returns the sorted token slice.  For compact subrings it materialises
// a copy; callers outside the hot path (e.g. GetTokenRangesForPartition) use this.
func (r *PartitionRing) resolveRingTokens() Tokens {
	if r.parent == nil {
		return r.ringTokens
	}
	r.ensureIndices()
	tokens := make(Tokens, len(r.ringTokenIndices))
	for i, idx := range r.ringTokenIndices {
		tokens[i] = r.parent.ringTokens[idx]
	}
	return tokens
}

// isInSubring reports whether partitionID belongs to this ring.
// For root rings this is always true; for compact subrings it checks selectedBits.
func (r *PartitionRing) isInSubring(partitionID int32) bool {
	if r.parent == nil || r.selectedBits == 0 {
		return true
	}
	return r.selectedBits>>uint(partitionID)&1 != 0
}

// PartitionRingOptions holds optional configuration parameters for creating a PartitionRing.
type PartitionRingOptions struct {
	// ShuffleShardCacheSize is the size of the cache used for shuffle sharding.
	// If zero or negative, an unbounded map-based cache is used.
	// If positive, an LRU cache with the specified size is used.
	ShuffleShardCacheSize int
}

// DefaultShuffleShardCacheSize is the default maximum number of shuffle-shard results
// cached per PartitionRing. A bounded LRU is used so the internal map never
// grows without limit; unbounded growth causes repeated map-bucket reallocations
// that dominate madvise/GC CPU cost in high-throughput workloads.
const DefaultShuffleShardCacheSize = 1024

// DefaultPartitionRingOptions returns the default options for creating a PartitionRing.
func DefaultPartitionRingOptions() PartitionRingOptions {
	return PartitionRingOptions{
		ShuffleShardCacheSize: DefaultShuffleShardCacheSize,
	}
}

// NewPartitionRing creates a new PartitionRing with default options.
func NewPartitionRing(desc PartitionRingDesc) (*PartitionRing, error) {
	return NewPartitionRingWithOptions(desc, DefaultPartitionRingOptions())
}

// NewPartitionRingWithOptions creates a new PartitionRing with custom options.
func NewPartitionRingWithOptions(desc PartitionRingDesc, opts PartitionRingOptions) (*PartitionRing, error) {
	shuffleShardCache, err := newPartitionRingShuffleShardCache(opts.ShuffleShardCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create shuffle shard cache: %w", err)
	}

	ringTokens, ringPartitionIDs := desc.tokensByPartition()

	var pidTokenCounts [64]uint16
	allPIDsFitBitset := true
	for _, pid := range ringPartitionIDs {
		if uint32(pid) >= 64 {
			allPIDsFitBitset = false
		} else {
			pidTokenCounts[pid]++
		}
	}

	var tokenBuckets *[257]uint16
	if len(ringTokens) <= 65535 {
		tb := new([257]uint16)
		for _, t := range ringTokens {
			tb[t>>24+1]++
		}
		for i := 1; i <= 256; i++ {
			tb[i] += tb[i-1]
		}
		tokenBuckets = tb
	}

	return &PartitionRing{
		desc:                  desc,
		ringTokens:            ringTokens,
		ringPartitionIDs:      ringPartitionIDs,
		ownersByPartition:     desc.ownersByPartition(),
		activePartitionsCount: desc.activePartitionsCount(),
		shuffleShardCache:     shuffleShardCache,
		pidTokenCounts:        pidTokenCounts,
		tokenBuckets:          tokenBuckets,
		allPIDsFitBitset:      allPIDsFitBitset,
		opts:                  opts,
	}, nil
}

// ActivePartitionForKey returns partition for the given key. Only active partitions are considered.
// Only one partition is returned: in other terms, the replication factor is always 1.
func (r *PartitionRing) ActivePartitionForKey(key uint32) (int32, error) {
	var (
		start       = r.searchRingToken(key)
		iterations  = 0
		tokensCount = r.tokenLen()
	)

	for i := start; iterations < tokensCount; i++ {
		iterations++

		if i >= tokensCount {
			i %= tokensCount
		}

		partitionID := r.partitionAt(i)

		partition, ok := r.desc.Partitions[partitionID]
		if !ok {
			return 0, ErrInconsistentTokensInfo
		}

		// If the partition is not active we'll keep walking the ring.
		if partition.IsActive() {
			return partitionID, nil
		}
	}

	return 0, ErrNoActivePartitionFound
}

// ShuffleShardSize returns number of partitions that would be in the result of ShuffleShard call with the same size.
func (r *PartitionRing) ShuffleShardSize(size int) int {
	if size <= 0 || size > r.activePartitionsCount {
		return r.activePartitionsCount
	}

	if size < r.activePartitionsCount {
		return size
	}
	return r.activePartitionsCount
}

// ShuffleShard returns a subring for the provided identifier (eg. a tenant ID)
// and size (number of partitions).
//
// The algorithm used to build the subring is a shuffle sharder based on probabilistic
// hashing. We pick N unique partitions, walking the ring starting from random but
// predictable numbers. The random generator is initialised with a seed based on the
// provided identifier.
//
// This function returns a subring containing ONLY ACTIVE partitions.
//
// This function supports caching.
//
// This implementation guarantees:
//
//   - Stability: given the same ring, two invocations returns the same result.
//
//   - Consistency: adding/removing 1 partition from the ring generates a resulting
//     subring with no more then 1 difference.
//
//   - Shuffling: probabilistically, for a large enough cluster each identifier gets a different
//     set of instances, with a reduced number of overlapping instances between two identifiers.
func (r *PartitionRing) ShuffleShard(identifier string, size int) (*PartitionRing, error) {
	if r.shuffleShardCache != nil {
		if cached := r.shuffleShardCache.getSubring(identifier, size); cached != nil {
			return cached, nil
		}
	}

	// No need to pass the time if there's no lookback.
	subring, err := r.shuffleShard(identifier, size, 0, time.Time{})
	if err != nil {
		return nil, err
	}

	if r.shuffleShardCache != nil {
		r.shuffleShardCache.setSubring(identifier, size, subring)
	}
	return subring, nil
}

// ShuffleShardWithLookback is like ShuffleShard() but the returned subring includes all instances
// that have been part of the identifier's shard in [now - lookbackPeriod, now] time window.
//
// This function can return a mix of ACTIVE and INACTIVE partitions. INACTIVE partitions are only
// included if they were part of the identifier's shard within the lookbackPeriod. PENDING partitions
// are never returned.
//
// This function supports caching, but the cache will only be effective if successive calls for the
// same identifier are with the same lookbackPeriod and increasing values of now.
func (r *PartitionRing) ShuffleShardWithLookback(identifier string, size int, lookbackPeriod time.Duration, now time.Time) (*PartitionRing, error) {
	if r.shuffleShardCache != nil {
		if cached := r.shuffleShardCache.getSubringWithLookback(identifier, size, lookbackPeriod, now); cached != nil {
			return cached, nil
		}
	}

	subring, err := r.shuffleShard(identifier, size, lookbackPeriod, now)
	if err != nil {
		return nil, err
	}

	if r.shuffleShardCache != nil {
		r.shuffleShardCache.setSubringWithLookback(identifier, size, lookbackPeriod, now, subring)
	}
	return subring, nil
}

func (r *PartitionRing) shuffleShard(identifier string, size int, lookbackPeriod time.Duration, now time.Time) (*PartitionRing, error) {
	// If the size is too small or too large, run with a size equal to the total number of partitions.
	// We have to run the function anyway because the logic may filter out some INACTIVE partitions.
	if size <= 0 || size >= len(r.desc.Partitions) {
		size = len(r.desc.Partitions)
	}

	var lookbackUntil int64
	if lookbackPeriod > 0 {
		lookbackUntil = now.Add(-lookbackPeriod).Unix()
	}

	// splitmix64 initialises from the seed in ~5 ns vs ~6 µs for math/rand's 607-element state.
	rngState := uint64(shardUtil.ShuffleShardSeed(identifier, ""))
	tokensCount := r.tokenLen()

	// Bitset fast path: no map allocs when all PIDs fit in a uint64 (PIDs 0-63).
	// Uses two uint64 bitsets instead of result/exclude maps, then calls newSubringFromBits
	// to build the compact subring without ever constructing a map[int32]struct{}.
	if r.allPIDsFitBitset && r.parent == nil && len(r.ringTokens) <= 65535 {
		var resultBits, excludeBits uint64
		resultCount := 0

		for resultCount < size {
			start := r.searchRingToken(splitmix64Step(&rngState))
			iterations := 0
			found := false

			for p := start; !found && iterations < tokensCount; p++ {
				iterations++
				if p >= tokensCount {
					p %= tokensCount
				}

				pid := r.ringPartitionIDs[p]
				pidBit := uint64(1) << uint(pid)

				if resultBits&pidBit != 0 || excludeBits&pidBit != 0 {
					continue
				}

				part, ok := r.desc.Partitions[pid]
				if !ok {
					return nil, ErrInconsistentTokensInfo
				}

				if part.IsPending() {
					excludeBits |= pidBit
					continue
				}

				withinLookbackPeriod := lookbackPeriod > 0 && part.GetStateTimestamp() >= lookbackUntil
				shouldInclude := part.IsActive() || withinLookbackPeriod

				if shouldInclude {
					resultBits |= pidBit
					resultCount++
				} else {
					excludeBits |= pidBit
				}
				if withinLookbackPeriod {
					size++
				}
				if shouldInclude && !withinLookbackPeriod {
					found = true
				}
			}

			if !found {
				break
			}
		}

		return r.newSubringFromBits(resultBits)
	}

	// Fallback: map-based approach for PIDs ≥ 64.
	result := make(map[int32]struct{}, size)
	exclude := map[int32]struct{}{}

	for len(result) < size {
		start := r.searchRingToken(splitmix64Step(&rngState))
		iterations := 0
		found := false

		for p := start; !found && iterations < tokensCount; p++ {
			iterations++

			// Wrap p around in the ring.
			if p >= tokensCount {
				p %= tokensCount
			}

			pid := r.partitionAt(p)

			// Ensure the partition has not already been included or excluded.
			if _, ok := result[pid]; ok {
				continue
			}
			if _, ok := exclude[pid]; ok {
				continue
			}

			p, ok := r.desc.Partitions[pid]
			if !ok {
				return nil, ErrInconsistentTokensInfo
			}

			// PENDING partitions should be skipped because they're not ready for read or write yet,
			// and they don't need to be looked back.
			if p.IsPending() {
				exclude[pid] = struct{}{}
				continue
			}

			var (
				withinLookbackPeriod = lookbackPeriod > 0 && p.GetStateTimestamp() >= lookbackUntil
				shouldExtend         = withinLookbackPeriod
				shouldInclude        = p.IsActive() || withinLookbackPeriod
			)

			// Either include or exclude the found partition.
			if shouldInclude {
				result[pid] = struct{}{}
			} else {
				exclude[pid] = struct{}{}
			}

			// Extend the shard, if requested.
			if shouldExtend {
				size++
			}

			// We can stop searching for other partitions only if this partition was included
			// and no extension was requested, which means it's the "stop partition" for this cycle.
			if shouldInclude && !shouldExtend {
				found = true
			}
		}

		// If we iterated over all tokens, and no new partition has been found, we can stop looking for more partitions.
		if !found {
			break
		}
	}

	return r.newSubring(result)
}

// newSubring builds a subring containing only the selected partition IDs.
//
// Compact path (root ring, all PIDs < 64, ≤65535 tokens):
//   - Shares the parent's immutable desc and ownersByPartition — skips WithPartitions (~1900 B).
//   - Defers building ringTokenIndices until first token-level access (skips another 8 KB lazily).
//   - Skips shuffleShardCache allocation; subrings are leaf nodes and ShuffleShard handles nil.
//
// Fallback path: creates a filtered PartitionRingDesc via WithPartitions for PIDs ≥ 64 or
// recursive subrings.
func (r *PartitionRing) newSubring(selected map[int32]struct{}) (*PartitionRing, error) {
	// Compact path: only when this is a root ring (not a subring) and all selected PIDs fit
	// in a uint64 bitset, so we can filter without copying the desc.
	if r.parent == nil && len(r.ringTokens) <= 65535 {
		var selectedBits uint64
		allFitBitset := true
		for pid := range selected {
			if uint32(pid) >= 64 {
				allFitBitset = false
				break
			}
			selectedBits |= 1 << uint(pid)
		}

		if allFitBitset {
			// Count tokens using precomputed per-PID counts: O(k) instead of O(total_tokens).
			tokenCount := 0
			for b := selectedBits; b != 0; b &= b - 1 {
				tokenCount += int(r.pidTokenCounts[bits.TrailingZeros64(b)])
			}

			// Count active partitions by iterating only the k selected PIDs.
			activeParts := 0
			for pid := range selected {
				if p, ok := r.desc.Partitions[pid]; ok && p.IsActive() {
					activeParts++
				}
			}

			// Share parent's immutable desc and ownersByPartition — zero extra allocations.
			return &PartitionRing{
				desc:                  r.desc,
				parent:                r,
				selectedBits:          selectedBits,
				tokenCountVal:         tokenCount,
				ownersByPartition:     r.ownersByPartition,
				activePartitionsCount: activeParts,
				// shuffleShardCache: nil — handled in ShuffleShard/ShuffleShardWithLookback.
				opts: r.opts,
			}, nil
		}
	}

	// Fallback: create filtered PartitionRingDesc (PIDs ≥ 64 or recursive subrings).
	subDesc := r.desc.WithPartitions(selected)

	shuffleShardCache, err := newPartitionRingShuffleShardCache(r.opts.ShuffleShardCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create shuffle shard cache: %w", err)
	}

	// Compact (but not bitset-optimized) path for root rings ≤ 65535 tokens.
	if r.parent == nil && len(r.ringTokens) <= 65535 {
		tokenCount := 0
		for _, pid := range r.ringPartitionIDs {
			if _, ok := selected[pid]; ok {
				tokenCount++
			}
		}
		return &PartitionRing{
			desc:                  subDesc,
			parent:                r,
			selectedBits:          0, // 0 = use desc.Partitions map in ensureIndices
			tokenCountVal:         tokenCount,
			ownersByPartition:     subDesc.ownersByPartition(),
			activePartitionsCount: subDesc.activePartitionsCount(),
			shuffleShardCache:     shuffleShardCache,
			opts:                  r.opts,
		}, nil
	}

	// Fallback: materialise full token/PID arrays (recursive subrings or rings > 65535 tokens).
	// Resolve parent arrays once regardless of whether r is compact.
	n := r.tokenLen()
	capacity := len(selected) * optimalTokensPerInstance
	subTokens := make(Tokens, 0, capacity)
	subPIDs := make([]int32, 0, capacity)

	var selectedBits uint64
	allFitBitset := true
	for pid := range selected {
		if uint32(pid) >= 64 {
			allFitBitset = false
			break
		}
		selectedBits |= 1 << uint(pid)
	}

	if allFitBitset {
		for i := 0; i < n; i++ {
			pid := r.partitionAt(i)
			if selectedBits>>uint(pid)&1 != 0 {
				subTokens = append(subTokens, r.tokenAt(i))
				subPIDs = append(subPIDs, pid)
			}
		}
	} else {
		for i := 0; i < n; i++ {
			pid := r.partitionAt(i)
			if _, ok := selected[pid]; ok {
				subTokens = append(subTokens, r.tokenAt(i))
				subPIDs = append(subPIDs, pid)
			}
		}
	}

	return &PartitionRing{
		desc:                  subDesc,
		ringTokens:            subTokens,
		ringPartitionIDs:      subPIDs,
		ownersByPartition:     subDesc.ownersByPartition(),
		activePartitionsCount: subDesc.activePartitionsCount(),
		shuffleShardCache:     shuffleShardCache,
		opts:                  r.opts,
	}, nil
}

// newSubringFromBits builds a compact subring from a uint64 bitset of selected PIDs.
// Zero-alloc fast path used by shuffleShard when allPIDsFitBitset is true: avoids
// constructing a map[int32]struct{} entirely (saves 2 allocs vs newSubring).
func (r *PartitionRing) newSubringFromBits(selectedBits uint64) (*PartitionRing, error) {
	// Combined O(k) loop: count tokens and active partitions using precomputed pidTokenCounts.
	tokenCount := 0
	activeParts := 0
	for b := selectedBits; b != 0; b &= b - 1 {
		pid := int32(bits.TrailingZeros64(b))
		tokenCount += int(r.pidTokenCounts[pid])
		if part, ok := r.desc.Partitions[pid]; ok && part.IsActive() {
			activeParts++
		}
	}

	return &PartitionRing{
		desc:                  r.desc,
		parent:                r,
		selectedBits:          selectedBits,
		tokenCountVal:         tokenCount,
		ownersByPartition:     r.ownersByPartition,
		activePartitionsCount: activeParts,
		opts:                  r.opts,
	}, nil
}

// PartitionsCount returns the number of partitions in the ring.
func (r *PartitionRing) PartitionsCount() int {
	if r.parent != nil && r.selectedBits != 0 {
		return bits.OnesCount64(r.selectedBits)
	}
	return len(r.desc.Partitions)
}

// ActivePartitionsCount returns the number of active partitions in the ring.
func (r *PartitionRing) ActivePartitionsCount() int {
	return r.activePartitionsCount
}

// Partitions returns the partitions in the ring.
// The returned slice is a deep copy, so the caller can freely manipulate it.
func (r *PartitionRing) Partitions() []PartitionDesc {
	res := make([]PartitionDesc, 0, r.PartitionsCount())

	for id, partition := range r.desc.Partitions {
		if !r.isInSubring(id) {
			continue
		}
		res = append(res, partition.Clone())
	}

	return res
}

// PartitionIDs returns a sorted list of all partition IDs in the ring.
// The returned slice is a copy, so the caller can freely manipulate it.
func (r *PartitionRing) PartitionIDs() []int32 {
	ids := make([]int32, 0, r.PartitionsCount())

	for id := range r.desc.Partitions {
		if !r.isInSubring(id) {
			continue
		}
		ids = append(ids, id)
	}

	slices.Sort(ids)
	return ids
}

// PendingPartitionIDs returns a sorted list of all PENDING partition IDs in the ring.
// The returned slice is a copy, so the caller can freely manipulate it.
func (r *PartitionRing) PendingPartitionIDs() []int32 {
	ids := make([]int32, 0, len(r.desc.Partitions))

	for id, partition := range r.desc.Partitions {
		if !r.isInSubring(id) {
			continue
		}
		if partition.IsPending() {
			ids = append(ids, id)
		}
	}

	slices.Sort(ids)
	return ids
}

// ActivePartitionIDs returns a sorted list of all ACTIVE partition IDs in the ring.
// The returned slice is a copy, so the caller can freely manipulate it.
func (r *PartitionRing) ActivePartitionIDs() []int32 {
	ids := make([]int32, 0, len(r.desc.Partitions))

	for id, partition := range r.desc.Partitions {
		if !r.isInSubring(id) {
			continue
		}
		if partition.IsActive() {
			ids = append(ids, id)
		}
	}

	slices.Sort(ids)
	return ids
}

// InactivePartitionIDs returns a sorted list of all INACTIVE partition IDs in the ring.
// The returned slice is a copy, so the caller can freely manipulate it.
func (r *PartitionRing) InactivePartitionIDs() []int32 {
	ids := make([]int32, 0, len(r.desc.Partitions))

	for id, partition := range r.desc.Partitions {
		if !r.isInSubring(id) {
			continue
		}
		if partition.IsInactive() {
			ids = append(ids, id)
		}
	}

	slices.Sort(ids)
	return ids
}

// PartitionOwnerIDs returns a list of owner IDs for the given partitionID.
// The returned slice is NOT a copy and should be never modified by the caller.
func (r *PartitionRing) PartitionOwnerIDs(partitionID int32) (doNotModify []string) {
	return r.ownersByPartition[partitionID]
}

// PartitionOwnerIDsCopy is like PartitionOwnerIDs(), but the returned slice is a copy,
// so the caller can freely manipulate it.
func (r *PartitionRing) PartitionOwnerIDsCopy(partitionID int32) []string {
	ids := r.ownersByPartition[partitionID]
	if len(ids) == 0 {
		return nil
	}

	return slices.Clone(ids)
}

// MultiPartitionOwnerIDs returns the ownerIDs of the given partitionID removing the suffix added to support ownership of multiple partitions.
// The slice returned will try to use the provided buf, and it can be modified (it will modify the buf if it was used).
func (r *PartitionRing) MultiPartitionOwnerIDs(partitionID int32, buf []string) []string {
	ids := r.ownersByPartition[partitionID]
	if len(ids) == 0 {
		return nil
	}

	if cap(buf) < len(ids) {
		buf = make([]string, len(ids))
	} else {
		buf = buf[:len(ids)]
	}

	for i, ownerID := range ids {
		if p := strings.LastIndexByte(ownerID, '/'); p != -1 {
			buf[i] = ownerID[:p]
		} else {
			// This isn't expected here: all owner IDs should have a suffix when multiple partitions can be owned.
			buf[i] = ownerID
		}
	}
	return buf
}

func (r *PartitionRing) String() string {
	buf := bytes.Buffer{}
	for pid, pd := range r.desc.Partitions {
		if !r.isInSubring(pid) {
			continue
		}
		buf.WriteString(fmt.Sprintf(" %d:%v", pid, pd.State.String()))
	}

	return fmt.Sprintf("PartitionRing{ownersCount: %d, partitionsCount: %d, partitions: {%s}}", len(r.desc.Owners), r.PartitionsCount(), buf.String())
}

// GetTokenRangesForPartition returns token-range owned by given partition. Note that this
// method does NOT take partition state into account, so if only active partitions should be
// considered, then PartitionRing with only active partitions must be created first (e.g. using ShuffleShard method).
func (r *PartitionRing) GetTokenRangesForPartition(partitionID int32) (TokenRanges, error) {
	if !r.isInSubring(partitionID) {
		return nil, ErrPartitionDoesNotExist
	}
	partition, ok := r.desc.Partitions[partitionID]
	if !ok {
		return nil, ErrPartitionDoesNotExist
	}

	// 1 range (2 values) per token + one additional if we need to split the rollover range.
	ranges := make(TokenRanges, 0, 2*(len(partition.Tokens)+1))

	addRange := func(start, end uint32) {
		// check if we can group ranges. If so, we just update end of previous range.
		if len(ranges) > 0 && ranges[len(ranges)-1] == start-1 {
			ranges[len(ranges)-1] = end
		} else {
			ranges = append(ranges, start, end)
		}
	}

	// "last" range is range that includes token math.MaxUint32.
	ownsLastRange := false
	startOfLastRange := uint32(0)

	// We start with all tokens, but will remove tokens we already skipped, to let binary search do less work.
	ringTokens := r.resolveRingTokens()

	for iter, t := range partition.Tokens {
		lastOwnedToken := t - 1

		ix := searchToken(ringTokens, lastOwnedToken)
		prevIx := ix - 1

		if prevIx < 0 {
			// We can only find "last" range during first iteration.
			if iter > 0 {
				return nil, ErrInconsistentTokensInfo
			}

			prevIx = len(ringTokens) - 1
			ownsLastRange = true

			startOfLastRange = ringTokens[prevIx]

			// We can only claim token 0 if our actual token in the ring (which is exclusive end of range) was not 0.
			if t > 0 {
				addRange(0, lastOwnedToken)
			}
		} else {
			addRange(ringTokens[prevIx], lastOwnedToken)
		}

		// Reduce number of tokens we need to search through. We keep current token to serve as min boundary for next search,
		// to make sure we don't find another "last" range (where prevIx < 0).
		ringTokens = ringTokens[ix:]
	}

	if ownsLastRange {
		addRange(startOfLastRange, math.MaxUint32)
	}

	return ranges, nil
}

// ActivePartitionBatchRing wraps PartitionRing and implements DoBatchRing to lookup ACTIVE partitions.
type ActivePartitionBatchRing struct {
	ring *PartitionRing
}

func NewActivePartitionBatchRing(ring *PartitionRing) *ActivePartitionBatchRing {
	return &ActivePartitionBatchRing{
		ring: ring,
	}
}

// InstancesCount returns the number of active partitions in the ring.
//
// InstancesCount implements DoBatchRing.InstancesCount.
func (r *ActivePartitionBatchRing) InstancesCount() int {
	return r.ring.ActivePartitionsCount()
}

// ReplicationFactor returns 1 as partitions replication factor: an entry (looked by key via Get())
// is always stored in 1 and only 1 partition.
//
// ReplicationFactor implements DoBatchRing.ReplicationFactor.
func (r *ActivePartitionBatchRing) ReplicationFactor() int {
	return 1
}

// Get implements DoBatchRing.Get.
func (r *ActivePartitionBatchRing) Get(key uint32, _ Operation, bufInstances []InstanceDesc, _, _ []string) (ReplicationSet, error) {
	partitionID, err := r.ring.ActivePartitionForKey(key)
	if err != nil {
		return ReplicationSet{}, err
	}

	// Ensure we have enough capacity in bufInstances.
	if cap(bufInstances) < 1 {
		bufInstances = []InstanceDesc{{}}
	} else {
		bufInstances = bufInstances[:1]
	}

	partitionIDString := strconv.Itoa(int(partitionID))

	bufInstances[0] = InstanceDesc{
		Addr:      partitionIDString,
		Timestamp: 0,
		State:     ACTIVE,
		Id:        partitionIDString,
	}

	return ReplicationSet{
		Instances:            bufInstances,
		MaxErrors:            0,
		MaxUnavailableZones:  0,
		ZoneAwarenessEnabled: false,
	}, nil
}

func multiPartitionOwnerInstanceID(instanceID string, partitionID int32) string {
	return instanceID + "/" + strconv.Itoa(int(partitionID))
}
