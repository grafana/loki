package grpc

// unsafeByteCodec is a byte slice type that implements Codec
type unsafeByteCodec []byte

// MarshalBinary returns the contained byte-slice
func (c *unsafeByteCodec) MarshalBinary() ([]byte, error) {
	return *c, nil
}

// UnmarshalBinary to provided data so they share the same backing array
// this is a generally unsafe performance optimization, but safe in the context
// of the gRPC server.
func (c *unsafeByteCodec) UnmarshalBinary(data []byte) error {
	*c = data
	return nil
}
