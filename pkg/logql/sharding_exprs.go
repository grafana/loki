package logql

// DownstreamSampleExpr is a SampleExpr which signals downstream computation
type DownstreamSampleExpr struct {
	shard *int
	SampleExpr
}

// DownstreamLogSelectorExpr is a LogSelectorExpr which signals downstream computation
type DownstreamLogSelectorExpr struct {
	shard *int
	LogSelectorExpr
}

// ConcatSampleExpr is an expr for concatenating multiple SampleExpr
type ConcatSampleExpr struct {
	SampleExpr
	next *ConcatSampleExpr
}

// ConcatLogSelectorExpr is an expr for concatenating multiple LogSelectorExpr
type ConcatLogSelectorExpr struct {
	LogSelectorExpr
	next *ConcatLogSelectorExpr
}
