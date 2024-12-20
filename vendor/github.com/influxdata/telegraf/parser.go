package telegraf

// Parser is an interface defining functions that a parser plugin must satisfy.
type Parser interface {
	// Parse takes a byte buffer separated by newlines
	// ie, `cpu.usage.idle 90\ncpu.usage.busy 10`
	// and parses it into telegraf metrics
	//
	// Must be thread-safe.
	Parse(buf []byte) ([]Metric, error)

	// ParseLine takes a single string metric
	// ie, "cpu.usage.idle 90"
	// and parses it into a telegraf metric.
	//
	// Must be thread-safe.
	ParseLine(line string) (Metric, error)

	// SetDefaultTags tells the parser to add all of the given tags
	// to each parsed metric.
	// NOTE: do _not_ modify the map after you've passed it here!!
	SetDefaultTags(tags map[string]string)
}

// ParserFunc is a function to create a new instance of a parser
type ParserFunc func() (Parser, error)

// ParserPlugin is an interface for plugins that are able to parse
// arbitrary data formats.
type ParserPlugin interface {
	// SetParser sets the parser function for the interface
	SetParser(parser Parser)
}

// ParserFuncPlugin is an interface for plugins that are able to parse
// arbitrary data formats.
type ParserFuncPlugin interface {
	// GetParser returns a new parser.
	SetParserFunc(fn ParserFunc)
}
