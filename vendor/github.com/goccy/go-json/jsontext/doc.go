// Package jsontext reads and writes the syntax of JSON, as RFC 4627, RFC 7159, RFC 7493, RFC 8259 and RFC 8785
// specify it. It is a drop-in replacement of encoding/json/jsontext: its API and behavior are the ones of the
// package of Go 1.27, on any version of Go.
//
// An Encoder writes, and a Decoder reads, a stream of JSON tokens and values.
//
// # Tokens and Values
//
// A JSON token is one of the elements of the structure of JSON:
//
//   - a JSON literal (i.e., null, true, or false)
//   - a JSON string (e.g., "hello, world!")
//   - a JSON number (e.g., 123.456)
//   - a begin or end delimiter of a JSON object (i.e., '{' or '}')
//   - a begin or end delimiter of a JSON array (i.e., '[' or ']')
//
// The Token type holds a token. The commas and colons are structural characters too, but no Token holds
// them: the grammar implies where they are, such as the colon between the name and the value of a member of an
// object.
//
// A JSON value is a whole unit of JSON:
//
//   - a JSON literal, string, or number
//   - a JSON object (e.g., `{"name":"value"}`)
//   - a JSON array (e.g., `[1,2,3]`)
//
// The Value type holds a value, as the raw text of it. Literals, strings and numbers are both tokens and
// values; only a value holds a whole object or array.
//
// The methods of Encoder and Decoder write or read the next Token or Value of a stream, and check with a
// state machine that the stream is valid JSON. Options passed to NewEncoder and NewDecoder configure them.
//
// # Terminology
//
// "Encode" and "decode" are about the syntax of JSON, which this package deals with, and "marshal" and
// "unmarshal" about the meaning of JSON values as Go values, which encoding/json/v2 deals with. A stream of
// tokens can be encoded or decoded without any Go value which represents it.
//
// This package uses the terms of JSON:
//
//   - an "object" is an unordered collection of members, each a name and a value.
//   - an "array" is an ordered sequence of elements.
//   - a "value" is a literal (i.e., null, false, or true), a string, a number, an object, or an array.
//
// See RFC 8259.
//
// # Specifications
//
// The RFCs are, in the order of their strictness:
//
//   - RFC 4627 and RFC 7159 recommend, but don't require, UTF-8 and unique names in an object.
//   - RFC 8259 requires UTF-8, and recommends unique names.
//   - RFC 7493 requires UTF-8 and unique names.
//   - RFC 8785 defines a canonical form: UTF-8, unique names in a specified order, and exact formats of the
//     strings and numbers.
//
// RFC 4627 restricts the top-level values to objects and arrays; the later RFCs allow any value there.
//
// By default, this package operates as RFC 7493 specifies, which is stricter than RFC 8259 and fully
// complies with it: it chooses a behavior where RFC 8259 leaves one undefined, for interoperability. Options
// configure it to operate as another RFC specifies.
package jsontext
