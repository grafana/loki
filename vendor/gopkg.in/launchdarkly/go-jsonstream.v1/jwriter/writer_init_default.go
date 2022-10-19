package jwriter

import "io"

// NewWriter creates a Writer that will buffer its entire output in memory.
//
// This function returns the struct by value (Writer, not *Writer). This avoids the overhead of a
// heap allocation since, in typical usage, the Writer will not escape the scope in which it was
// declared and can remain on the stack.
func NewWriter() Writer {
	return Writer{tw: newTokenWriter()}
}

// NewStreamingWriter creates a Writer that will buffer a limited amount of its output in memory
// and dump the output to the specified io.Writer whenever the buffer is full. You should also
// call Flush at the end of your output to ensure that any remaining buffered output is flushed.
//
// If the Writer returns an error at any point, it enters a failed state and will not try to
// write any more data to the target.
//
// This function returns the struct by value (Writer, not *Writer). This avoids the overhead of a
// heap allocation since, in typical usage, the Writer will not escape the scope in which it was
// declared and can remain on the stack.
func NewStreamingWriter(target io.Writer, bufferSize int) Writer {
	return Writer{tw: newStreamingTokenWriter(target, bufferSize)}
}
