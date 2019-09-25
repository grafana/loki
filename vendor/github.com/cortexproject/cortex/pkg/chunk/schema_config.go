package chunk

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/mtime"
	yaml "gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

const (
	secondsInHour      = int64(time.Hour / time.Second)
	secondsInDay       = int64(24 * time.Hour / time.Second)
	millisecondsInHour = int64(time.Hour / time.Millisecond)
	millisecondsInDay  = int64(24 * time.Hour / time.Millisecond)
)

// PeriodConfig defines the schema and tables to use for a period of time
type PeriodConfig struct {
	From        DayTime             `yaml:"from"`         // used when working with config
	IndexType   string              `yaml:"store"`        // type of index client to use.
	ObjectType  string              `yaml:"object_store"` // type of object client to use; if omitted, defaults to store.
	Schema      string              `yaml:"schema"`
	IndexTables PeriodicTableConfig `yaml:"index"`
	ChunkTables PeriodicTableConfig `yaml:"chunks,omitempty"`
	RowShards   uint32              `yaml:"row_shards"`
}

// DayTime is a model.Time what holds day-aligned values, and marshals to/from
// YAML in YYYY-MM-DD format.
type DayTime struct {
	model.Time
}

// MarshalYAML implements yaml.Marshaller.
func (d DayTime) MarshalYAML() (interface{}, error) {
	return d.Time.Time().Format("2006-01-02"), nil
}

// UnmarshalYAML implements yaml.Unmarshaller.
func (d *DayTime) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var from string
	if err := unmarshal(&from); err != nil {
		return err
	}
	t, err := time.Parse("2006-01-02", from)
	if err != nil {
		return err
	}
	d.Time = model.TimeFromUnix(t.Unix())
	return nil
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
	DailyBucketsFrom      flagext.DayValue
	Base64ValuesFrom      flagext.DayValue
	V4SchemaFrom          flagext.DayValue
	V5SchemaFrom          flagext.DayValue
	V6SchemaFrom          flagext.DayValue
	V9SchemaFrom          flagext.DayValue
	BigtableColumnKeyFrom flagext.DayValue

	// Config for the index & chunk tables.
	OriginalTableName string
	UsePeriodicTables bool
	IndexTablesFrom   flagext.DayValue
	IndexTables       PeriodicTableConfig
	ChunkTablesFrom   flagext.DayValue
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
			From:      DayTime{f},
			Schema:    t,
			IndexType: cfg.legacy.StorageClient,
			IndexTables: PeriodicTableConfig{
				Prefix: cfg.legacy.OriginalTableName,
				Tags:   cfg.legacy.IndexTables.Tags,
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
		config.IndexTables = cfg.legacy.IndexTables
	})
	if cfg.legacy.ChunkTablesFrom.IsSet() {
		cfg.ForEachAfter(cfg.legacy.ChunkTablesFrom.Time, func(config *PeriodConfig) {
			if config.IndexType == "aws" {
				config.IndexType = "aws-dynamo"
			}
			config.ChunkTables = cfg.legacy.ChunkTables
		})
	}
	if cfg.legacy.BigtableColumnKeyFrom.IsSet() {
		cfg.ForEachAfter(cfg.legacy.BigtableColumnKeyFrom.Time, func(config *PeriodConfig) {
			config.IndexType = "gcp-columnkey"
		})
	}
	return nil
}

// ForEachAfter will call f() on every entry after t, splitting
// entries if necessary so there is an entry starting at t
func (cfg *SchemaConfig) ForEachAfter(t model.Time, f func(config *PeriodConfig)) {
	for i := 0; i < len(cfg.Configs); i++ {
		if t > cfg.Configs[i].From.Time &&
			(i+1 == len(cfg.Configs) || t < cfg.Configs[i+1].From.Time) {
			// Split the i'th entry by duplicating then overwriting the From time
			cfg.Configs = append(cfg.Configs[:i+1], cfg.Configs[i:]...)
			cfg.Configs[i+1].From = DayTime{t}
		}
		if cfg.Configs[i].From.Time >= t {
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
	case "v10":
		rowShards := uint32(16)
		if cfg.RowShards > 0 {
			rowShards = cfg.RowShards
		}

		s = schema{cfg.dailyBuckets, v10Entries{
			rowShards: rowShards,
		}}
	}
	return s
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
	return decoder.Decode(&cfg)
}

// PrintYaml dumps the yaml to stdout, to aid in migration
func (cfg SchemaConfig) PrintYaml() {
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
			tableName: cfg.IndexTables.TableFor(model.TimeFromUnix(i * secondsInHour)),
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
			tableName: cfg.IndexTables.TableFor(model.TimeFromUnix(i * secondsInDay)),
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
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *PeriodicTableConfig) RegisterFlags(argPrefix, tablePrefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Prefix, argPrefix+".prefix", tablePrefix, "DynamoDB table prefix for period tables.")
	f.DurationVar(&cfg.Period, argPrefix+".period", 7*24*time.Hour, "DynamoDB table period.")
	f.Var(&cfg.Tags, argPrefix+".tag", "Tag (of the form key=value) to be added to all tables under management.")
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

func (cfg *PeriodicTableConfig) periodicTables(from, through model.Time, pCfg ProvisionConfig, beginGrace, endGrace time.Duration, retention time.Duration) []TableDesc {
	var (
		periodSecs     = int64(cfg.Period / time.Second)
		beginGraceSecs = int64(beginGrace / time.Second)
		endGraceSecs   = int64(endGrace / time.Second)
		firstTable     = from.Unix() / periodSecs
		lastTable      = through.Unix() / periodSecs
		tablesToKeep   = int64(int64(retention/time.Second) / periodSecs)
		now            = mtime.Now().Unix()
		nowWeek        = now / periodSecs
		result         = []TableDesc{}
	)
	// If through ends on 00:00 of the day, don't include the upcoming day
	if through.Unix()%secondsInDay == 0 {
		lastTable--
	}
	// Don't make tables further back than the configured retention
	if retention > 0 && lastTable > tablesToKeep && lastTable-firstTable >= tablesToKeep {
		firstTable = lastTable - tablesToKeep
	}
	for i := firstTable; i <= lastTable; i++ {
		table := TableDesc{
			Name:              cfg.tableForPeriod(i),
			ProvisionedRead:   pCfg.InactiveReadThroughput,
			ProvisionedWrite:  pCfg.InactiveWriteThroughput,
			UseOnDemandIOMode: pCfg.InactiveThroughputOnDemandMode,
			Tags:              cfg.Tags,
		}
		level.Debug(util.Logger).Log("msg", "Expected Table", "tableName", table.Name,
			"provisionedRead", table.ProvisionedRead,
			"provisionedWrite", table.ProvisionedWrite,
			"useOnDemandMode", table.UseOnDemandIOMode,
		)

		// if now is within table [start - grace, end + grace), then we need some write throughput
		if (i*periodSecs)-beginGraceSecs <= now && now < (i*periodSecs)+periodSecs+endGraceSecs {
			table.ProvisionedRead = pCfg.ProvisionedReadThroughput
			table.ProvisionedWrite = pCfg.ProvisionedWriteThroughput
			table.UseOnDemandIOMode = pCfg.ProvisionedThroughputOnDemandMode

			if pCfg.WriteScale.Enabled {
				table.WriteScale = pCfg.WriteScale
				table.UseOnDemandIOMode = false
			}

			if pCfg.ReadScale.Enabled {
				table.ReadScale = pCfg.ReadScale
				table.UseOnDemandIOMode = false
			}
			level.Debug(util.Logger).Log("msg", "Table is Active",
				"tableName", table.Name,
				"provisionedRead", table.ProvisionedRead,
				"provisionedWrite", table.ProvisionedWrite,
				"useOnDemandMode", table.UseOnDemandIOMode,
				"useWriteAutoScale", table.WriteScale.Enabled,
				"useReadAutoScale", table.ReadScale.Enabled)

		} else if pCfg.InactiveWriteScale.Enabled || pCfg.InactiveReadScale.Enabled {
			// Autoscale last N tables
			// this is measured against "now", since the lastWeek is the final week in the schema config range
			// the N last tables in that range will always be set to the inactive scaling settings.
			if pCfg.InactiveWriteScale.Enabled && i >= (nowWeek-pCfg.InactiveWriteScaleLastN) {
				table.WriteScale = pCfg.InactiveWriteScale
				table.UseOnDemandIOMode = false
			}
			if pCfg.InactiveReadScale.Enabled && i >= (nowWeek-pCfg.InactiveReadScaleLastN) {
				table.ReadScale = pCfg.InactiveReadScale
				table.UseOnDemandIOMode = false
			}

			level.Debug(util.Logger).Log("msg", "Table is Inactive",
				"tableName", table.Name,
				"provisionedRead", table.ProvisionedRead,
				"provisionedWrite", table.ProvisionedWrite,
				"useOnDemandMode", table.UseOnDemandIOMode,
				"useWriteAutoScale", table.WriteScale.Enabled,
				"useReadAutoScale", table.ReadScale.Enabled)
		}

		result = append(result, table)
	}
	return result
}

// ChunkTableFor calculates the chunk table shard for a given point in time.
func (cfg SchemaConfig) ChunkTableFor(t model.Time) (string, error) {
	for i := range cfg.Configs {
		if t >= cfg.Configs[i].From.Time && (i+1 == len(cfg.Configs) || t < cfg.Configs[i+1].From.Time) {
			return cfg.Configs[i].ChunkTables.TableFor(t), nil
		}
	}
	return "", fmt.Errorf("no chunk table found for time %v", t)
}

// TableFor calculates the table shard for a given point in time.
func (cfg *PeriodicTableConfig) TableFor(t model.Time) string {
	if cfg.Period == 0 { // non-periodic
		return cfg.Prefix
	}
	periodSecs := int64(cfg.Period / time.Second)
	return cfg.tableForPeriod(t.Unix() / periodSecs)
}

func (cfg *PeriodicTableConfig) tableForPeriod(i int64) string {
	return cfg.Prefix + strconv.Itoa(int(i))
}
