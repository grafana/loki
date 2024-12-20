package format

import (
	"fmt"

	"github.com/parquet-go/parquet-go/deprecated"
)

// Types supported by Parquet. These types are intended to be used in combination
// with the encodings to control the on disk storage format. For example INT16
// is not included as a type since a good encoding of INT32 would handle this.
type Type int32

const (
	Boolean           Type = 0
	Int32             Type = 1
	Int64             Type = 2
	Int96             Type = 3 // deprecated, only used by legacy implementations.
	Float             Type = 4
	Double            Type = 5
	ByteArray         Type = 6
	FixedLenByteArray Type = 7
)

func (t Type) String() string {
	switch t {
	case Boolean:
		return "BOOLEAN"
	case Int32:
		return "INT32"
	case Int64:
		return "INT64"
	case Int96:
		return "INT96"
	case Float:
		return "FLOAT"
	case Double:
		return "DOUBLE"
	case ByteArray:
		return "BYTE_ARRAY"
	case FixedLenByteArray:
		return "FIXED_LEN_BYTE_ARRAY"
	default:
		return "Type(?)"
	}
}

// Representation of Schemas.
type FieldRepetitionType int32

const (
	// The field is required (can not be null) and each record has exactly 1 value.
	Required FieldRepetitionType = 0
	// The field is optional (can be null) and each record has 0 or 1 values.
	Optional FieldRepetitionType = 1
	// The field is repeated and can contain 0 or more values.
	Repeated FieldRepetitionType = 2
)

func (t FieldRepetitionType) String() string {
	switch t {
	case Required:
		return "REQUIRED"
	case Optional:
		return "OPTIONAL"
	case Repeated:
		return "REPEATED"
	default:
		return "FieldRepeationaType(?)"
	}
}

// Statistics per row group and per page.
// All fields are optional.
type Statistics struct {
	// DEPRECATED: min and max value of the column. Use min_value and max_value.
	//
	// Values are encoded using PLAIN encoding, except that variable-length byte
	// arrays do not include a length prefix.
	//
	// These fields encode min and max values determined by signed comparison
	// only. New files should use the correct order for a column's logical type
	// and store the values in the min_value and max_value fields.
	//
	// To support older readers, these may be set when the column order is
	// signed.
	Max []byte `thrift:"1"`
	Min []byte `thrift:"2"`
	// Count of null value in the column.
	NullCount int64 `thrift:"3"`
	// Count of distinct values occurring.
	DistinctCount int64 `thrift:"4"`
	// Min and max values for the column, determined by its ColumnOrder.
	//
	// Values are encoded using PLAIN encoding, except that variable-length byte
	// arrays do not include a length prefix.
	MaxValue []byte `thrift:"5"`
	MinValue []byte `thrift:"6"`
}

// Empty structs to use as logical type annotations.
type StringType struct{} // allowed for BINARY, must be encoded with UTF-8
type UUIDType struct{}   // allowed for FIXED[16], must encode raw UUID bytes
type MapType struct{}    // see see LogicalTypes.md
type ListType struct{}   // see LogicalTypes.md
type EnumType struct{}   // allowed for BINARY, must be encoded with UTF-8
type DateType struct{}   // allowed for INT32

func (*StringType) String() string { return "STRING" }
func (*UUIDType) String() string   { return "UUID" }
func (*MapType) String() string    { return "MAP" }
func (*ListType) String() string   { return "LIST" }
func (*EnumType) String() string   { return "ENUM" }
func (*DateType) String() string   { return "DATE" }

// Logical type to annotate a column that is always null.
//
// Sometimes when discovering the schema of existing data, values are always
// null and the physical type can't be determined. This annotation signals
// the case where the physical type was guessed from all null values.
type NullType struct{}

func (*NullType) String() string { return "NULL" }

// Decimal logical type annotation
//
// To maintain forward-compatibility in v1, implementations using this logical
// type must also set scale and precision on the annotated SchemaElement.
//
// Allowed for physical types: INT32, INT64, FIXED, and BINARY
type DecimalType struct {
	Scale     int32 `thrift:"1,required"`
	Precision int32 `thrift:"2,required"`
}

func (t *DecimalType) String() string {
	// Matching parquet-cli's decimal string format: https://github.com/apache/parquet-java/blob/d057b39d93014fe40f5067ee4a33621e65c91552/parquet-column/src/test/java/org/apache/parquet/parser/TestParquetParser.java#L249-L265
	return fmt.Sprintf("DECIMAL(%d,%d)", t.Precision, t.Scale)
}

// Time units for logical types.
type MilliSeconds struct{}
type MicroSeconds struct{}
type NanoSeconds struct{}

func (*MilliSeconds) String() string { return "MILLIS" }
func (*MicroSeconds) String() string { return "MICROS" }
func (*NanoSeconds) String() string  { return "NANOS" }

type TimeUnit struct { // union
	Millis *MilliSeconds `thrift:"1"`
	Micros *MicroSeconds `thrift:"2"`
	Nanos  *NanoSeconds  `thrift:"3"`
}

func (u *TimeUnit) String() string {
	switch {
	case u.Millis != nil:
		return u.Millis.String()
	case u.Micros != nil:
		return u.Micros.String()
	case u.Nanos != nil:
		return u.Nanos.String()
	default:
		return ""
	}
}

// Timestamp logical type annotation
//
// Allowed for physical types: INT64
type TimestampType struct {
	IsAdjustedToUTC bool     `thrift:"1,required"`
	Unit            TimeUnit `thrift:"2,required"`
}

func (t *TimestampType) String() string {
	return fmt.Sprintf("TIMESTAMP(isAdjustedToUTC=%t,unit=%s)", t.IsAdjustedToUTC, &t.Unit)
}

// Time logical type annotation
//
// Allowed for physical types: INT32 (millis), INT64 (micros, nanos)
type TimeType struct {
	IsAdjustedToUTC bool     `thrift:"1,required"`
	Unit            TimeUnit `thrift:"2,required"`
}

func (t *TimeType) String() string {
	return fmt.Sprintf("TIME(isAdjustedToUTC=%t,unit=%s)", t.IsAdjustedToUTC, &t.Unit)
}

// Integer logical type annotation
//
// bitWidth must be 8, 16, 32, or 64.
//
// Allowed for physical types: INT32, INT64
type IntType struct {
	BitWidth int8 `thrift:"1,required"`
	IsSigned bool `thrift:"2,required"`
}

func (t *IntType) String() string {
	return fmt.Sprintf("INT(%d,%t)", t.BitWidth, t.IsSigned)
}

// Embedded JSON logical type annotation
//
// Allowed for physical types: BINARY
type JsonType struct{}

func (t *JsonType) String() string { return "JSON" }

// Embedded BSON logical type annotation
//
// Allowed for physical types: BINARY
type BsonType struct{}

func (t *BsonType) String() string { return "BSON" }

// LogicalType annotations to replace ConvertedType.
//
// To maintain compatibility, implementations using LogicalType for a
// SchemaElement must also set the corresponding ConvertedType (if any)
// from the following table.
type LogicalType struct { // union
	UTF8    *StringType  `thrift:"1"` // use ConvertedType UTF8
	Map     *MapType     `thrift:"2"` // use ConvertedType Map
	List    *ListType    `thrift:"3"` // use ConvertedType List
	Enum    *EnumType    `thrift:"4"` // use ConvertedType Enum
	Decimal *DecimalType `thrift:"5"` // use ConvertedType Decimal + SchemaElement.{Scale, Precision}
	Date    *DateType    `thrift:"6"` // use ConvertedType Date

	// use ConvertedType TimeMicros for Time{IsAdjustedToUTC: *, Unit: Micros}
	// use ConvertedType TimeMillis for Time{IsAdjustedToUTC: *, Unit: Millis}
	Time *TimeType `thrift:"7"`

	// use ConvertedType TimestampMicros for Timestamp{IsAdjustedToUTC: *, Unit: Micros}
	// use ConvertedType TimestampMillis for Timestamp{IsAdjustedToUTC: *, Unit: Millis}
	Timestamp *TimestampType `thrift:"8"`

	// 9: reserved for Interval
	Integer *IntType  `thrift:"10"` // use ConvertedType Int* or Uint*
	Unknown *NullType `thrift:"11"` // no compatible ConvertedType
	Json    *JsonType `thrift:"12"` // use ConvertedType JSON
	Bson    *BsonType `thrift:"13"` // use ConvertedType BSON
	UUID    *UUIDType `thrift:"14"` // no compatible ConvertedType
}

func (t *LogicalType) String() string {
	switch {
	case t.UTF8 != nil:
		return t.UTF8.String()
	case t.Map != nil:
		return t.Map.String()
	case t.List != nil:
		return t.List.String()
	case t.Enum != nil:
		return t.Enum.String()
	case t.Decimal != nil:
		return t.Decimal.String()
	case t.Date != nil:
		return t.Date.String()
	case t.Time != nil:
		return t.Time.String()
	case t.Timestamp != nil:
		return t.Timestamp.String()
	case t.Integer != nil:
		return t.Integer.String()
	case t.Unknown != nil:
		return t.Unknown.String()
	case t.Json != nil:
		return t.Json.String()
	case t.Bson != nil:
		return t.Bson.String()
	case t.UUID != nil:
		return t.UUID.String()
	default:
		return ""
	}
}

// Represents a element inside a schema definition.
//
//   - if it is a group (inner node) then type is undefined and num_children is
//     defined
//
//   - if it is a primitive type (leaf) then type is defined and num_children is
//     undefined
//
// The nodes are listed in depth first traversal order.
type SchemaElement struct {
	// Data type for this field. Not set if the current element is a non-leaf node.
	Type *Type `thrift:"1,optional"`

	// If type is FixedLenByteArray, this is the byte length of the values.
	// Otherwise, if specified, this is the maximum bit length to store any of the values.
	// (e.g. a low cardinality INT col could have this set to 3).  Note that this is
	// in the schema, and therefore fixed for the entire file.
	TypeLength *int32 `thrift:"2,optional"`

	// repetition of the field. The root of the schema does not have a repetition_type.
	// All other nodes must have one.
	RepetitionType *FieldRepetitionType `thrift:"3,optional"`

	// Name of the field in the schema.
	Name string `thrift:"4,required"`

	// Nested fields.  Since thrift does not support nested fields,
	// the nesting is flattened to a single list by a depth-first traversal.
	// The children count is used to construct the nested relationship.
	// This field is not set when the element is a primitive type
	NumChildren int32 `thrift:"5,optional"`

	// DEPRECATED: When the schema is the result of a conversion from another model.
	// Used to record the original type to help with cross conversion.
	//
	// This is superseded by logicalType.
	ConvertedType *deprecated.ConvertedType `thrift:"6,optional"`

	// DEPRECATED: Used when this column contains decimal data.
	// See the DECIMAL converted type for more details.
	//
	// This is superseded by using the DecimalType annotation in logicalType.
	Scale     *int32 `thrift:"7,optional"`
	Precision *int32 `thrift:"8,optional"`

	// When the original schema supports field ids, this will save the
	// original field id in the parquet schema.
	FieldID int32 `thrift:"9,optional"`

	// The logical type of this SchemaElement
	//
	// LogicalType replaces ConvertedType, but ConvertedType is still required
	// for some logical types to ensure forward-compatibility in format v1.
	LogicalType *LogicalType `thrift:"10,optional"`
}

// Encodings supported by Parquet. Not all encodings are valid for all types.
// These enums are also used to specify the encoding of definition and
// repetition levels. See the accompanying doc for the details of the more
// complicated encodings.
type Encoding int32

const (
	// Default encoding.
	// Boolean - 1 bit per value. 0 is false; 1 is true.
	// Int32 - 4 bytes per value. Stored as little-endian.
	// Int64 - 8 bytes per value. Stored as little-endian.
	// Float - 4 bytes per value. IEEE. Stored as little-endian.
	// Double - 8 bytes per value. IEEE. Stored as little-endian.
	// ByteArray - 4 byte length stored as little endian, followed by bytes.
	// FixedLenByteArray - Just the bytes.
	Plain Encoding = 0

	// Group VarInt encoding for Int32/Int64.
	// This encoding is deprecated. It was never used.
	// GroupVarInt Encoding = 1

	// Deprecated: Dictionary encoding. The values in the dictionary are encoded
	// in the plain type.
	// In a data page use RLEDictionary instead.
	// In a Dictionary page use Plain instead.
	PlainDictionary Encoding = 2

	// Group packed run length encoding. Usable for definition/repetition levels
	// encoding and Booleans (on one bit: 0 is false 1 is true.)
	RLE Encoding = 3

	// Bit packed encoding. This can only be used if the data has a known max
	// width. Usable for definition/repetition levels encoding.
	BitPacked Encoding = 4

	// Delta encoding for integers. This can be used for int columns and works best
	// on sorted data.
	DeltaBinaryPacked Encoding = 5

	// Encoding for byte arrays to separate the length values and the data.
	// The lengths are encoded using DeltaBinaryPacked.
	DeltaLengthByteArray Encoding = 6

	// Incremental-encoded byte array. Prefix lengths are encoded using DELTA_BINARY_PACKED.
	// Suffixes are stored as delta length byte arrays.
	DeltaByteArray Encoding = 7

	// Dictionary encoding: the ids are encoded using the RLE encoding
	RLEDictionary Encoding = 8

	// Encoding for floating-point data.
	// K byte-streams are created where K is the size in bytes of the data type.
	// The individual bytes of an FP value are scattered to the corresponding stream and
	// the streams are concatenated.
	// This itself does not reduce the size of the data but can lead to better compression
	// afterwards.
	ByteStreamSplit Encoding = 9
)

func (e Encoding) String() string {
	switch e {
	case Plain:
		return "PLAIN"
	case PlainDictionary:
		return "PLAIN_DICTIONARY"
	case RLE:
		return "RLE"
	case BitPacked:
		return "BIT_PACKED"
	case DeltaBinaryPacked:
		return "DELTA_BINARY_PACKED"
	case DeltaLengthByteArray:
		return "DELTA_LENGTH_BYTE_ARRAY"
	case DeltaByteArray:
		return "DELTA_BYTE_ARRAY"
	case RLEDictionary:
		return "RLE_DICTIONARY"
	case ByteStreamSplit:
		return "BYTE_STREAM_SPLIT"
	default:
		return "Encoding(?)"
	}
}

// Supported compression algorithms.
//
// Codecs added in format version X.Y can be read by readers based on X.Y and later.
// Codec support may vary between readers based on the format version and
// libraries available at runtime.
//
// See Compression.md for a detailed specification of these algorithms.
type CompressionCodec int32

const (
	Uncompressed CompressionCodec = 0
	Snappy       CompressionCodec = 1
	Gzip         CompressionCodec = 2
	LZO          CompressionCodec = 3
	Brotli       CompressionCodec = 4 // Added in 2.4
	Lz4          CompressionCodec = 5 // DEPRECATED (Added in 2.4)
	Zstd         CompressionCodec = 6 // Added in 2.4
	Lz4Raw       CompressionCodec = 7 // Added in 2.9
)

func (c CompressionCodec) String() string {
	switch c {
	case Uncompressed:
		return "UNCOMPRESSED"
	case Snappy:
		return "SNAPPY"
	case Gzip:
		return "GZIP"
	case LZO:
		return "LZO"
	case Brotli:
		return "BROTLI"
	case Lz4:
		return "LZ4"
	case Zstd:
		return "ZSTD"
	case Lz4Raw:
		return "LZ4_RAW"
	default:
		return "CompressionCodec(?)"
	}
}

type PageType int32

const (
	DataPage       PageType = 0
	IndexPage      PageType = 1
	DictionaryPage PageType = 2
	// Version 2 is indicated in the PageHeader and the use of DataPageHeaderV2,
	// and allows you to read repetition and definition level data without
	// decompressing the Page.
	DataPageV2 PageType = 3
)

func (p PageType) String() string {
	switch p {
	case DataPage:
		return "DATA_PAGE"
	case IndexPage:
		return "INDEX_PAGE"
	case DictionaryPage:
		return "DICTIONARY_PAGE"
	case DataPageV2:
		return "DATA_PAGE_V2"
	default:
		return "PageType(?)"
	}
}

// Enum to annotate whether lists of min/max elements inside ColumnIndex
// are ordered and if so, in which direction.
type BoundaryOrder int32

const (
	Unordered  BoundaryOrder = 0
	Ascending  BoundaryOrder = 1
	Descending BoundaryOrder = 2
)

func (b BoundaryOrder) String() string {
	switch b {
	case Unordered:
		return "UNORDERED"
	case Ascending:
		return "ASCENDING"
	case Descending:
		return "DESCENDING"
	default:
		return "BoundaryOrder(?)"
	}
}

// Data page header.
type DataPageHeader struct {
	// Number of values, including NULLs, in this data page.
	NumValues int32 `thrift:"1,required"`

	// Encoding used for this data page.
	Encoding Encoding `thrift:"2,required"`

	// Encoding used for definition levels.
	DefinitionLevelEncoding Encoding `thrift:"3,required"`

	// Encoding used for repetition levels.
	RepetitionLevelEncoding Encoding `thrift:"4,required"`

	// Optional statistics for the data in this page.
	Statistics Statistics `thrift:"5,optional"`
}

type IndexPageHeader struct {
	// TODO
}

// The dictionary page must be placed at the first position of the column chunk
// if it is partly or completely dictionary encoded. At most one dictionary page
// can be placed in a column chunk.
type DictionaryPageHeader struct {
	// Number of values in the dictionary.
	NumValues int32 `thrift:"1,required"`

	// Encoding using this dictionary page.
	Encoding Encoding `thrift:"2,required"`

	// If true, the entries in the dictionary are sorted in ascending order.
	IsSorted bool `thrift:"3,optional"`
}

// New page format allowing reading levels without decompressing the data
// Repetition and definition levels are uncompressed
// The remaining section containing the data is compressed if is_compressed is
// true.
type DataPageHeaderV2 struct {
	// Number of values, including NULLs, in this data page.
	NumValues int32 `thrift:"1,required"`
	// Number of NULL values, in this data page.
	// Number of non-null = num_values - num_nulls which is also the number of
	// values in the data section.
	NumNulls int32 `thrift:"2,required"`
	// Number of rows in this data page. which means pages change on record boundaries (r = 0).
	NumRows int32 `thrift:"3,required"`
	// Encoding used for data in this page.
	Encoding Encoding `thrift:"4,required"`

	// Repetition levels and definition levels are always using RLE (without size in it).

	// Length of the definition levels.
	DefinitionLevelsByteLength int32 `thrift:"5,required"`
	// Length of the repetition levels.
	RepetitionLevelsByteLength int32 `thrift:"6,required"`

	// Whether the values are compressed.
	// Which means the section of the page between
	// definition_levels_byte_length + repetition_levels_byte_length + 1 and compressed_page_size (included)
	// is compressed with the compression_codec.
	// If missing it is considered compressed.
	IsCompressed *bool `thrift:"7,optional"`

	// Optional statistics for the data in this page.
	Statistics Statistics `thrift:"8,optional"`
}

// Block-based algorithm type annotation.
type SplitBlockAlgorithm struct{}

// The algorithm used in Bloom filter.
type BloomFilterAlgorithm struct { // union
	Block *SplitBlockAlgorithm `thrift:"1"`
}

// Hash strategy type annotation. xxHash is an extremely fast non-cryptographic
// hash algorithm. It uses 64 bits version of xxHash.
type XxHash struct{}

// The hash function used in Bloom filter. This function takes the hash of a
// column value using plain encoding.
type BloomFilterHash struct { // union
	XxHash *XxHash `thrift:"1"`
}

// The compression used in the Bloom filter.
type BloomFilterUncompressed struct{}
type BloomFilterCompression struct { // union
	Uncompressed *BloomFilterUncompressed `thrift:"1"`
}

// Bloom filter header is stored at beginning of Bloom filter data of each column
// and followed by its bitset.
type BloomFilterHeader struct {
	// The size of bitset in bytes.
	NumBytes int32 `thrift:"1,required"`
	// The algorithm for setting bits.
	Algorithm BloomFilterAlgorithm `thrift:"2,required"`
	// The hash function used for Bloom filter.
	Hash BloomFilterHash `thrift:"3,required"`
	// The compression used in the Bloom filter.
	Compression BloomFilterCompression `thrift:"4,required"`
}

type PageHeader struct {
	// The type of the page indicates which of the *Header fields below is set.
	Type PageType `thrift:"1,required"`

	// Uncompressed page size in bytes (not including this header).
	UncompressedPageSize int32 `thrift:"2,required"`

	// Compressed (and potentially encrypted) page size in bytes, not including
	// this header.
	CompressedPageSize int32 `thrift:"3,required"`

	// The 32bit CRC for the page, to be be calculated as follows:
	// - Using the standard CRC32 algorithm
	// - On the data only, i.e. this header should not be included. 'Data'
	//   hereby refers to the concatenation of the repetition levels, the
	//   definition levels and the column value, in this exact order.
	// - On the encoded versions of the repetition levels, definition levels and
	//   column values.
	// - On the compressed versions of the repetition levels, definition levels
	//   and column values where possible;
	//   - For v1 data pages, the repetition levels, definition levels and column
	//     values are always compressed together. If a compression scheme is
	//     specified, the CRC shall be calculated on the compressed version of
	//     this concatenation. If no compression scheme is specified, the CRC
	//     shall be calculated on the uncompressed version of this concatenation.
	//   - For v2 data pages, the repetition levels and definition levels are
	//     handled separately from the data and are never compressed (only
	//     encoded). If a compression scheme is specified, the CRC shall be
	//     calculated on the concatenation of the uncompressed repetition levels,
	//     uncompressed definition levels and the compressed column values.
	//     If no compression scheme is specified, the CRC shall be calculated on
	//     the uncompressed concatenation.
	// - In encrypted columns, CRC is calculated after page encryption; the
	//   encryption itself is performed after page compression (if compressed)
	// If enabled, this allows for disabling checksumming in HDFS if only a few
	// pages need to be read.
	CRC int32 `thrift:"4,optional"`

	// Headers for page specific data. One only will be set.
	DataPageHeader       *DataPageHeader       `thrift:"5,optional"`
	IndexPageHeader      *IndexPageHeader      `thrift:"6,optional"`
	DictionaryPageHeader *DictionaryPageHeader `thrift:"7,optional"`
	DataPageHeaderV2     *DataPageHeaderV2     `thrift:"8,optional"`
}

// Wrapper struct to store key values.
type KeyValue struct {
	Key   string `thrift:"1,required"`
	Value string `thrift:"2,required"`
}

// Wrapper struct to specify sort order.
type SortingColumn struct {
	// The column index (in this row group)
	ColumnIdx int32 `thrift:"1,required"`

	// If true, indicates this column is sorted in descending order.
	Descending bool `thrift:"2,required"`

	// If true, nulls will come before non-null values, otherwise,
	// nulls go at the end.
	NullsFirst bool `thrift:"3,required"`
}

// Statistics of a given page type and encoding.
type PageEncodingStats struct {
	// The page type (data/dic/...).
	PageType PageType `thrift:"1,required"`

	// Encoding of the page.
	Encoding Encoding `thrift:"2,required"`

	// Number of pages of this type with this encoding.
	Count int32 `thrift:"3,required"`
}

// Description for column metadata.
type ColumnMetaData struct {
	// Type of this column.
	Type Type `thrift:"1,required"`

	// Set of all encodings used for this column. The purpose is to validate
	// whether we can decode those pages.
	Encoding []Encoding `thrift:"2,required"`

	// Path in schema.
	PathInSchema []string `thrift:"3,required"`

	// Compression codec.
	Codec CompressionCodec `thrift:"4,required"`

	// Number of values in this column.
	NumValues int64 `thrift:"5,required"`

	// Total byte size of all uncompressed pages in this column chunk (including the headers).
	TotalUncompressedSize int64 `thrift:"6,required"`

	// Total byte size of all compressed, and potentially encrypted, pages
	// in this column chunk (including the headers).
	TotalCompressedSize int64 `thrift:"7,required"`

	// Optional key/value metadata.
	KeyValueMetadata []KeyValue `thrift:"8,optional"`

	// Byte offset from beginning of file to first data page.
	DataPageOffset int64 `thrift:"9,required"`

	// Byte offset from beginning of file to root index page.
	IndexPageOffset int64 `thrift:"10,optional"`

	// Byte offset from the beginning of file to first (only) dictionary page.
	DictionaryPageOffset int64 `thrift:"11,optional"`

	// optional statistics for this column chunk.
	Statistics Statistics `thrift:"12,optional"`

	// Set of all encodings used for pages in this column chunk.
	// This information can be used to determine if all data pages are
	// dictionary encoded for example.
	EncodingStats []PageEncodingStats `thrift:"13,optional"`

	// Byte offset from beginning of file to Bloom filter data.
	BloomFilterOffset int64 `thrift:"14,optional"`
}

type EncryptionWithFooterKey struct{}

type EncryptionWithColumnKey struct {
	// Column path in schema.
	PathInSchema []string `thrift:"1,required"`

	// Retrieval metadata of column encryption key.
	KeyMetadata []byte `thrift:"2,optional"`
}

type ColumnCryptoMetaData struct {
	EncryptionWithFooterKey *EncryptionWithFooterKey `thrift:"1"`
	EncryptionWithColumnKey *EncryptionWithColumnKey `thrift:"2"`
}

type ColumnChunk struct {
	// File where column data is stored.  If not set, assumed to be same file as
	// metadata.  This path is relative to the current file.
	FilePath string `thrift:"1,optional"`

	// Byte offset in file_path to the ColumnMetaData.
	FileOffset int64 `thrift:"2,required"`

	// Column metadata for this chunk. This is the same content as what is at
	// file_path/file_offset. Having it here has it replicated in the file
	// metadata.
	MetaData ColumnMetaData `thrift:"3,optional"`

	// File offset of ColumnChunk's OffsetIndex.
	OffsetIndexOffset int64 `thrift:"4,optional"`

	// Size of ColumnChunk's OffsetIndex, in bytes.
	OffsetIndexLength int32 `thrift:"5,optional"`

	// File offset of ColumnChunk's ColumnIndex.
	ColumnIndexOffset int64 `thrift:"6,optional"`

	// Size of ColumnChunk's ColumnIndex, in bytes.
	ColumnIndexLength int32 `thrift:"7,optional"`

	// Crypto metadata of encrypted columns.
	CryptoMetadata ColumnCryptoMetaData `thrift:"8,optional"`

	// Encrypted column metadata for this chunk.
	EncryptedColumnMetadata []byte `thrift:"9,optional"`
}

type RowGroup struct {
	// Metadata for each column chunk in this row group.
	// This list must have the same order as the SchemaElement list in FileMetaData.
	Columns []ColumnChunk `thrift:"1,required"`

	// Total byte size of all the uncompressed column data in this row group.
	TotalByteSize int64 `thrift:"2,required"`

	// Number of rows in this row group.
	NumRows int64 `thrift:"3,required"`

	// If set, specifies a sort ordering of the rows in this RowGroup.
	// The sorting columns can be a subset of all the columns.
	SortingColumns []SortingColumn `thrift:"4,optional"`

	// Byte offset from beginning of file to first page (data or dictionary)
	// in this row group
	FileOffset int64 `thrift:"5,optional"`

	// Total byte size of all compressed (and potentially encrypted) column data
	// in this row group.
	TotalCompressedSize int64 `thrift:"6,optional"`

	// Row group ordinal in the file.
	Ordinal int16 `thrift:"7,optional"`
}

// Empty struct to signal the order defined by the physical or logical type.
type TypeDefinedOrder struct{}

// Union to specify the order used for the min_value and max_value fields for a
// column. This union takes the role of an enhanced enum that allows rich
// elements (which will be needed for a collation-based ordering in the future).
//
// Possible values are:
//
//	TypeDefinedOrder - the column uses the order defined by its logical or
//	                   physical type (if there is no logical type).
//
// If the reader does not support the value of this union, min and max stats
// for this column should be ignored.
type ColumnOrder struct { // union
	// The sort orders for logical types are:
	//   UTF8 - unsigned byte-wise comparison
	//   INT8 - signed comparison
	//   INT16 - signed comparison
	//   INT32 - signed comparison
	//   INT64 - signed comparison
	//   UINT8 - unsigned comparison
	//   UINT16 - unsigned comparison
	//   UINT32 - unsigned comparison
	//   UINT64 - unsigned comparison
	//   DECIMAL - signed comparison of the represented value
	//   DATE - signed comparison
	//   TIME_MILLIS - signed comparison
	//   TIME_MICROS - signed comparison
	//   TIMESTAMP_MILLIS - signed comparison
	//   TIMESTAMP_MICROS - signed comparison
	//   INTERVAL - unsigned comparison
	//   JSON - unsigned byte-wise comparison
	//   BSON - unsigned byte-wise comparison
	//   ENUM - unsigned byte-wise comparison
	//   LIST - undefined
	//   MAP - undefined
	//
	// In the absence of logical types, the sort order is determined by the physical type:
	//   BOOLEAN - false, true
	//   INT32 - signed comparison
	//   INT64 - signed comparison
	//   INT96 (only used for legacy timestamps) - undefined
	//   FLOAT - signed comparison of the represented value (*)
	//   DOUBLE - signed comparison of the represented value (*)
	//   BYTE_ARRAY - unsigned byte-wise comparison
	//   FIXED_LEN_BYTE_ARRAY - unsigned byte-wise comparison
	//
	// (*) Because the sorting order is not specified properly for floating
	//     point values (relations vs. total ordering) the following
	//     compatibility rules should be applied when reading statistics:
	//     - If the min is a NaN, it should be ignored.
	//     - If the max is a NaN, it should be ignored.
	//     - If the min is +0, the row group may contain -0 values as well.
	//     - If the max is -0, the row group may contain +0 values as well.
	//     - When looking for NaN values, min and max should be ignored.
	TypeOrder *TypeDefinedOrder `thrift:"1"`
}

type PageLocation struct {
	// Offset of the page in the file.
	Offset int64 `thrift:"1,required"`

	// Size of the page, including header. Sum of compressed_page_size and
	// header length.
	CompressedPageSize int32 `thrift:"2,required"`

	// Index within the RowGroup of the first row of the page; this means
	// pages change on record boundaries (r = 0).
	FirstRowIndex int64 `thrift:"3,required"`
}

type OffsetIndex struct {
	// PageLocations, ordered by increasing PageLocation.offset. It is required
	// that page_locations[i].first_row_index < page_locations[i+1].first_row_index.
	PageLocations []PageLocation `thrift:"1,required"`
}

// Description for ColumnIndex.
// Each <array-field>[i] refers to the page at OffsetIndex.PageLocations[i]
type ColumnIndex struct {
	// A list of Boolean values to determine the validity of the corresponding
	// min and max values. If true, a page contains only null values, and writers
	// have to set the corresponding entries in min_values and max_values to
	// byte[0], so that all lists have the same length. If false, the
	// corresponding entries in min_values and max_values must be valid.
	NullPages []bool `thrift:"1,required"`

	// Two lists containing lower and upper bounds for the values of each page
	// determined by the ColumnOrder of the column. These may be the actual
	// minimum and maximum values found on a page, but can also be (more compact)
	// values that do not exist on a page. For example, instead of storing ""Blart
	// Versenwald III", a writer may set min_values[i]="B", max_values[i]="C".
	// Such more compact values must still be valid values within the column's
	// logical type. Readers must make sure that list entries are populated before
	// using them by inspecting null_pages.
	MinValues [][]byte `thrift:"2,required"`
	MaxValues [][]byte `thrift:"3,required"`

	// Stores whether both min_values and max_values are ordered and if so, in
	// which direction. This allows readers to perform binary searches in both
	// lists. Readers cannot assume that max_values[i] <= min_values[i+1], even
	// if the lists are ordered.
	BoundaryOrder BoundaryOrder `thrift:"4,required"`

	// A list containing the number of null values for each page.
	NullCounts []int64 `thrift:"5,optional"`
}

type AesGcmV1 struct {
	// AAD prefix.
	AadPrefix []byte `thrift:"1,optional"`

	// Unique file identifier part of AAD suffix.
	AadFileUnique []byte `thrift:"2,optional"`

	// In files encrypted with AAD prefix without storing it,
	// readers must supply the prefix.
	SupplyAadPrefix bool `thrift:"3,optional"`
}

type AesGcmCtrV1 struct {
	// AAD prefix.
	AadPrefix []byte `thrift:"1,optional"`

	// Unique file identifier part of AAD suffix.
	AadFileUnique []byte `thrift:"2,optional"`

	// In files encrypted with AAD prefix without storing it,
	// readers must supply the prefix.
	SupplyAadPrefix bool `thrift:"3,optional"`
}

type EncryptionAlgorithm struct { // union
	AesGcmV1    *AesGcmV1    `thrift:"1"`
	AesGcmCtrV1 *AesGcmCtrV1 `thrift:"2"`
}

// Description for file metadata.
type FileMetaData struct {
	// Version of this file.
	Version int32 `thrift:"1,required"`

	// Parquet schema for this file.  This schema contains metadata for all the columns.
	// The schema is represented as a tree with a single root.  The nodes of the tree
	// are flattened to a list by doing a depth-first traversal.
	// The column metadata contains the path in the schema for that column which can be
	// used to map columns to nodes in the schema.
	// The first element is the root.
	Schema []SchemaElement `thrift:"2,required"`

	// Number of rows in this file.
	NumRows int64 `thrift:"3,required"`

	// Row groups in this file.
	RowGroups []RowGroup `thrift:"4,required"`

	// Optional key/value metadata.
	KeyValueMetadata []KeyValue `thrift:"5,optional"`

	// String for application that wrote this file.  This should be in the format
	// <Application> version <App Version> (build <App Build Hash>).
	// e.g. impala version 1.0 (build 6cf94d29b2b7115df4de2c06e2ab4326d721eb55)
	CreatedBy string `thrift:"6,optional"`

	// Sort order used for the min_value and max_value fields in the Statistics
	// objects and the min_values and max_values fields in the ColumnIndex
	// objects of each column in this file. Sort orders are listed in the order
	// matching the columns in the schema. The indexes are not necessary the same
	// though, because only leaf nodes of the schema are represented in the list
	// of sort orders.
	//
	// Without column_orders, the meaning of the min_value and max_value fields
	// in the Statistics object and the ColumnIndex object is undefined. To ensure
	// well-defined behavior, if these fields are written to a Parquet file,
	// column_orders must be written as well.
	//
	// The obsolete min and max fields in the Statistics object are always sorted
	// by signed comparison regardless of column_orders.
	ColumnOrders []ColumnOrder `thrift:"7,optional"`

	// Encryption algorithm. This field is set only in encrypted files
	// with plaintext footer. Files with encrypted footer store algorithm id
	// in FileCryptoMetaData structure.
	EncryptionAlgorithm EncryptionAlgorithm `thrift:"8,optional"`

	// Retrieval metadata of key used for signing the footer.
	// Used only in encrypted files with plaintext footer.
	FooterSigningKeyMetadata []byte `thrift:"9,optional"`
}

// Crypto metadata for files with encrypted footer.
type FileCryptoMetaData struct {
	// Encryption algorithm. This field is only used for files
	// with encrypted footer. Files with plaintext footer store algorithm id
	// inside footer (FileMetaData structure).
	EncryptionAlgorithm EncryptionAlgorithm `thrift:"1,required"`

	// Retrieval metadata of key used for encryption of footer,
	// and (possibly) columns.
	KeyMetadata []byte `thrift:"2,optional"`
}
