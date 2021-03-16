package client

import (
	"errors"
	"net/url"

	"k8s.io/apimachinery/pkg/conversion/queryparams"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ runtime.ParameterCodec = noConversionParamCodec{}

// noConversionParamCodec is a no-conversion codec for serializing parameters into URL query strings.
// it's useful in scenarios with the unstructured client and arbitrary resources.
type noConversionParamCodec struct{}

func (noConversionParamCodec) EncodeParameters(obj runtime.Object, to schema.GroupVersion) (url.Values, error) {
	return queryparams.Convert(obj)
}

func (noConversionParamCodec) DecodeParameters(parameters url.Values, from schema.GroupVersion, into runtime.Object) error {
	return errors.New("DecodeParameters not implemented on noConversionParamCodec")
}
