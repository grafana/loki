package jwriter

// MarshalJSONWithWriter is a convenience method for implementing json.Marshaler to marshal to a
// byte slice with the default TokenWriter implementation.
func MarshalJSONWithWriter(writable Writable) ([]byte, error) {
	w := NewWriter()
	w.tw.Grow(1000)
	writable.WriteToJSONWriter(&w)
	if err := w.Error(); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}
