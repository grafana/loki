package util

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/blang/semver"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/weaveworks/common/instrument"
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
func ParseProtoReader(ctx context.Context, reader io.Reader, req proto.Message, compression CompressionType) ([]byte, error) {
	var body []byte
	var err error
	switch compression {
	case NoCompression:
		body, err = ioutil.ReadAll(reader)
	case FramedSnappy:
		body, err = ioutil.ReadAll(snappy.NewReader(reader))
	case RawSnappy:
		body, err = ioutil.ReadAll(reader)
		if err == nil {
			body, err = snappy.Decode(nil, body)
		}
	}
	if err != nil {
		return nil, err
	}

	if err := instrument.CollectedRequest(ctx, "util.ParseProtoRequest[unmarshal]", &instrument.HistogramCollector{}, instrument.ErrorCode, func(_ context.Context) error {
		return proto.Unmarshal(body, req)
	}); err != nil {
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
