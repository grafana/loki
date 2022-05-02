package telegraf

var Debug bool

// DeprecationInfo contains information for marking a plugin deprecated.
type DeprecationInfo struct {
	// Since specifies the version since when the plugin is deprecated
	Since string
	// RemovalIn optionally specifies the version when the plugin is scheduled for removal
	RemovalIn string
	// Notice for the user on suggested replacements etc.
	Notice string
}

// Initializer is an interface that all plugin types: Inputs, Outputs,
// Processors, and Aggregators can optionally implement to initialize the
// plugin.
type Initializer interface {
	// Init performs one time setup of the plugin and returns an error if the
	// configuration is invalid.
	Init() error
}

// PluginDescriber contains the functions all plugins must implement to describe
// themselves to Telegraf. Note that all plugins may define a logger that is
// not part of the interface, but will receive an injected logger if it's set.
// eg: Log telegraf.Logger `toml:"-"`
type PluginDescriber interface {
	// SampleConfig returns the default configuration of the Processor
	SampleConfig() string

	// Description returns a one-sentence description on the Processor
	Description() string
}

// Logger defines an plugin-related interface for logging.
type Logger interface {
	// Errorf logs an error message, patterned after log.Printf.
	Errorf(format string, args ...interface{})
	// Error logs an error message, patterned after log.Print.
	Error(args ...interface{})
	// Debugf logs a debug message, patterned after log.Printf.
	Debugf(format string, args ...interface{})
	// Debug logs a debug message, patterned after log.Print.
	Debug(args ...interface{})
	// Warnf logs a warning message, patterned after log.Printf.
	Warnf(format string, args ...interface{})
	// Warn logs a warning message, patterned after log.Print.
	Warn(args ...interface{})
	// Infof logs an information message, patterned after log.Printf.
	Infof(format string, args ...interface{})
	// Info logs an information message, patterned after log.Print.
	Info(args ...interface{})
}
