package chunk

import (
	"flag"
	"fmt"
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
	DailyBucketsFrom      util.DayValue
	Base64ValuesFrom      util.DayValue
	V4SchemaFrom          util.DayValue
	V5SchemaFrom          util.DayValue
	V6SchemaFrom          util.DayValue
	V7SchemaFrom          util.DayValue
	V8SchemaFrom          util.DayValue
	V9SchemaFrom          util.DayValue
	BigtableColumnKeyFrom util.DayValue

	// Master 'off-switch' for table capacity updates, e.g. when troubleshooting
	ThroughputUpdatesDisabled bool

	// Period with which the table manager will poll for tables.
	DynamoDBPollInterval time.Duration

	// duration a table will be created before it is needed.
	CreationGracePeriod time.Duration

	// Config for the index & chunk tables.
	OriginalTableName string
	UsePeriodicTables bool
	IndexTables       PeriodicTableConfig
	ChunkTables       PeriodicTableConfig

	// Deprecated configuration for setting tags on all tables.
	Tags Tags
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *SchemaConfig) RegisterFlags(f *flag.FlagSet) {
	f.Var(&cfg.DailyBucketsFrom, "dynamodb.daily-buckets-from", "The date (in the format YYYY-MM-DD) of the first day for which DynamoDB index buckets should be day-sized vs. hour-sized.")
	f.Var(&cfg.Base64ValuesFrom, "dynamodb.base64-buckets-from", "The date (in the format YYYY-MM-DD) after which we will stop querying to non-base64 encoded values.")
	f.Var(&cfg.V4SchemaFrom, "dynamodb.v4-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v4 schema.")
	f.Var(&cfg.V5SchemaFrom, "dynamodb.v5-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v5 schema.")
	f.Var(&cfg.V6SchemaFrom, "dynamodb.v6-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v6 schema.")
	f.Var(&cfg.V7SchemaFrom, "dynamodb.v7-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v7 schema (Deprecated).")
	f.Var(&cfg.V8SchemaFrom, "dynamodb.v8-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v8 schema (Deprecated).")
	f.Var(&cfg.V9SchemaFrom, "dynamodb.v9-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v9 schema (Series indexing).")
	f.Var(&cfg.BigtableColumnKeyFrom, "bigtable.column-key-from", "The date (in the format YYYY-MM-DD) after which we use bigtable column keys.")

	f.BoolVar(&cfg.ThroughputUpdatesDisabled, "table-manager.throughput-updates-disabled", false, "If true, disable all changes to DB capacity")
	f.DurationVar(&cfg.DynamoDBPollInterval, "dynamodb.poll-interval", 2*time.Minute, "How frequently to poll DynamoDB to learn our capacity.")
	f.DurationVar(&cfg.CreationGracePeriod, "dynamodb.periodic-table.grace-period", 10*time.Minute, "DynamoDB periodic tables grace period (duration which table will be created/deleted before/after it's needed).")

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

// PeriodicTableConfig is configuration for a set of time-sharded tables.
type PeriodicTableConfig struct {
	From   util.DayValue
	Prefix string
	Period time.Duration
	Tags   Tags

	ProvisionedWriteThroughput int64
	ProvisionedReadThroughput  int64
	InactiveWriteThroughput    int64
	InactiveReadThroughput     int64

	WriteScale              AutoScalingConfig
	InactiveWriteScale      AutoScalingConfig
	InactiveWriteScaleLastN int64

	// Temporarily in place to support tags set on all tables, as means of
	// smoothing transition to per-table tags.
	globalTags *Tags
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *PeriodicTableConfig) RegisterFlags(argPrefix, tablePrefix string, f *flag.FlagSet) {
	f.Var(&cfg.From, argPrefix+".from", "Date after which to write chunks to DynamoDB.")
	f.StringVar(&cfg.Prefix, argPrefix+".prefix", tablePrefix, "DynamoDB table prefix for period tables.")
	f.DurationVar(&cfg.Period, argPrefix+".period", 7*24*time.Hour, "DynamoDB table period.")
	f.Var(&cfg.Tags, argPrefix+".tag", "Tag (of the form key=value) to be added to all tables under management.")

	f.Int64Var(&cfg.ProvisionedWriteThroughput, argPrefix+".write-throughput", 3000, "DynamoDB table default write throughput.")
	f.Int64Var(&cfg.ProvisionedReadThroughput, argPrefix+".read-throughput", 300, "DynamoDB table default read throughput.")
	f.Int64Var(&cfg.InactiveWriteThroughput, argPrefix+".inactive-write-throughput", 1, "DynamoDB table write throughput for inactive tables.")
	f.Int64Var(&cfg.InactiveReadThroughput, argPrefix+".inactive-read-throughput", 300, "DynamoDB table read throughput for inactive tables.")
	f.Var(&cfg.From, argPrefix+".start", fmt.Sprintf("Deprecated: use '%s.from'.", argPrefix))

	cfg.WriteScale.RegisterFlags(argPrefix+".write-throughput.scale", f)
	cfg.InactiveWriteScale.RegisterFlags(argPrefix+".inactive-write-throughput.scale", f)
	f.Int64Var(&cfg.InactiveWriteScaleLastN, argPrefix+".inactive-write-throughput.scale-last-n", 4, "Number of last inactive tables to enable write autoscale.")
}

// AutoScalingConfig for DynamoDB tables.
type AutoScalingConfig struct {
	Enabled     bool
	RoleARN     string
	MinCapacity int64
	MaxCapacity int64
	OutCooldown int64
	InCooldown  int64
	TargetValue float64
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *AutoScalingConfig) RegisterFlags(argPrefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, argPrefix+".enabled", false, "Should we enable autoscale for the table.")
	f.StringVar(&cfg.RoleARN, argPrefix+".role-arn", "", "AWS AutoScaling role ARN")
	f.Int64Var(&cfg.MinCapacity, argPrefix+".min-capacity", 3000, "DynamoDB minimum provision capacity.")
	f.Int64Var(&cfg.MaxCapacity, argPrefix+".max-capacity", 6000, "DynamoDB maximum provision capacity.")
	f.Int64Var(&cfg.OutCooldown, argPrefix+".out-cooldown", 1800, "DynamoDB minimum seconds between each autoscale up.")
	f.Int64Var(&cfg.InCooldown, argPrefix+".in-cooldown", 1800, "DynamoDB minimum seconds between each autoscale down.")
	f.Float64Var(&cfg.TargetValue, argPrefix+".target-value", 80, "DynamoDB target ratio of consumed capacity to provisioned capacity.")
}

func (cfg *PeriodicTableConfig) periodicTables(beginGrace, endGrace time.Duration) []TableDesc {
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

			if cfg.WriteScale.Enabled {
				table.WriteScale = cfg.WriteScale
			}
		} else if cfg.InactiveWriteScale.Enabled && i >= (lastTable-cfg.InactiveWriteScaleLastN) {
			// Autoscale last N tables
			table.WriteScale = cfg.InactiveWriteScale
		}

		result = append(result, table)
	}
	return result
}

// GetTags returns tags for the table. Exists to provide backwards
// compatibility for the command-line.
func (cfg *PeriodicTableConfig) GetTags() Tags {
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

// TableFor calculates the table shard for a given point in time.
func (cfg *PeriodicTableConfig) TableFor(t model.Time) string {
	var (
		periodSecs = int64(cfg.Period / time.Second)
		table      = t.Unix() / periodSecs
	)
	return cfg.Prefix + strconv.Itoa(int(table))
}
