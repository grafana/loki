package detected

import (
	"slices"

	"github.com/axiomhq/hyperloglog"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type UnmarshaledDetectedField struct {
	Label   string
	Type    logproto.DetectedFieldType
	Parsers []string
	Sketch  *hyperloglog.Sketch
}

func UnmarshalDetectedField(f *logproto.DetectedField) (*UnmarshaledDetectedField, error) {
	sketch := hyperloglog.New()
	err := sketch.UnmarshalBinary(f.Sketch)
	if err != nil {
		return nil, err
	}

	return &UnmarshaledDetectedField{
		Label:   f.Label,
		Type:    f.Type,
		Parsers: f.Parsers,
		Sketch:  sketch,
	}, nil
}

func (f *UnmarshaledDetectedField) Merge(df *logproto.DetectedField) error {
	sketch := hyperloglog.New()
	err := sketch.UnmarshalBinary(df.Sketch)
	if err != nil {
		return err
	}

	if f.Type != df.Type {
		f.Type = logproto.DetectedFieldString
	}

	f.Parsers = append(f.Parsers, df.Parsers...)
	slices.Sort(f.Parsers)
	f.Parsers = slices.Compact(f.Parsers)
	if len(f.Parsers) == 0 {
		f.Parsers = nil
	}

	return f.Sketch.Merge(sketch)
}

func MergeFields(
	fields []*logproto.DetectedField,
	limit uint32,
) ([]*logproto.DetectedField, error) {
	mergedFields := make(map[string]*UnmarshaledDetectedField, limit)
	foundFields := uint32(0)

	for _, field := range fields {
		if field == nil {
			continue
		}

		// TODO(twhitney): this will take the first N up to limit, is there a better
		// way to rank the fields to make sure we get the most interesting ones?
		f, ok := mergedFields[field.Label]
		if !ok && foundFields < limit {
			unmarshaledField, err := UnmarshalDetectedField(field)
			if err != nil {
				return nil, err
			}

			mergedFields[field.Label] = unmarshaledField
			foundFields++
			continue
		}

		if ok {
			// seeing the same field again, merge it with the existing one
			err := f.Merge(field)
			if err != nil {
				return nil, err
			}
		}
	}

	result := make([]*logproto.DetectedField, 0, limit)
	for _, field := range mergedFields {
		detectedField := &logproto.DetectedField{
			Label:       field.Label,
			Type:        field.Type,
			Cardinality: field.Sketch.Estimate(),
			Parsers:     field.Parsers,
			Sketch:      nil,
		}
		result = append(result, detectedField)
	}

	return result, nil
}

func MergeValues(
	values []string,
	limit uint32,
) ([]string, error) {
	mergedValues := make(map[string]struct{}, limit)

	for _, value := range values {
		if value == "" {
			continue
		}

		if len(mergedValues) >= int(limit) {
			break
		}

		mergedValues[value] = struct{}{}
	}

	result := make([]string, 0, limit)
	for value := range mergedValues {
		result = append(result, value)
	}

	return result, nil
}
