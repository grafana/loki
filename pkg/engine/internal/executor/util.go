package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
)

var (
	separator = []byte{0}
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

// labelValuesCache returns label values for a given row in range and vector aggregators, but cache them in order
// to reduce object allocations for repeated label sets. It first scans the row for non-empty labels and computes xxhash.
// In case of a cache miss it scans the row again and allocates arrays for label values.
type labelValuesCache struct {
	digest *xxhash.Digest
	cache  map[uint64][]string
}

func newLabelValuesCache() *labelValuesCache {
	return &labelValuesCache{
		digest: xxhash.New(),
		cache:  make(map[uint64][]string),
	}
}

func (c *labelValuesCache) getLabelValues(arrays []*array.String, row int) []string {
	c.digest.Reset()
	for _, arr := range arrays {
		val := arr.Value(row)
		if val != "" {
			_, _ = c.digest.Write(separator)
			_, _ = c.digest.WriteString(val)
		}
	}
	key := c.digest.Sum64()

	labelValues, ok := c.cache[key]
	if !ok {
		labelValues = make([]string, 0, len(arrays))

		for _, arr := range arrays {
			val := arr.Value(row)
			if val != "" {
				labelValues = append(labelValues, val)
			}
		}
		c.cache[key] = labelValues
	}

	return labelValues
}

// fieldsCache returns labels for a given row in range and vector aggregators, but cache them in order
// to reduce object allocations for repeated label sets. It first scans the row for non-empty labels and computes xxhash.
// In case of a cache miss it scans the row again and allocates arrays for label names.
type fieldsCache struct {
	digest *xxhash.Digest
	cache  map[uint64][]arrow.Field
}

func newFieldsCache() *fieldsCache {
	return &fieldsCache{
		digest: xxhash.New(),
		cache:  make(map[uint64][]arrow.Field),
	}
}

func (c *fieldsCache) getFields(arrays []*array.String, fields []arrow.Field, row int) []arrow.Field {
	c.digest.Reset()
	for i, arr := range arrays {
		val := arr.Value(row)
		if val != "" {
			_, _ = c.digest.Write(separator)
			_, _ = c.digest.WriteString(fields[i].Name)
		}
	}
	key := c.digest.Sum64()

	labels, ok := c.cache[key]
	if !ok {
		labels = make([]arrow.Field, 0, len(arrays))
		for i, arr := range arrays {
			val := arr.Value(row)
			if val != "" {
				labels = append(labels, fields[i])
			}
		}
		c.cache[key] = labels
	}

	return labels
}
