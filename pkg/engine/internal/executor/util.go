package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
)

// columnForIdent returns the column ([arrow.Array]) and its column index in the schema of the given input batch ([arrow.RecordBatch]).
// It returns an optional error in case the column with the fully qualified name of the identifier could not be found,
// or there are where multiple columns with the same name in the schema.
// In case of an error, the returned column index is -1.
func columnForIdent(ident *semconv.Identifier, batch arrow.RecordBatch) (arrow.Array, int, error) {
	return columnForFQN(ident.FQN(), batch)
}

func columnForFQN(fqn string, batch arrow.RecordBatch) (arrow.Array, int, error) {
	indices := batch.Schema().FieldIndices(fqn)
	if len(indices) == 0 {
		return nil, -1, fmt.Errorf("column not found for %s", fqn)
	}
	if len(indices) > 1 {
		return nil, -1, fmt.Errorf("multiple columns found for %s", fqn)
	}

	return batch.Column(indices[0]), indices[0], nil
}
