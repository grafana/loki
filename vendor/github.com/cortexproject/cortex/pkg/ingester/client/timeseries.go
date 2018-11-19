package client

import "flag"

var (
	expectedTimeseries       = 100
	expectedLabels           = 20
	expectedSamplesPerSeries = 10
)

// PreallocConfig configures how structures will be preallocated to optimise
// proto unmarshalling.
type PreallocConfig struct{}

// RegisterFlags registers configuration settings.
func (PreallocConfig) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&expectedTimeseries, "ingester-client.expected-timeseries", expectedTimeseries, "Expected number of timeseries per request, use for preallocations.")
	f.IntVar(&expectedLabels, "ingester-client.expected-labels", expectedLabels, "Expected number of labels per timeseries, used for preallocations.")
	f.IntVar(&expectedSamplesPerSeries, "ingester-client.expected-samples-per-series", expectedSamplesPerSeries, "Expected number of samples per timeseries, used for preallocations.")
}

// PreallocWriteRequest is a WriteRequest which preallocs slices on Unmarshall.
type PreallocWriteRequest struct {
	WriteRequest
}

// Unmarshal implements proto.Message.
func (p *PreallocWriteRequest) Unmarshal(dAtA []byte) error {
	p.Timeseries = make([]PreallocTimeseries, 0, expectedTimeseries)
	return p.WriteRequest.Unmarshal(dAtA)
}

// PreallocTimeseries is a TimeSeries which preallocs slices on Unmarshall.
type PreallocTimeseries struct {
	TimeSeries
}

// Unmarshal implements proto.Message.
func (p *PreallocTimeseries) Unmarshal(dAtA []byte) error {
	p.Labels = make([]LabelPair, 0, expectedLabels)
	p.Samples = make([]Sample, 0, expectedSamplesPerSeries)
	return p.TimeSeries.Unmarshal(dAtA)
}
