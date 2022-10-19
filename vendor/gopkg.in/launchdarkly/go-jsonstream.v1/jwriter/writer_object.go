package jwriter

// ObjectWriter is a decorator that writes values to an underlying Writer within the context of a
// JSON object, adding property names and commas between values as appropriate.
type ObjectState struct {
	w             *Writer
	hasItems      bool
	previousState writerState
}

// Name writes an object property name and a colon. You can then use Writer methods to write
// the property value. The return value is the same as the underlying Writer, so you can chain
// method calls:
//
//     obj.Name("myBooleanProperty").Bool(true)
func (obj *ObjectState) Name(name string) *Writer {
	if obj.w == nil || obj.w.err != nil {
		return &noOpWriter
	}
	if obj.hasItems {
		if err := obj.w.tw.Delimiter(','); err != nil {
			obj.w.AddError(err)
			return obj.w
		}
	}
	obj.hasItems = true
	obj.w.AddError(obj.w.tw.PropertyName(name))
	return obj.w
}

// Maybe writes an object property name conditionally depending on a boolean parameter.
// If shouldWrite is true, this behaves the same as Property(name). However, if shouldWrite is false,
// it does not write a property name and instead of returning the underlying Writer, it returns
// a stub Writer that does not produce any output. This allows you to chain method calls without
// having to use an if statement.
//
//     obj.Maybe(shouldWeIncludeTheProperty, "myBooleanProperty").Bool(true)
func (obj *ObjectState) Maybe(name string, shouldWrite bool) *Writer {
	if obj.w == nil {
		return &noOpWriter
	}
	if shouldWrite {
		return obj.Name(name)
	}
	return &noOpWriter
}

// End writes the closing delimiter of the object.
func (obj *ObjectState) End() {
	if obj.w == nil || obj.w.err != nil {
		return
	}
	obj.w.AddError(obj.w.tw.Delimiter('}'))
	obj.w.state = obj.previousState
	obj.w = nil
}
