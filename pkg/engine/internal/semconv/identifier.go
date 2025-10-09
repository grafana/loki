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
	ColumnIdentValue        = NewIdentifier("value", types.ColumnTypeGenerated, types.Loki.Integer)
	ColumnIdentError        = NewIdentifier("__error__", types.ColumnTypeGenerated, types.Loki.String)
	ColumnIdentErrorDetails = NewIdentifier("__error_details__", types.ColumnTypeGenerated, types.Loki.String)
)

// NewIdentifier creates a new column identifier from given name, column type, and data type.
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
}

func (i *Identifier) DataType() types.DataType {
	return i.dataType
}

func (i *Identifier) ColumnType() types.ColumnType {
	return i.columnType
}

func (i *Identifier) Name() string {
	return i.columnName
}

func (i *Identifier) Scope() Scope {
	return ScopeForColumnType(i.columnType)
}

func (i *Identifier) String() string {
	return fmt.Sprintf("%s.%s.%s", i.dataType, i.columnType, i.columnName)
}

// FQN returns the fully qualified name of the identifier.
func (i *Identifier) FQN() string {
	return i.String()
}

func (i *Identifier) Equal(other *Identifier) bool {
	if i == nil || other == nil {
		return false
	}
	return i.columnName == other.columnName &&
		i.columnType == other.columnType &&
		i.dataType == other.dataType
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

type scopeOrigin string
type scopeType string

// Scope describes the origin and type of an identifier.
type Scope struct {
	Origin scopeOrigin
	Type   scopeType
}

func (s Scope) String() string {
	if s.Origin == InvalidOrigin || s.Type == InvalidType {
		return invalid
	}
	return fmt.Sprintf("%s_%s", s.Origin, s.Type)
}

const (
	Resource  = scopeOrigin("resource")
	Record    = scopeOrigin("record")
	Generated = scopeOrigin("generated")
	Unscoped  = scopeOrigin("unscoped")

	Attribute = scopeType("attr")
	Builtin   = scopeType("builtin")

	InvalidOrigin = scopeOrigin("")
	InvalidType   = scopeType("")
)

// ScopeForColumnType converts a given [types.ColumnType] into a [Scope].
func ScopeForColumnType(value types.ColumnType) Scope {
	switch value {
	case types.ColumnTypeLabel:
		return Scope{Resource, Attribute}
	case types.ColumnTypeBuiltin:
		return Scope{Record, Builtin}
	case types.ColumnTypeMetadata:
		return Scope{Record, Attribute}
	case types.ColumnTypeParsed:
		return Scope{Generated, Attribute}
	case types.ColumnTypeGenerated:
		return Scope{Generated, Builtin}
	case types.ColumnTypeAmbiguous:
		return Scope{Unscoped, Attribute}
	default:
		return Scope{}
	}
}
