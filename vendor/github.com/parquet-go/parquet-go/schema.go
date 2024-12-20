package parquet

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go/compress"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
)

// Schema represents a parquet schema created from a Go value.
//
// Schema implements the Node interface to represent the root node of a parquet
// schema.
type Schema struct {
	name        string
	root        Node
	deconstruct deconstructFunc
	reconstruct reconstructFunc
	mapping     columnMapping
	columns     [][]string
}

// SchemaOf constructs a parquet schema from a Go value.
//
// The function can construct parquet schemas from struct or pointer-to-struct
// values only. A panic is raised if a Go value of a different type is passed
// to this function.
//
// When creating a parquet Schema from a Go value, the struct fields may contain
// a "parquet" tag to describe properties of the parquet node. The "parquet" tag
// follows the conventional format of Go struct tags: a comma-separated list of
// values describe the options, with the first one defining the name of the
// parquet column.
//
// The following options are also supported in the "parquet" struct tag:
//
//	optional  | make the parquet column optional
//	snappy    | sets the parquet column compression codec to snappy
//	gzip      | sets the parquet column compression codec to gzip
//	brotli    | sets the parquet column compression codec to brotli
//	lz4       | sets the parquet column compression codec to lz4
//	zstd      | sets the parquet column compression codec to zstd
//	plain     | enables the plain encoding (no-op default)
//	dict      | enables dictionary encoding on the parquet column
//	delta     | enables delta encoding on the parquet column
//	list      | for slice types, use the parquet LIST logical type
//	enum      | for string types, use the parquet ENUM logical type
//	uuid      | for string and [16]byte types, use the parquet UUID logical type
//	decimal   | for int32, int64 and [n]byte types, use the parquet DECIMAL logical type
//	date      | for int32 types use the DATE logical type
//	timestamp | for int64 types use the TIMESTAMP logical type with, by default, millisecond precision
//	split     | for float32/float64, use the BYTE_STREAM_SPLIT encoding
//	id(n)     | where n is int denoting a column field id. Example id(2) for a column with field id of 2
//
// # The date logical type is an int32 value of the number of days since the unix epoch
//
// The timestamp precision can be changed by defining which precision to use as an argument.
// Supported precisions are: nanosecond, millisecond and microsecond. Example:
//
//	type Message struct {
//	  TimestrampMicros int64 `parquet:"timestamp_micros,timestamp(microsecond)"
//	}
//
// The decimal tag must be followed by two integer parameters, the first integer
// representing the scale and the second the precision; for example:
//
//	type Item struct {
//		Cost int64 `parquet:"cost,decimal(0:3)"`
//	}
//
// Invalid combination of struct tags and Go types, or repeating options will
// cause the function to panic.
//
// As a special case, if the field tag is "-", the field is omitted from the schema
// and the data will not be written into the parquet file(s).
// Note that a field with name "-" can still be generated using the tag "-,".
//
// The configuration of Parquet maps are done via two tags:
//   - The `parquet-key` tag allows to configure the key of a map.
//   - The parquet-value tag allows users to configure a map's values, for example to declare their native Parquet types.
//
// When configuring a Parquet map, the `parquet` tag will configure the map itself.
//
// For example, the following will set the int64 key of the map to be a timestamp:
//
//	type Actions struct {
//	  Action map[int64]string `parquet:"," parquet-key:",timestamp"`
//	}
//
// The schema name is the Go type name of the value.
func SchemaOf(model interface{}) *Schema {
	return schemaOf(dereference(reflect.TypeOf(model)))
}

var cachedSchemas sync.Map // map[reflect.Type]*Schema

func schemaOf(model reflect.Type) *Schema {
	cached, _ := cachedSchemas.Load(model)
	schema, _ := cached.(*Schema)
	if schema != nil {
		return schema
	}
	if model.Kind() != reflect.Struct {
		panic("cannot construct parquet schema from value of type " + model.String())
	}
	schema = NewSchema(model.Name(), nodeOf(model, nil))
	if actual, loaded := cachedSchemas.LoadOrStore(model, schema); loaded {
		schema = actual.(*Schema)
	}
	return schema
}

// NewSchema constructs a new Schema object with the given name and root node.
//
// The function panics if Node contains more leaf columns than supported by the
// package (see parquet.MaxColumnIndex).
func NewSchema(name string, root Node) *Schema {
	mapping, columns := columnMappingOf(root)
	return &Schema{
		name:        name,
		root:        root,
		deconstruct: makeDeconstructFunc(root),
		reconstruct: makeReconstructFunc(root),
		mapping:     mapping,
		columns:     columns,
	}
}

func dereference(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func makeDeconstructFunc(node Node) (deconstruct deconstructFunc) {
	if schema, _ := node.(*Schema); schema != nil {
		return schema.deconstruct
	}
	if !node.Leaf() {
		_, deconstruct = deconstructFuncOf(0, node)
	}
	return deconstruct
}

func makeReconstructFunc(node Node) (reconstruct reconstructFunc) {
	if schema, _ := node.(*Schema); schema != nil {
		return schema.reconstruct
	}
	if !node.Leaf() {
		_, reconstruct = reconstructFuncOf(0, node)
	}
	return reconstruct
}

// ConfigureRowGroup satisfies the RowGroupOption interface, allowing Schema
// instances to be passed to row group constructors to pre-declare the schema of
// the output parquet file.
func (s *Schema) ConfigureRowGroup(config *RowGroupConfig) { config.Schema = s }

// ConfigureReader satisfies the ReaderOption interface, allowing Schema
// instances to be passed to NewReader to pre-declare the schema of rows
// read from the reader.
func (s *Schema) ConfigureReader(config *ReaderConfig) { config.Schema = s }

// ConfigureWriter satisfies the WriterOption interface, allowing Schema
// instances to be passed to NewWriter to pre-declare the schema of the
// output parquet file.
func (s *Schema) ConfigureWriter(config *WriterConfig) { config.Schema = s }

// ID returns field id of the root node.
func (s *Schema) ID() int { return s.root.ID() }

// String returns a parquet schema representation of s.
func (s *Schema) String() string { return sprint(s.name, s.root) }

// Name returns the name of s.
func (s *Schema) Name() string { return s.name }

// Type returns the parquet type of s.
func (s *Schema) Type() Type { return s.root.Type() }

// Optional returns false since the root node of a parquet schema is always required.
func (s *Schema) Optional() bool { return s.root.Optional() }

// Repeated returns false since the root node of a parquet schema is always required.
func (s *Schema) Repeated() bool { return s.root.Repeated() }

// Required returns true since the root node of a parquet schema is always required.
func (s *Schema) Required() bool { return s.root.Required() }

// Leaf returns true if the root node of the parquet schema is a leaf column.
func (s *Schema) Leaf() bool { return s.root.Leaf() }

// Fields returns the list of fields on the root node of the parquet schema.
func (s *Schema) Fields() []Field { return s.root.Fields() }

// Encoding returns the encoding set on the root node of the parquet schema.
func (s *Schema) Encoding() encoding.Encoding { return s.root.Encoding() }

// Compression returns the compression codec set on the root node of the parquet
// schema.
func (s *Schema) Compression() compress.Codec { return s.root.Compression() }

// GoType returns the Go type that best represents the schema.
func (s *Schema) GoType() reflect.Type { return s.root.GoType() }

// Deconstruct deconstructs a Go value and appends it to a row.
//
// The method panics is the structure of the go value does not match the
// parquet schema.
func (s *Schema) Deconstruct(row Row, value interface{}) Row {
	columns := make([][]Value, len(s.columns))
	values := make([]Value, len(s.columns))

	for i := range columns {
		columns[i] = values[i : i : i+1]
	}

	s.deconstructValueToColumns(columns, reflect.ValueOf(value))
	return appendRow(row, columns)
}

func (s *Schema) deconstructValueToColumns(columns [][]Value, value reflect.Value) {
	for value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface {
		if value.IsNil() {
			value = reflect.Value{}
			break
		}
		value = value.Elem()
	}
	s.deconstruct(columns, levels{}, value)
}

// Reconstruct reconstructs a Go value from a row.
//
// The go value passed as first argument must be a non-nil pointer for the
// row to be decoded into.
//
// The method panics if the structure of the go value and parquet row do not
// match.
func (s *Schema) Reconstruct(value interface{}, row Row) error {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		panic("cannot reconstruct row into go value of type <nil>")
	}
	if v.Kind() != reflect.Ptr {
		panic("cannot reconstruct row into go value of non-pointer type " + v.Type().String())
	}
	if v.IsNil() {
		panic("cannot reconstruct row into nil pointer of type " + v.Type().String())
	}
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}

	b := valuesSliceBufferPool.Get().(*valuesSliceBuffer)

	columns := b.reserve(len(s.columns))
	row.Range(func(columnIndex int, columnValues []Value) bool {
		if columnIndex < len(columns) {
			columns[columnIndex] = columnValues
		}
		return true
	})
	// we avoid the defer penalty by releasing b manually
	err := s.reconstruct(v, levels{}, columns)
	b.release()
	return err
}

type valuesSliceBuffer struct {
	values [][]Value
}

func (v *valuesSliceBuffer) reserve(n int) [][]Value {
	if n <= cap(v.values) {
		return v.values[:n]
	}
	// we can try to keep growing by the power of two, but we care more about the
	// memory footprint so  this should suffice.
	//
	// The nature of reads tends to be from similar number of columns.The less work
	// we do here the better performance we can get.
	v.values = make([][]Value, n)
	return v.values
}

func (v *valuesSliceBuffer) release() {
	v.values = v.values[:0]
	valuesSliceBufferPool.Put(v)
}

var valuesSliceBufferPool = &sync.Pool{
	New: func() interface{} {
		return &valuesSliceBuffer{
			// use 64 as a cache friendly base estimate of max column numbers we will be
			// reading.
			values: make([][]Value, 0, 64),
		}
	},
}

// Lookup returns the leaf column at the given path.
//
// The path is the sequence of column names identifying a leaf column (not
// including the root).
//
// If the path was not found in the mapping, or if it did not represent a
// leaf column of the parquet schema, the boolean will be false.
func (s *Schema) Lookup(path ...string) (LeafColumn, bool) {
	leaf := s.mapping.lookup(path)
	return LeafColumn{
		Node:               leaf.node,
		Path:               leaf.path,
		ColumnIndex:        int(leaf.columnIndex),
		MaxRepetitionLevel: int(leaf.maxRepetitionLevel),
		MaxDefinitionLevel: int(leaf.maxDefinitionLevel),
	}, leaf.node != nil
}

// Columns returns the list of column paths available in the schema.
//
// The method always returns the same slice value across calls to ColumnPaths,
// applications should treat it as immutable.
func (s *Schema) Columns() [][]string {
	return s.columns
}

// Comparator constructs a comparator function which orders rows according to
// the list of sorting columns passed as arguments.
func (s *Schema) Comparator(sortingColumns ...SortingColumn) func(Row, Row) int {
	return compareRowsFuncOf(s, sortingColumns)
}

func (s *Schema) forEachNode(do func(name string, node Node)) {
	forEachNodeOf(s.Name(), s, do)
}

type structNode struct {
	gotype reflect.Type
	fields []structField
}

func structNodeOf(t reflect.Type) *structNode {
	// Collect struct fields first so we can order them before generating the
	// column indexes.
	fields := structFieldsOf(t)

	s := &structNode{
		gotype: t,
		fields: make([]structField, len(fields)),
	}

	for i := range fields {
		field := structField{name: fields[i].Name, index: fields[i].Index}
		field.Node = makeNodeOf(fields[i].Type, fields[i].Name, []string{
			fields[i].Tag.Get("parquet"),
			fields[i].Tag.Get("parquet-key"),
			fields[i].Tag.Get("parquet-value"),
		})
		s.fields[i] = field
	}

	return s
}

func structFieldsOf(t reflect.Type) []reflect.StructField {
	fields := appendStructFields(t, nil, nil, 0)

	for i := range fields {
		f := &fields[i]

		if tag := f.Tag.Get("parquet"); tag != "" {
			name, _ := split(tag)
			if name != "" {
				f.Name = name
			}
		}
	}

	return fields
}

func appendStructFields(t reflect.Type, fields []reflect.StructField, index []int, offset uintptr) []reflect.StructField {
	for i, n := 0, t.NumField(); i < n; i++ {
		f := t.Field(i)
		if tag := f.Tag.Get("parquet"); tag != "" {
			name, _ := split(tag)
			if tag != "-," && name == "-" {
				continue
			}
		}

		fieldIndex := index[:len(index):len(index)]
		fieldIndex = append(fieldIndex, i)

		f.Offset += offset

		if f.Anonymous {
			fields = appendStructFields(f.Type, fields, fieldIndex, f.Offset)
		} else if f.IsExported() {
			f.Index = fieldIndex
			fields = append(fields, f)
		}
	}
	return fields
}

func (s *structNode) Optional() bool { return false }

func (s *structNode) Repeated() bool { return false }

func (s *structNode) Required() bool { return true }

func (s *structNode) Leaf() bool { return false }

func (s *structNode) Encoding() encoding.Encoding { return nil }

func (s *structNode) Compression() compress.Codec { return nil }

func (s *structNode) GoType() reflect.Type { return s.gotype }

func (s *structNode) ID() int { return 0 }

func (s *structNode) String() string { return sprint("", s) }

func (s *structNode) Type() Type { return groupType{} }

func (s *structNode) Fields() []Field {
	fields := make([]Field, len(s.fields))
	for i := range s.fields {
		fields[i] = &s.fields[i]
	}
	return fields
}

// fieldByIndex is like reflect.Value.FieldByIndex but returns the zero-value of
// reflect.Value if one of the fields was a nil pointer instead of panicking.
func fieldByIndex(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v = v.Field(i); v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
				v = v.Elem()
				break
			} else {
				v = v.Elem()
			}
		}
	}
	return v
}

type structField struct {
	Node
	name  string
	index []int
}

func (f *structField) Name() string { return f.name }

func (f *structField) Value(base reflect.Value) reflect.Value {
	switch base.Kind() {
	case reflect.Map:
		return base.MapIndex(reflect.ValueOf(&f.name).Elem())
	case reflect.Ptr:
		if base.IsNil() {
			base.Set(reflect.New(base.Type().Elem()))
		}
		return fieldByIndex(base.Elem(), f.index)
	default:
		if len(f.index) == 1 {
			return base.Field(f.index[0])
		} else {
			return fieldByIndex(base, f.index)
		}
	}
}

func nodeString(t reflect.Type, name string, tag ...string) string {
	return fmt.Sprintf("%s %s %v", name, t.String(), tag)
}

func throwInvalidTag(t reflect.Type, name string, tag string) {
	panic(tag + " is an invalid parquet tag: " + nodeString(t, name, tag))
}

func throwUnknownTag(t reflect.Type, name string, tag string) {
	panic(tag + " is an unrecognized parquet tag: " + nodeString(t, name, tag))
}

func throwInvalidNode(t reflect.Type, msg, name string, tag ...string) {
	panic(msg + ": " + nodeString(t, name, tag...))
}

// FixedLenByteArray decimals are sized based on precision
// this function calculates the necessary byte array size.
func decimalFixedLenByteArraySize(precision int) int {
	return int(math.Ceil((math.Log10(2) + float64(precision)) / math.Log10(256)))
}

func forEachStructTagOption(sf reflect.StructField, do func(t reflect.Type, option, args string)) {
	if tag := sf.Tag.Get("parquet"); tag != "" {
		_, tag = split(tag) // skip the field name
		for tag != "" {
			option := ""
			args := ""
			option, tag = split(tag)
			option, args = splitOptionArgs(option)
			ft := sf.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			do(ft, option, args)
		}
	}
}

func nodeOf(t reflect.Type, tag []string) Node {
	switch t {
	case reflect.TypeOf(deprecated.Int96{}):
		return Leaf(Int96Type)
	case reflect.TypeOf(uuid.UUID{}):
		return UUID()
	case reflect.TypeOf(time.Time{}):
		return Timestamp(Nanosecond)
	}

	var n Node
	switch t.Kind() {
	case reflect.Bool:
		n = Leaf(BooleanType)

	case reflect.Int, reflect.Int64:
		n = Int(64)

	case reflect.Int8, reflect.Int16, reflect.Int32:
		n = Int(t.Bits())

	case reflect.Uint, reflect.Uintptr, reflect.Uint64:
		n = Uint(64)

	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		n = Uint(t.Bits())

	case reflect.Float32:
		n = Leaf(FloatType)

	case reflect.Float64:
		n = Leaf(DoubleType)

	case reflect.String:
		n = String()

	case reflect.Ptr:
		n = Optional(nodeOf(t.Elem(), nil))

	case reflect.Slice:
		if elem := t.Elem(); elem.Kind() == reflect.Uint8 { // []byte?
			n = Leaf(ByteArrayType)
		} else {
			n = Repeated(nodeOf(elem, nil))
		}

	case reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			n = Leaf(FixedLenByteArrayType(t.Len()))
		}

	case reflect.Map:
		var mapTag, valueTag, keyTag string
		if len(tag) > 0 {
			mapTag = tag[0]
			if len(tag) > 1 {
				keyTag = tag[1]
			}
			if len(tag) >= 2 {
				valueTag = tag[2]
			}
		}

		if strings.Contains(mapTag, "json") {
			n = JSON()
		} else {
			n = Map(
				makeNodeOf(t.Key(), t.Name(), []string{keyTag}),
				makeNodeOf(t.Elem(), t.Name(), []string{valueTag}),
			)
		}

		forEachTagOption([]string{mapTag}, func(option, args string) {
			switch option {
			case "", "json":
				return
			case "optional":
				n = Optional(n)
			case "id":
				id, err := parseIDArgs(args)
				if err != nil {
					throwInvalidTag(t, "map", option)
				}
				n = FieldID(n, id)
			default:
				throwUnknownTag(t, "map", option)
			}
		})

	case reflect.Struct:
		return structNodeOf(t)
	}

	if n == nil {
		panic("cannot create parquet node from go value of type " + t.String())
	}

	return &goNode{Node: n, gotype: t}
}

func split(s string) (head, tail string) {
	if i := strings.IndexByte(s, ','); i < 0 {
		head = s
	} else {
		head, tail = s[:i], s[i+1:]
	}
	return
}

func splitOptionArgs(s string) (option, args string) {
	if i := strings.IndexByte(s, '('); i >= 0 {
		option = s[:i]
		args = s[i:]
	} else {
		option = s
		args = "()"
	}
	return
}

func parseDecimalArgs(args string) (scale, precision int, err error) {
	if !strings.HasPrefix(args, "(") || !strings.HasSuffix(args, ")") {
		return 0, 0, fmt.Errorf("malformed decimal args: %s", args)
	}
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	parts := strings.Split(args, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("malformed decimal args: (%s)", args)
	}
	s, err := strconv.ParseInt(parts[0], 10, 32)
	if err != nil {
		return 0, 0, err
	}
	p, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return 0, 0, err
	}
	return int(s), int(p), nil
}

func parseIDArgs(args string) (int, error) {
	if !strings.HasPrefix(args, "(") || !strings.HasSuffix(args, ")") {
		return 0, fmt.Errorf("malformed id args: %s", args)
	}
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	return strconv.Atoi(args)
}

func parseTimestampArgs(args string) (TimeUnit, error) {
	if !strings.HasPrefix(args, "(") || !strings.HasSuffix(args, ")") {
		return nil, fmt.Errorf("malformed timestamp args: %s", args)
	}

	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")

	if len(args) == 0 {
		return Millisecond, nil
	}

	switch args {
	case "millisecond":
		return Millisecond, nil
	case "microsecond":
		return Microsecond, nil
	case "nanosecond":
		return Nanosecond, nil
	default:
	}

	return nil, fmt.Errorf("unknown time unit: %s", args)
}

type goNode struct {
	Node
	gotype reflect.Type
}

func (n *goNode) GoType() reflect.Type { return n.gotype }

var (
	_ RowGroupOption = (*Schema)(nil)
	_ ReaderOption   = (*Schema)(nil)
	_ WriterOption   = (*Schema)(nil)
)

func makeNodeOf(t reflect.Type, name string, tag []string) Node {
	var (
		node       Node
		optional   bool
		list       bool
		encoded    encoding.Encoding
		compressed compress.Codec
		fieldID    int
	)

	setNode := func(n Node) {
		if node != nil {
			throwInvalidNode(t, "struct field has multiple logical parquet types declared", name, tag...)
		}
		node = n
	}

	setOptional := func() {
		if optional {
			throwInvalidNode(t, "struct field has multiple declaration of the optional tag", name, tag...)
		}
		optional = true
	}

	setList := func() {
		if list {
			throwInvalidNode(t, "struct field has multiple declaration of the list tag", name, tag...)
		}
		list = true
	}

	setEncoding := func(e encoding.Encoding) {
		if encoded != nil {
			throwInvalidNode(t, "struct field has encoding declared multiple time", name, tag...)
		}
		encoded = e
	}

	setCompression := func(c compress.Codec) {
		if compressed != nil {
			throwInvalidNode(t, "struct field has compression codecs declared multiple times", name, tag...)
		}
		compressed = c
	}

	forEachTagOption(tag, func(option, args string) {
		if t.Kind() == reflect.Map {
			node = nodeOf(t, tag)
			return
		}
		switch option {
		case "":
			return
		case "optional":
			setOptional()

		case "snappy":
			setCompression(&Snappy)

		case "gzip":
			setCompression(&Gzip)

		case "brotli":
			setCompression(&Brotli)

		case "lz4":
			setCompression(&Lz4Raw)

		case "zstd":
			setCompression(&Zstd)

		case "uncompressed":
			setCompression(&Uncompressed)

		case "plain":
			setEncoding(&Plain)

		case "dict":
			setEncoding(&RLEDictionary)

		case "json":
			setNode(JSON())

		case "delta":
			switch t.Kind() {
			case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64:
				setEncoding(&DeltaBinaryPacked)
			case reflect.String:
				setEncoding(&DeltaByteArray)
			case reflect.Slice:
				if t.Elem().Kind() == reflect.Uint8 { // []byte?
					setEncoding(&DeltaByteArray)
				} else {
					throwInvalidTag(t, name, option)
				}
			case reflect.Array:
				if t.Elem().Kind() == reflect.Uint8 { // [N]byte?
					setEncoding(&DeltaByteArray)
				} else {
					throwInvalidTag(t, name, option)
				}
			default:
				switch t {
				case reflect.TypeOf(time.Time{}):
					setEncoding(&DeltaBinaryPacked)
				default:
					throwInvalidTag(t, name, option)
				}
			}

		case "split":
			switch t.Kind() {
			case reflect.Float32, reflect.Float64:
				setEncoding(&ByteStreamSplit)
			default:
				throwInvalidTag(t, name, option)
			}

		case "list":
			switch t.Kind() {
			case reflect.Slice:
				element := nodeOf(t.Elem(), nil)
				setNode(element)
				setList()
			default:
				throwInvalidTag(t, name, option)
			}

		case "enum":
			switch t.Kind() {
			case reflect.String:
				setNode(Enum())
			default:
				throwInvalidTag(t, name, option)
			}

		case "uuid":
			switch t.Kind() {
			case reflect.Array:
				if t.Elem().Kind() != reflect.Uint8 || t.Len() != 16 {
					throwInvalidTag(t, name, option)
				}
			default:
				throwInvalidTag(t, name, option)
			}

		case "decimal":
			scale, precision, err := parseDecimalArgs(args)
			if err != nil {
				throwInvalidTag(t, name, option+args)
			}
			var baseType Type
			switch t.Kind() {
			case reflect.Int32:
				baseType = Int32Type
			case reflect.Int64:
				baseType = Int64Type
			case reflect.Array, reflect.Slice:
				baseType = FixedLenByteArrayType(decimalFixedLenByteArraySize(precision))
			default:
				throwInvalidTag(t, name, option)
			}

			setNode(Decimal(scale, precision, baseType))
		case "date":
			switch t.Kind() {
			case reflect.Int32:
				setNode(Date())
			default:
				throwInvalidTag(t, name, option)
			}
		case "timestamp":
			switch t.Kind() {
			case reflect.Int64:
				timeUnit, err := parseTimestampArgs(args)
				if err != nil {
					throwInvalidTag(t, name, option)
				}
				setNode(Timestamp(timeUnit))
			default:
				switch t {
				case reflect.TypeOf(time.Time{}):
					timeUnit, err := parseTimestampArgs(args)
					if err != nil {
						throwInvalidTag(t, name, option)
					}
					setNode(Timestamp(timeUnit))
				default:
					throwInvalidTag(t, name, option)
				}
			}
		case "id":
			id, err := parseIDArgs(args)
			if err != nil {
				throwInvalidNode(t, "struct field has field id that is not a valid int", name, tag...)
			}
			fieldID = id
		}
	})

	// Special case: an "optional" struct tag on a slice applies to the
	// individual items, not the overall list. The least messy way to
	// deal with this is at this level, instead of passing down optional
	// information into the nodeOf function, and then passing back whether an
	// optional tag was applied.
	if node == nil && t.Kind() == reflect.Slice {
		isUint8 := t.Elem().Kind() == reflect.Uint8
		// Note for strings "optional" applies only to the entire BYTE_ARRAY and
		// not each individual byte.
		if optional && !isUint8 {
			node = Repeated(Optional(nodeOf(t.Elem(), tag)))
			// Don't also apply "optional" to the whole list.
			optional = false
		}
	}

	if node == nil {
		node = nodeOf(t, tag)
	}

	if compressed != nil {
		node = Compressed(node, compressed)
	}

	if encoded != nil {
		node = Encoded(node, encoded)
	}

	if list {
		node = List(node)
	}

	if node.Repeated() && !list {
		repeated := node.GoType().Elem()
		if repeated.Kind() == reflect.Slice {
			// Special case: allow [][]uint as seen in a logical map of strings
			if repeated.Elem().Kind() != reflect.Uint8 {
				panic("unhandled nested slice on parquet schema without list tag")
			}
		}
	}

	if optional {
		node = Optional(node)
	}
	if fieldID != 0 {
		node = FieldID(node, fieldID)
	}
	return node
}

func forEachTagOption(tags []string, do func(option, args string)) {
	for _, tag := range tags {
		_, tag = split(tag) // skip the field name
		for tag != "" {
			option := ""
			option, tag = split(tag)
			var args string
			option, args = splitOptionArgs(option)
			do(option, args)
		}
	}
}
