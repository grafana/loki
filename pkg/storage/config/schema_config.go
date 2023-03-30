package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/mtime"
	yaml "gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/log"
)

const (
	// Supported storage clients

	StorageTypeAlibabaCloud   = "alibabacloud"
	StorageTypeAWS            = "aws"
	StorageTypeAWSDynamo      = "aws-dynamo"
	StorageTypeAzure          = "azure"
	StorageTypeBOS            = "bos"
	StorageTypeBoltDB         = "boltdb"
	StorageTypeCassandra      = "cassandra"
	StorageTypeInMemory       = "inmemory"
	StorageTypeBigTable       = "bigtable"
	StorageTypeBigTableHashed = "bigtable-hashed"
	StorageTypeFileSystem     = "filesystem"
	StorageTypeGCP            = "gcp"
	StorageTypeGCPColumnKey   = "gcp-columnkey"
	StorageTypeGCS            = "gcs"
	StorageTypeGrpc           = "grpc-store"
	StorageTypeLocal          = "local"
	StorageTypeS3             = "s3"
	StorageTypeSwift          = "swift"
	StorageTypeCOS            = "cos"
	// BoltDBShipperType holds the index type for using boltdb with shipper which keeps flushing them to a shared storage
	BoltDBShipperType = "boltdb-shipper"
	TSDBType          = "tsdb"

	// ObjectStorageIndexRequiredPeriod defines the required index period for object storage based index stores like boltdb-shipper and tsdb
	ObjectStorageIndexRequiredPeriod = 24 * time.Hour
)

var (
	errInvalidSchemaVersion     = errors.New("invalid schema version")
	errInvalidTablePeriod       = errors.New("the table period must be a multiple of 24h (1h for schema v1)")
	errConfigFileNotSet         = errors.New("schema config file needs to be set")
	errConfigChunkPrefixNotSet  = errors.New("schema config for chunks is missing the 'prefix' setting")
	errSchemaIncreasingFromTime = errors.New("from time in schemas must be distinct and in increasing order")

	errCurrentBoltdbShipperNon24Hours  = errors.New("boltdb-shipper works best with 24h periodic index config. Either add a new config with future date set to 24h to retain the existing index or change the existing config to use 24h period")
	errUpcomingBoltdbShipperNon24Hours = errors.New("boltdb-shipper with future date must always have periodic config for index set to 24h")
	errTSDBNon24HoursIndexPeriod       = errors.New("tsdb must always have periodic config for index set to 24h")
	errZeroLengthConfig                = errors.New("must specify at least one schema configuration")
)

// TableRange represents a range of table numbers built based on the configured schema start/end date and the table period.
// Both Start and End are inclusive.
type TableRange struct {
	Start, End   int64
	PeriodConfig *PeriodConfig
}

// TableRanges represents a list of table ranges for multiple schemas.
type TableRanges []TableRange

// TableInRange tells whether given table falls in any of the ranges and the tableName has the right prefix based on the schema config.
func (t TableRanges) TableInRange(tableNumber int64, tableName string) bool {
	cfg := t.ConfigForTableNumber(tableNumber)
	return cfg != nil && fmt.Sprintf("%s%s", cfg.IndexTables.Prefix, strconv.Itoa(int(tableNumber))) == tableName
}

func (t TableRanges) ConfigForTableNumber(tableNumber int64) *PeriodConfig {
	for _, r := range t {
		if r.Start <= tableNumber && tableNumber <= r.End {
			return r.PeriodConfig
		}
	}

	return nil
}

// PeriodConfig defines the schema and tables to use for a period of time
type PeriodConfig struct {
	// used when working with config
	From DayTime `yaml:"from" doc:"description=The date of the first day that index buckets should be created. Use a date in the past if this is your only period_config, otherwise use a date when you want the schema to switch over. In YYYY-MM-DD format, for example: 2018-04-15."`
	// type of index client to use.
	IndexType string `yaml:"store" doc:"description=store and object_store below affect which <storage_config> key is used.\nWhich store to use for the index. Either aws, aws-dynamo, gcp, bigtable, bigtable-hashed, cassandra, boltdb or boltdb-shipper. "`
	// type of object client to use; if omitted, defaults to store.
	ObjectType  string              `yaml:"object_store" doc:"description=Which store to use for the chunks. Either aws, azure, gcp, bigtable, gcs, cassandra, swift, filesystem or a named_store (refer to named_stores_config). If omitted, defaults to the same value as store."`
	Schema      string              `yaml:"schema" doc:"description=The schema version to use, current recommended schema is v11."`
	IndexTables PeriodicTableConfig `yaml:"index" doc:"description=Configures how the index is updated and stored."`
	ChunkTables PeriodicTableConfig `yaml:"chunks" doc:"description=Configured how the chunks are updated and stored."`
	RowShards   uint32              `yaml:"row_shards" doc:"description=How many shards will be created. Only used if schema is v10 or greater."`

	// Integer representation of schema used for hot path calculation. Populated on unmarshaling.
	schemaInt *int `yaml:"-"`
}

// UnmarshalYAML implements yaml.Unmarshaller.
func (cfg *PeriodConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain PeriodConfig
	err := unmarshal((*plain)(cfg))
	if err != nil {
		return err
	}

	// call VersionAsInt after unmarshaling to errcheck schema version and populate PeriodConfig.schemaInt
	_, err = cfg.VersionAsInt()
	return err
}

// GetIndexTableNumberRange returns the table number range calculated based on
// the configured schema start date, index table period and the given schemaEndDate
func (cfg *PeriodConfig) GetIndexTableNumberRange(schemaEndDate DayTime) TableRange {
	return TableRange{
		Start:        cfg.From.Unix() / int64(cfg.IndexTables.Period/time.Second),
		End:          schemaEndDate.Unix() / int64(cfg.IndexTables.Period/time.Second),
		PeriodConfig: cfg,
	}
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
	return d.Time.Time().UTC().Format("2006-01-02")
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
	if len(cfg.Configs) == 0 {
		return errZeroLengthConfig
	}
	activePCIndex := ActivePeriodConfig((*cfg).Configs)

	// if current index type is boltdb-shipper and there are no upcoming index types then it should be set to 24 hours.
	if cfg.Configs[activePCIndex].IndexType == BoltDBShipperType &&
		cfg.Configs[activePCIndex].IndexTables.Period != ObjectStorageIndexRequiredPeriod && len(cfg.Configs)-1 == activePCIndex {
		return errCurrentBoltdbShipperNon24Hours
	}

	// if upcoming index type is boltdb-shipper, it should always be set to 24 hours.
	if len(cfg.Configs)-1 > activePCIndex && (cfg.Configs[activePCIndex+1].IndexType == BoltDBShipperType &&
		cfg.Configs[activePCIndex+1].IndexTables.Period != ObjectStorageIndexRequiredPeriod) {
		return errUpcomingBoltdbShipperNon24Hours
	}

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

// ActivePeriodConfig returns index of active PeriodicConfig which would be applicable to logs that would be pushed starting now.
// Note: Another PeriodicConfig might be applicable for future logs which can change index type.
func ActivePeriodConfig(configs []PeriodConfig) int {
	now := model.Now()
	i := sort.Search(len(configs), func(i int) bool {
		return configs[i].From.Time > now
	})
	if i > 0 {
		i--
	}
	return i
}

func usingForPeriodConfigs(configs []PeriodConfig, fn func(PeriodConfig) bool) bool {
	activePCIndex := ActivePeriodConfig(configs)

	if fn(configs[activePCIndex]) ||
		(len(configs)-1 > activePCIndex && fn(configs[activePCIndex+1])) {
		return true
	}

	return false
}

func UsingObjectStorageIndex(configs []PeriodConfig) bool {
	fn := func(cfg PeriodConfig) bool {
		switch cfg.IndexType {
		case BoltDBShipperType, TSDBType:
			return true
		default:
			return false
		}
	}

	return usingForPeriodConfigs(configs, fn)
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

	if cfg.IndexType == TSDBType && cfg.IndexTables.Period != ObjectStorageIndexRequiredPeriod {
		return errTSDBNon24HoursIndexPeriod
	}

	// Ensure the tables period is a multiple of the bucket period
	if cfg.IndexTables.Period > 0 && cfg.IndexTables.Period%(24*time.Hour) != 0 {
		return errInvalidTablePeriod
	}

	if cfg.ChunkTables.Period > 0 && cfg.ChunkTables.Period%(24*time.Hour) != 0 {
		return errInvalidTablePeriod
	}
	v, err := cfg.VersionAsInt()
	if err != nil {
		return err
	}

	switch v {
	case 10, 11, 12:
		if cfg.RowShards == 0 {
			return fmt.Errorf("must have row_shards > 0 (current: %d) for schema (%s)", cfg.RowShards, cfg.Schema)
		}
	case 9:
		return nil
	default:
		return errInvalidSchemaVersion
	}
	return nil
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

func (cfg *PeriodConfig) VersionAsInt() (int, error) {
	// Read memoized schema version. This is called during unmarshaling,
	// but may be nil in the case of testware.
	if cfg.schemaInt != nil {
		return *cfg.schemaInt, nil
	}

	v := strings.Trim(cfg.Schema, "v")
	n, err := strconv.Atoi(v)
	cfg.schemaInt = &n
	return n, err
}

// PeriodicTableConfig is configuration for a set of time-sharded tables.
type PeriodicTableConfig struct {
	Prefix string        `yaml:"prefix" doc:"description=Table prefix for all period tables."`
	Period time.Duration `yaml:"period" doc:"description=Table period."`
	Tags   Tags          `yaml:"tags" doc:"description=A map to be added to all managed tables."`
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

func (cfg *PeriodicTableConfig) PeriodicTables(from, through model.Time, pCfg ProvisionConfig, beginGrace, endGrace time.Duration, retention time.Duration) []TableDesc {
	var (
		periodSecs     = int64(cfg.Period / time.Second)
		beginGraceSecs = int64(beginGrace / time.Second)
		endGraceSecs   = int64(endGrace / time.Second)
		firstTable     = from.Unix() / periodSecs
		lastTable      = through.Unix() / periodSecs
		tablesToKeep   = int64(retention/time.Second) / periodSecs
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

// SchemaForTime returns the Schema PeriodConfig to use for a given point in time.
func (cfg SchemaConfig) SchemaForTime(t model.Time) (PeriodConfig, error) {
	for i := range cfg.Configs {
		// TODO: callum, confirm we can rely on the schema configs being sorted in this order.
		if t >= cfg.Configs[i].From.Time && (i+1 == len(cfg.Configs) || t < cfg.Configs[i+1].From.Time) {
			return cfg.Configs[i], nil
		}
	}
	return PeriodConfig{}, fmt.Errorf("no schema config found for time %v", t)
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

// Generate the appropriate external key based on cfg.Schema, chunk.Checksum, and chunk.From
func (cfg SchemaConfig) ExternalKey(ref logproto.ChunkRef) string {
	p, err := cfg.SchemaForTime(ref.From)
	v, _ := p.VersionAsInt()
	if err == nil && v >= 12 {
		return newerExternalKey(ref)
	}
	return newExternalKey(ref)
}

// VersionForChunk will return the schema version associated with the `From` timestamp of a chunk.
// The schema and chunk must be valid+compatible as the errors are not checked.
func (cfg SchemaConfig) VersionForChunk(ref logproto.ChunkRef) int {
	p, _ := cfg.SchemaForTime(ref.From)
	v, _ := p.VersionAsInt()
	return v
}

// post-checksum
func newExternalKey(ref logproto.ChunkRef) string {
	// This is the inverse of chunk.parseNewExternalKey.
	return fmt.Sprintf("%s/%x:%x:%x:%x", ref.UserID, ref.Fingerprint, int64(ref.From), int64(ref.Through), ref.Checksum)
}

// v12+
func newerExternalKey(ref logproto.ChunkRef) string {
	return fmt.Sprintf("%s/%x/%x:%x:%x", ref.UserID, ref.Fingerprint, int64(ref.From), int64(ref.Through), ref.Checksum)
}
