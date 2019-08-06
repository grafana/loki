package queryrange

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkMarshalling(b *testing.B) {
	jsonfile := os.Getenv("JSON")
	buf, err := ioutil.ReadFile(jsonfile)
	require.NoError(b, err)

	for n := 0; n < b.N; n++ {
		apiResp, err := parseResponse(context.Background(), &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewReader(buf)),
		})
		require.NoError(b, err)

		resp, err := apiResp.toHTTPResponse(context.Background())
		require.NoError(b, err)

		buf2, err := ioutil.ReadAll(resp.Body)
		require.NoError(b, err)
		require.Equal(b, string(buf), string(buf2))
	}
}
