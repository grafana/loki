package jwriter

var noOpWriter = makeNoOpWriter() //nolint:gochecknoglobals

func makeNoOpWriter() Writer {
	w := Writer{}
	w.AddError(noOpWriterError{})
	return w
}

type noOpWriterError struct{}

func (noOpWriterError) Error() string {
	return "this is a stub Writer that produces no output"
}
