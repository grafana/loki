package parquet

import (
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/parquet-go/parquet-go/compress"
)

// ReadMode is an enum that is used to configure the way that a File reads pages.
type ReadMode int

const (
	ReadModeSync  ReadMode = iota // ReadModeSync reads pages synchronously on demand (Default).
	ReadModeAsync                 // ReadModeAsync reads pages asynchronously in the background.
)

const (
	DefaultColumnIndexSizeLimit = 16
	DefaultColumnBufferCapacity = 16 * 1024
	DefaultPageBufferSize       = 256 * 1024
	DefaultWriteBufferSize      = 32 * 1024
	DefaultDataPageVersion      = 2
	DefaultDataPageStatistics   = false
	DefaultSkipMagicBytes       = false
	DefaultSkipPageIndex        = false
	DefaultSkipBloomFilters     = false
	DefaultMaxRowsPerRowGroup   = math.MaxInt64
	DefaultReadMode             = ReadModeSync
)

const (
	parquetGoModulePath = "github.com/parquet-go/parquet-go"
)

var (
	defaultCreatedByInfo string
	defaultCreatedByOnce sync.Once
)

func defaultCreatedBy() string {
	defaultCreatedByOnce.Do(func() {
		createdBy := parquetGoModulePath
		build, ok := debug.ReadBuildInfo()
		if ok {
			for _, mod := range build.Deps {
				if mod.Replace == nil && mod.Path == parquetGoModulePath {
					semver, _, buildsha := parseModuleVersion(mod.Version)
					createdBy = formatCreatedBy(createdBy, semver, buildsha)
					break
				}
			}
		}
		defaultCreatedByInfo = createdBy
	})
	return defaultCreatedByInfo
}

func parseModuleVersion(version string) (semver, datetime, buildsha string) {
	semver, version = splitModuleVersion(version)
	datetime, version = splitModuleVersion(version)
	buildsha, _ = splitModuleVersion(version)
	semver = strings.TrimPrefix(semver, "v")
	return
}

func splitModuleVersion(s string) (head, tail string) {
	if i := strings.IndexByte(s, '-'); i < 0 {
		head = s
	} else {
		head, tail = s[:i], s[i+1:]
	}
	return
}

func formatCreatedBy(application, version, build string) string {
	return application + " version " + version + "(build " + build + ")"
}

// The FileConfig type carries configuration options for parquet files.
//
// FileConfig implements the FileOption interface so it can be used directly
// as argument to the OpenFile function when needed, for example:
//
//	f, err := parquet.OpenFile(reader, size, &parquet.FileConfig{
//		SkipPageIndex:    true,
//		SkipBloomFilters: true,
//		ReadMode:         ReadModeAsync,
//	})
type FileConfig struct {
	SkipMagicBytes   bool
	SkipPageIndex    bool
	SkipBloomFilters bool
	OptimisticRead   bool
	ReadBufferSize   int
	ReadMode         ReadMode
	Schema           *Schema
}

// DefaultFileConfig returns a new FileConfig value initialized with the
// default file configuration.
func DefaultFileConfig() *FileConfig {
	return &FileConfig{
		SkipMagicBytes:   DefaultSkipMagicBytes,
		SkipPageIndex:    DefaultSkipPageIndex,
		SkipBloomFilters: DefaultSkipBloomFilters,
		ReadBufferSize:   defaultReadBufferSize,
		ReadMode:         DefaultReadMode,
		Schema:           nil,
	}
}

// NewFileConfig constructs a new file configuration applying the options passed
// as arguments.
//
// The function returns an non-nil error if some of the options carried invalid
// configuration values.
func NewFileConfig(options ...FileOption) (*FileConfig, error) {
	config := DefaultFileConfig()
	config.Apply(options...)
	return config, config.Validate()
}

// Apply applies the given list of options to c.
func (c *FileConfig) Apply(options ...FileOption) {
	for _, opt := range options {
		opt.ConfigureFile(c)
	}
}

// ConfigureFile applies configuration options from c to config.
func (c *FileConfig) ConfigureFile(config *FileConfig) {
	*config = FileConfig{
		SkipMagicBytes:   c.SkipMagicBytes,
		SkipPageIndex:    c.SkipPageIndex,
		SkipBloomFilters: c.SkipBloomFilters,
		ReadBufferSize:   coalesceInt(c.ReadBufferSize, config.ReadBufferSize),
		ReadMode:         ReadMode(coalesceInt(int(c.ReadMode), int(config.ReadMode))),
		Schema:           coalesceSchema(c.Schema, config.Schema),
	}
}

// Validate returns a non-nil error if the configuration of c is invalid.
func (c *FileConfig) Validate() error {
	return nil
}

// The ReaderConfig type carries configuration options for parquet readers.
//
// ReaderConfig implements the ReaderOption interface so it can be used directly
// as argument to the NewReader function when needed, for example:
//
//	reader := parquet.NewReader(output, schema, &parquet.ReaderConfig{
//		// ...
//	})
type ReaderConfig struct {
	Schema *Schema
}

// DefaultReaderConfig returns a new ReaderConfig value initialized with the
// default reader configuration.
func DefaultReaderConfig() *ReaderConfig {
	return &ReaderConfig{}
}

// NewReaderConfig constructs a new reader configuration applying the options
// passed as arguments.
//
// The function returns an non-nil error if some of the options carried invalid
// configuration values.
func NewReaderConfig(options ...ReaderOption) (*ReaderConfig, error) {
	config := DefaultReaderConfig()
	config.Apply(options...)
	return config, config.Validate()
}

// Apply applies the given list of options to c.
func (c *ReaderConfig) Apply(options ...ReaderOption) {
	for _, opt := range options {
		opt.ConfigureReader(c)
	}
}

// ConfigureReader applies configuration options from c to config.
func (c *ReaderConfig) ConfigureReader(config *ReaderConfig) {
	*config = ReaderConfig{
		Schema: coalesceSchema(c.Schema, config.Schema),
	}
}

// Validate returns a non-nil error if the configuration of c is invalid.
func (c *ReaderConfig) Validate() error {
	return nil
}

// The WriterConfig type carries configuration options for parquet writers.
//
// WriterConfig implements the WriterOption interface so it can be used directly
// as argument to the NewWriter function when needed, for example:
//
//	writer := parquet.NewWriter(output, schema, &parquet.WriterConfig{
//		CreatedBy: "my test program",
//	})
type WriterConfig struct {
	CreatedBy            string
	ColumnPageBuffers    BufferPool
	ColumnIndexSizeLimit int
	PageBufferSize       int
	WriteBufferSize      int
	DataPageVersion      int
	DataPageStatistics   bool
	MaxRowsPerRowGroup   int64
	KeyValueMetadata     map[string]string
	Schema               *Schema
	BloomFilters         []BloomFilterColumn
	Compression          compress.Codec
	Sorting              SortingConfig
	SkipPageBounds       [][]string
}

// DefaultWriterConfig returns a new WriterConfig value initialized with the
// default writer configuration.
func DefaultWriterConfig() *WriterConfig {
	return &WriterConfig{
		CreatedBy:            defaultCreatedBy(),
		ColumnPageBuffers:    &defaultColumnBufferPool,
		ColumnIndexSizeLimit: DefaultColumnIndexSizeLimit,
		PageBufferSize:       DefaultPageBufferSize,
		WriteBufferSize:      DefaultWriteBufferSize,
		DataPageVersion:      DefaultDataPageVersion,
		DataPageStatistics:   DefaultDataPageStatistics,
		MaxRowsPerRowGroup:   DefaultMaxRowsPerRowGroup,
		Sorting: SortingConfig{
			SortingBuffers: &defaultSortingBufferPool,
		},
	}
}

// NewWriterConfig constructs a new writer configuration applying the options
// passed as arguments.
//
// The function returns an non-nil error if some of the options carried invalid
// configuration values.
func NewWriterConfig(options ...WriterOption) (*WriterConfig, error) {
	config := DefaultWriterConfig()
	config.Apply(options...)
	return config, config.Validate()
}

// Apply applies the given list of options to c.
func (c *WriterConfig) Apply(options ...WriterOption) {
	for _, opt := range options {
		opt.ConfigureWriter(c)
	}
}

// ConfigureWriter applies configuration options from c to config.
func (c *WriterConfig) ConfigureWriter(config *WriterConfig) {
	keyValueMetadata := config.KeyValueMetadata
	if len(c.KeyValueMetadata) > 0 {
		if keyValueMetadata == nil {
			keyValueMetadata = make(map[string]string, len(c.KeyValueMetadata))
		}
		for k, v := range c.KeyValueMetadata {
			keyValueMetadata[k] = v
		}
	}

	*config = WriterConfig{
		CreatedBy:            coalesceString(c.CreatedBy, config.CreatedBy),
		ColumnPageBuffers:    coalesceBufferPool(c.ColumnPageBuffers, config.ColumnPageBuffers),
		ColumnIndexSizeLimit: coalesceInt(c.ColumnIndexSizeLimit, config.ColumnIndexSizeLimit),
		PageBufferSize:       coalesceInt(c.PageBufferSize, config.PageBufferSize),
		WriteBufferSize:      coalesceInt(c.WriteBufferSize, config.WriteBufferSize),
		DataPageVersion:      coalesceInt(c.DataPageVersion, config.DataPageVersion),
		DataPageStatistics:   coalesceBool(c.DataPageStatistics, config.DataPageStatistics),
		MaxRowsPerRowGroup:   coalesceInt64(c.MaxRowsPerRowGroup, config.MaxRowsPerRowGroup),
		KeyValueMetadata:     keyValueMetadata,
		Schema:               coalesceSchema(c.Schema, config.Schema),
		BloomFilters:         coalesceBloomFilters(c.BloomFilters, config.BloomFilters),
		Compression:          coalesceCompression(c.Compression, config.Compression),
		Sorting:              coalesceSortingConfig(c.Sorting, config.Sorting),
	}
}

// Validate returns a non-nil error if the configuration of c is invalid.
func (c *WriterConfig) Validate() error {
	const baseName = "parquet.(*WriterConfig)."
	return errorInvalidConfiguration(
		validateNotNil(baseName+"ColumnPageBuffers", c.ColumnPageBuffers),
		validatePositiveInt(baseName+"ColumnIndexSizeLimit", c.ColumnIndexSizeLimit),
		validatePositiveInt(baseName+"PageBufferSize", c.PageBufferSize),
		validateOneOfInt(baseName+"DataPageVersion", c.DataPageVersion, 1, 2),
		c.Sorting.Validate(),
	)
}

// The RowGroupConfig type carries configuration options for parquet row groups.
//
// RowGroupConfig implements the RowGroupOption interface so it can be used
// directly as argument to the NewBuffer function when needed, for example:
//
//	buffer := parquet.NewBuffer(&parquet.RowGroupConfig{
//		ColumnBufferCapacity: 10_000,
//	})
type RowGroupConfig struct {
	ColumnBufferCapacity int
	Schema               *Schema
	Sorting              SortingConfig
}

// DefaultRowGroupConfig returns a new RowGroupConfig value initialized with the
// default row group configuration.
func DefaultRowGroupConfig() *RowGroupConfig {
	return &RowGroupConfig{
		ColumnBufferCapacity: DefaultColumnBufferCapacity,
		Sorting: SortingConfig{
			SortingBuffers: &defaultSortingBufferPool,
		},
	}
}

// NewRowGroupConfig constructs a new row group configuration applying the
// options passed as arguments.
//
// The function returns an non-nil error if some of the options carried invalid
// configuration values.
func NewRowGroupConfig(options ...RowGroupOption) (*RowGroupConfig, error) {
	config := DefaultRowGroupConfig()
	config.Apply(options...)
	return config, config.Validate()
}

// Validate returns a non-nil error if the configuration of c is invalid.
func (c *RowGroupConfig) Validate() error {
	const baseName = "parquet.(*RowGroupConfig)."
	return errorInvalidConfiguration(
		validatePositiveInt(baseName+"ColumnBufferCapacity", c.ColumnBufferCapacity),
		c.Sorting.Validate(),
	)
}

func (c *RowGroupConfig) Apply(options ...RowGroupOption) {
	for _, opt := range options {
		opt.ConfigureRowGroup(c)
	}
}

func (c *RowGroupConfig) ConfigureRowGroup(config *RowGroupConfig) {
	*config = RowGroupConfig{
		ColumnBufferCapacity: coalesceInt(c.ColumnBufferCapacity, config.ColumnBufferCapacity),
		Schema:               coalesceSchema(c.Schema, config.Schema),
		Sorting:              coalesceSortingConfig(c.Sorting, config.Sorting),
	}
}

// The SortingConfig type carries configuration options for parquet row groups.
//
// SortingConfig implements the SortingOption interface so it can be used
// directly as argument to the NewSortingWriter function when needed,
// for example:
//
//	buffer := parquet.NewSortingWriter[Row](
//		parquet.SortingWriterConfig(
//			parquet.DropDuplicatedRows(true),
//		),
//	})
type SortingConfig struct {
	SortingBuffers     BufferPool
	SortingColumns     []SortingColumn
	DropDuplicatedRows bool
}

// DefaultSortingConfig returns a new SortingConfig value initialized with the
// default row group configuration.
func DefaultSortingConfig() *SortingConfig {
	return &SortingConfig{
		SortingBuffers: &defaultSortingBufferPool,
	}
}

// NewSortingConfig constructs a new sorting configuration applying the
// options passed as arguments.
//
// The function returns an non-nil error if some of the options carried invalid
// configuration values.
func NewSortingConfig(options ...SortingOption) (*SortingConfig, error) {
	config := DefaultSortingConfig()
	config.Apply(options...)
	return config, config.Validate()
}

func (c *SortingConfig) Validate() error {
	const baseName = "parquet.(*SortingConfig)."
	return errorInvalidConfiguration(
		validateNotNil(baseName+"SortingBuffers", c.SortingBuffers),
	)
}

func (c *SortingConfig) Apply(options ...SortingOption) {
	for _, opt := range options {
		opt.ConfigureSorting(c)
	}
}

func (c *SortingConfig) ConfigureSorting(config *SortingConfig) {
	*config = coalesceSortingConfig(*c, *config)
}

// FileOption is an interface implemented by types that carry configuration
// options for parquet files.
type FileOption interface {
	ConfigureFile(*FileConfig)
}

// ReaderOption is an interface implemented by types that carry configuration
// options for parquet readers.
type ReaderOption interface {
	ConfigureReader(*ReaderConfig)
}

// WriterOption is an interface implemented by types that carry configuration
// options for parquet writers.
type WriterOption interface {
	ConfigureWriter(*WriterConfig)
}

// RowGroupOption is an interface implemented by types that carry configuration
// options for parquet row groups.
type RowGroupOption interface {
	ConfigureRowGroup(*RowGroupConfig)
}

// SortingOption is an interface implemented by types that carry configuration
// options for parquet sorting writers.
type SortingOption interface {
	ConfigureSorting(*SortingConfig)
}

// SkipMagicBytes is a file configuration option which prevents automatically
// reading the magic bytes when opening a parquet file, when set to true. This
// is useful as an optimization when programs can trust that they are dealing
// with parquet files and do not need to verify the first 4 bytes.
func SkipMagicBytes(skip bool) FileOption {
	return fileOption(func(config *FileConfig) { config.SkipMagicBytes = skip })
}

// SkipPageIndex is a file configuration option which prevents automatically
// reading the page index when opening a parquet file, when set to true. This is
// useful as an optimization when programs know that they will not need to
// consume the page index.
//
// Defaults to false.
func SkipPageIndex(skip bool) FileOption {
	return fileOption(func(config *FileConfig) { config.SkipPageIndex = skip })
}

// SkipBloomFilters is a file configuration option which prevents automatically
// reading the bloom filters when opening a parquet file, when set to true.
// This is useful as an optimization when programs know that they will not need
// to consume the bloom filters.
//
// Defaults to false.
func SkipBloomFilters(skip bool) FileOption {
	return fileOption(func(config *FileConfig) { config.SkipBloomFilters = skip })
}

// OptimisticRead configures a file to optimistically perform larger buffered
// reads to improve performance. This is useful when reading from remote storage
// and amortize the cost of network round trips.
//
// This is an option instead of enabled by default because dependents of this
// package have historically relied on the read patterns to provide external
// caches and achieve similar results (e.g., Tempo).
func OptimisticRead(enabled bool) FileOption {
	return fileOption(func(config *FileConfig) { config.OptimisticRead = enabled })
}

// FileReadMode is a file configuration option which controls the way pages
// are read. Currently the only two options are ReadModeAsync and ReadModeSync
// which control whether or not pages are loaded asynchronously. It can be
// advantageous to use ReadModeAsync if your reader is backed by network
// storage.
//
// Defaults to ReadModeSync.
func FileReadMode(mode ReadMode) FileOption {
	return fileOption(func(config *FileConfig) { config.ReadMode = mode })
}

// ReadBufferSize is a file configuration option which controls the default
// buffer sizes for reads made to the provided io.Reader. The default of 4096
// is appropriate for disk based access but if your reader is backed by network
// storage it can be advantageous to increase this value to something more like
// 4 MiB.
//
// Defaults to 4096.
func ReadBufferSize(size int) FileOption {
	return fileOption(func(config *FileConfig) { config.ReadBufferSize = size })
}

// FileSchema is used to pass a known schema in while opening a Parquet file.
// This optimization is only useful if your application is currently opening
// an extremely large number of parquet files with the same, known schema.
//
// Defaults to nil.
func FileSchema(schema *Schema) FileOption {
	return fileOption(func(config *FileConfig) { config.Schema = schema })
}

// PageBufferSize configures the size of column page buffers on parquet writers.
//
// Note that the page buffer size refers to the in-memory buffers where pages
// are generated, not the size of pages after encoding and compression.
// This design choice was made to help control the amount of memory needed to
// read and write pages rather than controlling the space used by the encoded
// representation on disk.
//
// Defaults to 256KiB.
func PageBufferSize(size int) WriterOption {
	return writerOption(func(config *WriterConfig) { config.PageBufferSize = size })
}

// WriteBufferSize configures the size of the write buffer.
//
// Setting the writer buffer size to zero deactivates buffering, all writes are
// immediately sent to the output io.Writer.
//
// Defaults to 32KiB.
func WriteBufferSize(size int) WriterOption {
	return writerOption(func(config *WriterConfig) { config.WriteBufferSize = size })
}

// MaxRowsPerRowGroup configures the maximum number of rows that a writer will
// produce in each row group.
//
// This limit is useful to control size of row groups in both number of rows and
// byte size. While controlling the byte size of a row group is difficult to
// achieve with parquet due to column encoding and compression, the number of
// rows remains a useful proxy.
//
// Defaults to unlimited.
func MaxRowsPerRowGroup(numRows int64) WriterOption {
	if numRows <= 0 {
		numRows = DefaultMaxRowsPerRowGroup
	}
	return writerOption(func(config *WriterConfig) { config.MaxRowsPerRowGroup = numRows })
}

// CreatedBy creates a configuration option which sets the name of the
// application that created a parquet file.
//
// The option formats the "CreatedBy" file metadata according to the convention
// described by the parquet spec:
//
//	"<application> version <version> (build <build>)"
//
// By default, the option is set to the parquet-go module name, version, and
// build hash.
func CreatedBy(application, version, build string) WriterOption {
	createdBy := formatCreatedBy(application, version, build)
	return writerOption(func(config *WriterConfig) { config.CreatedBy = createdBy })
}

// ColumnPageBuffers creates a configuration option to customize the buffer pool
// used when constructing row groups. This can be used to provide on-disk buffers
// as swap space to ensure that the parquet file creation will no be bottlenecked
// on the amount of memory available.
//
// Defaults to using in-memory buffers.
func ColumnPageBuffers(buffers BufferPool) WriterOption {
	return writerOption(func(config *WriterConfig) { config.ColumnPageBuffers = buffers })
}

// ColumnIndexSizeLimit creates a configuration option to customize the size
// limit of page boundaries recorded in column indexes.
//
// Defaults to 16.
func ColumnIndexSizeLimit(sizeLimit int) WriterOption {
	return writerOption(func(config *WriterConfig) { config.ColumnIndexSizeLimit = sizeLimit })
}

// DataPageVersion creates a configuration option which configures the version of
// data pages used when creating a parquet file.
//
// Defaults to version 2.
func DataPageVersion(version int) WriterOption {
	return writerOption(func(config *WriterConfig) { config.DataPageVersion = version })
}

// DataPageStatistics creates a configuration option which defines whether data
// page statistics are emitted. This option is useful when generating parquet
// files that intend to be backward compatible with older readers which may not
// have the ability to load page statistics from the column index.
//
// Defaults to false.
func DataPageStatistics(enabled bool) WriterOption {
	return writerOption(func(config *WriterConfig) { config.DataPageStatistics = enabled })
}

// KeyValueMetadata creates a configuration option which adds key/value metadata
// to add to the metadata of parquet files.
//
// This option is additive, it may be used multiple times to add more than one
// key/value pair.
//
// Keys are assumed to be unique, if the same key is repeated multiple times the
// last value is retained. While the parquet format does not require unique keys,
// this design decision was made to optimize for the most common use case where
// applications leverage this extension mechanism to associate single values to
// keys. This may create incompatibilities with other parquet libraries, or may
// cause some key/value pairs to be lost when open parquet files written with
// repeated keys. We can revisit this decision if it ever becomes a blocker.
func KeyValueMetadata(key, value string) WriterOption {
	return writerOption(func(config *WriterConfig) {
		if config.KeyValueMetadata == nil {
			config.KeyValueMetadata = map[string]string{key: value}
		} else {
			config.KeyValueMetadata[key] = value
		}
	})
}

// BloomFilters creates a configuration option which defines the bloom filters
// that parquet writers should generate.
//
// The compute and memory footprint of generating bloom filters for all columns
// of a parquet schema can be significant, so by default no filters are created
// and applications need to explicitly declare the columns that they want to
// create filters for.
func BloomFilters(filters ...BloomFilterColumn) WriterOption {
	filters = append([]BloomFilterColumn{}, filters...)
	return writerOption(func(config *WriterConfig) { config.BloomFilters = filters })
}

// Compression creates a configuration option which sets the default compression
// codec used by a writer for columns where none were defined.
func Compression(codec compress.Codec) WriterOption {
	return writerOption(func(config *WriterConfig) { config.Compression = codec })
}

// SortingWriterConfig is a writer option which applies configuration specific
// to sorting writers.
func SortingWriterConfig(options ...SortingOption) WriterOption {
	options = append([]SortingOption{}, options...)
	return writerOption(func(config *WriterConfig) { config.Sorting.Apply(options...) })
}

// SkipPageBounds lists the path to a column that shouldn't have bounds written to the
// footer of the parquet file. This is useful for data blobs, like a raw html file,
// where the bounds are not meaningful.
//
// This option is additive, it may be used multiple times to skip multiple columns.
func SkipPageBounds(path ...string) WriterOption {
	return writerOption(func(config *WriterConfig) { config.SkipPageBounds = append(config.SkipPageBounds, path) })
}

// ColumnBufferCapacity creates a configuration option which defines the size of
// row group column buffers.
//
// Defaults to 16384.
func ColumnBufferCapacity(size int) RowGroupOption {
	return rowGroupOption(func(config *RowGroupConfig) { config.ColumnBufferCapacity = size })
}

// SortingRowGroupConfig is a row group option which applies configuration
// specific sorting row groups.
func SortingRowGroupConfig(options ...SortingOption) RowGroupOption {
	options = append([]SortingOption{}, options...)
	return rowGroupOption(func(config *RowGroupConfig) { config.Sorting.Apply(options...) })
}

// SortingColumns creates a configuration option which defines the sorting order
// of columns in a row group.
//
// The order of sorting columns passed as argument defines the ordering
// hierarchy; when elements are equal in the first column, the second column is
// used to order rows, etc...
func SortingColumns(columns ...SortingColumn) SortingOption {
	// Make a copy so that we do not retain the input slice generated implicitly
	// for the variable argument list, and also avoid having a nil slice when
	// the option is passed with no sorting columns, so we can differentiate it
	// from it not being passed.
	columns = append([]SortingColumn{}, columns...)
	return sortingOption(func(config *SortingConfig) { config.SortingColumns = columns })
}

// SortingBuffers creates a configuration option which sets the pool of buffers
// used to hold intermediary state when sorting parquet rows.
//
// Defaults to using in-memory buffers.
func SortingBuffers(buffers BufferPool) SortingOption {
	return sortingOption(func(config *SortingConfig) { config.SortingBuffers = buffers })
}

// DropDuplicatedRows configures whether a sorting writer will keep or remove
// duplicated rows.
//
// Two rows are considered duplicates if the values of their all their sorting
// columns are equal.
//
// Defaults to false
func DropDuplicatedRows(drop bool) SortingOption {
	return sortingOption(func(config *SortingConfig) { config.DropDuplicatedRows = drop })
}

type fileOption func(*FileConfig)

func (opt fileOption) ConfigureFile(config *FileConfig) { opt(config) }

type readerOption func(*ReaderConfig)

func (opt readerOption) ConfigureReader(config *ReaderConfig) { opt(config) }

type writerOption func(*WriterConfig)

func (opt writerOption) ConfigureWriter(config *WriterConfig) { opt(config) }

type rowGroupOption func(*RowGroupConfig)

func (opt rowGroupOption) ConfigureRowGroup(config *RowGroupConfig) { opt(config) }

type sortingOption func(*SortingConfig)

func (opt sortingOption) ConfigureSorting(config *SortingConfig) { opt(config) }

func coalesceBool(i1, i2 bool) bool {
	return i1 || i2
}

func coalesceInt(i1, i2 int) int {
	if i1 != 0 {
		return i1
	}
	return i2
}

func coalesceInt64(i1, i2 int64) int64 {
	if i1 != 0 {
		return i1
	}
	return i2
}

func coalesceString(s1, s2 string) string {
	if s1 != "" {
		return s1
	}
	return s2
}

func coalesceBytes(b1, b2 []byte) []byte {
	if b1 != nil {
		return b1
	}
	return b2
}

func coalesceBufferPool(p1, p2 BufferPool) BufferPool {
	if p1 != nil {
		return p1
	}
	return p2
}

func coalesceSchema(s1, s2 *Schema) *Schema {
	if s1 != nil {
		return s1
	}
	return s2
}

func coalesceSortingColumns(s1, s2 []SortingColumn) []SortingColumn {
	if s1 != nil {
		return s1
	}
	return s2
}

func coalesceSortingConfig(c1, c2 SortingConfig) SortingConfig {
	return SortingConfig{
		SortingBuffers:     coalesceBufferPool(c1.SortingBuffers, c2.SortingBuffers),
		SortingColumns:     coalesceSortingColumns(c1.SortingColumns, c2.SortingColumns),
		DropDuplicatedRows: c1.DropDuplicatedRows,
	}
}

func coalesceBloomFilters(f1, f2 []BloomFilterColumn) []BloomFilterColumn {
	if f1 != nil {
		return f1
	}
	return f2
}

func coalesceCompression(c1, c2 compress.Codec) compress.Codec {
	if c1 != nil {
		return c1
	}
	return c2
}

func validatePositiveInt(optionName string, optionValue int) error {
	if optionValue > 0 {
		return nil
	}
	return errorInvalidOptionValue(optionName, optionValue)
}

func validatePositiveInt64(optionName string, optionValue int64) error {
	if optionValue > 0 {
		return nil
	}
	return errorInvalidOptionValue(optionName, optionValue)
}

func validateOneOfInt(optionName string, optionValue int, supportedValues ...int) error {
	for _, value := range supportedValues {
		if value == optionValue {
			return nil
		}
	}
	return errorInvalidOptionValue(optionName, optionValue)
}

func validateNotNil(optionName string, optionValue interface{}) error {
	if optionValue != nil {
		return nil
	}
	return errorInvalidOptionValue(optionName, optionValue)
}

func errorInvalidOptionValue(optionName string, optionValue interface{}) error {
	return fmt.Errorf("invalid option value: %s: %v", optionName, optionValue)
}

func errorInvalidConfiguration(reasons ...error) error {
	var err *invalidConfiguration

	for _, reason := range reasons {
		if reason != nil {
			if err == nil {
				err = new(invalidConfiguration)
			}
			err.reasons = append(err.reasons, reason)
		}
	}

	if err != nil {
		return err
	}

	return nil
}

type invalidConfiguration struct {
	reasons []error
}

func (err *invalidConfiguration) Error() string {
	errorMessage := new(strings.Builder)
	for _, reason := range err.reasons {
		errorMessage.WriteString(reason.Error())
		errorMessage.WriteString("\n")
	}
	errorString := errorMessage.String()
	if errorString != "" {
		errorString = errorString[:len(errorString)-1]
	}
	return errorString
}

var (
	_ FileOption     = (*FileConfig)(nil)
	_ ReaderOption   = (*ReaderConfig)(nil)
	_ WriterOption   = (*WriterConfig)(nil)
	_ RowGroupOption = (*RowGroupConfig)(nil)
	_ SortingOption  = (*SortingConfig)(nil)
)
