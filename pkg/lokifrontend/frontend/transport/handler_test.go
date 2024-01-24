package transport

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatRequestHeaders(t *testing.T) {
	h := http.Header{}
	h.Add("X-Header-To-Log", "i should be logged!")
	h.Add("X-Header-To-Not-Log", "i shouldn't be logged!")

	fields := formatRequestHeaders(&h, []string{"X-Header-To-Log", "X-Header-Not-Present"})

	expected := []interface{}{
		"header_x_header_to_log",
		"i should be logged!",
	}

	require.Equal(t, expected, fields)
}
