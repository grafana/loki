package conversion

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

// Decoder knows how to decode the contents of a CRD version conversion
// request into a concrete object.
// TODO(droot): consider reusing decoder from admission pkg for this.
type Decoder struct {
	codecs serializer.CodecFactory
}

// NewDecoder creates a Decoder given the runtime.Scheme
func NewDecoder(scheme *runtime.Scheme) (*Decoder, error) {
	return &Decoder{codecs: serializer.NewCodecFactory(scheme)}, nil
}

// Decode decodes the inlined object.
func (d *Decoder) Decode(content []byte) (runtime.Object, *schema.GroupVersionKind, error) {
	deserializer := d.codecs.UniversalDeserializer()
	return deserializer.Decode(content, nil, nil)
}

// DecodeInto decodes the inlined object in the into the passed-in runtime.Object.
func (d *Decoder) DecodeInto(content []byte, into runtime.Object) error {
	deserializer := d.codecs.UniversalDeserializer()
	return runtime.DecodeInto(deserializer, content, into)
}
