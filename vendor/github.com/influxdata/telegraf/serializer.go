package telegraf

// Serializer is an interface defining functions that a serializer plugin must
// satisfy.
//
// Implementations of this interface should be reentrant but are not required
// to be thread-safe.
type Serializer interface {
	// Serialize takes a single telegraf metric and turns it into a byte buffer.
	// separate metrics should be separated by a newline, and there should be
	// a newline at the end of the buffer.
	//
	// New plugins should use SerializeBatch instead to allow for non-line
	// delimited metrics.
	Serialize(metric Metric) ([]byte, error)

	// SerializeBatch takes an array of telegraf metric and serializes it into
	// a byte buffer.  This method is not required to be suitable for use with
	// line oriented framing.
	SerializeBatch(metrics []Metric) ([]byte, error)
}

// SerializerFunc is a function to create a new instance of a serializer
type SerializerFunc func() (Serializer, error)

// SerializerPlugin is an interface for plugins that are able to
// serialize telegraf metrics into arbitrary data formats.
type SerializerPlugin interface {
	// SetSerializer sets the serializer function for the interface.
	SetSerializer(serializer Serializer)
}

// SerializerFuncPlugin is an interface for plugins that are able to serialize
// arbitrary data formats and require multiple instances of a parser.
type SerializerFuncPlugin interface {
	// GetParser returns a new parser.
	SetSerializerFunc(fn SerializerFunc)
}
