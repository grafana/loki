package logql

// Downstreamer is an interface for deferring responsibility for query execution.
// It is decoupled from but consumed by a downStreamEvaluator to dispatch ASTs.
type Downstreamer interface {
	// Downstream(*LokiRequest) (*LokiResponse, error)
}
