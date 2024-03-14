package bloomcompactor

import (
	"fmt"
	"math"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/config"
)

type tableRangeProgress struct {
	tenant string
	table  config.DayTime
	bounds v1.FingerprintBounds

	lastFP model.Fingerprint
}

type compactionTracker struct {
	sync.Mutex

	nTables int
	// tables -> n_tenants
	metadata map[config.DayTime]int

	// table -> tenant -> workload_id -> keyspace
	tables map[config.DayTime]map[string]map[string]*tableRangeProgress
}

func newCompactionTracker(nTables int) (*compactionTracker, error) {
	if nTables <= 0 {
		return nil, errors.New("nTables must be positive")
	}

	return &compactionTracker{
		nTables:  nTables,
		tables:   make(map[config.DayTime]map[string]map[string]*tableRangeProgress),
		metadata: make(map[config.DayTime]int),
	}, nil
}

func (c *compactionTracker) registerTable(tbl config.DayTime, nTenants int) {
	c.Lock()
	defer c.Unlock()
	c.metadata[tbl] = nTenants
	c.tables[tbl] = make(map[string]map[string]*tableRangeProgress)
}

func (c *compactionTracker) update(
	tenant string,
	table config.DayTime,
	bounds v1.FingerprintBounds,
	mostRecentFP model.Fingerprint,
) {
	c.Lock()
	defer c.Unlock()
	key := fmt.Sprintf("%s_%s_%s", tenant, table.String(), bounds.String())
	tbl, ok := c.tables[table]
	if !ok {
		panic(fmt.Sprintf("table not registered: %s", table.String()))
	}
	workloads, ok := tbl[tenant]
	if !ok {
		workloads = make(map[string]*tableRangeProgress)
		tbl[tenant] = workloads
	}
	workloads[key] = &tableRangeProgress{
		tenant: tenant,
		table:  table,
		bounds: bounds,
		// ensure lastFP is at least the minimum fp for each range;
		// this handles the case when the first fingeprint hasn't been processed yet.
		// as a precaution we also clip the lastFP to the bounds.
		lastFP: min(max(mostRecentFP, bounds.Min), bounds.Max),
	}
}

// Returns progress in (0-1) range, bounded to 3 decimals.
// compaction progress is measured by the following:
// 1. The number of days of data that has been compacted
// as a percentage of the total number of days of data that needs to be compacted.
// 2. Within each day, the number of tenants that have been compacted
// as a percentage of the total number of tenants that need to be compacted.
// 3. Within each tenant, the percent of the keyspaces that have been compacted.
// NB(owen-d): this treats all tenants equally, when this may not be the case wrt
// the work they have to do. This is a simplification and can be x-referenced with
// the tenant_compaction_seconds_total metric to see how much time is being spent on
// each tenant while the compaction tracker shows total compaction progress across
// all tables and tenants.
func (c *compactionTracker) progress() (progress float64) {
	c.Lock()
	defer c.Unlock()

	perTablePct := 1. / float64(c.nTables)

	// for all registered tables, determine the number of registered tenants
	for tbl, nTenants := range c.metadata {
		perTenantPct := perTablePct / float64(nTenants)

		// iterate tenants in each table
		for _, tenant := range c.tables[tbl] {
			var (
				totalKeyspace    uint64
				finishedKeyspace uint64
			)

			// iterate table ranges for each tenant+table pair
			for _, batch := range tenant {
				totalKeyspace += batch.bounds.Range()
				finishedKeyspace += uint64(batch.lastFP - batch.bounds.Min)
			}

			tenantProgress := float64(finishedKeyspace) / float64(totalKeyspace)
			progress += perTenantPct * tenantProgress
		}
	}

	return math.Round(progress*1000) / 1000
}
