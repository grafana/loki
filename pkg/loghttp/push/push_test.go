package push

import (
	"bytes"
	"compress/gzip"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	util_log "github.com/grafana/loki/pkg/util/log"
)

// GZip source string and return compressed string
func gzipString(source string) string {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(source)); err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	return buf.String()
}

func TestParseRequest(t *testing.T) {
	tests := []struct {
		path            string
		body            string
		contentType     string
		contentEncoding string
		valid           bool
	}{
		{
			path:        `/loki/api/v1/push`,
			body:        ``,
			contentType: `application/json`,
			valid:       false,
		},
		{
			path:        `/loki/api/v1/push`,
			body:        `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType: ``,
			valid:       false,
		},
		{
			path:        `/loki/api/v1/push`,
			body:        `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType: `application/json`,
			valid:       true,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:     `application/json`,
			contentEncoding: ``,
			valid:           true,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            gzipString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json`,
			contentEncoding: `gzip`,
			valid:           true,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            gzipString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json`,
			contentEncoding: `snappy`,
			valid:           false,
		},
	}

	// Testing input array
	for index, test := range tests {
		request := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
		if len(test.contentType) > 0 {
			request.Header.Add("Content-Type", test.contentType)
		}
		if len(test.contentEncoding) > 0 {
			request.Header.Add("Content-Encoding", test.contentEncoding)
		}
		data, err := ParseRequest(util_log.Logger, "", request, nil)
		if test.valid {
			assert.Nil(t, err, "Should not give error for %d", index)
			assert.NotNil(t, data, "Should give data for %d", index)
		} else {
			assert.NotNil(t, err, "Should give error for %d", index)
			assert.Nil(t, data, "Should not give data for %d", index)
		}
	}
}
