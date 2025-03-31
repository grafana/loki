package config

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/mtime"
	"github.com/prometheus/common/model"
	yaml "gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/log"
)

const (
	// Supported storage clients

	// BoltDBShipperType holds the index type for using boltdb with shipper which keeps flushing them to a shared storage

	// ObjectStorageIndexRequiredPeriod defines the required index period for object storage based index stores like boltdb-shipper and tsdb
	ObjectStorageIndexRequiredPeriod = 24 * time.Hour

	pathPrefixDelimiter = "/"
)

var (
	errInvalidSchemaVersion     = errors.New("invalid schema version")
	errInvalidTablePeriod       = errors.New("the table period must be a multiple of 24h (1h for schema v1)")
	errInvalidTableName         = errors.New("invalid table name")
	errConfigFileNotSet         = errors.New("schema config file needs to be set")
	errConfigChunkPrefixNotSet  = errors.New("schema config for chunks is missing the 'prefix' setting")
	errSchemaIncreasingFromTime = errors.New("from time in schemas must be distinct and in increasing order")

	errCurrentBoltdbShipperNon24Hours  = errors.New("boltdb-shipper works best with 24h periodic index config. Either add a new config with future date set to 24h to retain the existing index or change the existing config to use 24h period")
	errUpcomingBoltdbShipperNon24Hours = errors.New("boltdb-shipper with future date must always have periodic config for index set to 24h")
	errTSDBNon24HoursIndexPeriod       = errors.New("tsdb must always have periodic config for index set to 24h")
	errZeroLengthConfig                = errors.New("must specify at least one schema configuration")

	// regexp for finding the trailing index table number at the end of the table name
	extractTableNumberRegex = regexp.MustCompile(`[0-9]+$`)
)

// ExtractTableNumberFromName extracts the table number from a given tableName.
// returns -1 on error.
func ExtractTableNumberFromName(tableName string) (int64, error) {
	match := extractTableNumberRegex.Find([]byte(tableName))
	if match == nil {
		return -1, errInvalidTableName
	}

	tableNumber, err := strconv.ParseInt(string(match), 10, 64)
	if err != nil {
		return -1, err
	}

	return tableNumber, nil
}

// TableRange represents a range of table numbers built based on the configured schema start/end date and the table period.
// Both Start and End are inclusive.
type TableRange struct {
	Start, End   int64
	PeriodConfig *PeriodConfig
}

// TableRanges represents a list of table ranges for multiple schemas.
type TableRanges []TableRange

// TableInRange tells whether given table falls in any of the ranges and the tableName has the right prefix based on the schema config.
func (t TableRanges) TableInRange(tableName string) (bool, error) {
	tableNumber, err := ExtractTableNumberFromName(tableName)
	if err != nil {
		return false, err
	}

	cfg := t.ConfigForTableNumber(tableNumber)
	return cfg != nil &&
		fmt.Sprintf("%s%s", cfg.IndexTables.Prefix, strconv.Itoa(int(tableNumber))) == tableName, nil
}

func (t TableRanges) ConfigForTableNumber(tableNumber int64) *PeriodConfig {
	for _, r := range t {
		if cfg := r.ConfigForTableNumber(tableNumber); cfg != nil {
			return cfg
		}
	}

	return nil
}

func (t TableRanges) TableNameFor(table int64) (string, bool) {
	cfg := t.ConfigForTableNumber(table)
	if cfg == nil {
		return "", false
	}
	return fmt.Sprintf("%s%d", cfg.IndexTables.Prefix, table), true
}

// TableInRange tells whether given table falls in the range and the tableName has the right prefix based on the schema config.
func (t TableRange) TableInRange(tableName string) (bool, error) {
	// non-periodic tables
	if t.PeriodConfig.IndexTables.Period == 0 {
		return t.PeriodConfig.IndexTables.Prefix == tableName, nil
	}

	tableNumber, err := ExtractTableNumberFromName(tableName)
	if err != nil {
		return false, err
	}

	cfg := t.ConfigForTableNumber(tableNumber)
	return cfg != nil &&
		fmt.Sprintf("%s%s", cfg.IndexTables.Prefix, strconv.Itoa(int(tableNumber))) == tableName, nil
}

func (t TableRange) ConfigForTableNumber(tableNumber int64) *PeriodConfig {
	if t.Start <= tableNumber && tableNumber <= t.End {
		return t.PeriodConfig
	}

	return nil
}

// PeriodConfig defines the schema and tables to use for a period of time
type PeriodConfig struct {
	// used when working with config
	From DayTime `yaml:"from" doc:"description=The date of the first day that index buckets should be created. Use a date in the past if this is your only period_config, otherwise use a date when you want the schema to switch over. In YYYY-MM-DD format, for example: 2018-04-15."`
	// type of index client to use.
	IndexType string `yaml:"store" doc:"description=store and object_store below affect which <storage_config> key is used. Which index to use. Either tsdb or boltdb-shipper. Following stores are deprecated: aws, aws-dynamo, gcp, gcp-columnkey, bigtable, bigtable-hashed, cassandra, grpc."`
	// type of object client to use.
	ObjectType  string                   `yaml:"object_store" doc:"description=Which store to use for the chunks. Either aws (alias s3), azure, gcs, alibabacloud, bos, cos, swift, filesystem, or a named_store (refer to named_stores_config). Following stores are deprecated: aws-dynamo, gcp, gcp-columnkey, bigtable, bigtable-hashed, cassandra, grpc."`
	Schema      string                   `yaml:"schema" doc:"description=The schema version to use, current recommended schema is v13."`
	IndexTables IndexPeriodicTableConfig `yaml:"index" doc:"description=Configures how the index is updated and stored."`
	ChunkTables PeriodicTableConfig      `yaml:"chunks" doc:"description=Configured how the chunks are updated and stored."`
	RowShards   uint32                   `yaml:"row_shards" doc:"default=16|description=How many shards will be created. Only used if schema is v10 or greater."`

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
	// non-periodic tables
	if cfg.IndexTables.Period == 0 {
		return TableRange{
			PeriodConfig: cfg,
		}
	}

	return TableRange{
		Start:        cfg.From.Unix() / int64(cfg.IndexTables.Period/time.Second),
		End:          schemaEndDate.Unix() / int64(cfg.IndexTables.Period/time.Second),
		PeriodConfig: cfg,
	}
}

func NewDayTime(d model.Time) DayTime {
	beginningOfDay := model.TimeFromUnix(d.Time().Truncate(24 * time.Hour).Unix())
	return DayTime{beginningOfDay}
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

func (d *DayTime) Set(value string) error {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return err
	}
	d.Time = model.TimeFromUnix(t.Unix())
	return nil
}

func (d DayTime) String() string {
	return d.Time.Time().UTC().Format("2006-01-02")
}

func (d DayTime) Inc() DayTime {
	return DayTime{d.Add(ObjectStorageIndexRequiredPeriod)}
}

func (d DayTime) Dec() DayTime {
	return DayTime{d.Add(-ObjectStorageIndexRequiredPeriod)}
}

func (d DayTime) Before(other DayTime) bool {
	return d.Time.Before(other.Time)
}

func (d DayTime) After(other DayTime) bool {
	return d.Time.After(other.Time)
}

func (d DayTime) ModelTime() model.Time {
	return d.Time
}

func (d DayTime) Bounds() (model.Time, model.Time) {
	return d.Time, d.Inc().Time
}

type DayTable struct {
	DayTime
	Prefix string
}

func (d DayTable) String() string {
	return d.Addr()
}

func NewDayTable(d DayTime, prefix string) DayTable {
	return DayTable{
		DayTime: d,
		Prefix:  prefix,
	}
}

// Addr returns the prefix (if any) and the unix day offset as a string, which is used
// as the address for the index table in storage.
func (d DayTable) Addr() string {
	return fmt.Sprintf("%s%d",
		d.Prefix,
		d.ModelTime().Time().UnixNano()/int64(ObjectStorageIndexRequiredPeriod))
}

// SchemaConfig contains the config for our chunk index schemas
type SchemaConfig struct {
	Configs []PeriodConfig `yaml:"configs"`

	fileName string
}

func (cfg *SchemaConfig) Clone() SchemaConfig {
	clone := *cfg
	clone.Configs = make([]PeriodConfig, len(cfg.Configs))
	copy(clone.Configs, cfg.Configs)
	return clone
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
	if cfg.Configs[activePCIndex].IndexType == types.BoltDBShipperType &&
		cfg.Configs[activePCIndex].IndexTables.Period != ObjectStorageIndexRequiredPeriod && len(cfg.Configs)-1 == activePCIndex {
		return errCurrentBoltdbShipperNon24Hours
	}

	// if upcoming index type is boltdb-shipper, it should always be set to 24 hours.
	if len(cfg.Configs)-1 > activePCIndex && (cfg.Configs[activePCIndex+1].IndexType == types.BoltDBShipperType &&
		cfg.Configs[activePCIndex+1].IndexTables.Period != ObjectStorageIndexRequiredPeriod) {
		return errUpcomingBoltdbShipperNon24Hours
	}

	for i := range cfg.Configs {
		periodCfg := &cfg.Configs[i]
		periodCfg.applyDefaults()
		if err := periodCfg.validate(); err != nil {
			return fmt.Errorf("validating period_config: %w", err)
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

func usingForPeriodConfigs(configs []PeriodConfig, fn func(string) bool) bool {
	activePCIndex := ActivePeriodConfig(configs)

	if fn(configs[activePCIndex].IndexType) ||
		(len(configs)-1 > activePCIndex && fn(configs[activePCIndex+1].IndexType)) {
		return true
	}

	return false
}

// IsObjectStorageIndex returns true if the index type is either boltdb-shipper or tsdb.
func IsObjectStorageIndex(indexType string) bool {
	return indexType == types.BoltDBShipperType || indexType == types.TSDBType
}

// UsingObjectStorageIndex returns true if the current or any of the upcoming periods
// use an object store index.
func UsingObjectStorageIndex(configs []PeriodConfig) bool {
	return usingForPeriodConfigs(configs, IsObjectStorageIndex)
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
	if cfg.IndexTables.PathPrefix == "" {
		cfg.IndexTables.PathPrefix = "index/"
	}

	if cfg.RowShards == 0 {
		cfg.RowShards = defaultRowShards(cfg.Schema)
	}
}

// ChunkFormat returns chunk format including it's headBlockFormat corresponding to the `schema` version
// in the given `PeriodConfig`.
func (cfg *PeriodConfig) ChunkFormat() (byte, chunkenc.HeadBlockFmt, error) {
	sver, err := cfg.VersionAsInt()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get chunk format: %w", err)
	}

	switch {
	case sver <= 12:
		return chunkenc.ChunkFormatV3, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV3), nil
	default: // for v13 and above
		return chunkenc.ChunkFormatV4, chunkenc.ChunkHeadFormatFor(chunkenc.ChunkFormatV4), nil
	}
}

// TSDBFormat returns index format corresponding to the `schema` version
// in the given `PeriodConfig`.
func (cfg *PeriodConfig) TSDBFormat() (int, error) {
	sver, err := cfg.VersionAsInt()
	if err != nil {
		return 0, fmt.Errorf("failed to get index format: %w", err)
	}

	switch {
	case sver <= 12:
		return index.FormatV2, nil
	default: // for v13 and above
		return index.FormatV3, nil
	}
}

// Validate the period config.
func (cfg PeriodConfig) validate() error {
	validateError := validateChunks(cfg)
	if validateError != nil {
		return validateError
	}

	if cfg.IndexType == types.TSDBType && cfg.IndexTables.Period != ObjectStorageIndexRequiredPeriod {
		return errTSDBNon24HoursIndexPeriod
	}

	if err := cfg.IndexTables.Validate(); err != nil {
		return fmt.Errorf("validating index tables: %w", err)
	}

	if err := cfg.ChunkTables.Validate(); err != nil {
		return fmt.Errorf("validating chunk tables: %w", err)
	}

	v, err := cfg.VersionAsInt()
	if err != nil {
		return err
	}

	switch v {
	case 10, 11, 12, 13:
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
	if err != nil {
		err = fmt.Errorf("invalid schema version: %w", err)
	}
	cfg.schemaInt = &n
	return n, err
}

type IndexPeriodicTableConfig struct {
	PathPrefix          string `yaml:"path_prefix" doc:"default=index/|description=Path prefix for index tables. Prefix always needs to end with a path delimiter '/', except when the prefix is empty."`
	PeriodicTableConfig `yaml:",inline"`
}

func (cfg *IndexPeriodicTableConfig) Validate() error {
	if err := cfg.PeriodicTableConfig.Validate(); err != nil {
		return err
	}

	return ValidatePathPrefix(cfg.PathPrefix)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *IndexPeriodicTableConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	g := struct {
		PathPrefix string         `yaml:"path_prefix"`
		Prefix     string         `yaml:"prefix"`
		Period     model.Duration `yaml:"period"`
		Tags       Tags           `yaml:"tags"`
	}{}
	if err := unmarshal(&g); err != nil {
		return err
	}

	cfg.PathPrefix = g.PathPrefix
	cfg.Prefix = g.Prefix
	cfg.Period = time.Duration(g.Period)
	cfg.Tags = g.Tags

	return nil
}

// MarshalYAML implements the yaml.Marshaler interface.
func (cfg IndexPeriodicTableConfig) MarshalYAML() (interface{}, error) {
	g := &struct {
		PathPrefix string         `yaml:"path_prefix"`
		Prefix     string         `yaml:"prefix"`
		Period     model.Duration `yaml:"period"`
		Tags       Tags           `yaml:"tags"`
	}{
		PathPrefix: cfg.PathPrefix,
		Prefix:     cfg.Prefix,
		Period:     model.Duration(cfg.Period),
		Tags:       cfg.Tags,
	}

	return g, nil
}

func ValidatePathPrefix(prefix string) error {
	if prefix == "" {
		return errors.New("prefix must be set")
	} else if strings.Contains(prefix, "\\") {
		// When using windows filesystem as object store the implementation of ObjectClient in Cortex takes care of conversion of separator.
		// We just need to always use `/` as a path separator.
		return fmt.Errorf("prefix should only have '%s' as a path separator", pathPrefixDelimiter)
	} else if strings.HasPrefix(prefix, pathPrefixDelimiter) {
		return errors.New("prefix should never start with a path separator i.e '/'")
	} else if !strings.HasSuffix(prefix, pathPrefixDelimiter) {
		return errors.New("prefix should end with a path separator i.e '/'")
	}

	return nil
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

func (cfg PeriodicTableConfig) Validate() error {
	// Ensure the tables period is a multiple of the bucket period
	if cfg.Period > 0 && cfg.Period%(24*time.Hour) != 0 {
		return errInvalidTablePeriod
	}

	return nil
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

func GetIndexStoreTableRanges(indexType string, periodicConfigs []PeriodConfig) TableRanges {
	var ranges TableRanges
	for i := range periodicConfigs {
		if periodicConfigs[i].IndexType != indexType {
			continue
		}

		periodEndTime := DayTime{Time: math.MaxInt64}
		if i < len(periodicConfigs)-1 {
			periodEndTime = DayTime{Time: periodicConfigs[i+1].From.Time.Add(-time.Millisecond)}
		}

		ranges = append(ranges, periodicConfigs[i].GetIndexTableNumberRange(periodEndTime))
	}

	return ranges
}
