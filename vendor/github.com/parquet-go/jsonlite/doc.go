// Package jsonlite provides a lightweight JSON parser optimized for
// performance through careful memory management. It parses JSON into
// a tree of Value nodes that can be inspected and serialized back to JSON.
//
// The parser handles all standard JSON types: null, booleans, numbers,
// strings, arrays, and objects. It properly handles UTF-16 surrogate
// pairs for emoji and extended Unicode characters.
package jsonlite
