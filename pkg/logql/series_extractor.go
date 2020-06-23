package logql

import (
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
)

var (
	extractBytes = bytesSampleExtractor{}
	extractCount = countSampleExtractor{}
)

// SampleExtractor transforms a log entry into a sample.
// In case of failure the second return value will be false.
type SampleExtractor interface {
	From(labels []client.LabelAdapter, ts int64, line []byte) (logproto.Sample, bool)
}

type countSampleExtractor struct{}

func (countSampleExtractor) From(labels []client.LabelAdapter, ts int64, line []byte) (logproto.Sample, bool) {
	return logproto.Sample{
		Labels:    labels,
		Timestamp: ts,
		Value:     1.,
	}, true
}

type bytesSampleExtractor struct{}

func (bytesSampleExtractor) From(labels []client.LabelAdapter, ts int64, line []byte) (logproto.Sample, bool) {
	return logproto.Sample{
		Labels:    labels,
		Timestamp: ts,
		Value:     float64(len(line)),
	}, true
}
