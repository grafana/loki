package log

// UnsafeBytes is a log line that the code does not own. The owner can reuse or overwrite its
// memory as soon as the call that gets it returns.
//
// A stage must not keep UnsafeData, a slice of it, or a string that aliases it, for example one
// made with unsafe. This includes the values of the labels that a stage sets. A stage must copy
// what it keeps. A line that a stage returns can alias the line that it got, so the same rule
// applies to the line that Process returns.
//
// UnsafeBytes is a struct, not an alias or a defined type over []byte. An alias is the same type
// as []byte, so the compiler does not tell them apart. A defined type is a new type, but Go
// assigns it to a []byte without a conversion. Code can then pass the line to any function that
// takes []byte, or store it, and nothing in the code shows that. A struct has no implicit
// conversion. Each read of the memory needs UnsafeData, so every use of the raw bytes is explicit
// and searchable.
type UnsafeBytes struct {
	// UnsafeData is the memory of the line. The name repeats the warning at each use, so a reader
	// sees that the code touches memory that it does not own.
	UnsafeData []byte
}
