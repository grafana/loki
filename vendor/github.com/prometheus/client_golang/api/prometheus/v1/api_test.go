// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build go1.7

package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

type apiTest struct {
	do           func() (interface{}, error)
	inErr        error
	inStatusCode int
	inRes        interface{}

	reqPath   string
	reqParam  url.Values
	reqMethod string
	res       interface{}
	err       error
}

type apiTestClient struct {
	*testing.T
	curTest apiTest
}

func (c *apiTestClient) URL(ep string, args map[string]string) *url.URL {
	path := ep
	for k, v := range args {
		path = strings.Replace(path, ":"+k, v, -1)
	}
	u := &url.URL{
		Host: "test:9090",
		Path: path,
	}
	return u
}

func (c *apiTestClient) Do(ctx context.Context, req *http.Request) (*http.Response, []byte, error) {

	test := c.curTest

	if req.URL.Path != test.reqPath {
		c.Errorf("unexpected request path: want %s, got %s", test.reqPath, req.URL.Path)
	}
	if req.Method != test.reqMethod {
		c.Errorf("unexpected request method: want %s, got %s", test.reqMethod, req.Method)
	}

	b, err := json.Marshal(test.inRes)
	if err != nil {
		c.Fatal(err)
	}

	resp := &http.Response{}
	if test.inStatusCode != 0 {
		resp.StatusCode = test.inStatusCode
	} else if test.inErr != nil {
		resp.StatusCode = statusAPIError
	} else {
		resp.StatusCode = http.StatusOK
	}

	return resp, b, test.inErr
}

func TestAPIs(t *testing.T) {

	testTime := time.Now()

	client := &apiTestClient{T: t}

	promAPI := &httpAPI{
		client: client,
	}

	doAlertManagers := func() func() (interface{}, error) {
		return func() (interface{}, error) {
			return promAPI.AlertManagers(context.Background())
		}
	}

	doCleanTombstones := func() func() (interface{}, error) {
		return func() (interface{}, error) {
			return nil, promAPI.CleanTombstones(context.Background())
		}
	}

	doConfig := func() func() (interface{}, error) {
		return func() (interface{}, error) {
			return promAPI.Config(context.Background())
		}
	}

	doDeleteSeries := func(matcher string, startTime time.Time, endTime time.Time) func() (interface{}, error) {
		return func() (interface{}, error) {
			return nil, promAPI.DeleteSeries(context.Background(), []string{matcher}, startTime, endTime)
		}
	}

	doFlags := func() func() (interface{}, error) {
		return func() (interface{}, error) {
			return promAPI.Flags(context.Background())
		}
	}

	doLabelValues := func(label string) func() (interface{}, error) {
		return func() (interface{}, error) {
			return promAPI.LabelValues(context.Background(), label)
		}
	}

	doQuery := func(q string, ts time.Time) func() (interface{}, error) {
		return func() (interface{}, error) {
			return promAPI.Query(context.Background(), q, ts)
		}
	}

	doQueryRange := func(q string, rng Range) func() (interface{}, error) {
		return func() (interface{}, error) {
			return promAPI.QueryRange(context.Background(), q, rng)
		}
	}

	doSeries := func(matcher string, startTime time.Time, endTime time.Time) func() (interface{}, error) {
		return func() (interface{}, error) {
			return promAPI.Series(context.Background(), []string{matcher}, startTime, endTime)
		}
	}

	doSnapshot := func(skipHead bool) func() (interface{}, error) {
		return func() (interface{}, error) {
			return promAPI.Snapshot(context.Background(), skipHead)
		}
	}

	doTargets := func() func() (interface{}, error) {
		return func() (interface{}, error) {
			return promAPI.Targets(context.Background())
		}
	}

	queryTests := []apiTest{
		{
			do: doQuery("2", testTime),
			inRes: &queryResult{
				Type: model.ValScalar,
				Result: &model.Scalar{
					Value:     2,
					Timestamp: model.TimeFromUnix(testTime.Unix()),
				},
			},

			reqMethod: "GET",
			reqPath:   "/api/v1/query",
			reqParam: url.Values{
				"query": []string{"2"},
				"time":  []string{testTime.Format(time.RFC3339Nano)},
			},
			res: &model.Scalar{
				Value:     2,
				Timestamp: model.TimeFromUnix(testTime.Unix()),
			},
		},
		{
			do:    doQuery("2", testTime),
			inErr: fmt.Errorf("some error"),

			reqMethod: "GET",
			reqPath:   "/api/v1/query",
			reqParam: url.Values{
				"query": []string{"2"},
				"time":  []string{testTime.Format(time.RFC3339Nano)},
			},
			err: fmt.Errorf("some error"),
		},
		{
			do:           doQuery("2", testTime),
			inRes:        "some body",
			inStatusCode: 500,
			inErr: &Error{
				Type:   ErrServer,
				Msg:    "server error: 500",
				Detail: "some body",
			},

			reqMethod: "GET",
			reqPath:   "/api/v1/query",
			reqParam: url.Values{
				"query": []string{"2"},
				"time":  []string{testTime.Format(time.RFC3339Nano)},
			},
			err: errors.New("server_error: server error: 500"),
		},
		{
			do:           doQuery("2", testTime),
			inRes:        "some body",
			inStatusCode: 404,
			inErr: &Error{
				Type:   ErrClient,
				Msg:    "client error: 404",
				Detail: "some body",
			},

			reqMethod: "GET",
			reqPath:   "/api/v1/query",
			reqParam: url.Values{
				"query": []string{"2"},
				"time":  []string{testTime.Format(time.RFC3339Nano)},
			},
			err: errors.New("client_error: client error: 404"),
		},

		{
			do: doQueryRange("2", Range{
				Start: testTime.Add(-time.Minute),
				End:   testTime,
				Step:  time.Minute,
			}),
			inErr: fmt.Errorf("some error"),

			reqMethod: "GET",
			reqPath:   "/api/v1/query_range",
			reqParam: url.Values{
				"query": []string{"2"},
				"start": []string{testTime.Add(-time.Minute).Format(time.RFC3339Nano)},
				"end":   []string{testTime.Format(time.RFC3339Nano)},
				"step":  []string{time.Minute.String()},
			},
			err: fmt.Errorf("some error"),
		},

		{
			do:        doLabelValues("mylabel"),
			inRes:     []string{"val1", "val2"},
			reqMethod: "GET",
			reqPath:   "/api/v1/label/mylabel/values",
			res:       model.LabelValues{"val1", "val2"},
		},

		{
			do:        doLabelValues("mylabel"),
			inErr:     fmt.Errorf("some error"),
			reqMethod: "GET",
			reqPath:   "/api/v1/label/mylabel/values",
			err:       fmt.Errorf("some error"),
		},

		{
			do: doSeries("up", testTime.Add(-time.Minute), testTime),
			inRes: []map[string]string{
				{
					"__name__": "up",
					"job":      "prometheus",
					"instance": "localhost:9090"},
			},
			reqMethod: "GET",
			reqPath:   "/api/v1/series",
			reqParam: url.Values{
				"match": []string{"up"},
				"start": []string{testTime.Add(-time.Minute).Format(time.RFC3339Nano)},
				"end":   []string{testTime.Format(time.RFC3339Nano)},
			},
			res: []model.LabelSet{
				model.LabelSet{
					"__name__": "up",
					"job":      "prometheus",
					"instance": "localhost:9090",
				},
			},
		},

		{
			do:        doSeries("up", testTime.Add(-time.Minute), testTime),
			inErr:     fmt.Errorf("some error"),
			reqMethod: "GET",
			reqPath:   "/api/v1/series",
			reqParam: url.Values{
				"match": []string{"up"},
				"start": []string{testTime.Add(-time.Minute).Format(time.RFC3339Nano)},
				"end":   []string{testTime.Format(time.RFC3339Nano)},
			},
			err: fmt.Errorf("some error"),
		},

		{
			do: doSnapshot(true),
			inRes: map[string]string{
				"name": "20171210T211224Z-2be650b6d019eb54",
			},
			reqMethod: "POST",
			reqPath:   "/api/v1/admin/tsdb/snapshot",
			reqParam: url.Values{
				"skip_head": []string{"true"},
			},
			res: SnapshotResult{
				Name: "20171210T211224Z-2be650b6d019eb54",
			},
		},

		{
			do:        doSnapshot(true),
			inErr:     fmt.Errorf("some error"),
			reqMethod: "POST",
			reqPath:   "/api/v1/admin/tsdb/snapshot",
			err:       fmt.Errorf("some error"),
		},

		{
			do:        doCleanTombstones(),
			reqMethod: "POST",
			reqPath:   "/api/v1/admin/tsdb/clean_tombstones",
		},

		{
			do:        doCleanTombstones(),
			inErr:     fmt.Errorf("some error"),
			reqMethod: "POST",
			reqPath:   "/api/v1/admin/tsdb/clean_tombstones",
			err:       fmt.Errorf("some error"),
		},

		{
			do: doDeleteSeries("up", testTime.Add(-time.Minute), testTime),
			inRes: []map[string]string{
				{
					"__name__": "up",
					"job":      "prometheus",
					"instance": "localhost:9090"},
			},
			reqMethod: "POST",
			reqPath:   "/api/v1/admin/tsdb/delete_series",
			reqParam: url.Values{
				"match": []string{"up"},
				"start": []string{testTime.Add(-time.Minute).Format(time.RFC3339Nano)},
				"end":   []string{testTime.Format(time.RFC3339Nano)},
			},
		},

		{
			do:        doDeleteSeries("up", testTime.Add(-time.Minute), testTime),
			inErr:     fmt.Errorf("some error"),
			reqMethod: "POST",
			reqPath:   "/api/v1/admin/tsdb/delete_series",
			reqParam: url.Values{
				"match": []string{"up"},
				"start": []string{testTime.Add(-time.Minute).Format(time.RFC3339Nano)},
				"end":   []string{testTime.Format(time.RFC3339Nano)},
			},
			err: fmt.Errorf("some error"),
		},

		{
			do:        doConfig(),
			reqMethod: "GET",
			reqPath:   "/api/v1/status/config",
			inRes: map[string]string{
				"yaml": "<content of the loaded config file in YAML>",
			},
			res: ConfigResult{
				YAML: "<content of the loaded config file in YAML>",
			},
		},

		{
			do:        doConfig(),
			reqMethod: "GET",
			reqPath:   "/api/v1/status/config",
			inErr:     fmt.Errorf("some error"),
			err:       fmt.Errorf("some error"),
		},

		{
			do:        doFlags(),
			reqMethod: "GET",
			reqPath:   "/api/v1/status/flags",
			inRes: map[string]string{
				"alertmanager.notification-queue-capacity": "10000",
				"alertmanager.timeout":                     "10s",
				"log.level":                                "info",
				"query.lookback-delta":                     "5m",
				"query.max-concurrency":                    "20",
			},
			res: FlagsResult{
				"alertmanager.notification-queue-capacity": "10000",
				"alertmanager.timeout":                     "10s",
				"log.level":                                "info",
				"query.lookback-delta":                     "5m",
				"query.max-concurrency":                    "20",
			},
		},

		{
			do:        doFlags(),
			reqMethod: "GET",
			reqPath:   "/api/v1/status/flags",
			inErr:     fmt.Errorf("some error"),
			err:       fmt.Errorf("some error"),
		},

		{
			do:        doAlertManagers(),
			reqMethod: "GET",
			reqPath:   "/api/v1/alertmanagers",
			inRes: map[string]interface{}{
				"activeAlertManagers": []map[string]string{
					{
						"url": "http://127.0.0.1:9091/api/v1/alerts",
					},
				},
				"droppedAlertManagers": []map[string]string{
					{
						"url": "http://127.0.0.1:9092/api/v1/alerts",
					},
				},
			},
			res: AlertManagersResult{
				Active: []AlertManager{
					{
						URL: "http://127.0.0.1:9091/api/v1/alerts",
					},
				},
				Dropped: []AlertManager{
					{
						URL: "http://127.0.0.1:9092/api/v1/alerts",
					},
				},
			},
		},

		{
			do:        doAlertManagers(),
			reqMethod: "GET",
			reqPath:   "/api/v1/alertmanagers",
			inErr:     fmt.Errorf("some error"),
			err:       fmt.Errorf("some error"),
		},

		{
			do:        doTargets(),
			reqMethod: "GET",
			reqPath:   "/api/v1/targets",
			inRes: map[string]interface{}{
				"activeTargets": []map[string]interface{}{
					{
						"discoveredLabels": map[string]string{
							"__address__":      "127.0.0.1:9090",
							"__metrics_path__": "/metrics",
							"__scheme__":       "http",
							"job":              "prometheus",
						},
						"labels": map[string]string{
							"instance": "127.0.0.1:9090",
							"job":      "prometheus",
						},
						"scrapeUrl":  "http://127.0.0.1:9090",
						"lastError":  "error while scraping target",
						"lastScrape": testTime.UTC().Format(time.RFC3339Nano),
						"health":     "up",
					},
				},
				"droppedTargets": []map[string]interface{}{
					{
						"discoveredLabels": map[string]string{
							"__address__":      "127.0.0.1:9100",
							"__metrics_path__": "/metrics",
							"__scheme__":       "http",
							"job":              "node",
						},
					},
				},
			},
			res: TargetsResult{
				Active: []ActiveTarget{
					{
						DiscoveredLabels: model.LabelSet{
							"__address__":      "127.0.0.1:9090",
							"__metrics_path__": "/metrics",
							"__scheme__":       "http",
							"job":              "prometheus",
						},
						Labels: model.LabelSet{
							"instance": "127.0.0.1:9090",
							"job":      "prometheus",
						},
						ScrapeURL:  "http://127.0.0.1:9090",
						LastError:  "error while scraping target",
						LastScrape: testTime.UTC(),
						Health:     HealthGood,
					},
				},
				Dropped: []DroppedTarget{
					{
						DiscoveredLabels: model.LabelSet{
							"__address__":      "127.0.0.1:9100",
							"__metrics_path__": "/metrics",
							"__scheme__":       "http",
							"job":              "node",
						},
					},
				},
			},
		},

		{
			do:        doTargets(),
			reqMethod: "GET",
			reqPath:   "/api/v1/targets",
			inErr:     fmt.Errorf("some error"),
			err:       fmt.Errorf("some error"),
		},
	}

	var tests []apiTest
	tests = append(tests, queryTests...)

	for i, test := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			client.curTest = test

			res, err := test.do()

			if test.err != nil {
				if err == nil {
					t.Fatalf("expected error %q but got none", test.err)
				}
				if err.Error() != test.err.Error() {
					t.Errorf("unexpected error: want %s, got %s", test.err, err)
				}
				if apiErr, ok := err.(*Error); ok {
					if apiErr.Detail != test.inRes {
						t.Errorf("%q should be %q", apiErr.Detail, test.inRes)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			if !reflect.DeepEqual(res, test.res) {
				t.Errorf("unexpected result: want %v, got %v", test.res, res)
			}
		})
	}
}

type testClient struct {
	*testing.T

	ch  chan apiClientTest
	req *http.Request
}

type apiClientTest struct {
	code         int
	response     interface{}
	expectedBody string
	expectedErr  *Error
}

func (c *testClient) URL(ep string, args map[string]string) *url.URL {
	return nil
}

func (c *testClient) Do(ctx context.Context, req *http.Request) (*http.Response, []byte, error) {
	if ctx == nil {
		c.Fatalf("context was not passed down")
	}
	if req != c.req {
		c.Fatalf("request was not passed down")
	}

	test := <-c.ch

	var b []byte
	var err error

	switch v := test.response.(type) {
	case string:
		b = []byte(v)
	default:
		b, err = json.Marshal(v)
		if err != nil {
			c.Fatal(err)
		}
	}

	resp := &http.Response{
		StatusCode: test.code,
	}

	return resp, b, nil
}

func TestAPIClientDo(t *testing.T) {
	tests := []apiClientTest{
		{
			code: statusAPIError,
			response: &apiResponse{
				Status:    "error",
				Data:      json.RawMessage(`null`),
				ErrorType: ErrBadData,
				Error:     "failed",
			},
			expectedErr: &Error{
				Type: ErrBadData,
				Msg:  "failed",
			},
			expectedBody: `null`,
		},
		{
			code: statusAPIError,
			response: &apiResponse{
				Status:    "error",
				Data:      json.RawMessage(`"test"`),
				ErrorType: ErrTimeout,
				Error:     "timed out",
			},
			expectedErr: &Error{
				Type: ErrTimeout,
				Msg:  "timed out",
			},
			expectedBody: `test`,
		},
		{
			code:     http.StatusInternalServerError,
			response: "500 error details",
			expectedErr: &Error{
				Type:   ErrServer,
				Msg:    "server error: 500",
				Detail: "500 error details",
			},
		},
		{
			code:     http.StatusNotFound,
			response: "404 error details",
			expectedErr: &Error{
				Type:   ErrClient,
				Msg:    "client error: 404",
				Detail: "404 error details",
			},
		},
		{
			code: http.StatusBadRequest,
			response: &apiResponse{
				Status:    "error",
				Data:      json.RawMessage(`null`),
				ErrorType: ErrBadData,
				Error:     "end timestamp must not be before start time",
			},
			expectedErr: &Error{
				Type: ErrBadData,
				Msg:  "end timestamp must not be before start time",
			},
		},
		{
			code:     statusAPIError,
			response: "bad json",
			expectedErr: &Error{
				Type: ErrBadResponse,
				Msg:  "invalid character 'b' looking for beginning of value",
			},
		},
		{
			code: statusAPIError,
			response: &apiResponse{
				Status: "success",
				Data:   json.RawMessage(`"test"`),
			},
			expectedErr: &Error{
				Type: ErrBadResponse,
				Msg:  "inconsistent body for response code",
			},
		},
		{
			code: statusAPIError,
			response: &apiResponse{
				Status:    "success",
				Data:      json.RawMessage(`"test"`),
				ErrorType: ErrTimeout,
				Error:     "timed out",
			},
			expectedErr: &Error{
				Type: ErrBadResponse,
				Msg:  "inconsistent body for response code",
			},
		},
		{
			code: http.StatusOK,
			response: &apiResponse{
				Status:    "error",
				Data:      json.RawMessage(`"test"`),
				ErrorType: ErrTimeout,
				Error:     "timed out",
			},
			expectedErr: &Error{
				Type: ErrBadResponse,
				Msg:  "inconsistent body for response code",
			},
		},
	}

	tc := &testClient{
		T:   t,
		ch:  make(chan apiClientTest, 1),
		req: &http.Request{},
	}
	client := &apiClient{tc}

	for i, test := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {

			tc.ch <- test

			_, body, err := client.Do(context.Background(), tc.req)

			if test.expectedErr != nil {
				if err == nil {
					t.Fatalf("expected error %q but got none", test.expectedErr)
				}
				if test.expectedErr.Error() != err.Error() {
					t.Errorf("unexpected error: want %q, got %q", test.expectedErr, err)
				}
				if test.expectedErr.Detail != "" {
					apiErr := err.(*Error)
					if apiErr.Detail != test.expectedErr.Detail {
						t.Errorf("unexpected error details: want %q, got %q", test.expectedErr.Detail, apiErr.Detail)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpeceted error %s", err)
			}

			want, got := test.expectedBody, string(body)
			if want != got {
				t.Errorf("unexpected body: want %q, got %q", want, got)
			}
		})

	}
}
