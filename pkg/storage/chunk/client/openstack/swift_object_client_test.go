package openstack

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/storage/bucket/swift"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
)

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestSwiftConfig_UnmarshalYAML(t *testing.T) {
	in := []byte(`container_name: foobar
request_timeout: 30s
`)

	dst := &SwiftConfig{}
	require.NoError(t, yaml.UnmarshalStrict(in, dst))
	require.Equal(t, "foobar", dst.ContainerName)

	// set defaults
	require.Equal(t, 10*time.Second, dst.ConnectTimeout)

	// override defaults
	require.Equal(t, 30*time.Second, dst.RequestTimeout)
}

func Test_Hedging(t *testing.T) {
	for _, tc := range []struct {
		name          string
		expectedCalls int32
		hedgeAt       time.Duration
		upTo          int
		do            func(c *SwiftObjectClient)
	}{
		{
			"delete/put/list are not hedged",
			3,
			20 * time.Nanosecond,
			10,
			func(c *SwiftObjectClient) {
				_ = c.DeleteObject(context.Background(), "foo")
				_, _, _ = c.List(context.Background(), "foo", "/")
				_ = c.PutObject(context.Background(), "foo", bytes.NewReader([]byte("bar")))
			},
		},
		{
			"gets are hedged",
			3,
			20 * time.Nanosecond,
			3,
			func(c *SwiftObjectClient) {
				_, _, _ = c.GetObject(context.Background(), "foo")
			},
		},
		{
			"gets are not hedged when not configured",
			1,
			0,
			0,
			func(c *SwiftObjectClient) {
				_, _, _ = c.GetObject(context.Background(), "foo")
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			count := atomic.NewInt32(0)
			// hijack the transport to count the number of calls
			defaultTransport = RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				// fake auth
				if req.Header.Get("X-Auth-Key") == "passwd" {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       http.NoBody,
						Header: http.Header{
							"X-Storage-Url": []string{"http://swift.example.com/v1/AUTH_test"},
							"X-Auth-Token":  []string{"token"},
						},
					}, nil
				}
				// fake container creation
				if req.Method == "PUT" && req.URL.Path == "/v1/AUTH_test/foo" {
					return &http.Response{
						StatusCode: http.StatusCreated,
						Body:       http.NoBody,
					}, nil
				}
				count.Inc()
				time.Sleep(200 * time.Millisecond)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       http.NoBody,
				}, nil
			})

			c, err := NewSwiftObjectClient(SwiftConfig{
				Config: swift.Config{
					MaxRetries:     1,
					ContainerName:  "foo",
					AuthVersion:    1,
					Password:       "passwd",
					ConnectTimeout: 10 * time.Second,
					RequestTimeout: 10 * time.Second,
				},
			}, hedging.Config{
				At:           tc.hedgeAt,
				UpTo:         tc.upTo,
				MaxPerSecond: 1000,
			})
			require.NoError(t, err)
			tc.do(c)
			require.Equal(t, tc.expectedCalls, count.Load())
		})
	}
}
