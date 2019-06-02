package chunk

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"math/rand"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLabelSeriesID(t *testing.T) {
	for _, c := range []struct {
		lbls     labels.Labels
		expected string
	}{
		{
			labels.Labels{{Name: model.MetricNameLabel, Value: "foo"}},
			"LCa0a2j/xo/5m0U8HTBBNBNCLXBkg7+g+YpeiGJm564",
		},
		{
			labels.Labels{
				{Name: model.MetricNameLabel, Value: "foo"},
				{Name: "bar", Value: "baz"},
				{Name: "flip", Value: "flop"},
				{Name: "toms", Value: "code"},
			},
			"KrbXMezYneba+o7wfEdtzOdAWhbfWcDrlVfs1uOCX3M",
		},
		{
			labels.Labels{},
			"RBNvo1WzZ4oRRq0W9+hknpT7T8If536DEMBg9hyq/4o",
		},
	} {
		seriesID := string(labelsSeriesID(c.lbls))
		assert.Equal(t, c.expected, seriesID, labelsString(c.lbls))
	}
}

func TestSchemaTimeEncoding(t *testing.T) {
	assert.Equal(t, uint32(0), decodeTime(encodeTime(0)), "0")
	assert.Equal(t, uint32(math.MaxUint32), decodeTime(encodeTime(math.MaxUint32)), "MaxUint32")

	for i := 0; i < 100; i++ {
		a, b := uint32(rand.Int31()), uint32(rand.Int31())

		assert.Equal(t, a, decodeTime(encodeTime(a)), "a")
		assert.Equal(t, b, decodeTime(encodeTime(b)), "b")

		if a < b {
			assert.Equal(t, -1, bytes.Compare(encodeTime(a), encodeTime(b)), "lt")
		} else if a > b {
			assert.Equal(t, 1, bytes.Compare(encodeTime(a), encodeTime(b)), "gt")
		} else {
			assert.Equal(t, 1, bytes.Compare(encodeTime(a), encodeTime(b)), "eq")
		}
	}
}

func TestParseChunkTimeRangeValue(t *testing.T) {
	// Test we can decode legacy range values
	for _, c := range []struct {
		encoded        []byte
		value, chunkID string
	}{
		{[]byte("1\x002\x003\x00"), "2", "3"},

		// version 1 range keys (v3 Schema) base64-encodes the label value
		{[]byte("toms\x00Y29kZQ\x002:1484661279394:1484664879394\x001\x00"),
			"code", "2:1484661279394:1484664879394"},

		// version 1 range keys (v4 Schema) doesn't have the label name in the range key
		{[]byte("\x00Y29kZQ\x002:1484661279394:1484664879394\x001\x00"),
			"code", "2:1484661279394:1484664879394"},

		// version 2 range keys (also v4 Schema) don't have the label name or value in the range key
		{[]byte("\x00\x002:1484661279394:1484664879394\x002\x00"),
			"", "2:1484661279394:1484664879394"},

		// version 3 range keys (v5 Schema) have timestamp in first 'dimension'
		{[]byte("a1b2c3d4\x00\x002:1484661279394:1484664879394\x003\x00"),
			"", "2:1484661279394:1484664879394"},

		// version 4 range keys (also v5 Schema) have timestamp in first 'dimension',
		// base64 value in second
		{[]byte("a1b2c3d4\x00Y29kZQ\x002:1484661279394:1484664879394\x004\x00"),
			"code", "2:1484661279394:1484664879394"},
	} {
		chunkID, labelValue, _, _, err := parseChunkTimeRangeValue(c.encoded, nil)
		require.NoError(t, err)
		assert.Equal(t, model.LabelValue(c.value), labelValue)
		assert.Equal(t, c.chunkID, chunkID)
	}
}

func TestParseMetricNameRangeValue(t *testing.T) {
	for _, c := range []struct {
		encoded       []byte
		value         string
		expMetricName string
	}{
		// version 1 (id 6) metric name range keys (used in v7 Schema) have
		// metric name hash in first 'dimension', however just returns the value
		{[]byte("a1b2c3d4\x00\x00\x006\x00"), "foo", "foo"},
		{encodeRangeKey([]byte("bar"), nil, nil, metricNameRangeKeyV1), "bar", "bar"},
	} {
		metricName, err := parseMetricNameRangeValue(c.encoded, []byte(c.value))
		require.NoError(t, err)
		assert.Equal(t, model.LabelValue(c.expMetricName), metricName)
	}
}

func TestParseSeriesRangeValue(t *testing.T) {
	metric := model.Metric{
		model.MetricNameLabel: "foo",
		"bar":                 "bary",
		"baz":                 "bazy",
	}

	fingerprintBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(fingerprintBytes, uint64(metric.Fingerprint()))
	metricBytes, err := json.Marshal(metric)
	require.NoError(t, err)

	for _, c := range []struct {
		encoded   []byte
		value     []byte
		expMetric model.Metric
	}{
		{encodeRangeKey(fingerprintBytes, nil, nil, seriesRangeKeyV1), metricBytes, metric},
	} {
		metric, err := parseSeriesRangeValue(c.encoded, c.value)
		require.NoError(t, err)
		assert.Equal(t, c.expMetric, metric)
	}
}
