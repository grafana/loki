package util

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryStringBuilder(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input           map[string]interface{}
		expectedEncoded string
	}{
		"should return an empty query string on no params": {
			input:           map[string]interface{}{},
			expectedEncoded: "",
		},
		"should return the URL encoded query string parameters": {
			input: map[string]interface{}{
				"float32":    float32(123.456),
				"float64":    float64(123.456),
				"float64int": float64(12345.0),
				"int32":      32,
				"int64":      int64(64),
				"string":     "foo",
			},
			expectedEncoded: "float32=123.456&float64=123.456&float64int=12345&int32=32&int64=64&string=foo",
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			params := NewQueryStringBuilder()

			for name, value := range testData.input {
				switch value := value.(type) {
				case string:
					params.SetString(name, value)
				case float32:
					params.SetFloat32(name, value)
				case float64:
					params.SetFloat(name, value)
				case int:
					params.SetInt32(name, value)
				case int64:
					params.SetInt(name, value)
				default:
					require.Fail(t, fmt.Sprintf("Unknown data type for test fixture with name '%s'", name))
				}
			}

			assert.Equal(t, testData.expectedEncoded, params.Encode())
		})
	}
}
