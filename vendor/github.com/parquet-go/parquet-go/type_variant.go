package parquet

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Variant constructs a node of unshredded [VARIANT logical type]. It is a group with
// two required fields, "metadata" and "value", both byte arrays.
//
// *Experimental*: Support for the VARIANT type is still being developed and subject to
// change.
//
// Initial support does not attempt to process the variant data. So reading and writing
// data of this type behaves as if it were just a group with two byte array fields, as
// if the logical type annotation were absent. This may change in the future.
//
// [VARIANT logical type]: https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#variant
func Variant() Node {
	return variantNode{Group{"metadata": Required(Leaf(ByteArrayType)), "value": Required(Leaf(ByteArrayType))}}
}

// ShreddedVariant constructs a node of shredded [VARIANT logical type]. It is a group
// with a required byte array "metadata" field, an optional byte array "value" field
// (for any unshredded values), and an optional "typed_value" field whose type is that
// of the shredded value. If the given node is a group or list (or contains a list),
// the resulting "typed_value" field will have some additional structure to allow
// each group or element in a list to have a mix of shredded and unshredded data.
//
// The given node may only contain types that map to valid [variant value types].
// Therefore, it may not contain ENUM, FLOAT16, INTERVAL, JSON, BSON, VARIANT,
// GEOMETRY, GEOGRAPHY, MAP, or UNKNOWN logical types. It may only use signed INT
// logical types. It may only use DECIMAL logical types whose precision is less than
// or equal to 38. It may not contain any repeated fields unless they are the middle
// level of a 3-level LIST logical type. Any "required" settings on fields will be
// ignored: shredded fields must always be optional to represent values that may not
// conform to the shredded type. It also may not contain empty groups: any groups
// must have at least one field.
//
// More information on shredded variants can be found in the [Parquet documentation].
//
// *Experimental*: Support for the VARIANT type is still being developed and subject to
// change.
//
// Initial support does not attempt to process the variant data. So reading and writing
// data of this type behaves as if it were just a group with the three described fields,
// as if the logical type annotation were absent. This may change in the future.
//
// [VARIANT logical type]: https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#variant
// [variant value types]: https://github.com/apache/parquet-format/blob/master/VariantShredding.md#shredded-value-types
// [Parquet documentation]: https://github.com/apache/parquet-format/blob/master/VariantShredding.md
func ShreddedVariant(shreddedType Node) (Node, error) {
	typedNode, err := variantTypedValueNode(shreddedType)
	if err != nil {
		return nil, err
	}
	return variantNode{Group{
		"metadata":    Required(Leaf(ByteArrayType)),
		"value":       Optional(Leaf(ByteArrayType)),
		"typed_value": Optional(typedNode),
	}}, nil
}

func variantTypedValueNode(node Node, fieldPath ...string) (typed Node, err error) {
	defer func() {
		if err != nil && len(fieldPath) > 0 {
			err = fmt.Errorf("field %s: %w", strings.Join(fieldPath, "."), err)
		}
	}()
	if node.Repeated() {
		return nil, errors.New("repeated types are not allowed unless they are part of a 3-level LIST logical type")
	}
	if lt := node.Type().LogicalType(); lt != nil {
		switch lt := getLogicalType(lt).(type) {
		case *format.ListType:
			// We must first extract the inner list.element field of the 3-level LIST type.
			children := node.Fields()
			if len(children) != 1 {
				return nil, fmt.Errorf("invalid LIST logical type: expecting a single 'list' field but instead found %d", len(children))
			}
			grandchildren := children[0].Fields()
			if len(grandchildren) != 1 {
				return nil, fmt.Errorf("invalid LIST logical type: expecting a single 'element' field but instead found %d", len(grandchildren))
			}
			elementNode, err := variantTypedValueNode(grandchildren[0], fieldPath...)
			if err != nil {
				return nil, err
			}
			list := List(Required(Group{
				"value":       Optional(Leaf(ByteArrayType)),
				"typed_value": Optional(elementNode),
			}))
			if node.ID() != 0 {
				return FieldID(list, node.ID()), nil
			}
			return list, nil
		case *format.IntType:
			if !lt.IsSigned {
				return nil, errors.New("signed INT logical types are not allowed")
			}
		case *format.DecimalType:
			if lt.Precision > 38 {
				return nil, fmt.Errorf("DECIMAL logical types with precision >38 are not allowed (got %d)", lt.Precision)
			}
		case *format.DateType:
		case *format.TimeType:
		case *format.TimestampType:
		case *format.StringType:
		case *format.UUIDType:
		default:
			// No other logical types are allowed.
			return nil, fmt.Errorf("%s logical types are not allowed", lt)
		}
	}
	if node.Leaf() {
		return node, nil
	}
	children := node.Fields()
	if len(children) == 0 {
		return nil, errors.New("empty groups are not allowed")
	}
	group := make(Group, len(children))
	for _, child := range children {
		childNode, err := variantTypedValueNode(child, append(fieldPath, child.Name())...)
		if err != nil {
			return nil, err
		}
		group[child.Name()] = Required(Group{
			"value":       Optional(Leaf(ByteArrayType)),
			"typed_value": Optional(childNode),
		})
	}
	if node.ID() != 0 {
		return FieldID(group, node.ID()), nil
	}
	return group, nil
}

func getLogicalType(lt *format.LogicalType) any {
	// We use reflection so we can always catch a logical type annotation. If a
	// new one is added, we don't have to remember to update the switch above (unless
	// a new one is added that is also supported as a variant value).
	refVal := reflect.Indirect(reflect.ValueOf(lt))
	for i := range refVal.NumField() {
		field := refVal.Field(i)
		if field.CanInterface() && !field.IsZero() {
			return field.Interface()
		}
	}
	return nil
}

type variantNode struct{ Group }

func (variantNode) Type() Type { return &variantType{} }

type variantType format.VariantType

var variantLogicalType = format.LogicalType{Variant: new(format.VariantType)}

func (t *variantType) String() string { return (*format.VariantType)(t).String() }

func (t *variantType) Kind() Kind { panic("cannot call Kind on parquet VARIANT type") }

func (t *variantType) Length() int { return 0 }

func (t *variantType) EstimateSize(int) int { return 0 }

func (t *variantType) EstimateNumValues(int) int { return 0 }

func (t *variantType) Compare(Value, Value) int {
	panic("cannot compare values on parquet VARIANT type")
}

func (t *variantType) ColumnOrder() *format.ColumnOrder { return nil }

func (t *variantType) PhysicalType() *format.Type { return nil }

func (t *variantType) LogicalType() *format.LogicalType { return &variantLogicalType }

func (t *variantType) ConvertedType() *deprecated.ConvertedType { return nil }

func (t *variantType) NewColumnIndexer(int) ColumnIndexer {
	panic("create create column indexer from parquet VARIANT type")
}

func (t *variantType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet VARIANT type")
}

func (t *variantType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet VARIANT type")
}

func (t *variantType) NewPage(int, int, encoding.Values) Page {
	panic("cannot create page from parquet VARIANT type")
}

func (t *variantType) NewValues(values []byte, _ []uint32) encoding.Values {
	panic("cannot create values from parquet VARIANT type")
}

func (t *variantType) Encode(_ []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	panic("cannot encode parquet VARIANT type")
}

func (t *variantType) Decode(_ encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	panic("cannot decode parquet VARIANT type")
}

func (t *variantType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	panic("cannot estimate decode size of parquet VARIANT type")
}

func (t *variantType) AssignValue(reflect.Value, Value) error {
	panic("cannot assign value to a parquet VARIANT type")
}

func (t *variantType) ConvertValue(Value, Type) (Value, error) {
	panic("cannot convert value to a parquet VARIANT type")
}
