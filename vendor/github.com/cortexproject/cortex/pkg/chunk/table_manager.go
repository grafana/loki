package chunk

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/mtime"
)

const (
	readLabel  = "read"
	writeLabel = "write"

	bucketRetentionEnforcementInterval = 12 * time.Hour
)

var (
	syncTableDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "dynamo_sync_tables_seconds",
		Help:      "Time spent doing SyncTables.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation", "status_code"}))
	tableCapacity = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "dynamo_table_capacity_units",
		Help:      "Per-table DynamoDB capacity, measured in DynamoDB capacity units.",
	}, []string{"op", "table"})
)

func init() {
	prometheus.MustRegister(tableCapacity)
	syncTableDuration.Register()
}

// TableManagerConfig holds config for a TableManager
type TableManagerConfig struct {
	// Master 'off-switch' for table capacity updates, e.g. when troubleshooting
	ThroughputUpdatesDisabled bool `yaml:"throughput_updates_disabled"`

	// Master 'on-switch' for table retention deletions
	RetentionDeletesEnabled bool `yaml:"retention_deletes_enabled"`

	// How far back tables will be kept before they are deleted
	RetentionPeriod time.Duration `yaml:"retention_period"`

	// Period with which the table manager will poll for tables.
	DynamoDBPollInterval time.Duration `yaml:"dynamodb_poll_interval"`

	// duration a table will be created before it is needed.
	CreationGracePeriod time.Duration `yaml:"creation_grace_period"`

	IndexTables ProvisionConfig `yaml:"index_tables_provisioning"`
	ChunkTables ProvisionConfig `yaml:"chunk_tables_provisioning"`
}

// ProvisionConfig holds config for provisioning capacity (on DynamoDB)
type ProvisionConfig struct {
	ProvisionedThroughputOnDemandMode bool  `yaml:"provisioned_throughput_on_demand_mode"`
	ProvisionedWriteThroughput        int64 `yaml:"provisioned_write_throughput"`
	ProvisionedReadThroughput         int64 `yaml:"provisioned_read_throughput"`
	InactiveThroughputOnDemandMode    bool  `yaml:"inactive_throughput_on_demand_mode"`
	InactiveWriteThroughput           int64 `yaml:"inactive_write_throughput"`
	InactiveReadThroughput            int64 `yaml:"inactive_read_throughput"`

	WriteScale              AutoScalingConfig `yaml:"write_scale"`
	InactiveWriteScale      AutoScalingConfig `yaml:"inactive_write_scale"`
	InactiveWriteScaleLastN int64             `yaml:"inactive_write_scale_lastn"`
	ReadScale               AutoScalingConfig `yaml:"read_scale"`
	InactiveReadScale       AutoScalingConfig `yaml:"inactive_read_scale"`
	InactiveReadScaleLastN  int64             `yaml:"inactive_read_scale_lastn"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *TableManagerConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.ThroughputUpdatesDisabled, "table-manager.throughput-updates-disabled", false, "If true, disable all changes to DB capacity")
	f.BoolVar(&cfg.RetentionDeletesEnabled, "table-manager.retention-deletes-enabled", false, "If true, enables retention deletes of DB tables")
	f.DurationVar(&cfg.RetentionPeriod, "table-manager.retention-period", 0, "Tables older than this retention period are deleted. Note: This setting is destructive to data!(default: 0, which disables deletion)")
	f.DurationVar(&cfg.DynamoDBPollInterval, "dynamodb.poll-interval", 2*time.Minute, "How frequently to poll DynamoDB to learn our capacity.")
	f.DurationVar(&cfg.CreationGracePeriod, "dynamodb.periodic-table.grace-period", 10*time.Minute, "DynamoDB periodic tables grace period (duration which table will be created/deleted before/after it's needed).")

	cfg.IndexTables.RegisterFlags("dynamodb.periodic-table", f)
	cfg.ChunkTables.RegisterFlags("dynamodb.chunk-table", f)
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *ProvisionConfig) RegisterFlags(argPrefix string, f *flag.FlagSet) {
	f.Int64Var(&cfg.ProvisionedWriteThroughput, argPrefix+".write-throughput", 1000, "DynamoDB table default write throughput.")
	f.Int64Var(&cfg.ProvisionedReadThroughput, argPrefix+".read-throughput", 300, "DynamoDB table default read throughput.")
	f.BoolVar(&cfg.ProvisionedThroughputOnDemandMode, argPrefix+".enable-ondemand-throughput-mode", false, "Enables on demand throughput provisioning for the storage provider (if supported). Applies only to tables which are not autoscaled")
	f.Int64Var(&cfg.InactiveWriteThroughput, argPrefix+".inactive-write-throughput", 1, "DynamoDB table write throughput for inactive tables.")
	f.Int64Var(&cfg.InactiveReadThroughput, argPrefix+".inactive-read-throughput", 300, "DynamoDB table read throughput for inactive tables.")
	f.BoolVar(&cfg.InactiveThroughputOnDemandMode, argPrefix+".inactive-enable-ondemand-throughput-mode", false, "Enables on demand throughput provisioning for the storage provider (if supported). Applies only to tables which are not autoscaled")

	cfg.WriteScale.RegisterFlags(argPrefix+".write-throughput.scale", f)
	cfg.InactiveWriteScale.RegisterFlags(argPrefix+".inactive-write-throughput.scale", f)
	f.Int64Var(&cfg.InactiveWriteScaleLastN, argPrefix+".inactive-write-throughput.scale-last-n", 4, "Number of last inactive tables to enable write autoscale.")

	cfg.ReadScale.RegisterFlags(argPrefix+".read-throughput.scale", f)
	cfg.InactiveReadScale.RegisterFlags(argPrefix+".inactive-read-throughput.scale", f)
	f.Int64Var(&cfg.InactiveReadScaleLastN, argPrefix+".inactive-read-throughput.scale-last-n", 4, "Number of last inactive tables to enable read autoscale.")
}

// TableManager creates and manages the provisioned throughput on DynamoDB tables
type TableManager struct {
	client       TableClient
	cfg          TableManagerConfig
	schemaCfg    SchemaConfig
	maxChunkAge  time.Duration
	done         chan struct{}
	wait         sync.WaitGroup
	bucketClient BucketClient
}

// NewTableManager makes a new TableManager
func NewTableManager(cfg TableManagerConfig, schemaCfg SchemaConfig, maxChunkAge time.Duration, tableClient TableClient,
	objectClient BucketClient) (*TableManager, error) {

	if cfg.RetentionPeriod != 0 {
		// Assume the newest config is the one to use for validation of retention
		indexTablesPeriod := schemaCfg.Configs[len(schemaCfg.Configs)-1].IndexTables.Period
		if indexTablesPeriod != 0 && cfg.RetentionPeriod%indexTablesPeriod != 0 {
			return nil, errors.New("retention period should now be a multiple of periodic table duration")
		}
	}

	return &TableManager{
		cfg:          cfg,
		schemaCfg:    schemaCfg,
		maxChunkAge:  maxChunkAge,
		client:       tableClient,
		done:         make(chan struct{}),
		bucketClient: objectClient,
	}, nil
}

// Start the TableManager
func (m *TableManager) Start() {
	m.wait.Add(1)
	go m.loop()

	if m.bucketClient != nil && m.cfg.RetentionPeriod != 0 && m.cfg.RetentionDeletesEnabled {
		m.wait.Add(1)
		go m.bucketRetentionLoop()
	}
}

// Stop the TableManager
func (m *TableManager) Stop() {
	close(m.done)
	m.wait.Wait()
}

func (m *TableManager) loop() {
	defer m.wait.Done()

	// Sleep for a bit to spread the sync load across different times if the tablemanagers are all started at once.
	time.Sleep(time.Duration(rand.Int63n(int64(m.cfg.DynamoDBPollInterval))))

	ticker := time.NewTicker(m.cfg.DynamoDBPollInterval)
	defer ticker.Stop()

	if err := instrument.CollectedRequest(context.Background(), "TableManager.SyncTables", syncTableDuration, instrument.ErrorCode, func(ctx context.Context) error {
		return m.SyncTables(ctx)
	}); err != nil {
		level.Error(util.Logger).Log("msg", "error syncing tables", "err", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := instrument.CollectedRequest(context.Background(), "TableManager.SyncTables", syncTableDuration, instrument.ErrorCode, func(ctx context.Context) error {
				return m.SyncTables(ctx)
			}); err != nil {
				level.Error(util.Logger).Log("msg", "error syncing tables", "err", err)
			}
		case <-m.done:
			return
		}
	}
}

func (m *TableManager) bucketRetentionLoop() {
	defer m.wait.Done()

	ticker := time.NewTicker(bucketRetentionEnforcementInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := m.bucketClient.DeleteChunksBefore(context.Background(), mtime.Now().Add(-m.cfg.RetentionPeriod))

			if err != nil {
				level.Error(util.Logger).Log("msg", "error enforcing filesystem retention", "err", err)
			}
		case <-m.done:
			return
		}
	}
}

// SyncTables will calculate the tables expected to exist, create those that do
// not and update those that need it.  It is exposed for testing.
func (m *TableManager) SyncTables(ctx context.Context) error {
	expected := m.calculateExpectedTables()
	level.Info(util.Logger).Log("msg", "synching tables", "expected_tables", len(expected))

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

	return m.updateTables(ctx, toCheckThroughput)
}

func (m *TableManager) calculateExpectedTables() []TableDesc {
	result := []TableDesc{}

	for i, config := range m.schemaCfg.Configs {
		if config.From.Time.Time().After(mtime.Now()) {
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
	for _, desc := range descriptions {
		level.Info(util.Logger).Log("msg", "creating table", "table", desc.Name)
		err := m.client.CreateTable(ctx, desc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *TableManager) deleteTables(ctx context.Context, descriptions []TableDesc) error {
	for _, desc := range descriptions {
		level.Info(util.Logger).Log("msg", "table has exceeded the retention period", "table", desc.Name)
		if !m.cfg.RetentionDeletesEnabled {
			continue
		}

		level.Info(util.Logger).Log("msg", "deleting table", "table", desc.Name)
		err := m.client.DeleteTable(ctx, desc.Name)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *TableManager) updateTables(ctx context.Context, descriptions []TableDesc) error {
	for _, expected := range descriptions {
		level.Debug(util.Logger).Log("msg", "checking provisioned throughput on table", "table", expected.Name)
		current, isActive, err := m.client.DescribeTable(ctx, expected.Name)
		if err != nil {
			return err
		}

		tableCapacity.WithLabelValues(readLabel, expected.Name).Set(float64(current.ProvisionedRead))
		tableCapacity.WithLabelValues(writeLabel, expected.Name).Set(float64(current.ProvisionedWrite))

		if m.cfg.ThroughputUpdatesDisabled {
			continue
		}

		if !isActive {
			level.Info(util.Logger).Log("msg", "skipping update on table, not yet ACTIVE", "table", expected.Name)
			continue
		}

		if expected.Equals(current) {
			level.Info(util.Logger).Log("msg", "provisioned throughput on table, skipping", "table", current.Name, "read", current.ProvisionedRead, "write", current.ProvisionedWrite)
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
