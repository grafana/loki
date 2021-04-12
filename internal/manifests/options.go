package manifests

import (
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
)

// Options is a set of options to use when building manifests such as resource sizes, etc.
// Most of this should be provided - either directly or indirectly - by the user. This will
// probably be converted from the CR.
type Options struct {
	Name      string
	Namespace string
	Image     string

	Stack lokiv1beta1.LokiStackSpec
}
