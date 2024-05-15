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
			Parsers:     []string{"logfmt", "json"},
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
			Parsers:     []string{"json"},
		},
		{
			Label:       "baz",
			Type:        logproto.DetectedFieldBoolean,
			Cardinality: 3,
			Sketch:      marhsalledOtherFooSketch,
		},
		{
			Label:       "baz",
			Type:        logproto.DetectedFieldFloat,
			Cardinality: 3,
			Sketch:      marhsalledOtherFooSketch,
		},
	}

	limit := uint32(3)

	t.Run("merges fields", func(t *testing.T) {
		result, err := MergeFields(fields, limit)
		require.NoError(t, err)
		assert.Equal(t, 3, len(result))
		var foo *logproto.DetectedField
		var baz *logproto.DetectedField

		for _, field := range result {
			if field.Label == "foo" {
				foo = field
			}
			if field.Label == "baz" {
				baz = field
			}
		}

		assert.Equal(t, logproto.DetectedFieldString, foo.Type)
		assert.Equal(t, uint64(3), foo.Cardinality)
		assert.Equal(t, []string{"json", "logfmt"}, foo.Parsers)

		assert.Equal(t, logproto.DetectedFieldString, baz.Type)
	})

	t.Run("returns up to limit number of fields", func(t *testing.T) {
		lowLimit := uint32(1)
		result, err := MergeFields(fields, lowLimit)
		require.NoError(t, err)
		assert.Equal(t, 1, len(result))

		highLimit := uint32(4)
		result, err = MergeFields(fields, highLimit)
		require.NoError(t, err)
		assert.Equal(t, 3, len(result))
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
