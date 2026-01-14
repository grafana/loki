package koanf

// Provider represents a configuration provider. Providers can
// read configuration from a source (file, HTTP etc.)
type Provider interface {
	// ReadBytes returns the entire configuration as raw []bytes to be parsed.
	// with a Parser.
	ReadBytes() ([]byte, error)

	// Read returns the parsed configuration as a nested map[string]interface{}.
	// It is important to note that the string keys should not be flat delimited
	// keys like `parent.child.key`, but nested like `{parent: {child: {key: 1}}}`.
	Read() (map[string]interface{}, error)
}

// Parser represents a configuration format parser.
type Parser interface {
	Unmarshal([]byte) (map[string]interface{}, error)
	Marshal(map[string]interface{}) ([]byte, error)
}
