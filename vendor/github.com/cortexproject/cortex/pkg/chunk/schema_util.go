package chunk

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"fmt"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// Backwards-compatible with model.Metric.String()
func labelsString(ls labels.Labels) string {
	metricName := ls.Get(labels.MetricName)
	if metricName != "" && len(ls) == 1 {
		return metricName
	}
	var b strings.Builder
	b.Grow(1000)

	b.WriteString(metricName)
	b.WriteByte('{')
	i := 0
	for _, l := range ls {
		if l.Name == labels.MetricName {
			continue
		}
		if i > 0 {
			b.WriteByte(',')
			b.WriteByte(' ')
		}
		b.WriteString(l.Name)
		b.WriteByte('=')
		var buf [1000]byte
		b.Write(strconv.AppendQuote(buf[:0], l.Value))
		i++
	}
	b.WriteByte('}')

	return b.String()
}

func labelsSeriesID(ls labels.Labels) []byte {
	h := sha256.Sum256([]byte(labelsString(ls)))
	return encodeBase64Bytes(h[:])
}

func sha256bytes(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return encodeBase64Bytes(h[:])
}

func encodeRangeKey(ss ...[]byte) []byte {
	length := 0
	for _, s := range ss {
		length += len(s) + 1
	}
	output, i := make([]byte, length, length), 0
	for _, s := range ss {
		copy(output[i:i+len(s)], s)
		i += len(s) + 1
	}
	return output
}

func decodeRangeKey(value []byte) [][]byte {
	components := make([][]byte, 0, 5)
	i, j := 0, 0
	for j < len(value) {
		if value[j] != 0 {
			j++
			continue
		}
		components = append(components, value[i:j])
		j++
		i = j
	}
	return components
}

func encodeBase64Bytes(bytes []byte) []byte {
	encodedLen := base64.RawStdEncoding.EncodedLen(len(bytes))
	encoded := make([]byte, encodedLen, encodedLen)
	base64.RawStdEncoding.Encode(encoded, bytes)
	return encoded
}

func encodeBase64Value(value string) []byte {
	encodedLen := base64.RawStdEncoding.EncodedLen(len(value))
	encoded := make([]byte, encodedLen, encodedLen)
	base64.RawStdEncoding.Encode(encoded, []byte(value))
	return encoded
}

func decodeBase64Value(bs []byte) (model.LabelValue, error) {
	decodedLen := base64.RawStdEncoding.DecodedLen(len(bs))
	decoded := make([]byte, decodedLen, decodedLen)
	if _, err := base64.RawStdEncoding.Decode(decoded, bs); err != nil {
		return "", err
	}
	return model.LabelValue(decoded), nil
}

func encodeTime(t uint32) []byte {
	// timestamps are hex encoded such that it doesn't contain null byte,
	// but is still lexicographically sortable.
	throughBytes := make([]byte, 4, 4)
	binary.BigEndian.PutUint32(throughBytes, t)
	encodedThroughBytes := make([]byte, 8, 8)
	hex.Encode(encodedThroughBytes, throughBytes)
	return encodedThroughBytes
}

func decodeTime(bs []byte) uint32 {
	buf := make([]byte, 4, 4)
	hex.Decode(buf, bs)
	return binary.BigEndian.Uint32(buf)
}

// parseMetricNameRangeValue returns the metric name stored in metric name
// range values. Currently checks range value key and returns the value as the
// metric name.
func parseMetricNameRangeValue(rangeValue []byte, value []byte) (model.LabelValue, error) {
	components := decodeRangeKey(rangeValue)
	switch {
	case len(components) < 4:
		return "", fmt.Errorf("invalid metric name range value: %x", rangeValue)

	// v1 has the metric name as the value (with the hash as the first component)
	case bytes.Equal(components[3], metricNameRangeKeyV1):
		return model.LabelValue(value), nil

	default:
		return "", fmt.Errorf("unrecognised metricNameRangeKey version: %q", string(components[3]))
	}
}

// parseSeriesRangeValue returns the model.Metric stored in metric fingerprint
// range values.
func parseSeriesRangeValue(rangeValue []byte, value []byte) (model.Metric, error) {
	components := decodeRangeKey(rangeValue)
	switch {
	case len(components) < 4:
		return nil, fmt.Errorf("invalid metric range value: %x", rangeValue)

	// v1 has the encoded json metric as the value (with the fingerprint as the first component)
	case bytes.Equal(components[3], seriesRangeKeyV1):
		var series model.Metric
		if err := json.Unmarshal(value, &series); err != nil {
			return nil, err
		}
		return series, nil

	default:
		return nil, fmt.Errorf("unrecognised seriesRangeKey version: %q", string(components[3]))
	}
}

// parseChunkTimeRangeValue returns the chunkKey, labelValue and metadataInIndex
// for chunk time range values.
func parseChunkTimeRangeValue(rangeValue []byte, value []byte) (
	chunkID string, labelValue model.LabelValue, metadataInIndex bool,
	isSeriesID bool, err error,
) {
	components := decodeRangeKey(rangeValue)

	switch {
	case len(components) < 3:
		err = errors.Errorf("invalid chunk time range value: %x", rangeValue)
		return

	// v1 & v2 schema had three components - label name, label value and chunk ID.
	// No version number.
	case len(components) == 3:
		chunkID = string(components[2])
		labelValue = model.LabelValue(components[1])
		metadataInIndex = true
		return

	// v3 schema had four components - label name, label value, chunk ID and version.
	// "version" is 1 and label value is base64 encoded.
	// (older code wrote "version" as 1, not '1')
	case bytes.Equal(components[3], chunkTimeRangeKeyV1a):
		fallthrough
	case bytes.Equal(components[3], chunkTimeRangeKeyV1):
		chunkID = string(components[2])
		labelValue, err = decodeBase64Value(components[1])
		return

	// v4 schema wrote v3 range keys and a new range key - version 2,
	// with four components - <empty>, <empty>, chunk ID and version.
	case bytes.Equal(components[3], chunkTimeRangeKeyV2):
		chunkID = string(components[2])
		return

	// v5 schema version 3 range key is chunk end time, <empty>, chunk ID, version
	case bytes.Equal(components[3], chunkTimeRangeKeyV3):
		chunkID = string(components[2])
		return

	// v5 schema version 4 range key is chunk end time, label value, chunk ID, version
	case bytes.Equal(components[3], chunkTimeRangeKeyV4):
		chunkID = string(components[2])
		labelValue, err = decodeBase64Value(components[1])
		return

	// v6 schema added version 5 range keys, which have the label value written in
	// to the value, not the range key. So they are [chunk end time, <empty>, chunk ID, version].
	case bytes.Equal(components[3], chunkTimeRangeKeyV5):
		chunkID = string(components[2])
		labelValue = model.LabelValue(value)
		return

	// v9 schema actually return series IDs
	case bytes.Equal(components[3], seriesRangeKeyV1):
		chunkID = string(components[0])
		isSeriesID = true
		return

	case bytes.Equal(components[3], labelSeriesRangeKeyV1):
		chunkID = string(components[1])
		labelValue = model.LabelValue(value)
		isSeriesID = true
		return

	default:
		err = fmt.Errorf("unrecognised chunkTimeRangeKey version: %q", string(components[3]))
		return
	}
}
