package chunk

import (
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/model"
	"golang.org/x/net/context"

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
		Help:      "Time spent doing syncTables.",
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
}

// TableManagerConfig is the config for a TableManager
type TableManagerConfig struct {
	DynamoDBPollInterval time.Duration

	PeriodicTableConfig
	PeriodicChunkTableConfig

	// duration a table will be created before it is needed.
	CreationGracePeriod        time.Duration
	MaxChunkAge                time.Duration
	ProvisionedWriteThroughput int64
	ProvisionedReadThroughput  int64
	InactiveWriteThroughput    int64
	InactiveReadThroughput     int64

	ChunkTableProvisionedWriteThroughput int64
	ChunkTableProvisionedReadThroughput  int64
	ChunkTableInactiveWriteThroughput    int64
	ChunkTableInactiveReadThroughput     int64

	TableTags Tags
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

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *TableManagerConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.DynamoDBPollInterval, "dynamodb.poll-interval", 2*time.Minute, "How frequently to poll DynamoDB to learn our capacity.")
	f.DurationVar(&cfg.CreationGracePeriod, "dynamodb.periodic-table.grace-period", 10*time.Minute, "DynamoDB periodic tables grace period (duration which table will be created/deleted before/after it's needed).")
	f.DurationVar(&cfg.MaxChunkAge, "ingester.max-chunk-age", 12*time.Hour, "Maximum chunk age time before flushing.")
	f.Int64Var(&cfg.ProvisionedWriteThroughput, "dynamodb.periodic-table.write-throughput", 3000, "DynamoDB periodic tables write throughput.")
	f.Int64Var(&cfg.ProvisionedReadThroughput, "dynamodb.periodic-table.read-throughput", 300, "DynamoDB periodic tables read throughput.")
	f.Int64Var(&cfg.InactiveWriteThroughput, "dynamodb.periodic-table.inactive-write-throughput", 1, "DynamoDB periodic tables write throughput for inactive tables.")
	f.Int64Var(&cfg.InactiveReadThroughput, "dynamodb.periodic-table.inactive-read-throughput", 300, "DynamoDB periodic tables read throughput for inactive tables.")
	f.Int64Var(&cfg.ChunkTableProvisionedWriteThroughput, "dynamodb.chunk-table.write-throughput", 3000, "DynamoDB chunk tables write throughput.")
	f.Int64Var(&cfg.ChunkTableProvisionedReadThroughput, "dynamodb.chunk-table.read-throughput", 300, "DynamoDB chunk tables read throughput.")
	f.Int64Var(&cfg.ChunkTableInactiveWriteThroughput, "dynamodb.chunk-table.inactive-write-throughput", 1, "DynamoDB chunk tables write throughput for inactive tables.")
	f.Int64Var(&cfg.ChunkTableInactiveReadThroughput, "dynamodb.chunk-table.inactive-read-throughput", 300, "DynamoDB chunk tables read throughput for inactive tables.")
	f.Var(&cfg.TableTags, "dynamodb.table.tag", "Tag (of the form key=value) to be added to all tables under management.")

	cfg.PeriodicTableConfig.RegisterFlags(f)
	cfg.PeriodicChunkTableConfig.RegisterFlags(f)
}

// PeriodicTableConfig for the use of periodic tables (ie, weekly tables).  Can
// control when to start the periodic tables, how long the period should be,
// and the prefix to give the tables.
type PeriodicTableConfig struct {
	OriginalTableName    string
	UsePeriodicTables    bool
	TablePrefix          string
	TablePeriod          time.Duration
	PeriodicTableStartAt util.DayValue
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *PeriodicTableConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.OriginalTableName, "dynamodb.original-table-name", "", "The name of the DynamoDB table used before versioned schemas were introduced.")
	f.BoolVar(&cfg.UsePeriodicTables, "dynamodb.use-periodic-tables", true, "Should we use periodic tables.")
	f.StringVar(&cfg.TablePrefix, "dynamodb.periodic-table.prefix", "cortex_", "DynamoDB table prefix for the periodic tables.")
	f.DurationVar(&cfg.TablePeriod, "dynamodb.periodic-table.period", 7*24*time.Hour, "DynamoDB periodic tables period.")
	f.Var(&cfg.PeriodicTableStartAt, "dynamodb.periodic-table.start", "DynamoDB periodic tables start time.")
}

// PeriodicChunkTableConfig contains the various parameters for the chunk table.
type PeriodicChunkTableConfig struct {
	ChunkTableFrom   util.DayValue
	ChunkTablePrefix string
	ChunkTablePeriod time.Duration
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *PeriodicChunkTableConfig) RegisterFlags(f *flag.FlagSet) {
	f.Var(&cfg.ChunkTableFrom, "dynamodb.chunk-table.from", "Date after which to write chunks to DynamoDB.")
	f.StringVar(&cfg.ChunkTablePrefix, "dynamodb.chunk-table.prefix", "cortex_chunks_", "DynamoDB table prefix for period chunk tables.")
	f.DurationVar(&cfg.ChunkTablePeriod, "dynamodb.chunk-table.period", 7*24*time.Hour, "DynamoDB chunk tables period.")
}

// TableManager creates and manages the provisioned throughput on DynamoDB tables
type TableManager struct {
	client TableClient
	cfg    TableManagerConfig
	done   chan struct{}
	wait   sync.WaitGroup
}

// NewTableManager makes a new TableManager
func NewTableManager(cfg TableManagerConfig, tableClient TableClient) (*TableManager, error) {
	return &TableManager{
		cfg:    cfg,
		client: tableClient,
		done:   make(chan struct{}),
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

	if err := instrument.TimeRequestHistogram(context.Background(), "TableManager.syncTables", syncTableDuration, func(ctx context.Context) error {
		return m.syncTables(ctx)
	}); err != nil {
		log.Errorf("Error syncing tables: %v", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := instrument.TimeRequestHistogram(context.Background(), "TableManager.syncTables", syncTableDuration, func(ctx context.Context) error {
				return m.syncTables(ctx)
			}); err != nil {
				log.Errorf("Error syncing tables: %v", err)
			}
		case <-m.done:
			return
		}
	}
}

func (m *TableManager) syncTables(ctx context.Context) error {
	expected := m.calculateExpectedTables()
	log.Infof("Expecting %d tables: %+v", len(expected), expected)

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
		ProvisionedRead:  m.cfg.InactiveReadThroughput,
		ProvisionedWrite: m.cfg.InactiveWriteThroughput,
		Tags:             m.cfg.TableTags,
	}

	if m.cfg.UsePeriodicTables {
		// if we are before the switch to periodic table, we need to give this table write throughput
		var (
			tablePeriodSecs = int64(m.cfg.TablePeriod / time.Second)
			gracePeriodSecs = int64(m.cfg.CreationGracePeriod / time.Second)
			maxChunkAgeSecs = int64(m.cfg.MaxChunkAge / time.Second)
			firstTable      = m.cfg.PeriodicTableStartAt.Unix() / tablePeriodSecs
			now             = mtime.Now().Unix()
		)

		if now < (firstTable*tablePeriodSecs)+gracePeriodSecs+maxChunkAgeSecs {
			legacyTable.ProvisionedRead = m.cfg.ProvisionedReadThroughput
			legacyTable.ProvisionedWrite = m.cfg.ProvisionedWriteThroughput
		}
	}
	result = append(result, legacyTable)

	if m.cfg.UsePeriodicTables {
		result = append(result, m.periodicTables(
			m.cfg.TablePrefix, m.cfg.PeriodicTableStartAt.Time, m.cfg.TablePeriod,
			m.cfg.CreationGracePeriod, m.cfg.MaxChunkAge,
			m.cfg.ProvisionedReadThroughput, m.cfg.ProvisionedWriteThroughput,
			m.cfg.InactiveReadThroughput, m.cfg.InactiveWriteThroughput,
		)...)
	}

	if m.cfg.ChunkTableFrom.IsSet() {
		result = append(result, m.periodicTables(
			m.cfg.ChunkTablePrefix, m.cfg.ChunkTableFrom.Time, m.cfg.ChunkTablePeriod,
			m.cfg.CreationGracePeriod, m.cfg.MaxChunkAge,
			m.cfg.ChunkTableProvisionedReadThroughput, m.cfg.ChunkTableProvisionedWriteThroughput,
			m.cfg.ChunkTableInactiveReadThroughput, m.cfg.ChunkTableInactiveWriteThroughput,
		)...)
	}

	sort.Sort(byName(result))
	return result
}

func (m *TableManager) periodicTables(
	prefix string, start model.Time, period, beginGrace, endGrace time.Duration,
	activeRead, activeWrite, inactiveRead, inactiveWrite int64,
) []TableDesc {
	var (
		periodSecs     = int64(period / time.Second)
		beginGraceSecs = int64(beginGrace / time.Second)
		endGraceSecs   = int64(endGrace / time.Second)
		firstTable     = start.Unix() / periodSecs
		lastTable      = (mtime.Now().Unix() + beginGraceSecs) / periodSecs
		now            = mtime.Now().Unix()
		result         = []TableDesc{}
	)
	for i := firstTable; i <= lastTable; i++ {
		table := TableDesc{
			// Name construction needs to be consistent with chunk_store.bigBuckets
			Name:             prefix + strconv.Itoa(int(i)),
			ProvisionedRead:  inactiveRead,
			ProvisionedWrite: inactiveWrite,
			Tags:             m.cfg.TableTags,
		}

		// if now is within table [start - grace, end + grace), then we need some write throughput
		if (i*periodSecs)-beginGraceSecs <= now && now < (i*periodSecs)+periodSecs+endGraceSecs {
			table.ProvisionedRead = activeRead
			table.ProvisionedWrite = activeWrite
		}
		result = append(result, table)
	}
	log.Infof("periodicTables: %+v", result)
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
		log.Infof("Creating table %s", desc.Name)
		err := m.client.CreateTable(ctx, desc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *TableManager) updateTables(ctx context.Context, descriptions []TableDesc) error {
	for _, desc := range descriptions {
		log.Infof("Checking provisioned throughput on table %s", desc.Name)
		current, status, err := m.client.DescribeTable(ctx, desc.Name)
		if err != nil {
			return err
		}

		if status != dynamodb.TableStatusActive {
			log.Infof("Skipping update on  table %s, not yet ACTIVE (%s)", desc.Name, status)
			continue
		}

		tableCapacity.WithLabelValues(readLabel, desc.Name).Set(float64(current.ProvisionedRead))
		tableCapacity.WithLabelValues(writeLabel, desc.Name).Set(float64(current.ProvisionedWrite))

		if desc.Equals(current) {
			log.Infof("  Provisioned throughput: read = %d, write = %d, skipping.", current.ProvisionedRead, current.ProvisionedWrite)
			continue
		}

		log.Infof("  Updating provisioned throughput on table %s to read = %d, write = %d", desc.Name, desc.ProvisionedRead, desc.ProvisionedWrite)
		err = m.client.UpdateTable(ctx, desc)
		if err != nil {
			return err
		}
	}
	return nil
}
