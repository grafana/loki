package molecule

// Package molecule is a Go library for parsing and encoding protobufs in an
// efficient and zero-allocation manner, progressively consuming or creating
// the encoded bytes in a streaming fashion instead of (un)marhsaling full
// structs.
//
// While protobuf is not intended for use in infinite streams, such an
// interface is still useful for rapidly creating protobuf messages from data
// that is not already in easily-marshaled structs.  In other words, this
// package can save a substantial amount of copying and allocation over an
// equivalent implementation using more typical struct marshaling.
//
// The drawback is that the implementation requires a more detailed
// understanding of the protobuf encoding, including the field numbers and
// types for the messages being encoded.
//
// ## Decoding
//
// To decode a protobuf message, create a `codec.Buffer` wrapping the encoded
// data, and then use `molecule.MessageEach` to iterate over each field in that
// message.
//
// See the examples for more details.
//
// ## Encoding
//
// Begin by creating a new `ProtoStream` with `New`, passing an `io.Writer`
// to which the output bytes should be written.  In many cases this is simply
// a `bytes.Buffer`, but any writer will do.
//
// Next, for each field, call the method appropriate to its protobuf type,
// passing the field number as the first argument.  For repeated fields
// containing scalar types, use the `*Packed` methods to write the packed form.
// For non-scalar repeated fields, call the encoding method repeatedly.
//
// This package requires a more detailed understanding of the protobuf encoding
// format than marshaling-based packages.  In particular, the message fields
// and their types must be encoded in the source calling each of the methods.
// The field numbers are best encoded as `const` values in the source code.
// For protocol compatibility, such field numbers will never change, so there
// is no difficulty with synchronizing the values in two places.  See the
// examples for details.
//
// Note that most methods will do nothing when given their zero value.
