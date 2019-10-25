package util

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/blang/semver"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
)

// WriteJSONResponse writes some JSON as a HTTP response.
func WriteJSONResponse(w http.ResponseWriter, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err = w.Write(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
}

// CompressionType for encoding and decoding requests and responses.
type CompressionType int

// Values for CompressionType
const (
	NoCompression CompressionType = iota
	FramedSnappy
	RawSnappy
)

var rawSnappyFromVersion = semver.MustParse("0.1.0")

// CompressionTypeFor a given version of the Prometheus remote storage protocol.
// See https://github.com/prometheus/prometheus/issues/2692.
func CompressionTypeFor(version string) CompressionType {
	ver, err := semver.Make(version)
	if err != nil {
		return FramedSnappy
	}

	if ver.GTE(rawSnappyFromVersion) {
		return RawSnappy
	}
	return FramedSnappy
}

// ParseProtoReader parses a compressed proto from an io.Reader.
func ParseProtoReader(ctx context.Context, reader io.Reader, expectedSize, maxSize int, req proto.Message, compression CompressionType) ([]byte, error) {
	var body []byte
	var err error
	sp := opentracing.SpanFromContext(ctx)
	if sp != nil {
		sp.LogFields(otlog.String("event", "util.ParseProtoRequest[start reading]"))
	}
	var buf bytes.Buffer
	if expectedSize > 0 {
		if expectedSize > maxSize {
			return nil, fmt.Errorf("message expected size larger than max (%d vs %d)", expectedSize, maxSize)
		}
		buf.Grow(expectedSize + bytes.MinRead) // extra space guarantees no reallocation
	}
	switch compression {
	case NoCompression:
		// Read from LimitReader with limit max+1. So if the underlying
		// reader is over limit, the result will be bigger than max.
		_, err = buf.ReadFrom(io.LimitReader(reader, int64(maxSize)+1))
		body = buf.Bytes()
	case FramedSnappy:
		_, err = buf.ReadFrom(io.LimitReader(snappy.NewReader(reader), int64(maxSize)+1))
		body = buf.Bytes()
	case RawSnappy:
		_, err = buf.ReadFrom(reader)
		body = buf.Bytes()
		if sp != nil {
			sp.LogFields(otlog.String("event", "util.ParseProtoRequest[decompress]"),
				otlog.Int("size", len(body)))
		}
		if err == nil && len(body) <= maxSize {
			body, err = snappy.Decode(nil, body)
		}
	}
	if err != nil {
		return nil, err
	}
	if len(body) > maxSize {
		return nil, fmt.Errorf("received message larger than max (%d vs %d)", len(body), maxSize)
	}

	if sp != nil {
		sp.LogFields(otlog.String("event", "util.ParseProtoRequest[unmarshal]"),
			otlog.Int("size", len(body)))
	}

	// We re-implement proto.Unmarshal here as it calls XXX_Unmarshal first,
	// which we can't override without upsetting golint.
	req.Reset()
	if u, ok := req.(proto.Unmarshaler); ok {
		err = u.Unmarshal(body)
	} else {
		err = proto.NewBuffer(body).Unmarshal(req)
	}
	if err != nil {
		return nil, err
	}

	return body, nil
}

// SerializeProtoResponse serializes a protobuf response into an HTTP response.
func SerializeProtoResponse(w http.ResponseWriter, resp proto.Message, compression CompressionType) error {
	data, err := proto.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return fmt.Errorf("error marshaling proto response: %v", err)
	}

	switch compression {
	case NoCompression:
	case FramedSnappy:
		buf := bytes.Buffer{}
		if _, err := snappy.NewWriter(&buf).Write(data); err != nil {
			return err
		}
		data = buf.Bytes()
	case RawSnappy:
		data = snappy.Encode(nil, data)
	}

	if _, err := w.Write(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return fmt.Errorf("error sending proto response: %v", err)
	}
	return nil
}
