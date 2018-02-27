package chunk

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/mtime"
	"github.com/weaveworks/cortex/pkg/util"
)

const (
	readLabel  = "read"
	writeLabel = "write"
)

var (
	syncTableDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "dynamo_sync_tables_seconds",
		Help:      "Time spent doing SyncTables.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation", "status_code"})
	tableCapacity = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "dynamo_table_capacity_units",
		Help:      "Per-table DynamoDB capacity, measured in DynamoDB capacity units.",
	}, []string{"op", "table"})
)

func init() {
	prometheus.MustRegister(tableCapacity)
	prometheus.MustRegister(syncTableDuration)
}

// Tags is a string-string map that implements flag.Value.
type Tags map[string]string

// String implements flag.Value
func (ts Tags) String() string {
	if ts == nil {
		return ""
	}

	return fmt.Sprintf("%v", map[string]string(ts))
}

// Set implements flag.Value
func (ts *Tags) Set(s string) error {
	if *ts == nil {
		*ts = map[string]string{}
	}

	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("tag must of the format key=value")
	}
	(*ts)[parts[0]] = parts[1]
	return nil
}

// Equals returns true is other matches ts.
func (ts Tags) Equals(other Tags) bool {
	if len(ts) != len(other) {
		return false
	}

	for k, v1 := range ts {
		v2, ok := other[k]
		if !ok || v1 != v2 {
			return false
		}
	}

	return true
}

// AWSTags converts ts into a []*dynamodb.Tag.
func (ts Tags) AWSTags() []*dynamodb.Tag {
	if ts == nil {
		return nil
	}

	var result []*dynamodb.Tag
	for k, v := range ts {
		result = append(result, &dynamodb.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}
	return result
}

// TableManager creates and manages the provisioned throughput on DynamoDB tables
type TableManager struct {
	client      TableClient
	cfg         SchemaConfig
	maxChunkAge time.Duration
	done        chan struct{}
	wait        sync.WaitGroup
}

// NewTableManager makes a new TableManager
func NewTableManager(cfg SchemaConfig, maxChunkAge time.Duration, tableClient TableClient) (*TableManager, error) {
	return &TableManager{
		cfg:         cfg,
		maxChunkAge: maxChunkAge,
		client:      tableClient,
		done:        make(chan struct{}),
	}, nil
}

// Start the TableManager
func (m *TableManager) Start() {
	m.wait.Add(1)
	go m.loop()
}

// Stop the TableManager
func (m *TableManager) Stop() {
	close(m.done)
	m.wait.Wait()
}

func (m *TableManager) loop() {
	defer m.wait.Done()

	ticker := time.NewTicker(m.cfg.DynamoDBPollInterval)
	defer ticker.Stop()

	if err := instrument.TimeRequestHistogram(context.Background(), "TableManager.SyncTables", syncTableDuration, func(ctx context.Context) error {
		return m.SyncTables(ctx)
	}); err != nil {
		level.Error(util.Logger).Log("msg", "error syncing tables", "err", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := instrument.TimeRequestHistogram(context.Background(), "TableManager.SyncTables", syncTableDuration, func(ctx context.Context) error {
				return m.SyncTables(ctx)
			}); err != nil {
				level.Error(util.Logger).Log("msg", "error syncing tables", "err", err)
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
	level.Info(util.Logger).Log("msg", "synching tables", "num_expected_tables", len(expected), "expected_tables", expected)

	toCreate, toCheckThroughput, err := m.partitionTables(ctx, expected)
	if err != nil {
		return err
	}

	if err := m.createTables(ctx, toCreate); err != nil {
		return err
	}

	return m.updateTables(ctx, toCheckThroughput)
}

func (m *TableManager) calculateExpectedTables() []TableDesc {
	result := []TableDesc{}

	// Add the legacy table
	legacyTable := TableDesc{
		Name:             m.cfg.OriginalTableName,
		ProvisionedRead:  m.cfg.IndexTables.InactiveReadThroughput,
		ProvisionedWrite: m.cfg.IndexTables.InactiveWriteThroughput,
		Tags:             m.cfg.IndexTables.GetTags(),
	}

	if m.cfg.UsePeriodicTables {
		// if we are before the switch to periodic table, we need to give this table write throughput

		var (
			tablePeriodSecs = int64(m.cfg.IndexTables.Period / time.Second)
			gracePeriodSecs = int64(m.cfg.CreationGracePeriod / time.Second)
			maxChunkAgeSecs = int64(m.maxChunkAge / time.Second)
			firstTable      = m.cfg.IndexTables.From.Unix() / tablePeriodSecs
			now             = mtime.Now().Unix()
		)

		if now < (firstTable*tablePeriodSecs)+gracePeriodSecs+maxChunkAgeSecs {
			legacyTable.ProvisionedRead = m.cfg.IndexTables.ProvisionedReadThroughput
			legacyTable.ProvisionedWrite = m.cfg.IndexTables.ProvisionedWriteThroughput

			if m.cfg.IndexTables.WriteScale.Enabled {
				legacyTable.WriteScale = m.cfg.IndexTables.WriteScale
			}
		}
	}
	result = append(result, legacyTable)

	if m.cfg.UsePeriodicTables {
		result = append(result, m.cfg.IndexTables.periodicTables(
			m.cfg.CreationGracePeriod, m.maxChunkAge,
		)...)
	}

	if m.cfg.ChunkTables.From.IsSet() {
		result = append(result, m.cfg.ChunkTables.periodicTables(
			m.cfg.CreationGracePeriod, m.maxChunkAge,
		)...)
	}

	sort.Sort(byName(result))
	return result
}

// partitionTables works out tables that need to be created vs tables that need to be updated
func (m *TableManager) partitionTables(ctx context.Context, descriptions []TableDesc) ([]TableDesc, []TableDesc, error) {
	existingTables, err := m.client.ListTables(ctx)
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(existingTables)

	toCreate, toCheck := []TableDesc{}, []TableDesc{}
	i, j := 0, 0
	for i < len(descriptions) && j < len(existingTables) {
		if descriptions[i].Name < existingTables[j] {
			// Table descriptions[i] doesn't exist
			toCreate = append(toCreate, descriptions[i])
			i++
		} else if descriptions[i].Name > existingTables[j] {
			// existingTables[j].name isn't in descriptions, can ignore
			j++
		} else {
			// Table exists, need to check it has correct throughput
			toCheck = append(toCheck, descriptions[i])
			i++
			j++
		}
	}
	for ; i < len(descriptions); i++ {
		toCreate = append(toCreate, descriptions[i])
	}

	return toCreate, toCheck, nil
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

func (m *TableManager) updateTables(ctx context.Context, descriptions []TableDesc) error {
	for _, expected := range descriptions {
		level.Info(util.Logger).Log("msg", "checking provisioned throughput on table", "table", expected.Name)
		current, status, err := m.client.DescribeTable(ctx, expected.Name)
		if err != nil {
			return err
		}

		if status != dynamodb.TableStatusActive {
			level.Info(util.Logger).Log("msg", "skipping update on table, not yet ACTIVE", "table", expected.Name, "status", status)
			continue
		}

		tableCapacity.WithLabelValues(readLabel, expected.Name).Set(float64(current.ProvisionedRead))
		tableCapacity.WithLabelValues(writeLabel, expected.Name).Set(float64(current.ProvisionedWrite))

		if expected.Equals(current) {
			level.Info(util.Logger).Log("msg", "provisioned throughput on table, skipping", "table", current.Name, "read", current.ProvisionedRead, "write", current.ProvisionedWrite)
			continue
		}

		level.Info(util.Logger).Log("msg", "updating provisioned throughput on table", "table", expected.Name, "old_read", current.ProvisionedRead, "old_write", current.ProvisionedWrite, "new_read", expected.ProvisionedRead, "old_read", expected.ProvisionedWrite)
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
			return fmt.Errorf("Expected '%v', found '%v' for table '%s'", expect, desc, desc.Name)
		}
	}

	return nil
}
