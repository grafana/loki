package detected

import (
	"testing"

	"github.com/axiomhq/hyperloglog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func Test_MergeFields(t *testing.T) {
	fooSketch := hyperloglog.New()
	fooSketch.Insert([]byte("bar"))
	marshalledFooSketch, err := fooSketch.MarshalBinary()
	require.NoError(t, err)

	barSketch := hyperloglog.New()
	barSketch.Insert([]byte("baz"))
	marshalledBarSketch, err := barSketch.MarshalBinary()
	require.NoError(t, err)

	otherFooSketch := hyperloglog.New()
	otherFooSketch.Insert([]byte("bar"))
	otherFooSketch.Insert([]byte("baz"))
	otherFooSketch.Insert([]byte("qux"))
	marhsalledOtherFooSketch, err := otherFooSketch.MarshalBinary()
	require.NoError(t, err)

	fields := []*logproto.DetectedField{
		{
			Label:       "foo",
			Type:        logproto.DetectedFieldString,
			Cardinality: 1,
			Sketch:      marshalledFooSketch,
			Parser:      "logfmt",
		},
		{
			Label:       "bar",
			Type:        logproto.DetectedFieldBoolean,
			Cardinality: 2,
			Sketch:      marshalledBarSketch,
		},
		{
			Label:       "foo",
			Type:        logproto.DetectedFieldString,
			Cardinality: 3,
			Sketch:      marhsalledOtherFooSketch,
		},
	}

	limit := uint32(3)

	t.Run("merges fields", func(t *testing.T) {
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
		assert.Equal(t, "logfmt", foo.Parser)
	})

	t.Run("returns up to limit number of fields", func(t *testing.T) {
		lowLimit := uint32(1)
		result, err := MergeFields(fields, lowLimit)
		require.NoError(t, err)
		assert.Equal(t, 1, len(result))

		highLimit := uint32(4)
		result, err = MergeFields(fields, highLimit)
		require.NoError(t, err)
		assert.Equal(t, 2, len(result))
	})

	t.Run("returns an error when the field cannot be unmarshalled", func(t *testing.T) {
		badFields := []*logproto.DetectedField{
			{
				Label:       "bad",
				Type:        logproto.DetectedFieldBoolean,
				Cardinality: 42,
				Sketch:      []byte("bad"),
			},
		}
		_, err := MergeFields(badFields, limit)
		require.Error(t, err)
	})
}
