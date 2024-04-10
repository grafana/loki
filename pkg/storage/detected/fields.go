package detected

import (
	"github.com/grafana/loki/v3/pkg/logproto"
)

func MergeFields(fields []*logproto.DetectedField, limit uint32) []*logproto.DetectedField {
	mergedFields := make(map[string]*logproto.DetectedField, limit)
	foundFields := uint32(0)

	for _, field := range fields {
		if field == nil {
			continue
		}

		//TODO(twhitney): this will take the first N up to limit, is there a better
		//way to rank the fields to make sure we get the most interesting ones?
		f, ok := mergedFields[field.Label]
		if !ok && foundFields < limit {
			mergedFields[field.Label] = field
			foundFields++
			continue
		}

		//seeing the same field again, update the cardinality if it's higher
		//this is an estimation, as the true cardinality could be greater
		//than either of the seen values, but will never be less
		if ok {
			curCard, newCard := f.Cardinality, field.Cardinality
			if newCard > curCard {
				f.Cardinality = newCard
			}
		}
	}

	result := make([]*logproto.DetectedField, 0, limit)
	for _, field := range mergedFields {
		result = append(result, field)
	}

	return result
}
