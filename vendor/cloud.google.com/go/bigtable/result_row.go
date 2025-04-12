/*
Copyright 2025 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bigtable // import "cloud.google.com/go/bigtable"

import (
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"time"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"cloud.google.com/go/civil"
)

// ResultRow represents a single row in the result set returned on executing a GoogleSQL query in Cloud Bigtable
type ResultRow struct {
	pbValues   []*btpb.Value
	pbMetadata *btpb.ResultSetMetadata

	Metadata *ResultRowMetadata
}

// ColumnMetadata describes a single column in a ResultRowMetadata.
type ColumnMetadata struct {
	// Name is the name of the column as returned by the query (e.g., alias or derived name).
	Name string
	// SQLType provides the original Bigtable SQL type information. This can be useful
	// for understanding the underlying storage or type details.
	SQLType SQLType
}

// ResultRowMetadata provides information about the schema of the ResultRow
type ResultRowMetadata struct {
	// the order of values returned by [ResultRow.Scan].
	Columns []ColumnMetadata
	// map from column name to list of indices {name -> [idx1, idx2, ...]}
	colNameToIndex *map[string][]int
}

func newResultRow(pbValues []*btpb.Value, pbMetadata *btpb.ResultSetMetadata, rrMetadata *ResultRowMetadata) (*ResultRow, error) {
	return &ResultRow{
		pbValues:   pbValues,
		pbMetadata: pbMetadata,
		Metadata:   rrMetadata,
	}, nil
}

// newResultRowMetadata returns the schema of the result row, describing the name and type of each column.
// The order of columns matches the order of values returned by [ResultRow.Scan].
func newResultRowMetadata(metadata *btpb.ResultSetMetadata) (*ResultRowMetadata, error) {
	if metadata == nil {
		return nil, errors.New("bigtable: metadata not found")
	}
	protoSchema := metadata.GetProtoSchema()
	if protoSchema == nil {
		return nil, fmt.Errorf("bigtable: unknown schema in metadata %T", metadata.Schema)
	}
	cols := protoSchema.GetColumns()
	md := make([]ColumnMetadata, len(cols))
	colNameToIndex := make(map[string][]int)
	for i, colMeta := range cols {
		pbType := colMeta.GetType()
		sqlType, err := pbTypeToSQLType(pbType)
		if err != nil {
			return nil, fmt.Errorf("error parsing metadata type for column %q (index %d): %w", colMeta.GetName(), i, err)
		}
		md[i] = ColumnMetadata{
			Name:    colMeta.GetName(),
			SQLType: sqlType,
		}
		colNameToIndex[colMeta.GetName()] = append(colNameToIndex[colMeta.GetName()], i)
	}

	return &ResultRowMetadata{
		Columns:        md,
		colNameToIndex: &colNameToIndex,
	}, nil
}

// Struct represents a value read from a SQL STRUCT column.
// It preserves the original order and names of the fields from the STRUCT definition,
// correctly handling duplicate names and unnamed fields (where Name=="").
// Use the provided methods (Field, Value, ValueByName, etc.) to access field data.
type Struct struct {
	// fields contains the ordered field data. Use methods for access.
	// Unexported to prevent direct manipulation inconsistent with SQL STRUCT semantics.
	fields      []structFieldWithValue
	nameToIndex map[string][]int
}

// structFieldWithValue holds the name and converted Go value for a single field
// within a Struct. Name can be empty for unnamed SQL STRUCT fields.
type structFieldWithValue struct {
	Name  string
	Value any // Holds T, *T, []*T, map[K]*V, Struct, nil etc.
}

// newStruct creates a Struct instance.
func newStruct(fields []structFieldWithValue) Struct {
	nameIndexMap := map[string][]int{}
	for i, f := range fields {
		nameIndexMap[f.Name] = append(nameIndexMap[f.Name], i)
	}
	return Struct{fields: fields, nameToIndex: nameIndexMap}
}

// Len returns the number of fields in the Struct.
func (s Struct) Len() int {
	return len(s.fields)
}

// GetByIndex returns the value of the field at the specified zero-based index
// and stores it in the value pointed to by dest.
//
// The dest argument must be a non-nil pointer. See documentation for
// [ResultRow.GetByIndex] for details on type conversions and NULL handling performed
// during assignment.
// Returns an error if the index is out of bounds, dest is invalid, or assignment fails.
func (s Struct) GetByIndex(index int, dest any) error {
	if index < 0 || index >= len(s.fields) {
		return fmt.Errorf("bigtable: index %d out of bounds for struct with %d fields", index, len(s.fields))
	}

	// Validate destination pointer
	if dest == nil {
		return errors.New("bigtable: Struct.GetByIndex destination cannot be nil")
	}
	destPtr := reflect.ValueOf(dest)
	if destPtr.Kind() != reflect.Ptr {
		return fmt.Errorf("bigtable: Struct.GetByIndex destination is not a pointer (got %T)", dest)
	}
	if destPtr.IsNil() {
		return errors.New("bigtable: Struct.GetByIndex destination is a nil pointer")
	}
	destVal := destPtr.Elem()
	if !destVal.CanSet() {
		return errors.New("bigtable: Struct.GetByIndex destination cannot be set")
	}

	// Get the already converted Go value from the struct's internal field
	fieldValue := s.fields[index].Value // Value is T, *T, []*T, map, Struct, nil etc.

	// Use assignValue to handle assignment and conversions (T<->*T, []*T<->[]T etc.)
	err := assignValue(destVal, fieldValue)
	if err != nil {
		// Add context about the struct field being assigned
		return fmt.Errorf("error assigning struct field %d (name %q, type %T) to destination (type %s): %w", index, s.fields[index].Name, fieldValue, destVal.Type(), err)
	}
	return nil
}

// GetByName returns the value of the field matching the specified name
// (case-sensitive) and stores it in the value pointed to by dest.
//
// The dest argument must be a non-nil pointer. See documentation for
// [ResultRow.GetByIndex] for details on type conversions and NULL handling performed
// during assignment.
// Returns an error if no/multiple field matches the name, dest is invalid, or assignment fails.
func (s Struct) GetByName(name string, dest any) error {
	if len(name) == 0 {
		return errors.New("bigtable: field name cannot be empty. Use GetByIndex instead to retrieve unnamed fields")
	}
	indices, found := (s.nameToIndex)[name]
	if !found || len(indices) == 0 {
		return errors.New("bigtable: field " + name + " not found in struct")
	}

	if len(indices) > 1 {
		return fmt.Errorf("bigtable: found %d fields with name %q, expected only one", len(indices), name)
	}

	return s.GetByIndex(indices[0], dest)
}

// GetByIndex returns the value of the column at the specified zero-based index and stores it
// in the value pointed to by dest.
//
// The dest argument must be a non-nil pointer.
// It performs basic type conversions. It converts columns to the following Go types where possible:
//   - string
//   - []byte
//   - int64 (and other integer types like int, int32, uint64 etc.)
//   - float32, float64
//   - bool
//   - time.Time (for TIMESTAMP)
//   - civil.Date (for DATE)
//   - Slice types (e.g., []string, []int64) for ARRAY
//   - Map types (e.g., map[string]any) for MAP
//   - Struct for STRUCT
//   - any (interface{})
//   - Pointers to the above types
//
// SQL NULL values are converted to Go nil, which can only be
// assigned to pointer types (*T), interfaces (any), or other nillable types (slices, maps).
// Attempting to scan a SQL NULL into a non-nillable Go type (like int64, string, bool)
// will result in an error.
//
// For SQL ARRAY columns containing NULL elements, dest should typically be a pointer
// to a slice of pointers (e.g., *[]*int64) or a slice of interfaces (*[]any).
// Assigning to a non-pointer slice (e.g., *[]int64) will fail if the array contains NULLs.
// For SQL STRUCT or MAP columns, dest should typically be a pointer to a map type
// (e.g., *map[string]any, *map[string]*int64).
//
// When (WITH_HISTORY=>TRUE) is used in the query, the value of versioned column is of the form []Struct
// i.e. []{{"timestamp": <timestamp>, "value": <value> }, {"timestamp": <timestamp>, "value": <value> }}.
//
// BYTES Keys in MAPs: For SQL MAP columns where the key type is `BYTES` (e.g., MAP<BYTES, INT64>),
// the Go map representation assigned to dest will use `string` keys. These string keys are the
// Base64 standard encoding of the original `BYTES` keys. This conversion is necessary due to
// Go's map key restrictions. Callers interacting with these maps must Base64 encode their
// `[]byte` keys when performing lookups.
//
// Returns an error if the index is out of bounds, dest is invalid (nil, not a pointer,
// pointer to struct other than time.Time and civil.Date), or if a type conversion fails.
func (rr *ResultRow) GetByIndex(index int, dest any) error {
	// Validate index
	if index < 0 || index >= len(rr.pbValues) {
		return fmt.Errorf("bigtable: index %d out of bounds for row with %d columns", index, len(rr.pbValues))
	}

	// Validate destination pointer
	if dest == nil {
		return errors.New("bigtable: destination cannot be nil")
	}
	destPtr := reflect.ValueOf(dest)
	if destPtr.Kind() != reflect.Ptr {
		return fmt.Errorf("bigtable: destination is not a pointer (got %T)", dest)
	}
	if destPtr.IsNil() {
		return errors.New("bigtable: destination is a nil pointer")
	}
	destVal := destPtr.Elem() // The value the pointer points to
	if !destVal.CanSet() {
		return errors.New("bigtable: destination cannot be set (perhaps pointer to unexported field)")
	}

	// Get protobuf value and type
	colInfo := rr.Metadata.Columns[index]
	sqlType := colInfo.SQLType
	if sqlType == nil {
		return fmt.Errorf("bigtable: internal error - nil SQLType for column index %d", index)
	}

	if _, isSQLStruct := sqlType.(StructSQLType); isSQLStruct {
		// Column is STRUCT. Check if destination is *bigtable.Struct or *any.
		destElemType := destVal.Type()
		if !(destElemType == reflect.TypeOf(Struct{}) || (destElemType.Kind() == reflect.Interface && destElemType.NumMethod() == 0)) {
			return fmt.Errorf("bigtable: Get destination for STRUCT column %q must be *bigtable.Struct or *any (got pointer to %s)", colInfo.Name, destElemType)
		}
	} else if destVal.Kind() == reflect.Struct {
		// For non-STRUCT columns, still disallow general struct pointers (allow time/date)
		destElemType := destVal.Type()
		if destElemType != timeType && destElemType != dateType {
			return fmt.Errorf("bigtable: Get destination cannot be a pointer to struct type %s for non-STRUCT column %q", destElemType, colInfo.Name)
		}
	}
	pbType, err := sqlType.typeProto()
	if err != nil {
		return fmt.Errorf("bigtable: internal error - failed to get protobuf type for column index %d: %w", index, err)
	}

	// Convert protobuf value to Go value
	goVal, err := pbValueToGoValue(rr.pbValues[index], pbType)
	if err != nil {
		return fmt.Errorf("error converting column %d (%q): %w", index, colInfo.Name, err)
	}

	// Assign the Go value to the destination pointer
	if err = assignValue(destVal, goVal); err != nil {
		return fmt.Errorf("error assigning column %d (%q) (value type %T) to destination (type %s): %w", index, colInfo.Name, goVal, destVal.Type(), err)
	}
	return nil
}

// GetByName returns the value of the column with the specified name
// and stores it in the value pointed to by dest. Column name matching is case-sensitive.
//
// See the documentation for [ResultRow.GetByIndex] for details on destination types, NULL handling,
// and type conversions.
//
// Returns an error if dest is invalid, if a type conversion fails or if no/multiple columns with the
// specified name are found.
func (rr *ResultRow) GetByName(name string, dest any) error {
	indices, found := (*rr.Metadata.colNameToIndex)[name]
	if !found || len(indices) == 0 {
		return errors.New("bigtable: column " + name + " not found in result row")
	}

	if len(indices) > 1 {
		return fmt.Errorf("bigtable: found %d columns with name %q, expected only one", len(indices), name)
	}

	return rr.GetByIndex(indices[0], dest)
}

// pbTypeToSQLType converts a protobuf Type to its corresponding SQLType interface implementation.
// errors returned should be wrapped before returning to the user.
func pbTypeToSQLType(pbType *btpb.Type) (SQLType, error) {
	if pbType == nil {
		return nil, errors.New("protobuf type is nil")
	}
	switch k := pbType.Kind.(type) {
	case *btpb.Type_BytesType:
		return BytesSQLType{}, nil
	case *btpb.Type_StringType:
		return StringSQLType{}, nil
	case *btpb.Type_Int64Type:
		return Int64SQLType{}, nil
	case *btpb.Type_Float32Type:
		return Float32SQLType{}, nil
	case *btpb.Type_Float64Type:
		return Float64SQLType{}, nil
	case *btpb.Type_BoolType:
		return BoolSQLType{}, nil
	case *btpb.Type_TimestampType:
		return TimestampSQLType{}, nil
	case *btpb.Type_DateType:
		return DateSQLType{}, nil
	case *btpb.Type_ArrayType:
		elemPbType := k.ArrayType.GetElementType()
		if elemPbType == nil {
			return nil, errors.New("array element type is nil")
		}
		elemSQLType, err := pbTypeToSQLType(elemPbType)
		if err != nil {
			return nil, fmt.Errorf("invalid array element type: %w", err)
		}
		return ArraySQLType{ElemType: elemSQLType}, nil
	case *btpb.Type_MapType:
		keyPbType := k.MapType.GetKeyType()
		valPbType := k.MapType.GetValueType()
		if keyPbType == nil || valPbType == nil {
			return nil, errors.New("map key or value type is nil")
		}
		keySQLType, err := pbTypeToSQLType(keyPbType)
		if err != nil {
			return nil, fmt.Errorf("invalid map key type: %w", err)
		}
		valueSQLType, err := pbTypeToSQLType(valPbType)
		if err != nil {
			return nil, fmt.Errorf("invalid map value type: %w", err)
		}
		return MapSQLType{KeyType: keySQLType, ValueType: valueSQLType}, nil
	case *btpb.Type_StructType:
		fields := k.StructType.GetFields()
		structFields := make([]StructSQLField, len(fields))
		for i, f := range fields {
			fieldPbType := f.GetType()
			if fieldPbType == nil {
				return nil, errors.New("struct field " + f.GetFieldName() + " type is nil")
			}
			fieldSQLType, err := pbTypeToSQLType(fieldPbType)
			if err != nil {
				return nil, fmt.Errorf("invalid struct field %q type: %w", f.GetFieldName(), err)
			}
			structFields[i] = StructSQLField{Name: f.GetFieldName(), Type: fieldSQLType}
		}
		return StructSQLType{Fields: structFields}, nil
	default:
		return nil, fmt.Errorf("unrecognized response type kind: %T. You might need to upgrade your client", k)
	}
}

// reflection types
var (
	bytesType   = reflect.TypeOf([]byte(nil))
	stringType  = reflect.TypeOf("")
	int64Type   = reflect.TypeOf(int64(0))
	float32Type = reflect.TypeOf(float32(0))
	float64Type = reflect.TypeOf(float64(0))
	boolType    = reflect.TypeOf(false)
	timeType    = reflect.TypeOf(time.Time{})
	dateType    = reflect.TypeOf(civil.Date{})
	structType  = reflect.TypeOf(Struct{})
)

// pbTypeToGoReflectTypeInternal determines the Go reflect.Type, returning pointers
// for nullable base types if pointerIfNullable is true.
// Errors returned should be wrapped before returning to the end user.
func pbTypeToGoReflectTypeInternal(pbType *btpb.Type, pointerIfNullable bool) (reflect.Type, error) {
	if pbType == nil {
		return nil, errors.New("protobuf type is nil")
	}
	var baseType reflect.Type
	var needsPointerWrapperForNull bool = true
	switch k := pbType.Kind.(type) {
	case *btpb.Type_BytesType:
		baseType = bytesType
		needsPointerWrapperForNull = false // []byte is already reference type
	case *btpb.Type_StringType:
		baseType = stringType
	case *btpb.Type_Int64Type:
		baseType = int64Type
	case *btpb.Type_Float32Type:
		baseType = float32Type
	case *btpb.Type_Float64Type:
		baseType = float64Type
	case *btpb.Type_BoolType:
		baseType = boolType
	case *btpb.Type_TimestampType:
		baseType = timeType
	case *btpb.Type_DateType:
		baseType = dateType
	case *btpb.Type_ArrayType:
		needsPointerWrapperForNull = false
		elemPbType := k.ArrayType.GetElementType()
		if elemPbType == nil {
			return nil, errors.New("array element type is nil")
		}
		elemGoType, err := pbTypeToGoReflectTypeInternal(elemPbType, true)
		if err != nil {
			return nil, fmt.Errorf("invalid array element type: %w", err)
		}
		baseType = reflect.SliceOf(elemGoType)
	case *btpb.Type_MapType:
		needsPointerWrapperForNull = false
		keyPbType := k.MapType.GetKeyType()
		valPbType := k.MapType.GetValueType()
		if keyPbType == nil || valPbType == nil {
			return nil, errors.New("map key or value type is nil")
		}
		keyGoType, errK := pbTypeToGoReflectTypeInternal(keyPbType, false)
		valGoType, errV := pbTypeToGoReflectTypeInternal(valPbType, true)
		if errK != nil || errV != nil {
			return nil, fmt.Errorf("invalid map key/value type: %v / %v", errK, errV)
		}
		baseType = reflect.MapOf(keyGoType, valGoType)
	case *btpb.Type_StructType:
		needsPointerWrapperForNull = false
		baseType = structType
	default:
		return nil, fmt.Errorf("unrecognized response type kind: %T. You might need to upgrade your client", k)
	}
	if pointerIfNullable && needsPointerWrapperForNull {
		switch baseType.Kind() { // Check if base type itself is already nillable
		case reflect.Interface, reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
			return baseType, nil // Already nillable
		default:
			return reflect.PointerTo(baseType), nil // Return *T for non-nillable base types
		}
	}
	return baseType, nil
}

// pbTypeToGoReflectType is pbTypeToGoReflectTypeInternal wrapper.
// Returns pointers for nullable base types.
func pbTypeToGoReflectType(pbType *btpb.Type) (reflect.Type, error) {
	return pbTypeToGoReflectTypeInternal(pbType, true)
}

// pbValueToGoValue converts a protobuf Value to a standard Go value (any).
// Base types -> T (e.g. int64, string), []byte -> []byte,
// Arrays -> []*T (e.g. []*int64), Maps -> map[K]*V, Structs -> map[string]any.
// errors returned should be wrapped before returning to the end user.
func pbValueToGoValue(pbVal *btpb.Value, pbType *btpb.Type) (any, error) {
	if pbType == nil {
		return nil, errors.New("internal error - pbType is nil during value conversion")
	}
	if pbVal == nil {
		return nil, errors.New("internal error - pbVal is nil during value conversion")
	}
	if pbVal.Kind == nil {
		// Represent SQL NULL as Go's nil interface value.
		return nil, nil
	}
	switch k := pbType.Kind.(type) {
	// Base types -> return T
	case *btpb.Type_BytesType:
		if val, ok := pbVal.Kind.(*btpb.Value_BytesValue); ok {
			return val.BytesValue, nil
		}
		return nil, fmt.Errorf("type mismatch: expected BytesValue for BytesType, got %T", pbVal.Kind)

	case *btpb.Type_StringType:
		if val, ok := pbVal.Kind.(*btpb.Value_StringValue); ok {
			return val.StringValue, nil
		}
		return nil, fmt.Errorf("type mismatch: expected StringValue for StringType, got %T", pbVal.Kind)

	case *btpb.Type_Int64Type:
		if val, ok := pbVal.Kind.(*btpb.Value_IntValue); ok {
			return val.IntValue, nil
		}
		return nil, fmt.Errorf("type mismatch: expected IntValue for Int64Type, got %T", pbVal.Kind)

	case *btpb.Type_Float32Type:
		if val, ok := pbVal.Kind.(*btpb.Value_FloatValue); ok {
			// Proto uses float64 for transport
			return float32(val.FloatValue), nil
		}
		return nil, fmt.Errorf("type mismatch: expected FloatValue for Float32Type, got %T", pbVal.Kind)

	case *btpb.Type_Float64Type:
		if val, ok := pbVal.Kind.(*btpb.Value_FloatValue); ok {
			return val.FloatValue, nil
		}
		return nil, fmt.Errorf("type mismatch: expected FloatValue for Float64Type, got %T", pbVal.Kind)

	case *btpb.Type_BoolType:
		if val, ok := pbVal.Kind.(*btpb.Value_BoolValue); ok {
			return val.BoolValue, nil
		}
		return nil, fmt.Errorf("type mismatch: expected BoolValue for BoolType, got %T", pbVal.Kind)

	case *btpb.Type_TimestampType:
		if val, ok := pbVal.Kind.(*btpb.Value_TimestampValue); ok {
			ts := val.TimestampValue
			if ts == nil {
				return nil, nil
			}
			if err := ts.CheckValid(); err != nil {
				return nil, fmt.Errorf("invalid timestamp value: %w", err)
			}
			return ts.AsTime(), nil
		}
		return nil, fmt.Errorf("type mismatch: expected TimestampValue for TimestampType, got %T", pbVal.Kind)

	case *btpb.Type_DateType:
		if val, ok := pbVal.Kind.(*btpb.Value_DateValue); ok {
			d := val.DateValue
			if d == nil {
				return nil, nil
			}
			return civil.Date{Year: int(d.Year), Month: time.Month(d.Month), Day: int(d.Day)}, nil
		}
		return nil, fmt.Errorf("type mismatch: expected DateValue for DateType, got %T", pbVal.Kind)

	// Array -> return []*T
	case *btpb.Type_ArrayType:
		arrValProto, ok := pbVal.Kind.(*btpb.Value_ArrayValue)
		if !ok {
			return nil, fmt.Errorf("type mismatch: expected ArrayValue for ArrayType, got %T", pbVal.Kind)
		}
		elemPbType := k.ArrayType.GetElementType()
		if elemPbType == nil {
			return nil, errors.New("array element type is nil")
		}
		if arrValProto.ArrayValue == nil {
			return nil, nil
		}
		elemGoPtrType, err := pbTypeToGoReflectType(elemPbType)
		if err != nil {
			return nil, fmt.Errorf("internal error getting array element Go type: %w", err)
		}
		// Gets *T type (or T if map/slice/interface)
		if len(arrValProto.ArrayValue.Values) == 0 {
			// Return empty slice []*T{} (or []T{} if element not pointer)
			return reflect.MakeSlice(reflect.SliceOf(elemGoPtrType), 0, 0).Interface(), nil
		}
		pbElements := arrValProto.ArrayValue.Values
		goSlice := reflect.MakeSlice(reflect.SliceOf(elemGoPtrType), len(pbElements), len(pbElements)) // Slice of *T (or T)
		for i, pbElem := range pbElements {
			goElem, err := pbValueToGoValue(pbElem, elemPbType)
			if err != nil {
				return nil, fmt.Errorf("error converting array element at index %d: %w", i, err)
			}
			// Returns T or nil as any
			elemValDest := goSlice.Index(i) // Destination element is *T (or T)
			if err := assignValue(elemValDest, goElem); err != nil {
				return nil, fmt.Errorf("error assigning array element %d: %w", i, err)
			}
		}
		return goSlice.Interface(), nil // Return []*T (or []T) as any

	// Map -> return map[K]*V
	case *btpb.Type_MapType:
		mapArrProto, ok := pbVal.Kind.(*btpb.Value_ArrayValue)
		if !ok {
			return nil, fmt.Errorf("type mismatch: expected ArrayValue for MapType, got %T", pbVal.Kind)
		}
		keyPbType := k.MapType.GetKeyType()
		valPbType := k.MapType.GetValueType()
		if keyPbType == nil || valPbType == nil {
			return nil, errors.New("map key or value type is nil")
		}
		// Determine Go map key type (use string for BYTES keys)
		keyGoType, _ := pbTypeToGoReflectTypeInternal(keyPbType, false) // Key type T (Bytes, String, Int64)
		isBytesKey := keyGoType == bytesType
		mapKeyGoType := keyGoType
		if isBytesKey {
			mapKeyGoType = stringType
		}

		valGoType, _ := pbTypeToGoReflectTypeInternal(valPbType, true)   // Value type *V (or V if map/slice/...)
		goMap := reflect.MakeMap(reflect.MapOf(mapKeyGoType, valGoType)) // map[K]*V (or map[K]V)
		if mapArrProto.ArrayValue == nil || len(mapArrProto.ArrayValue.Values) == 0 {
			return goMap.Interface(), nil
		} // Return empty map
		pbEntries := mapArrProto.ArrayValue.Values
		for i, pbEntry := range pbEntries {
			kvPairProto, ok := pbEntry.Kind.(*btpb.Value_ArrayValue)
			if !ok || kvPairProto.ArrayValue == nil || len(kvPairProto.ArrayValue.Values) != 2 {
				return nil, fmt.Errorf("invalid map entry format at index %d", i)
			}
			pbKey := kvPairProto.ArrayValue.Values[0]
			pbValue := kvPairProto.ArrayValue.Values[1]

			// Recursively convert the protobuf key value (pbKey) based on its protobuf type (keyPbType).
			// If keyPbType is BytesType, goKey will hold the resulting Go []byte slice.
			// If keyPbType is StringType or Int64Type, goKey will hold the string or int64.
			// For BYTES keys, the subsequent logic base64 encodes this []byte slice for use in the Go map.
			goKey, errK := pbValueToGoValue(pbKey, keyPbType)
			if errK != nil {
				return nil, fmt.Errorf("error converting map key at entry index %d: %w", i, errK)
			}
			goValue, errV := pbValueToGoValue(pbValue, valPbType)
			if errV != nil {
				return nil, fmt.Errorf("error converting map value at entry index %d: %w", i, errV)
			}

			var finalMapKey reflect.Value // This will hold string or int64
			switch keyVal := goKey.(type) {
			case []byte:
				if !isBytesKey {
					return nil, fmt.Errorf("internal error: got bytes key for non-bytes map type")
				}
				//  Base64 Encode the bytes key
				// []byte is not comparable and thus, cannot be used as map key. It is not guaranteed to be valid utf-8.
				// So, do not do string([]byte). Instead, base64 encode byte keys
				finalMapKey = reflect.ValueOf(base64.StdEncoding.EncodeToString(keyVal))
			case string:
				if isBytesKey {
					return nil, fmt.Errorf("internal error: got string key for bytes map type")
				}
				finalMapKey = reflect.ValueOf(keyVal)
			case int64:
				if isBytesKey {
					return nil, fmt.Errorf("internal error: got int64 key for bytes map type")
				}
				finalMapKey = reflect.ValueOf(keyVal)
			default:
				return nil, fmt.Errorf("internal error: unsupported map key type %T resulted", goKey)
			}
			valReflect := reflect.New(valGoType).Elem() // Dest for key is T, dest for val is *V (or V)
			if errV := assignValue(valReflect, goValue); errV != nil {
				return nil, fmt.Errorf("error assigning map value at index %d: %w", i, errV)
			}
			goMap.SetMapIndex(finalMapKey, valReflect)
		}
		return goMap.Interface(), nil // Returns map[K]*V (or map[K]V) as any

	// Struct -> return map[string]any (Fields are T or *T or nil as any)
	case *btpb.Type_StructType:
		structArrProto, ok := pbVal.Kind.(*btpb.Value_ArrayValue)
		if !ok {
			return nil, fmt.Errorf("type mismatch: expected ArrayValue for StructType, got %T", pbVal.Kind)
		}
		pbFields := k.StructType.GetFields()
		if structArrProto.ArrayValue == nil {
			return nil, nil
		}

		pbFieldValues := structArrProto.ArrayValue.Values
		if len(pbFieldValues) != len(pbFields) {
			return nil, fmt.Errorf("struct data/schema mismatch: expected %d fields, got %d values", len(pbFields), len(pbFieldValues))
		}

		// Represent struct as map[string]any
		structFields := make([]structFieldWithValue, len(pbFields))
		for i, pbFieldInfo := range pbFields {
			fieldName := pbFieldInfo.GetFieldName()
			fieldPbType := pbFieldInfo.GetType()
			if fieldPbType == nil {
				return nil, errors.New("struct field " + fieldName + " type is nil")
			}
			fieldPbValue := pbFieldValues[i]

			goFieldValue, err := pbValueToGoValue(fieldPbValue, fieldPbType)
			if err != nil {
				return nil, fmt.Errorf("error converting struct field %q: %w", fieldName, err)
			}
			structFields[i] = structFieldWithValue{
				Name:  fieldName,
				Value: goFieldValue, // Store the converted value directly
			}
		}
		return newStruct(structFields), nil

	default:
		return nil, fmt.Errorf("unrecognized response type  kind: %T. You might need to upgrade your client", k)
	}
}

// assignValue attempts to assign src Go value to dest reflect.Value.
// Handles direct assignment, pointer assignments (T <-> *T) for nullability,
// and structural slice/map conversions (e.g., []*T -> []T, map[K]*V -> map[K]V).
// dest must be settable. errors returned must be wrapped by caller.
func assignValue(dest reflect.Value, src any) error {
	if !dest.CanSet() {
		return errors.New("destination is not settable")
	}

	if src == nil {
		// Assigning nil. Check if dest is nillable.
		switch dest.Kind() {
		case reflect.Interface, reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
			// Assign typed nil (zero value of the destination type)
			dest.Set(reflect.Zero(dest.Type()))
			return nil
		default:
			// Cannot assign nil to non-nillable types like int, string, bool, struct.
			return fmt.Errorf("bigtable: cannot assign SQL NULL to non-pointer Go type %s; "+
				" use a pointer destination (e.g., *%s) or interface{} to handle NULL values", dest.Type(), dest.Type())

		}
	}

	srcVal := reflect.ValueOf(src)

	// Direct assignment check
	if srcVal.Type().AssignableTo(dest.Type()) {
		dest.Set(srcVal)
		return nil
	}

	// Add check to prevent assigning Struct to map
	if srcVal.IsValid() && srcVal.Type() == reflect.TypeOf(Struct{}) && dest.Kind() == reflect.Map {
		return fmt.Errorf("cannot assign bigtable.Struct to destination map type %s", dest.Type())
	}
	// Add check to prevent assigning Struct to other struct types
	if srcVal.IsValid() && srcVal.Type() == reflect.TypeOf(Struct{}) && dest.Kind() == reflect.Struct && dest.Type() != reflect.TypeOf(Struct{}) {
		return fmt.Errorf("cannot assign bigtable.Struct to destination struct type %s", dest.Type())
	}

	// Pointer related assignments
	// Assign T to *T
	if dest.Kind() == reflect.Ptr && dest.Type().Elem() == srcVal.Type() {
		newPtr := reflect.New(dest.Type().Elem()) // Create *T
		// Use recursive assignValue in case src is complex type needing conversion to dest.Elem()
		if err := assignValue(newPtr.Elem(), src); err != nil {
			return fmt.Errorf("error setting pointer element during T -> *T assignment: %w", err)
		}
		dest.Set(newPtr)
		return nil // Assign *T to dest
	}
	// Assign *T to T (Dereference)
	if srcVal.Kind() == reflect.Ptr && !srcVal.IsNil() && srcVal.Elem().Type().AssignableTo(dest.Type()) {
		dest.Set(srcVal.Elem())
		return nil
	}
	// Assign *T to *T (If types match - should be covered by direct assignment, but check anyway)
	if dest.Kind() == reflect.Ptr && srcVal.Kind() == reflect.Ptr && srcVal.Type().AssignableTo(dest.Type()) {
		dest.Set(srcVal)
		return nil
	}

	// Slice Assignments
	if dest.Kind() == reflect.Slice && srcVal.Kind() == reflect.Slice {
		destElemType := dest.Type().Elem()
		srcElemType := srcVal.Type().Elem()
		srcLen := srcVal.Len()

		// Case: Assigning []*T source to *[]T destination (e.g., []*int64 -> []int64)
		if srcElemType.Kind() == reflect.Ptr && destElemType == srcElemType.Elem() {
			newSlice := reflect.MakeSlice(dest.Type(), srcLen, srcLen)
			for i := 0; i < srcLen; i++ {
				srcPtrVal := srcVal.Index(i)
				if srcPtrVal.IsNil() {
					return fmt.Errorf("cannot assign slice containing nil element to destination slice with non-pointer element type %s", destElemType)
				}
				// Assign dereferenced value T to destination slice element T
				if err := assignValue(newSlice.Index(i), srcPtrVal.Elem().Interface()); err != nil {
					return fmt.Errorf("error assigning dereferenced slice element %d: %w", i, err)
				}
			}
			dest.Set(newSlice)
			return nil
		}
		// Case: Assigning []T source to *[]*T destination (e.g., []int64 -> []*int64)
		if destElemType.Kind() == reflect.Ptr && srcElemType == destElemType.Elem() {
			newSlice := reflect.MakeSlice(dest.Type(), srcLen, srcLen)
			for i := 0; i < srcLen; i++ {
				srcValue := srcVal.Index(i)
				elemPtrDest := newSlice.Index(i) // Dest element *T
				// Assign T to *T: Need to allocate pointer
				newElemPtr := reflect.New(destElemType.Elem()) // New *T
				if err := assignValue(newElemPtr.Elem(), srcValue.Interface()); err != nil {
					return fmt.Errorf("error assigning value element %d to pointer slice: %w", i, err)
				}
				elemPtrDest.Set(newElemPtr)
			}
			dest.Set(newSlice)
			return nil
		}
		// Case: Assigning []*T source to *[]any destination (e.g., []*int64 -> []any)
		if destElemType.Kind() == reflect.Interface && destElemType.NumMethod() == 0 && srcElemType.Kind() == reflect.Ptr {
			newSlice := reflect.MakeSlice(dest.Type(), srcLen, srcLen)
			for i := 0; i < srcLen; i++ {
				srcPtrVal := srcVal.Index(i)
				var elemValToSet any
				if !srcPtrVal.IsNil() {
					elemValToSet = srcPtrVal.Elem().Interface()
				} else {
					elemValToSet = nil
				}
				if err := assignValue(newSlice.Index(i), elemValToSet); err != nil {
					return fmt.Errorf("error assigning slice element %d to destination interface slice: %w", i, err)
				}
			}
			dest.Set(newSlice)
			return nil
		}
		// Case: Assigning []T source to *[]any destination (e.g. []float64 -> []any)
		if destElemType.Kind() == reflect.Interface && destElemType.NumMethod() == 0 && srcElemType.Kind() != reflect.Ptr {
			newSlice := reflect.MakeSlice(dest.Type(), srcLen, srcLen)
			for i := 0; i < srcLen; i++ {
				srcElemVal := srcVal.Index(i).Interface()
				if err := assignValue(newSlice.Index(i), srcElemVal); err != nil {
					return fmt.Errorf("error assigning slice element %d to destination interface slice: %w", i, err)
				}
			}
			dest.Set(newSlice)
			return nil
		}
		// Case: Assigning []any source to *[]T or *[]*T
		if srcElemType.Kind() == reflect.Interface && srcElemType.NumMethod() == 0 {
			newSlice := reflect.MakeSlice(dest.Type(), srcLen, srcLen)
			for i := 0; i < srcLen; i++ {
				srcElemInterface := srcVal.Index(i).Interface() // Get T/*T/nil from []any
				// Assign element to destination slice element (T or *T)
				if err := assignValue(newSlice.Index(i), srcElemInterface); err != nil {
					return fmt.Errorf("error assigning from interface slice element %d (type %T) to %s: %w", i, srcElemInterface, newSlice.Index(i).Type(), err)
				}
			}
			dest.Set(newSlice)
			return nil
		}
	}

	// Handle Map Assignments
	if dest.Kind() == reflect.Map && srcVal.Kind() == reflect.Map {
		destType := dest.Type()
		srcType := srcVal.Type()
		destKeyType := destType.Key()
		destValType := destType.Elem()
		srcKeyType := srcType.Key()
		srcValType := srcType.Elem()

		// Case: Assigning map[K]*V source to map[K]V destination (Error on nil source value)
		if destKeyType == srcKeyType && srcValType.Kind() == reflect.Ptr && destValType == srcValType.Elem() {
			if dest.IsNil() {
				dest.Set(reflect.MakeMap(destType))
			} // Initialize dest map if nil
			mapIter := srcVal.MapRange()
			for mapIter.Next() {
				srcKey := mapIter.Key()
				srcValPtr := mapIter.Value() // K and *V
				if srcValPtr.IsNil() {
					// Cannot put nil *V into destination type V
					return fmt.Errorf(
						"cannot assign nil map value from source type %s to non-pointer destination map value type %s for key %v",
						srcType, destType, srcKey.Interface())
				}
				srcValElem := srcValPtr.Elem() // Dereferenced V
				// Need new instances for map SetMapIndex
				destKey := reflect.New(destKeyType).Elem()
				destValue := reflect.New(destValType).Elem()
				// Assign K to K and V to V recursively
				if err := assignValue(destKey, srcKey.Interface()); err != nil {
					return fmt.Errorf("error assigning map key type %s to %s: %w", srcKeyType, destKeyType, err)
				}
				if err := assignValue(destValue, srcValElem.Interface()); err != nil {
					return fmt.Errorf("error assigning map value type %s to %s: %w", srcValElem.Type(), destValType, err)
				}
				dest.SetMapIndex(destKey, destValue)
			}
			return nil
		}

		// Case: Assigning map[K]V source to map[K]*V destination (Allocate pointers)
		if destKeyType == srcKeyType && destValType.Kind() == reflect.Ptr && srcValType == destValType.Elem() {
			if dest.IsNil() {
				dest.Set(reflect.MakeMap(destType))
			}
			mapIter := srcVal.MapRange()
			for mapIter.Next() {
				srcKey := mapIter.Key()
				srcValue := mapIter.Value() // K and V
				// Need new instances for map SetMapIndex
				destKey := reflect.New(destKeyType).Elem()
				destValPtr := reflect.New(destValType).Elem() // Destination element *V
				// Assign K to K
				if err := assignValue(destKey, srcKey.Interface()); err != nil {
					return fmt.Errorf("error assigning map key type %s to %s: %w", srcKeyType, destKeyType, err)
				}
				// Assign V to *V (will allocate pointer)
				if err := assignValue(destValPtr, srcValue.Interface()); err != nil {
					return fmt.Errorf("error assigning map value type %s to %s: %w", srcValType, destValType, err)
				}
				dest.SetMapIndex(destKey, destValPtr)
			}
			return nil
		}

		// Case: Assigning map[K]V or map[K]*V source to map[string]any
		// If dest is map[string]any, destValType will be anyType (interface{})
		if destKeyType.Kind() == reflect.String && destValType.Kind() == reflect.Interface {
			// Source map keys must be assignable/convertible to string
			// Check if keys are compatible string types
			if !srcKeyType.AssignableTo(destKeyType) && !(srcKeyType.Kind() == reflect.String && destKeyType.Kind() == reflect.String) {
				// If srcKeyType is []byte, do not allow conversion
				return fmt.Errorf("cannot assign source map with key type %s to destination map with key type %s", srcKeyType, destKeyType)
			}
			if dest.IsNil() {
				dest.Set(reflect.MakeMap(destType))
			}
			mapIter := srcVal.MapRange()
			for mapIter.Next() {
				srcKey := mapIter.Key()     // K
				srcValue := mapIter.Value() // V or *V
				// Key: Assume string assignable or kind string
				destKey := reflect.ValueOf(srcKey.Convert(destKeyType).Interface())
				// Value: Assign V or *V to interface{} element
				destValue := reflect.New(destValType).Elem()
				if err := assignValue(destValue, srcValue.Interface()); err != nil {
					return fmt.Errorf("error assigning map value type %s to interface{}: %w", srcValue.Type(), err)
				}
				dest.SetMapIndex(destKey, destValue)
			}
			return nil
		}
	}

	return fmt.Errorf("unsupported type conversion or assignment from %s to %s", srcVal.Type(), dest.Type())
}
