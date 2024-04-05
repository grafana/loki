package detected

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/stretchr/testify/assert"
)

func Test_MergeFields(t *testing.T) {
	fields := []*logproto.DetectedField{
		{
			Label:       "foo",
			Type:        logproto.DetectedFieldString,
			Cardinality: 1,
		},
		{
			Label:       "bar",
			Type:        logproto.DetectedFieldBoolean,
			Cardinality: 2,
		},
		{
			Label:       "foo",
			Type:        logproto.DetectedFieldString,
			Cardinality: 3,
		},
	}

	t.Run("merges fields, taking the highest cardinality", func(t *testing.T) {
		limit := uint32(3)
		result := MergeFields(fields, limit)
		assert.Equal(t, 2, len(result))
		var foo *logproto.DetectedField

		for _, field := range result {
			if field.Label == "foo" {
				foo = field
			}
		}

		assert.Equal(t, logproto.DetectedFieldString, foo.Type)
		assert.Equal(t, uint64(3), foo.Cardinality)
	})

	t.Run("returns up to limit number of fields", func(t *testing.T) {
		limit := uint32(1)
		result := MergeFields(fields, limit)
		assert.Equal(t, 1, len(result))

		limit = uint32(4)
		result = MergeFields(fields, limit)
		assert.Equal(t, 2, len(result))
	})
}
