package logproto

import (
	"flag"
	"sync"
)

var (
	expectedTimeseries       = 100
	expectedLabels           = 20
	expectedSamplesPerSeries = 10

	/*
		We cannot pool these as pointer-to-slice because the place we use them is in WriteRequest which is generated from Protobuf
		and we don't have an option to make it a pointer. There is overhead here 24 bytes of garbage every time a PreallocTimeseries
		is re-used. But since the slices are far far larger, we come out ahead.
	*/
	slicePool = sync.Pool{
		New: func() interface{} {
			return make([]PreallocTimeseries, 0, expectedTimeseries)
		},
	}

	timeSeriesPool = sync.Pool{
		New: func() interface{} {
			return &TimeSeries{
				Labels:  make([]LabelAdapter, 0, expectedLabels),
				Samples: make([]LegacySample, 0, expectedSamplesPerSeries),
			}
		},
	}
)

// PreallocConfig configures how structures will be preallocated to optimise
// proto unmarshalling.
type PreallocConfig struct{}

// RegisterFlags registers configuration settings.
func (PreallocConfig) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&expectedTimeseries, "ingester-client.expected-timeseries", expectedTimeseries, "Expected number of timeseries per request, used for preallocations.")
	f.IntVar(&expectedLabels, "ingester-client.expected-labels", expectedLabels, "Expected number of labels per timeseries, used for preallocations.")
	f.IntVar(&expectedSamplesPerSeries, "ingester-client.expected-samples-per-series", expectedSamplesPerSeries, "Expected number of samples per timeseries, used for preallocations.")
}

// PreallocWriteRequest is a WriteRequest which preallocs slices on Unmarshal.
type PreallocWriteRequest struct {
	WriteRequest
}

// Unmarshal implements proto.Message.
func (p *PreallocWriteRequest) Unmarshal(dAtA []byte) error {
	p.Timeseries = PreallocTimeseriesSliceFromPool()
	return p.WriteRequest.Unmarshal(dAtA)
}

// PreallocTimeseries is a TimeSeries which preallocs slices on Unmarshal.
type PreallocTimeseries struct {
	*TimeSeries
}

// Unmarshal implements proto.Message.
func (p *PreallocTimeseries) Unmarshal(dAtA []byte) error {
	p.TimeSeries = TimeseriesFromPool()
	return p.TimeSeries.Unmarshal(dAtA)
}

// PreallocTimeseriesSliceFromPool retrieves a slice of PreallocTimeseries from a sync.Pool.
// ReuseSlice should be called once done.
func PreallocTimeseriesSliceFromPool() []PreallocTimeseries {
	return slicePool.Get().([]PreallocTimeseries)
}

// ReuseSlice puts the slice back into a sync.Pool for reuse.
func ReuseSlice(ts []PreallocTimeseries) {
	for i := range ts {
		ReuseTimeseries(ts[i].TimeSeries)
	}

	slicePool.Put(ts[:0]) //nolint:staticcheck //see comment on slicePool for more details
}

// TimeseriesFromPool retrieves a pointer to a TimeSeries from a sync.Pool.
// ReuseTimeseries should be called once done, unless ReuseSlice was called on the slice that contains this TimeSeries.
func TimeseriesFromPool() *TimeSeries {
	return timeSeriesPool.Get().(*TimeSeries)
}

// ReuseTimeseries puts the timeseries back into a sync.Pool for reuse.
func ReuseTimeseries(ts *TimeSeries) {
	// Name and Value may point into a large gRPC buffer, so clear the reference to allow GC
	for i := 0; i < len(ts.Labels); i++ {
		ts.Labels[i].Name = ""
		ts.Labels[i].Value = ""
	}
	ts.Labels = ts.Labels[:0]
	ts.Samples = ts.Samples[:0]
	timeSeriesPool.Put(ts)
}

// SizeWiresmith implements the wiresmith customtype contract.
func (p *PreallocTimeseries) SizeWiresmith() int { return p.TimeSeries.Size() }

// MarshalWiresmith implements the wiresmith customtype contract.
func (p *PreallocTimeseries) MarshalWiresmith(buf []byte) (int, error) {
	return p.TimeSeries.MarshalTo(buf)
}

// UnmarshalWiresmith implements the wiresmith customtype contract. Like
// Unmarshal, it sources the backing TimeSeries from the pool.
func (p *PreallocTimeseries) UnmarshalWiresmith(buf []byte) error { return p.Unmarshal(buf) }

// EqualWiresmith implements the wiresmith customtype contract.
func (p *PreallocTimeseries) EqualWiresmith(other any) bool {
	o, ok := coercePreallocTimeseries(other)
	if !ok {
		return false
	}
	return p.TimeSeries.Equal(o.TimeSeries)
}

// CompareWiresmith implements the wiresmith customtype contract. There is no
// natural order for timeseries; equality decides 0, otherwise -1.
func (p *PreallocTimeseries) CompareWiresmith(other any) int {
	if p.EqualWiresmith(other) {
		return 0
	}
	return -1
}

func coercePreallocTimeseries(other any) (PreallocTimeseries, bool) {
	switch o := other.(type) {
	case PreallocTimeseries:
		return o, true
	case *PreallocTimeseries:
		if o == nil {
			return PreallocTimeseries{}, false
		}
		return *o, true
	default:
		return PreallocTimeseries{}, false
	}
}
