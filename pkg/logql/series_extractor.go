package logql

var (
	ExtractBytes = bytesSampleExtractor{}
	ExtractCount = countSampleExtractor{}
)

// SampleExtractor transforms a log entry into a sample.
// In case of failure the second return value will be false.
type SampleExtractor interface {
	Extract(line []byte) (float64, bool)
}

type countSampleExtractor struct{}

func (countSampleExtractor) Extract(line []byte) (float64, bool) {
	return 1., true
}

type bytesSampleExtractor struct{}

func (bytesSampleExtractor) Extract(line []byte) (float64, bool) {
	return float64(len(line)), true
}
