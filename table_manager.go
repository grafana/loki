package chunk

import (
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
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

// DynamoTableClient is a client for telling Dynamo what to do with tables.
type DynamoTableClient interface {
	ListTables(ctx context.Context) ([]string, error)
	CreateTable(ctx context.Context, name string, readCapacity, writeCapacity int64) error
	DescribeTable(ctx context.Context, name string) (readCapacity, writeCapacity int64, status string, err error)
	UpdateTable(ctx context.Context, name string, readCapacity, writeCapacity int64) error
}

// DynamoTableClientConfig configures the DynamoDB table client.
type DynamoTableClientConfig struct {
	DynamoClient string
	DynamoDBConfig
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *DynamoTableClientConfig) RegisterFlags(f *flag.FlagSet) {
	flag.StringVar(&cfg.DynamoClient, "table-manager.dynamo-client", "aws", "Which DynamoDB table client to use (aws, inmemory).")
	cfg.DynamoDBConfig.RegisterFlags(f)
}

// NewDynamoTableClient creates a new DynamoTableClient.
func NewDynamoTableClient(cfg DynamoTableClientConfig) (DynamoTableClient, error) {
	switch cfg.DynamoClient {
	case "inmemory":
		return NewMockStorage(), nil
	case "aws":
		path := strings.TrimPrefix(cfg.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			log.Warnf("Ignoring DynamoDB URL path: %v.", path)
		}
		return newDynamoTableClient(cfg.DynamoDBConfig)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: aws, inmemory", cfg.DynamoClient)
	}
}

// TableManagerConfig is the config for a DynamoTableManager
type TableManagerConfig struct {
	DynamoDBPollInterval time.Duration

	PeriodicTableConfig
	OriginalTableName string

	// duration a table will be created before it is needed.
	CreationGracePeriod        time.Duration
	MaxChunkAge                time.Duration
	ProvisionedWriteThroughput int64
	ProvisionedReadThroughput  int64
	InactiveWriteThroughput    int64
	InactiveReadThroughput     int64
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *TableManagerConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.DynamoDBPollInterval, "dynamodb.poll-interval", 2*time.Minute, "How frequently to poll DynamoDB to learn our capacity.")
	f.DurationVar(&cfg.CreationGracePeriod, "dynamodb.periodic-table.grace-period", 10*time.Minute, "DynamoDB periodic tables grace period (duration which table will be created/deleted before/after it's needed).")
	f.DurationVar(&cfg.MaxChunkAge, "ingester.max-chunk-age", 12*time.Hour, "Maximum chunk age time before flushing.")
	f.Int64Var(&cfg.ProvisionedWriteThroughput, "dynamodb.periodic-table.write-throughput", 3000, "DynamoDB periodic tables write throughput")
	f.Int64Var(&cfg.ProvisionedReadThroughput, "dynamodb.periodic-table.read-throughput", 300, "DynamoDB periodic tables read throughput")
	f.Int64Var(&cfg.InactiveWriteThroughput, "dynamodb.periodic-table.inactive-write-throughput", 1, "DynamoDB periodic tables write throughput for inactive tables.")
	f.Int64Var(&cfg.InactiveReadThroughput, "dynamodb.periodic-table.inactive-read-throughput", 300, "DynamoDB periodic tables read throughput for inactive tables")

	cfg.PeriodicTableConfig.RegisterFlags(f)
	// XXX: Should this be in PeriodicTableConfig?
	flag.StringVar(&cfg.OriginalTableName, "dynamodb.original-table-name", "", "The name of the DynamoDB table used before versioned schemas were introduced.")
}

// PeriodicTableConfig for the use of periodic tables (ie, weekly tables).  Can
// control when to start the periodic tables, how long the period should be,
// and the prefix to give the tables.
type PeriodicTableConfig struct {
	UsePeriodicTables    bool
	TablePrefix          string
	TablePeriod          time.Duration
	PeriodicTableStartAt util.DayValue
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *PeriodicTableConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.UsePeriodicTables, "dynamodb.use-periodic-tables", true, "Should we use periodic tables.")
	f.StringVar(&cfg.TablePrefix, "dynamodb.periodic-table.prefix", "cortex_", "DynamoDB table prefix for the periodic tables.")
	f.DurationVar(&cfg.TablePeriod, "dynamodb.periodic-table.period", 7*24*time.Hour, "DynamoDB periodic tables period.")
	f.Var(&cfg.PeriodicTableStartAt, "dynamodb.periodic-table.start", "DynamoDB periodic tables start time.")
}

// DynamoTableManager creates and manages the provisioned throughput on DynamoDB tables
type DynamoTableManager struct {
	dynamoDB DynamoTableClient
	cfg      TableManagerConfig
	done     chan struct{}
	wait     sync.WaitGroup
}

// NewDynamoTableManager makes a new DynamoTableManager
func NewDynamoTableManager(cfg TableManagerConfig, dynamoDBClient DynamoTableClient) (*DynamoTableManager, error) {
	return &DynamoTableManager{
		cfg:      cfg,
		dynamoDB: dynamoDBClient,
		done:     make(chan struct{}),
	}, nil
}

// Start the DynamoTableManager
func (m *DynamoTableManager) Start() {
	m.wait.Add(1)
	go m.loop()
}

// Stop the DynamoTableManager
func (m *DynamoTableManager) Stop() {
	close(m.done)
	m.wait.Wait()
}

func (m *DynamoTableManager) loop() {
	defer m.wait.Done()

	ticker := time.NewTicker(m.cfg.DynamoDBPollInterval)
	defer ticker.Stop()

	if err := instrument.TimeRequestHistogram(context.Background(), "DynamoTableManager.syncTables", syncTableDuration, func(ctx context.Context) error {
		return m.syncTables(ctx)
	}); err != nil {
		log.Errorf("Error syncing tables: %v", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := instrument.TimeRequestHistogram(context.Background(), "DynamoTableManager.syncTables", syncTableDuration, func(ctx context.Context) error {
				return m.syncTables(ctx)
			}); err != nil {
				log.Errorf("Error syncing tables: %v", err)
			}
		case <-m.done:
			return
		}
	}
}

func (m *DynamoTableManager) syncTables(ctx context.Context) error {
	expected := m.calculateExpectedTables()
	log.Infof("Expecting %d tables", len(expected))

	toCreate, toCheckThroughput, err := m.partitionTables(ctx, expected)
	if err != nil {
		return err
	}

	if err := m.createTables(ctx, toCreate); err != nil {
		return err
	}

	return m.updateTables(ctx, toCheckThroughput)
}

type tableDescription struct {
	name             string
	provisionedRead  int64
	provisionedWrite int64
}

type byName []tableDescription

func (a byName) Len() int           { return len(a) }
func (a byName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byName) Less(i, j int) bool { return a[i].name < a[j].name }

func (m *DynamoTableManager) calculateExpectedTables() []tableDescription {
	if !m.cfg.UsePeriodicTables {
		return []tableDescription{
			{
				name:             m.cfg.OriginalTableName,
				provisionedRead:  m.cfg.ProvisionedReadThroughput,
				provisionedWrite: m.cfg.ProvisionedWriteThroughput,
			},
		}
	}

	result := []tableDescription{}

	var (
		tablePeriodSecs = int64(m.cfg.TablePeriod / time.Second)
		gracePeriodSecs = int64(m.cfg.CreationGracePeriod / time.Second)
		maxChunkAgeSecs = int64(m.cfg.MaxChunkAge / time.Second)
		firstTable      = m.cfg.PeriodicTableStartAt.Unix() / tablePeriodSecs
		lastTable       = (mtime.Now().Unix() + gracePeriodSecs) / tablePeriodSecs
		now             = mtime.Now().Unix()
	)

	// Add the legacy table
	{
		legacyTable := tableDescription{
			name:             m.cfg.OriginalTableName,
			provisionedRead:  m.cfg.InactiveReadThroughput,
			provisionedWrite: m.cfg.InactiveWriteThroughput,
		}

		// if we are before the switch to periodic table, we need to give this table write throughput
		if now < (firstTable*tablePeriodSecs)+gracePeriodSecs+maxChunkAgeSecs {
			legacyTable.provisionedRead = m.cfg.ProvisionedReadThroughput
			legacyTable.provisionedWrite = m.cfg.ProvisionedWriteThroughput
		}
		result = append(result, legacyTable)
	}

	for i := firstTable; i <= lastTable; i++ {
		table := tableDescription{
			// Name construction needs to be consistent with chunk_store.bigBuckets
			name:             m.cfg.TablePrefix + strconv.Itoa(int(i)),
			provisionedRead:  m.cfg.InactiveReadThroughput,
			provisionedWrite: m.cfg.InactiveWriteThroughput,
		}

		// if now is within table [start - grace, end + grace), then we need some write throughput
		if (i*tablePeriodSecs)-gracePeriodSecs <= now && now < (i*tablePeriodSecs)+tablePeriodSecs+gracePeriodSecs+maxChunkAgeSecs {
			table.provisionedRead = m.cfg.ProvisionedReadThroughput
			table.provisionedWrite = m.cfg.ProvisionedWriteThroughput
		}
		result = append(result, table)
	}

	sort.Sort(byName(result))
	return result
}

// partitionTables works out tables that need to be created vs tables that need to be updated
func (m *DynamoTableManager) partitionTables(ctx context.Context, descriptions []tableDescription) ([]tableDescription, []tableDescription, error) {
	existingTables, err := m.dynamoDB.ListTables(ctx)
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(existingTables)

	toCreate, toCheckThroughput := []tableDescription{}, []tableDescription{}
	i, j := 0, 0
	for i < len(descriptions) && j < len(existingTables) {
		if descriptions[i].name < existingTables[j] {
			// Table descriptions[i] doesn't exist
			toCreate = append(toCreate, descriptions[i])
			i++
		} else if descriptions[i].name > existingTables[j] {
			// existingTables[j].name isn't in descriptions, can ignore
			j++
		} else {
			// Table exists, need to check it has correct throughput
			toCheckThroughput = append(toCheckThroughput, descriptions[i])
			i++
			j++
		}
	}
	for ; i < len(descriptions); i++ {
		toCreate = append(toCreate, descriptions[i])
	}

	return toCreate, toCheckThroughput, nil
}

func (m *DynamoTableManager) createTables(ctx context.Context, descriptions []tableDescription) error {
	for _, desc := range descriptions {
		log.Infof("Creating table %s", desc.name)
		err := m.dynamoDB.CreateTable(ctx, desc.name, desc.provisionedRead, desc.provisionedWrite)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *DynamoTableManager) updateTables(ctx context.Context, descriptions []tableDescription) error {
	for _, desc := range descriptions {
		log.Infof("Checking provisioned throughput on table %s", desc.name)
		readCapacity, writeCapacity, status, err := m.dynamoDB.DescribeTable(ctx, desc.name)
		if err != nil {
			return err
		}

		if status != dynamodb.TableStatusActive {
			log.Infof("Skipping update on  table %s, not yet ACTIVE (%s)", desc.name, status)
			continue
		}

		tableCapacity.WithLabelValues(readLabel, desc.name).Set(float64(readCapacity))
		tableCapacity.WithLabelValues(writeLabel, desc.name).Set(float64(writeCapacity))

		if readCapacity == desc.provisionedRead && writeCapacity == desc.provisionedWrite {
			log.Infof("  Provisioned throughput: read = %d, write = %d, skipping.", readCapacity, writeCapacity)
			continue
		}

		log.Infof("  Updating provisioned throughput on table %s to read = %d, write = %d", desc.name, desc.provisionedRead, desc.provisionedWrite)
		err = m.dynamoDB.UpdateTable(ctx, desc.name, desc.provisionedRead, desc.provisionedWrite)
		if err != nil {
			return err
		}
	}
	return nil
}
