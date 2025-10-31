package semconv

import (
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

const (
	invalid = "<invalid>"
)

var (
	ColumnIdentMessage      = NewIdentifier("message", types.ColumnTypeBuiltin, types.Loki.String)
	ColumnIdentTimestamp    = NewIdentifier("timestamp", types.ColumnTypeBuiltin, types.Loki.Timestamp)
	ColumnIdentValue        = NewIdentifier("value", types.ColumnTypeGenerated, types.Loki.Float)
	ColumnIdentError        = NewIdentifier("__error__", types.ColumnTypeGenerated, types.Loki.String)
	ColumnIdentErrorDetails = NewIdentifier("__error_details__", types.ColumnTypeGenerated, types.Loki.String)
)

// NewIdentifier creates a new column identifier from given name, column type, and data type.
// The semantic type of an identifier is derived from its column type.
func NewIdentifier(name string, ct types.ColumnType, dt types.DataType) *Identifier {
	return &Identifier{
		columnName: name,
		columnType: ct,
		dataType:   dt,
	}
}

// Identifier is the unique identifier of a column.
// The uniqueness of a column is defined by its data type, column type (scope), and name.
// The fully qualified name is represented as [DATA_TYPE].[COLUMN_TYPE].[COLUMN_NAME].
type Identifier struct {
	columnName string
	columnType types.ColumnType
	dataType   types.DataType

	// fqn is the cached fully qualified name to avoid allocations when calling FQN() multiple times
	fqn string
}

// DataType returns the Loki data type of the identifier.
func (i *Identifier) DataType() types.DataType {
	return i.dataType
}

// ColumnType returns the column type of the identifier.
func (i *Identifier) ColumnType() types.ColumnType {
	return i.columnType
}

// SemType returns the semantic type of the identifier.
func (i *Identifier) SemType() SemanticType {
	return SemTypeForColumnType(i.columnType)
}

// SemName returns the semantic name of the column identifier, defined as
// [ORIGIN].[NAME] in case of attributes
// [ORIGIN]:[NAME] in case of builtins
func (i *Identifier) SemName() string {
	semTy := SemTypeForColumnType(i.columnType)
	switch semTy.Type {
	case Attribute:
		return fmt.Sprintf("%s.%s", semTy.Origin, i.columnName)
	case Builtin:
		return fmt.Sprintf("%s:%s", semTy.Origin, i.columnName)
	default:
		return invalid
	}
}

// ShortName returns the non-unique name part of the column identifier.
func (i *Identifier) ShortName() string {
	return i.columnName
}

// String returns the string representation of the column.
// This must not be used as name for [arrow.Field].
func (i *Identifier) String() string {
	return fmt.Sprintf("%s[%s]", i.SemName(), i.dataType)
}

// FQN returns the fully qualified name of the identifier.
func (i *Identifier) FQN() string {
	if i.fqn == "" {
		i.fqn = fmt.Sprintf("%s.%s.%s", i.dataType, i.columnType, i.columnName)
	}
	return i.fqn
}

// Equal checks equality of of the identifier against a second identifier.
func (i *Identifier) Equal(other *Identifier) bool {
	if i == nil || other == nil {
		return false
	}
	return i.columnName == other.columnName &&
		i.columnType == other.columnType &&
		i.dataType == other.dataType
}

func (i *Identifier) ColumnRef() types.ColumnRef {
	return types.ColumnRef{
		Column: i.ShortName(),
		Type:   i.columnType,
	}
}

// FQN returns a fully qualified name for a column by given name, column type, and data type.
func FQN(name string, ct types.ColumnType, dt types.DataType) string {
	return NewIdentifier(name, ct, dt).FQN()
}

// ParseFQN returns an [Identifier] from the given fully qualified name.
// It returns an error if the name cannot be parsed.
func ParseFQN(fqn string) (*Identifier, error) {
	if fqn == "" {
		return nil, errors.New("empty identifier")
	}

	dt, rest, found := strings.Cut(fqn, ".")
	if !found {
		return nil, errors.New("missing data type separator")
	}
	if rest == "" {
		return nil, errors.New("missing column type")
	}

	dataType, err := types.FromString(dt)
	if err != nil {
		return nil, err
	}

	ct, columnName, found := strings.Cut(rest, ".")
	if !found {
		return nil, errors.New("missing column type separator")
	}
	if columnName == "" {
		return nil, errors.New("missing column name")
	}
	columnType := types.ColumnTypeFromString(ct)

	return &Identifier{
		columnName: columnName,
		columnType: columnType,
		dataType:   dataType,
		fqn:        fqn,
	}, nil
}

// ParseFQN returns an [Identifier] from the given fully qualified name.
// It panics if the name cannot be parsed.
func MustParseFQN(fqn string) *Identifier {
	ident, err := ParseFQN(fqn)
	if err != nil {
		panic(fmt.Sprintf("parsing fqn: %s", err))
	}
	return ident
}

// do not export this type, because we don't want to allow creating new instances outside of this package
type semOrigin string

// do not export this type, because we don't want to allow creating new instances outside of this package
type semType string

// SemanticType describes the origin and type of an identifier.
type SemanticType struct {
	Origin semOrigin
	Type   semType
}

func (s SemanticType) String() string {
	if s.Origin == InvalidOrigin || s.Type == InvalidType {
		return invalid
	}
	return fmt.Sprintf("%s_%s", s.Origin, s.Type)
}

const (
	Resource  = semOrigin("resource")
	Record    = semOrigin("record")
	Generated = semOrigin("generated")
	Unscoped  = semOrigin("unscoped")

	Attribute = semType("attr")
	Builtin   = semType("builtin")

	InvalidOrigin = semOrigin("")
	InvalidType   = semType("")
)

// SemTypeForColumnType converts a given [types.ColumnType] into a [SemanticType].
func SemTypeForColumnType(value types.ColumnType) SemanticType {
	switch value {
	case types.ColumnTypeLabel:
		return SemanticType{Resource, Attribute}
	case types.ColumnTypeBuiltin:
		return SemanticType{Record, Builtin}
	case types.ColumnTypeMetadata:
		return SemanticType{Record, Attribute}
	case types.ColumnTypeParsed:
		return SemanticType{Generated, Attribute}
	case types.ColumnTypeGenerated:
		return SemanticType{Generated, Builtin}
	case types.ColumnTypeAmbiguous:
		return SemanticType{Unscoped, Attribute}
	default:
		return SemanticType{}
	}
}
