package detected

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
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
		result, err := MergeFields(fields, limit)
		require.NoError(t, err)
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
		result, err := MergeFields(fields, limit)
		require.NoError(t, err)
		assert.Equal(t, 1, len(result))

		limit = uint32(4)
		result, err = MergeFields(fields, limit)
		require.NoError(t, err)
		assert.Equal(t, 2, len(result))
	})
}
