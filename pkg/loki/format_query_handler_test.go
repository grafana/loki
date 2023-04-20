package loki

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_formatQueryHandlerResponse(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		expected FormatQueryResponse
	}{
		{
			name:  "happy-path",
			query: `{foo="bar"}`,
			expected: FormatQueryResponse{
				Status: "success",
				Data:   `{foo="bar"}`,
			},
		},
		{
			name:  "invalid-query",
			query: `{foo="bar}`,
			expected: FormatQueryResponse{
				Status: "invalid-query",
				Err:    "parse error at line 1, col 6: literal not terminated",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:808?query=%s", tc.query), nil)
			require.NoError(t, err)

			w := httptest.NewRecorder()

			formatQueryHandler()(w, req)

			var got FormatQueryResponse

			err = json.NewDecoder(w.Body).Decode(&got)
			require.NoError(t, err)

			assert.Equal(t, tc.expected, got)
		})
	}
}
