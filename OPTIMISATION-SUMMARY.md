# ShuffleShard Optimisation Research Summary

**Benchmark**: `BenchmarkShuffleSharding/partitions=32`  
**Target file**: `vendor/github.com/grafana/dskit/ring/partition_ring.go`  
**Also modified**: `vendor/github.com/grafana/dskit/ring/partitions_ring_shuffle_shard_cache.go`  
**Period**: This session (continued from prior session)  
**Overall result**: **113,848 → 322 ns/op — 99.7% total improvement (353× faster)**

---

## Results Table

| Iter | Commit | ns/op | Δ | Description |
|------|--------|-------|---|-------------|
| 0 | `c5682e8cbb` | 113,848 | baseline | Initial state |
| 1 | — | 128,850 | +15,002 | ❌ ringPartitionIDs parallel array (construction overhead > savings) |
| 2 | — | 117,052 | +3,204 | ❌ bool arrays for result/exclude (heap allocs eat savings) |
| 3 | — | 120,949 | +7,101 | ❌ uint64 bitset fast path attempt (maps already stack-allocated) |
| 4 | `74780cc727` | 98,500 | −15,348 | ✅ ringPartitionIDs array + newSubring() — eliminates per-shard token sort |
| 5 | `4133843657` | 40,000 | −58,500 | ✅ uint64 bitset in newSubring filter loop — replaces 16,384 map lookups |
| 6 | `34c2bcaf27` | 36,000 | −4,000 | ✅ splitmix64 PRNG replaces math/rand 607-element init |
| 7 | `89e8b3d14c` | 28,317 | −7,683 | ✅ Compact subring: uint16 indices replace subTokens+subPIDs arrays |
| 8 | `1bfbc843e8` | 7,152 | −21,165 | ✅ Lazy-init ringTokenIndices — defers 8KB alloc until first token access |
| 9 | `c811e454fe` | 6,204 | −948 | ✅ Share parent desc in compact subrings; skip WithPartitions alloc |
| 10 | `316dbea095` | 1,082 | −5,122 | ✅ Precompute per-PID token counts — O(16K) loop → O(k=8) |
| 11 | `4ad8cbca1e` | 970 | −112 | ✅ Bitset fast path in shuffleShard — eliminate result/exclude map allocs |
| 12 | `9a51631cbe` | 845 | −125 | ✅ Default to bounded LRU(1024) cache — stops map-bucket realloc madvise |
| 13 | `06ebee99cb` | 676 | −169 | ✅ 256-bucket prefix-sum index narrows searchRingToken from 14→6 comparisons |
| 14 | `56fcf18b1b` | 627 | −49 | ✅ Precompute activePartBits/pendingPartBits — eliminates inner-loop map lookups |
| 15 | `872ddcf665` | 504 | −123 | ✅ 4096 buckets (top 12 bits) — narrows search from ~64 to ~4 tokens |
| 16 | `b36ea921f6` | 456 | −48 | ✅ `pidTokenCounts *[64]uint16` pointer — shrinks compact subring 320→200 bytes |
| 17 | `2bc82eef0c` | 352 | −104 | ✅ Direct-mapped zero-alloc cache replaces LRU — no per-insert list-node alloc |
| 18 | `4732151fab` | 322 | −30 | ✅ 16384 buckets (top 14 bits) — O(1) token search (~1 token per bucket) |

### Discards in this session (below 5% threshold or slower)

| Description | Result | Reason |
|---|---|---|
| `rootData *partitionRingRootData` struct | 471 ns/op (+3.3%) | Extra pointer chasing in hot path (`tokenBuckets` accessed 16×/call) outweighs GC savings |
| `md5.Sum()` for zone="" case in ShuffleShardSeed | 344.7 ns/op (−2%) | Only saved 16 bytes / 1 alloc; MD5 computation time unchanged |

---

## Key Techniques Applied

### 1. Algorithmic (iters 4–11)
- **Pre-sorted `ringPartitionIDs` array**: avoids per-shard `partitionByToken` map build
- **uint64 bitset filter loop**: replaces map-based 16,384-token membership test with 64-bit operations
- **splitmix64 PRNG**: O(1) seeded init vs O(607) for `math/rand`
- **Compact subring with lazy uint16 index**: returns parent pointer + bitset instead of copying 32KB token arrays; defers 8KB index until first use
- **`pidTokenCounts [64]uint16`**: precomputed per-PID token counts → O(k) subring construction vs O(16K)
- **`allPIDsFitBitset` + bitset fast path**: eliminates `result`/`exclude` map allocs for PIDs 0–63
- **`activePartBits`/`pendingPartBits`**: precomputed bitsets eliminate `r.desc.Partitions[pid]` map lookups in the inner loop for `lookbackPeriod=0`

### 2. Cache / Memory (iters 12, 16, 17)
- **Bounded LRU(1024)**: prevents unbounded map growth → eliminates GC madvise from bucket reallocations
- **`pidTokenCounts *[64]uint16`**: pointer instead of inline 128-byte array → compact subrings shrink from 320 to 200 bytes
- **Direct-mapped zero-alloc cache**: replaces hashicorp LRU with a fixed-slot array indexed by `FNV-1a(key) % N`; `set()` is an in-place overwrite with no heap allocation; eliminates 19% of all allocated bytes

### 3. Search / Lookup (iters 13, 15, 18)
- **Prefix-sum bucket table** (`tokenBuckets *[N]uint16`): narrows binary search on the sorted token ring from O(log 16384)=14 comparisons to O(log 4)=2 (4096 buckets) then O(1) (16384 buckets, ~1 token per bucket)
- Build: `tb[t>>shift+1]++` then prefix-sum sweep
- Query: `lo = tb[key>>shift]`, `hi = tb[key>>shift+1]`, binary search on `[lo,hi)`

---

## Architecture of the Final Implementation

### `PartitionRing` struct key fields
```go
type PartitionRing struct {
    desc              PartitionRingDesc    // proto desc (16 bytes, map ptr)
    ringTokens        Tokens               // sorted tokens (root only)
    ringPartitionIDs  []int32              // parallel PID array (root only)
    parent            *PartitionRing       // non-nil for compact subrings
    selectedBits      uint64               // bitset of selected PIDs 0-63
    tokenCountVal     int                  // pre-computed token count
    indicesOnce       sync.Once
    ringTokenIndices  []uint16             // lazy-init indices into parent.ringTokens
    ownersByPartition map[int32][]string
    shuffleShardCache *partitionRingShuffleShardCache
    activePartitionsCount int
    pidTokenCounts    *[64]uint16          // root only; pointer saves 120B in compact subrings
    tokenBuckets      *[16385]uint16       // 16384-bucket prefix-sum, root only
    activePartBits    uint64               // precomputed active-PID bitset (root only)
    pendingPartBits   uint64               // precomputed pending-PID bitset (root only)
    allPIDsFitBitset  bool                 // enables zero-alloc bitset fast path
    opts              PartitionRingOptions
}
// Struct size: 200 bytes
```

### `directMappedCacheStorage` (new in iter 17)
```go
type directMappedCacheStorage[V any] struct {
    mask  uint64
    slots []directMappedSlot[V]    // pre-allocated, never grows
}
type directMappedSlot[V any] struct {
    key   subringCacheKey           // 32 bytes
    value V
    valid bool
}
// set(): hash key → slot index → overwrite (no alloc)
// get(): hash key → slot index → compare key
```

### `shuffleShard` bitset fast path (lookbackPeriod=0, all PIDs < 64)
```
For each of the k=8 shard slots:
  1. splitmix64(rngState) → random uint32 key
  2. searchRingToken(key): tokenBuckets[key>>18] → lo/hi → 0-1 binary search → ring position
  3. ringPartitionIDs[pos] → pid
  4. Check resultBits / excludeBits via bit operations (no alloc)
  5. pendingPartBits & pidBit? → exclude
  6. activePartBits & pidBit? → include (found = true)
  7. else → exclude
newSubringFromBits(resultBits):
  - tokenCount = Σ pidTokenCounts[bit] for each set bit  [O(k)]
  - activeParts = OnesCount64(resultBits & activePartBits) [O(1)]
  - return &PartitionRing{parent: r, selectedBits: resultBits, ...}
```

---

## Current Bottleneck Analysis (after iter 18, at 322 ns/op)

From CPU profile:
| Metric | % CPU | ~ns/op | Notes |
|---|---|---|---|
| `runtime.kevent` + GC overhead | ~65% | ~209 ns | Collecting discarded compact subrings |
| `searchRingToken` | 7.3% | 23 ns | 8 calls × ~3 ns each (near optimal) |
| `crypto/md5.block` | 6.1% | 20 ns | Seed computation; changing hash breaks sharding semantics |
| `runtime.madvise` | 7.2% | 23 ns | GC page decommit; proportional to alloc rate |
| `shuffleShard` (direct) | 2.6% | 8 ns | Inner loop work |

**Root cause**: ~65% of time is GC overhead from collecting one 200-byte `PartitionRing` allocated per benchmark iteration. The benchmark uses unique identifiers, so the cache always misses and always allocates.

**Remaining allocs**: 4 allocs / 248 B per call:
1. `newSubringFromBits` → `*PartitionRing` (200 bytes) — unavoidable given the `*PartitionRing` return type
2. `md5.New()` (shard seed) — ~16 bytes
3. `md5.Sum(nil)` (checksum slice) — ~16 bytes  
4. `fmt.Sprintf` (benchmark harness) — ~16 bytes

---

## Approaches Explored but Not Viable

### `rootData *partitionRingRootData` struct (tried, discarded)
Moving 5 root-ring-only fields (`pidTokenCounts`, `tokenBuckets`, `activePartBits`, `pendingPartBits`, `allPIDsFitBitset`) behind a single pointer saved 32 bytes per compact subring (200→168 bytes) but introduced extra pointer chasing in `searchRingToken` which accesses `tokenBuckets` 16× per `shuffleShard`. Net: 3.3% **slower**.

### `sync.Pool` for compact subrings
Safe only if callers never hold references across evictions. Not guaranteed for concurrent production use.

### `md5.Sum()` for `zone=""` case
Correct and saves 1 alloc, but only 16 bytes → 2% improvement, below 5% threshold.

---

## Potential Next Steps

If further improvement is needed (currently <306 ns/op is the 5% threshold):

1. **Separate compact-subring struct** (96 bytes) from root-ring struct (large refactor, changes public API shape)
2. **Epoch-based subring reclamation** — circular slab of N pre-allocated PartitionRings, reuse after guaranteed-quiescent period
3. **Inline `ShuffleShardSeed`** using a faster non-cryptographic hash (changes sharding — requires coordinated migration)
4. **Reduce compact subring further**: make `indicesOnce+ringTokenIndices` lazy via separate allocation (saves 24 bytes per compact subring, but adds 1 alloc for production callers that use the returned subring)
5. **Remove `ringTokens`/`ringPartitionIDs` slice headers** (48 bytes) behind a pointer — saves 40 bytes but adds deref for root-ring hot path (measured ~9% overhead)

---

## Files Modified

| File | Changes |
|---|---|
| `vendor/github.com/grafana/dskit/ring/partition_ring.go` | All algorithm + data-structure changes |
| `vendor/github.com/grafana/dskit/ring/partitions_ring_shuffle_shard_cache.go` | Added `directMappedCacheStorage`; replaced LRU with it |
| `vendor/github.com/grafana/dskit/ring/partition_ring_test.go` | Benchmark test file (read-only reference) |
| `autoresearch-results.tsv` | Results log |

The `shard/shard.go` file was temporarily modified (md5.Sum optimisation) but reverted.
