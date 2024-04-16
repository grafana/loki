package detected

import (
	"github.com/axiomhq/hyperloglog"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type UnmarshaledDetectedField struct {
	Label  string
	Type   logproto.DetectedFieldType
	Sketch *hyperloglog.Sketch
}

func UnmarshalDetectedField(f *logproto.DetectedField) (*UnmarshaledDetectedField, error) {
	sketch := hyperloglog.New()
	err := sketch.UnmarshalBinary(f.Sketch)
	if err != nil {
		return nil, err
	}

	return &UnmarshaledDetectedField{
		Label:  f.Label,
		Type:   f.Type,
		Sketch: sketch,
	}, nil
}

func (f *UnmarshaledDetectedField) Merge(df *logproto.DetectedField) error {
	sketch := hyperloglog.New()
	err := sketch.UnmarshalBinary(df.Sketch)
	if err != nil {
		return err
	}

	return f.Sketch.Merge(sketch)
}

func MergeFields(
	fields []*logproto.DetectedField,
	fieldLimit uint32,
) ([]*logproto.DetectedField, error) {
	mergedFields := make(map[string]*UnmarshaledDetectedField, fieldLimit)
	foundFields := uint32(0)

	for _, field := range fields {
		if field == nil {
			continue
		}

		// TODO(twhitney): this will take the first N up to limit, is there a better
		// way to rank the fields to make sure we get the most interesting ones?
		f, ok := mergedFields[field.Label]
		if !ok && foundFields < fieldLimit {
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

	result := make([]*logproto.DetectedField, 0, fieldLimit)
	for _, field := range mergedFields {
		// TODO(twhitney): what's the performance cost of marshalling here? We technically don't need to marshal in the merge
		// but it's nice to keep the response consistent through middlewares in case we need the sketch somewhere else,
		// need to benchmark this to find out.
		sketch, err := field.Sketch.MarshalBinary()
		if err != nil {
			return nil, err
		}
		detectedField := &logproto.DetectedField{
			Label:       field.Label,
			Type:        field.Type,
			Cardinality: field.Sketch.Estimate(),
			Sketch:      sketch,
		}
		result = append(result, detectedField)
	}

	return result, nil
}
