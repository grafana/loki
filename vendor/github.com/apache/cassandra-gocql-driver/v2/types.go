/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package gocql

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// CQLType is the interface that must be implemented by all registered types.
// For simple types, you can just wrap a TypeInfo with SimpleCQLType.
type CQLType interface {
	// Params should return a new slice of zero values of the closest Go types to'
	// be filled when parsing a frame. These values associcated with the types
	// are sent to TypeInfoFromParams after being read from the frame.
	//
	// The supported types are: Type, TypeInfo, []UDTField, []byte, string, int, uint16, byte.
	// Pointers are followed when filling the params but the slice sent to
	// TypeInfoFromParams will only contain the underlying values. Since
	// TypeInfo(nil) isn't supported, you can send (*TypeInfo)(nil) and
	// TypeInfoFromParams will get a TypeInfo (not *TypeInfo since that's not usable).
	//
	// If no params are needed this can return a nil slice and TypeInfoFromParams
	// will be sent nil.
	Params(proto int) []interface{}

	// TypeInfoFromParams should return a TypeInfo implementation for the type
	// with the given filled parameters. See the Params() method for what values
	// to expect.
	TypeInfoFromParams(proto int, params []interface{}) (TypeInfo, error)

	// TypeInfoFromString should return a TypeInfo implementation for the type with
	// the given names/classes. Only the portion within the parantheses or arrows
	// are passed to this function. For simple types, the name passed might be empty.
	TypeInfoFromString(proto int, name string) (TypeInfo, error)
}

// SimpleCQLType is a convenience wrapper around a TypeInfo that implements
// CQLType by returning nil for Params, and the TypeInfo for TypeInfoFromParams
// and TypeInfoFromString.
type SimpleCQLType struct {
	TypeInfo
}

// Params returns nil.
func (SimpleCQLType) Params(int) []interface{} {
	return nil
}

// TypeInfoFromParams returns the wrapped TypeInfo.
func (s SimpleCQLType) TypeInfoFromParams(proto int, params []interface{}) (TypeInfo, error) {
	return s.TypeInfo, nil
}

// TypeInfoFromString returns the wrapped TypeInfo.
func (s SimpleCQLType) TypeInfoFromString(proto int, name string) (TypeInfo, error) {
	return s.TypeInfo, nil
}

// TypeInfo describes a Cassandra specific data type and handles marshalling
// and unmarshalling.
type TypeInfo interface {
	// Type returns the Type id for the TypeInfo.
	Type() Type

	// Zero returns the Go zero value. For types that directly map to a Go type like
	// list<integer> it should return []int(nil) but for complex types like a
	// tuple<integer, boolean> it should be []interface{}{int(0), bool(false)}.
	Zero() interface{}

	// Marshal should marshal the value for the given TypeInfo into a byte slice
	Marshal(value interface{}) ([]byte, error)

	// Unmarshal should unmarshal the byte slice into the value for the given
	// TypeInfo.
	Unmarshal(data []byte, value interface{}) error
}

// RegisteredTypes is a collection of CQL types
type RegisteredTypes struct {
	byType   map[Type]CQLType
	simples  map[Type]TypeInfo
	byString map[string]Type
	custom   map[string]CQLType

	// this mutex is only used for registration and after that it's assumed that
	// the types are immutable
	mut         sync.Mutex
	initialized sync.Once
}

func (r *RegisteredTypes) init() {
	r.initialized.Do(func() {
		if r.byType == nil {
			r.byType = map[Type]CQLType{}
		}
		if r.simples == nil {
			r.simples = map[Type]TypeInfo{}
		}
		if r.byString == nil {
			r.byString = map[string]Type{}
		}
		if r.custom == nil {
			r.custom = map[string]CQLType{}
		}
	})
}

func (r *RegisteredTypes) addDefaultTypes() {
	r.init()
	r.mut.Lock()
	defer r.mut.Unlock()

	r.mustRegisterType(TypeAscii, "ascii", SimpleCQLType{varcharLikeTypeInfo{
		typ: TypeAscii,
	}})
	r.mustRegisterAlias("AsciiType", "ascii")

	r.mustRegisterType(TypeBigInt, "bigint", SimpleCQLType{bigIntLikeTypeInfo{
		typ: TypeBigInt,
	}})
	r.mustRegisterAlias("LongType", "bigint")

	r.mustRegisterType(TypeBlob, "blob", SimpleCQLType{varcharLikeTypeInfo{
		typ: TypeBlob,
	}})
	r.mustRegisterAlias("BytesType", "blob")

	r.mustRegisterType(TypeBoolean, "boolean", SimpleCQLType{booleanTypeInfo{}})
	r.mustRegisterAlias("BooleanType", "boolean")

	r.mustRegisterType(TypeCounter, "counter", SimpleCQLType{bigIntLikeTypeInfo{
		typ: TypeCounter,
	}})
	r.mustRegisterAlias("CounterColumnType", "counter")

	r.mustRegisterType(TypeDate, "date", SimpleCQLType{dateTypeInfo{}})
	r.mustRegisterAlias("SimpleDateType", "date")

	r.mustRegisterType(TypeDecimal, "decimal", SimpleCQLType{decimalTypeInfo{}})
	r.mustRegisterAlias("DecimalType", "decimal")

	r.mustRegisterType(TypeDouble, "double", SimpleCQLType{doubleTypeInfo{}})
	r.mustRegisterAlias("DoubleType", "double")

	r.mustRegisterType(TypeDuration, "duration", SimpleCQLType{durationTypeInfo{}})
	r.mustRegisterAlias("DurationType", "duration")

	r.mustRegisterType(TypeFloat, "float", SimpleCQLType{floatTypeInfo{}})
	r.mustRegisterAlias("FloatType", "float")

	r.mustRegisterType(TypeInet, "inet", SimpleCQLType{inetType{}})
	r.mustRegisterAlias("InetAddressType", "inet")

	r.mustRegisterType(TypeInt, "int", SimpleCQLType{intTypeInfo{}})
	r.mustRegisterAlias("Int32Type", "int")

	r.mustRegisterType(TypeSmallInt, "smallint", SimpleCQLType{smallIntTypeInfo{}})
	r.mustRegisterAlias("ShortType", "smallint")

	r.mustRegisterType(TypeText, "text", SimpleCQLType{varcharLikeTypeInfo{
		typ: TypeText,
	}})

	r.mustRegisterType(TypeTime, "time", SimpleCQLType{timeTypeInfo{}})
	r.mustRegisterAlias("TimeType", "time")

	r.mustRegisterType(TypeTimestamp, "timestamp", SimpleCQLType{timestampTypeInfo{}})
	r.mustRegisterAlias("TimestampType", "timestamp")
	// DateType was a timestamp when date didn't exist
	r.mustRegisterAlias("DateType", "timestamp")

	r.mustRegisterType(TypeTimeUUID, "timeuuid", SimpleCQLType{timeUUIDType{}})
	r.mustRegisterAlias("TimeUUIDType", "timeuuid")

	r.mustRegisterType(TypeTinyInt, "tinyint", SimpleCQLType{tinyIntTypeInfo{}})
	r.mustRegisterAlias("ByteType", "tinyint")

	r.mustRegisterType(TypeUUID, "uuid", SimpleCQLType{uuidType{}})
	r.mustRegisterAlias("UUIDType", "uuid")
	r.mustRegisterAlias("LexicalUUIDType", "uuid")

	r.mustRegisterType(TypeVarchar, "varchar", SimpleCQLType{varcharLikeTypeInfo{
		typ: TypeVarchar,
	}})
	r.mustRegisterAlias("UTF8Type", "varchar")

	r.mustRegisterType(TypeVarint, "varint", SimpleCQLType{varintTypeInfo{}})
	r.mustRegisterAlias("IntegerType", "varint")

	// these types need references to the registered types
	r.mustRegisterType(TypeList, "list", listSetCQLType{
		typ:   TypeList,
		types: r,
	})
	r.mustRegisterAlias("ListType", "list")

	r.mustRegisterType(TypeSet, "set", listSetCQLType{
		typ:   TypeSet,
		types: r,
	})
	r.mustRegisterAlias("SetType", "set")

	r.mustRegisterType(TypeMap, "map", mapCQLType{
		types: r,
	})
	r.mustRegisterAlias("MapType", "map")

	r.mustRegisterType(TypeTuple, "tuple", tupleCQLType{
		types: r,
	})
	r.mustRegisterAlias("TupleType", "tuple")

	r.mustRegisterType(TypeUDT, "udt", udtCQLType{
		types: r,
	})
	r.mustRegisterAlias("UserType", "udt")

	r.mustRegisterCustom("vector", vectorCQLType{
		types: r,
	})
	r.mustRegisterAlias("VectorType", "vector")
}

// RegisterType registers a new CQL data type. Type should be the CQL id for
// the type. Name is the name of the type as returned in the metadata for the
// column. CQLType is the implementation of the type.
// This function must not be called after a session has been created.
func (r *RegisteredTypes) RegisterType(typ Type, name string, t CQLType) error {
	r.init()
	r.mut.Lock()
	defer r.mut.Unlock()
	return r.registerType(typ, name, t)
}

func (r *RegisteredTypes) registerType(typ Type, name string, t CQLType) error {
	if typ == TypeCustom {
		return errors.New("custom types must be registered with RegisterCustom")
	}

	if _, ok := r.byType[typ]; ok {
		return fmt.Errorf("type %d already registered", typ)
	}
	if _, ok := r.byString[name]; ok {
		return fmt.Errorf("type name %s already registered", name)
	}
	r.byType[typ] = t
	if s, ok := t.(SimpleCQLType); ok {
		r.simples[typ] = s.TypeInfo
	}
	r.byString[name] = typ
	return nil
}

func (r *RegisteredTypes) mustRegisterType(typ Type, name string, t CQLType) {
	if err := r.registerType(typ, name, t); err != nil {
		panic(err)
	}
}

// RegisterCustom registers a new custom CQL type. Name is the name of the type
// as returned in the metadata for the column. CQLType is the implementation of
// the type.
// This function must not be called after a session has been created.
func (r *RegisteredTypes) RegisterCustom(name string, t CQLType) error {
	r.init()
	r.mut.Lock()
	defer r.mut.Unlock()
	return r.registerCustom(name, t)
}

func (r *RegisteredTypes) registerCustom(name string, t CQLType) error {
	if r.custom == nil {
		r.custom = map[string]CQLType{}
	}
	if _, ok := r.custom[name]; ok {
		return fmt.Errorf("custom type %s already registered", name)
	}
	if _, ok := r.byString[name]; ok {
		return fmt.Errorf("type name %s already registered", name)
	}

	r.custom[name] = t
	r.byString[name] = TypeCustom
	return nil
}

func (r *RegisteredTypes) mustRegisterCustom(name string, t CQLType) {
	if err := r.registerCustom(name, t); err != nil {
		panic(err)
	}
}

// AddAlias adds an alias for an already registered type. If you expect a type
// to be referenced as multiple different types or if you need to add the Java
// marshal class for a type you should call this method.
// This function must not be called after a session has been created.
func (r *RegisteredTypes) AddAlias(name, as string) error {
	r.init()
	r.mut.Lock()
	defer r.mut.Unlock()
	return r.registerAlias(name, as)
}

func (r *RegisteredTypes) registerAlias(name, as string) error {
	if strings.HasPrefix(name, apacheCassandraTypePrefix) {
		name = strings.TrimPrefix(name, apacheCassandraTypePrefix)
	}
	if strings.HasPrefix(as, apacheCassandraTypePrefix) {
		as = strings.TrimPrefix(as, apacheCassandraTypePrefix)
	}
	if _, ok := r.byString[name]; ok {
		return fmt.Errorf("type name %s already registered", name)
	}
	if _, ok := r.byString[as]; !ok {
		return fmt.Errorf("type name %s was not registered", as)
	}
	if _, ok := r.custom[as]; ok {
		r.custom[name] = r.custom[as]
	}
	r.byString[name] = r.byString[as]
	return nil
}

func (r *RegisteredTypes) mustRegisterAlias(name, as string) {
	if err := r.registerAlias(name, as); err != nil {
		panic(err)
	}
}

func (r *RegisteredTypes) typeInfoFromJavaString(proto int, fullName string) (TypeInfo, error) {
	name := strings.TrimPrefix(fullName, apacheCassandraTypePrefix)
	compositeNameIdx := strings.Index(name, "(")
	var params string
	if compositeNameIdx != -1 {
		compositeParamsEndIdx := strings.LastIndex(name, ")")
		if compositeParamsEndIdx == -1 {
			return nil, fmt.Errorf("invalid type string %v", fullName)
		}
		params = name[compositeNameIdx+1 : compositeParamsEndIdx]
		name = name[:compositeNameIdx]
	}
	t, ok := r.getType(name)
	if !ok {
		return nil, unknownTypeError(fullName)
	}
	return t.TypeInfoFromString(proto, params)
}

func (r *RegisteredTypes) typeInfoFromString(proto int, name string) (TypeInfo, error) {
	// check for java long-form type
	if strings.HasPrefix(name, apacheCassandraTypePrefix) {
		return r.typeInfoFromJavaString(proto, name)
	}

	compositeNameIdx := strings.Index(name, "<")
	var params string
	if compositeNameIdx != -1 {
		compositeParamsEndIdx := strings.LastIndex(name, ">")
		if compositeParamsEndIdx == -1 {
			return nil, fmt.Errorf("invalid type string %v", name)
		}
		params = name[compositeNameIdx+1 : compositeParamsEndIdx]
		name = name[:compositeNameIdx]
		// frozen is a special case
		if name == "frozen" {
			return r.typeInfoFromString(proto, params)
		}
	} else if strings.Contains(name, "(") {
		// most likely a java long-form type
		return r.typeInfoFromJavaString(proto, name)
	}

	t, ok := r.getType(name)
	if !ok {
		return nil, unknownTypeError(name)
	}
	return t.TypeInfoFromString(proto, params)
}

func splitCompositeTypes(name string) []string {
	// check for the simple case without any composite types
	if !strings.Contains(name, "(") && !strings.Contains(name, "<") {
		parts := strings.Split(name, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	var parts []string
	lessCount := 0
	segment := ""
	var openChar, closeChar rune
	for _, char := range name {
		if char == ',' && lessCount == 0 {
			if segment != "" {
				parts = append(parts, strings.TrimSpace(segment))
			}
			segment = ""
			continue
		}
		segment += string(char)
		// determine which open/close characters to use
		if openChar == 0 {
			if char == '<' {
				openChar = '<'
				closeChar = '>'
				lessCount++
			} else if char == '(' {
				openChar = '('
				closeChar = ')'
				lessCount++
			}
		} else if char == openChar {
			lessCount++
		} else if char == closeChar {
			lessCount--
		}
	}
	if segment != "" {
		parts = append(parts, strings.TrimSpace(segment))
	}
	return parts
}

func (r *RegisteredTypes) getType(classOrName string) (CQLType, bool) {
	classOrName = strings.TrimPrefix(classOrName, apacheCassandraTypePrefix)
	typ, ok := r.byString[classOrName]
	if !ok {
		// it could also be a UDT but this is what we've always done
		typ = TypeCustom
	}
	var t CQLType
	if typ == TypeCustom {
		t, ok = r.custom[classOrName]
	} else {
		t = r.fastRegisteredTypeLookup(typ)
		ok = t != nil
	}
	return t, ok
}

func (r *RegisteredTypes) fastTypeInfoLookup(typ Type) TypeInfo {
	switch typ {
	case TypeAscii:
		return varcharLikeTypeInfo{
			typ: TypeAscii,
		}
	case TypeBigInt:
		return bigIntLikeTypeInfo{
			typ: TypeBigInt,
		}
	case TypeBlob:
		return varcharLikeTypeInfo{
			typ: TypeBlob,
		}
	case TypeBoolean:
		return booleanTypeInfo{}
	case TypeCounter:
		return bigIntLikeTypeInfo{
			typ: TypeCounter,
		}
	case TypeDate:
		return dateTypeInfo{}
	case TypeDecimal:
		return decimalTypeInfo{}
	case TypeDouble:
		return doubleTypeInfo{}
	case TypeDuration:
		return durationTypeInfo{}
	case TypeFloat:
		return floatTypeInfo{}
	case TypeInet:
		return inetType{}
	case TypeInt:
		return intTypeInfo{}
	case TypeSmallInt:
		return smallIntTypeInfo{}
	case TypeText:
		return varcharLikeTypeInfo{
			typ: TypeText,
		}
	case TypeTime:
		return timeTypeInfo{}
	case TypeTimestamp:
		return timestampTypeInfo{}
	case TypeTimeUUID:
		return timeUUIDType{}
	case TypeTinyInt:
		return tinyIntTypeInfo{}
	case TypeUUID:
		return uuidType{}
	case TypeVarchar:
		return varcharLikeTypeInfo{
			typ: TypeVarchar,
		}
	case TypeVarint:
		return varintTypeInfo{}
	default:
		return r.simples[typ]
	}
}

// fastRegisteredTypeLookup is a fast lookup for the registered type that avoids
// the need for a map lookup which was shown to be significant
// in cases where it's necessary you should consider manually inlining this method
func (r *RegisteredTypes) fastRegisteredTypeLookup(typ Type) CQLType {
	switch typ {
	case TypeAscii:
		return SimpleCQLType{varcharLikeTypeInfo{
			typ: TypeAscii,
		}}
	case TypeBigInt:
		return SimpleCQLType{bigIntLikeTypeInfo{
			typ: TypeBigInt,
		}}
	case TypeBlob:
		return SimpleCQLType{varcharLikeTypeInfo{
			typ: TypeBlob,
		}}
	case TypeBoolean:
		return SimpleCQLType{booleanTypeInfo{}}
	case TypeCounter:
		return SimpleCQLType{bigIntLikeTypeInfo{
			typ: TypeCounter,
		}}
	case TypeDate:
		return SimpleCQLType{dateTypeInfo{}}
	case TypeDecimal:
		return SimpleCQLType{decimalTypeInfo{}}
	case TypeDouble:
		return SimpleCQLType{doubleTypeInfo{}}
	case TypeDuration:
		return SimpleCQLType{durationTypeInfo{}}
	case TypeFloat:
		return SimpleCQLType{floatTypeInfo{}}
	case TypeInet:
		return SimpleCQLType{inetType{}}
	case TypeInt:
		return SimpleCQLType{intTypeInfo{}}
	case TypeSmallInt:
		return SimpleCQLType{smallIntTypeInfo{}}
	case TypeText:
		return SimpleCQLType{varcharLikeTypeInfo{
			typ: TypeText,
		}}
	case TypeTime:
		return SimpleCQLType{timeTypeInfo{}}
	case TypeTimestamp:
		return SimpleCQLType{timestampTypeInfo{}}
	case TypeTimeUUID:
		return SimpleCQLType{timeUUIDType{}}
	case TypeTinyInt:
		return SimpleCQLType{tinyIntTypeInfo{}}
	case TypeUUID:
		return SimpleCQLType{uuidType{}}
	case TypeVarchar:
		return SimpleCQLType{varcharLikeTypeInfo{
			typ: TypeVarchar,
		}}
	case TypeVarint:
		return SimpleCQLType{varintTypeInfo{}}
	case TypeCustom:
		// this should never happen
		panic("custom types cannot be returned from fastRegisteredTypeLookup")
	default:
		return r.byType[typ]
	}
}

// Copy returns a new shallow copy of the RegisteredTypes
func (r *RegisteredTypes) Copy() *RegisteredTypes {
	r.mut.Lock()
	defer r.mut.Unlock()

	copy := &RegisteredTypes{}
	copy.init()
	for typ, t := range r.byType {
		copy.byType[typ] = t
	}
	for typ, t := range r.simples {
		copy.simples[typ] = t
	}
	for name, typ := range r.byString {
		copy.byString[name] = typ
	}
	for name, t := range r.custom {
		copy.custom[name] = t
	}
	return copy
}

// GlobalTypes is the set of types that are registered globally and are copied
// by all sessions that don't define their own RegisteredTypes in ClusterConfig.
// Since a new session copies this, you should be modifying this or creating your
// own before a session is created.
var GlobalTypes = func() *RegisteredTypes {
	r := &RegisteredTypes{}
	// we init because we end up calling GlobalTypes in tests and other spots before
	// init would get called
	r.init()
	r.addDefaultTypes()
	return r
}()

// Type is the identifier of a Cassandra internal datatype.
// Available types include: TypeCustom, TypeAscii, TypeBigInt, TypeBlob, TypeBoolean, TypeCounter,
// TypeDecimal, TypeDouble, TypeFloat, TypeInt, TypeText, TypeTimestamp, TypeUUID, TypeVarchar,
// TypeVarint, TypeTimeUUID, TypeInet, TypeDate, TypeTime, TypeSmallInt, TypeTinyInt, TypeDuration,
// TypeList, TypeMap, TypeSet, TypeUDT, TypeTuple.
type Type int

const (
	TypeCustom    Type = 0x0000
	TypeAscii     Type = 0x0001
	TypeBigInt    Type = 0x0002
	TypeBlob      Type = 0x0003
	TypeBoolean   Type = 0x0004
	TypeCounter   Type = 0x0005
	TypeDecimal   Type = 0x0006
	TypeDouble    Type = 0x0007
	TypeFloat     Type = 0x0008
	TypeInt       Type = 0x0009
	TypeText      Type = 0x000A
	TypeTimestamp Type = 0x000B
	TypeUUID      Type = 0x000C
	TypeVarchar   Type = 0x000D
	TypeVarint    Type = 0x000E
	TypeTimeUUID  Type = 0x000F
	TypeInet      Type = 0x0010
	TypeDate      Type = 0x0011
	TypeTime      Type = 0x0012
	TypeSmallInt  Type = 0x0013
	TypeTinyInt   Type = 0x0014
	TypeDuration  Type = 0x0015
	TypeList      Type = 0x0020
	TypeMap       Type = 0x0021
	TypeSet       Type = 0x0022
	TypeUDT       Type = 0x0030
	TypeTuple     Type = 0x0031
)

// NewNativeType returns a TypeInfo from the global registered types.
// Deprecated.
func NewNativeType(proto byte, typ Type, custom string) TypeInfo {
	if typ == TypeCustom {
		t, err := GlobalTypes.typeInfoFromString(int(proto), custom)
		if err != nil {
			panic(err)
		}
		return t
	}
	rt := GlobalTypes.fastRegisteredTypeLookup(typ)
	if rt == nil {
		return unknownTypeInfo(fmt.Sprintf("%d", typ))
	}
	// most of the time this will do nothing because it's a SimpleCQLType but if
	// it's not then we don't have anything to pass but custom
	t, err := rt.TypeInfoFromString(int(proto), custom)
	if err != nil {
		panic(err)
	}
	return t
}

type unknownTypeInfo string

func (unknownTypeInfo) Type() Type {
	return TypeCustom
}

// Zero returns the zero value for the unknown custom type.
func (unknownTypeInfo) Zero() interface{} {
	return nil
}

func (u unknownTypeInfo) Marshal(value interface{}) ([]byte, error) {
	return nil, fmt.Errorf("can not marshal %T into %s", value, string(u))
}

func (u unknownTypeInfo) Unmarshal(_ []byte, value interface{}) error {
	return fmt.Errorf("can not unmarshal %s into %T", string(u), value)
}

type unknownTypeError string

func (e unknownTypeError) Error() string {
	return fmt.Sprintf("unknown type %v", string(e))
}
