package closer

// Func is like http.HandlerFunc but for io.Closers.
type Func func() error

// Close implements io.Closer.
func (f Func) Close() error {
	return f()
}
