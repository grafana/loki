package jwriter

// Writable is an interface for types that can write their data to a Writer.
type Writable interface {
	// WriteToJSONWriter writes JSON content to the Writer.
	//
	// This method does not need to return an error value. If the Writer encounters an error during output
	// generation, it will remember its own error state, which can be detected with Writer.Error().
	WriteToJSONWriter(*Writer)
}
