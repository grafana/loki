package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/cespare/xxhash/v2"

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

// labelsCache returns labels and label values for a given row in range and vector aggregators, but cache them in order
// to reduce object allocations for repeated label sets. It first scans the row for non-empty labels and computes xxhash.
// In case of a cache miss it scans the row again and allocates arrays for label names and label values.
type labelsCache struct {
	digest *xxhash.Digest
	cache  map[uint64]struct {
		labels      []arrow.Field
		labelValues []string
	}
}

func newLabelsCache() *labelsCache {
	return &labelsCache{
		digest: xxhash.New(),
		cache: make(map[uint64]struct {
			labels      []arrow.Field
			labelValues []string
		}),
	}
}

func (c *labelsCache) getLabels(arrays []*array.String, fields []arrow.Field, row int) ([]arrow.Field, []string) {
	c.digest.Reset()

	for i, arr := range arrays {
		val := arr.Value(row)
		if val != "" {
			_, _ = c.digest.Write([]byte{0})
			_, _ = c.digest.WriteString(fields[i].Name)
			_, _ = c.digest.Write([]byte("="))
			_, _ = c.digest.WriteString(val)

		}
	}
	key := c.digest.Sum64()

	l, ok := c.cache[key]
	if !ok {
		l = struct {
			labels      []arrow.Field
			labelValues []string
		}{
			labels:      make([]arrow.Field, 0),
			labelValues: make([]string, 0),
		}
		for i, arr := range arrays {
			val := arr.Value(row)
			if val != "" {
				l.labelValues = append(l.labelValues, val)
				l.labels = append(l.labels, fields[i])
			}
		}
		c.cache[key] = l
	}

	return l.labels, l.labelValues
}
