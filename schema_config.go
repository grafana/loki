package chunk

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/common/model"
	yaml "gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/mtime"
)

const (
	secondsInHour      = int64(time.Hour / time.Second)
	secondsInDay       = int64(24 * time.Hour / time.Second)
	millisecondsInHour = int64(time.Hour / time.Millisecond)
	millisecondsInDay  = int64(24 * time.Hour / time.Millisecond)
)

// PeriodConfig defines the schema and tables to use for a period of time
type PeriodConfig struct {
	From        model.Time          `yaml:"-"`              // used when working with config
	FromStr     string              `yaml:"from,omitempty"` // used when loading from yaml
	Store       string              `yaml:"store"`
	Schema      string              `yaml:"schema"`
	IndexTables PeriodicTableConfig `yaml:"index"`
	ChunkTables PeriodicTableConfig `yaml:"chunks,omitempty"`
}

// SchemaConfig contains the config for our chunk index schemas
type SchemaConfig struct {
	Configs []PeriodConfig `yaml:"configs"`

	fileName string
	legacy   LegacySchemaConfig // if fileName is set then legacy config is ignored
}

// LegacySchemaConfig lets you configure schema via command-line flags
type LegacySchemaConfig struct {
	StorageClient string // aws, gcp, etc.

	// After midnight on this day, we start bucketing indexes by day instead of by
	// hour.  Only the day matters, not the time within the day.
	DailyBucketsFrom      util.DayValue
	Base64ValuesFrom      util.DayValue
	V4SchemaFrom          util.DayValue
	V5SchemaFrom          util.DayValue
	V6SchemaFrom          util.DayValue
	V9SchemaFrom          util.DayValue
	BigtableColumnKeyFrom util.DayValue

	// Config for the index & chunk tables.
	OriginalTableName string
	UsePeriodicTables bool
	IndexTablesFrom   util.DayValue
	IndexTables       PeriodicTableConfig
	ChunkTablesFrom   util.DayValue
	ChunkTables       PeriodicTableConfig
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *SchemaConfig) RegisterFlags(f *flag.FlagSet) {
	flag.StringVar(&cfg.fileName, "config-yaml", "", "Schema config yaml")
	cfg.legacy.RegisterFlags(f)
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *LegacySchemaConfig) RegisterFlags(f *flag.FlagSet) {
	flag.StringVar(&cfg.StorageClient, "chunk.storage-client", "aws", "Which storage client to use (aws, gcp, cassandra, inmemory).")
	f.Var(&cfg.DailyBucketsFrom, "dynamodb.daily-buckets-from", "The date (in the format YYYY-MM-DD) of the first day for which DynamoDB index buckets should be day-sized vs. hour-sized.")
	f.Var(&cfg.Base64ValuesFrom, "dynamodb.base64-buckets-from", "The date (in the format YYYY-MM-DD) after which we will stop querying to non-base64 encoded values.")
	f.Var(&cfg.V4SchemaFrom, "dynamodb.v4-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v4 schema.")
	f.Var(&cfg.V5SchemaFrom, "dynamodb.v5-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v5 schema.")
	f.Var(&cfg.V6SchemaFrom, "dynamodb.v6-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v6 schema.")
	f.Var(&cfg.V9SchemaFrom, "dynamodb.v9-schema-from", "The date (in the format YYYY-MM-DD) after which we enable v9 schema (Series indexing).")
	f.Var(&cfg.BigtableColumnKeyFrom, "bigtable.column-key-from", "The date (in the format YYYY-MM-DD) after which we use bigtable column keys.")

	f.StringVar(&cfg.OriginalTableName, "dynamodb.original-table-name", "cortex", "The name of the DynamoDB table used before versioned schemas were introduced.")
	f.BoolVar(&cfg.UsePeriodicTables, "dynamodb.use-periodic-tables", false, "Should we use periodic tables.")

	f.Var(&cfg.IndexTablesFrom, "dynamodb.periodic-table.from", "Date after which to use periodic tables.")
	cfg.IndexTables.RegisterFlags("dynamodb.periodic-table", "cortex_", f)
	f.Var(&cfg.ChunkTablesFrom, "dynamodb.chunk-table.from", "Date after which to write chunks to DynamoDB.")
	cfg.ChunkTables.RegisterFlags("dynamodb.chunk-table", "cortex_chunks_", f)
}

// translate from command-line parameters into new config data structure
func (cfg *SchemaConfig) translate() error {
	cfg.Configs = []PeriodConfig{}

	add := func(t string, f model.Time) {
		cfg.Configs = append(cfg.Configs, PeriodConfig{
			From:    f,
			FromStr: f.Time().Format("2006-01-02"),
			Schema:  t,
			Store:   cfg.legacy.StorageClient,
			IndexTables: PeriodicTableConfig{
				Prefix: cfg.legacy.OriginalTableName,
			},
		})
	}

	add("v1", 0)

	if cfg.legacy.DailyBucketsFrom.IsSet() {
		add("v2", cfg.legacy.DailyBucketsFrom.Time)
	}
	if cfg.legacy.Base64ValuesFrom.IsSet() {
		add("v3", cfg.legacy.Base64ValuesFrom.Time)
	}
	if cfg.legacy.V4SchemaFrom.IsSet() {
		add("v4", cfg.legacy.V4SchemaFrom.Time)
	}
	if cfg.legacy.V5SchemaFrom.IsSet() {
		add("v5", cfg.legacy.V5SchemaFrom.Time)
	}
	if cfg.legacy.V6SchemaFrom.IsSet() {
		add("v6", cfg.legacy.V6SchemaFrom.Time)
	}
	if cfg.legacy.V9SchemaFrom.IsSet() {
		add("v9", cfg.legacy.V9SchemaFrom.Time)
	}

	cfg.ForEachAfter(cfg.legacy.IndexTablesFrom.Time, func(config *PeriodConfig) {
		config.IndexTables = cfg.legacy.IndexTables.clean()
	})
	if cfg.legacy.ChunkTablesFrom.IsSet() {
		cfg.ForEachAfter(cfg.legacy.ChunkTablesFrom.Time, func(config *PeriodConfig) {
			config.Store = "aws-dynamo"
			config.ChunkTables = cfg.legacy.ChunkTables.clean()
		})
	}
	if cfg.legacy.BigtableColumnKeyFrom.IsSet() {
		cfg.ForEachAfter(cfg.legacy.BigtableColumnKeyFrom.Time, func(config *PeriodConfig) {
			config.Store = "gcp-columnkey"
		})
	}
	return nil
}

func (cfg PeriodicTableConfig) clean() PeriodicTableConfig {
	cfg.WriteScale.clean()
	cfg.InactiveWriteScale.clean()
	return cfg
}

func (cfg *AutoScalingConfig) clean() {
	if !cfg.Enabled {
		// Blank the default values from flag since they aren't used
		cfg.MinCapacity = 0
		cfg.MaxCapacity = 0
		cfg.OutCooldown = 0
		cfg.InCooldown = 0
		cfg.TargetValue = 0
	}
}

// ForEachAfter will call f() on every entry after t, splitting
// entries if necessary so there is an entry starting at t
func (cfg *SchemaConfig) ForEachAfter(t model.Time, f func(config *PeriodConfig)) {
	for i := 0; i < len(cfg.Configs); i++ {
		if t > cfg.Configs[i].From &&
			(i+1 == len(cfg.Configs) || t < cfg.Configs[i+1].From) {
			// Split the i'th entry by duplicating then overwriting the From time
			cfg.Configs = append(cfg.Configs[:i+1], cfg.Configs[i:]...)
			cfg.Configs[i+1].From = t
		}
		if cfg.Configs[i].From >= t {
			f(&cfg.Configs[i])
		}
	}
}

func (cfg PeriodConfig) createSchema() Schema {
	var s schema
	switch cfg.Schema {
	case "v1":
		s = schema{cfg.hourlyBuckets, originalEntries{}}
	case "v2":
		s = schema{cfg.dailyBuckets, originalEntries{}}
	case "v3":
		s = schema{cfg.dailyBuckets, base64Entries{originalEntries{}}}
	case "v4":
		s = schema{cfg.dailyBuckets, labelNameInHashKeyEntries{}}
	case "v5":
		s = schema{cfg.dailyBuckets, v5Entries{}}
	case "v6":
		s = schema{cfg.dailyBuckets, v6Entries{}}
	case "v9":
		s = schema{cfg.dailyBuckets, v9Entries{}}
	}
	return s
}

func (cfg *PeriodConfig) tableForBucket(bucketStart int64) string {
	if cfg.IndexTables.Period == 0 {
		return cfg.IndexTables.Prefix
	}
	// TODO remove reference to time package here
	return cfg.IndexTables.Prefix + strconv.Itoa(int(bucketStart/int64(cfg.IndexTables.Period/time.Second)))
}

// Load the yaml file, or build the config from legacy command-line flags
func (cfg *SchemaConfig) Load() error {
	if len(cfg.Configs) > 0 {
		return nil
	}
	if cfg.fileName == "" {
		return cfg.translate()
	}

	f, err := os.Open(cfg.fileName)
	if err != nil {
		return err
	}

	decoder := yaml.NewDecoder(f)
	decoder.SetStrict(true)
	if err := decoder.Decode(&cfg); err != nil {
		return err
	}
	for i := range cfg.Configs {
		t, err := time.Parse("2006-01-02", cfg.Configs[i].FromStr)
		if err != nil {
			return err
		}
		cfg.Configs[i].From = model.TimeFromUnix(t.Unix())
	}

	return nil
}

// PrintYaml dumps the yaml to stdout, to aid in migration
func (cfg SchemaConfig) PrintYaml() {
	for i := range cfg.Configs {
		cfg.Configs[i].FromStr = cfg.Configs[i].From.Time().Format("2006-01-02")
	}
	encoder := yaml.NewEncoder(os.Stdout)
	encoder.Encode(cfg)
}

// Bucket describes a range of time with a tableName and hashKey
type Bucket struct {
	from      uint32
	through   uint32
	tableName string
	hashKey   string
}

func (cfg *PeriodConfig) hourlyBuckets(from, through model.Time, userID string) []Bucket {
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

func (cfg *PeriodConfig) dailyBuckets(from, through model.Time, userID string) []Bucket {
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
	Prefix string        `yaml:"prefix"`
	Period time.Duration `yaml:"period,omitempty"`
	Tags   Tags          `yaml:"tags,omitempty"`

	ProvisionedWriteThroughput int64 `yaml:"write_throughput,omitempty"`
	ProvisionedReadThroughput  int64 `yaml:"read_throughput,omitempty"`
	InactiveWriteThroughput    int64 `yaml:"inactive_write_throughput,omitempty"`
	InactiveReadThroughput     int64 `yaml:"inactive_read_throughput,omitempty"`

	WriteScale              AutoScalingConfig `yaml:"write_scale,omitempty"`
	InactiveWriteScale      AutoScalingConfig `yaml:"inactive_write_scale,omitempty"`
	InactiveWriteScaleLastN int64             `yaml:"inactive_write_scale_last_n,omitempty"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *PeriodicTableConfig) RegisterFlags(argPrefix, tablePrefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Prefix, argPrefix+".prefix", tablePrefix, "DynamoDB table prefix for period tables.")
	f.DurationVar(&cfg.Period, argPrefix+".period", 7*24*time.Hour, "DynamoDB table period.")
	f.Var(&cfg.Tags, argPrefix+".tag", "Tag (of the form key=value) to be added to all tables under management.")

	f.Int64Var(&cfg.ProvisionedWriteThroughput, argPrefix+".write-throughput", 3000, "DynamoDB table default write throughput.")
	f.Int64Var(&cfg.ProvisionedReadThroughput, argPrefix+".read-throughput", 300, "DynamoDB table default read throughput.")
	f.Int64Var(&cfg.InactiveWriteThroughput, argPrefix+".inactive-write-throughput", 1, "DynamoDB table write throughput for inactive tables.")
	f.Int64Var(&cfg.InactiveReadThroughput, argPrefix+".inactive-read-throughput", 300, "DynamoDB table read throughput for inactive tables.")

	cfg.WriteScale.RegisterFlags(argPrefix+".write-throughput.scale", f)
	cfg.InactiveWriteScale.RegisterFlags(argPrefix+".inactive-write-throughput.scale", f)
	f.Int64Var(&cfg.InactiveWriteScaleLastN, argPrefix+".inactive-write-throughput.scale-last-n", 4, "Number of last inactive tables to enable write autoscale.")
}

// AutoScalingConfig for DynamoDB tables.
type AutoScalingConfig struct {
	Enabled     bool    `yaml:"enabled,omitempty"`
	RoleARN     string  `yaml:"role_arn,omitempty"`
	MinCapacity int64   `yaml:"min_capacity,omitempty"`
	MaxCapacity int64   `yaml:"max_capacity,omitempty"`
	OutCooldown int64   `yaml:"out_cooldown,omitempty"`
	InCooldown  int64   `yaml:"in_cooldown,omitempty"`
	TargetValue float64 `yaml:"target,omitempty"`
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

func (cfg *PeriodicTableConfig) periodicTables(from, through model.Time, beginGrace, endGrace time.Duration) []TableDesc {
	var (
		periodSecs     = int64(cfg.Period / time.Second)
		beginGraceSecs = int64(beginGrace / time.Second)
		endGraceSecs   = int64(endGrace / time.Second)
		firstTable     = from.Unix() / periodSecs
		lastTable      = through.Unix() / periodSecs
		now            = mtime.Now().Unix()
		result         = []TableDesc{}
	)
	// If through ends on 00:00 of the day, don't include the upcoming day
	if through.Unix()%secondsInDay == 0 {
		lastTable--
	}
	for i := firstTable; i <= lastTable; i++ {
		table := TableDesc{
			// Name construction needs to be consistent with chunk_store.bigBuckets
			Name:             cfg.Prefix + strconv.Itoa(int(i)),
			ProvisionedRead:  cfg.InactiveReadThroughput,
			ProvisionedWrite: cfg.InactiveWriteThroughput,
			Tags:             cfg.Tags,
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

// ChunkTableFor calculates the chunk table shard for a given point in time.
func (cfg SchemaConfig) ChunkTableFor(t model.Time) string {
	for i := range cfg.Configs {
		if t > cfg.Configs[i].From && (i+1 == len(cfg.Configs) || t < cfg.Configs[i+1].From) {
			return cfg.Configs[i].ChunkTables.TableFor(t)
		}
	}
	return ""
}

// TableFor calculates the table shard for a given point in time.
func (cfg *PeriodicTableConfig) TableFor(t model.Time) string {
	var (
		periodSecs = int64(cfg.Period / time.Second)
		table      = t.Unix() / periodSecs
	)
	return cfg.Prefix + strconv.Itoa(int(table))
}
