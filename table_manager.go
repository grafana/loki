package chunk

import (
	"flag"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
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

// TableManagerConfig is the config for a TableManager
type TableManagerConfig struct {
	DynamoDBPollInterval time.Duration

	PeriodicTableConfig

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

func (m *TableManager) calculateExpectedTables() []tableDescription {
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
func (m *TableManager) partitionTables(ctx context.Context, descriptions []tableDescription) ([]tableDescription, []tableDescription, error) {
	existingTables, err := m.client.ListTables(ctx)
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

func (m *TableManager) createTables(ctx context.Context, descriptions []tableDescription) error {
	for _, desc := range descriptions {
		log.Infof("Creating table %s", desc.name)
		err := m.client.CreateTable(ctx, desc.name, desc.provisionedRead, desc.provisionedWrite)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *TableManager) updateTables(ctx context.Context, descriptions []tableDescription) error {
	for _, desc := range descriptions {
		log.Infof("Checking provisioned throughput on table %s", desc.name)
		readCapacity, writeCapacity, status, err := m.client.DescribeTable(ctx, desc.name)
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
		err = m.client.UpdateTable(ctx, desc.name, desc.provisionedRead, desc.provisionedWrite)
		if err != nil {
			return err
		}
	}
	return nil
}

// TableClient is a client for telling Dynamo what to do with tables.
type TableClient interface {
	ListTables(ctx context.Context) ([]string, error)
	CreateTable(ctx context.Context, name string, readCapacity, writeCapacity int64) error
	DescribeTable(ctx context.Context, name string) (readCapacity, writeCapacity int64, status string, err error)
	UpdateTable(ctx context.Context, name string, readCapacity, writeCapacity int64) error
}

type dynamoTableClient struct {
	DynamoDB dynamodbiface.DynamoDBAPI
}

// NewDynamoDBTableClient makes a new DynamoTableClient.
func NewDynamoDBTableClient(cfg DynamoDBConfig) (TableClient, error) {
	dynamoDB, err := dynamoClientFromURL(cfg.DynamoDB.URL)
	if err != nil {
		return nil, err
	}
	return dynamoTableClient{
		DynamoDB: dynamoDB,
	}, nil
}

func (d dynamoTableClient) ListTables(ctx context.Context) ([]string, error) {
	table := []string{}
	err := instrument.TimeRequestHistogram(ctx, "DynamoDB.ListTablesPages", dynamoRequestDuration, func(_ context.Context) error {
		return d.DynamoDB.ListTablesPages(&dynamodb.ListTablesInput{}, func(resp *dynamodb.ListTablesOutput, _ bool) bool {
			for _, s := range resp.TableNames {
				table = append(table, *s)
			}
			return true
		})
	})
	return table, err
}

func (d dynamoTableClient) CreateTable(ctx context.Context, name string, readCapacity, writeCapacity int64) error {
	return instrument.TimeRequestHistogram(ctx, "DynamoDB.CreateTable", dynamoRequestDuration, func(_ context.Context) error {
		input := &dynamodb.CreateTableInput{
			TableName: aws.String(name),
			AttributeDefinitions: []*dynamodb.AttributeDefinition{
				{
					AttributeName: aws.String(hashKey),
					AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
				},
				{
					AttributeName: aws.String(rangeKey),
					AttributeType: aws.String(dynamodb.ScalarAttributeTypeB),
				},
			},
			KeySchema: []*dynamodb.KeySchemaElement{
				{
					AttributeName: aws.String(hashKey),
					KeyType:       aws.String(dynamodb.KeyTypeHash),
				},
				{
					AttributeName: aws.String(rangeKey),
					KeyType:       aws.String(dynamodb.KeyTypeRange),
				},
			},
			ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
				ReadCapacityUnits:  aws.Int64(readCapacity),
				WriteCapacityUnits: aws.Int64(writeCapacity),
			},
		}
		_, err := d.DynamoDB.CreateTable(input)
		return err
	})
}

func (d dynamoTableClient) DescribeTable(ctx context.Context, name string) (readCapacity, writeCapacity int64, status string, err error) {
	var out *dynamodb.DescribeTableOutput
	instrument.TimeRequestHistogram(ctx, "DynamoDB.DescribeTable", dynamoRequestDuration, func(_ context.Context) error {
		out, err = d.DynamoDB.DescribeTable(&dynamodb.DescribeTableInput{
			TableName: aws.String(name),
		})
		readCapacity = *out.Table.ProvisionedThroughput.ReadCapacityUnits
		writeCapacity = *out.Table.ProvisionedThroughput.WriteCapacityUnits
		status = *out.Table.TableStatus
		return err
	})
	return
}

func (d dynamoTableClient) UpdateTable(ctx context.Context, name string, readCapacity, writeCapacity int64) error {
	return instrument.TimeRequestHistogram(ctx, "DynamoDB.UpdateTable", dynamoRequestDuration, func(_ context.Context) error {
		_, err := d.DynamoDB.UpdateTable(&dynamodb.UpdateTableInput{
			TableName: aws.String(name),
			ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
				ReadCapacityUnits:  aws.Int64(readCapacity),
				WriteCapacityUnits: aws.Int64(writeCapacity),
			},
		})
		return err
	})
}
