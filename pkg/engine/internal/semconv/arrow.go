package semconv

import (
	"github.com/apache/arrow-go/v18/arrow"
)

func FieldFromIdent(ident *Identifier, nullable bool) arrow.Field {
	return arrow.Field{
		Name:     ident.FQN(),
		Type:     ident.dataType.ArrowType(),
		Nullable: nullable,
	}
}

func FieldFromFQN(fqn string, nullable bool) arrow.Field {
	ident := MustParseFQN(fqn)
	return FieldFromIdent(ident, nullable)
}
