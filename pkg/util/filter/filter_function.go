package filter

// Func is a function type that is the signature of a filter function used in log deletion.
// This is used as a parameter in the Rebound function.
type Func func(string) bool
