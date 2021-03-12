package log

import (
	"io"
)

// Option is a configuration option
type Option func(*Logger)

// WithOutput sets the output to w
func WithOutput(w io.Writer) Option {
	return func(l *Logger) {
		l.SetOutput(w)
	}
}

// WithLogLevel sets the output log level and controls which verbosity logs are printed
func WithLogLevel(v int) Option {
	return func(*Logger) {
		logLevel = v
	}
}
