package cloudflare

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// LogpullRetentionConfiguration describes a the structure of a Logpull Retention
// payload.
type LogpullRetentionConfiguration struct {
	Flag bool `json:"flag"`
}

// LogpullRetentionConfigurationResponse is the API response, containing the
// Logpull retention result.
type LogpullRetentionConfigurationResponse struct {
	Response
	Result LogpullRetentionConfiguration `json:"result"`
}

// GetLogpullRetentionFlag gets the current setting flag.
//
// API reference: https://developers.cloudflare.com/logs/logpull-api/enabling-log-retention/
func (api *API) GetLogpullRetentionFlag(ctx context.Context, zoneID string) (*LogpullRetentionConfiguration, error) {
	uri := fmt.Sprintf("/zones/%s/logs/control/retention/flag", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return &LogpullRetentionConfiguration{}, err
	}
	var r LogpullRetentionConfigurationResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return &r.Result, nil
}

// SetLogpullRetentionFlag updates the retention flag to the defined boolean.
//
// API reference: https://developers.cloudflare.com/logs/logpull-api/enabling-log-retention/
func (api *API) SetLogpullRetentionFlag(ctx context.Context, zoneID string, enabled bool) (*LogpullRetentionConfiguration, error) {
	uri := fmt.Sprintf("/zones/%s/logs/control/retention/flag", zoneID)
	flagPayload := LogpullRetentionConfiguration{Flag: enabled}

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, flagPayload)
	if err != nil {
		return &LogpullRetentionConfiguration{}, err
	}
	var r LogpullRetentionConfigurationResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return &LogpullRetentionConfiguration{}, err
	}
	return &r.Result, nil
}

type (
	LogpullReceivedIterator interface {
		Next() bool
		Err() error
		Line() []byte
		Fields() (map[string]string, error)
		Close() error
	}
	logpullReceivedResponse struct {
		scanner *bufio.Scanner
		reader  io.ReadCloser
	}
	LogpullReceivedOption struct {
		Fields []string
		Count  *int64
		Sample *float64
	}
)

// LogpullReceived return the logs received for a given zone.
// The response returned is an iterator over the logs.
// Use Next() to iterate over the logs, and Close() to close the iterator.
// `Err()` returned the last error encountered while iterating.
// The logs are returned in the order they were received.
// Calling `Line()` return the current raw log line as a slice of bytes, you must copy the line as each call to `Next() will change its value.
// `Fields()` return the parsed log fields.
//
// Usage:
//
//		defer r.Close()
//		for r.Next() {
// 			if r.Err() != nil {
//				return r.Err()
//			}
//  		l := r.Line()
// 		}
//
// API reference: https://developers.cloudflare.com/logs/logpull/requesting-logs
func (api *API) LogpullReceived(ctx context.Context, zoneID string, start, end time.Time, opts LogpullReceivedOption) (LogpullReceivedIterator, error) {
	uri := "/zones/" + zoneID + "/logs/received"
	v := url.Values{}

	v.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	v.Set("end", strconv.FormatInt(end.UnixNano(), 10))

	if opts.Fields != nil {
		v.Set("fields", strings.Join(opts.Fields, ","))
	}

	if opts.Count != nil {
		v.Set("count", strconv.FormatInt(*opts.Count, 10))
	}

	if opts.Sample != nil {
		v.Set("sample", strconv.FormatFloat(*opts.Sample, 'f', -1, 64))
	}

	if len(v) > 0 {
		uri = fmt.Sprintf("%s?%s", uri, v.Encode())
	}
	reader, err := api.makeRequestWithAuthTypeAndHeaders(ctx, http.MethodGet, uri, nil, api.authType, http.Header{"Accept-Encoding": []string{"gzip"}})
	if err != nil {
		return nil, err
	}
	return &logpullReceivedResponse{
		scanner: bufio.NewScanner(reader),
		reader:  reader,
	}, nil
}

// Next Advances the iterator to the next log line, returns true if there is a line to be read.
func (r *logpullReceivedResponse) Next() bool {
	return r.scanner.Scan()
}

// Err returns the last error encountered while iterating.
func (r *logpullReceivedResponse) Err() error {
	return r.scanner.Err()
}

// Line returns the current raw log line as a slice of bytes, you must copy the line as each call to `Next()` will change its value.
func (r *logpullReceivedResponse) Line() []byte {
	return r.scanner.Bytes()
}

// Fields returns the parsed log fields as a map of string.
func (r *logpullReceivedResponse) Fields() (map[string]string, error) {
	var fields map[string]string
	data := r.Line()
	err := json.Unmarshal(data, &fields)
	return fields, err
}

// Close closes the iterator.
func (r *logpullReceivedResponse) Close() error {
	return r.reader.Close()
}

// LogpullFields returns the list of all available log fields.
//
// API reference: https://developers.cloudflare.com/logs/logpull/requesting-logs
func (api *API) LogpullFields(ctx context.Context, zoneID string) (map[string]string, error) {
	uri := "/zones/" + zoneID + "/logs/received/fields"
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	var r map[string]string
	err = json.Unmarshal(res, &r)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return r, nil
}
