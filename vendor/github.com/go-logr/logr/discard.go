package logr

// Discard returns a valid Logger that discards all messages logged to it.
// It can be used whenever the caller is not interested in the logs.
func Discard() Logger {
	return discardLogger{}
}

// discardLogger is a Logger that discards all messages.
type discardLogger struct{}

func (l discardLogger) Enabled() bool {
	return false
}

func (l discardLogger) Info(msg string, keysAndValues ...interface{}) {
}

func (l discardLogger) Error(err error, msg string, keysAndValues ...interface{}) {
}

func (l discardLogger) V(level int) Logger {
	return l
}

func (l discardLogger) WithValues(keysAndValues ...interface{}) Logger {
	return l
}

func (l discardLogger) WithName(name string) Logger {
	return l
}

// Verify that it actually implements the interface
var _ Logger = discardLogger{}
