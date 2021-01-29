package chunk

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/mtime"
	yaml "gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/math"
)

const (
	secondsInHour      = int64(time.Hour / time.Second)
	secondsInDay       = int64(24 * time.Hour / time.Second)
	millisecondsInHour = int64(time.Hour / time.Millisecond)
	millisecondsInDay  = int64(24 * time.Hour / time.Millisecond)
)

var (
	errInvalidSchemaVersion     = errors.New("invalid schema version")
	errInvalidTablePeriod       = errors.New("the table period must be a multiple of 24h (1h for schema v1)")
	errConfigFileNotSet         = errors.New("schema config file needs to be set")
	errConfigChunkPrefixNotSet  = errors.New("schema config for chunks is missing the 'prefix' setting")
	errSchemaIncreasingFromTime = errors.New("from time in schemas must be distinct and in increasing order")
)

// PeriodConfig defines the schema and tables to use for a period of time
type PeriodConfig struct {
	From        DayTime             `yaml:"from"`         // used when working with config
	IndexType   string              `yaml:"store"`        // type of index client to use.
	ObjectType  string              `yaml:"object_store"` // type of object client to use; if omitted, defaults to store.
	Schema      string              `yaml:"schema"`
	IndexTables PeriodicTableConfig `yaml:"index"`
	ChunkTables PeriodicTableConfig `yaml:"chunks"`
	RowShards   uint32              `yaml:"row_shards"`
}

// DayTime is a model.Time what holds day-aligned values, and marshals to/from
// YAML in YYYY-MM-DD format.
type DayTime struct {
	model.Time
}

// MarshalYAML implements yaml.Marshaller.
func (d DayTime) MarshalYAML() (interface{}, error) {
	return d.String(), nil
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

func (d *DayTime) String() string {
	return d.Time.Time().Format("2006-01-02")
}

// SchemaConfig contains the config for our chunk index schemas
type SchemaConfig struct {
	Configs []PeriodConfig `yaml:"configs"`

	fileName string
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *SchemaConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.fileName, "schema-config-file", "", "The path to the schema config file. The schema config is used only when running Cortex with the chunks storage.")
}

// loadFromFile loads the schema config from a yaml file
func (cfg *SchemaConfig) loadFromFile() error {
	if cfg.fileName == "" {
		return errConfigFileNotSet
	}

	f, err := os.Open(cfg.fileName)
	if err != nil {
		return err
	}

	decoder := yaml.NewDecoder(f)
	decoder.SetStrict(true)
	return decoder.Decode(&cfg)
}

// Validate the schema config and returns an error if the validation
// doesn't pass
func (cfg *SchemaConfig) Validate() error {
	for i := range cfg.Configs {
		periodCfg := &cfg.Configs[i]
		periodCfg.applyDefaults()
		if err := periodCfg.validate(); err != nil {
			return err
		}

		if i+1 < len(cfg.Configs) {
			if cfg.Configs[i].From.Time.Unix() >= cfg.Configs[i+1].From.Time.Unix() {
				return errSchemaIncreasingFromTime
			}
		}
	}
	return nil
}

func defaultRowShards(schema string) uint32 {
	switch schema {
	case "v1", "v2", "v3", "v4", "v5", "v6", "v9":
		return 0
	default:
		return 16
	}
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

func validateChunks(cfg PeriodConfig) error {
	objectStore := cfg.IndexType
	if cfg.ObjectType != "" {
		objectStore = cfg.ObjectType
	}
	switch objectStore {
	case "cassandra", "aws-dynamo", "bigtable-hashed", "gcp", "gcp-columnkey", "bigtable", "grpc-store":
		if cfg.ChunkTables.Prefix == "" {
			return errConfigChunkPrefixNotSet
		}
		return nil
	default:
		return nil
	}
}

// CreateSchema returns the schema defined by the PeriodConfig
func (cfg PeriodConfig) CreateSchema() (BaseSchema, error) {
	buckets, bucketsPeriod := cfg.createBucketsFunc()

	// Ensure the tables period is a multiple of the bucket period
	if cfg.IndexTables.Period > 0 && cfg.IndexTables.Period%bucketsPeriod != 0 {
		return nil, errInvalidTablePeriod
	}

	if cfg.ChunkTables.Period > 0 && cfg.ChunkTables.Period%bucketsPeriod != 0 {
		return nil, errInvalidTablePeriod
	}

	switch cfg.Schema {
	case "v1":
		return newStoreSchema(buckets, originalEntries{}), nil
	case "v2":
		return newStoreSchema(buckets, originalEntries{}), nil
	case "v3":
		return newStoreSchema(buckets, base64Entries{originalEntries{}}), nil
	case "v4":
		return newStoreSchema(buckets, labelNameInHashKeyEntries{}), nil
	case "v5":
		return newStoreSchema(buckets, v5Entries{}), nil
	case "v6":
		return newStoreSchema(buckets, v6Entries{}), nil
	case "v9":
		return newSeriesStoreSchema(buckets, v9Entries{}), nil
	case "v10", "v11":
		if cfg.RowShards == 0 {
			return nil, fmt.Errorf("Must have row_shards > 0 (current: %d) for schema (%s)", cfg.RowShards, cfg.Schema)
		}

		v10 := v10Entries{rowShards: cfg.RowShards}
		if cfg.Schema == "v10" {
			return newSeriesStoreSchema(buckets, v10), nil
		}

		return newSeriesStoreSchema(buckets, v11Entries{v10}), nil
	default:
		return nil, errInvalidSchemaVersion
	}
}

func (cfg PeriodConfig) createBucketsFunc() (schemaBucketsFunc, time.Duration) {
	switch cfg.Schema {
	case "v1":
		return cfg.hourlyBuckets, 1 * time.Hour
	default:
		return cfg.dailyBuckets, 24 * time.Hour
	}
}

func (cfg *PeriodConfig) applyDefaults() {
	if cfg.RowShards == 0 {
		cfg.RowShards = defaultRowShards(cfg.Schema)
	}
}

// Validate the period config.
func (cfg PeriodConfig) validate() error {
	validateError := validateChunks(cfg)
	if validateError != nil {
		return validateError
	}

	_, err := cfg.CreateSchema()
	return err
}

// Load the yaml file, or build the config from legacy command-line flags
func (cfg *SchemaConfig) Load() error {
	if len(cfg.Configs) > 0 {
		return nil
	}

	// Load config from file.
	if err := cfg.loadFromFile(); err != nil {
		return err
	}

	return cfg.Validate()
}

// Bucket describes a range of time with a tableName and hashKey
type Bucket struct {
	from       uint32
	through    uint32
	tableName  string
	hashKey    string
	bucketSize uint32 // helps with deletion of series ids in series store. Size in milliseconds.
}

func (cfg *PeriodConfig) hourlyBuckets(from, through model.Time, userID string) []Bucket {
	var (
		fromHour    = from.Unix() / secondsInHour
		throughHour = through.Unix() / secondsInHour
		result      = []Bucket{}
	)

	for i := fromHour; i <= throughHour; i++ {
		relativeFrom := math.Max64(0, int64(from)-(i*millisecondsInHour))
		relativeThrough := math.Min64(millisecondsInHour, int64(through)-(i*millisecondsInHour))
		result = append(result, Bucket{
			from:       uint32(relativeFrom),
			through:    uint32(relativeThrough),
			tableName:  cfg.IndexTables.TableFor(model.TimeFromUnix(i * secondsInHour)),
			hashKey:    fmt.Sprintf("%s:%d", userID, i),
			bucketSize: uint32(millisecondsInHour), // helps with deletion of series ids in series store
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

		relativeFrom := math.Max64(0, int64(from)-(i*millisecondsInDay))
		relativeThrough := math.Min64(millisecondsInDay, int64(through)-(i*millisecondsInDay))
		result = append(result, Bucket{
			from:       uint32(relativeFrom),
			through:    uint32(relativeThrough),
			tableName:  cfg.IndexTables.TableFor(model.TimeFromUnix(i * secondsInDay)),
			hashKey:    fmt.Sprintf("%s:d%d", userID, i),
			bucketSize: uint32(millisecondsInDay), // helps with deletion of series ids in series store
		})
	}
	return result
}

// PeriodicTableConfig is configuration for a set of time-sharded tables.
type PeriodicTableConfig struct {
	Prefix string
	Period time.Duration
	Tags   Tags
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *PeriodicTableConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	g := struct {
		Prefix string         `yaml:"prefix"`
		Period model.Duration `yaml:"period"`
		Tags   Tags           `yaml:"tags"`
	}{}
	if err := unmarshal(&g); err != nil {
		return err
	}

	cfg.Prefix = g.Prefix
	cfg.Period = time.Duration(g.Period)
	cfg.Tags = g.Tags

	return nil
}

// MarshalYAML implements the yaml.Marshaler interface.
func (cfg PeriodicTableConfig) MarshalYAML() (interface{}, error) {
	g := &struct {
		Prefix string         `yaml:"prefix"`
		Period model.Duration `yaml:"period"`
		Tags   Tags           `yaml:"tags"`
	}{
		Prefix: cfg.Prefix,
		Period: model.Duration(cfg.Period),
		Tags:   cfg.Tags,
	}

	return g, nil
}

// AutoScalingConfig for DynamoDB tables.
type AutoScalingConfig struct {
	Enabled     bool    `yaml:"enabled"`
	RoleARN     string  `yaml:"role_arn"`
	MinCapacity int64   `yaml:"min_capacity"`
	MaxCapacity int64   `yaml:"max_capacity"`
	OutCooldown int64   `yaml:"out_cooldown"`
	InCooldown  int64   `yaml:"in_cooldown"`
	TargetValue float64 `yaml:"target"`
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
	// If interval ends exactly on a period boundary, don’t include the upcoming period
	if through.Unix()%periodSecs == 0 {
		lastTable--
	}
	// Don't make tables further back than the configured retention
	if retention > 0 && lastTable > tablesToKeep && lastTable-firstTable >= tablesToKeep {
		firstTable = lastTable - tablesToKeep
	}
	for i := firstTable; i <= lastTable; i++ {
		tableName := cfg.tableForPeriod(i)
		table := TableDesc{}

		// if now is within table [start - grace, end + grace), then we need some write throughput
		if (i*periodSecs)-beginGraceSecs <= now && now < (i*periodSecs)+periodSecs+endGraceSecs {
			table = pCfg.ActiveTableProvisionConfig.BuildTableDesc(tableName, cfg.Tags)

			level.Debug(log.Logger).Log("msg", "Table is Active",
				"tableName", table.Name,
				"provisionedRead", table.ProvisionedRead,
				"provisionedWrite", table.ProvisionedWrite,
				"useOnDemandMode", table.UseOnDemandIOMode,
				"useWriteAutoScale", table.WriteScale.Enabled,
				"useReadAutoScale", table.ReadScale.Enabled)

		} else {
			// Autoscale last N tables
			// this is measured against "now", since the lastWeek is the final week in the schema config range
			// the N last tables in that range will always be set to the inactive scaling settings.
			disableAutoscale := i < (nowWeek - pCfg.InactiveWriteScaleLastN)
			table = pCfg.InactiveTableProvisionConfig.BuildTableDesc(tableName, cfg.Tags, disableAutoscale)

			level.Debug(log.Logger).Log("msg", "Table is Inactive",
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
