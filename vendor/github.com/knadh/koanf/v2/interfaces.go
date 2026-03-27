package koanf

// Provider represents a configuration provider. Providers can
// read configuration from a source (file, HTTP etc.)
type Provider interface {
	// ReadBytes returns the entire configuration as raw []bytes to be parsed.
	// with a Parser.
	ReadBytes() ([]byte, error)

	// Read returns the parsed configuration as a nested map[string]any.
	// It is important to note that the string keys should not be flat delimited
	// keys like `parent.child.key`, but nested like `{parent: {child: {key: 1}}}`.
	Read() (map[string]any, error)
}

// Parser represents a configuration format parser.
type Parser interface {
	Unmarshal([]byte) (map[string]any, error)
	Marshal(map[string]any) ([]byte, error)
}
