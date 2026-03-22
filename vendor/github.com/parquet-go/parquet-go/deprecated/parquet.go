package deprecated

// DEPRECATED: Common types used by frameworks(e.g. hive, pig) using parquet.
// ConvertedType is superseded by LogicalType.  This enum should not be extended.
//
// See LogicalTypes.md for conversion between ConvertedType and LogicalType.
type ConvertedType int32

const (
	// a BYTE_ARRAY actually contains UTF8 encoded chars
	UTF8 ConvertedType = 0

	// a map is converted as an optional field containing a repeated key/value pair
	Map ConvertedType = 1

	// a key/value pair is converted into a group of two fields
	MapKeyValue ConvertedType = 2

	// a list is converted into an optional field containing a repeated field for its
	// values
	List ConvertedType = 3

	// an enum is converted into a binary field
	Enum ConvertedType = 4

	// A decimal value.
	//
	// This may be used to annotate binary or fixed primitive types. The
	// underlying byte array stores the unscaled value encoded as two's
	// complement using big-endian byte order (the most significant byte is the
	// zeroth element). The value of the decimal is the value * 10^{-scale}.
	//
	// This must be accompanied by a (maximum) precision and a scale in the
	// SchemaElement. The precision specifies the number of digits in the decimal
	// and the scale stores the location of the decimal point. For example 1.23
	// would have precision 3 (3 total digits) and scale 2 (the decimal point is
	// 2 digits over).
	Decimal ConvertedType = 5

	// A Date
	//
	// Stored as days since Unix epoch, encoded as the INT32 physical type.
	Date ConvertedType = 6

	// A time
	//
	// The total number of milliseconds since midnight.  The value is stored
	// as an INT32 physical type.
	TimeMillis ConvertedType = 7

	// A time.
	//
	// The total number of microseconds since midnight.  The value is stored as
	// an INT64 physical type.
	TimeMicros ConvertedType = 8

	// A date/time combination
	//
	// Date and time recorded as milliseconds since the Unix epoch.  Recorded as
	// a physical type of INT64.
	TimestampMillis ConvertedType = 9

	// A date/time combination
	//
	// Date and time recorded as microseconds since the Unix epoch.  The value is
	// stored as an INT64 physical type.
	TimestampMicros ConvertedType = 10

	// An unsigned integer value.
	//
	// The number describes the maximum number of meaningful data bits in
	// the stored value. 8, 16 and 32 bit values are stored using the
	// INT32 physical type.  64 bit values are stored using the INT64
	// physical type.
	Uint8  ConvertedType = 11
	Uint16 ConvertedType = 12
	Uint32 ConvertedType = 13
	Uint64 ConvertedType = 14

	// A signed integer value.
	//
	// The number describes the maximum number of meaningful data bits in
	// the stored value. 8, 16 and 32 bit values are stored using the
	// INT32 physical type.  64 bit values are stored using the INT64
	// physical type.
	Int8  ConvertedType = 15
	Int16 ConvertedType = 16
	Int32 ConvertedType = 17
	Int64 ConvertedType = 18

	// An embedded JSON document
	//
	// A JSON document embedded within a single UTF8 column.
	Json ConvertedType = 19

	// An embedded BSON document
	//
	// A BSON document embedded within a single BINARY column.
	Bson ConvertedType = 20

	// An interval of time
	//
	// This type annotates data stored as a FIXED_LEN_BYTE_ARRAY of length 12
	// This data is composed of three separate little endian unsigned
	// integers.  Each stores a component of a duration of time.  The first
	// integer identifies the number of months associated with the duration,
	// the second identifies the number of days associated with the duration
	// and the third identifies the number of milliseconds associated with
	// the provided duration.  This duration of time is independent of any
	// particular timezone or date.
	Interval ConvertedType = 21
)
