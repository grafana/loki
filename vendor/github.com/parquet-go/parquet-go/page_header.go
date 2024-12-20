package parquet

import (
	"fmt"

	"github.com/parquet-go/parquet-go/format"
)

// PageHeader is an interface implemented by parquet page headers.
type PageHeader interface {
	// Returns the number of values in the page (including nulls).
	NumValues() int64

	// Returns the page encoding.
	Encoding() format.Encoding

	// Returns the parquet format page type.
	PageType() format.PageType
}

// DataPageHeader is a specialization of the PageHeader interface implemented by
// data pages.
type DataPageHeader interface {
	PageHeader

	// Returns the encoding of the repetition level section.
	RepetitionLevelEncoding() format.Encoding

	// Returns the encoding of the definition level section.
	DefinitionLevelEncoding() format.Encoding

	// Returns the number of null values in the page.
	NullCount() int64

	// Returns the minimum value in the page based on the ordering rules of the
	// column's logical type.
	//
	// As an optimization, the method may return the same slice across multiple
	// calls. Programs must treat the returned value as immutable to prevent
	// unpredictable behaviors.
	//
	// If the page only contains only null values, an empty slice is returned.
	MinValue() []byte

	// Returns the maximum value in the page based on the ordering rules of the
	// column's logical type.
	//
	// As an optimization, the method may return the same slice across multiple
	// calls. Programs must treat the returned value as immutable to prevent
	// unpredictable behaviors.
	//
	// If the page only contains only null values, an empty slice is returned.
	MaxValue() []byte
}

// DictionaryPageHeader is an implementation of the PageHeader interface
// representing dictionary pages.
type DictionaryPageHeader struct {
	header *format.DictionaryPageHeader
}

func (dict DictionaryPageHeader) NumValues() int64 {
	return int64(dict.header.NumValues)
}

func (dict DictionaryPageHeader) Encoding() format.Encoding {
	return dict.header.Encoding
}

func (dict DictionaryPageHeader) PageType() format.PageType {
	return format.DictionaryPage
}

func (dict DictionaryPageHeader) IsSorted() bool {
	return dict.header.IsSorted
}

func (dict DictionaryPageHeader) String() string {
	return fmt.Sprintf("DICTIONARY_PAGE_HEADER{NumValues=%d,Encoding=%s,IsSorted=%t}",
		dict.header.NumValues,
		dict.header.Encoding,
		dict.header.IsSorted)
}

// DataPageHeaderV1 is an implementation of the DataPageHeader interface
// representing data pages version 1.
type DataPageHeaderV1 struct {
	header *format.DataPageHeader
}

func (v1 DataPageHeaderV1) NumValues() int64 {
	return int64(v1.header.NumValues)
}

func (v1 DataPageHeaderV1) RepetitionLevelEncoding() format.Encoding {
	return v1.header.RepetitionLevelEncoding
}

func (v1 DataPageHeaderV1) DefinitionLevelEncoding() format.Encoding {
	return v1.header.DefinitionLevelEncoding
}

func (v1 DataPageHeaderV1) Encoding() format.Encoding {
	return v1.header.Encoding
}

func (v1 DataPageHeaderV1) PageType() format.PageType {
	return format.DataPage
}

func (v1 DataPageHeaderV1) NullCount() int64 {
	return v1.header.Statistics.NullCount
}

func (v1 DataPageHeaderV1) MinValue() []byte {
	return v1.header.Statistics.MinValue
}

func (v1 DataPageHeaderV1) MaxValue() []byte {
	return v1.header.Statistics.MaxValue
}

func (v1 DataPageHeaderV1) String() string {
	return fmt.Sprintf("DATA_PAGE_HEADER{NumValues=%d,Encoding=%s}",
		v1.header.NumValues,
		v1.header.Encoding)
}

// DataPageHeaderV2 is an implementation of the DataPageHeader interface
// representing data pages version 2.
type DataPageHeaderV2 struct {
	header *format.DataPageHeaderV2
}

func (v2 DataPageHeaderV2) NumValues() int64 {
	return int64(v2.header.NumValues)
}

func (v2 DataPageHeaderV2) NumNulls() int64 {
	return int64(v2.header.NumNulls)
}

func (v2 DataPageHeaderV2) NumRows() int64 {
	return int64(v2.header.NumRows)
}

func (v2 DataPageHeaderV2) RepetitionLevelsByteLength() int64 {
	return int64(v2.header.RepetitionLevelsByteLength)
}

func (v2 DataPageHeaderV2) DefinitionLevelsByteLength() int64 {
	return int64(v2.header.DefinitionLevelsByteLength)
}

func (v2 DataPageHeaderV2) RepetitionLevelEncoding() format.Encoding {
	return format.RLE
}

func (v2 DataPageHeaderV2) DefinitionLevelEncoding() format.Encoding {
	return format.RLE
}

func (v2 DataPageHeaderV2) Encoding() format.Encoding {
	return v2.header.Encoding
}

func (v2 DataPageHeaderV2) PageType() format.PageType {
	return format.DataPageV2
}

func (v2 DataPageHeaderV2) NullCount() int64 {
	return v2.header.Statistics.NullCount
}

func (v2 DataPageHeaderV2) MinValue() []byte {
	return v2.header.Statistics.MinValue
}

func (v2 DataPageHeaderV2) MaxValue() []byte {
	return v2.header.Statistics.MaxValue
}

func (v2 DataPageHeaderV2) IsCompressed() bool {
	return v2.header.IsCompressed == nil || *v2.header.IsCompressed
}

func (v2 DataPageHeaderV2) String() string {
	return fmt.Sprintf("DATA_PAGE_HEADER_V2{NumValues=%d,NumNulls=%d,NumRows=%d,Encoding=%s,IsCompressed=%t}",
		v2.header.NumValues,
		v2.header.NumNulls,
		v2.header.NumRows,
		v2.header.Encoding,
		v2.IsCompressed())
}

type unknownPageHeader struct {
	header *format.PageHeader
}

func (u unknownPageHeader) NumValues() int64 {
	return 0
}

func (u unknownPageHeader) Encoding() format.Encoding {
	return -1
}

func (u unknownPageHeader) PageType() format.PageType {
	return u.header.Type
}

func (u unknownPageHeader) String() string {
	return fmt.Sprintf("UNKNOWN_PAGE_HEADER{Type=%d}", u.header.Type)
}

var (
	_ PageHeader     = DictionaryPageHeader{}
	_ DataPageHeader = DataPageHeaderV1{}
	_ DataPageHeader = DataPageHeaderV2{}
	_ PageHeader     = unknownPageHeader{}
)
