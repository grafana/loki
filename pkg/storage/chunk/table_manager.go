package chunk

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/mtime"

	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	readLabel  = "read"
	writeLabel = "write"

	bucketRetentionEnforcementInterval = 12 * time.Hour
)

type tableManagerMetrics struct {
	syncTableDuration  *prometheus.HistogramVec
	tableCapacity      *prometheus.GaugeVec
	createFailures     prometheus.Gauge
	deleteFailures     prometheus.Gauge
	lastSuccessfulSync prometheus.Gauge
}

func newTableManagerMetrics(r prometheus.Registerer) *tableManagerMetrics {
	m := tableManagerMetrics{}
	m.syncTableDuration = promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "table_manager_sync_duration_seconds",
		Help:      "Time spent synching tables.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation", "status_code"})

	m.tableCapacity = promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "table_capacity_units",
		Help:      "Per-table capacity, measured in DynamoDB capacity units.",
	}, []string{"op", "table"})

	m.createFailures = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "table_manager_create_failures",
		Help:      "Number of table creation failures during the last table-manager reconciliation",
	})
	m.deleteFailures = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "table_manager_delete_failures",
		Help:      "Number of table deletion failures during the last table-manager reconciliation",
	})

	m.lastSuccessfulSync = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "table_manager_sync_success_timestamp_seconds",
		Help:      "Timestamp of the last successful table manager sync.",
	})

	return &m
}

// ExtraTables holds the list of tables that TableManager has to manage using a TableClient.
// This is useful for managing tables other than Chunk and Index tables.
type ExtraTables struct {
	TableClient TableClient
	Tables      []TableDesc
}

// TableManagerConfig holds config for a TableManager
type TableManagerConfig struct {
	// Master 'off-switch' for table capacity updates, e.g. when troubleshooting
	ThroughputUpdatesDisabled bool `yaml:"throughput_updates_disabled"`

	// Master 'on-switch' for table retention deletions
	RetentionDeletesEnabled bool `yaml:"retention_deletes_enabled"`

	// How far back tables will be kept before they are deleted
	RetentionPeriod time.Duration `yaml:"-"`
	// This is so that we can accept 1w, 1y in the YAML.
	RetentionPeriodModel model.Duration `yaml:"retention_period"`

	// Period with which the table manager will poll for tables.
	PollInterval time.Duration `yaml:"poll_interval"`

	// duration a table will be created before it is needed.
	CreationGracePeriod time.Duration `yaml:"creation_grace_period"`

	IndexTables ProvisionConfig `yaml:"index_tables_provisioning"`
	ChunkTables ProvisionConfig `yaml:"chunk_tables_provisioning"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface. To support RetentionPeriod.
func (cfg *TableManagerConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// If we call unmarshal on TableManagerConfig, it will call UnmarshalYAML leading to infinite recursion.
	// To make unmarshal fill the plain data struct rather than calling UnmarshalYAML
	// again, we have to hide it using a type indirection.
	type plain TableManagerConfig
	if err := unmarshal((*plain)(cfg)); err != nil {
		return err
	}

	if cfg.RetentionPeriodModel > 0 {
		cfg.RetentionPeriod = time.Duration(cfg.RetentionPeriodModel)
	}

	return nil
}

// MarshalYAML implements the yaml.Marshaler interface. To support RetentionPeriod.
func (cfg *TableManagerConfig) MarshalYAML() (interface{}, error) {
	cfg.RetentionPeriodModel = model.Duration(cfg.RetentionPeriod)
	return cfg, nil
}

// Validate validates the config.
func (cfg *TableManagerConfig) Validate() error {
	// We're setting this field because when using flags, you set the RetentionPeriodModel but not RetentionPeriod.
	// TODO(gouthamve): Its a hack, but I can't think of any other way :/
	if cfg.RetentionPeriodModel > 0 {
		cfg.RetentionPeriod = time.Duration(cfg.RetentionPeriodModel)
	}

	return nil
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *TableManagerConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.ThroughputUpdatesDisabled, "table-manager.throughput-updates-disabled", false, "If true, disable all changes to DB capacity")
	f.BoolVar(&cfg.RetentionDeletesEnabled, "table-manager.retention-deletes-enabled", false, "If true, enables retention deletes of DB tables")
	f.Var(&cfg.RetentionPeriodModel, "table-manager.retention-period", "Tables older than this retention period are deleted. Must be either 0 (disabled) or a multiple of 24h. When enabled, be aware this setting is destructive to data!")
	f.DurationVar(&cfg.PollInterval, "table-manager.poll-interval", 2*time.Minute, "How frequently to poll backend to learn our capacity.")
	f.DurationVar(&cfg.CreationGracePeriod, "table-manager.periodic-table.grace-period", 10*time.Minute, "Periodic tables grace period (duration which table will be created/deleted before/after it's needed).")

	cfg.IndexTables.RegisterFlags("table-manager.index-table", f)
	cfg.ChunkTables.RegisterFlags("table-manager.chunk-table", f)
}

// TableManager creates and manages the provisioned throughput on DynamoDB tables
type TableManager struct {
	services.Service

	client       TableClient
	cfg          TableManagerConfig
	schemaCfg    SchemaConfig
	maxChunkAge  time.Duration
	bucketClient BucketClient
	metrics      *tableManagerMetrics
	extraTables  []ExtraTables

	bucketRetentionLoop services.Service
}

// NewTableManager makes a new TableManager
func NewTableManager(cfg TableManagerConfig, schemaCfg SchemaConfig, maxChunkAge time.Duration, tableClient TableClient,
	objectClient BucketClient, extraTables []ExtraTables, registerer prometheus.Registerer) (*TableManager, error) {

	if cfg.RetentionPeriod != 0 {
		// Assume the newest config is the one to use for validation of retention
		indexTablesPeriod := schemaCfg.Configs[len(schemaCfg.Configs)-1].IndexTables.Period
		if indexTablesPeriod != 0 && cfg.RetentionPeriod%indexTablesPeriod != 0 {
			return nil, errors.New("retention period should now be a multiple of periodic table duration")
		}
	}

	tm := &TableManager{
		cfg:          cfg,
		schemaCfg:    schemaCfg,
		maxChunkAge:  maxChunkAge,
		client:       tableClient,
		bucketClient: objectClient,
		metrics:      newTableManagerMetrics(registerer),
		extraTables:  extraTables,
	}

	tm.Service = services.NewBasicService(tm.starting, tm.loop, tm.stopping)
	return tm, nil
}

// Start the TableManager
func (m *TableManager) starting(ctx context.Context) error {
	if m.bucketClient != nil && m.cfg.RetentionPeriod != 0 && m.cfg.RetentionDeletesEnabled {
		m.bucketRetentionLoop = services.NewTimerService(bucketRetentionEnforcementInterval, nil, m.bucketRetentionIteration, nil)
		return services.StartAndAwaitRunning(ctx, m.bucketRetentionLoop)
	}
	return nil
}

// Stop the TableManager
func (m *TableManager) stopping(_ error) error {
	if m.bucketRetentionLoop != nil {
		return services.StopAndAwaitTerminated(context.Background(), m.bucketRetentionLoop)
	}
	m.client.Stop()
	return nil
}

func (m *TableManager) loop(ctx context.Context) error {
	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	if err := instrument.CollectedRequest(context.Background(), "TableManager.SyncTables", instrument.NewHistogramCollector(m.metrics.syncTableDuration), instrument.ErrorCode, func(ctx context.Context) error {
		return m.SyncTables(ctx)
	}); err != nil {
		level.Error(util_log.Logger).Log("msg", "error syncing tables", "err", err)
	}

	// Sleep for a bit to spread the sync load across different times if the tablemanagers are all started at once.
	select {
	case <-time.After(time.Duration(rand.Int63n(int64(m.cfg.PollInterval)))):
	case <-ctx.Done():
		return nil
	}

	for {
		select {
		case <-ticker.C:
			if err := instrument.CollectedRequest(context.Background(), "TableManager.SyncTables", instrument.NewHistogramCollector(m.metrics.syncTableDuration), instrument.ErrorCode, func(ctx context.Context) error {
				return m.SyncTables(ctx)
			}); err != nil {
				level.Error(util_log.Logger).Log("msg", "error syncing tables", "err", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *TableManager) checkAndCreateExtraTables() error {
	for _, extraTables := range m.extraTables {
		existingTablesList, err := extraTables.TableClient.ListTables(context.Background())
		if err != nil {
			return err
		}

		existingTablesMap := map[string]struct{}{}
		for _, table := range existingTablesList {
			existingTablesMap[table] = struct{}{}
		}

		for _, tableDesc := range extraTables.Tables {
			if _, ok := existingTablesMap[tableDesc.Name]; !ok {
				// creating table
				level.Info(util_log.Logger).Log("msg", "creating extra table",
					"tableName", tableDesc.Name,
					"provisionedRead", tableDesc.ProvisionedRead,
					"provisionedWrite", tableDesc.ProvisionedWrite,
					"useOnDemandMode", tableDesc.UseOnDemandIOMode,
					"useWriteAutoScale", tableDesc.WriteScale.Enabled,
					"useReadAutoScale", tableDesc.ReadScale.Enabled,
				)
				err = extraTables.TableClient.CreateTable(context.Background(), tableDesc)
				if err != nil {
					return err
				}
				continue
			} else if m.cfg.ThroughputUpdatesDisabled {
				// table already exists, throughput updates are disabled so no need to check for difference in configured throuhput vs actual
				continue
			}

			level.Info(util_log.Logger).Log("msg", "checking throughput of extra table", "table", tableDesc.Name)
			// table already exists, lets check actual throughput for tables is same as what is in configurations, if not let us update it
			current, _, err := extraTables.TableClient.DescribeTable(context.Background(), tableDesc.Name)
			if err != nil {
				return err
			}

			if !current.Equals(tableDesc) {
				level.Info(util_log.Logger).Log("msg", "updating throughput of extra table",
					"table", tableDesc.Name,
					"tableName", tableDesc.Name,
					"provisionedRead", tableDesc.ProvisionedRead,
					"provisionedWrite", tableDesc.ProvisionedWrite,
					"useOnDemandMode", tableDesc.UseOnDemandIOMode,
					"useWriteAutoScale", tableDesc.WriteScale.Enabled,
					"useReadAutoScale", tableDesc.ReadScale.Enabled,
				)
				err := extraTables.TableClient.UpdateTable(context.Background(), current, tableDesc)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// single iteration of bucket retention loop
func (m *TableManager) bucketRetentionIteration(ctx context.Context) error {
	err := m.bucketClient.DeleteChunksBefore(ctx, mtime.Now().Add(-m.cfg.RetentionPeriod))
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error enforcing filesystem retention", "err", err)
	}

	// don't return error, otherwise timer service would stop.
	return nil
}

// SyncTables will calculate the tables expected to exist, create those that do
// not and update those that need it.  It is exposed for testing.
func (m *TableManager) SyncTables(ctx context.Context) error {
	err := m.checkAndCreateExtraTables()
	if err != nil {
		return err
	}

	expected := m.calculateExpectedTables()
	level.Info(util_log.Logger).Log("msg", "synching tables", "expected_tables", len(expected))

	toCreate, toCheckThroughput, toDelete, err := m.partitionTables(ctx, expected)
	if err != nil {
		return err
	}

	if err := m.deleteTables(ctx, toDelete); err != nil {
		return err
	}

	if err := m.createTables(ctx, toCreate); err != nil {
		return err
	}

	if err := m.updateTables(ctx, toCheckThroughput); err != nil {
		return err
	}

	m.metrics.lastSuccessfulSync.SetToCurrentTime()
	return nil
}

func (m *TableManager) calculateExpectedTables() []TableDesc {
	result := []TableDesc{}

	for i, config := range m.schemaCfg.Configs {
		// Consider configs which we are about to hit and requires tables to be created due to grace period
		if config.From.Time.Time().After(mtime.Now().Add(m.cfg.CreationGracePeriod)) {
			continue
		}
		if config.IndexTables.Period == 0 { // non-periodic table
			if len(result) > 0 && result[len(result)-1].Name == config.IndexTables.Prefix {
				continue // already got a non-periodic table with this name
			}

			table := TableDesc{
				Name:              config.IndexTables.Prefix,
				ProvisionedRead:   m.cfg.IndexTables.InactiveReadThroughput,
				ProvisionedWrite:  m.cfg.IndexTables.InactiveWriteThroughput,
				UseOnDemandIOMode: m.cfg.IndexTables.InactiveThroughputOnDemandMode,
				Tags:              config.IndexTables.Tags,
			}
			isActive := true
			if i+1 < len(m.schemaCfg.Configs) {
				var (
					endTime         = m.schemaCfg.Configs[i+1].From.Unix()
					gracePeriodSecs = int64(m.cfg.CreationGracePeriod / time.Second)
					maxChunkAgeSecs = int64(m.maxChunkAge / time.Second)
					now             = mtime.Now().Unix()
				)
				if now >= endTime+gracePeriodSecs+maxChunkAgeSecs {
					isActive = false
				}
			}
			if isActive {
				table.ProvisionedRead = m.cfg.IndexTables.ProvisionedReadThroughput
				table.ProvisionedWrite = m.cfg.IndexTables.ProvisionedWriteThroughput
				table.UseOnDemandIOMode = m.cfg.IndexTables.ProvisionedThroughputOnDemandMode
				if m.cfg.IndexTables.WriteScale.Enabled {
					table.WriteScale = m.cfg.IndexTables.WriteScale
					table.UseOnDemandIOMode = false
				}
				if m.cfg.IndexTables.ReadScale.Enabled {
					table.ReadScale = m.cfg.IndexTables.ReadScale
					table.UseOnDemandIOMode = false
				}
			}
			result = append(result, table)
		} else {
			endTime := mtime.Now().Add(m.cfg.CreationGracePeriod)
			if i+1 < len(m.schemaCfg.Configs) {
				nextFrom := m.schemaCfg.Configs[i+1].From.Time.Time()
				if endTime.After(nextFrom) {
					endTime = nextFrom
				}
			}
			endModelTime := model.TimeFromUnix(endTime.Unix())
			result = append(result, config.IndexTables.periodicTables(
				config.From.Time, endModelTime, m.cfg.IndexTables, m.cfg.CreationGracePeriod, m.maxChunkAge, m.cfg.RetentionPeriod,
			)...)
			if config.ChunkTables.Prefix != "" {
				result = append(result, config.ChunkTables.periodicTables(
					config.From.Time, endModelTime, m.cfg.ChunkTables, m.cfg.CreationGracePeriod, m.maxChunkAge, m.cfg.RetentionPeriod,
				)...)
			}
		}
	}

	sort.Sort(byName(result))
	return result
}

// partitionTables works out tables that need to be created vs tables that need to be updated
func (m *TableManager) partitionTables(ctx context.Context, descriptions []TableDesc) ([]TableDesc, []TableDesc, []TableDesc, error) {
	tables, err := m.client.ListTables(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	existingTables := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		existingTables[table] = struct{}{}
	}

	expectedTables := make(map[string]TableDesc, len(descriptions))
	for _, desc := range descriptions {
		expectedTables[desc.Name] = desc
	}

	toCreate, toCheck, toDelete := []TableDesc{}, []TableDesc{}, []TableDesc{}
	for _, expectedTable := range expectedTables {
		if _, ok := existingTables[expectedTable.Name]; ok {
			toCheck = append(toCheck, expectedTable)
		} else {
			toCreate = append(toCreate, expectedTable)
		}
	}

	if m.cfg.RetentionPeriod > 0 {
		// Ensure we only delete tables which have a prefix managed by Cortex.
		tablePrefixes := map[string]struct{}{}
		for _, cfg := range m.schemaCfg.Configs {
			if cfg.IndexTables.Prefix != "" {
				tablePrefixes[cfg.IndexTables.Prefix] = struct{}{}
			}
			if cfg.ChunkTables.Prefix != "" {
				tablePrefixes[cfg.ChunkTables.Prefix] = struct{}{}
			}
		}

		for existingTable := range existingTables {
			if _, ok := expectedTables[existingTable]; !ok {
				for tblPrefix := range tablePrefixes {
					if strings.HasPrefix(existingTable, tblPrefix) {
						toDelete = append(toDelete, TableDesc{Name: existingTable})
						break
					}
				}
			}
		}
	}

	return toCreate, toCheck, toDelete, nil
}

func (m *TableManager) createTables(ctx context.Context, descriptions []TableDesc) error {
	numFailures := 0
	merr := tsdb_errors.NewMulti()

	for _, desc := range descriptions {
		level.Info(util_log.Logger).Log("msg", "creating table", "table", desc.Name)
		err := m.client.CreateTable(ctx, desc)
		if err != nil {
			numFailures++
			merr.Add(err)
		}
	}

	m.metrics.createFailures.Set(float64(numFailures))
	return merr.Err()
}

func (m *TableManager) deleteTables(ctx context.Context, descriptions []TableDesc) error {
	numFailures := 0
	merr := tsdb_errors.NewMulti()

	for _, desc := range descriptions {
		level.Info(util_log.Logger).Log("msg", "table has exceeded the retention period", "table", desc.Name)
		if !m.cfg.RetentionDeletesEnabled {
			continue
		}

		level.Info(util_log.Logger).Log("msg", "deleting table", "table", desc.Name)
		err := m.client.DeleteTable(ctx, desc.Name)
		if err != nil {
			numFailures++
			merr.Add(err)
		}
	}

	m.metrics.deleteFailures.Set(float64(numFailures))
	return merr.Err()
}

func (m *TableManager) updateTables(ctx context.Context, descriptions []TableDesc) error {
	for _, expected := range descriptions {
		level.Debug(util_log.Logger).Log("msg", "checking provisioned throughput on table", "table", expected.Name)
		current, isActive, err := m.client.DescribeTable(ctx, expected.Name)
		if err != nil {
			return err
		}

		m.metrics.tableCapacity.WithLabelValues(readLabel, expected.Name).Set(float64(current.ProvisionedRead))
		m.metrics.tableCapacity.WithLabelValues(writeLabel, expected.Name).Set(float64(current.ProvisionedWrite))

		if m.cfg.ThroughputUpdatesDisabled {
			continue
		}

		if !isActive {
			level.Info(util_log.Logger).Log("msg", "skipping update on table, not yet ACTIVE", "table", expected.Name)
			continue
		}

		if expected.Equals(current) {
			level.Info(util_log.Logger).Log("msg", "provisioned throughput on table, skipping", "table", current.Name, "read", current.ProvisionedRead, "write", current.ProvisionedWrite)
			continue
		}

		err = m.client.UpdateTable(ctx, current, expected)
		if err != nil {
			return err
		}
	}
	return nil
}

// ExpectTables compares existing tables to an expected set of tables.  Exposed
// for testing,
func ExpectTables(ctx context.Context, client TableClient, expected []TableDesc) error {
	tables, err := client.ListTables(ctx)
	if err != nil {
		return err
	}

	if len(expected) != len(tables) {
		return fmt.Errorf("Unexpected number of tables: %v != %v", expected, tables)
	}

	sort.Strings(tables)
	sort.Sort(byName(expected))

	for i, expect := range expected {
		if tables[i] != expect.Name {
			return fmt.Errorf("Expected '%s', found '%s'", expect.Name, tables[i])
		}

		desc, _, err := client.DescribeTable(ctx, expect.Name)
		if err != nil {
			return err
		}

		if !desc.Equals(expect) {
			return fmt.Errorf("Expected '%#v', found '%#v' for table '%s'", expect, desc, desc.Name)
		}
	}

	return nil
}
