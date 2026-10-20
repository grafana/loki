package indexgateway

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"path"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/config"
	indexstore "github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// IndexOwnersRead is the operation used to find the instances that serve an
// index (tenant, table): ACTIVE instances only. Unlike IndexesRead, it extends
// the replica set past instances that are not ACTIVE, so while a new owner is
// still JOINING the instance it is replacing is still returned.
var IndexOwnersRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, func(s ring.InstanceState) bool {
	return s != ring.ACTIVE
})

// PerIndexOwnershipConfig configures per-index ownership on the index gateway:
// each (tenant, table) index is owned by replication-factor gateways, which
// preload it before they go ACTIVE in the ring.
type PerIndexOwnershipConfig struct {
	Enabled                  bool          `yaml:"enabled" category:"experimental"`
	WaitStabilityMinDuration time.Duration `yaml:"wait_stability_min_duration" category:"experimental"`
	WaitStabilityMaxDuration time.Duration `yaml:"wait_stability_max_duration" category:"experimental"`
	PreloadTimeout           time.Duration `yaml:"preload_timeout" category:"experimental"`
	RingCheckPeriod          time.Duration `yaml:"ring_check_period" category:"experimental"`
}

// RegisterFlagsWithPrefix registers flags.
func (cfg *PerIndexOwnershipConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false,
		"Experimental. Give each (tenant, table) index replication-factor owners in the index gateway ring. An index gateway preloads only the indexes it owns, after it joins the ring and before it becomes ACTIVE. Requires -index-gateway.mode=ring.")
	f.DurationVar(&cfg.WaitStabilityMinDuration, prefix+"wait-stability-min-duration", 0,
		"Experimental. Minimum time the ring must be stable before the index gateway preloads its owned indexes. Set it when many index gateways join at once, such as when the ring is created or scaled up, so that each one preloads what it owns once every new instance has registered its tokens. 0 disables the wait.")
	f.DurationVar(&cfg.WaitStabilityMaxDuration, prefix+"wait-stability-max-duration", 5*time.Minute,
		"Experimental. Maximum time to wait for the ring to be stable before preloading anyway. Only used when -index-gateway.per-index-ownership.wait-stability-min-duration is greater than 0.")
	f.DurationVar(&cfg.PreloadTimeout, prefix+"preload-timeout", 30*time.Minute,
		"Experimental. Maximum time to preload owned indexes before becoming ACTIVE anyway. Indexes not loaded by then are loaded by the periodic query readiness loop. 0 means no timeout.")
	cfg.registerReconcileFlags(prefix, f)
}

// Validate validates the config.
func (cfg *PerIndexOwnershipConfig) Validate(mode Mode) error {
	if !cfg.Enabled {
		return nil
	}
	if mode != RingMode {
		return errors.New("index-gateway.per-index-ownership.enabled requires index-gateway.mode=ring")
	}
	if cfg.WaitStabilityMinDuration < 0 || cfg.WaitStabilityMaxDuration < 0 || cfg.PreloadTimeout < 0 || cfg.RingCheckPeriod < 0 {
		return errors.New("index-gateway.per-index-ownership durations must not be negative")
	}
	if cfg.WaitStabilityMinDuration > 0 && cfg.WaitStabilityMaxDuration < cfg.WaitStabilityMinDuration {
		return errors.New("index-gateway.per-index-ownership.wait-stability-max-duration must be at least wait-stability-min-duration")
	}
	return nil
}

// WholeIndexShard is the shard of an index that is not split into
// fingerprint-range shards: `0_of_1`, the whole fingerprint space. It is the
// shard of every (tenant, table) whose period shard_factor is 0 or 1.
var WholeIndexShard = index.NewShard(0, 1)

// IndexOwnershipKey returns the ring key of the shard of the index of tenant in
// table, where table is the full table name (e.g. "index_19500") and shard is
// one of the period's shard_factor fingerprint-range shards, or
// WholeIndexShard. The key hashes "tenant/table/<x>_of_<K>". Index gateways
// and their clients must use the same key.
func IndexOwnershipKey(tenant, table string, shard index.ShardAnnotation) uint32 {
	if shard.Of < 2 {
		shard = WholeIndexShard
	}
	d := xxhash.New()
	_, _ = d.WriteString(tenant)
	_, _ = d.WriteString("/")
	_, _ = d.WriteString(table)
	_, _ = d.WriteString("/")
	_, _ = d.WriteString(shard.String())
	return uint32(d.Sum64())
}

// IndexShards returns the shards the index of a tenant is split into for a
// shard factor k: one shard per fingerprint range when k is greater than 1,
// otherwise WholeIndexShard.
func IndexShards(k int) []index.ShardAnnotation {
	if k < 2 {
		return []index.ShardAnnotation{WholeIndexShard}
	}
	shards := make([]index.ShardAnnotation, 0, k)
	for x := range k {
		shards = append(shards, index.NewShard(uint32(x), uint32(k)))
	}
	return shards
}

// shardBounds returns the [from, through) fingerprint range of a shard, the
// whole space for WholeIndexShard.
func shardBounds(shard index.ShardAnnotation) (model.Fingerprint, model.Fingerprint) {
	if shard.Of < 2 {
		return 0, model.Fingerprint(math.MaxUint64)
	}
	return shard.GetFromThrough()
}

// rangesOverlap reports whether the fingerprint ranges intersect.
// MaxUint64 is an inclusive upper sentinel.
func rangesOverlap(aFrom, aThrough, bFrom, bThrough model.Fingerprint) bool {
	return index.FingerprintRangesOverlap(aFrom, aThrough, bFrom, bThrough)
}

// ShardsOverlapping returns the shards of k whose fingerprint range intersects
// [from, through), in fingerprint order. A filter that is the whole space
// returns every shard.
func ShardsOverlapping(k int, from, through model.Fingerprint) []index.ShardAnnotation {
	var overlapping []index.ShardAnnotation
	for _, shard := range IndexShards(k) {
		sFrom, sThrough := shardBounds(shard)
		if rangesOverlap(sFrom, sThrough, from, through) {
			overlapping = append(overlapping, shard)
		}
	}
	return overlapping
}

// IndexFileFingerprintBounds returns the [from, through) fingerprint range an
// index file holds, from its name: a file the compactor split into shards is
// named `..-<x>_of_<K>.tsdb`. Any other file holds every fingerprint.
func IndexFileFingerprintBounds(name string) (model.Fingerprint, model.Fingerprint) {
	name = strings.TrimSuffix(path.Base(name), ".gz")
	trimmed := strings.TrimSuffix(name, ".tsdb")
	if trimmed == name {
		return 0, model.Fingerprint(math.MaxUint64)
	}
	suffix := trimmed[strings.LastIndex(trimmed, "-")+1:]
	shard, err := index.ParseShardString(suffix)
	if err != nil {
		return 0, model.Fingerprint(math.MaxUint64)
	}
	return shardBounds(shard)
}

// IndexOwnership answers which index gateways own a shard of the index of a
// (tenant, table). Owners are found on the full ring, with the ring's
// replication factor. It is used by index gateways and by their clients.
type IndexOwnership struct {
	r ring.ReadRing
}

// NewIndexOwnership returns an IndexOwnership over r.
func NewIndexOwnership(r ring.ReadRing) *IndexOwnership {
	return &IndexOwnership{r: r}
}

// Owners returns the instances that own shard of the index of tenant in table
// for op: IndexesSync for loading, IndexOwnersRead for routing and dropping.
func (o *IndexOwnership) Owners(tenant, table string, shard index.ShardAnnotation, op ring.Operation) (ring.ReplicationSet, error) {
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()
	return o.r.Get(IndexOwnershipKey(tenant, table, shard), op, bufDescs, bufHosts, bufZones)
}

// Owns reports whether the instance at instanceAddr owns shard of the index of
// tenant in table for op.
func (o *IndexOwnership) Owns(instanceAddr, tenant, table string, shard index.ShardAnnotation, op ring.Operation) (bool, error) {
	rs, err := o.Owners(tenant, table, shard, op)
	if err != nil {
		return false, err
	}
	return rs.Includes(instanceAddr), nil
}

// OwnedShards returns the shards of the index of tenant in table, split k
// ways, that the instance at instanceAddr owns for op.
func (o *IndexOwnership) OwnedShards(instanceAddr, tenant, table string, k int, op ring.Operation) ([]index.ShardAnnotation, error) {
	var owned []index.ShardAnnotation
	for _, shard := range IndexShards(k) {
		ok, err := o.Owns(instanceAddr, tenant, table, shard, op)
		if err != nil {
			return nil, err
		}
		if ok {
			owned = append(owned, shard)
		}
	}
	return owned, nil
}

// IndexOwnershipFilter keeps the tenants, and their index files, whose index
// in a table is owned by one index gateway instance.
type IndexOwnershipFilter struct {
	ownership    *IndexOwnership
	r            ring.ReadRing
	instanceAddr string
	tables       config.TableRanges
}

// NewIndexOwnershipFilter returns a filter for the instance at instanceAddr.
// tables gives the schema period, and so the shard factor, of each table.
func NewIndexOwnershipFilter(r ring.ReadRing, instanceAddr string, tables config.TableRanges) *IndexOwnershipFilter {
	return &IndexOwnershipFilter{ownership: NewIndexOwnership(r), r: r, instanceAddr: instanceAddr, tables: tables}
}

// shardFactor returns the shard_factor of the schema period of table, or 1
// for a table of no period.
func (f *IndexOwnershipFilter) shardFactor(table string) int {
	n, err := config.ExtractTableNumberFromName(table)
	if err != nil {
		return 1
	}
	if p := f.tables.ConfigForTableNumber(n); p != nil {
		return p.IndexTables.ShardFactor
	}
	return 1
}

// requireInRing fails unless this instance is in the ring for op. As in
// ShuffleShardingStrategy, an instance that has dropped out would otherwise
// conclude it owns nothing.
func (f *IndexOwnershipFilter) requireInRing(op ring.Operation) error {
	set, err := f.r.GetAllHealthy(op)
	if err != nil {
		return err
	}
	if !set.Includes(f.instanceAddr) {
		return errGatewayUnhealthy
	}
	return nil
}

// FilterTenants returns the tenants of which this instance owns at least one
// shard of the index in table, for loading. It matches downloads.TenantFilter.
func (f *IndexOwnershipFilter) FilterTenants(table string, tenantIDs []string) ([]string, error) {
	if err := f.requireInRing(IndexesSync); err != nil {
		return nil, err
	}

	k := f.shardFactor(table)
	var owned []string
	for _, tenantID := range tenantIDs {
		shards, err := f.ownership.OwnedShards(f.instanceAddr, tenantID, table, k, IndexesSync)
		if err != nil {
			return nil, err
		}
		if len(shards) > 0 {
			owned = append(owned, tenantID)
		}
	}
	return owned, nil
}

// FilterFiles returns the index files of tenant in table whose fingerprint
// range overlaps a shard this instance owns, for loading (IndexesSync) or for
// routing (IndexOwnersRead, which keeps an instance serving a shard while its
// new owner is still JOINING). A file that is not split into shards holds
// every fingerprint and so is kept whenever any shard is owned. It matches
// downloads.FileFilter.
func (f *IndexOwnershipFilter) FilterFiles(table, tenantID string, files []storage.IndexFile) ([]storage.IndexFile, error) {
	if err := f.requireInRing(IndexesSync); err != nil {
		return nil, err
	}

	k := f.shardFactor(table)
	owned, err := f.ownership.OwnedShards(f.instanceAddr, tenantID, table, k, IndexesSync)
	if err != nil {
		return nil, err
	}
	serving, err := f.ownership.OwnedShards(f.instanceAddr, tenantID, table, k, IndexOwnersRead)
	if err != nil {
		return nil, err
	}
	owned = append(owned, serving...)

	kept := make([]storage.IndexFile, 0, len(files))
	for _, file := range files {
		fileFrom, fileThrough := IndexFileFingerprintBounds(file.Name)
		for _, shard := range owned {
			sFrom, sThrough := shardBounds(shard)
			if rangesOverlap(fileFrom, fileThrough, sFrom, sThrough) {
				kept = append(kept, file)
				break
			}
		}
	}
	return kept, nil
}

// NewOwnedIndexPreload returns the function an index gateway runs after it is
// JOINING in the ring and before it becomes ACTIVE: wait for the ring to
// settle, then preload the indexes it owns. A preload that times out is logged
// and does not stop the instance from becoming ACTIVE; any other preload error
// is returned.
func NewOwnedIndexPreload(cfg PerIndexOwnershipConfig, r ring.ReadRing, store indexstore.QueryReadinessEnsurer, logger log.Logger) func(context.Context) error {
	return func(ctx context.Context) error {
		if cfg.WaitStabilityMinDuration > 0 {
			level.Info(logger).Log("msg", "waiting for the index gateway ring to be stable before preloading", "min_duration", cfg.WaitStabilityMinDuration, "max_duration", cfg.WaitStabilityMaxDuration)
			start := time.Now()
			if err := ring.WaitRingTokensStability(ctx, r, IndexesSync, cfg.WaitStabilityMinDuration, cfg.WaitStabilityMaxDuration); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				level.Warn(logger).Log("msg", "index gateway ring is not stable, preloading anyway", "waited", time.Since(start), "err", err)
			} else {
				level.Info(logger).Log("msg", "index gateway ring is stable", "waited", time.Since(start))
			}
		}

		preloadCtx := ctx
		if cfg.PreloadTimeout > 0 {
			var cancel context.CancelFunc
			preloadCtx, cancel = context.WithTimeout(ctx, cfg.PreloadTimeout)
			defer cancel()
		}

		level.Info(logger).Log("msg", "preloading owned indexes")
		start := time.Now()
		err := store.EnsureQueryReadiness(preloadCtx)
		switch {
		case err == nil:
			level.Info(logger).Log("msg", "preloaded owned indexes", "duration", time.Since(start))
			return nil
		case ctx.Err() != nil:
			return ctx.Err()
		case preloadCtx.Err() != nil:
			level.Warn(logger).Log("msg", "preloading owned indexes timed out, becoming ACTIVE anyway", "duration", time.Since(start), "timeout", cfg.PreloadTimeout, "err", err)
			return nil
		default:
			return fmt.Errorf("preload owned indexes: %w", err)
		}
	}
}
