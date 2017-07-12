package chunk

import (
	"flag"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/prometheus/common/model"

	"github.com/weaveworks/common/mtime"
	"github.com/weaveworks/cortex/pkg/util"
)

const (
	secondsInHour      = int64(time.Hour / time.Second)
	secondsInDay       = int64(24 * time.Hour / time.Second)
	millisecondsInHour = int64(time.Hour / time.Millisecond)
	millisecondsInDay  = int64(24 * time.Hour / time.Millisecond)
)

// SchemaConfig contains the config for our chunk index schemas
type SchemaConfig struct {
	// After midnight on this day, we start bucketing indexes by day instead of by
	// hour.  Only the day matters, not the time within the day.
	DailyBucketsFrom util.DayValue
	Base64ValuesFrom util.DayValue
	V4SchemaFrom     util.DayValue
	V5SchemaFrom     util.DayValue
	V6SchemaFrom     util.DayValue
	V7SchemaFrom     util.DayValue
	V8SchemaFrom     util.DayValue

	// Period with which the table manager will poll for tables.
	DynamoDBPollInterval time.Duration

	// duration a table will be created before it is needed.
	CreationGracePeriod time.Duration
	MaxChunkAge         time.Duration

	// Config for the index & chunk tables.
	OriginalTableName string
	UsePeriodicTables bool
	IndexTables       periodicTableConfig
	ChunkTables       periodicTableConfig

	// Deprecated configuration for setting tags on all tables.
	Tags Tags
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *SchemaConfig) RegisterFlags(f *flag.FlagSet) {
	f.Var(&cfg.DailyBucketsFrom, "dynamodb.daily-buckets-from", "The date (in the format YYYY-MM-DD) of the first day for which DynamoDB index buckets should be day-sized vs. hour-sized.")
	f.Var(&cfg.Base64ValuesFrom, "dynamodb.base64-buckets-from", "The date (in the format YYYY-MM-DD) after which we will stop querying to non-base64 encoded values.")
	f.Var(&cfg.V4SchemaFrom, "dynamodb.v4-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v4 schema.")
	f.Var(&cfg.V5SchemaFrom, "dynamodb.v5-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v5 schema.")
	f.Var(&cfg.V6SchemaFrom, "dynamodb.v6-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v6 schema.")
	f.Var(&cfg.V7SchemaFrom, "dynamodb.v7-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v7 schema.")
	f.Var(&cfg.V8SchemaFrom, "dynamodb.v8-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v8 schema.")

	f.DurationVar(&cfg.DynamoDBPollInterval, "dynamodb.poll-interval", 2*time.Minute, "How frequently to poll DynamoDB to learn our capacity.")
	f.DurationVar(&cfg.CreationGracePeriod, "dynamodb.periodic-table.grace-period", 10*time.Minute, "DynamoDB periodic tables grace period (duration which table will be created/deleted before/after it's needed).")
	f.DurationVar(&cfg.MaxChunkAge, "ingester.max-chunk-age", 12*time.Hour, "Maximum chunk age time before flushing.")

	f.StringVar(&cfg.OriginalTableName, "dynamodb.original-table-name", "cortex", "The name of the DynamoDB table used before versioned schemas were introduced.")
	f.BoolVar(&cfg.UsePeriodicTables, "dynamodb.use-periodic-tables", false, "Should we use periodic tables.")

	f.Var(&cfg.Tags, "dynamodb.table.tag", "Deprecated. Set tags on tables individually.")

	cfg.IndexTables.RegisterFlags("dynamodb.periodic-table", "cortex_", f)
	cfg.IndexTables.globalTags = &cfg.Tags
	cfg.ChunkTables.RegisterFlags("dynamodb.chunk-table", "cortex_chunks_", f)
	cfg.ChunkTables.globalTags = &cfg.Tags
}

func (cfg *SchemaConfig) tableForBucket(bucketStart int64) string {
	if !cfg.UsePeriodicTables || bucketStart < (cfg.IndexTables.From.Unix()) {
		return cfg.OriginalTableName
	}
	// TODO remove reference to time package here
	return cfg.IndexTables.Prefix + strconv.Itoa(int(bucketStart/int64(cfg.IndexTables.Period/time.Second)))
}

// Bucket describes a range of time with a tableName and hashKey
type Bucket struct {
	from      uint32
	through   uint32
	tableName string
	hashKey   string
}

func (cfg SchemaConfig) hourlyBuckets(from, through model.Time, userID string) []Bucket {
	var (
		fromHour    = from.Unix() / secondsInHour
		throughHour = through.Unix() / secondsInHour
		result      = []Bucket{}
	)

	// If through ends on the hour, don't include the upcoming hour
	if through.Unix()%secondsInHour == 0 {
		throughHour--
	}

	for i := fromHour; i <= throughHour; i++ {
		relativeFrom := util.Max64(0, int64(from)-(i*millisecondsInHour))
		relativeThrough := util.Min64(millisecondsInHour, int64(through)-(i*millisecondsInHour))
		result = append(result, Bucket{
			from:      uint32(relativeFrom),
			through:   uint32(relativeThrough),
			tableName: cfg.tableForBucket(i * secondsInHour),
			hashKey:   fmt.Sprintf("%s:%d", userID, i),
		})
	}
	return result
}

func (cfg SchemaConfig) dailyBuckets(from, through model.Time, userID string) []Bucket {
	var (
		fromDay    = from.Unix() / secondsInDay
		throughDay = through.Unix() / secondsInDay
		result     = []Bucket{}
	)

	// If through ends on 00:00 of the day, don't include the upcoming day
	if through.Unix()%secondsInDay == 0 {
		throughDay--
	}

	for i := fromDay; i <= throughDay; i++ {
		// The idea here is that the hash key contains the bucket start time (rounded to
		// the nearest day).  The range key can contain the offset from that, to the
		// (start/end) of the chunk. For chunks that span multiple buckets, these
		// offsets will be capped to the bucket boundaries, i.e. start will be
		// positive in the first bucket, then zero in the next etc.
		//
		// The reason for doing all this is to reduce the size of the time stamps we
		// include in the range keys - we use a uint32 - as we then have to base 32
		// encode it.

		relativeFrom := util.Max64(0, int64(from)-(i*millisecondsInDay))
		relativeThrough := util.Min64(millisecondsInDay, int64(through)-(i*millisecondsInDay))
		result = append(result, Bucket{
			from:      uint32(relativeFrom),
			through:   uint32(relativeThrough),
			tableName: cfg.tableForBucket(i * secondsInDay),
			hashKey:   fmt.Sprintf("%s:d%d", userID, i),
		})
	}
	return result
}

type periodicTableConfig struct {
	From                       util.DayValue
	Prefix                     string
	Period                     time.Duration
	ProvisionedWriteThroughput int64
	ProvisionedReadThroughput  int64
	InactiveWriteThroughput    int64
	InactiveReadThroughput     int64
	Tags                       Tags
	// Temporarily in place to support tags set on all tables, as means of
	// smoothing transition to per-table tags.
	globalTags *Tags
}

func (cfg *periodicTableConfig) RegisterFlags(argPrefix, tablePrefix string, f *flag.FlagSet) {
	f.Var(&cfg.From, argPrefix+".from", "Date after which to write chunks to DynamoDB.")
	f.StringVar(&cfg.Prefix, argPrefix+".prefix", tablePrefix, "DynamoDB table prefix for period chunk tables.")
	f.DurationVar(&cfg.Period, argPrefix+".period", 7*24*time.Hour, "DynamoDB chunk tables period.")
	f.Int64Var(&cfg.ProvisionedWriteThroughput, argPrefix+".write-throughput", 3000, "DynamoDB chunk tables write throughput.")
	f.Int64Var(&cfg.ProvisionedReadThroughput, argPrefix+".read-throughput", 300, "DynamoDB chunk tables read throughput.")
	f.Int64Var(&cfg.InactiveWriteThroughput, argPrefix+".inactive-write-throughput", 1, "DynamoDB chunk tables write throughput for inactive tables.")
	f.Int64Var(&cfg.InactiveReadThroughput, argPrefix+".inactive-read-throughput", 300, "DynamoDB chunk tables read throughput for inactive tables.")
	f.Var(&cfg.Tags, argPrefix+".tag", "Tag (of the form key=value) to be added to all tables under management.")

	f.Var(&cfg.From, argPrefix+".start", fmt.Sprintf("Deprecated: use '%s.from'.", argPrefix))
}

func (cfg *periodicTableConfig) periodicTables(beginGrace, endGrace time.Duration) []TableDesc {
	var (
		periodSecs     = int64(cfg.Period / time.Second)
		beginGraceSecs = int64(beginGrace / time.Second)
		endGraceSecs   = int64(endGrace / time.Second)
		firstTable     = cfg.From.Unix() / periodSecs
		lastTable      = (mtime.Now().Unix() + beginGraceSecs) / periodSecs
		now            = mtime.Now().Unix()
		result         = []TableDesc{}
	)
	for i := firstTable; i <= lastTable; i++ {
		tags := Tags(map[string]string{})
		for k, v := range cfg.Tags {
			tags[k] = v
		}
		if cfg.globalTags != nil {
			for k, v := range *cfg.globalTags {
				tags[k] = v
			}
		}
		table := TableDesc{
			// Name construction needs to be consistent with chunk_store.bigBuckets
			Name:             cfg.Prefix + strconv.Itoa(int(i)),
			ProvisionedRead:  cfg.InactiveReadThroughput,
			ProvisionedWrite: cfg.InactiveWriteThroughput,
			Tags:             cfg.GetTags(),
		}

		// if now is within table [start - grace, end + grace), then we need some write throughput
		if (i*periodSecs)-beginGraceSecs <= now && now < (i*periodSecs)+periodSecs+endGraceSecs {
			table.ProvisionedRead = cfg.ProvisionedReadThroughput
			table.ProvisionedWrite = cfg.ProvisionedWriteThroughput
		}
		result = append(result, table)
	}
	return result
}

// GetTags returns tags for the table. Exists to provide backwards
// compatibility for the command-line.
func (cfg *periodicTableConfig) GetTags() Tags {
	tags := Tags(map[string]string{})
	for k, v := range cfg.Tags {
		tags[k] = v
	}
	if cfg.globalTags != nil {
		for k, v := range *cfg.globalTags {
			tags[k] = v
		}
	}
	return tags
}

// compositeSchema is a Schema which delegates to various schemas depending
// on when they were activated.
type compositeSchema struct {
	schemas []compositeSchemaEntry
}

type compositeSchemaEntry struct {
	start model.Time
	Schema
}

type byStart []compositeSchemaEntry

func (a byStart) Len() int           { return len(a) }
func (a byStart) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byStart) Less(i, j int) bool { return a[i].start < a[j].start }

func newCompositeSchema(cfg SchemaConfig) (Schema, error) {
	schemas := []compositeSchemaEntry{
		{0, v1Schema(cfg)},
	}

	if cfg.DailyBucketsFrom.IsSet() {
		schemas = append(schemas, compositeSchemaEntry{cfg.DailyBucketsFrom.Time, v2Schema(cfg)})
	}

	if cfg.Base64ValuesFrom.IsSet() {
		schemas = append(schemas, compositeSchemaEntry{cfg.Base64ValuesFrom.Time, v3Schema(cfg)})
	}

	if cfg.V4SchemaFrom.IsSet() {
		schemas = append(schemas, compositeSchemaEntry{cfg.V4SchemaFrom.Time, v4Schema(cfg)})
	}

	if cfg.V5SchemaFrom.IsSet() {
		schemas = append(schemas, compositeSchemaEntry{cfg.V5SchemaFrom.Time, v5Schema(cfg)})
	}

	if cfg.V6SchemaFrom.IsSet() {
		schemas = append(schemas, compositeSchemaEntry{cfg.V6SchemaFrom.Time, v6Schema(cfg)})
	}

	if cfg.V7SchemaFrom.IsSet() {
		schemas = append(schemas, compositeSchemaEntry{cfg.V7SchemaFrom.Time, v7Schema(cfg)})
	}

	if cfg.V8SchemaFrom.IsSet() {
		schemas = append(schemas, compositeSchemaEntry{cfg.V8SchemaFrom.Time, v8Schema(cfg)})
	}

	if !sort.IsSorted(byStart(schemas)) {
		return nil, fmt.Errorf("schemas not in time-sorted order")
	}

	return compositeSchema{schemas}, nil
}

func (c compositeSchema) forSchemasIndexQuery(from, through model.Time, callback func(from, through model.Time, schema Schema) ([]IndexQuery, error)) ([]IndexQuery, error) {
	if len(c.schemas) == 0 {
		return nil, nil
	}

	// first, find the schema with the highest start _before or at_ from
	i := sort.Search(len(c.schemas), func(i int) bool {
		return c.schemas[i].start > from
	})
	if i > 0 {
		i--
	} else {
		// This could happen if we get passed a sample from before 1970.
		i = 0
		from = c.schemas[0].start
	}

	// next, find the schema with the lowest start _after_ through
	j := sort.Search(len(c.schemas), func(j int) bool {
		return c.schemas[j].start > through
	})

	min := func(a, b model.Time) model.Time {
		if a < b {
			return a
		}
		return b
	}

	start := from
	result := []IndexQuery{}
	for ; i < j; i++ {
		nextSchemaStarts := model.Latest
		if i+1 < len(c.schemas) {
			nextSchemaStarts = c.schemas[i+1].start
		}

		// If the next schema starts at the same time as this one,
		// skip this one.
		if nextSchemaStarts == c.schemas[i].start {
			continue
		}

		end := min(through, nextSchemaStarts-1)
		entries, err := callback(start, end, c.schemas[i].Schema)
		if err != nil {
			return nil, err
		}

		result = append(result, entries...)
		start = nextSchemaStarts
	}

	return result, nil
}

func (c compositeSchema) forSchemasIndexEntry(from, through model.Time, callback func(from, through model.Time, schema Schema) ([]IndexEntry, error)) ([]IndexEntry, error) {
	if len(c.schemas) == 0 {
		return nil, nil
	}

	// first, find the schema with the highest start _before or at_ from
	i := sort.Search(len(c.schemas), func(i int) bool {
		return c.schemas[i].start > from
	})
	if i > 0 {
		i--
	} else {
		// This could happen if we get passed a sample from before 1970.
		i = 0
		from = c.schemas[0].start
	}

	// next, find the schema with the lowest start _after_ through
	j := sort.Search(len(c.schemas), func(j int) bool {
		return c.schemas[j].start > through
	})

	min := func(a, b model.Time) model.Time {
		if a < b {
			return a
		}
		return b
	}

	start := from
	result := []IndexEntry{}
	for ; i < j; i++ {
		nextSchemaStarts := model.Latest
		if i+1 < len(c.schemas) {
			nextSchemaStarts = c.schemas[i+1].start
		}

		// If the next schema starts at the same time as this one,
		// skip this one.
		if nextSchemaStarts == c.schemas[i].start {
			continue
		}

		end := min(through, nextSchemaStarts-1)
		entries, err := callback(start, end, c.schemas[i].Schema)
		if err != nil {
			return nil, err
		}

		result = append(result, entries...)
		start = nextSchemaStarts
	}

	return result, nil
}

func (c compositeSchema) GetWriteEntries(from, through model.Time, userID string, metricName model.LabelValue, labels model.Metric, chunkID string) ([]IndexEntry, error) {
	return c.forSchemasIndexEntry(from, through, func(from, through model.Time, schema Schema) ([]IndexEntry, error) {
		return schema.GetWriteEntries(from, through, userID, metricName, labels, chunkID)
	})
}

func (c compositeSchema) GetReadQueries(from, through model.Time, userID string) ([]IndexQuery, error) {
	return c.forSchemasIndexQuery(from, through, func(from, through model.Time, schema Schema) ([]IndexQuery, error) {
		return schema.GetReadQueries(from, through, userID)
	})
}

func (c compositeSchema) GetReadQueriesForMetric(from, through model.Time, userID string, metricName model.LabelValue) ([]IndexQuery, error) {
	return c.forSchemasIndexQuery(from, through, func(from, through model.Time, schema Schema) ([]IndexQuery, error) {
		return schema.GetReadQueriesForMetric(from, through, userID, metricName)
	})
}

func (c compositeSchema) GetReadQueriesForMetricLabel(from, through model.Time, userID string, metricName model.LabelValue, labelName model.LabelName) ([]IndexQuery, error) {
	return c.forSchemasIndexQuery(from, through, func(from, through model.Time, schema Schema) ([]IndexQuery, error) {
		return schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, labelName)
	})
}

func (c compositeSchema) GetReadQueriesForMetricLabelValue(from, through model.Time, userID string, metricName model.LabelValue, labelName model.LabelName, labelValue model.LabelValue) ([]IndexQuery, error) {
	return c.forSchemasIndexQuery(from, through, func(from, through model.Time, schema Schema) ([]IndexQuery, error) {
		return schema.GetReadQueriesForMetricLabelValue(from, through, userID, metricName, labelName, labelValue)
	})
}
